package pipeline

import (
	"fmt"
	"strings"

	"github.com/spock2300/vmake/internal/pkgvis"
	"github.com/spock2300/vmake/internal/toposort"
)

// graphValidator owns the cross-package rules applied while the unified graph
// is built. It reports issues through addIssue so the rules can be exercised
// without a project, a toolchain or an interpreted build script.
type graphValidator struct {
	needed   NeededSet
	visPath  func(string) string
	addIssue func(GraphIssue)
	seen     map[[2]string]bool
}

func newGraphValidator(needed NeededSet, visPath func(string) string, addIssue func(GraphIssue)) *graphValidator {
	return &graphValidator{
		needed:   needed,
		visPath:  visPath,
		addIssue: addIssue,
		seen:     make(map[[2]string]bool),
	}
}

// internalVisibility reports a visibility violation at most once per package
// pair: the same pair can be reached from several targets.
func (v *graphValidator) internalVisibility(fromPkg, toPkg, targetFull string) {
	if fromPkg == "" || toPkg == "" || fromPkg == toPkg {
		return
	}
	pair := [2]string{fromPkg, toPkg}
	if v.seen[pair] {
		return
	}
	err := pkgvis.CheckInternal(fromPkg, v.visPathOf(fromPkg), toPkg, v.visPathOf(toPkg))
	if err == nil {
		return
	}
	v.seen[pair] = true
	v.addIssue(GraphIssue{
		Severity: "error",
		Kind:     "internal-visibility",
		Package:  fromPkg,
		Target:   targetFull,
		Message:  err.Error(),
	})
}

// crossPkgOnBuild reports an OnBuild reference from a needed package to a
// package that OnRequire never pulled in, and answers whether the reference may
// become an edge. The rule deliberately stops at one hop: the reference itself
// is already an error, so the edge is not materialized.
func (v *graphValidator) crossPkgOnBuild(fromPkg, toPkg, targetFull, refDesc string) bool {
	if fromPkg == "" || toPkg == "" || fromPkg == toPkg {
		return true
	}
	if !v.needed[fromPkg] || v.needed[toPkg] {
		return true
	}
	v.internalVisibility(fromPkg, toPkg, targetFull)
	v.addIssue(GraphIssue{
		Severity: "error",
		Kind:     "missing-dependency",
		Package:  fromPkg,
		Target:   targetFull,
		Message: fmt.Sprintf("package %s references %q (%s) in OnBuild without declaring AddRequires(%q) in OnRequire",
			fromPkg, toPkg, refDesc, toPkg),
	})
	return false
}

func (v *graphValidator) visPathOf(pkg string) string {
	if v == nil || v.visPath == nil {
		return ""
	}
	return v.visPath(pkg)
}

// checkGraphCycles reports the package and target cycles in an assembled
// graph. viaOf supplies the edge labels that explain a package cycle.
func checkGraphCycles(g *UnifiedGraph, viaOf func(from, to string) []string) []GraphIssue {
	identity := func(deps []string) []string { return deps }
	var issues []GraphIssue

	if _, err := toposort.TopologicalSort(g.PackageDeps, identity); err != nil {
		cycle := toposort.FindCycle(g.PackageDeps, identity)
		if len(cycle) > 0 {
			var details []string
			for i := 0; i+1 < len(cycle); i++ {
				if via := viaOf(cycle[i], cycle[i+1]); len(via) > 0 {
					details = append(details, via[0])
				}
			}
			msg := fmt.Sprintf("package circular dependency detected: %s", strings.Join(cycle, " -> "))
			if len(details) > 0 {
				msg = fmt.Sprintf("%s (%s)", msg, strings.Join(details, ", "))
			}
			issues = append(issues, GraphIssue{
				Severity: "error",
				Kind:     "package-cycle",
				Package:  cycle[0],
				Message:  msg,
			})
		}
	}

	if _, err := toposort.TopologicalSort(g.TargetDeps, identity); err != nil {
		cycle := toposort.FindCycle(g.TargetDeps, identity)
		if len(cycle) > 0 {
			ownerPkg, _, _ := strings.Cut(cycle[0], ":")
			issues = append(issues, GraphIssue{
				Severity: "error",
				Kind:     "target-cycle",
				Package:  ownerPkg,
				Target:   cycle[0],
				Message:  fmt.Sprintf("target circular dependency detected: %s", strings.Join(cycle, " -> ")),
			})
		}
	}

	return issues
}
