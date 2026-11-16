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

	if len(report.Issues) == 0 && len(g.declaredTargets) > 0 && g.buildGraph != nil {
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
		for _, v := range build.CheckExistingBuildBoundaries(g.buildGraph, inspection.layouts, packages, workspacePackageDirs(ctx), rootDir, inspection.Mode) {
			report.Issues = append(report.Issues, GraphIssue{
				Severity: "error",
				Kind:     v.Kind,
				Package:  v.Package,
				Target:   v.Target,
				Message:  v.Message,
			})
		}
	}

	return report, nil
}
