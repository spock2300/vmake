package pipeline

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/pkgvis"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/internal/toposort"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

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

type UnifiedGraph struct {
	Roots        []string            `json:"roots"`
	Needed       map[string]bool     `json:"-"`
	Packages     []string            `json:"packages"`
	PackageDeps  map[string][]string `json:"packageDeps"`
	PackageEdges []PackageEdge       `json:"packageEdges"`
	Targets      []string            `json:"targets"`
	TargetDeps   map[string][]string `json:"targetDeps"`
	TargetEdges  []TargetEdge        `json:"targetEdges"`
	Issues       []GraphIssue        `json:"issues,omitempty"`

	declaredTargets map[string]map[string]*api.Target
	pkgMeta         map[string]build.PkgBuildMeta
	unmaterialized  map[string]bool
	inspection      *Inspection
}

type WhyStep struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Via  []string `json:"via,omitempty"`
}

type WhyResult struct {
	Query  string      `json:"query"`
	Kind   string      `json:"kind"`
	Exists bool        `json:"exists"`
	Needed bool        `json:"needed"`
	IsRoot bool        `json:"isRoot"`
	Paths  [][]string  `json:"paths,omitempty"`
	Steps  [][]WhyStep `json:"steps,omitempty"`
	Reason string      `json:"reason,omitempty"`
}

type DepsCheckReport struct {
	Roots            []string     `json:"roots"`
	PackageCount     int          `json:"packageCount"`
	TargetCount      int          `json:"targetCount"`
	PackageEdgeCount int          `json:"packageEdgeCount"`
	TargetEdgeCount  int          `json:"targetEdgeCount"`
	Issues           []GraphIssue `json:"issues"`
}

