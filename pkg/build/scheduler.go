package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/glob"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

const (
	subdirObjects   = "object"
	subdirGenerated = "generated"
)

type ResolvedTarget struct {
	Node          *BuildNode
	SourceFiles   []string
	AllIncludes   []string
	AllDefines    []string
	AllCFlags     []string
	AllCxxFlags   []string
	AllLdFlags    []string
	AllLinks      []string
	DepArtifacts  []string
	OutputPath    string
	VersionScript string
	LinkerScript  string
	ExcludeLibs   []string
	SymbolBinding string
}

type compileResult struct {
	src     string
	objPath string
	deps    []string
	err     error
}

type PkgInfo struct {
	api.PkgDirs
	OutputDir string
	BuildKey  string
}

func (pi *PkgInfo) OutputPath(subpath string) string {
	if pi.OutputDir != "" {
		return filepath.Join(pi.OutputDir, subpath)
	}
	return BuildPath(".", pi.BuildKey, subpath)
}

func (pi *PkgInfo) GeneratedDir() string {
	if pi.OutputDir != "" {
		return filepath.Join(pi.OutputDir, subdirGenerated)
	}
	return BuildPath(".", pi.BuildKey, subdirGenerated)
}

type Scheduler struct {
	ctx           context.Context
	graph         *BuildGraph
	compiler      *Compiler
	linker        *Linker
	toolchain     *toolchain.Toolchain
	platform      api.Platform
	resolvedTools *ResolvedTools
	tcName        string
	mode          string
	pkgOptions    map[string]map[string]any
	pkgs          map[string]*PkgInfo
	ccWriter      *CompileCommandsWriter
	packages      map[string]*api.Package
	rootDir       string
	includeTests  bool
	pkgKeyExtra   map[string]string
	pkgLockDir    string
	numWorkers    int
	keepGoing     bool

	buildTargetFn  func(string) error
	globalCFlags   []string
	globalCxxFlags []string
	globalLdFlags  []string
	globalLinks    []string
}

func NewScheduler(
	graph *BuildGraph,
	tc *toolchain.Toolchain,
	pkgDirs map[string]*api.PkgDirs,
	mode string,
	pkgOptions map[string]map[string]any,
	platform api.Platform,
) (*Scheduler, error) {
	tools, err := ResolveTools(tc, platform)
	if err != nil {
		return nil, err
	}
	return newScheduler(graph, tc, pkgDirs, mode, pkgOptions, platform, tools), nil
}

func newScheduler(graph *BuildGraph, tc *toolchain.Toolchain, pkgDirs map[string]*api.PkgDirs, mode string, pkgOptions map[string]map[string]any, platform api.Platform, tools *ResolvedTools) *Scheduler {
	compiler := NewCompiler(tools)
	linker := NewLinker(tools)

	tcName := tc.Name
	if mode == "" {
		mode = api.ModeDebug
	}

	ccWriter := NewCompileCommandsWriter(tools)
	manager := toolchain.GetManager()

	s := &Scheduler{
		ctx:            context.Background(),
		graph:          graph,
		compiler:       compiler,
		linker:         linker,
		toolchain:      tc,
		platform:       platform,
		resolvedTools:  tools,
		tcName:         tcName,
		mode:           mode,
		pkgOptions:     pkgOptions,
		pkgs:           make(map[string]*PkgInfo),
		ccWriter:       ccWriter,
		packages:       make(map[string]*api.Package),
		globalCFlags:   append([]string{}, manager.GetGlobalCFlags()...),
		globalCxxFlags: append([]string{}, manager.GetGlobalCxxFlags()...),
		globalLdFlags:  append([]string{}, manager.GetGlobalLdFlags()...),
		globalLinks:    append([]string{}, manager.GetGlobalLinks()...),
	}

	for pkgName, pd := range pkgDirs {
		buildKey := BuildKey(tools.CCKey(), mode, pkgOptions[pkgName], s.pkgExtra(pkgName))
		info := &PkgInfo{
			PkgDirs:  *pd,
			BuildKey: buildKey,
		}
		if pd.BuildDir != "" {
			info.OutputDir = pd.BuildDir
		}
		s.pkgs[pkgName] = info
	}

	return s
}

func (s *Scheduler) SetPackage(pkgName string, pkg *api.Package) {
	s.packages[pkgName] = pkg
}

func (s *Scheduler) SetRootDir(dir string) {
	s.rootDir = dir
}

func (s *Scheduler) SetIncludeTests(v bool) {
	s.includeTests = v
}

