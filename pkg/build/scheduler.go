package build

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

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
	Dir        string
	OutputDir  string
	InstallDir string
}

type PackageProvider interface {
	GetInstalledPackage(name string) *api.InstalledPackage
	GetTransitivePackageNames(name string) []string
}

type Scheduler struct {
	graph       *BuildGraph
	compiler    *Compiler
	linker      *Linker
	toolchain   *toolchain.Toolchain
	tcName      string
	mode        string
	buildDir    string
	pkgs        map[string]*PkgInfo
	origDir     string
	ccWriter    *CompileCommandsWriter
	pkgProvider PackageProvider
	packages    map[string]*api.Package
	state       *BuildState
	stateDir    string
}

func NewScheduler(
	graph *BuildGraph,
	tc *toolchain.Toolchain,
	pkgDirs map[string]string,
	mode string,
) (*Scheduler, error) {
	compiler, err := NewCompiler(tc)
	if err != nil {
		return nil, err
	}

	linker, err := NewLinker(tc)
	if err != nil {
		return nil, err
	}

	origDir, _ := os.Getwd()

	tcName := tc.Name
	if mode == "" {
		mode = api.ModeDebug
	}
	buildDir := fmt.Sprintf("%s-%s", tcName, mode)

	ccWriter, err := NewCompileCommandsWriter(tc)
	if err != nil {
		return nil, err
	}

	state, err := LoadState(buildDir)
	if err != nil {
		state = NewBuildState(tc)
		state.Mode = mode
	}
	if state.NeedFullRebuild(tc) || state.Mode != mode {
		CleanObjects(buildDir)
		state = NewBuildState(tc)
		state.Mode = mode
	}

	s := &Scheduler{
		graph:     graph,
		compiler:  compiler,
		linker:    linker,
		toolchain: tc,
		tcName:    tcName,
		mode:      mode,
		buildDir:  buildDir,
		pkgs:      make(map[string]*PkgInfo),
		origDir:   origDir,
		ccWriter:  ccWriter,
		packages:  make(map[string]*api.Package),
		state:     state,
		stateDir:  buildDir,
	}

	for pkgName, pkgDir := range pkgDirs {
		s.pkgs[pkgName] = &PkgInfo{Dir: pkgDir}
	}

	return s, nil
}

func (s *Scheduler) SetPackageProvider(provider PackageProvider) {
	s.pkgProvider = provider
}

func (s *Scheduler) SetPackage(pkgName string, pkg *api.Package) {
	s.packages[pkgName] = pkg
}

func (s *Scheduler) SetPkgDirs(pkgName, sourceDir, outputDir, installDir string) {
	s.pkgs[pkgName] = &PkgInfo{
		Dir:        sourceDir,
		OutputDir:  outputDir,
		InstallDir: installDir,
	}
}

func (s *Scheduler) BuildAll() error {
	for _, fullName := range s.graph.Order {
		node := s.graph.Nodes[fullName]
		if node == nil {
			return fmt.Errorf("target not found in graph: %s", fullName)
		}

		if !node.Target.IsDefault() {
			continue
		}

		if err := s.Build(fullName); err != nil {
			return err
		}
	}

	return s.ccWriter.Save(filepath.Join("build", "compile_commands.json"))
}

