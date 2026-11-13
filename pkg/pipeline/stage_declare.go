package pipeline

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spock2300/vmake/internal/pkgvis"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/resolver"
)

func (s *declareStage) executeOnBuild() error {
	vlog.Info("")
	vlog.Info("Executing OnBuild...")

	s.allTargets = make(map[string]map[string]*api.Target)
	s.buildCtxs = make(map[string]*api.BuildContext)

	s.pkgMetaMap = make(map[string]build.PkgBuildMeta)
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		node := s.rc.ctx.DepGraph.Packages[name]
		if !s.conf.needed[name] || node.Source == nil {
			continue
		}
		s.pkgMetaMap[name] = build.PkgBuildMeta{
			Origin:  node.Source.Origin,
			Linked:  node.Source.Link != nil,
			Deps:    node.Deps,
			VisPath: s.rc.ctx.Resolver.VisibilityPath(name),
		}
	}

	if err := s.executeMainPackages(s.conf.needed); err != nil {
		return err
	}

	return nil
}

func (s *declareStage) executeMainPackages(filter map[string]bool) error {
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		if s.rc.ctx.Context != nil && s.rc.ctx.Context.Err() != nil {
			return s.rc.ctx.Context.Err()
		}
		node := s.rc.ctx.DepGraph.Packages[name]
		if !filter[name] || node.Pkg == nil {
			continue
		}
		if s.declarations[name] == 2 {
			if s.scopeValues != nil {
				if _, err := s.bindPackage(name); err != nil {
					return err
				}
			}
			continue
		}
		if s.declarations[name] != 0 {
			return fmt.Errorf("recursive or failed OnBuild declaration: %s -> %s", strings.Join(s.declarationPath, " -> "), name)
		}
		if err := s.executeOnePackage(name, node); err != nil {
			return err
		}
	}
	return nil
}

func (s *declareStage) executeOnePackage(name string, node *resolver.PackageNode) (err error) {
	s.declarations[name] = 1
	s.declarationPath = append(s.declarationPath, name)
	defer func() {
		s.declarationPath = s.declarationPath[:len(s.declarationPath)-1]
		if err != nil {
			s.declarations[name] = 3
		}
	}()
	defer scriptcall.Recover(&err)
	bound, err := s.bindPackage(name)
	if err != nil {
		return err
	}
	buildCtx, err := newBuildContext(s.rc.ctx, name, bound.values)
	if err != nil {
		return err
	}
	lookupBase := s.scopeValues
	if lookupBase == nil {
		lookupBase = s.rc.cfg.GlobalValues
	}
	buildCtx.SetConfigLookup(newConfigLookup(s.rc.ctx, name, lookupBase, func(pkg string) (map[string]any, bool) {
		binding := s.bindings[pkg]
		if binding == nil {
			return nil, false
		}
		return binding.values, true
	}))
	platform := platformFromValues(buildCtx.CfgVals)
	buildCtx.SetBuildSubGraphFunc(func(pkgName string) error { return s.buildSubGraph(name, pkgName) })
	buildCtx.SetDepOutputFunc(func(depRef string) string { return s.computeDepOutput(name, depRef) })
	buildCtx.SetDepSrcDirFunc(func(depRef string) string { return s.computeDepSrcDir(name, depRef) })
	buildCtx.SetDepPackageDirFunc(func(depRef string) string { return s.computeDepPackageDir(name, depRef) })
	flags := api.DefaultBuildFlags(bound.toolchain.Name, platform.OSOrHost())
	buildCtx.SetDefaultFlags(flags.CFlags, flags.CxxFlags, flags.LdFlags)
	pkg := node.Pkg
	allOpts := s.rc.ctx.AllOptions[name]
	if allOpts == nil {
		allOpts = pkg.GetOptions()
	}
	pkg.SetDirs(*s.source.pkgDirs[name])
	pkg.SetOptions(allOpts)
	pkg.SetCfgVals(maps.Clone(buildCtx.CfgVals))
	pkg.SetToolchain(bound.toolchain)
	pkg.SetPlatform(platform)
	buildCtx.SetPackage(pkg)
	pkg.ExecBuildFuncs(s.source.pkgDirs[name].SourceDir, func(fn api.BuildFunc) { fn(buildCtx) })
	applyBuildContextConfig(buildCtx, node, s.rc.ctx, name, s.conf.needed)
	targets := make(map[string]*api.Target)
	for targetName, target := range buildCtx.GetTargets() {
		targets[targetName] = api.SnapshotTarget(target)
	}
	s.allTargets[name] = targets
	if rawSubs := buildCtx.SubGraphs(); len(rawSubs) > 0 {
		meta := s.pkgMetaMap[name]
		for _, sub := range rawSubs {
			resolvedSub := s.resolveSubPackageName(name, sub, func(candidate string) bool {
				_, ok := s.pkgMetaMap[candidate]
				return ok
			})
			if !slices.Contains(meta.SubGraphs, resolvedSub) {
				meta.SubGraphs = append(meta.SubGraphs, resolvedSub)
			}
		}
		s.pkgMetaMap[name] = meta
	}
	s.buildCtxs[name] = buildCtx
	s.declarations[name] = 2
	return nil
}