func (s *Scheduler) SetPkgKeyExtra(extra map[string]string) {
	s.pkgKeyExtra = maps.Clone(extra)
	for name, info := range s.pkgs {
		info.BuildKey = BuildKey(s.toolIdentity(), s.mode, s.pkgOptions[name], s.pkgExtra(name))
	}
}

func (s *Scheduler) SetPkgLockDir(dir string) {
	s.pkgLockDir = dir
}

func (s *Scheduler) SetNumWorkers(n int) {
	s.numWorkers = n
}

func (s *Scheduler) SetKeepGoing(v bool) {
	s.keepGoing = v
}

func (s *Scheduler) SetBuildTargetFunc(fn func(string) error) {
	s.buildTargetFn = fn
}

func (s *Scheduler) pkgExtra(pkgName string) string {
	if s.pkgKeyExtra == nil {
		return ""
	}
	return s.pkgKeyExtra[pkgName]
}

func (s *Scheduler) lockPackage(pkgName string) (func(), error) {
	if s.pkgLockDir == "" {
		return func() {}, nil
	}
	h := sha256.Sum256([]byte(pkgName))
	safe := strings.ReplaceAll(pkgName, "/", "_") + "_" + hex.EncodeToString(h[:4])
	l, err := flock.AcquireContext(s.ctx, filepath.Join(s.pkgLockDir, safe+".lock"))
	if err != nil {
		return nil, fmt.Errorf("acquire build lock for %s: %w", pkgName, err)
	}
	return func() { _ = l.Release() }, nil
}

func (s *Scheduler) SetPkgDirs(pkgName string, dirs *api.PkgDirs) {
	if info, ok := s.pkgs[pkgName]; ok {
		info.PkgDirs = *dirs
	} else {
		s.pkgs[pkgName] = &PkgInfo{PkgDirs: *dirs}
	}
}

func (s *Scheduler) GetPkgInfo(pkgName string) (*PkgInfo, bool) {
	info, ok := s.pkgs[pkgName]
	return info, ok
}

func (s *Scheduler) effectiveSourceDir(pkgName string) string {
	if pkg := s.packages[pkgName]; pkg != nil && pkg.SrcDirRaw() != "" {
		return pkg.SrcDir()
	}
	if info := s.pkgs[pkgName]; info != nil {
		return info.SourceDir
	}
	return ""
}

func (s *Scheduler) pkgInfoOf(pkgName string) (*PkgInfo, error) {
	info := s.pkgs[pkgName]
	if info == nil {
		return nil, fmt.Errorf("package %s has no build dirs registered (SetPkgDirs not called)", pkgName)
	}
	return info, nil
}

func (s *Scheduler) BuildAll() error {
	var errs []error
	failedTargets := make(map[string]bool)
	buildTarget := s.buildTargetFn
	if buildTarget == nil {
		buildTarget = s.Build
	}
	err := s.graph.ForEachDefault(s.includeTests, func(node *BuildNode) error {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if s.depsFailed(node, failedTargets) {
			vlog.Info("[skip] %s (dependency failed)", node.FullName)
			failedTargets[node.FullName] = true
			return nil
		}
		if err := buildTarget(node.FullName); err != nil {
			failedTargets[node.FullName] = true
			errs = append(errs, err)
			if !s.keepGoing || s.ctx.Err() != nil {
				return err
			}
		}
		return nil
	})
	if err != nil && len(errs) == 0 {
		errs = append(errs, err)
	}
	root := s.rootDir
	if root == "" {
		root = "."
	}
	if err := s.ccWriter.Save(filepath.Join(root, "build", "compile_commands.json")); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *Scheduler) depsFailed(node *BuildNode, failedTargets map[string]bool) bool {
	for _, dep := range node.Deps {
		if failedTargets[dep] {
			return true
		}
	}
	return false
}

