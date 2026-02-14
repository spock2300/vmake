package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	pkgs      map[string]*PkgInfo
	origDir   string
}

func NewScheduler(
	graph *BuildGraph,
	tc *toolchain.Toolchain,
	pkgDirs map[string]string,
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

	s := &Scheduler{
		graph:     graph,
		compiler:  compiler,
		linker:    linker,
		toolchain: tc,
		tcName:    tcName,
		pkgs:      make(map[string]*PkgInfo),
		origDir:   origDir,
	}

	for pkgName, pkgDir := range pkgDirs {
		if err := os.Chdir(pkgDir); err != nil {
			os.Chdir(origDir)
			return nil, fmt.Errorf("failed to chdir to %s: %w", pkgDir, err)
		}

		cache, err := LoadCache(tcName)
		if err != nil {
			cache = NewBuildCache(tc)
		}

		if cache.NeedFullRebuild(tc) {
			CleanObjects(tcName)
			cache = NewBuildCache(tc)
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
	return nil
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

	vlog.Info("[%s]", fullName)

	resolved, err := s.resolveTarget(node)
	if err != nil {
		return err
	}

	os.MkdirAll(fmt.Sprintf("build/%s/objects", s.tcName), 0755)

	var objs []string
	for _, src := range resolved.SourceFiles {
		objPath, deps, err := s.compileSource(resolved, src)
		if err != nil {
			return err
		}
		objs = append(objs, objPath)
		_ = deps
	}

	if err := s.link(resolved, objs); err != nil {
		return err
	}

	if node.Target.Kind() == api.TargetObject && len(resolved.SourceFiles) == 1 {
		if cachedSrc := pkgInfo.Cache.Sources[resolved.SourceFiles[0]]; cachedSrc != nil {
			cachedSrc.ObjPath = resolved.OutputPath
		}
	}

	return pkgInfo.Cache.Save(s.tcName)
}

func (s *Scheduler) resolveTarget(node *BuildNode) (*ResolvedTarget, error) {
	resolved := &ResolvedTarget{
		Node:        node,
		AllDefines:  append([]string{}, node.Target.Defines()...),
		AllCFlags:   append([]string{}, s.toolchain.DefaultFlags.CFlags...),
		AllCxxFlags: append([]string{}, s.toolchain.DefaultFlags.CxxFlags...),
		AllLdFlags:  append([]string{}, s.toolchain.DefaultFlags.LdFlags...),
	}

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

	return filepath.Join("build", s.tcName, name)
}

func (s *Scheduler) compileSource(resolved *ResolvedTarget, src string) (string, []string, error) {
	pkgInfo := s.pkgs[resolved.Node.PkgName]

	objRel := fmt.Sprintf("build/%s/objects/%s.o", s.tcName, strings.ReplaceAll(src, "/", "_"))

	if !pkgInfo.Cache.NeedRebuild(src) {
		cachedSrc := pkgInfo.Cache.Sources[src]
		if cachedSrc != nil {
			return cachedSrc.ObjPath, cachedSrc.Deps, nil
		}
	}

	vlog.InfoNormal("  CC %s", src)

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
		vlog.InfoNormal("  LINK %s", outputName)
		allObjs := append([]string{}, objs...)
		for _, artifact := range resolved.DepArtifacts {
			allObjs = append(allObjs, artifact)
		}
		links := unique(resolved.Node.Target.Links())
		return s.linker.LinkBinary(allObjs, links, resolved.AllLdFlags, resolved.OutputPath)
	case api.TargetStatic:
		vlog.InfoNormal("  AR %s", outputName)
		allObjs := append([]string{}, objs...)
		for _, artifact := range resolved.DepArtifacts {
			allObjs = append(allObjs, artifact)
		}
		return s.linker.LinkStatic(allObjs, resolved.OutputPath)
	case api.TargetShared:
		vlog.InfoNormal("  LINK %s", outputName)
		return s.linker.LinkShared(objs, resolved.AllLdFlags, resolved.OutputPath)
	case api.TargetObject:
		if len(objs) == 1 {
			if objs[0] == resolved.OutputPath {
				return nil
			}
			return os.Rename(objs[0], resolved.OutputPath)
		}
		return fmt.Errorf("object target requires exactly one source file")
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
