package pipeline

import (
	"strings"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

type PackageEdge = build.PackageEdge

type TargetEdge = build.TargetEdge

type GraphIssue = build.GraphIssue

type UnifiedGraph struct {
	Roots        []string            `json:"roots"`
	Needed       NeededSet           `json:"-"`
	Packages     []string            `json:"packages"`
	PackageDeps  map[string][]string `json:"packageDeps"`
	PackageEdges []PackageEdge       `json:"packageEdges"`
	Targets      []string            `json:"targets"`
	TargetDeps   map[string][]string `json:"targetDeps"`
	TargetEdges  []TargetEdge        `json:"targetEdges"`
	Issues       []GraphIssue        `json:"issues,omitempty"`

	declaredTargets map[string]map[string]*api.Target
	declaredCtxs    map[string]*api.BuildContext
	pkgMeta         map[string]build.PkgBuildMeta
	unmaterialized  map[string]bool
	buildGraph      *build.BuildGraph
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

// DeclaredTargets returns the targets declared for a package, if any.
func (g *UnifiedGraph) DeclaredTargets(name string) map[string]*api.Target {
	if g == nil {
		return nil
	}
	return g.declaredTargets[name]
}

// BuildContext returns the declaration context of a package, which carries the
// install items and sub-graph registrations that targets alone do not.
func (g *UnifiedGraph) BuildContext(name string) (*api.BuildContext, bool) {
	if g == nil {
		return nil, false
	}
	bc, ok := g.declaredCtxs[name]
	return bc, ok
}

// GraphFacts is the stable answer of a built graph: the edges, roots and
// issues a consumer renders. It deliberately omits construction state
// (declarations, per-package meta, validator input) so callers cannot depend on
// how the graph was computed, only on what it says.
type GraphFacts struct {
	Packages     []string
	Needed       NeededSet
	Roots        []string
	Targets      []string
	PackageEdges []PackageEdge
	TargetEdges  []TargetEdge
	PackageDeps  map[string][]string
	Issues       []GraphIssue
}

// Facts snapshots the graph. Slices and maps are copied so a renderer cannot
// mutate the graph it reports on.
func (g *UnifiedGraph) Facts() GraphFacts {
	if g == nil {
		return GraphFacts{}
	}
	deps := make(map[string][]string, len(g.PackageDeps))
	for name, list := range g.PackageDeps {
		deps[name] = append([]string(nil), list...)
	}
	return GraphFacts{
		Packages:     append([]string(nil), g.Packages...),
		Needed:       g.Needed.Clone(),
		Roots:        append([]string(nil), g.Roots...),
		Targets:      append([]string(nil), g.Targets...),
		PackageEdges: append([]PackageEdge(nil), g.PackageEdges...),
		TargetEdges:  append([]TargetEdge(nil), g.TargetEdges...),
		PackageDeps:  deps,
		Issues:       append([]GraphIssue(nil), g.Issues...),
	}
}

// ResolvePackageRef resolves a package reference made from fromPkg. Bare names
// are matched against known sub-packages first (gated by needed) and then
// against the resolved graph; references containing "/" are returned as-is.
// It is the single implementation behind the graph, why and inspect edge
// builders.
func ResolvePackageRef(ctx *RuntimeContext, needed NeededSet, fromPkg, ref string) string {
	if ref == "" || strings.Contains(ref, "/") {
		return ref
	}
	var subParents map[string]string
	if ctx != nil && ctx.Resolver != nil {
		subParents = ctx.Resolver.SubParents()
	}
	if needed.Has(fromPkg) {
		if cand := api.ResolveSubPackageName(fromPkg, ref, subParents, func(c string) bool {
			return needed.Has(c)
		}); cand != ref || needed.Has(cand) {
			return cand
		}
	}
	return api.ResolveSubPackageName(fromPkg, ref, subParents, func(cand string) bool {
		if ctx == nil || ctx.DepGraph == nil {
			return false
		}
		_, ok := ctx.DepGraph.Packages[cand]
		return ok
	})
}

// GraphOptions selects what the unified graph computes. DeclareTargets runs the
// OnBuild callbacks, so callers that only need package edges can turn it off.
type GraphOptions struct {
	IncludeAll     bool
	DeclareTargets bool
}
