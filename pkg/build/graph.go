package build

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/pkgvis"
	"github.com/spock2300/vmake/internal/toposort"
	"github.com/spock2300/vmake/pkg/api"
)

type PkgBuildMeta struct {
	Origin         api.SourceOrigin
	Linked         bool
	Deps           []string
	SubGraphs      []string
	VisPath        string
	Unmaterialized bool
}

func (m PkgBuildMeta) IsRemote() bool {
	return m.Origin == api.SourceRemote
}

func (m PkgBuildMeta) IsFirstParty() bool {
	return !m.IsRemote() || m.Linked
}

type PackageEdge struct {
	From  string   `json:"from"`
	To    string   `json:"to"`
	Kinds []string `json:"kinds"`
	Via   []string `json:"via,omitempty"`
}

type TargetEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type GraphIssue struct {
	Severity string `json:"severity"`
	Kind     string `json:"kind"`
	Package  string `json:"package,omitempty"`
	Target   string `json:"target,omitempty"`
	Message  string `json:"message"`
}

type GraphAuditOptions struct {
	SubParents map[string]string
	Needed     map[string]bool
	Included   map[string]bool
}

type BuildGraph struct {
	Nodes        map[string]*BuildNode
	Order        []string
	PkgMeta      map[string]PkgBuildMeta
	Packages     []string
	PackageDeps  map[string][]string
	PackageEdges []PackageEdge
	Targets      []string
	TargetDeps   map[string][]string
	TargetEdges  []TargetEdge
	Issues       []GraphIssue
}

// visibilityPathFor is the internal-visibility path recorded for a package.
func visibilityPathFor(pkgMeta map[string]PkgBuildMeta, pkg string) string {
	if m, ok := pkgMeta[pkg]; ok && m.VisPath != "" {
		return m.VisPath
	}
	return pkgvis.VisibilityPath(!strings.Contains(pkg, "/"), pkg, "", "")
}

// declaredTargetDeps expands a target's declared dependencies, including the
// UseRequires shorthand for every package requirement.
func declaredTargetDeps(target *api.Target, meta PkgBuildMeta) []string {
	deps := target.Deps()
	if !target.UsesRequires() || len(meta.Deps) == 0 {
		return deps
	}
	reqDeps := make([]string, 0, len(meta.Deps)+len(deps))
	for _, depPkg := range meta.Deps {
		reqDeps = append(reqDeps, depPkg+":*")
	}
	return append(reqDeps, deps...)
}

// checkInternalVisibility rejects an edge that crosses an internal/ package
// boundary.
func checkInternalVisibility(pkgMeta map[string]PkgBuildMeta, fromPkg, toPkg string) error {
	if fromPkg == "" || toPkg == "" || fromPkg == toPkg {
		return nil
	}
	return pkgvis.CheckInternal(fromPkg, visibilityPathFor(pkgMeta, fromPkg), toPkg, visibilityPathFor(pkgMeta, toPkg))
}