func (s *declareStage) resolveSubPackageName(currentPkg, name string, exists func(string) bool) string {
	if name == "" || strings.Contains(name, "/") {
		return name
	}
	var subParents map[string]string
	if s.rc.ctx.Resolver != nil {
		subParents = s.rc.ctx.Resolver.SubParents()
	}
	return api.ResolveSubPackageName(currentPkg, name, subParents, exists)
}

func (s *declareStage) buildSubGraph(currentPkg, rootPkg string) (err error) {
	rootPkg = s.resolveSubPackageName(currentPkg, rootPkg, func(candidate string) bool {
		_, ok := s.pkgMetaMap[candidate]
		return ok
	})
	if active := s.rc.session.ActiveTarget(); active != "" {
		return fmt.Errorf("BuildSubGraph(%s) is not allowed while target %s is running", rootPkg, active)
	}
	previous, completed := s.subGraphErrors[rootPkg]
	if s.subGraphBuilt[rootPkg] && !completed {
		return fmt.Errorf("recursive BuildSubGraph(%s)", rootPkg)
	}
	if !s.conf.needed[rootPkg] {
		return fmt.Errorf("subgraph package %s not found in required packages", rootPkg)
	}
	if currentPkg != "" && s.rc.ctx.Resolver != nil {
		if err := pkgvis.CheckInternal(
			currentPkg,
			s.rc.ctx.Resolver.VisibilityPath(currentPkg),
			rootPkg,
			s.rc.ctx.Resolver.VisibilityPath(rootPkg),
		); err != nil {
			return err
		}
	}
	previousScope := s.scopeValues
	base := previousScope
	if base == nil {
		base = s.rc.cfg.GlobalValues
	}
	values, err := s.packageBindingValues(rootPkg, base)
	if err != nil {
		return err
	}
	scope := maps.Clone(base)
	for _, key := range []string{api.ToolchainOptionName, api.TargetOSOptionName, api.TargetTripleOptionName} {
		scope[key] = values[key]
	}
	request, err := s.subGraphRequestSignature(rootPkg, values, scope)
	if err != nil {
		return err
	}
	subPkgs := build.CollectSubGraphPackages(rootPkg, s.pkgMetaMap, s.allTargets, s.conf.needed, s.rc.ctx.Resolver.SubParents())
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		if !subPkgs[name] || s.bindings[name] == nil {
			continue
		}
		values, err := s.packageBindingValues(name, scope)
		if err != nil {
			return err
		}
		if _, err := s.matchingPackageBinding(name, values); err != nil {
			return err
		}
	}
	if previous, ok := s.subGraphRequests[rootPkg]; ok && previous != request {
		return fmt.Errorf("subgraph package %s was already requested with an incompatible build configuration", rootPkg)
	}
	if completed {
		return previous
	}
	if s.subGraphRequests == nil {
		s.subGraphRequests = make(map[string]string)
	}
	s.subGraphRequests[rootPkg] = request
	s.subGraphBuilt[rootPkg] = true
	defer func() { s.subGraphErrors[rootPkg] = err }()
	defer scriptcall.Recover(&err)
	s.scopeValues = scope
	defer func() { s.scopeValues = previousScope }()
	for {
		if err := s.executeMainPackages(subPkgs); err != nil {
			return err
		}
		expanded := build.CollectSubGraphPackages(rootPkg, s.pkgMetaMap, s.allTargets, s.conf.needed, s.rc.ctx.Resolver.SubParents())
		if len(expanded) == len(subPkgs) {
			break
		}
		subPkgs = expanded
	}
	bound := s.bindings[rootPkg]
	if bound == nil {
		return fmt.Errorf("subgraph package %s has no build declaration", rootPkg)
	}
	env := buildEnvironment(s.rc, s.conf, s.source, s)
	job := &build.BuildJob{
		Env:          env,
		Toolchain:    bound.toolchain,
		Platform:     platformFromValues(bound.values),
		Mode:         s.rc.cfg.Mode,
		IncludeTests: s.rc.opts.IncludeTests,
		Explain:      s.rc.opts.Explain,
	}
	return build.BuildSubGraph(job, build.SubGraphRequest{
		RootPkg: rootPkg,
		Targets: s.allTargets,
		PkgMeta: s.pkgMetaMap,
		Needed:  s.conf.needed,
	})
}

