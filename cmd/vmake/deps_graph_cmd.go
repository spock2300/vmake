package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	iexec "github.com/spock2300/vmake/internal/exec"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

type depsGraphEnvelope struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Command       string                 `json:"command"`
	Roots         []string               `json:"roots"`
	Packages      []string               `json:"packages"`
	PackageEdges  []pipeline.PackageEdge `json:"packageEdges"`
	Targets       []string               `json:"targets"`
	TargetEdges   []pipeline.TargetEdge  `json:"targetEdges"`
	Issues        []pipeline.GraphIssue  `json:"issues,omitempty"`
}

type depsWhyEnvelope struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Command       string                `json:"command"`
	Query         string                `json:"query"`
	Kind          string                `json:"kind"`
	Exists        bool                  `json:"exists"`
	Needed        bool                  `json:"needed"`
	IsRoot        bool                  `json:"isRoot"`
	Paths         [][]string            `json:"paths,omitempty"`
	Steps         [][]pipeline.WhyStep  `json:"steps,omitempty"`
	Reason        string                `json:"reason,omitempty"`
	Issues        []pipeline.GraphIssue `json:"issues,omitempty"`
}

type depsCheckEnvelope struct {
	SchemaVersion    int                   `json:"schemaVersion"`
	Command          string                `json:"command"`
	Status           string                `json:"status"`
	Roots            []string              `json:"roots"`
	PackageCount     int                   `json:"packageCount"`
	TargetCount      int                   `json:"targetCount"`
	PackageEdgeCount int                   `json:"packageEdgeCount"`
	TargetEdgeCount  int                   `json:"targetEdgeCount"`
	Issues           []pipeline.GraphIssue `json:"issues"`
}

