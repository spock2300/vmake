package pipeline

import (
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

func CheckProjectDeps(ctx *RuntimeContext, inspection *Inspection) (*DepsCheckReport, error) {
	var err error
	if inspection == nil {
		inspection, err = InspectWithOptions(ctx, InspectOptions{FilterDeps: true})
		if err != nil {
			return nil, err
		}
	}
	g, err := BuildUnifiedGraph(ctx, inspection, GraphOptions{DeclareTargets: true})
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
				return ResolvePackageRef(ctx, g.Needed, fromPkg, ref)
			}
			for pkgName, tMap := range g.declaredTargets {
				for _, t := range tMap {
					for _, dep := range t.Deps() {
						ref := build.ParseDependencyRef(dep)
						pkgRef, targetSpec, ok := ref.Package, ref.Target, ref.HasTarget
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
			for _, v := range build.CheckExistingBuildBoundaries(bg, inspection.layouts, packages, workspacePackageDirs(ctx), rootDir, inspection.Mode) {
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