func (s *declareStage) resolveDepTargets() map[string]map[string]*api.Target {
	return s.allTargets
}

// computeDepSrcDir resolves a dependency reference to that package's upstream
// source root. Target names are accepted for symmetry with computeDepOutput but
// do not affect the result.
func (s *declareStage) computeDepSrcDir(currentPkg, depRef string) string {
	pkgName, _, ok := strings.Cut(depRef, ":")
	if !ok {
		pkgName = depRef
	}
	pkgName = s.resolveSubPackageName(currentPkg, pkgName, func(candidate string) bool {
		return s.conf.needed[candidate] && s.source.pkgDirs[candidate] != nil
	})
	if pkgName == "" || s.source.pkgDirs[pkgName] == nil {
		return ""
	}
	node := s.rc.ctx.DepGraph.Packages[pkgName]
	if node == nil || node.Pkg == nil {
		return ""
	}
	return node.Pkg.SrcDir()
}

func (s *declareStage) computeDepPackageDir(currentPkg, depRef string) string {
	pkgName, _, ok := strings.Cut(depRef, ":")
	if !ok {
		pkgName = depRef
	}
	pkgName = s.resolveSubPackageName(currentPkg, pkgName, func(candidate string) bool {
		return s.conf.needed[candidate] && s.source.pkgDirs[candidate] != nil
	})
	if pkgName == "" || s.source.pkgDirs[pkgName] == nil {
		validateDirectDepPackageRef(s.rc.ctx, currentPkg, depRef)
		return ""
	}
	if pkgName != currentPkg && !hasDirectNodeDep(s.rc.ctx, currentPkg, pkgName) {
		panic(&api.BuildScriptError{
			Package: currentPkg,
			Op:      "DepPackageDir",
			Err:     fmt.Errorf("package %q is not a direct dependency of %q (add AddRequires(%q))", pkgName, currentPkg, pkgName),
		})
	}
	return s.source.pkgDirs[pkgName].SourceDir
}

