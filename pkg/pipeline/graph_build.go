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
	allWorkspacePkgs := make([]string, 0, len(ctx.DepGraph.Packages))
	for name, node := range ctx.DepGraph.Packages {
		if node == nil || node.Source == nil {
			continue
		}
		allWorkspacePkgs = append(allWorkspacePkgs, name)
		if opts.IncludeAll || inspection.needed[name] {
			included[name] = true
		}
	}
	sort.Strings(allWorkspacePkgs)

	resolvePkg := func(fromPkg, ref string) string {
		return ResolvePackageRef(ctx, inspection.needed, fromPkg, ref)
	}

	for _, name := range allWorkspacePkgs {
		node := ctx.DepGraph.Packages[name]
		visPath := ""
		if ctx.Resolver != nil {
			visPath = ctx.Resolver.VisibilityPath(name)
		}
		present := IsPackageSourcePresent(ctx, name, node)
		meta := pkgBuildMetaFor(ctx, node, visPath, !present)
		if !present && included[name] {
			g.unmaterialized[name] = true
		}
		if included[name] && node.Pkg != nil && present && opts.DeclareTargets {
			dirs := inspection.pkgDirs[name]
			if dirs == nil {
				dirs = &api.PkgDirs{SourceDir: ExpectedPackageSourceDir(ctx, name, node)}
			}
			scriptfs.TakeBlockedWrites()
			buildCtx, declErr := DeclareTargets(ctx, name, dirs, inspection.packageToolchains[name], inspection.GlobalValues)
			if blocked := scriptfs.TakeBlockedWrites(); len(blocked) > 0 {
				if opts.OnBlockedWrite != nil {
					opts.OnBlockedWrite(name, blocked)
				}
				if declErr == nil {
					declErr = fmt.Errorf("%s", blocked[0])
				}
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

	bg := build.AuditBuildGraph(g.declaredTargets, g.pkgMeta, build.GraphAuditOptions{
		Needed:   inspection.needed,
		Included: included,
	})
	g.buildGraph = bg
	g.Packages = bg.Packages
	g.PackageDeps = bg.PackageDeps
	g.PackageEdges = bg.PackageEdges
	g.Targets = bg.Targets
	g.TargetDeps = bg.TargetDeps
	g.TargetEdges = bg.TargetEdges
	g.Issues = append(g.Issues, bg.Issues...)
	g.Roots = RootsFromEdges(ctx, g.Packages, g.PackageEdges, inspection.needed)

	return g, nil
}
