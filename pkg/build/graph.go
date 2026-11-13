package build

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/pkgvis"
	"github.com/spock2300/vmake/internal/toposort"
	"github.com/spock2300/vmake/pkg/api"
)

type PkgBuildMeta struct {
	Origin    api.SourceOrigin
	Linked    bool
	Deps      []string
	SubGraphs []string
	VisPath   string
}

func (m PkgBuildMeta) IsRemote() bool {
	return m.Origin == api.SourceRemote
}

func (m PkgBuildMeta) IsFirstParty() bool {
	return !m.IsRemote() || m.Linked
}

type BuildGraph struct {
	Nodes   map[string]*BuildNode
	Order   []string
	PkgMeta map[string]PkgBuildMeta
}

func (g *BuildGraph) GetNode(name string) (*BuildNode, error) {
	node, ok := g.Nodes[name]
	if !ok {
		return nil, fmt.Errorf("target not found: %s", name)
	}
	return node, nil
}

func (g *BuildGraph) ForEachDefault(includeTests bool, fn func(node *BuildNode) error) error {
	for _, fullName := range g.Order {
		node, err := g.GetNode(fullName)
		if err != nil {
			return err
		}
		if !node.Target.IsDefault() {
			continue
		}
		if node.Target.IsTest() && !includeTests {
			continue
		}
		if err := fn(node); err != nil {
			return err
		}
	}
	return nil
}

type BuildNode struct {
	FullName        string
	PkgName         string
	Target          *api.Target
	Deps            []string
	DirectDeps      []string
	DeclaredDepPkgs []string
}

func NewBuildGraph(
	targets map[string]map[string]*api.Target,
	pkgMeta map[string]PkgBuildMeta,
	subParents map[string]string,
) (*BuildGraph, error) {
	graph := &BuildGraph{
		Nodes:   make(map[string]*BuildNode),
		PkgMeta: pkgMeta,
	}

	for pkgName, pkgTargets := range targets {
		for targetName, target := range pkgTargets {
			fullName := fmt.Sprintf("%s:%s", pkgName, targetName)
			graph.Nodes[fullName] = &BuildNode{
				FullName:   fullName,
				PkgName:    pkgName,
				Target:     target,
				Deps:       make([]string, 0),
				DirectDeps: make([]string, 0),
			}
		}
	}

	resolver := newDepResolver(graph.Nodes, pkgMeta, subParents)

	pkgNames := make([]string, 0, len(targets))
	for pkgName := range targets {
		pkgNames = append(pkgNames, pkgName)
	}
	sort.Strings(pkgNames)

	for _, pkgName := range pkgNames {
		pkgTargets := targets[pkgName]
		targetNames := make([]string, 0, len(pkgTargets))
		for targetName := range pkgTargets {
			targetNames = append(targetNames, targetName)
		}
		sort.Strings(targetNames)

		for _, targetName := range targetNames {
			target := pkgTargets[targetName]
			fullName := fmt.Sprintf("%s:%s", pkgName, targetName)
			node := graph.Nodes[fullName]

			deps := target.Deps()
			if target.UsesRequires() {
				if meta, ok := pkgMeta[pkgName]; ok && len(meta.Deps) > 0 {
					reqDeps := make([]string, 0, len(meta.Deps)+len(deps))
					for _, depPkg := range meta.Deps {
						reqDeps = append(reqDeps, depPkg+":*")
					}
					deps = append(reqDeps, deps...)
				}
			}
			resolved, err := resolver.resolveDeps(deps, pkgName, nil)
			if err != nil {
				return nil, err
			}
			direct, err := resolver.resolveDirectDeps(deps, pkgName, nil)
			if err != nil {
				return nil, err
			}
			node.Deps = resolved
			node.DirectDeps = direct
			node.DeclaredDepPkgs = resolver.resolveDeclaredPkgs(deps, pkgName)
		}
	}

	if err := validatePackageGraph(targets, graph.Nodes, pkgMeta, resolver); err != nil {
		return nil, err
	}

	order, err := topologicalSort(graph.Nodes)
	if err != nil {
		return nil, err
	}
	graph.Order = order

	return graph, nil
}

