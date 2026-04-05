package build

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	iexec "gitee.com/spock2300/vmake/internal/exec"
	"gitee.com/spock2300/vmake/internal/fs"
	"gitee.com/spock2300/vmake/internal/glob"
	"gitee.com/spock2300/vmake/pkg/api"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type ResolvedTarget struct {
	Node         *BuildNode
	SourceFiles  []string
	AllIncludes  []string
	AllDefines   []string
	AllCFlags    []string
	AllCxxFlags  []string
	AllLdFlags   []string
	DepArtifacts []string
	OutputPath   string
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

type Scheduler struct {
	graph             *BuildGraph
	compiler          *Compiler
	linker            *Linker
	toolchain         *toolchain.Toolchain
	resolvedTools     *ResolvedTools
	tcName            string
	mode              string
	pkgs              map[string]*PkgInfo
	origDir           string
	ccWriter          *CompileCommandsWriter
	packages          map[string]*api.Package
	buildKeyOverrides map[string]string
}

func NewScheduler(
	graph *BuildGraph,
	tc *toolchain.Toolchain,
	pkgDirs map[string]*api.PkgDirs,
	mode string,
	pkgOptions map[string]map[string]any,
	buildKeyOverrides map[string]string,
) (*Scheduler, error) {
	tools, err := ResolveTools(tc)
	if err != nil {
		return nil, err
	}

	compiler := NewCompiler(tools)
	linker := NewLinker(tools)

	origDir, _ := os.Getwd()

	tcName := tc.Name
	if mode == "" {
		mode = api.ModeDebug
	}

	ccWriter := NewCompileCommandsWriter(tools)

	s := &Scheduler{
		graph:             graph,
		compiler:          compiler,
		linker:            linker,
		toolchain:         tc,
		resolvedTools:     tools,
		tcName:            tcName,
		mode:              mode,
		pkgs:              make(map[string]*PkgInfo),
		origDir:           origDir,
		ccWriter:          ccWriter,
		packages:          make(map[string]*api.Package),
		buildKeyOverrides: buildKeyOverrides,
	}

	for pkgName, pd := range pkgDirs {
		buildKey := BuildKey(tools.CC, mode, pkgOptions[pkgName])
		if override, ok := buildKeyOverrides[pkgName]; ok {
			buildKey = override
		}
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

func (s *Scheduler) BuildAll() error {
	if err := s.graph.ForEachDefault(func(node *BuildNode) error {
		return s.Build(node.FullName)
	}); err != nil {
		return err
	}
	return s.ccWriter.Save(filepath.Join("build", "compile_commands.json"))
}

func (s *Scheduler) Build(fullName string) error {
	node, err := s.graph.GetNode(fullName)
	if err != nil {
		return err
	}

	if !node.Target.IsDefault() {
		return nil
	}

	pkgInfo := s.pkgs[node.PkgName]

	if err := os.Chdir(pkgInfo.SourceDir); err != nil {
		return err
	}
	defer os.Chdir(s.origDir)

	s.ccWriter.SetPackageDir(pkgInfo.SourceDir)

	vlog.Info("[%s]", fullName)

	resolved, err := s.resolveTarget(node)
	if err != nil {
		return err
	}

	genRules := node.Target.GenRules()
	if len(genRules) > 0 {
		generatedDir := BuildPath(".", pkgInfo.BuildKey, "generated")
		if err := runGenRules(genRules, generatedDir); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(BuildPath(".", pkgInfo.BuildKey, "objects"), 0755); err != nil {
		return fmt.Errorf("create build directory: %w", err)
	}

	numFiles := len(resolved.SourceFiles)
	if numFiles == 0 {
		if err := s.link(resolved, nil); err != nil {
			return err
		}
		if err := s.postLink(resolved); err != nil {
			return err
		}
		if pkgInfo.InstallDir != "" {
			return s.publishTarget(resolved, pkgInfo)
		}
		return nil
	}

	numWorkers := runtime.NumCPU()
	if numWorkers > numFiles {
		numWorkers = numFiles
	}

	jobs := make(chan string, numFiles)
	results := make(chan compileResult, numFiles)

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go s.compileWorker(resolved, jobs, results, &wg)
	}

	for _, src := range resolved.SourceFiles {
		jobs <- src
	}
	close(jobs)

	wg.Wait()
	close(results)

	objs := make([]string, 0, numFiles)
	for r := range results {
		if r.err != nil {
			return r.err
		}
		objs = append(objs, r.objPath)
	}

	if err := s.link(resolved, objs); err != nil {
		return err
	}

	if err := s.postLink(resolved); err != nil {
		return err
	}

	if pkgInfo.InstallDir != "" {
		if err := s.publishTarget(resolved, pkgInfo); err != nil {
			return err
		}
	}

	return nil
}

func (s *Scheduler) compileWorker(resolved *ResolvedTarget, jobs <-chan string, results chan<- compileResult, wg *sync.WaitGroup) {
	defer wg.Done()
	for src := range jobs {
		objPath, deps, err := s.compileSource(resolved, src)
		results <- compileResult{src: src, objPath: objPath, deps: deps, err: err}
	}
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
			srcDir := depPkg.SourceDir
			if dep := s.packages[depNode.PkgName]; dep != nil && dep.SrcDir() != "" {
				srcDir = dep.SrcDir()
			}
			for _, pubInc := range depNode.Target.PublicIncludes() {
				result.includes = append(result.includes, filepath.Join(srcDir, pubInc))
			}
		} else if depPkg.InstallDir != "" {
			result.includes = append(result.includes, filepath.Join(depPkg.InstallDir, "include"))
		}

		if depNode.Target.Kind() == api.TargetVoid && depPkg.InstallDir != "" {
			libDir := fs.DetectLibDir(depPkg.InstallDir)
			result.voidLdFlags = append(result.voidLdFlags, "-L"+libDir)
			if depPkg := s.packages[depNode.PkgName]; depPkg != nil && len(depPkg.Libs()) > 0 {
				for _, lib := range depPkg.Libs() {
					result.voidLdFlags = append(result.voidLdFlags, "-l"+lib)
				}
			} else {
				parts := strings.Split(depNode.PkgName, "/")
				result.voidLdFlags = append(result.voidLdFlags, "-l"+parts[len(parts)-1])
			}
		} else if depNode.Target.Kind() != api.TargetVoid {
			var depOutput string
			if depPkg.InstallDir != "" && depPkg.OutputDir == "" {
				depOutput = filepath.Join(depPkg.InstallDir, "lib", targetFilename(depNode.Target.Kind(), depNode.Target.Name()))
			} else {
				depOutput = s.getTargetOutputPath(depNode)
			}
			result.artifacts = append(result.artifacts, depOutput)

			if pkg := s.packages[depNode.PkgName]; pkg != nil {
				for _, lib := range pkg.Libs() {
					if lib != depNode.Target.Name() {
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
		AllCFlags:   append([]string{}, s.toolchain.DefaultFlags.CFlags...),
		AllCxxFlags: append([]string{}, s.toolchain.DefaultFlags.CxxFlags...),
		AllLdFlags:  append([]string{}, s.toolchain.DefaultFlags.LdFlags...),
	}

	resolved.AllCFlags = append(resolved.AllCFlags, modeFlags...)
	resolved.AllCxxFlags = append(resolved.AllCxxFlags, modeFlags...)
	resolved.AllDefines = append(resolved.AllDefines, modeDefines...)

	for _, inc := range node.Target.Includes() {
		resolved.AllIncludes = append(resolved.AllIncludes, inc)
	}

	for _, pubInc := range node.Target.PublicIncludes() {
		resolved.AllIncludes = append(resolved.AllIncludes, pubInc)
	}

	resolved.AllCFlags = append(resolved.AllCFlags, node.Target.CFlags()...)
	resolved.AllCxxFlags = append(resolved.AllCxxFlags, node.Target.CxxFlags()...)
	resolved.AllLdFlags = append(resolved.AllLdFlags, node.Target.LdFlags()...)

	deps, err := s.collectDepArtifacts(node)
	if err != nil {
		return nil, err
	}
	resolved.AllIncludes = append(resolved.AllIncludes, deps.includes...)
	resolved.DepArtifacts = deps.artifacts

	if len(deps.voidLdFlags) > 0 {
		resolved.AllLdFlags = append(resolved.AllLdFlags, "-Wl,--start-group")
		resolved.AllLdFlags = append(resolved.AllLdFlags, deps.voidLdFlags...)
		resolved.AllLdFlags = append(resolved.AllLdFlags, "-Wl,--end-group")
	}

	for _, pattern := range node.Target.Files() {
		files, err := glob.Match(pattern, ".")
		if err != nil {
			return nil, err
		}
		resolved.SourceFiles = append(resolved.SourceFiles, files...)
	}

	resolved.OutputPath = s.getTargetOutputPath(node)

	pkgInfo := s.pkgs[node.PkgName]
	genRules := node.Target.GenRules()
	if len(genRules) > 0 {
		generatedDir := BuildPath(".", pkgInfo.BuildKey, "generated")
		resolved.AllIncludes = append(resolved.AllIncludes, generatedDir)
	}

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

func collectAllObjects(objs []string, artifacts []string) []string {
	allObjs := append([]string{}, objs...)
	for _, artifact := range artifacts {
		allObjs = append(allObjs, artifact)
	}
	return allObjs
}

func (s *Scheduler) getTargetOutputPath(node *BuildNode) string {
	pkgInfo := s.pkgs[node.PkgName]

	name := targetFilename(node.Target.Kind(), node.Target.Name())

	if pkgInfo.OutputDir != "" {
		return filepath.Join(pkgInfo.OutputDir, name)
	}
	return BuildPath(".", pkgInfo.BuildKey, name)
}

func (s *Scheduler) compileSource(resolved *ResolvedTarget, src string) (string, []string, error) {
	pkgInfo := s.pkgs[resolved.Node.PkgName]

	var objRel string
	if pkgInfo.OutputDir != "" {
		objRel = filepath.Join(pkgInfo.OutputDir, "objects", strings.ReplaceAll(src, "/", "_")+".o")
	} else {
		objRel = BuildPath(".", pkgInfo.BuildKey, "objects/"+strings.ReplaceAll(src, "/", "_")+".o")
	}

	valid, deps := IsSourceValid(src, objRel)
	if valid {
		return objRel, deps, nil
	}

	vlog.Info("  CC %s", src)

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

	deps, err := s.compiler.Compile(src, objRel, opts)
	if err != nil {
		return "", nil, err
	}

	return objRel, deps, nil
}

func (s *Scheduler) needRelink(resolved *ResolvedTarget, objs []string) bool {
	outputInfo, err := os.Stat(resolved.OutputPath)
	if err != nil {
		return true
	}

	outputTime := outputInfo.ModTime()

	for _, obj := range objs {
		objInfo, err := os.Stat(obj)
		if err != nil || objInfo.ModTime().After(outputTime) {
			return true
		}
	}

	for _, artifact := range resolved.DepArtifacts {
		artifactInfo, err := os.Stat(artifact)
		if err != nil || artifactInfo.ModTime().After(outputTime) {
			return true
		}
	}

	return false
}

func (s *Scheduler) buildVoidTarget(resolved *ResolvedTarget) error {
	fn := resolved.Node.Target.BuildFunc()
	if fn == nil {
		return nil
	}

	pkg := s.packages[resolved.Node.PkgName]
	if pkg == nil {
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
		s.packages[resolved.Node.PkgName] = pkg
	}
	s.populateDepsFromGraph(pkg, resolved.Node)

	if pkg.InstallDir() != "" {
		if info, err := os.Stat(pkg.InstallDir()); err == nil && info.IsDir() {
			entries, _ := os.ReadDir(pkg.InstallDir())
			if len(entries) > 0 {
				vlog.Info("  SKIP (already installed)")
				return nil
			}
		}
		if err := os.MkdirAll(pkg.InstallDir(), 0755); err != nil {
			return fmt.Errorf("create install dir: %w", err)
		}
	} else if pkg.BuildDir() != "" {
		stampPath := filepath.Join(pkg.BuildDir(), ".vmake_stamp")
		if si, err := os.Stat(stampPath); err == nil {
			stale := false
			for _, cf := range pkg.ConfigFiles() {
				cp := filepath.Join(pkg.SrcDir(), cf)
				ci, err := os.Stat(cp)
				if err != nil {
					stale = true
					break
				}
				if ci.ModTime().After(si.ModTime()) {
					stale = true
					break
				}
			}
			if !stale {
				vlog.Info("  SKIP (already built)")
				return nil
			}
		}
	}

	if err := os.MkdirAll(pkg.BuildDir(), 0755); err != nil {
		return fmt.Errorf("create build dir: %s: %w", pkg.BuildDir(), err)
	}

	if err := fn(pkg); err != nil {
		return err
	}

	if pkg.InstallDir() == "" && pkg.BuildDir() != "" {
		stampPath := filepath.Join(pkg.BuildDir(), ".vmake_stamp")
		os.WriteFile(stampPath, []byte{}, 0644)
	}

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
	return nil
}

func (s *Scheduler) link(resolved *ResolvedTarget, objs []string) error {
	kind := resolved.Node.Target.Kind()

	if !s.needRelink(resolved, objs) {
		return nil
	}

	outputName := filepath.Base(resolved.OutputPath)

	allObjs := collectAllObjects(objs, resolved.DepArtifacts)

	switch kind {
	case api.TargetBinary:
		vlog.Info("  LINK %s", outputName)
		return s.linker.LinkBinary(allObjs, unique(resolved.Node.Target.Links()), resolved.AllLdFlags, resolved.OutputPath, resolved.Node.Target.LinkerScript())
	case api.TargetStatic:
		vlog.Info("  AR %s", outputName)
		return s.linker.LinkStatic(allObjs, resolved.OutputPath)
	case api.TargetShared:
		vlog.Info("  LINK %s", outputName)
		return s.linker.LinkShared(allObjs, resolved.AllLdFlags, resolved.OutputPath)
	case api.TargetObject:
		vlog.Info("  LD -r %s", outputName)
		if len(allObjs) == 0 {
			return fmt.Errorf("object target requires at least one source file")
		}
		return s.linker.LinkObject(allObjs, resolved.OutputPath)
	case api.TargetVoid:
		return s.buildVoidTarget(resolved)
	}
	return nil
}

func (s *Scheduler) postLink(resolved *ResolvedTarget) error {
	steps := resolved.Node.Target.PostLinkSteps()
	if len(steps) == 0 {
		return nil
	}

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
		if _, err := iexec.Run(tool, args...); err != nil {
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
		if depPkg := s.packages[depPkgName]; depPkg != nil {
			if depPkg.Libs() != nil {
				depLibs = depPkg.Libs()
			}
		}
		ip := api.NewInstalledPackage(depPkgName, "", pkgInfo.InstallDir, depLibs)
		pkg.SetDep(depPkgName, ip)
	}
}

func (s *Scheduler) publishTarget(resolved *ResolvedTarget, pkgInfo *PkgInfo) error {
	t := resolved.Node.Target
	kind := t.Kind()

	if kind == api.TargetVoid || kind == api.TargetObject {
		return nil
	}

	libDir := filepath.Join(pkgInfo.InstallDir, "lib")
	includeDir := filepath.Join(pkgInfo.InstallDir, "include")

	if resolved.OutputPath != "" {
		dest := filepath.Join(libDir, filepath.Base(resolved.OutputPath))
		if info, err := os.Stat(dest); err == nil {
			srcInfo, err2 := os.Stat(resolved.OutputPath)
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
		if _, err := os.Stat(resolved.OutputPath); err == nil {
			dest := filepath.Join(libDir, filepath.Base(resolved.OutputPath))
			vlog.Info("  INSTALL %s -> %s", filepath.Base(resolved.OutputPath), dest)
			if err := CopyFile(resolved.OutputPath, dest); err != nil {
				return fmt.Errorf("install library failed: %w", err)
			}
		}
	}

	if err := os.MkdirAll(includeDir, 0755); err != nil {
		return fmt.Errorf("create include dir: %w", err)
	}

	srcDir := pkgInfo.SourceDir
	if pkg := s.packages[resolved.Node.PkgName]; pkg != nil && pkg.SrcDir() != "" {
		srcDir = pkg.SrcDir()
	}
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