// packageCycleError formats a package cycle with the first recorded reason per
// hop.
func packageCycleError(cycle []string, reasons map[[2]string][]string) error {
	msg := fmt.Sprintf("package circular dependency detected: %s", strings.Join(cycle, " -> "))
	details := make([]string, 0, len(cycle))
	for i := 0; i+1 < len(cycle); i++ {
		if rs := reasons[[2]string{cycle[i], cycle[i+1]}]; len(rs) > 0 {
			details = append(details, rs[0])
		}
	}
	if len(details) > 0 {
		msg = fmt.Sprintf("%s (%s)", msg, strings.Join(details, ", "))
	}
	return errors.New(msg)
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

			deps := declaredTargetDeps(target, pkgMeta[pkgName])
			resolved, direct, err := resolver.resolveTargetDeps(deps, pkgName, nil)
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

func AuditBuildGraph(
	targets map[string]map[string]*api.Target,
	pkgMeta map[string]PkgBuildMeta,
	opts ...GraphAuditOptions,
) *BuildGraph {
	var opt GraphAuditOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	g := &BuildGraph{
		Nodes:       make(map[string]*BuildNode),
		PkgMeta:     pkgMeta,
		PackageDeps: make(map[string][]string),
		TargetDeps:  make(map[string][]string),
	}

	included := make(map[string]bool)
	if opt.Included != nil {
		for name, ok := range opt.Included {
			if ok {
				included[name] = true
				g.Packages = append(g.Packages, name)
			}
		}
	} else {
		for name := range pkgMeta {
			included[name] = true
			g.Packages = append(g.Packages, name)
		}
		for name := range targets {
			if !included[name] {
				included[name] = true
				g.Packages = append(g.Packages, name)
			}
		}
	}
	sort.Strings(g.Packages)

	for _, pkgName := range g.Packages {
		tMap := targets[pkgName]
		for tName, t := range tMap {
			full := pkgName + ":" + tName
			g.Targets = append(g.Targets, full)
			g.Nodes[full] = &BuildNode{
				FullName:   full,
				PkgName:    pkgName,
				Target:     t,
				Deps:       make([]string, 0),
				DirectDeps: make([]string, 0),
			}
		}
	}
	sort.Strings(g.Targets)
	for _, full := range g.Targets {
		g.TargetDeps[full] = nil
	}

	resolver := newDepResolver(g.Nodes, pkgMeta, opt.SubParents)
	resolver.resolvePkgOverride = func(fromPkg, ref string) string {
		if ref == "" || strings.Contains(ref, "/") {
			return ref
		}
		if opt.Needed != nil && opt.Needed[fromPkg] {
			if cand := api.ResolveSubPackageName(fromPkg, ref, opt.SubParents, func(c string) bool {
				return opt.Needed[c]
			}); cand != ref || opt.Needed[cand] {
				return cand
			}
		}
		return api.ResolveSubPackageName(fromPkg, ref, opt.SubParents, resolver.hasPkg)
	}

	type edgeAcc struct {
		kinds []string
		via   []string
	}
	pkgEdgeMap := make(map[[2]string]*edgeAcc)
	addPkgEdge := func(from, to, kind, via string) {
		if from == "" || to == "" || from == to {
			return
		}
		if !included[from] || !included[to] {
			return
		}
		key := [2]string{from, to}
		acc := pkgEdgeMap[key]
		if acc == nil {
			acc = &edgeAcc{}
			pkgEdgeMap[key] = acc
		}
		if kind != "" && !slices.Contains(acc.kinds, kind) {
			acc.kinds = append(acc.kinds, kind)
		}
		if via != "" && !slices.Contains(acc.via, via) {
			acc.via = append(acc.via, via)
		}
		if !slices.Contains(g.PackageDeps[from], to) {
			g.PackageDeps[from] = append(g.PackageDeps[from], to)
		}
	}

	seenVis := make(map[[2]string]bool)
	checkInternalVis := func(fromPkg, toPkg, targetFull string) {
		if fromPkg == "" || toPkg == "" || fromPkg == toPkg {
			return
		}
		pair := [2]string{fromPkg, toPkg}
		if seenVis[pair] {
			return
		}
		if err := checkInternalVisibility(pkgMeta, fromPkg, toPkg); err != nil {
			seenVis[pair] = true
			g.Issues = append(g.Issues, GraphIssue{
				Severity: "error",
				Kind:     "internal-visibility",
				Package:  fromPkg,
				Target:   targetFull,
				Message:  err.Error(),
			})
		}
	}

	crossPkgOnBuild := func(fromPkg, toPkg, targetFull, refDesc string) bool {
		if fromPkg == "" || toPkg == "" || fromPkg == toPkg {
			return true
		}
		if opt.Needed == nil || !opt.Needed[fromPkg] || opt.Needed[toPkg] {
			return true
		}
		checkInternalVis(fromPkg, toPkg, targetFull)
		g.Issues = append(g.Issues, GraphIssue{
			Severity: "error",
			Kind:     "missing-dependency",
			Package:  fromPkg,
			Target:   targetFull,
			Message: fmt.Sprintf("package %s references %q (%s) in OnBuild without declaring AddRequires(%q) in OnRequire",
				fromPkg, toPkg, refDesc, toPkg),
		})
		return false
	}

	for _, name := range g.Packages {
		g.PackageDeps[name] = nil
	}
	for _, name := range g.Packages {
		meta := pkgMeta[name]
		for _, dep := range meta.Deps {
			resolved := resolver.resolveDepPkgName(name, dep)
			if !resolver.hasPkg(resolved) {
				g.Issues = append(g.Issues, GraphIssue{
					Severity: "error",
					Kind:     "missing-dependency",
					Package:  name,
					Message:  fmt.Sprintf("package %s requires unknown package %q", name, dep),
				})
				continue
			}
			addPkgEdge(name, resolved, "require", "OnRequire")
		}
		for _, rawSub := range meta.SubGraphs {
			sub := resolver.resolveDepPkgName(name, rawSub)
			if sub == name {
				g.Issues = append(g.Issues, GraphIssue{
					Severity: "error",
					Kind:     "package-cycle",
					Package:  name,
					Message:  fmt.Sprintf("package circular dependency detected: %s -> %s (BuildSubGraph(%s))", name, name, name),
				})
				continue
			}
			if !resolver.hasPkg(sub) {
				g.Issues = append(g.Issues, GraphIssue{
					Severity: "error",
					Kind:     "missing-dependency",
					Package:  name,
					Target:   "",
					Message:  fmt.Sprintf("package %s BuildSubGraph references unknown package %q", name, sub),
				})
				continue
			}
			via := fmt.Sprintf("BuildSubGraph(%s)", sub)
			_ = crossPkgOnBuild(name, sub, "", via)
			addPkgEdge(name, sub, "subgraph", via)
		}
	}

	seenTargetEdge := make(map[TargetEdge]bool)
	addTargetEdge := func(fromFull, toFull string) {
		if fromFull == "" || toFull == "" {
			return
		}
		te := TargetEdge{From: fromFull, To: toFull}
		if !seenTargetEdge[te] {
			seenTargetEdge[te] = true
			g.TargetEdges = append(g.TargetEdges, te)
		}
		if !slices.Contains(g.TargetDeps[fromFull], toFull) {
			g.TargetDeps[fromFull] = append(g.TargetDeps[fromFull], toFull)
		}
	}

	appendUnique := func(dst []string, items ...string) []string {
		for _, item := range items {
			if item != "" && !slices.Contains(dst, item) {
				dst = append(dst, item)
			}
		}
		return dst
	}

	pkgViaLabel := func(fromFull, depPkg, fallbackSuffix string, expanded []string) string {
		if direct := resolver.pkgTargets[depPkg]; len(direct) == 1 {
			return fromFull + " -> " + direct[0]
		}
		if len(expanded) == 1 {
			return fromFull + " -> " + expanded[0]
		}
		return fromFull + " -> " + depPkg + fallbackSuffix
	}

	var pkgRefTargetPairs [][2]string
	seenPkgRefPair := make(map[[2]string]bool)
	recordPkgRefPair := func(fromFull, depPkg string) {
		pair := [2]string{fromFull, depPkg}
		if !seenPkgRefPair[pair] {
			seenPkgRefPair[pair] = true
			pkgRefTargetPairs = append(pkgRefTargetPairs, pair)
		}
	}

	for _, pkgName := range g.Packages {
		tMap := targets[pkgName]
		tNames := make([]string, 0, len(tMap))
		for tName := range tMap {
			tNames = append(tNames, tName)
		}
		sort.Strings(tNames)

		for _, tName := range tNames {
			target := tMap[tName]
			fullName := pkgName + ":" + tName
			node := g.Nodes[fullName]
			rawDeps := append([]string(nil), declaredTargetDeps(target, pkgMeta[pkgName])...)
			node.DeclaredDepPkgs = resolver.resolveDeclaredPkgs(rawDeps, pkgName)

			for _, dep := range rawDeps {
				c := resolver.classifyDep(pkgName, dep)
				switch c.kind {
				case depKindSamePkgTarget:
					addTargetEdge(fullName, c.targetFull)
					node.DirectDeps = appendUnique(node.DirectDeps, c.targetFull)
					node.Deps = appendUnique(node.Deps, c.targetFull)

				case depKindCrossTarget:
					pkgKnown := resolver.hasPkg(c.pkg)
					if pkgKnown && !crossPkgOnBuild(pkgName, c.pkg, fullName, dep) {
						if c.pkg != pkgName {
							addPkgEdge(pkgName, c.pkg, "target", fullName+" -> "+c.targetFull)
						}
						continue
					}
					if c.pkg != pkgName {
						addPkgEdge(pkgName, c.pkg, "target", fullName+" -> "+c.targetFull)
					}
					sourceMaterialized := pkgKnown && !pkgMeta[c.pkg].Unmaterialized
					if _, exists := g.Nodes[c.targetFull]; !exists {
						if !pkgKnown || sourceMaterialized {
							g.Issues = append(g.Issues, GraphIssue{
								Severity: "error",
								Kind:     "missing-dependency",
								Package:  pkgName,
								Target:   fullName,
								Message:  fmt.Sprintf("dependency not found: %s (resolved: %s)", dep, c.targetFull),
							})
						}
						continue
					}
					addTargetEdge(fullName, c.targetFull)
					node.DirectDeps = appendUnique(node.DirectDeps, c.targetFull)
					node.Deps = appendUnique(node.Deps, c.targetFull)

				case depKindPackageRef:
					suffix := ""
					if c.wildcard {
						suffix = ":*"
					}
					if !resolver.hasPkg(c.pkg) {
						g.Issues = append(g.Issues, GraphIssue{
							Severity: "error",
							Kind:     "missing-dependency",
							Package:  pkgName,
							Target:   fullName,
							Message:  fmt.Sprintf("dependency not found: %s (from %s)", dep, fullName),
						})
						continue
					}
					if !crossPkgOnBuild(pkgName, c.pkg, fullName, dep) {
						addPkgEdge(pkgName, c.pkg, "target", fullName+" -> "+c.pkg+suffix)
						continue
					}
					expanded := resolver.expandDirectPkgTargets(c.pkg)
					if c.pkg != pkgName {
						addPkgEdge(pkgName, c.pkg, "target", pkgViaLabel(fullName, c.pkg, suffix, expanded))
					}
					for _, depFull := range expanded {
						addTargetEdge(fullName, depFull)
					}
					node.DirectDeps = appendUnique(node.DirectDeps, expanded...)
					node.Deps = appendUnique(node.Deps, resolver.expandAllPkgTargets(c.pkg)...)
					recordPkgRefPair(fullName, c.pkg)

				default:
					g.Issues = append(g.Issues, GraphIssue{
						Severity: "error",
						Kind:     "missing-dependency",
						Package:  pkgName,
						Target:   fullName,
						Message:  fmt.Sprintf("dependency not found: %s (from %s)", dep, fullName),
					})
				}
			}
		}
	}

	for _, pair := range pkgRefTargetPairs {
		fromFull, depPkg := pair[0], pair[1]
		allTransTargets := resolver.expandAllPkgTargets(depPkg)
		if len(allTransTargets) == 0 {
			continue
		}
		reachable := make(map[string]bool)
		queue := append([]string(nil), g.TargetDeps[fromFull]...)
		for _, q := range queue {
			reachable[q] = true
		}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, nxt := range g.TargetDeps[cur] {
				if !reachable[nxt] {
					reachable[nxt] = true
					queue = append(queue, nxt)
				}
			}
		}
		for _, tFull := range allTransTargets {
			if !reachable[tFull] {
				addTargetEdge(fromFull, tFull)
			}
		}
	}

	for _, name := range g.Packages {
		sort.Strings(g.PackageDeps[name])
	}
	for _, full := range g.Targets {
		sort.Strings(g.TargetDeps[full])
	}
	sort.Slice(g.TargetEdges, func(i, j int) bool {
		if g.TargetEdges[i].From != g.TargetEdges[j].From {
			return g.TargetEdges[i].From < g.TargetEdges[j].From
		}
		return g.TargetEdges[i].To < g.TargetEdges[j].To
	})

	edgeKeys := make([][2]string, 0, len(pkgEdgeMap))
	for k := range pkgEdgeMap {
		edgeKeys = append(edgeKeys, k)
	}
	sort.Slice(edgeKeys, func(i, j int) bool {
		if edgeKeys[i][0] != edgeKeys[j][0] {
			return edgeKeys[i][0] < edgeKeys[j][0]
		}
		return edgeKeys[i][1] < edgeKeys[j][1]
	})
	for _, k := range edgeKeys {
		acc := pkgEdgeMap[k]
		g.PackageEdges = append(g.PackageEdges, PackageEdge{
			From:  k[0],
			To:    k[1],
			Kinds: acc.kinds,
			Via:   acc.via,
		})
	}

	for _, pe := range g.PackageEdges {
		checkInternalVis(pe.From, pe.To, "")
	}

	identity := func(deps []string) []string { return deps }
	if _, err := toposort.TopologicalSort(g.PackageDeps, identity); err != nil {
		cycle := toposort.FindCycle(g.PackageDeps, identity)
		if len(cycle) > 0 {
			reasons := make(map[[2]string][]string, len(pkgEdgeMap))
			for key, acc := range pkgEdgeMap {
				reasons[key] = acc.via
			}
			g.Issues = append(g.Issues, GraphIssue{
				Severity: "error",
				Kind:     "package-cycle",
				Package:  cycle[0],
				Message:  packageCycleError(cycle, reasons).Error(),
			})
		}
	}

	if order, err := toposort.TopologicalSort(g.TargetDeps, identity); err != nil {
		cycle := toposort.FindCycle(g.TargetDeps, identity)
		if len(cycle) > 0 {
			ownerPkg, _, _ := strings.Cut(cycle[0], ":")
			g.Issues = append(g.Issues, GraphIssue{
				Severity: "error",
				Kind:     "target-cycle",
				Package:  ownerPkg,
				Target:   cycle[0],
				Message:  fmt.Sprintf("target circular dependency detected: %s", strings.Join(cycle, " -> ")),
			})
		}
	} else {
		g.Order = order
	}

	return g
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

	addEdge := func(fromPkg, toPkg, reason string) error {
		if fromPkg == "" || toPkg == "" || fromPkg == toPkg {
			return nil
		}
		if err := checkInternalVisibility(pkgMeta, fromPkg, toPkg); err != nil {
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
				if ref := ParseDependencyRef(rawDep); ref.HasTarget && ref.Target != "" && ref.Target != "*" {
					reasonTarget = depPkg + ":" + ref.Target
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
		return packageCycleError(cycle, edgeReasons)
	}
	return nil
}

type depResolver struct {
	nodes              map[string]*BuildNode
	pkgMeta            map[string]PkgBuildMeta
	subParents         map[string]string
	pkgTargets         map[string][]string
	memo               map[string][]string
	resolvePkgOverride func(fromPkg, ref string) string
}

func newDepResolver(nodes map[string]*BuildNode, pkgMeta map[string]PkgBuildMeta, subParents map[string]string) *depResolver {
	pkgTargets := make(map[string][]string)
	for fullName := range nodes {
		if ref := ParseDependencyRef(fullName); ref.HasTarget {
			pkgTargets[ref.Package] = append(pkgTargets[ref.Package], fullName)
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

type depKind int

const (
	depKindUnknown depKind = iota
	depKindSamePkgTarget
	depKindCrossTarget
	depKindPackageRef
)

type classifiedDep struct {
	kind       depKind
	rawPkg     string
	pkg        string
	targetFull string
	wildcard   bool
}

func (d *depResolver) classifyDep(currentPkg, dep string) classifiedDep {
	ref := ParseDependencyRef(dep)
	if ref.HasTarget {
		resolvedPkg := d.resolveDepPkgName(currentPkg, ref.Package)
		if ref.Target == "*" {
			return classifiedDep{kind: depKindPackageRef, rawPkg: ref.Package, pkg: resolvedPkg, wildcard: true}
		}
		return classifiedDep{
			kind:       depKindCrossTarget,
			rawPkg:     ref.Package,
			pkg:        resolvedPkg,
			targetFull: resolvedPkg + ":" + ref.Target,
		}
	}
	if ref.IsPackagePath() {
		return classifiedDep{kind: depKindPackageRef, rawPkg: ref.Package, pkg: ref.Package}
	}
	qualified := currentPkg + ":" + ref.Package
	if _, exists := d.nodes[qualified]; exists {
		return classifiedDep{kind: depKindSamePkgTarget, rawPkg: ref.Package, pkg: currentPkg, targetFull: qualified}
	}
	if resolvedPkg := d.resolveDepPkgName(currentPkg, ref.Package); resolvedPkg != currentPkg && d.hasPkg(resolvedPkg) {
		return classifiedDep{kind: depKindPackageRef, rawPkg: ref.Package, pkg: resolvedPkg}
	}
	return classifiedDep{kind: depKindUnknown, rawPkg: ref.Package}
}

func (d *depResolver) expandDirectPkgTargets(depPkg string) []string {
	visited := make(map[string]bool)
	var out []string
	var walk func(p string)
	walk = func(p string) {
		if p == "" || visited[p] {
			return
		}
		visited[p] = true
		if ts := d.pkgTargets[p]; len(ts) > 0 {
			for _, full := range ts {
				if !slices.Contains(out, full) {
					out = append(out, full)
				}
			}
			return
		}
		for _, trans := range d.pkgMeta[p].Deps {
			walk(d.resolveDepPkgName(p, trans))
		}
	}
	walk(depPkg)
	return out
}

func (d *depResolver) expandAllPkgTargets(depPkg string) []string {
	visited := make(map[string]bool)
	var out []string
	var walk func(p string)
	walk = func(p string) {
		if p == "" || visited[p] {
			return
		}
		visited[p] = true
		for _, trans := range d.pkgMeta[p].Deps {
			walk(d.resolveDepPkgName(p, trans))
		}
		for _, full := range d.pkgTargets[p] {
			if !slices.Contains(out, full) {
				out = append(out, full)
			}
		}
	}
	walk(depPkg)
	return out
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
		c := d.classifyDep(currentPkg, dep)
		switch c.kind {
		case depKindPackageRef:
			addTransitive(c.pkg)
		case depKindCrossTarget:
			if c.pkg != "" && c.pkg != currentPkg && !seen[c.pkg] {
				seen[c.pkg] = true
				out = append(out, c.pkg)
			}
		}
	}
	return out
}

func (d *depResolver) resolveRawCrossPkg(currentPkg, dep string) string {
	c := d.classifyDep(currentPkg, dep)
	switch c.kind {
	case depKindCrossTarget, depKindPackageRef:
		return c.pkg
	default:
		return ""
	}
}

func (d *depResolver) resolveTargetDeps(deps []string, currentPkg string, path []string) ([]string, []string, error) {
	var allResult []string
	var directResult []string
	seenAll := make(map[string]bool)
	seenDirect := make(map[string]bool)

	for _, dep := range deps {
		expandedAll, expandedDirect, err := d.resolveDepSpec(dep, currentPkg, path)
		if err != nil {
			return nil, nil, err
		}
		for _, x := range expandedAll {
			if !seenAll[x] {
				seenAll[x] = true
				allResult = append(allResult, x)
			}
		}
		for _, x := range expandedDirect {
			if !seenDirect[x] {
				seenDirect[x] = true
				directResult = append(directResult, x)
			}
		}
	}
	return allResult, directResult, nil
}

func (d *depResolver) resolveDep(dep string, currentPkg string, path []string) ([]string, error) {
	all, _, err := d.resolveDepSpec(dep, currentPkg, path)
	return all, err
}

func (d *depResolver) resolveDirectDep(dep string, currentPkg string, path []string) ([]string, error) {
	_, direct, err := d.resolveDepSpec(dep, currentPkg, path)
	return direct, err
}

func (d *depResolver) resolveDepSpec(dep string, currentPkg string, path []string) ([]string, []string, error) {
	c := d.classifyDep(currentPkg, dep)
	switch c.kind {
	case depKindSamePkgTarget:
		return []string{c.targetFull}, []string{c.targetFull}, nil
	case depKindCrossTarget:
		if _, exists := d.nodes[c.targetFull]; !exists {
			hint := subPackageHint(currentPkg, c.rawPkg, c.pkg, d.subParents)
			return nil, nil, fmt.Errorf("dependency not found: %s (resolved: %s)%s", dep, c.targetFull, hint)
		}
		return []string{c.targetFull}, []string{c.targetFull}, nil
	case depKindPackageRef:
		all, err := d.resolvePackageRef(c.pkg, path)
		if err != nil {
			return nil, nil, err
		}
		direct, err := d.resolveDirectPackageRef(c.pkg, path)
		if err != nil {
			return nil, nil, err
		}
		return all, direct, nil
	default:
		return nil, nil, fmt.Errorf("dependency not found: %s", dep)
	}
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
	if d.resolvePkgOverride != nil {
		return d.resolvePkgOverride(currentPkg, depName)
	}
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