func validatePackageGraph(
	targets map[string]map[string]*api.Target,
	nodes map[string]*BuildNode,
	pkgMeta map[string]PkgBuildMeta,
	resolver *depResolver,
) error {
	pkgNodes := make(map[string][]string)
	edgeReasons := make(map[[2]string][]string)

	ensurePkg := func(p string) {
		if p == "" {
			return
		}
		if _, ok := pkgNodes[p]; !ok {
			pkgNodes[p] = nil
		}
	}
	for p := range pkgMeta {
		ensurePkg(p)
	}
	for p := range targets {
		ensurePkg(p)
	}

	visPath := func(pkg string) string {
		if m, ok := pkgMeta[pkg]; ok && m.VisPath != "" {
			return m.VisPath
		}
		return pkgvis.VisibilityPath(!strings.Contains(pkg, "/"), pkg, "", "")
	}

	addEdge := func(fromPkg, toPkg, reason string) error {
		if fromPkg == "" || toPkg == "" || fromPkg == toPkg {
			return nil
		}
		if err := pkgvis.CheckInternal(fromPkg, visPath(fromPkg), toPkg, visPath(toPkg)); err != nil {
			return err
		}
		ensurePkg(fromPkg)
		ensurePkg(toPkg)
		if !slices.Contains(pkgNodes[fromPkg], toPkg) {
			pkgNodes[fromPkg] = append(pkgNodes[fromPkg], toPkg)
		}
		key := [2]string{fromPkg, toPkg}
		if reason != "" && !slices.Contains(edgeReasons[key], reason) {
			edgeReasons[key] = append(edgeReasons[key], reason)
		}
		return nil
	}

	metaNames := make([]string, 0, len(pkgMeta))
	for p := range pkgMeta {
		metaNames = append(metaNames, p)
	}
	sort.Strings(metaNames)

	for _, fromPkg := range metaNames {
		meta := pkgMeta[fromPkg]
		for _, dep := range meta.Deps {
			resolved := resolver.resolveDepPkgName(fromPkg, dep)
			if err := addEdge(fromPkg, resolved, fmt.Sprintf("%s -> %s (OnRequire)", fromPkg, resolved)); err != nil {
				return err
			}
		}
		for _, sub := range meta.SubGraphs {
			resolved := resolver.resolveDepPkgName(fromPkg, sub)
			if resolved == fromPkg {
				return fmt.Errorf("package circular dependency detected: %s -> %s (%s -> BuildSubGraph(%s))", fromPkg, fromPkg, fromPkg, resolved)
			}
			if err := addEdge(fromPkg, resolved, fmt.Sprintf("%s -> BuildSubGraph(%s)", fromPkg, resolved)); err != nil {
				return err
			}
		}
	}

	nodeNames := make([]string, 0, len(nodes))
	for fullName := range nodes {
		nodeNames = append(nodeNames, fullName)
	}
	sort.Strings(nodeNames)

	for _, fullName := range nodeNames {
		node := nodes[fullName]
		for _, rawDep := range node.Target.Deps() {
			depPkg := resolver.resolveRawCrossPkg(node.PkgName, rawDep)
			if depPkg != "" && depPkg != node.PkgName {
				reasonTarget := depPkg
				if _, targetSpec, hasColon := strings.Cut(rawDep, ":"); hasColon && targetSpec != "" && targetSpec != "*" {
					reasonTarget = depPkg + ":" + targetSpec
				}
				if err := addEdge(node.PkgName, depPkg, fmt.Sprintf("%s -> %s", node.FullName, reasonTarget)); err != nil {
					return err
				}
			}
		}
	}

	if _, err := toposort.TopologicalSort(pkgNodes, func(deps []string) []string { return deps }); err != nil {
		cycle := toposort.FindCycle(pkgNodes, func(deps []string) []string { return deps })
		if len(cycle) == 0 {
			return err
		}
		var details []string
		for i := 0; i+1 < len(cycle); i++ {
			reasons := edgeReasons[[2]string{cycle[i], cycle[i+1]}]
			if len(reasons) > 0 {
				details = append(details, reasons[0])
			}
		}
		if len(details) > 0 {
			return fmt.Errorf("package circular dependency detected: %s (%s)", strings.Join(cycle, " -> "), strings.Join(details, ", "))
		}
		return fmt.Errorf("package circular dependency detected: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

type depResolver struct {
	nodes      map[string]*BuildNode
	pkgMeta    map[string]PkgBuildMeta
	subParents map[string]string
	pkgTargets map[string][]string
	memo       map[string][]string
}

func newDepResolver(nodes map[string]*BuildNode, pkgMeta map[string]PkgBuildMeta, subParents map[string]string) *depResolver {
	pkgTargets := make(map[string][]string)
	for fullName := range nodes {
		if pkg, _, ok := strings.Cut(fullName, ":"); ok {
			pkgTargets[pkg] = append(pkgTargets[pkg], fullName)
		}
	}
	for _, names := range pkgTargets {
		sort.Strings(names)
	}
	return &depResolver{
		nodes:      nodes,
		pkgMeta:    pkgMeta,
		subParents: subParents,
		pkgTargets: pkgTargets,
		memo:       make(map[string][]string),
	}
}

func (d *depResolver) hasPkg(name string) bool {
	if _, ok := d.pkgMeta[name]; ok {
		return true
	}
	return len(d.pkgTargets[name]) > 0
}

func (d *depResolver) resolveDeclaredPkgs(deps []string, currentPkg string) []string {
	seen := make(map[string]bool)
	var out []string
	var addTransitive func(pkg string)
	addTransitive = func(pkg string) {
		if pkg == "" || pkg == currentPkg || seen[pkg] {
			return
		}
		seen[pkg] = true
		out = append(out, pkg)
		if meta, ok := d.pkgMeta[pkg]; ok {
			for _, trans := range meta.Deps {
				addTransitive(d.resolveDepPkgName(pkg, trans))
			}
		}
	}
	for _, dep := range deps {
		if strings.Contains(dep, ":") {
			pkgRef, targetSpec, _ := strings.Cut(dep, ":")
			resolvedPkg := d.resolveDepPkgName(currentPkg, pkgRef)
			if targetSpec == "*" {
				addTransitive(resolvedPkg)
			} else if resolvedPkg != "" && resolvedPkg != currentPkg && !seen[resolvedPkg] {
				seen[resolvedPkg] = true
				out = append(out, resolvedPkg)
			}
			continue
		}
		if strings.Contains(dep, "/") {
			addTransitive(dep)
			continue
		}
		if _, exists := d.nodes[currentPkg+":"+dep]; exists {
			continue
		}
		if resolvedPkg := d.resolveDepPkgName(currentPkg, dep); resolvedPkg != currentPkg && d.hasPkg(resolvedPkg) {
			addTransitive(resolvedPkg)
		}
	}
	return out
}

func (d *depResolver) resolveRawCrossPkg(currentPkg, dep string) string {
	if strings.Contains(dep, ":") {
		pkgRef, _, _ := strings.Cut(dep, ":")
		return d.resolveDepPkgName(currentPkg, pkgRef)
	}
	if strings.Contains(dep, "/") {
		return dep
	}
	if _, exists := d.nodes[currentPkg+":"+dep]; exists {
		return ""
	}
	if resolvedPkg := d.resolveDepPkgName(currentPkg, dep); resolvedPkg != currentPkg {
		if d.hasPkg(resolvedPkg) {
			return resolvedPkg
		}
	}
	return ""
}

func (d *depResolver) resolveDeps(deps []string, currentPkg string, path []string) ([]string, error) {
	var result []string
	seen := make(map[string]bool)

	for _, dep := range deps {
		expanded, err := d.resolveDep(dep, currentPkg, path)
		if err != nil {
			return nil, err
		}
		for _, x := range expanded {
			if !seen[x] {
				seen[x] = true
				result = append(result, x)
			}
		}
	}
	return result, nil
}

func (d *depResolver) resolveDirectDeps(deps []string, currentPkg string, path []string) ([]string, error) {
	var result []string
	seen := make(map[string]bool)

	for _, dep := range deps {
		expanded, err := d.resolveDirectDep(dep, currentPkg, path)
		if err != nil {
			return nil, err
		}
		for _, x := range expanded {
			if !seen[x] {
				seen[x] = true
				result = append(result, x)
			}
		}
	}
	return result, nil
}

func (d *depResolver) resolveDep(dep string, currentPkg string, path []string) ([]string, error) {
	if strings.Contains(dep, ":") {
		pkgRef, targetSpec, _ := strings.Cut(dep, ":")
		resolvedPkg := d.resolveDepPkgName(currentPkg, pkgRef)
		if targetSpec == "*" {
			return d.resolvePackageRef(resolvedPkg, path)
		}
		fullDep := resolvedPkg + ":" + targetSpec
		if _, exists := d.nodes[fullDep]; !exists {
			hint := subPackageHint(currentPkg, pkgRef, resolvedPkg, d.subParents)
			return nil, fmt.Errorf("dependency not found: %s (resolved: %s)%s", dep, fullDep, hint)
		}
		return []string{fullDep}, nil
	}

	if strings.Contains(dep, "/") {
		return d.resolvePackageRef(dep, path)
	}

	qualified := currentPkg + ":" + dep
	if _, exists := d.nodes[qualified]; exists {
		return []string{qualified}, nil
	}
	if resolvedPkg := d.resolveDepPkgName(currentPkg, dep); resolvedPkg != currentPkg {
		if d.hasPkg(resolvedPkg) {
			return d.resolvePackageRef(resolvedPkg, path)
		}
	}
	return nil, fmt.Errorf("dependency not found: %s", dep)
}

func (d *depResolver) resolveDirectDep(dep string, currentPkg string, path []string) ([]string, error) {
	if strings.Contains(dep, ":") {
		pkgRef, targetSpec, _ := strings.Cut(dep, ":")
		resolvedPkg := d.resolveDepPkgName(currentPkg, pkgRef)
		if targetSpec == "*" {
			return d.resolveDirectPackageRef(resolvedPkg, path)
		}
		return []string{resolvedPkg + ":" + targetSpec}, nil
	}

	if strings.Contains(dep, "/") {
		return d.resolveDirectPackageRef(dep, path)
	}

	qualified := currentPkg + ":" + dep
	if _, exists := d.nodes[qualified]; exists {
		return []string{qualified}, nil
	}
	if resolvedPkg := d.resolveDepPkgName(currentPkg, dep); resolvedPkg != currentPkg {
		if d.hasPkg(resolvedPkg) {
			return d.resolveDirectPackageRef(resolvedPkg, path)
		}
	}
	return nil, fmt.Errorf("dependency not found: %s", dep)
}

func (d *depResolver) resolveDirectPackageRef(pkgRef string, path []string) ([]string, error) {
	if err := api.CheckCycle(path, pkgRef); err != nil {
		return nil, err
	}
	if pkgTargetNodes := d.pkgTargets[pkgRef]; len(pkgTargetNodes) > 0 {
		return pkgTargetNodes, nil
	}
	meta, ok := d.pkgMeta[pkgRef]
	if !ok {
		return nil, fmt.Errorf("package not found in build graph: %s", pkgRef)
	}
	var result []string
	for _, transDep := range meta.Deps {
		transDep = d.resolveDepPkgName(pkgRef, transDep)
		expanded, err := d.resolveDirectPackageRef(transDep, append(path, pkgRef))
		if err != nil {
			return nil, err
		}
		result = append(result, expanded...)
	}
	return result, nil
}

func (d *depResolver) resolveDepPkgName(currentPkg, depName string) string {
	return api.ResolveSubPackageName(currentPkg, depName, d.subParents, func(candidate string) bool {
		return d.hasPkg(candidate)
	})
}

func subPackageHint(currentPkg, depName, resolvedPkg string, subParents map[string]string) string {
	if resolvedPkg != depName {
		return ""
	}
	candidates := api.SubPackageCandidates(currentPkg, depName, subParents)
	if len(candidates) == 0 {
		return ""
	}
	return fmt.Sprintf(" (tried sub-package candidates: %s)", strings.Join(candidates, ", "))
}

func (d *depResolver) resolvePackageRef(pkgRef string, path []string) ([]string, error) {
	if err := api.CheckCycle(path, pkgRef); err != nil {
		return nil, err
	}

	if cached, ok := d.memo[pkgRef]; ok {
		return cached, nil
	}

	var result []string

	hasMeta := false
	if meta, ok := d.pkgMeta[pkgRef]; ok {
		hasMeta = true
		for _, transDep := range meta.Deps {
			transDep = d.resolveDepPkgName(pkgRef, transDep)
			expanded, err := d.resolvePackageRef(transDep, append(path, pkgRef))
			if err != nil {
				return nil, err
			}
			result = append(result, expanded...)
		}
	}

	pkgTargetNodes := d.pkgTargets[pkgRef]
	if len(pkgTargetNodes) == 0 && !hasMeta {
		return nil, fmt.Errorf("package not found in build graph: %s", pkgRef)
	}
	result = append(result, pkgTargetNodes...)

	d.memo[pkgRef] = result
	return result, nil
}

func topologicalSort(nodes map[string]*BuildNode) ([]string, error) {
	return toposort.TopologicalSort(nodes, func(n *BuildNode) []string { return n.Deps })
}