func (s *Scheduler) Build(fullName string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	node, err := s.graph.GetNode(fullName)
	if err != nil {
		return err
	}

	if !node.Target.IsDefault() {
		return nil
	}
	if node.Target.IsTest() && !s.includeTests {
		return nil
	}

	pkgInfo, err := s.pkgInfoOf(node.PkgName)
	if err != nil {
		return err
	}
	workDir := pkgInfo.SourceDir

	// The package lock serializes access to the SHARED remote out/<key>
	// directory in the global cache. Local package build dirs are
	// project-private — locking them by bare package name would serialize
	// unrelated projects that happen to use the same local package name.
	if meta, ok := s.graph.PkgMeta[node.PkgName]; ok && meta.IsRemote() {
		release, err := s.lockPackage(node.PkgName)
		if err != nil {
			return err
		}
		defer release()
	}
	vlog.Info("[%s]", fullName)

	resolved, err := s.resolveTarget(node)
	if err != nil {
		return err
	}

	if err := s.prepareTarget(resolved, pkgInfo, workDir); err != nil {
		return err
	}

	numFiles := len(resolved.SourceFiles)
	if numFiles == 0 || node.Target.Prebuilt() != "" {
		return s.finalizeTarget(resolved, pkgInfo, nil)
	}

	objs, err := s.compileAll(resolved, pkgInfo)
	if err != nil {
		return err
	}

	return s.finalizeTarget(resolved, pkgInfo, objs)
}

func (s *Scheduler) prepareTarget(resolved *ResolvedTarget, pkgInfo *PkgInfo, workDir string) error {
	node := resolved.Node

	genRules := node.Target.GenRules()
	if len(genRules) > 0 {
		generatedDir := pkgInfo.GeneratedDir()
		if err := runGenRulesContext(s.ctx, genRules, resolveWorkPath(workDir, generatedDir), workDir); err != nil {
			return err
		}
	}

	pkg := s.packages[node.PkgName]
	if pkg != nil && pkg.GenConfigHeader() {
		generatedDir := pkgInfo.GeneratedDir()
		if err := s.generateConfigHeader(pkg, resolveWorkPath(workDir, generatedDir)); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(resolveWorkPath(workDir, pkgInfo.OutputPath(subdirObjects)), 0755); err != nil {
		return fmt.Errorf("create build directory: %w", err)
	}
	return nil
}

func (s *Scheduler) finalizeTarget(resolved *ResolvedTarget, pkgInfo *PkgInfo, objs []string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if resolved.Node.Target.Kind() == api.TargetVoid {
		if err := s.buildVoidTarget(resolved); err != nil {
			return err
		}
		if err := s.postLink(resolved); err != nil {
			return err
		}
		if err := s.ctx.Err(); err != nil {
			return err
		}
		for _, output := range expandPostLinkArgs(resolved.Node.Target.PostLinkOutputs(), pkgInfo.SourceDir, resolved.OutputPath) {
			path := resolveWorkPath(pkgInfo.SourceDir, output)
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("post-link output %s: %w", path, err)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("post-link output is not a regular file: %s", path)
			}
		}
		return nil
	}
	action, err := s.planLink(resolved, objs)
	if err != nil {
		return err
	}
	if !actionUpToDate(action.recordPath, action.signature, pkgInfo.SourceDir, action.inputs, action.outputs) {
		if err := invalidateActionRecord(action.recordPath); err != nil {
			return err
		}
		if resolved.Node.Target.Prebuilt() != "" {
			if err := s.realizePrebuilt(resolved); err != nil {
				return err
			}
		} else {
			vlog.Info("  LINK %s", filepath.Base(resolved.OutputPath))
			if err := s.linker.execute(action.command, resolved.OutputPath, pkgInfo.SourceDir, resolved.Node.Target.Kind() == api.TargetStatic); err != nil {
				return err
			}
		}
		if err := s.postLink(resolved); err != nil {
			return err
		}
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if err := saveActionRecord(action.recordPath, action.signature, pkgInfo.SourceDir, action.inputs, action.outputs); err != nil {
			return err
		}
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if pkgInfo.InstallDir != "" {
		return s.publishTarget(resolved, pkgInfo)
	}
	return nil
}

func (s *Scheduler) compileAll(resolved *ResolvedTarget, pkgInfo *PkgInfo) ([]string, error) {
	numFiles := len(resolved.SourceFiles)

	numWorkers := s.numWorkers
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}
	if numWorkers > numFiles {
		numWorkers = numFiles
	}

	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	jobs := make(chan int, numFiles)
	results := make([]compileResult, numFiles)

	var wg sync.WaitGroup
	var firstError sync.Once
	var firstErr error

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case idx, ok := <-jobs:
					if !ok {
						return
					}
					if ctx.Err() != nil {
						return
					}
					src := resolved.SourceFiles[idx]
					objPath, deps, err := s.compileSourceContext(ctx, resolved, src)
					results[idx] = compileResult{src: src, objPath: objPath, deps: deps, err: err}
					if err != nil {
						firstError.Do(func() {
							firstErr = err
							cancel()
						})
					}
				}
			}
		}()
	}