func validateDirectDepPackageRef(ctx *RuntimeContext, currentPkg, depRef string) {
	if ctx == nil || ctx.DepGraph == nil {
		return
	}
	pkgName, _, ok := strings.Cut(depRef, ":")
	if !ok {
		pkgName = depRef
	}
	if pkgName == "" {
		return
	}
	var subParents map[string]string
	if ctx.Resolver != nil {
		subParents = ctx.Resolver.SubParents()
	}
	pkgName = api.ResolveSubPackageName(currentPkg, pkgName, subParents, func(candidate string) bool {
		return ctx.DepGraph.Packages[candidate] != nil
	})
	if pkgName != currentPkg && !hasDirectNodeDep(ctx, currentPkg, pkgName) {
		panic(&api.BuildScriptError{
			Package: currentPkg,
			Op:      "DepPackageDir",
			Err:     fmt.Errorf("package %q is not a direct dependency of %q (add AddRequires(%q))", pkgName, currentPkg, pkgName),
		})
	}
}

func hasDirectNodeDep(ctx *RuntimeContext, fromPkg, toPkg string) bool {
	if ctx == nil || ctx.DepGraph == nil {
		return false
	}
	node := ctx.DepGraph.Packages[fromPkg]
	if node == nil {
		return false
	}
	var subParents map[string]string
	if ctx.Resolver != nil {
		subParents = ctx.Resolver.SubParents()
	}
	for _, dep := range node.Deps {
		resolved := api.ResolveSubPackageName(fromPkg, dep, subParents, func(candidate string) bool {
			return ctx.DepGraph.Packages[candidate] != nil
		})
		if resolved == toPkg {
			return true
		}
	}
	return false
}

func (s *declareStage) computeDepOutput(currentPkg, depRef string) string {
	pkgName, targetName, ok := strings.Cut(depRef, ":")
	if !ok {
		pkgName = depRef
		targetName = ""
	}
	pkgName = s.resolveSubPackageName(currentPkg, pkgName, func(candidate string) bool {
		return s.conf.needed[candidate] && s.source.pkgDirs[candidate] != nil
	})
	pd := s.source.pkgDirs[pkgName]
	if pd == nil {
		return ""
	}
	targets := s.resolveDepTargets()
	if targetName == "" {
		pkgTargets := targets[pkgName]
		if len(pkgTargets) == 1 {
			for name := range pkgTargets {
				targetName = name
			}
		}
	}
	if targetName == "" {
		return ""
	}
	target := targets[pkgName][targetName]
	if target == nil {
		return ""
	}
	if pd.BuildDir != "" {
		platform := s.rc.cfg.Platform
		if node := s.rc.ctx.DepGraph.Packages[pkgName]; node != nil && node.Pkg != nil {
			platform.OS = node.Pkg.TargetOS()
		}
		filename := api.TargetFilename(target.Kind(), targetName, platform.OSOrHost())
		return filepath.Join(pd.BuildDir, filename)
	}
	return ""
}

func (s *declareStage) logResults() {
	vlog.Verbose("")
	vlog.Verbose("Targets found:")
	for _, line := range targetListLines(s.allTargets) {
		vlog.Verbose("%s", line)
	}

	vlog.Info("")
	vlog.Info("Using toolchain: %s, mode: %s", s.rc.cfg.TcName, s.rc.cfg.Mode)
}

func targetListLines(allTargets map[string]map[string]*api.Target) []string {
	lines := make([]string, 0)
	for _, pkgName := range slices.Sorted(maps.Keys(allTargets)) {
		targets := allTargets[pkgName]
		for _, targetName := range slices.Sorted(maps.Keys(targets)) {
			t := targets[targetName]
			defaultMark := ""
			testMark := ""
			if !t.IsDefault() {
				defaultMark = " [disabled]"
			}
			if t.IsTest() {
				testMark = " [test]"
			}
			lines = append(lines, fmt.Sprintf("  - %s:%s (%s)%s%s", pkgName, t.Name(), t.Kind(), defaultMark, testMark))
		}
	}
	return lines
}