func newDepsGraphCmd() *cobra.Command {
	var targetLevel bool
	var format string
	var includeAll bool
	var jsonOut bool
	var locked bool
	var offline bool
	var setFlags []string

	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Print the unified package or target dependency graph",
		Long: `Print the unified dependency graph combining OnRequire, OnBuild (AddDeps and
UseRequires) and BuildSubGraph edges without building or fetching sources.

By default, each line prints "<from> <to>" at package granularity (matching
'go mod graph'). Pass --targets for "<pkg>:<target>" granularity, or --format
dot|mermaid to render Graphviz DOT or Mermaid flowcharts.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut && cmd.Flags().Changed("format") {
				return &usageError{err: fmt.Errorf("--json cannot be combined with --format")}
			}
			if jsonOut && targetLevel {
				return &usageError{err: fmt.Errorf("--json cannot be combined with --targets")}
			}
			switch format {
			case "text", "dot", "mermaid":
			default:
				return &usageError{err: fmt.Errorf("invalid --format %q (expected text, dot, or mermaid)", format)}
			}
			overrides, err := parseInspectOverrides(setFlags)
			if err != nil {
				return &usageError{err: err}
			}
			if findProjectDirSoft() == "" {
				return errors.New("not inside a vmake project (no .vmake/, build.go or */build.go found in the current directory or any parent directory)")
			}
			if jsonOut || (!verbose && !veryVerbose) {
				vlog.SetLevel(vlog.Quiet)
				iexec.SetQuiet(true)
			}
			defer queryReadOnly()()

			restoreStdout := silenceStdout()
			defer restoreStdout()
			restoreStderr := func() {}
			if jsonOut {
				restoreStderr = silenceStderr()
				defer restoreStderr()
			}

			policy := resolver.LockPolicy{
				DryRun:        true,
				Locked:        locked,
				StrictWrapper: locked,
				Offline:       offline,
			}
			ctx, err := openProject(projectOptions{execution: cmd.Context(), policy: &policy, overrides: overrides, skipGitignore: true})
			if err != nil {
				restoreStdout()
				restoreStderr()
				return unwrapDepsOpenErr(err)
			}
			defer closeProject(ctx)

			inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true})
			if err != nil {
				restoreStdout()
				restoreStderr()
				return err
			}
			g, err := pipeline.BuildUnifiedGraph(ctx, inspection, includeAll)
			restoreStdout()
			restoreStderr()
			if err != nil {
				return err
			}

			if jsonOut {
				return writeDepsJSON(cmd.OutOrStdout(), &depsGraphEnvelope{
					SchemaVersion: 1,
					Command:       "deps graph",
					Roots:         nonNilStrings(g.Roots),
					Packages:      nonNilStrings(g.Packages),
					PackageEdges:  nonNilPackageEdges(g.PackageEdges),
					Targets:       nonNilStrings(g.Targets),
					TargetEdges:   nonNilTargetEdges(g.TargetEdges),
					Issues:        g.Issues,
				})
			}
			warnTargetDeclareIssues(cmd.ErrOrStderr(), g.Issues)
			renderDepsGraph(cmd.OutOrStdout(), g, targetLevel, format)
			return nil
		},
	}
	cmd.Flags().BoolVar(&targetLevel, "targets", false, "print target-level (<pkg>:<target>) dependency edges")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text, dot, or mermaid")
	cmd.Flags().BoolVar(&includeAll, "all", false, "include resolved workspace packages not needed by active roots")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print machine-readable JSON")
	cmd.Flags().BoolVar(&locked, "locked", false, "require .vmake/vmake.lock to be up-to-date")
	cmd.Flags().BoolVar(&offline, "offline", false, "resolve strictly from local cache without network access")
	cmd.Flags().StringArrayVarP(&setFlags, "set", "s", nil, "override an option for this run only (format: [pkg/]option=value)")
	cmd.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"text", "dot", "mermaid"}, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func newDepsWhyCmd() *cobra.Command {
	var allPaths bool
	var jsonOut bool
	var locked bool
	var offline bool
	var setFlags []string

	cmd := &cobra.Command{
		Use:   "why <pkg|pkg:target>",
		Short: "Explain why a package or target is needed by the root build",
		Long: `Show the dependency chain from active root package(s) or target(s) to the
queried package or <pkg>:<target>, including the OnRequire, BuildSubGraph or
OnBuild target edge that introduced each hop.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeDepsWhyArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(args[0])
			if query == "" {
				return &usageError{err: fmt.Errorf("empty package or target query")}
			}
			overrides, err := parseInspectOverrides(setFlags)
			if err != nil {
				return &usageError{err: err}
			}
			if findProjectDirSoft() == "" {
				return errors.New("not inside a vmake project (no .vmake/, build.go or */build.go found in the current directory or any parent directory)")
			}
			if jsonOut || (!verbose && !veryVerbose) {
				vlog.SetLevel(vlog.Quiet)
				iexec.SetQuiet(true)
			}
			defer queryReadOnly()()

			restoreStdout := silenceStdout()
			defer restoreStdout()
			restoreStderr := func() {}
			if jsonOut {
				restoreStderr = silenceStderr()
				defer restoreStderr()
			}

			policy := resolver.LockPolicy{
				DryRun:        true,
				Locked:        locked,
				StrictWrapper: locked,
				Offline:       offline,
			}
			ctx, err := openProject(projectOptions{execution: cmd.Context(), policy: &policy, overrides: overrides, skipGitignore: true})
			if err != nil {
				restoreStdout()
				restoreStderr()
				return unwrapDepsOpenErr(err)
			}
			defer closeProject(ctx)

			inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true})
			if err != nil {
				restoreStdout()
				restoreStderr()
				return err
			}
			g, err := pipeline.BuildUnifiedGraph(ctx, inspection, false)
			if err != nil {
				restoreStdout()
				restoreStderr()
				return err
			}
			res := pipeline.WhyQuery(ctx, g, query, allPaths)
			restoreStdout()
			restoreStderr()

			if jsonOut {
				if err := writeDepsJSON(cmd.OutOrStdout(), &depsWhyEnvelope{
					SchemaVersion: 1,
					Command:       "deps why",
					Query:         res.Query,
					Kind:          res.Kind,
					Exists:        res.Exists,
					Needed:        res.Needed,
					IsRoot:        res.IsRoot,
					Paths:         res.Paths,
					Steps:         res.Steps,
					Reason:        res.Reason,
					Issues:        g.Issues,
				}); err != nil {
					return err
				}
				if !res.Exists {
					return errors.New(res.Reason)
				}
				return nil
			}
			warnTargetDeclareIssues(cmd.ErrOrStderr(), g.Issues)
			if !res.Exists {
				return errors.New(res.Reason)
			}
			renderDepsWhy(cmd.OutOrStdout(), res)
			return nil
		},
	}
	cmd.Flags().BoolVar(&allPaths, "all", false, "show multiple dependency paths instead of only the shortest path")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print machine-readable JSON")
	cmd.Flags().BoolVar(&locked, "locked", false, "require .vmake/vmake.lock to be up-to-date")
	cmd.Flags().BoolVar(&offline, "offline", false, "resolve strictly from local cache without network access")
	cmd.Flags().StringArrayVarP(&setFlags, "set", "s", nil, "override an option for this run only (format: [pkg/]option=value)")
	return cmd
}