feed:
	for i := range resolved.SourceFiles {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	objs := make([]string, 0, numFiles)
	for _, r := range results {
		if r.objPath != "" {
			objs = append(objs, r.objPath)
		}
	}
	return objs, nil
}

type depResolveResult struct {
	includes    []string
	voidLdFlags []string
	artifacts   []string
}

func (s *Scheduler) collectDepArtifacts(node *BuildNode) (*depResolveResult, error) {
	result := &depResolveResult{}

	for _, depName := range node.Deps {
		depNode, err := s.graph.GetNode(depName)
		if err != nil {
			return nil, fmt.Errorf("dependency not found: %s", depName)
		}

		depPkg := s.pkgs[depNode.PkgName]
		if depPkg == nil {
			continue
		}

		if len(depNode.Target.PublicIncludes()) > 0 {
			srcDir := s.pkgs[depNode.PkgName].SourceDir
			for _, pubInc := range depNode.Target.PublicIncludes() {
				result.includes = append(result.includes, filepath.Join(srcDir, pubInc))
			}
		} else if depPkg.InstallDir != "" {
			result.includes = append(result.includes, filepath.Join(depPkg.InstallDir, "include"))
		}

		if depNode.Target.Kind() == api.TargetVoid && depPkg.InstallDir != "" {
			libDir := fs.DetectLibDir(depPkg.InstallDir)
			var libNames []string
			if len(depNode.Target.ProvidedLibs()) > 0 {
				libNames = depNode.Target.ProvidedLibs()
			} else {
				libNames = []string{depNode.Target.Name()}
			}
			for _, lib := range libNames {
				archivePath := filepath.Join(libDir, "lib"+lib+".a")
				if fs.FileExists(archivePath) {
					result.artifacts = append(result.artifacts, archivePath)
				} else {
					result.voidLdFlags = append(result.voidLdFlags, "-l"+lib)
				}
			}
		} else if depNode.Target.Kind() != api.TargetVoid {
			var depOutput string
			if depPkg.InstallDir != "" && depPkg.OutputDir == "" {
				depOutput = filepath.Join(depPkg.InstallDir, "lib", targetFilename(depNode.Target.Kind(), depNode.Target.Name(), s.targetOS(depNode.PkgName)))
			} else {
				depOutput = s.getTargetOutputPath(depNode)
			}
			// A PE shared library is linked against its import library, which
			// LinkShared emits next to the DLL as libfoo.dll.a. Prebuilt
			// targets ship only the declared file, so consumers link the DLL
			// itself.
			if s.targetOS(depNode.PkgName) == "windows" && depNode.Target.Kind() == api.TargetShared && depNode.Target.Prebuilt() == "" {
				depOutput += ".a"
			}
			if !fs.FileExists(depOutput) {
				return nil, fmt.Errorf("dependency artifact missing: %s (was the dependency target %s built?)", depOutput, depName)
			}
			result.artifacts = append(result.artifacts, depOutput)

			for _, lib := range depNode.Target.ProvidedLibs() {
				if lib != depNode.Target.Name() {
					archivePath := ""
					if depPkg.InstallDir != "" {
						libDir := fs.DetectLibDir(depPkg.InstallDir)
						archivePath = filepath.Join(libDir, "lib"+lib+".a")
					}
					if archivePath != "" && fs.FileExists(archivePath) {
						result.artifacts = append(result.artifacts, archivePath)
					} else {
						result.voidLdFlags = append(result.voidLdFlags, "-l"+lib)
					}
				}
			}
		}
	}

	return result, nil
}

