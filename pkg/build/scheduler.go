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
	Dir   string
	Cache *BuildCache
}

type Scheduler struct {
	graph     *BuildGraph
	compiler  *Compiler
	linker    *Linker
	toolchain *toolchain.Toolchain
	tcName    string
	mode      string
	buildDir  string
	pkgs      map[string]*PkgInfo
	origDir   string
	ccWriter  *CompileCommandsWriter
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
	}

	for pkgName, pkgDir := range pkgDirs {
		if err := os.Chdir(pkgDir); err != nil {
			os.Chdir(origDir)
			return nil, fmt.Errorf("failed to chdir to %s: %w", pkgDir, err)
		}

		cache, err := LoadCache(buildDir)
		if err != nil {
			cache = NewBuildCache(tc)
		}

		if cache.NeedFullRebuild(tc) || cache.Mode != mode {
			CleanObjects(buildDir)
			cache = NewBuildCache(tc)
			cache.Mode = mode
		}

		s.pkgs[pkgName] = &PkgInfo{
			Dir:   pkgDir,
			Cache: cache,
		}
	}

	os.Chdir(origDir)
	return s, nil
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

	return s.ccWriter.Save("build/compile_commands.json")
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

	os.MkdirAll(fmt.Sprintf("build/%s/objects", s.buildDir), 0755)

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

	return pkgInfo.Cache.Save(s.buildDir)
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

		depOutput := filepath.Join(depPkg.Dir, s.getTargetOutputPath(depNode))
		resolved.DepArtifacts = append(resolved.DepArtifacts, depOutput)
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

func (s *Scheduler) getTargetOutputPath(node *BuildNode) string {
	var name string
	switch node.Target.Kind() {
	case api.TargetBinary:
		name = node.Target.Name()
	case api.TargetStatic:
		name = "lib" + node.Target.Name() + ".a"
	case api.TargetShared:
		name = "lib" + node.Target.Name() + ".so"
	case api.TargetObject:
		name = node.Target.Name() + ".o"
	default:
		name = node.Target.Name()
	}

	return filepath.Join("build", s.buildDir, name)
}

func (s *Scheduler) compileSource(resolved *ResolvedTarget, src string) (string, []string, error) {
	pkgInfo := s.pkgs[resolved.Node.PkgName]

	objRel := fmt.Sprintf("build/%s/objects/%s.o", s.buildDir, strings.ReplaceAll(src, "/", "_"))

	if cached := pkgInfo.Cache.GetIfValid(src); cached != nil {
		return cached.ObjPath, cached.Deps, nil
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

	pkgInfo.Cache.Update(src, objRel, deps)

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