func newDepsCheckCmd() *cobra.Command {
	var jsonOut bool
	var locked bool
	var offline bool
	var setFlags []string

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Verify DAG acyclicity, internal/ visibility, and header boundaries",
		Long: `Audit the project dependency architecture against VMake's Go-style rules:
  1. Package-level and target-level DAG acyclicity (including cross-package
     reverse target edges)
  2. Go-style internal/ directory visibility across local and remote packages
  3. Declared target dependency existence and self-contained package paths
  4. C/C++ public and direct header boundary enforcement (via .d depfiles when
     built, or static #include scanning on cold trees)

Exits 0 when clean and 1 when any violation is detected.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			overrides, err := parseInspectOverrides(setFlags)
			if err != nil {
				return &usageError{err: err}
			}
			if findProjectDirSoft() == "" {
				noProjErr := errors.New("not inside a vmake project (no .vmake/, build.go or */build.go found in the current directory or any parent directory)")
				if jsonOut {
					return finishDepsCheck(cmd.OutOrStdout(), true, &pipeline.DepsCheckReport{
						Issues: []pipeline.GraphIssue{classifyResolveIssue(noProjErr)},
					})
				}
				return noProjErr
			}
			if jsonOut || (!verbose && !veryVerbose) {
				vlog.SetLevel(vlog.Quiet)
				iexec.SetQuiet(true)
			}
			defer queryReadOnly()()

			restoreStdout := silenceStdout()
			defer restoreStdout()
			restoreStderr := func() {}
			if jsonOut {
				restoreStderr = silenceStderr()
				defer restoreStderr()
			}

			policy := resolver.LockPolicy{
				DryRun:        true,
				Locked:        locked,
				StrictWrapper: locked,
				Offline:       offline,
			}
			ctx, err := openProject(projectOptions{execution: cmd.Context(), policy: &policy, overrides: overrides, skipGitignore: true})
			if err != nil {
				restoreStdout()
				restoreStderr()
				var overrideErr *optionOverrideError
				if errors.As(err, &overrideErr) {
					return &usageError{err: overrideErr.err}
				}
				issue := classifyResolveIssue(err)
				return finishDepsCheck(cmd.OutOrStdout(), jsonOut, &pipeline.DepsCheckReport{
					Issues: []pipeline.GraphIssue{issue},
				})
			}
			defer closeProject(ctx)

			inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true})
			if err != nil {
				restoreStdout()
				restoreStderr()
				issue := classifyResolveIssue(err)
				return finishDepsCheck(cmd.OutOrStdout(), jsonOut, &pipeline.DepsCheckReport{
					Issues: []pipeline.GraphIssue{issue},
				})
			}
			report, err := pipeline.CheckProjectDeps(ctx, inspection)
			restoreStdout()
			restoreStderr()
			if err != nil {
				issue := classifyResolveIssue(err)
				return finishDepsCheck(cmd.OutOrStdout(), jsonOut, &pipeline.DepsCheckReport{
					Issues: []pipeline.GraphIssue{issue},
				})
			}
			return finishDepsCheck(cmd.OutOrStdout(), jsonOut, report)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print machine-readable JSON")
	cmd.Flags().BoolVar(&locked, "locked", false, "require .vmake/vmake.lock to be up-to-date")
	cmd.Flags().BoolVar(&offline, "offline", false, "resolve strictly from local cache without network access")
	cmd.Flags().StringArrayVarP(&setFlags, "set", "s", nil, "override an option for this run only (format: [pkg/]option=value)")
	return cmd
}

func unwrapDepsOpenErr(err error) error {
	var overrideErr *optionOverrideError
	if errors.As(err, &overrideErr) {
		return &usageError{err: overrideErr.err}
	}
	return err
}

func warnTargetDeclareIssues(w io.Writer, issues []pipeline.GraphIssue) {
	for _, iss := range issues {
		if iss.Kind == "target-declare" {
			fmt.Fprintf(w, "Warning: %s\n", iss.Message)
		}
	}
}

func classifyResolveIssue(err error) pipeline.GraphIssue {
	msg := err.Error()
	kind := "resolve"
	switch {
	case strings.Contains(msg, "circular dependency"):
		kind = "package-cycle"
	case strings.Contains(msg, "internal package"):
		kind = "internal-visibility"
	}
	return pipeline.GraphIssue{
		Severity: "error",
		Kind:     kind,
		Message:  msg,
	}
}

func finishDepsCheck(w io.Writer, jsonOut bool, report *pipeline.DepsCheckReport) error {
	issues := report.Issues
	if issues == nil {
		issues = []pipeline.GraphIssue{}
	}
	status := "ok"
	if len(issues) > 0 {
		status = "error"
	}
	if jsonOut {
		if err := writeDepsJSON(w, &depsCheckEnvelope{
			SchemaVersion:    1,
			Command:          "deps check",
			Status:           status,
			Roots:            nonNilStrings(report.Roots),
			PackageCount:     report.PackageCount,
			TargetCount:      report.TargetCount,
			PackageEdgeCount: report.PackageEdgeCount,
			TargetEdgeCount:  report.TargetEdgeCount,
			Issues:           issues,
		}); err != nil {
			return err
		}
		if len(issues) > 0 {
			return fmt.Errorf("found %d dependency architecture issue(s)", len(issues))
		}
		return nil
	}

	if len(issues) == 0 {
		fmt.Fprintf(w, "Dependency graph OK: %d package(s), %d target(s), %d package edge(s), %d target edge(s)\n",
			report.PackageCount, report.TargetCount, report.PackageEdgeCount, report.TargetEdgeCount)
		return nil
	}

	for _, iss := range issues {
		fmt.Fprintf(w, "[%s] %s\n", iss.Kind, iss.Message)
	}
	return fmt.Errorf("found %d dependency architecture issue(s)", len(issues))
}

func renderDepsGraph(w io.Writer, g *pipeline.UnifiedGraph, targetLevel bool, format string) {
	var nodes []string
	var edges [][2]string
	if targetLevel {
		nodes = g.Targets
		edges = make([][2]string, 0, len(g.TargetEdges))
		for _, te := range g.TargetEdges {
			edges = append(edges, [2]string{te.From, te.To})
		}
	} else {
		nodes = g.Packages
		edges = make([][2]string, 0, len(g.PackageEdges))
		for _, pe := range g.PackageEdges {
			edges = append(edges, [2]string{pe.From, pe.To})
		}
	}

	switch format {
	case "text":
		for _, e := range edges {
			fmt.Fprintf(w, "%s %s\n", e[0], e[1])
		}
	case "dot":
		fmt.Fprintln(w, "digraph vmake_deps {")
		fmt.Fprintln(w, "  rankdir=LR;")
		for _, n := range nodes {
			fmt.Fprintf(w, "  %s;\n", strconv.Quote(n))
		}
		for _, e := range edges {
			fmt.Fprintf(w, "  %s -> %s;\n", strconv.Quote(e[0]), strconv.Quote(e[1]))
		}
		fmt.Fprintln(w, "}")
	case "mermaid":
		fmt.Fprintln(w, "flowchart TD")
		ids := make(map[string]string, len(nodes))
		for i, n := range nodes {
			id := fmt.Sprintf("n%d", i)
			ids[n] = id
			fmt.Fprintf(w, "  %s[%s]\n", id, strconv.Quote(n))
		}
		nextID := len(nodes)
		ensureID := func(n string) string {
			if id, ok := ids[n]; ok {
				return id
			}
			id := fmt.Sprintf("n%d", nextID)
			nextID++
			ids[n] = id
			fmt.Fprintf(w, "  %s[%s]\n", id, strconv.Quote(n))
			return id
		}
		for _, e := range edges {
			fromID := ensureID(e[0])
			toID := ensureID(e[1])
			fmt.Fprintf(w, "  %s --> %s\n", fromID, toID)
		}
	}
}

func renderDepsWhy(w io.Writer, res *pipeline.WhyResult) {
	fmt.Fprintf(w, "# %s\n", res.Query)
	if !res.Needed {
		fmt.Fprintf(w, "(%s)\n", res.Reason)
		return
	}
	if res.IsRoot {
		fmt.Fprintf(w, "%s (root)\n", res.Query)
		return
	}
	if len(res.Steps) == 0 {
		if res.Reason != "" {
			fmt.Fprintf(w, "%s (%s)\n", res.Query, res.Reason)
		} else {
			fmt.Fprintf(w, "%s\n", res.Query)
		}
		return
	}
	for pIdx, path := range res.Paths {
		if pIdx > 0 {
			fmt.Fprintln(w)
		}
		if len(path) == 0 {
			continue
		}
		fmt.Fprintln(w, path[0])
		steps := res.Steps[pIdx]
		for i, step := range steps {
			indent := strings.Repeat("  ", i+1)
			if len(step.Via) > 0 {
				fmt.Fprintf(w, "%s└── %s (%s)\n", indent, step.To, strings.Join(step.Via, ", "))
			} else {
				fmt.Fprintf(w, "%s└── %s\n", indent, step.To)
			}
		}
	}
}

func completeDepsWhyArg(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	seen := make(map[string]bool)
	var refs []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			refs = append(refs, s)
		}
	}

	base, _ := completeDepsPkgRef(cmd, args, toComplete)
	for _, s := range base {
		add(s)
	}

	if findProjectDirSoft() != "" {
		restoreOut := silenceStdout()
		restoreErr := silenceStderr()
		vlog.SetLevel(vlog.Quiet)
		iexec.SetQuiet(true)
		restoreRO := queryReadOnly()
		policy := resolver.LockPolicy{DryRun: true, Offline: true}
		ctx, err := openProject(projectOptions{execution: cmd.Context(), policy: &policy, skipGitignore: true})
		if err == nil {
			for name := range ctx.DepGraph.Packages {
				add(name)
			}
			if strings.Contains(toComplete, ":") {
				if inspection, inspErr := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true}); inspErr == nil {
					if g, gErr := pipeline.BuildUnifiedGraph(ctx, inspection, true); gErr == nil {
						for _, t := range g.Targets {
							add(t)
						}
					}
				}
			}
			closeProject(ctx)
		}
		restoreRO()
		restoreErr()
		restoreOut()
	}

	sort.Strings(refs)
	return refs, cobra.ShellCompDirectiveNoFileComp
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func nonNilPackageEdges(in []pipeline.PackageEdge) []pipeline.PackageEdge {
	if in == nil {
		return []pipeline.PackageEdge{}
	}
	return in
}

func nonNilTargetEdges(in []pipeline.TargetEdge) []pipeline.TargetEdge {
	if in == nil {
		return []pipeline.TargetEdge{}
	}
	return in
}
