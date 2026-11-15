package pipeline

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

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
			// The package is reachable in the workspace but not needed, so the
			// graph did not declare it. The inspection does not resolve toolchains
			// for filtered packages, so the probe may run with a nil toolchain; the
			// declared targets are still the ones the package would report.
			dirs := &api.PkgDirs{SourceDir: ExpectedPackageSourceDir(ctx, pkgName, node)}
			var globalVals map[string]any
			var tc *toolchain.Toolchain
			if g.inspection != nil {
				if view, ok := g.inspection.Package(pkgName); ok && !view.Skipped {
					dirs = view.Dirs
					tc = view.Toolchain
				}
				globalVals = g.inspection.GlobalValues
			}
			scriptfs.TakeBlockedWrites()
			buildCtx, declErr := DeclareTargets(ctx, pkgName, dirs, tc, globalVals)
			if blocked := scriptfs.TakeBlockedWrites(); len(blocked) > 0 && declErr == nil {
				declErr = fmt.Errorf("%s", blocked[0])
			}
			if declErr != nil {
				message := fmt.Sprintf("cannot declare targets for %s: %v", pkgName, declErr)
				g.Issues = append(g.Issues, GraphIssue{
					Severity: "error",
					Kind:     "target-declare",
					Package:  pkgName,
					Target:   targetFull,
					Message:  message,
				})
				res.Reason = message
				return res
			}
			if buildCtx != nil {
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
