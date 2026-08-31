package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	"github.com/spock2300/vmake/pkg/buildscript"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

const (
	subdirObjects   = "objects"
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
	graph         *BuildGraph
	compiler      *Compiler
	linker        *Linker
	toolchain     *toolchain.Toolchain
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
}

func NewScheduler(
	graph *BuildGraph,
	tc *toolchain.Toolchain,
	pkgDirs map[string]*api.PkgDirs,
	mode string,
	pkgOptions map[string]map[string]any,
) (*Scheduler, error) {
	tools, err := ResolveTools(tc)
	if err != nil {
		return nil, err
	}

	compiler := NewCompiler(tools)
	linker := NewLinker(tools)

	tcName := tc.Name
	if mode == "" {
		mode = api.ModeDebug
	}

	ccWriter := NewCompileCommandsWriter(tools)

	s := &Scheduler{
		graph:         graph,
		compiler:      compiler,
		linker:        linker,
		toolchain:     tc,
		resolvedTools: tools,
		tcName:        tcName,
		mode:          mode,
		pkgOptions:    pkgOptions,
		pkgs:          make(map[string]*PkgInfo),
		ccWriter:      ccWriter,
		packages:      make(map[string]*api.Package),
	}

	for pkgName, pd := range pkgDirs {
		buildKey := BuildKey(tools.CC, mode, pkgOptions[pkgName], s.pkgExtra(pkgName))
		info := &PkgInfo{
			PkgDirs:  *pd,
			BuildKey: buildKey,
		}
		if pd.BuildDir != "" {
			info.OutputDir = pd.BuildDir
		}
		s.pkgs[pkgName] = info
	}

	return s, nil
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
	s.pkgKeyExtra = extra
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
	l, err := flock.Acquire(filepath.Join(s.pkgLockDir, safe+".lock"))
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
	return s.pkgs[pkgName].SourceDir
}

