package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/internal/glob"
	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type ResolvedTarget struct {
	Node         *BuildNode
	BuildDir     string
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
	BuildDir string
	Cache    *BuildCache
}

type Scheduler struct {
	graph     *BuildGraph
	compiler  *Compiler
	linker    *Linker
	toolchain *toolchain.Toolchain
	pkgInfos  map[string]*PkgInfo
}

func NewScheduler(
	graph *BuildGraph,
	tc *toolchain.Toolchain,
	pkgBuildDirs map[string]string,
) (*Scheduler, error) {
	compiler, err := NewCompiler(tc)
	if err != nil {
		return nil, err
	}

	linker, err := NewLinker(tc)
	if err != nil {
		return nil, err
	}

	s := &Scheduler{
		graph:     graph,
		compiler:  compiler,
		linker:    linker,
		toolchain: tc,
		pkgInfos:  make(map[string]*PkgInfo),
	}

	for pkgName, buildDir := range pkgBuildDirs {
		cache, err := LoadCache(buildDir)
		if err != nil {
			cache = NewBuildCache(tc)
		}

		if cache.NeedFullRebuild(tc) {
			CleanObjects(buildDir)
			cache = NewBuildCache(tc)
		}

		s.pkgInfos[pkgName] = &PkgInfo{
			BuildDir: buildDir,
			Cache:    cache,
		}
	}

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

	resolved, err := s.resolveTarget(node)
	if err != nil {
		return err
	}

	os.MkdirAll(filepath.Join(resolved.BuildDir, "objects"), 0755)

	var objs []string
	for _, src := range resolved.SourceFiles {
		objRel, deps, err := s.compileSource(resolved, src)
		if err != nil {
			return err
		}
		objs = append(objs, filepath.Join(resolved.BuildDir, objRel))
		_ = deps
	}

	if err := s.link(resolved, objs); err != nil {
		return err
	}

	pkgInfo := s.pkgInfos[node.PkgName]
	return pkgInfo.Cache.Save(pkgInfo.BuildDir)
}

func (s *Scheduler) resolveTarget(node *BuildNode) (*ResolvedTarget, error) {
	pkgInfo := s.pkgInfos[node.PkgName]
	srcDir := filepath.Dir(pkgInfo.BuildDir)

	resolved := &ResolvedTarget{
		Node:        node,
		BuildDir:    pkgInfo.BuildDir,
		AllDefines:  append([]string{}, node.Target.Defines()...),
		AllCFlags:   append([]string{}, s.toolchain.DefaultFlags.CFlags...),
		AllCxxFlags: append([]string{}, s.toolchain.DefaultFlags.CxxFlags...),
		AllLdFlags:  append([]string{}, s.toolchain.DefaultFlags.LdFlags...),
	}

	for _, inc := range node.Target.Includes() {
		absInc := inc
		if !filepath.IsAbs(inc) {
			absInc = filepath.Join(srcDir, inc)
		}
		resolved.AllIncludes = append(resolved.AllIncludes, absInc)
	}

	for _, pubInc := range node.Target.PublicIncludes() {
		absInc := pubInc
		if !filepath.IsAbs(pubInc) {
			absInc = filepath.Join(srcDir, pubInc)
		}
		resolved.AllIncludes = append(resolved.AllIncludes, absInc)
	}

	resolved.AllCFlags = append(resolved.AllCFlags, node.Target.CFlags()...)
	resolved.AllCxxFlags = append(resolved.AllCxxFlags, node.Target.CxxFlags()...)
	resolved.AllLdFlags = append(resolved.AllLdFlags, node.Target.LdFlags()...)

	for _, depName := range node.Deps {
		depNode := s.graph.Nodes[depName]
		if depNode == nil {
			return nil, fmt.Errorf("dependency not found: %s", depName)
		}

		depSrcDir := filepath.Dir(s.pkgInfos[depNode.PkgName].BuildDir)
		for _, pubInc := range depNode.Target.PublicIncludes() {
			absInc := pubInc
			if !filepath.IsAbs(pubInc) {
				absInc = filepath.Join(depSrcDir, pubInc)
			}
			resolved.AllIncludes = append(resolved.AllIncludes, absInc)
		}

		depOutput := s.getTargetOutputPath(depNode)
		resolved.DepArtifacts = append(resolved.DepArtifacts, depOutput)
	}

	for _, pattern := range node.Target.Files() {
		files, err := glob.Match(pattern, srcDir)
		if err != nil {
			return nil, err
		}
		resolved.SourceFiles = append(resolved.SourceFiles, files...)
	}

	resolved.OutputPath = s.getTargetOutputPath(node)

	return resolved, nil
}

func (s *Scheduler) getTargetOutputPath(node *BuildNode) string {
	pkgInfo := s.pkgInfos[node.PkgName]

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

	return filepath.Join(pkgInfo.BuildDir, name)
}

func (s *Scheduler) compileSource(resolved *ResolvedTarget, src string) (string, []string, error) {
	pkgInfo := s.pkgInfos[resolved.Node.PkgName]

	srcRel, _ := filepath.Rel(filepath.Dir(resolved.BuildDir), src)
	objRel := "objects/" + strings.ReplaceAll(srcRel, "/", "_") + ".o"
	objPath := filepath.Join(resolved.BuildDir, objRel)

	if !pkgInfo.Cache.NeedRebuild(src) {
		cachedSrc := pkgInfo.Cache.Sources[src]
		if cachedSrc != nil {
			return objRel, cachedSrc.Deps, nil
		}
	}

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

	deps, err := s.compiler.Compile(src, objPath, opts)
	if err != nil {
		return "", nil, err
	}

	pkgInfo.Cache.Update(src, objRel, deps)

	return objRel, deps, nil
}

func (s *Scheduler) link(resolved *ResolvedTarget, objs []string) error {
	switch resolved.Node.Target.Kind() {
	case api.TargetBinary:
		allObjs := append([]string{}, objs...)
		for _, artifact := range resolved.DepArtifacts {
			allObjs = append(allObjs, artifact)
		}
		return s.linker.LinkBinary(allObjs, resolved.Node.Target.Links(),
			resolved.AllLdFlags, resolved.OutputPath)
	case api.TargetStatic:
		return s.linker.LinkStatic(objs, resolved.OutputPath)
	case api.TargetShared:
		return s.linker.LinkShared(objs, resolved.AllLdFlags, resolved.OutputPath)
	case api.TargetObject:
		if len(objs) == 1 {
			return os.Rename(objs[0], resolved.OutputPath)
		}
		return fmt.Errorf("object target requires exactly one source file")
	}
	return nil
}