func (s *Scheduler) Build(fullName string) error {
	node := s.graph.Nodes[fullName]
	if node == nil {
		return fmt.Errorf("target not found: %s", fullName)
	}

	if !node.Target.IsDefault() {
		return nil
	}

	pkgInfo := s.pkgs[node.PkgName]

	if err := os.Chdir(pkgInfo.Dir); err != nil {
		return err
	}
	defer os.Chdir(s.origDir)

	s.ccWriter.SetPackageDir(pkgInfo.Dir)

	vlog.Info("[%s]", fullName)

	resolved, err := s.resolveTarget(node)
	if err != nil {
		return err
	}

	os.MkdirAll(filepath.Join("build", s.buildDir, "objects"), 0755)

	numFiles := len(resolved.SourceFiles)
	if numFiles == 0 {
		return s.link(resolved, nil)
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

	if pkgInfo.InstallDir != "" {
		if err := s.installTarget(resolved, pkgInfo); err != nil {
			return err
		}
	}

	return s.state.Save(s.stateDir)
}

func (s *Scheduler) compileWorker(resolved *ResolvedTarget, jobs <-chan string, results chan<- compileResult, wg *sync.WaitGroup) {
	defer wg.Done()
	for src := range jobs {
		objPath, deps, err := s.compileSource(resolved, src)
		results <- compileResult{src: src, objPath: objPath, deps: deps, err: err}
	}
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

	for _, depName := range node.Deps {
		depNode := s.graph.Nodes[depName]
		if depNode == nil {
			return nil, fmt.Errorf("dependency not found: %s", depName)
		}

		depPkg := s.pkgs[depNode.PkgName]
		for _, pubInc := range depNode.Target.PublicIncludes() {
			absInc := filepath.Join(depPkg.Dir, pubInc)
			resolved.AllIncludes = append(resolved.AllIncludes, absInc)
		}

		if depNode.Target.Kind() != api.TargetVoid {
			var depOutput string
			if depPkg.InstallDir != "" {
				depOutput = filepath.Join(depPkg.InstallDir, "lib", targetFilename(depNode.Target.Kind(), depNode.Target.Name()))
			} else {
				depOutput = filepath.Join(depPkg.Dir, s.getTargetOutputPath(depNode))
			}
			resolved.DepArtifacts = append(resolved.DepArtifacts, depOutput)
		}
	}

	if s.pkgProvider != nil {
		for _, pkgRef := range node.Target.Packages() {
			allPkgNames := s.pkgProvider.GetTransitivePackageNames(pkgRef)
			for _, name := range allPkgNames {
				pkg := s.pkgProvider.GetInstalledPackage(name)
				if pkg != nil {
					resolved.AllIncludes = append(resolved.AllIncludes, pkg.IncludeDir)
					resolved.AllLdFlags = append(resolved.AllLdFlags, "-L"+pkg.LibDir)
					if len(pkg.Libs) > 0 {
						for _, lib := range pkg.Libs {
							resolved.AllLdFlags = append(resolved.AllLdFlags, "-l"+lib)
						}
					} else {
						parts := strings.Split(name, "/")
						libName := parts[len(parts)-1]
						resolved.AllLdFlags = append(resolved.AllLdFlags, "-l"+libName)
					}
				}
				// Also include deps that the build function actually used
				if pkg := s.packages[name]; pkg != nil {
					for _, dep := range pkg.Deps() {
						resolved.AllIncludes = append(resolved.AllIncludes, dep.IncludeDir)
						resolved.AllLdFlags = append(resolved.AllLdFlags, "-L"+dep.LibDir)
						for _, lib := range dep.Libs {
							resolved.AllLdFlags = append(resolved.AllLdFlags, "-l"+lib)
						}
					}
				}
			}
		}
	}

	for _, pattern := range node.Target.Files() {
		files, err := glob.Match(pattern, ".")
		if err != nil {
			return nil, err
		}
		resolved.SourceFiles = append(resolved.SourceFiles, files...)
	}

	resolved.OutputPath = s.getTargetOutputPath(node)

	resolved.AllDefines = unique(resolved.AllDefines)
	resolved.AllIncludes = unique(resolved.AllIncludes)
	resolved.AllCFlags = unique(resolved.AllCFlags)
	resolved.AllCxxFlags = unique(resolved.AllCxxFlags)
	resolved.AllLdFlags = unique(resolved.AllLdFlags)

	return resolved, nil
}

func targetFilename(kind api.TargetKind, name string) string {
	switch kind {
	case api.TargetStatic:
		return "lib" + name + ".a"
	case api.TargetShared:
		return "lib" + name + ".so"
	case api.TargetObject:
		return name + ".o"
	default:
		return name
	}
}

func (s *Scheduler) getTargetOutputPath(node *BuildNode) string {
	pkgInfo := s.pkgs[node.PkgName]

	name := targetFilename(node.Target.Kind(), node.Target.Name())

	if pkgInfo.OutputDir != "" {
		return filepath.Join(pkgInfo.OutputDir, name)
	}
	return filepath.Join("build", s.buildDir, name)
}

func (s *Scheduler) compileSource(resolved *ResolvedTarget, src string) (string, []string, error) {
	pkgInfo := s.pkgs[resolved.Node.PkgName]

	var objRel string
	if pkgInfo.OutputDir != "" {
		objRel = filepath.Join(pkgInfo.OutputDir, "objects", strings.ReplaceAll(src, "/", "_")+".o")
	} else {
		objRel = filepath.Join("build", s.buildDir, "objects", strings.ReplaceAll(src, "/", "_")+".o")
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
		Mode:     s.mode,
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

func (s *Scheduler) link(resolved *ResolvedTarget, objs []string) error {
	kind := resolved.Node.Target.Kind()

	if !s.needRelink(resolved, objs) {
		return nil
	}

	outputName := filepath.Base(resolved.OutputPath)

	switch kind {
	case api.TargetBinary:
		vlog.Info("  LINK %s", outputName)
		allObjs := append([]string{}, objs...)
		for _, artifact := range resolved.DepArtifacts {
			allObjs = append(allObjs, artifact)
		}
		links := unique(resolved.Node.Target.Links())
		return s.linker.LinkBinary(allObjs, links, resolved.AllLdFlags, resolved.OutputPath)
	case api.TargetStatic:
		vlog.Info("  AR %s", outputName)
		allObjs := append([]string{}, objs...)
		for _, artifact := range resolved.DepArtifacts {
			allObjs = append(allObjs, artifact)
		}
		return s.linker.LinkStatic(allObjs, resolved.OutputPath)
	case api.TargetShared:
		vlog.Info("  LINK %s", outputName)
		allObjs := append([]string{}, objs...)
		for _, artifact := range resolved.DepArtifacts {
			allObjs = append(allObjs, artifact)
		}
		return s.linker.LinkShared(allObjs, resolved.AllLdFlags, resolved.OutputPath)
	case api.TargetObject:
		vlog.Info("  LD -r %s", outputName)
		allObjs := append([]string{}, objs...)
		for _, artifact := range resolved.DepArtifacts {
			allObjs = append(allObjs, artifact)
		}
		if len(allObjs) == 0 {
			return fmt.Errorf("object target requires at least one source file")
		}
		return s.linker.LinkObject(allObjs, resolved.OutputPath)
	case api.TargetVoid:
		if fn := resolved.Node.Target.BuildFunc(); fn != nil {
			pkg := s.packages[resolved.Node.PkgName]
			if pkg == nil {
				return fmt.Errorf("no Package for TargetVoid target %s", resolved.Node.FullName)
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
				os.MkdirAll(pkg.BuildDir(), 0755)
				os.MkdirAll(pkg.InstallDir(), 0755)
			}
			if err := fn(pkg); err != nil {
				return err
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
		return nil
	}
	return nil
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

func (s *Scheduler) installTarget(resolved *ResolvedTarget, pkgInfo *PkgInfo) error {
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
				vlog.Info("  SKIP (already installed)")
				return nil
			}
		}
	}

	os.MkdirAll(libDir, 0755)
	os.MkdirAll(includeDir, 0755)

	if resolved.OutputPath != "" {
		if _, err := os.Stat(resolved.OutputPath); err == nil {
			dest := filepath.Join(libDir, filepath.Base(resolved.OutputPath))
			vlog.Info("  INSTALL %s -> %s", filepath.Base(resolved.OutputPath), dest)
			if err := CopyFile(resolved.OutputPath, dest); err != nil {
				return fmt.Errorf("install library failed: %w", err)
			}
		}
	}

	for _, inc := range t.PublicIncludes() {
		srcPath := filepath.Join(pkgInfo.Dir, inc)
		if info, err := os.Stat(srcPath); err == nil {
			if info.IsDir() {
				vlog.Info("  INSTALL DIR %s -> %s", inc, includeDir)
				if err := CopyDir(srcPath, includeDir); err != nil {
					return fmt.Errorf("install headers failed: %w", err)
				}
			} else {
				dest := filepath.Join(includeDir, filepath.Base(srcPath))
				vlog.Info("  INSTALL %s -> %s", filepath.Base(srcPath), dest)
				if err := CopyFile(srcPath, dest); err != nil {
					return fmt.Errorf("install header failed: %w", err)
				}
			}
		}
	}

	return nil
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