func (s *Scheduler) BuildAll() error {
	var errs []error
	failedTargets := make(map[string]bool)
	if err := s.graph.ForEachDefault(s.includeTests, func(node *BuildNode) error {
		if s.depsFailed(node, failedTargets) {
			vlog.Info("[skip] %s (dependency failed)", node.FullName)
			failedTargets[node.FullName] = true
			return nil
		}
		if err := s.Build(node.FullName); err != nil {
			failedTargets[node.FullName] = true
			errs = append(errs, err)
			if !s.keepGoing {
				return err
			}
			return nil
		}
		return nil
	}); err != nil {
		return err
	}
	root := s.rootDir
	if root == "" {
		root = "."
	}
	saveErr := s.ccWriter.Save(filepath.Join(root, "build", "compile_commands.json"))
	if len(errs) > 0 {
		if saveErr != nil {
			errs = append(errs, saveErr)
		}
		return errors.Join(errs...)
	}
	return saveErr
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

	pkgInfo := s.pkgs[node.PkgName]
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
	s.ccWriter.SetPackageDir(workDir)

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
		if err := runGenRules(genRules, resolveWorkPath(workDir, generatedDir), workDir); err != nil {
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
	linked, err := s.realizeTarget(resolved, objs)
	if err != nil {
		return err
	}

	if linked {
		if err := s.postLink(resolved); err != nil {
			return err
		}
	}

	if pkgInfo.InstallDir != "" {
		if err := s.publishTarget(resolved, pkgInfo); err != nil {
			return err
		}
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

	goFiles, err := buildscript.ListGoFiles(pkgInfo.SourceDir)
	if err != nil {
		vlog.Error("list .go files in %s: %v", pkgInfo.SourceDir, err)
		buildGo := filepath.Join(pkgInfo.SourceDir, "build.go")
		if _, statErr := os.Stat(buildGo); statErr == nil {
			goFiles = []string{buildGo}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobs := make(chan int, numFiles)
	results := make([]compileResult, numFiles)

	var wg sync.WaitGroup

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
					src := resolved.SourceFiles[idx]
					objPath, deps, err := s.compileSource(resolved, src, goFiles)
					results[idx] = compileResult{src: src, objPath: objPath, deps: deps, err: err}
					if err != nil {
						cancel()
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

	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
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
				depOutput = filepath.Join(depPkg.InstallDir, "lib", targetFilename(depNode.Target.Kind(), depNode.Target.Name()))
			} else {
				depOutput = s.getTargetOutputPath(depNode)
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

	resolved := &ResolvedTarget{
		Node:        node,
		AllDefines:  append([]string{}, node.Target.Defines()...),
		AllCFlags:   append([]string{}, node.Target.CFlags()...),
		AllCxxFlags: append([]string{}, node.Target.CxxFlags()...),
		AllLdFlags:  append([]string{}, node.Target.LdFlags()...),
		AllLinks:    append([]string{}, node.Target.Links()...),
	}

	resolved.AllCFlags = append(resolved.AllCFlags, modeFlags...)
	resolved.AllCFlags = append(resolved.AllCFlags, toolchain.GetManager().GetGlobalCFlags()...)
	resolved.AllCxxFlags = append(resolved.AllCxxFlags, modeFlags...)
	resolved.AllCxxFlags = append(resolved.AllCxxFlags, toolchain.GetManager().GetGlobalCxxFlags()...)
	resolved.AllDefines = append(resolved.AllDefines, modeDefines...)
	resolved.AllLdFlags = append(resolved.AllLdFlags, toolchain.GetManager().GetGlobalLdFlags()...)
	resolved.AllLinks = append(resolved.AllLinks, toolchain.GetManager().GetGlobalLinks()...)

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
	sourceDir := s.pkgs[node.PkgName].SourceDir
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

	pkgInfo := s.pkgs[node.PkgName]
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
		resolved.LinkerScript = linkerScript
	}

	if el := node.Target.ExcludeLibs(); len(el) > 0 {
		resolved.ExcludeLibs = append([]string{}, el...)
	}
	resolved.SymbolBinding = node.Target.SymbolBinding()

	resolved.AllDefines = unique(resolved.AllDefines)
	resolved.AllIncludes = unique(resolved.AllIncludes)
	resolved.AllCFlags = unique(resolved.AllCFlags)
	resolved.AllCxxFlags = unique(resolved.AllCxxFlags)
	resolved.AllLdFlags = unique(resolved.AllLdFlags)

	return resolved, nil
}

func targetFilename(kind api.TargetKind, name string) string {
	return kind.Prefix() + name + kind.Ext()
}

func (s *Scheduler) checkLibDependencies(resolved *ResolvedTarget) {
	kind := resolved.Node.Target.Kind()
	if kind != api.TargetStatic && kind != api.TargetObject {
		return
	}
	for _, artifact := range resolved.DepArtifacts {
		ext := strings.ToLower(filepath.Ext(artifact))
		if ext == ".a" || ext == ".so" || ext == ".dylib" {
			vlog.Info("  NOTE %s: library dependency %s provides build ordering/includes only; it is not merged into the %s output", resolved.Node.FullName, filepath.Base(artifact), kind)
		}
	}
}

func (s *Scheduler) getTargetOutputPath(node *BuildNode) string {
	return s.pkgs[node.PkgName].OutputPath(targetFilename(node.Target.Kind(), node.Target.Name()))
}

func (s *Scheduler) compileSource(resolved *ResolvedTarget, src string, goFiles []string) (string, []string, error) {
	pkgInfo := s.pkgs[resolved.Node.PkgName]
	workDir := pkgInfo.SourceDir

	objRel := pkgInfo.OutputPath(filepath.Join(subdirObjects, strings.ReplaceAll(src, "/", "_")+".o"))

	lang := "c"
	if glob.IsCppFile(src) {
		lang = "cxx"
	}

	opts := &CompileOptions{
		Includes: resolved.AllIncludes,
		Defines:  resolved.AllDefines,
		CFlags:   resolved.AllCFlags,
		CxxFlags: resolved.AllCxxFlags,
		Language: lang,
	}

	s.ccWriter.AddCommand(src, objRel, opts)

	valid, deps := IsSourceValid(src, objRel, goFiles, workDir)
	if valid {
		return objRel, deps, nil
	}

	vlog.Info("  CC %s", src)

	deps, err := s.compiler.Compile(src, objRel, opts, workDir)
	if err != nil {
		return "", nil, err
	}

	return objRel, deps, nil
}

func (s *Scheduler) needRelink(resolved *ResolvedTarget, objs []string) bool {
	workDir := s.pkgs[resolved.Node.PkgName].SourceDir
	absOutput := resolveWorkPath(workDir, resolved.OutputPath)
	outputInfo, err := os.Stat(absOutput)
	if err != nil {
		return true
	}

	outputTime := outputInfo.ModTime()

	for _, obj := range objs {
		objInfo, err := os.Stat(resolveWorkPath(workDir, obj))
		if err != nil || objInfo.ModTime().After(outputTime) {
			return true
		}
	}

	for _, artifact := range resolved.DepArtifacts {
		artifactInfo, err := os.Stat(resolveWorkPath(workDir, artifact))
		if err != nil || artifactInfo.ModTime().After(outputTime) {
			return true
		}
	}

	for _, dep := range resolved.Node.Target.PostLinkDeps() {
		depPath := resolveWorkPath(workDir, dep)
		depInfo, err := os.Stat(depPath)
		if err != nil {
			vlog.Info("  RELINK %s (post-link dep %s missing)", resolved.Node.Target.Name(), dep)
			return true
		}
		if depInfo.ModTime().After(outputTime) {
			vlog.Info("  RELINK %s (post-link dep %s newer)", resolved.Node.Target.Name(), dep)
			return true
		}
	}

	for _, script := range []string{resolved.VersionScript, resolved.LinkerScript} {
		if script == "" {
			continue
		}
		scriptInfo, err := os.Stat(resolveWorkPath(workDir, script))
		if err != nil {
			vlog.Info("  RELINK %s (script %s missing)", resolved.Node.Target.Name(), script)
			return true
		}
		if scriptInfo.ModTime().After(outputTime) {
			vlog.Info("  RELINK %s (script %s newer)", resolved.Node.Target.Name(), script)
			return true
		}
	}

	return false
}

func (s *Scheduler) realizePrebuilt(resolved *ResolvedTarget) error {
	src := resolved.Node.Target.Prebuilt()
	workDir := s.pkgs[resolved.Node.PkgName].SourceDir
	absDst := resolveWorkPath(workDir, resolved.OutputPath)
	absSrc := resolveWorkPath(workDir, src)

	if _, err := os.Stat(absSrc); err != nil {
		return fmt.Errorf("prebuilt file not found: %s: %w", absSrc, err)
	}

	if link, err := os.Readlink(absDst); err == nil && link == absSrc {
		return nil
	}

	vlog.Info("  PREBUILT %s", filepath.Base(absDst))
	return fs.EnsureSymlink(absDst, absSrc)
}

func (s *Scheduler) buildVoidTarget(resolved *ResolvedTarget) error {
	fn := resolved.Node.Target.BuildFunc()
	if fn == nil {
		return nil
	}

	pkg := s.ensurePackageForVoid(resolved)
	s.populateDepsFromGraph(pkg, resolved.Node)

	mgr := toolchain.GetManager()
	pkg.SetGlobalFlags(mgr.GetGlobalCFlags(), mgr.GetGlobalCxxFlags(), mgr.GetGlobalLdFlags(), mgr.GetGlobalLinks())

	if s.isVoidUpToDate(pkg) && !s.depArtifactsNewer(resolved) {
		return nil
	}

	if err := os.MkdirAll(pkg.BuildDir(), 0755); err != nil {
		return fmt.Errorf("create build dir: %s: %w", pkg.BuildDir(), err)
	}

	srcDir := pkg.SrcDir()
	stamp := buildStampData(srcDir, pkg.ConfigFiles())

	if err := fn(pkg); err != nil {
		return err
	}

	if stamp.ConfigHash == "" && len(pkg.ConfigFiles()) > 0 {
		if h, err := computeConfigHash(srcDir, pkg.ConfigFiles()); err == nil {
			stamp.ConfigHash = h
		}
	}

	if pkg.InstallDir() == "" && pkg.BuildDir() != "" {
		if err := writeStamp(filepath.Join(pkg.BuildDir(), ".vmake_stamp"), stamp); err != nil {
			return fmt.Errorf("write stamp for %s: %w", resolved.Node.PkgName, err)
		}
	}

	s.updateVoidLibDirs(resolved, pkg)
	return nil
}

func (s *Scheduler) ensurePackageForVoid(resolved *ResolvedTarget) *api.Package {
	pkg := s.packages[resolved.Node.PkgName]
	if pkg != nil {
		return pkg
	}

	pkgInfo := s.pkgs[resolved.Node.PkgName]
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
	pkg.SetSrcDir(pkgInfo.SourceDir)
	cfgVals := map[string]any{api.ModeOptionName: s.mode}
	if opts, ok := s.pkgOptions[resolved.Node.PkgName]; ok {
		for k, v := range opts {
			cfgVals[k] = v
		}
	}
	pkg.SetCfgVals(cfgVals)
	s.packages[resolved.Node.PkgName] = pkg
	return pkg
}

func (s *Scheduler) isVoidUpToDate(pkg *api.Package) bool {
	if pkg.InstallDir() != "" {
		if info, err := os.Stat(pkg.InstallDir()); err == nil && info.IsDir() {
			entries, _ := os.ReadDir(pkg.InstallDir())
			if len(entries) > 0 {
				vlog.Info("  SKIP (already installed)")
				return true
			}
		}
		if err := os.MkdirAll(pkg.InstallDir(), 0755); err == nil {
			return false
		}
	} else if pkg.BuildDir() != "" {
		stampPath := filepath.Join(pkg.BuildDir(), ".vmake_stamp")
		srcDir := pkg.SrcDir()
		if isStampUpToDate(stampPath, srcDir, pkg.ConfigFiles()) {
			vlog.Info("  SKIP (already built)")
			return true
		}
	}
	return false
}

func (s *Scheduler) depArtifactsNewer(resolved *ResolvedTarget) bool {
	if len(resolved.DepArtifacts) == 0 {
		return false
	}
	pkg := s.packages[resolved.Node.PkgName]
	if pkg == nil || pkg.InstallDir() != "" || pkg.BuildDir() == "" {
		return false
	}
	stampPath := filepath.Join(pkg.BuildDir(), ".vmake_stamp")
	stampInfo, err := os.Stat(stampPath)
	if err != nil {
		return true
	}
	stampTime := stampInfo.ModTime()
	for _, artifact := range resolved.DepArtifacts {
		artInfo, err := os.Stat(artifact)
		if err != nil {
			vlog.Info("  REBUILD (dependency artifact %s missing)", filepath.Base(artifact))
			return true
		}
		if artInfo.ModTime().After(stampTime) {
			return true
		}
	}
	return false
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

func (s *Scheduler) realizeTarget(resolved *ResolvedTarget, objs []string) (bool, error) {
	kind := resolved.Node.Target.Kind()

	if resolved.Node.Target.Prebuilt() == "" && !s.needRelink(resolved, objs) {
		return false, nil
	}

	workDir := s.pkgs[resolved.Node.PkgName].SourceDir
	outputName := filepath.Base(resolved.OutputPath)

	if resolved.Node.Target.Prebuilt() != "" {
		return true, s.realizePrebuilt(resolved)
	}

	allObjs := append(append([]string{}, objs...), resolved.DepArtifacts...)

	switch kind {
	case api.TargetBinary:
		linkerScript := resolved.LinkerScript
		vlog.Info("  LINK %s", outputName)
		policy := LinkPolicy{
			VersionScript: resolved.VersionScript,
			ExcludeLibs:   resolved.ExcludeLibs,
			SymbolBinding: resolved.SymbolBinding,
		}
		err := s.linker.LinkBinary(allObjs, unique(resolved.AllLinks), resolved.AllLdFlags, resolved.OutputPath, linkerScript, policy, workDir)
		return err == nil, err
	case api.TargetStatic:
		var objOnly []string
		for _, o := range allObjs {
			ext := strings.ToLower(filepath.Ext(o))
			if ext != ".a" && ext != ".so" && ext != ".dylib" {
				objOnly = append(objOnly, o)
			}
		}
		vlog.Info("  AR %s", outputName)
		err := s.linker.LinkStatic(objOnly, resolved.OutputPath, workDir)
		return err == nil, err
	case api.TargetShared:
		vlog.Info("  LINK %s", outputName)
		policy := LinkPolicy{
			VersionScript: resolved.VersionScript,
			ExcludeLibs:   resolved.ExcludeLibs,
			SymbolBinding: resolved.SymbolBinding,
		}
		err := s.linker.LinkShared(allObjs, resolved.AllLdFlags, resolved.OutputPath, policy, workDir)
		return err == nil, err
	case api.TargetObject:
		vlog.Info("  LD -r %s", outputName)
		if len(objs) == 0 {
			return false, fmt.Errorf("object target requires at least one source file")
		}
		err := s.linker.LinkObject(objs, resolved.OutputPath, workDir)
		return err == nil, err
	case api.TargetVoid:
		err := s.buildVoidTarget(resolved)
		return err == nil, err
	default:
		return false, fmt.Errorf("target %s has unknown kind %q", resolved.Node.FullName, kind)
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

		args := make([]string, len(step.Args))
		for i, a := range step.Args {
			args[i] = strings.ReplaceAll(a, "{output}", resolved.OutputPath)
		}

		vlog.Info("  %s %s", filepath.Base(tool), strings.Join(args, " "))
		if _, err := iexec.RunInDir(tool, workDir, args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scheduler) resolvePostLinkTool(name string) string {
	switch strings.ToUpper(name) {
	case "OBJCOPY":
		if s.resolvedTools.OBJCOPY != "" {
			return s.resolvedTools.OBJCOPY
		}
	case "SIZE":
		if s.resolvedTools.SIZE != "" {
			return s.resolvedTools.SIZE
		}
	case "OBJDUMP":
		if s.resolvedTools.OBJDUMP != "" {
			return s.resolvedTools.OBJDUMP
		}
	case "NM":
		if s.resolvedTools.NM != "" {
			return s.resolvedTools.NM
		}
	case "STRIP":
		if s.toolchain.Prefix != "" {
			return s.toolchain.Prefix + "strip"
		}
		return "strip"
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

func (s *Scheduler) publishTarget(resolved *ResolvedTarget, pkgInfo *PkgInfo) error {
	t := resolved.Node.Target
	kind := t.Kind()

	if kind == api.TargetVoid || kind == api.TargetObject || t.IsTest() {
		return nil
	}

	libDir := filepath.Join(pkgInfo.InstallDir, "lib")
	includeDir := filepath.Join(pkgInfo.InstallDir, "include")

	if resolved.OutputPath != "" {
		srcPath := resolveWorkPath(pkgInfo.SourceDir, resolved.OutputPath)
		dest := filepath.Join(libDir, filepath.Base(resolved.OutputPath))
		if info, err := os.Stat(dest); err == nil {
			srcInfo, err2 := os.Stat(srcPath)
			if err2 == nil && info.Size() == srcInfo.Size() && !info.ModTime().Before(srcInfo.ModTime()) {
				vlog.Info("  SKIP (already published)")
				return nil
			}
		}
	}

	if err := os.MkdirAll(libDir, 0755); err != nil {
		return fmt.Errorf("create lib dir: %w", err)
	}

	if resolved.OutputPath != "" {
		srcPath := resolveWorkPath(pkgInfo.SourceDir, resolved.OutputPath)
		if _, err := os.Stat(srcPath); err == nil {
			dest := filepath.Join(libDir, filepath.Base(resolved.OutputPath))
			vlog.Info("  INSTALL %s -> %s", filepath.Base(resolved.OutputPath), dest)
			if err := CopyFile(srcPath, dest); err != nil {
				return fmt.Errorf("install library failed: %w", err)
			}
		}
	}

	if err := os.MkdirAll(includeDir, 0755); err != nil {
		return fmt.Errorf("create include dir: %w", err)
	}

	srcDir := s.effectiveSourceDir(resolved.Node.PkgName)
	return copyPublicIncludes(t, srcDir, includeDir)
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