func (s *Scheduler) resolveTarget(node *BuildNode) (*ResolvedTarget, error) {
	modeFlags, modeDefines := api.GetModeFlags(s.mode)
	var visibilityCFlags, visibilityCxxFlags []string
	if pkg := s.packages[node.PkgName]; pkg != nil {
		visibilityCFlags, visibilityCxxFlags = pkg.VisibilityFlags()
	}

	resolved := &ResolvedTarget{
		Node:        node,
		AllDefines:  append([]string{}, node.Target.Defines()...),
		AllCFlags:   append(visibilityCFlags, node.Target.CFlags()...),
		AllCxxFlags: append(visibilityCxxFlags, node.Target.CxxFlags()...),
		AllLdFlags:  append([]string{}, node.Target.LdFlags()...),
		AllLinks:    append([]string{}, node.Target.Links()...),
	}

	resolved.AllCFlags = append(resolved.AllCFlags, modeFlags...)
	resolved.AllCFlags = append(resolved.AllCFlags, s.globalCFlags...)
	resolved.AllCxxFlags = append(resolved.AllCxxFlags, modeFlags...)
	resolved.AllCxxFlags = append(resolved.AllCxxFlags, s.globalCxxFlags...)
	resolved.AllDefines = append(resolved.AllDefines, modeDefines...)
	resolved.AllLdFlags = append(resolved.AllLdFlags, s.globalLdFlags...)
	resolved.AllLinks = append(resolved.AllLinks, s.globalLinks...)

	for _, inc := range node.Target.Includes() {
		resolved.AllIncludes = append(resolved.AllIncludes, inc)
	}

	for _, pubInc := range node.Target.PublicIncludes() {
		resolved.AllIncludes = append(resolved.AllIncludes, pubInc)
	}

	deps, err := s.collectDepArtifacts(node)
	if err != nil {
		return nil, err
	}
	resolved.AllIncludes = append(resolved.AllIncludes, deps.includes...)
	resolved.DepArtifacts = deps.artifacts

	s.checkLibDependencies(resolved)

	resolved.AllLdFlags = append(resolved.AllLdFlags, deps.voidLdFlags...)

	excludes := node.Target.ExcludedFiles()
	pkgInfo, err := s.pkgInfoOf(node.PkgName)
	if err != nil {
		return nil, err
	}
	sourceDir := pkgInfo.SourceDir
	for _, pattern := range node.Target.Files() {
		files, err := glob.Match(pattern, sourceDir)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if !matchesAny(f, excludes) {
				resolved.SourceFiles = append(resolved.SourceFiles, f)
			}
		}
	}

	resolved.OutputPath = s.getTargetOutputPath(node)

	genRules := node.Target.GenRules()
	pkg := s.packages[node.PkgName]
	needGenerated := len(genRules) > 0 || (pkg != nil && pkg.GenConfigHeader())
	if needGenerated {
		generatedDir := pkgInfo.GeneratedDir()
		resolved.AllIncludes = append(resolved.AllIncludes, generatedDir)
	}

	if vs := node.Target.VersionScript(); vs != "" {
		kind := node.Target.Kind()
		if kind != api.TargetShared && kind != api.TargetBinary {
			return nil, fmt.Errorf("SetVersionScript(%q): only valid on TargetShared/TargetBinary, got %q", vs, kind)
		}
		if pkgInfo != nil {
			resolved.VersionScript = filepath.Join(pkgInfo.SourceDir, vs)
		} else {
			resolved.VersionScript = vs
		}
	}

	if node.Target.Kind() == api.TargetBinary {
		linkerScript := node.Target.LinkerScript()
		if linkerScript != "" {
			linkerScript = resolveWorkPath(pkgInfo.SourceDir, linkerScript)
		}
		if linkerScript == "" && node.Target.UseDepLinkerScript() {
			for _, depFullName := range node.Deps {
				depPkgName, _, _ := strings.Cut(depFullName, ":")
				depPkg := s.packages[depPkgName]
				if depPkg != nil && depPkg.ProvidedLinkerScript() != "" {
					if depInfo := s.pkgs[depPkgName]; depInfo != nil {
						linkerScript = filepath.Join(depInfo.SourceDir, depPkg.ProvidedLinkerScript())
					}
					break
				}
			}
		}
		if node.Target.UseDepLinkerScript() && linkerScript == "" {
			return nil, fmt.Errorf("%s: no dependency provides a linker script", node.FullName)
		}
		if linkerScript != "" {
			info, err := os.Stat(linkerScript)
			if err != nil {
				return nil, fmt.Errorf("linker script %s: %w", linkerScript, err)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("linker script %s is not a regular file", linkerScript)
			}
		}
		resolved.LinkerScript = linkerScript
	}

	if el := node.Target.ExcludeLibs(); len(el) > 0 {
		resolved.ExcludeLibs = append([]string{}, el...)
	}
	resolved.SymbolBinding = node.Target.SymbolBinding()

	resolved.AllIncludes = unique(resolved.AllIncludes)
	resolved.SourceFiles = unique(resolved.SourceFiles)

	return resolved, nil
}

func targetFilename(kind api.TargetKind, name, targetOS string) string {
	return api.TargetFilename(kind, name, targetOS)
}

func (s *Scheduler) targetOS(pkgName string) string {
	if pkg := s.packages[pkgName]; pkg != nil {
		return pkg.TargetOS()
	}
	return s.platform.OSOrHost()
}