func BuildUnifiedGraph(ctx *RuntimeContext, inspection *Inspection, includeAll bool) (*UnifiedGraph, error) {
	if ctx == nil || ctx.DepGraph == nil {
		return nil, fmt.Errorf("project dependency graph is not initialized")
	}
	var err error
	if inspection == nil {
		inspection, err = InspectWithOptions(ctx, InspectOptions{FilterDeps: true})
		if err != nil {
			return nil, err
		}
	}

	g := &UnifiedGraph{
		Needed:          inspection.Needed,
		PackageDeps:     make(map[string][]string),
		TargetDeps:      make(map[string][]string),
		declaredTargets: make(map[string]map[string]*api.Target),
		pkgMeta:         make(map[string]build.PkgBuildMeta),
		unmaterialized:  make(map[string]bool),
		inspection:      inspection,
	}

	included := make(map[string]bool)
	for name, node := range ctx.DepGraph.Packages {
		if node == nil || node.Source == nil {
			continue
		}
		if includeAll || inspection.Needed[name] {
			included[name] = true
			g.Packages = append(g.Packages, name)
		}
	}
	sort.Strings(g.Packages)

	var subParents map[string]string
	if ctx.Resolver != nil {
		subParents = ctx.Resolver.SubParents()
	}
	resolvePkg := func(fromPkg, ref string) string {
		if ref == "" || strings.Contains(ref, "/") {
			return ref
		}
		if inspection.Needed[fromPkg] {
			if cand := api.ResolveSubPackageName(fromPkg, ref, subParents, func(c string) bool {
				return inspection.Needed[c]
			}); cand != ref || inspection.Needed[cand] {
				return cand
			}
		}
		return api.ResolveSubPackageName(fromPkg, ref, subParents, func(cand string) bool {
			_, ok := ctx.DepGraph.Packages[cand]
			return ok
		})
	}

	for _, name := range g.Packages {
		node := ctx.DepGraph.Packages[name]
		visPath := ""
		if ctx.Resolver != nil {
			visPath = ctx.Resolver.VisibilityPath(name)
		}
		meta := build.PkgBuildMeta{
			Origin:  node.Source.Origin,
			Linked:  node.Source.Link != nil,
			Deps:    append([]string(nil), node.Deps...),
			VisPath: visPath,
		}

		present := IsPackageSourcePresent(ctx, name, node)
		if !present {
			g.unmaterialized[name] = true
		}
		if node.Pkg != nil && present {
			dirs := inspection.PkgDirs[name]
			if dirs == nil {
				dirs = &api.PkgDirs{SourceDir: ExpectedPackageSourceDir(ctx, name, node)}
			}
			scriptfs.TakeBlockedWrites()
			buildCtx, declErr := DeclareTargets(ctx, name, dirs, inspection.PackageToolchains[name], inspection.GlobalValues)
			if blocked := scriptfs.TakeBlockedWrites(); len(blocked) > 0 && declErr == nil {
				declErr = fmt.Errorf("%s", blocked[0])
			}
			if declErr != nil {
				g.Issues = append(g.Issues, GraphIssue{
					Severity: "error",
					Kind:     "target-declare",
					Package:  name,
					Message:  fmt.Sprintf("cannot declare targets for %s: %v", name, declErr),
				})
			} else if buildCtx != nil {
				targets := make(map[string]*api.Target)
				for tName, t := range buildCtx.GetTargets() {
					targets[tName] = api.SnapshotTarget(t)
				}
				if len(targets) > 0 {
					g.declaredTargets[name] = targets
				}
				for _, sub := range buildCtx.SubGraphs() {
					resolvedSub := resolvePkg(name, sub)
					if resolvedSub != "" && !slices.Contains(meta.SubGraphs, resolvedSub) {
						meta.SubGraphs = append(meta.SubGraphs, resolvedSub)
					}
				}
			}
		}
		g.pkgMeta[name] = meta
	}

	pkgTargets := make(map[string][]string)
	for _, pkgName := range g.Packages {
		tMap := g.declaredTargets[pkgName]
		for tName := range tMap {
			full := pkgName + ":" + tName
			g.Targets = append(g.Targets, full)
			pkgTargets[pkgName] = append(pkgTargets[pkgName], full)
		}
		sort.Strings(pkgTargets[pkgName])
	}
	sort.Strings(g.Targets)
	targetExists := make(map[string]bool, len(g.Targets))
	for _, full := range g.Targets {
		targetExists[full] = true
		g.TargetDeps[full] = nil
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

	visPathOf := func(pkg string) string {
		if m, ok := g.pkgMeta[pkg]; ok && m.VisPath != "" {
			return m.VisPath
		}
		if ctx.Resolver != nil {
			return ctx.Resolver.VisibilityPath(pkg)
		}
		return ""
	}
	seenInternalPair := make(map[[2]string]bool)
	recordInternalVisibility := func(fromPkg, toPkg, targetFull string) {
		if fromPkg == "" || toPkg == "" || fromPkg == toPkg {
			return
		}
		pair := [2]string{fromPkg, toPkg}
		if seenInternalPair[pair] {
			return
		}
		if err := pkgvis.CheckInternal(fromPkg, visPathOf(fromPkg), toPkg, visPathOf(toPkg)); err != nil {
			seenInternalPair[pair] = true
			g.Issues = append(g.Issues, GraphIssue{
				Severity: "error",
				Kind:     "internal-visibility",
				Package:  fromPkg,
				Target:   targetFull,
				Message:  err.Error(),
			})
		}
	}
	validateCrossPkgOnBuild := func(fromPkg, toPkg, targetFull, refDesc string) bool {
		if fromPkg == "" || toPkg == "" || fromPkg == toPkg {
			return true
		}
		if inspection.Needed[fromPkg] && !inspection.Needed[toPkg] {
			recordInternalVisibility(fromPkg, toPkg, targetFull)
			g.Issues = append(g.Issues, GraphIssue{
				Severity: "error",
				Kind:     "missing-dependency",
				Package:  fromPkg,
				Target:   targetFull,
				Message:  fmt.Sprintf("package %s references %q (%s) in OnBuild without declaring AddRequires(%q) in OnRequire", fromPkg, toPkg, refDesc, toPkg),
			})
			return false
		}
		return true
	}

	for _, name := range g.Packages {
		g.PackageDeps[name] = nil
	}
	for _, name := range g.Packages {
		meta := g.pkgMeta[name]
		for _, dep := range meta.Deps {
			resolved := resolvePkg(name, dep)
			if _, ok := ctx.DepGraph.Packages[resolved]; !ok {
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
		for _, sub := range meta.SubGraphs {
			if sub == name {
				g.Issues = append(g.Issues, GraphIssue{
					Severity: "error",
					Kind:     "package-cycle",
					Package:  name,
					Message:  fmt.Sprintf("package circular dependency detected: %s -> %s (BuildSubGraph(%s))", name, name, name),
				})
				continue
			}
			if _, ok := ctx.DepGraph.Packages[sub]; !ok {
				g.Issues = append(g.Issues, GraphIssue{
					Severity: "error",
					Kind:     "missing-dependency",
					Package:  name,
					Message:  fmt.Sprintf("package %s BuildSubGraph references unknown package %q", name, sub),
				})
				continue
			}
			via := fmt.Sprintf("BuildSubGraph(%s)", sub)
			if !validateCrossPkgOnBuild(name, sub, "", via) {
				addPkgEdge(name, sub, "subgraph", via)
				continue
			}
			addPkgEdge(name, sub, "subgraph", via)
		}
	}

	expandDirectPkgTargets := func(depPkg string) []string {
		visited := make(map[string]bool)
		var out []string
		var walk func(p string)
		walk = func(p string) {
			if p == "" || visited[p] {
				return
			}
			visited[p] = true
			if ts := pkgTargets[p]; len(ts) > 0 {
				for _, full := range ts {
					if !slices.Contains(out, full) {
						out = append(out, full)
					}
				}
				return
			}
			for _, trans := range g.pkgMeta[p].Deps {
				walk(resolvePkg(p, trans))
			}
		}
		walk(depPkg)
		return out
	}

	expandAllPkgTargets := func(depPkg string) []string {
		visited := make(map[string]bool)
		var out []string
		var walk func(p string)
		walk = func(p string) {
			if p == "" || visited[p] {
				return
			}
			visited[p] = true
			for _, trans := range g.pkgMeta[p].Deps {
				walk(resolvePkg(p, trans))
			}
			for _, full := range pkgTargets[p] {
				if !slices.Contains(out, full) {
					out = append(out, full)
				}
			}
		}
		walk(depPkg)
		return out
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

	pkgViaLabel := func(fromFull, depPkg, fallbackSuffix string, expanded []string) string {
		if direct := pkgTargets[depPkg]; len(direct) == 1 {
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
		tMap := g.declaredTargets[pkgName]
		tNames := make([]string, 0, len(tMap))
		for tName := range tMap {
			tNames = append(tNames, tName)
		}
		sort.Strings(tNames)

		for _, tName := range tNames {
			target := tMap[tName]
			fullName := pkgName + ":" + tName
			rawDeps := append([]string(nil), target.Deps()...)
			if target.UsesRequires() {
				reqDeps := make([]string, 0, len(g.pkgMeta[pkgName].Deps)+len(rawDeps))
				for _, depPkg := range g.pkgMeta[pkgName].Deps {
					reqDeps = append(reqDeps, depPkg+":*")
				}
				rawDeps = append(reqDeps, rawDeps...)
			}

			for _, dep := range rawDeps {
				if strings.Contains(dep, ":") {
					pkgRef, targetSpec, _ := strings.Cut(dep, ":")
					resolvedPkg := resolvePkg(pkgName, pkgRef)
					if targetSpec == "*" {
						if _, ok := ctx.DepGraph.Packages[resolvedPkg]; !ok {
							g.Issues = append(g.Issues, GraphIssue{
								Severity: "error",
								Kind:     "missing-dependency",
								Package:  pkgName,
								Target:   fullName,
								Message:  fmt.Sprintf("dependency not found: %s (from %s)", dep, fullName),
							})
							continue
						}
						if !validateCrossPkgOnBuild(pkgName, resolvedPkg, fullName, dep) {
							addPkgEdge(pkgName, resolvedPkg, "target", fullName+" -> "+resolvedPkg+":*")
							continue
						}
						expanded := expandDirectPkgTargets(resolvedPkg)
						if resolvedPkg != pkgName {
							addPkgEdge(pkgName, resolvedPkg, "target", pkgViaLabel(fullName, resolvedPkg, ":*", expanded))
						}
						for _, depFull := range expanded {
							addTargetEdge(fullName, depFull)
						}
						recordPkgRefPair(fullName, resolvedPkg)
						continue
					}
					depFull := resolvedPkg + ":" + targetSpec
					depNode := ctx.DepGraph.Packages[resolvedPkg]
					if depNode != nil && !validateCrossPkgOnBuild(pkgName, resolvedPkg, fullName, dep) {
						if resolvedPkg != pkgName {
							addPkgEdge(pkgName, resolvedPkg, "target", fullName+" -> "+depFull)
						}
						continue
					}
					if resolvedPkg != pkgName {
						addPkgEdge(pkgName, resolvedPkg, "target", fullName+" -> "+depFull)
					}
					sourceMaterialized := depNode != nil && !g.unmaterialized[resolvedPkg]
					if !targetExists[depFull] {
						if depNode == nil || sourceMaterialized {
							g.Issues = append(g.Issues, GraphIssue{
								Severity: "error",
								Kind:     "missing-dependency",
								Package:  pkgName,
								Target:   fullName,
								Message:  fmt.Sprintf("dependency not found: %s (resolved: %s)", dep, depFull),
							})
						}
						continue
					}
					addTargetEdge(fullName, depFull)
					continue
				}

				if strings.Contains(dep, "/") {
					if _, ok := ctx.DepGraph.Packages[dep]; !ok {
						g.Issues = append(g.Issues, GraphIssue{
							Severity: "error",
							Kind:     "missing-dependency",
							Package:  pkgName,
							Target:   fullName,
							Message:  fmt.Sprintf("dependency not found: %s (from %s)", dep, fullName),
						})
						continue
					}
					if !validateCrossPkgOnBuild(pkgName, dep, fullName, dep) {
						addPkgEdge(pkgName, dep, "target", fullName+" -> "+dep)
						continue
					}
					expanded := expandDirectPkgTargets(dep)
					if dep != pkgName {
						addPkgEdge(pkgName, dep, "target", pkgViaLabel(fullName, dep, "", expanded))
					}
					for _, depFull := range expanded {
						addTargetEdge(fullName, depFull)
					}
					recordPkgRefPair(fullName, dep)
					continue
				}

				samePkgTarget := pkgName + ":" + dep
				if targetExists[samePkgTarget] {
					addTargetEdge(fullName, samePkgTarget)
					continue
				}
				resolvedPkg := resolvePkg(pkgName, dep)
				if resolvedPkg != pkgName {
					if _, ok := ctx.DepGraph.Packages[resolvedPkg]; ok {
						if !validateCrossPkgOnBuild(pkgName, resolvedPkg, fullName, dep) {
							addPkgEdge(pkgName, resolvedPkg, "target", fullName+" -> "+resolvedPkg)
							continue
						}
						expanded := expandDirectPkgTargets(resolvedPkg)
						addPkgEdge(pkgName, resolvedPkg, "target", pkgViaLabel(fullName, resolvedPkg, "", expanded))
						for _, depFull := range expanded {
							addTargetEdge(fullName, depFull)
						}
						recordPkgRefPair(fullName, resolvedPkg)
						continue
					}
				}
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

	for _, pair := range pkgRefTargetPairs {
		fromFull, depPkg := pair[0], pair[1]
		allTransTargets := expandAllPkgTargets(depPkg)
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

	g.Roots = computeUnifiedRoots(ctx, g.Packages, g.PackageEdges, inspection.Needed)

	for _, pe := range g.PackageEdges {
		recordInternalVisibility(pe.From, pe.To, "")
	}

	if _, err := toposort.TopologicalSort(g.PackageDeps, func(deps []string) []string { return deps }); err != nil {
		cycle := toposort.FindCycle(g.PackageDeps, func(deps []string) []string { return deps })
		if len(cycle) > 0 {
			var details []string
			for i := 0; i+1 < len(cycle); i++ {
				acc := pkgEdgeMap[[2]string{cycle[i], cycle[i+1]}]
				if acc != nil && len(acc.via) > 0 {
					details = append(details, acc.via[0])
				}
			}
			msg := fmt.Sprintf("package circular dependency detected: %s", strings.Join(cycle, " -> "))
			if len(details) > 0 {
				msg = fmt.Sprintf("%s (%s)", msg, strings.Join(details, ", "))
			}
			g.Issues = append(g.Issues, GraphIssue{
				Severity: "error",
				Kind:     "package-cycle",
				Package:  cycle[0],
				Message:  msg,
			})
		}
	}

	if _, err := toposort.TopologicalSort(g.TargetDeps, func(deps []string) []string { return deps }); err != nil {
		cycle := toposort.FindCycle(g.TargetDeps, func(deps []string) []string { return deps })
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
	}

	return g, nil
}

func computeUnifiedRoots(ctx *RuntimeContext, pkgs []string, edges []PackageEdge, needed map[string]bool) []string {
	if rootOverride := configRootOverride(ctx); rootOverride != "" {
		if ctx.DepGraph.Packages[rootOverride] != nil {
			return []string{rootOverride}
		}
	}
	for _, name := range pkgs {
		node := ctx.DepGraph.Packages[name]
		if node != nil && node.IsLocal() && node.Pkg != nil && node.Pkg.IsRoot() {
			return []string{name}
		}
	}
	dependedOn := make(map[string]bool)
	for _, e := range edges {
		if needed[e.From] && needed[e.To] {
			dependedOn[e.To] = true
		}
	}
	var roots []string
	for _, name := range pkgs {
		node := ctx.DepGraph.Packages[name]
		if node == nil || !needed[name] || !node.IsLocal() || dependedOn[name] {
			continue
		}
		roots = append(roots, name)
	}
	if len(roots) == 0 {
		for _, name := range pkgs {
			node := ctx.DepGraph.Packages[name]
			if node != nil && needed[name] && node.IsLocal() {
				roots = append(roots, name)
			}
		}
	}
	sort.Strings(roots)
	return roots
}

func WhyQuery(ctx *RuntimeContext, g *UnifiedGraph, query string, allPaths bool) *WhyResult {
	if strings.Contains(query, ":") {
		return WhyTarget(ctx, g, query, allPaths)
	}
	return WhyPackage(ctx, g, query, allPaths)
}

func WhyPackage(ctx *RuntimeContext, g *UnifiedGraph, targetPkg string, allPaths bool) *WhyResult {
	res := &WhyResult{
		Query: targetPkg,
		Kind:  "package",
	}
	if ctx == nil || ctx.DepGraph == nil || ctx.DepGraph.Packages[targetPkg] == nil {
		res.Reason = fmt.Sprintf("package %q not found in workspace", targetPkg)
		return res
	}
	res.Exists = true
	if !g.Needed[targetPkg] {
		res.Reason = fmt.Sprintf("package %q is resolved in workspace but not reachable from active root(s): %s", targetPkg, strings.Join(g.Roots, ", "))
		return res
	}
	res.Needed = true
	if slices.Contains(g.Roots, targetPkg) {
		res.IsRoot = true
		res.Paths = [][]string{{targetPkg}}
		res.Reason = "root package"
		return res
	}

	viaLookup := make(map[[2]string][]string, len(g.PackageEdges))
	for _, pe := range g.PackageEdges {
		viaLookup[[2]string{pe.From, pe.To}] = pe.Via
	}

	paths := findGraphPaths(g.Roots, targetPkg, g.PackageDeps, allPaths)
	if len(paths) == 0 {
		res.Paths = [][]string{{targetPkg}}
		res.Reason = "needed local package (no incoming dependency edge)"
		return res
	}
	res.Paths = paths
	for _, p := range paths {
		steps := make([]WhyStep, 0, len(p)-1)
		for i := 0; i+1 < len(p); i++ {
			steps = append(steps, WhyStep{
				From: p[i],
				To:   p[i+1],
				Via:  viaLookup[[2]string{p[i], p[i+1]}],
			})
		}
		res.Steps = append(res.Steps, steps)
	}
	return res
}

func WhyTarget(ctx *RuntimeContext, g *UnifiedGraph, targetFull string, allPaths bool) *WhyResult {
	res := &WhyResult{
		Query: targetFull,
		Kind:  "target",
	}
	pkgName, targetName, ok := strings.Cut(targetFull, ":")
	if !ok || pkgName == "" || targetName == "" {
		res.Reason = fmt.Sprintf("invalid target reference %q (expected <pkg>:<target>)", targetFull)
		return res
	}
	if ctx == nil || ctx.DepGraph == nil || ctx.DepGraph.Packages[pkgName] == nil {
		res.Reason = fmt.Sprintf("package %q not found in workspace", pkgName)
		return res
	}
	node := ctx.DepGraph.Packages[pkgName]
	present := IsPackageSourcePresent(ctx, pkgName, node)
	if !g.Needed[pkgName] {
		if tMap, hasDecl := g.declaredTargets[pkgName]; hasDecl {
			if tMap[targetName] == nil {
				res.Reason = fmt.Sprintf("target %q not found in package %q", targetFull, pkgName)
				return res
			}
			res.Exists = true
		} else if present && node.Pkg != nil {
			dirs := &api.PkgDirs{SourceDir: ExpectedPackageSourceDir(ctx, pkgName, node)}
			var globalVals map[string]any
			if g.inspection != nil {
				if d := g.inspection.PkgDirs[pkgName]; d != nil {
					dirs = d
				}
				globalVals = g.inspection.GlobalValues
			}
			scriptfs.TakeBlockedWrites()
			buildCtx, declErr := DeclareTargets(ctx, pkgName, dirs, nil, globalVals)
			scriptfs.TakeBlockedWrites()
			if declErr == nil && buildCtx != nil {
				if _, exists := buildCtx.GetTargets()[targetName]; !exists {
					res.Reason = fmt.Sprintf("target %q not found in package %q", targetFull, pkgName)
					return res
				}
				res.Exists = true
			}
		}
		if !present {
			res.Reason = fmt.Sprintf("package %q is not reachable from active root(s): %s (and source tree is not materialized)", pkgName, strings.Join(g.Roots, ", "))
			return res
		}
		res.Reason = fmt.Sprintf("package %q is not reachable from active root(s): %s", pkgName, strings.Join(g.Roots, ", "))
		return res
	}
	res.Needed = true
	if !present {
		res.Reason = fmt.Sprintf("source tree for needed package %q is not materialized yet; cannot inspect target %q", pkgName, targetFull)
		return res
	}
	if !slices.Contains(g.Targets, targetFull) {
		res.Reason = fmt.Sprintf("target %q not found in package %q", targetFull, pkgName)
		return res
	}
	res.Exists = true

	targetOf := func(full string) *api.Target {
		owner, tName, ok := strings.Cut(full, ":")
		if !ok {
			return nil
		}
		return g.declaredTargets[owner][tName]
	}
	isDefaultTarget := func(full string) bool {
		t := targetOf(full)
		return t != nil && t.IsDefault() && !t.IsTest()
	}

	allInDeg := make(map[string]int, len(g.Targets))
	defaultInDeg := make(map[string]int, len(g.Targets))
	for _, e := range g.TargetEdges {
		allInDeg[e.To]++
		if isDefaultTarget(e.From) {
			defaultInDeg[e.To]++
		}
	}
	rootSet := make(map[string]bool, len(g.Roots))
	for _, r := range g.Roots {
		rootSet[r] = true
	}

	var startTargets []string
	for _, tFull := range g.Targets {
		owner, _, _ := strings.Cut(tFull, ":")
		if rootSet[owner] && isDefaultTarget(tFull) && defaultInDeg[tFull] == 0 {
			startTargets = append(startTargets, tFull)
		}
	}
	if len(startTargets) == 0 {
		for _, tFull := range g.Targets {
			owner, _, _ := strings.Cut(tFull, ":")
			if g.Needed[owner] && isDefaultTarget(tFull) && defaultInDeg[tFull] == 0 {
				startTargets = append(startTargets, tFull)
			}
		}
	}
	if len(startTargets) == 0 {
		for _, tFull := range g.Targets {
			owner, _, _ := strings.Cut(tFull, ":")
			if rootSet[owner] && isDefaultTarget(tFull) {
				startTargets = append(startTargets, tFull)
			}
		}
	}
	if len(startTargets) == 0 {
		for _, tFull := range g.Targets {
			owner, _, _ := strings.Cut(tFull, ":")
			if (rootSet[owner] || g.Needed[owner]) && allInDeg[tFull] == 0 {
				startTargets = append(startTargets, tFull)
			}
		}
	}
	if len(startTargets) == 0 {
		for _, tFull := range g.Targets {
			owner, _, _ := strings.Cut(tFull, ":")
			if rootSet[owner] || g.Needed[owner] {
				startTargets = append(startTargets, tFull)
			}
		}
	}
	sort.Strings(startTargets)

	if slices.Contains(startTargets, targetFull) {
		res.IsRoot = true
		res.Paths = [][]string{{targetFull}}
		res.Reason = "root target"
		return res
	}

	paths := findGraphPaths(startTargets, targetFull, g.TargetDeps, allPaths)
	if len(paths) == 0 {
		var testStarts []string
		for _, tFull := range g.Targets {
			owner, _, _ := strings.Cut(tFull, ":")
			t := targetOf(tFull)
			if g.Needed[owner] && t != nil && t.IsTest() && allInDeg[tFull] == 0 {
				testStarts = append(testStarts, tFull)
			}
		}
		sort.Strings(testStarts)
		if slices.Contains(testStarts, targetFull) {
			res.Paths = [][]string{{targetFull}}
			res.Reason = "test target (built by 'vmake test' or 'vmake build --tests')"
			return res
		}
		if testPaths := findGraphPaths(testStarts, targetFull, g.TargetDeps, allPaths); len(testPaths) > 0 {
			res.Paths = testPaths
			res.Reason = "reachable from test target (built by 'vmake test' or 'vmake build --tests')"
			for _, p := range testPaths {
				steps := make([]WhyStep, 0, len(p)-1)
				for i := 0; i+1 < len(p); i++ {
					steps = append(steps, WhyStep{From: p[i], To: p[i+1]})
				}
				res.Steps = append(res.Steps, steps)
			}
			return res
		}
		res.Paths = [][]string{{targetFull}}
		res.Reason = "declared target in needed package (not referenced by root target)"
		return res
	}
	res.Paths = paths
	for _, p := range paths {
		steps := make([]WhyStep, 0, len(p)-1)
		for i := 0; i+1 < len(p); i++ {
			steps = append(steps, WhyStep{From: p[i], To: p[i+1]})
		}
		res.Steps = append(res.Steps, steps)
	}
	return res
}

func findGraphPaths(roots []string, goal string, adj map[string][]string, allPaths bool) [][]string {
	if !allPaths {
		type item struct {
			node string
			path []string
		}
		visited := make(map[string]bool)
		var queue []item
		for _, r := range roots {
			if r == goal {
				return [][]string{{goal}}
			}
			visited[r] = true
			queue = append(queue, item{node: r, path: []string{r}})
		}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, next := range adj[cur.node] {
				nextPath := append(append([]string(nil), cur.path...), next)
				if next == goal {
					return [][]string{nextPath}
				}
				if !visited[next] {
					visited[next] = true
					queue = append(queue, item{node: next, path: nextPath})
				}
			}
		}
		return nil
	}

	const maxPaths = 16
	var results [][]string
	onStack := make(map[string]bool)
	var dfs func(u string, path []string)
	dfs = func(u string, path []string) {
		if len(results) >= maxPaths {
			return
		}
		if u == goal {
			results = append(results, append([]string(nil), path...))
			return
		}
		onStack[u] = true
		for _, v := range adj[u] {
			if !onStack[v] {
				dfs(v, append(path, v))
				if len(results) >= maxPaths {
					break
				}
			}
		}
		onStack[u] = false
	}
	for _, r := range roots {
		dfs(r, []string{r})
		if len(results) >= maxPaths {
			break
		}
	}
	return results
}

func CheckProjectDeps(ctx *RuntimeContext, inspection *Inspection) (*DepsCheckReport, error) {
	var err error
	if inspection == nil {
		inspection, err = InspectWithOptions(ctx, InspectOptions{FilterDeps: true})
		if err != nil {
			return nil, err
		}
	}
	g, err := BuildUnifiedGraph(ctx, inspection, false)
	if err != nil {
		return nil, err
	}

	report := &DepsCheckReport{
		Roots:            g.Roots,
		PackageCount:     len(g.Packages),
		TargetCount:      len(g.Targets),
		PackageEdgeCount: len(g.PackageEdges),
		TargetEdgeCount:  len(g.TargetEdges),
		Issues:           append([]GraphIssue{}, g.Issues...),
	}

	if len(report.Issues) == 0 && len(g.declaredTargets) > 0 {
		var subParents map[string]string
		if ctx.Resolver != nil {
			subParents = ctx.Resolver.SubParents()
		}
		targetsForCheck := make(map[string]map[string]*api.Target, len(g.declaredTargets))
		for pkgName, tMap := range g.declaredTargets {
			copied := make(map[string]*api.Target, len(tMap))
			for k, v := range tMap {
				copied[k] = v
			}
			targetsForCheck[pkgName] = copied
		}
		if len(g.unmaterialized) > 0 {
			resolveCheckPkg := func(fromPkg, ref string) string {
				if ref == "" || strings.Contains(ref, "/") {
					return ref
				}
				if g.Needed[fromPkg] {
					if cand := api.ResolveSubPackageName(fromPkg, ref, subParents, func(c string) bool {
						return g.Needed[c]
					}); cand != ref || g.Needed[cand] {
						return cand
					}
				}
				return api.ResolveSubPackageName(fromPkg, ref, subParents, func(cand string) bool {
					_, ok := ctx.DepGraph.Packages[cand]
					return ok
				})
			}
			for pkgName, tMap := range g.declaredTargets {
				for _, t := range tMap {
					for _, dep := range t.Deps() {
						pkgRef, targetSpec, ok := strings.Cut(dep, ":")
						if !ok || targetSpec == "" || targetSpec == "*" {
							continue
						}
						resolvedPkg := resolveCheckPkg(pkgName, pkgRef)
						if g.unmaterialized[resolvedPkg] {
							if targetsForCheck[resolvedPkg] == nil {
								targetsForCheck[resolvedPkg] = make(map[string]*api.Target)
							}
							if targetsForCheck[resolvedPkg][targetSpec] == nil {
								targetsForCheck[resolvedPkg][targetSpec] = api.NewTargetRegistry().Target(targetSpec).SetKind(api.TargetVoid)
							}
						}
					}
				}
			}
		}
		bg, bgErr := build.NewBuildGraph(targetsForCheck, g.pkgMeta, subParents)
		if bgErr != nil {
			report.Issues = append(report.Issues, GraphIssue{
				Severity: "error",
				Kind:     "build-graph",
				Message:  bgErr.Error(),
			})
		} else {
			packages := make(map[string]*api.Package, len(ctx.DepGraph.Packages))
			for name, node := range ctx.DepGraph.Packages {
				if node != nil && node.Pkg != nil {
					packages[name] = node.Pkg
				}
			}
			rootDir := ctx.WorkDir
			if ctx.Paths != nil && ctx.Paths.ProjectDir != "" {
				rootDir = ctx.Paths.ProjectDir
			}
			for _, v := range build.CheckExistingBuildBoundaries(bg, inspection.Layouts, packages, workspacePackageDirs(ctx), rootDir, inspection.Mode) {
				report.Issues = append(report.Issues, GraphIssue{
					Severity: "error",
					Kind:     v.Kind,
					Package:  v.Package,
					Target:   v.Target,
					Message:  v.Message,
				})
			}
		}
	}

	return report, nil
}
