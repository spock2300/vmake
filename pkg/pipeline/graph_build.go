package pipeline

import (
	"fmt"
	"slices"
	"sort"

	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

func BuildUnifiedGraph(ctx *RuntimeContext, inspection *Inspection, opts GraphOptions) (*UnifiedGraph, error) {
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
		Needed:          inspection.needed,
		PackageDeps:     make(map[string][]string),
		TargetDeps:      make(map[string][]string),
		declaredTargets: make(map[string]map[string]*api.Target),
		declaredCtxs:    make(map[string]*api.BuildContext),
		pkgMeta:         make(map[string]build.PkgBuildMeta),
		unmaterialized:  make(map[string]bool),
		inspection:      inspection,
	}

	included := make(map[string]bool)
	for name, node := range ctx.DepGraph.Packages {
		if node == nil || node.Source == nil {
			continue
		}
		if opts.IncludeAll || inspection.needed[name] {
			included[name] = true
			g.Packages = append(g.Packages, name)
		}
	}
	sort.Strings(g.Packages)

	resolvePkg := func(fromPkg, ref string) string {
		return ResolvePackageRef(ctx, inspection.needed, fromPkg, ref)
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
		if node.Pkg != nil && present && opts.DeclareTargets {
			dirs := inspection.pkgDirs[name]
			if dirs == nil {
				dirs = &api.PkgDirs{SourceDir: ExpectedPackageSourceDir(ctx, name, node)}
			}
			scriptfs.TakeBlockedWrites()
			buildCtx, declErr := DeclareTargets(ctx, name, dirs, inspection.packageToolchains[name], inspection.GlobalValues)
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
				g.declaredCtxs[name] = buildCtx
				targets := make(map[string]*api.Target)
				for tName, t := range buildCtx.GetTargets() {
					targets[tName] = api.SnapshotTarget(t)
				}
				if len(targets) > 0 {
					g.declaredTargets[name] = targets
				}
				for _, sub := range api.BindSubGraphs(buildCtx) {
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
	validator := newGraphValidator(inspection.needed, visPathOf, func(issue GraphIssue) {
		g.Issues = append(g.Issues, issue)
	})

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
			if !validator.crossPkgOnBuild(name, sub, "", via) {
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
				ref := build.ParseDependencyRef(dep)
				if ref.HasTarget {
					pkgRef, targetSpec := ref.Package, ref.Target
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
						if !validator.crossPkgOnBuild(pkgName, resolvedPkg, fullName, dep) {
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
					if depNode != nil && !validator.crossPkgOnBuild(pkgName, resolvedPkg, fullName, dep) {
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

				if ref.IsPackagePath() {
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
					if !validator.crossPkgOnBuild(pkgName, dep, fullName, dep) {
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

				samePkgTarget := pkgName + ":" + ref.Package
				if targetExists[samePkgTarget] {
					addTargetEdge(fullName, samePkgTarget)
					continue
				}
				resolvedPkg := resolvePkg(pkgName, ref.Package)
				if resolvedPkg != pkgName {
					if _, ok := ctx.DepGraph.Packages[resolvedPkg]; ok {
						if !validator.crossPkgOnBuild(pkgName, resolvedPkg, fullName, dep) {
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

	g.Roots = RootsFromEdges(ctx, g.Packages, g.PackageEdges, inspection.needed)

	for _, pe := range g.PackageEdges {
		validator.internalVisibility(pe.From, pe.To, "")
	}

	g.Issues = append(g.Issues, checkGraphCycles(g, func(from, to string) []string {
		if acc := pkgEdgeMap[[2]string{from, to}]; acc != nil {
			return acc.via
		}
		return nil
	})...)

	return g, nil
}