func objectPath(target, src, workDir string) (string, error) {
	root, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolveWorkPath(root, src))
	if err != nil {
		return "", fmt.Errorf("object source path %s: %w", src, err)
	}
	hash := sha256.Sum256([]byte(target + "\x00" + filepath.ToSlash(rel)))
	name := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)) + ".o"
	return filepath.Join(subdirObjects, hex.EncodeToString(hash[:]), name), nil
}

func (s *Scheduler) checkLibDependencies(resolved *ResolvedTarget) {
	kind := resolved.Node.Target.Kind()
	if kind != api.TargetStatic && kind != api.TargetObject {
		return
	}
	for _, artifact := range resolved.DepArtifacts {
		if isLibraryArtifact(artifact) {
			vlog.Info("  NOTE %s: library dependency %s provides build ordering/includes only; it is not merged into the %s output", resolved.Node.FullName, filepath.Base(artifact), kind)
		}
	}
}

func (s *Scheduler) getTargetOutputPath(node *BuildNode) string {
	return s.pkgs[node.PkgName].OutputPath(targetFilename(node.Target.Kind(), node.Target.Name(), s.targetOS(node.PkgName)))
}

func (s *Scheduler) compileSource(resolved *ResolvedTarget, src string) (string, []string, error) {
	return s.compileSourceContext(context.Background(), resolved, src)
}

func (s *Scheduler) compileSourceContext(ctx context.Context, resolved *ResolvedTarget, src string) (string, []string, error) {
	pkgInfo := s.pkgs[resolved.Node.PkgName]
	workDir := pkgInfo.SourceDir
	object, err := objectPath(resolved.Node.FullName, src, workDir)
	if err != nil {
		return "", nil, err
	}
	objRel := pkgInfo.OutputPath(object)
	opts := &CompileOptions{
		Includes: resolved.AllIncludes,
		Defines:  resolved.AllDefines,
		CFlags:   resolved.AllCFlags,
		CxxFlags: resolved.AllCxxFlags,
		Language: sourceLanguage(src),
		clang:    s.compiler.clangCC,
	}
	s.ccWriter.AddCommand(workDir, src, objRel, opts)
	compilerPath, flags := selectCompilerAndFlags(s.compiler.ccPath, s.compiler.cxxPath, opts.CFlags, opts.CxxFlags, opts)
	env, err := environmentSignature(s.compiler.env)
	if err != nil {
		return "", nil, err
	}
	signature, err := digestValue(struct {
		Version     int
		Target      string
		WorkDir     string
		TargetOS    string
		Tools       string
		Environment string
		Command     commandSpec
	}{buildFormatVersion, resolved.Node.FullName, workDir, s.targetOS(resolved.Node.PkgName), s.toolIdentity(), env, commandSpec{Program: compilerPath, Args: compileArgs(opts, objRel, src, flags, objRel+".d", workDir)}})
	if err != nil {
		return "", nil, err
	}
	recordPath := resolveWorkPath(workDir, objRel+".vmake.json")
	deps, depErr := ParseDepFile(resolveWorkPath(workDir, objRel+".d"))
	inputs := append([]string{src}, deps...)
	outputs := []string{objRel, objRel + ".d"}
	if depErr == nil && actionUpToDate(recordPath, signature, workDir, inputs, outputs) {
		return objRel, deps, nil
	}
	if err := invalidateActionRecord(recordPath); err != nil {
		return "", nil, err
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	vlog.Info("  CC %s", src)
	compiler := *s.compiler
	compiler.targetOS = s.targetOS(resolved.Node.PkgName)
	deps, err = compiler.CompileContext(ctx, src, objRel, opts, workDir)
	if err != nil {
		return "", nil, err
	}
	if err := ctx.Err(); err != nil {
		return "", nil, errors.Join(err, removeCompileFiles(workDir, objRel, objRel+".d"))
	}
	if err := saveActionRecord(recordPath, signature, workDir, append([]string{src}, deps...), outputs); err != nil {
		return "", nil, err
	}
	return objRel, deps, nil
}

func (s *Scheduler) needRelink(resolved *ResolvedTarget, objs []string) bool {
	action, err := s.planLink(resolved, objs)
	if err != nil {
		return true
	}
	return !actionUpToDate(action.recordPath, action.signature, s.pkgs[resolved.Node.PkgName].SourceDir, action.inputs, action.outputs)
}

func (s *Scheduler) realizePrebuilt(resolved *ResolvedTarget) error {
	src := resolved.Node.Target.Prebuilt()
	workDir := s.pkgs[resolved.Node.PkgName].SourceDir
	absDst := resolveWorkPath(workDir, resolved.OutputPath)
	absSrc := resolveWorkPath(workDir, src)

	if _, err := os.Stat(absSrc); err != nil {
		return fmt.Errorf("prebuilt file not found: %s: %w", absSrc, err)
	}

	vlog.Info("  PREBUILT %s", filepath.Base(absDst))
	if filepath.Clean(absSrc) == filepath.Clean(absDst) {
		return fmt.Errorf("prebuilt source and output must differ: %s", absSrc)
	}
	if err := os.Remove(absDst); err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(resolved.Node.Target.PostLinkSteps()) == 0 {
		srcPath, err := filepath.Abs(absSrc)
		if err != nil {
			return err
		}
		return fs.EnsureSymlink(absDst, srcPath)
	}
	return CopyFile(absSrc, absDst)
}

func (s *Scheduler) buildVoidTarget(resolved *ResolvedTarget) error {
	fn := resolved.Node.Target.BuildFunc()
	if fn == nil {
		return nil
	}

	pkg := s.ensurePackageForVoid(resolved.Node)
	s.populateDepsFromGraph(pkg, resolved.Node)

	pkg.SetGlobalFlags(s.globalCFlags, s.globalCxxFlags, s.globalLdFlags, s.globalLinks)

	if err := os.MkdirAll(pkg.BuildDir(), 0755); err != nil {
		return fmt.Errorf("create build dir: %s: %w", pkg.BuildDir(), err)
	}

	if err := fn(pkg); err != nil {
		return err
	}

	s.updateVoidLibDirs(resolved, pkg)
	return nil
}

func (s *Scheduler) ensurePackageForVoid(node *BuildNode) *api.Package {
	pkg := s.packages[node.PkgName]
	if pkg != nil {
		return pkg
	}

	pkgInfo := s.pkgs[node.PkgName]
	pkg = api.NewPackage()
	buildDir := pkgInfo.BuildDir
	if buildDir == "" {
		buildDir = BuildPath(pkgInfo.SourceDir, pkgInfo.BuildKey, "")
	}
	pkg.SetDirs(api.PkgDirs{
		SourceDir: pkgInfo.SourceDir,
		BuildDir:  buildDir,
	})
	pkg.SetToolchain(s.toolchain)
	pkg.SetPlatform(s.platform)
	pkg.SetSrcDir(pkgInfo.SourceDir)
	cfgVals := map[string]any{api.ModeOptionName: s.mode}
	if opts, ok := s.pkgOptions[node.PkgName]; ok {
		for k, v := range opts {
			cfgVals[k] = v
		}
	}
	pkg.SetCfgVals(cfgVals)
	s.packages[node.PkgName] = pkg
	return pkg
}

func (s *Scheduler) updateVoidLibDirs(resolved *ResolvedTarget, pkg *api.Package) {
	pkgName := resolved.Node.PkgName
	for _, dep := range pkg.Deps() {
		if dep.Name == pkgName {
			dep.UpdateLibDir()
		}
	}
	for _, otherPkg := range s.packages {
		if dep, ok := otherPkg.Deps()[pkgName]; ok {
			dep.UpdateLibDir()
		}
	}
}

func (s *Scheduler) postLink(resolved *ResolvedTarget) error {
	steps := resolved.Node.Target.PostLinkSteps()
	if len(steps) == 0 {
		return nil
	}

	workDir := s.pkgs[resolved.Node.PkgName].SourceDir

	for _, step := range steps {
		tool := s.resolvePostLinkTool(step.Tool)
		if tool == "" {
			return fmt.Errorf("post-link tool not found: %s", step.Tool)
		}

		args := expandPostLinkArgs(step.Args, workDir, resolved.OutputPath)

		vlog.Info("  %s %s", filepath.Base(tool), strings.Join(args, " "))
		if _, err := iexec.RunWithOptions(tool, args, iexec.RunOptions{Dir: workDir, Env: s.toolchain.CommandEnv(), Context: s.ctx}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scheduler) resolvePostLinkTool(name string) string {
	switch name {
	case "objcopy":
		return s.resolvedTools.OBJCOPY
	case "size":
		return s.resolvedTools.SIZE
	case "objdump":
		return s.resolvedTools.OBJDUMP
	case "nm":
		return s.resolvedTools.NM
	case "strip":
		return s.resolvedTools.STRIP
	}
	return ""
}

func (s *Scheduler) populateDepsFromGraph(pkg *api.Package, node *BuildNode) {
	for _, depFullName := range node.Deps {
		depNode := s.graph.Nodes[depFullName]
		if depNode == nil {
			continue
		}
		depPkgName := depNode.PkgName
		if _, ok := pkg.Deps()[depPkgName]; ok {
			continue
		}
		pkgInfo := s.pkgs[depPkgName]
		if pkgInfo == nil || pkgInfo.InstallDir == "" {
			continue
		}
		var depLibs []string
		depLibs = append(depLibs, depNode.Target.ProvidedLibs()...)
		ip := api.NewInstalledPackage(depPkgName, "", pkgInfo.InstallDir, depLibs)
		pkg.SetDep(depPkgName, ip)
	}
}

func sameFileContent(a, b string) bool {
	ha, err := FileHash(a)
	if err != nil {
		return false
	}
	hb, err := FileHash(b)
	if err != nil {
		return false
	}
	return ha == hb
}

func (s *Scheduler) publishTarget(resolved *ResolvedTarget, pkgInfo *PkgInfo) error {
	t := resolved.Node.Target
	kind := t.Kind()
	targetOS := s.targetOS(resolved.Node.PkgName)

	if kind == api.TargetVoid || kind == api.TargetObject || t.IsTest() {
		return nil
	}

	libDir := filepath.Join(pkgInfo.InstallDir, "lib")
	includeDir := filepath.Join(pkgInfo.InstallDir, "include")
	published := false

	if resolved.OutputPath != "" {
		srcPath := resolveWorkPath(pkgInfo.SourceDir, resolved.OutputPath)
		dest := filepath.Join(libDir, filepath.Base(resolved.OutputPath))
		if info, err := os.Stat(dest); err == nil && info.Mode().IsRegular() &&
			sameFileContent(srcPath, dest) && !implibMissing(srcPath, libDir, targetOS, kind) {
			vlog.Info("  SKIP (already published)")
			published = true
		}
	}

	if err := os.MkdirAll(libDir, 0755); err != nil {
		return fmt.Errorf("create lib dir: %w", err)
	}

	if resolved.OutputPath != "" && !published {
		srcPath := resolveWorkPath(pkgInfo.SourceDir, resolved.OutputPath)
		if _, err := os.Stat(srcPath); err == nil {
			dest := filepath.Join(libDir, filepath.Base(resolved.OutputPath))
			vlog.Info("  INSTALL %s -> %s", filepath.Base(resolved.OutputPath), dest)
			if err := CopyFile(srcPath, dest); err != nil {
				return fmt.Errorf("install library failed: %w", err)
			}
		}
		if implib := importLibraryPath(srcPath, targetOS, kind); implib != "" {
			if _, err := os.Stat(implib); err == nil {
				dest := filepath.Join(libDir, filepath.Base(implib))
				vlog.Info("  INSTALL %s -> %s", filepath.Base(implib), dest)
				if err := CopyFile(implib, dest); err != nil {
					return fmt.Errorf("install import library failed: %w", err)
				}
			}
		}
	}

	if err := os.MkdirAll(includeDir, 0755); err != nil {
		return fmt.Errorf("create include dir: %w", err)
	}

	srcDir := s.effectiveSourceDir(resolved.Node.PkgName)
	return copyPublicIncludes(t, srcDir, includeDir)
}

// implibMissing reports whether a PE shared target's import library still needs
// publishing: LinkShared emitted one next to the artifact, but the destination
// directory does not have it. Guards the "already published" fast path, which
// otherwise compares only the DLL and never repairs a deleted implib.
func implibMissing(srcPath, destDir, targetOS string, kind api.TargetKind) bool {
	implib := importLibraryPath(srcPath, targetOS, kind)
	if implib == "" {
		return false
	}
	if _, err := os.Stat(implib); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(destDir, filepath.Base(implib)))
	return err != nil
}

func unique(s []string) []string {
	if len(s) == 0 {
		return s
	}
	seen := make(map[string]bool, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

func matchesAny(path string, patterns []string) bool {
	for _, p := range patterns {
		if glob.MatchPath(p, path) {
			return true
		}
	}
	return false
}

func (s *Scheduler) generateConfigHeader(pkg *api.Package, generatedDir string) error {
	var importPkgs []*api.Package
	for _, depName := range pkg.ImportConfigs() {
		if depPkg := s.packages[depName]; depPkg != nil {
			importPkgs = append(importPkgs, depPkg)
		}
	}
	mergedOpts, mergedVals := api.MergeImportedOptions(pkg.Options, pkg.CfgVals, importPkgs)
	content := api.ConfigToHeader(mergedOpts, mergedVals)
	return api.WriteConfigHeader(generatedDir, content)
}
