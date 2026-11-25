package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	quietpkg "github.com/spock2300/vmake/internal/quiet"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

func newQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Show dependency tree",
		Long: `Show the resolved dependency tree.

Each package is expanded once; later occurrences are printed as "<label> (*)".
A package is labelled by its primary target as "<pkg>:<target> (<kind>)". Extra
targets are appended to the line as "+<target> (<kind>)". Targets are shown for
every needed package with a materialized source tree, including remote packages.

Dry-run target declaration runs with interpreted-script writes disabled; a
callback that writes reports a warning on stderr and that package's targets are
omitted, without modifying the project. Any package whose target declaration
fails is omitted and warned.`,
		Run: func(cmd *cobra.Command, args []string) {
			fatalErr(runQuery(cmd, args))
		},
	}
	addPersistentConfigFlag(cmd)
	cmd.PersistentFlags().StringArrayVarP(&ephemeralSetFlags, "set", "s", nil, "override an option for this run only (format: [pkg:]option=value)")
	cmd.AddCommand(newQueryTargetsCmd())
	cmd.AddCommand(newQueryConfigCmd())
	return cmd
}

func newQueryTargetsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "targets",
		Short: "List build targets without building",
		Long: `List the declared targets of needed packages without building.

Each target prints as "<package>:<target> (<kind>)". Both local and remote
packages are listed when their source tree is present. Dry-run target
declaration runs with interpreted-script writes disabled. A package whose
expected source tree is missing keeps its warning; a package whose OnBuild
fails to declare targets prints one warning on stderr and the command
continues.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQueryTargets(cmd.Context())
		},
	}
}

func newQueryConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config <pkg>",
		Short: "Show effective option values and generated defines",
		Args:  cobra.ExactArgs(1),
		Run:   runQueryConfig,
	}
}

func runQueryTargets(execution context.Context) error {
	root, err := resolveProjectRoot()
	if err != nil {
		fatalResolveRoot(err)
		return nil
	}
	session, err := openInspection(inspectionRequest{project: projectOptions{execution: execution, root: root}, quiet: true, execQuiet: true, facts: true})
	if err != nil {
		return err
	}
	defer session.Close()
	attachInspectionGraph(session.ctx, session.inspection, true)
	printInspectionDiagnostics(session.inspection)

	for _, name := range session.inspection.Order() {
		view, ok := session.inspection.Package(name)
		if !ok || !view.Needed {
			continue
		}
		for _, k := range targetKindsOf(session.inspection.Targets(name), name) {
			fmt.Printf("%s:%s (%s)\n", name, k.name, k.kind)
		}
	}
	return nil
}

func runQueryConfig(cmd *cobra.Command, args []string) {
	restoreScope := quietpkg.ProbeScope(true, false)
	defer restoreScope()

	name := args[0]
	ctx := mustOpenProject(projectOptions{})
	defer closeProject(ctx)

	node := ctx.DepGraph.Packages[name]
	if node == nil {
		fatalMsg("package %q not found", name)
	}
	if node.Source != nil && node.Source.Link != nil {
		fmt.Printf("%s@%s (linked %s)\n", name, resolver.LocalLinkVersion, node.Source.Link.Path)
	} else if node.IsNative() {
		if c := ctx.DepGraph.Containers[node.Container]; c != nil && c.Version != "" {
			fmt.Printf("%s@%s\n", name, c.Version)
		}
	}

	opts := maps.Clone(ctx.GlobalOptions())
	if opts == nil {
		opts = make(map[string]*api.Option)
	}
	maps.Copy(opts, ctx.PackageOptions(name))

	globalValues := pipeline.ProjectGlobalValues(ctx)
	vals := pipeline.DisplayPackageValues(ctx, name, globalValues)

	if s := formatOptions(ctx, name, globalValues); s != "" {
		fmt.Println(s)
	}
	for _, d := range ctx.GlobalDefines() {
		fmt.Printf("-D%s\n", d)
	}
	for _, d := range api.ConfigToDefines(opts, vals) {
		fmt.Printf("-D%s\n", d)
	}
}

func runQuery(cmd *cobra.Command, args []string) error {
	root, err := resolveProjectRoot()
	if err != nil {
		fatalResolveRoot(err)
		return nil
	}
	session, err := openInspection(inspectionRequest{project: projectOptions{execution: cmd.Context(), root: root}, quiet: true, execQuiet: true, facts: true})
	if err != nil {
		return err
	}
	defer session.Close()
	attachInspectionGraph(session.ctx, session.inspection, true)
	inspection := session.inspection
	printInspectionDiagnostics(inspection)

	printer := &treePrinter{
		w:          os.Stdout,
		inspection: inspection,
		workDir:    session.ctx.WorkDir,
		visited:    make(map[string]bool),
	}
	printed := 0
	for _, rootPkg := range inspection.GraphRoots() {
		if len(printer.targetKinds(rootPkg)) == 0 && len(printer.packageDeps(rootPkg)) == 0 {
			continue
		}
		if printed > 0 {
			fmt.Fprintln(os.Stdout)
		}
		printer.print(rootPkg, "", true, true)
		printed++
	}
	return nil
}

func formatPkgDir(pkgDir, workDir string) string {
	if pkgDir == "" {
		return ""
	}
	rel, err := filepath.Rel(workDir, pkgDir)
	if err != nil {
		rel = pkgDir
	}
	return fmt.Sprintf("[%s]", rel)
}

type treePrinter struct {
	w          *os.File
	inspection *pipeline.Inspection
	workDir    string

	visited map[string]bool
}

func (p *treePrinter) targetKinds(name string) []targetKindInfo {
	return targetKindsOf(p.inspection.Targets(name), name)
}

func (p *treePrinter) packageDeps(name string) []string {
	var out []string
	seen := make(map[string]bool)
	if facts, ok := p.inspection.Facts(name); ok {
		for _, dep := range facts.Deps {
			if !seen[dep] {
				seen[dep] = true
				out = append(out, dep)
			}
		}
	}
	for _, dep := range inspectSortedStrings(p.inspection.PackageDeps(name)) {
		if !seen[dep] {
			seen[dep] = true
			out = append(out, dep)
		}
	}
	return out
}

func (p *treePrinter) print(name, prefix string, isRoot, isLast bool) {
	if _, ok := p.inspection.Package(name); !ok {
		return
	}

	connector := ""
	if !isRoot {
		if isLast {
			connector = "└── "
		} else {
			connector = "├── "
		}
	}

	kinds := p.targetKinds(name)
	label := p.label(name, kinds)
	if p.visited[name] {
		fmt.Fprintf(p.w, "%s%s%s (*)\n", prefix, connector, label)
		return
	}
	p.visited[name] = true

	fmt.Fprintf(p.w, "%s%s%s\n", prefix, connector, strings.Join(p.parts(name, kinds), " "))

	deps := p.packageDeps(name)
	for i, dep := range deps {
		var childPrefix string
		if isRoot {
			childPrefix = ""
		} else if isLast {
			childPrefix = prefix + "    "
		} else {
			childPrefix = prefix + "│   "
		}
		p.print(dep, childPrefix, false, i == len(deps)-1)
	}
}

func (p *treePrinter) label(name string, kinds []targetKindInfo) string {
	if len(kinds) == 0 {
		return name
	}
	primary := name
	if kinds[0].name != name {
		primary = name + ":" + kinds[0].name
	}
	return fmt.Sprintf("%s (%s)", primary, kinds[0].kind)
}

func (p *treePrinter) parts(name string, kinds []targetKindInfo) []string {
	var parts []string
	parts = append(parts, p.label(name, kinds))
	if len(kinds) > 1 {
		parts = append(parts, formatExtraTargets(kinds[1:]))
	}
	facts, ok := p.inspection.Facts(name)
	if !ok {
		return parts
	}
	if facts.NativeVersion != "" {
		parts = append(parts, fmt.Sprintf("@%s", facts.NativeVersion))
	}
	if facts.Description != "" {
		parts = append(parts, fmt.Sprintf("- %q", facts.Description))
	}
	var pkgDir string
	if view, ok := p.inspection.Package(name); ok && !view.Skipped && facts.Kind == "local" && view.Dirs != nil {
		pkgDir = view.Dirs.SourceDir
	}
	if dir := formatPkgDir(pkgDir, p.workDir); dir != "" {
		parts = append(parts, dir)
	}
	if view, ok := p.inspection.Package(name); ok {
		if opts := formatOptionValues(view.OptionValues(pipeline.OptionProfileQuery)); opts != "" {
			parts = append(parts, opts)
		}
	}
	return parts
}

func formatExtraTargets(kinds []targetKindInfo) string {
	if len(kinds) == 0 {
		return ""
	}
	items := make([]string, 0, len(kinds))
	for _, k := range kinds {
		items = append(items, fmt.Sprintf("%s (%s)", k.name, k.kind))
	}
	return "+" + strings.Join(items, ", ")
}

type targetKindInfo struct {
	name string
	kind string
}

func targetKindsOf(facts []pipeline.TargetFacts, name string) []targetKindInfo {
	if len(facts) == 0 {
		return nil
	}
	result := make([]targetKindInfo, 0, len(facts))
	for _, fact := range facts {
		target := fact.Target
		result = append(result, targetKindInfo{name: target.Name(), kind: string(target.Kind())})
	}
	primaryName := ""
	if name != api.LocalContainerID {
		if idx := strings.LastIndex(name, "/"); idx >= 0 && idx+1 < len(name) {
			primaryName = name[idx+1:]
		} else {
			primaryName = name
		}
	}
	for i, k := range result {
		if k.name == name || (primaryName != "" && k.name == primaryName) {
			primary := result[i]
			copy(result[1:i+1], result[0:i])
			result[0] = primary
			break
		}
	}
	return result
}

func printInspectionDiagnostics(inspection *pipeline.Inspection) {
	if err := inspection.GraphError(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", err.Error())
		return
	}
	for _, diagnostic := range inspection.Diagnostics() {
		message := diagnostic.Message
		if diagnostic.IsBlockedWrite() {
			message += " (guard generated-file work with p.DryRun())"
		}
		fmt.Fprintf(os.Stderr, "Warning: %s\n", message)
	}
}

func formatOptions(ctx *RuntimeContext, name string, globalValues map[string]any) string {
	return formatOptionValues(pipeline.OptionProjection{
		Values:       pipeline.DisplayPackageValues(ctx, name, globalValues),
		Defs:         ctx.PackageOptions(name),
		GlobalDefs:   ctx.GlobalOptions(),
		GlobalValues: globalValues,
	}.Project(pipeline.OptionProfileQuery))
}

func formatOptionValues(projected []pipeline.OptionValue) string {
	if len(projected) == 0 {
		return ""
	}

	parts := make([]string, 0, len(projected))
	for _, option := range projected {
		parts = append(parts, fmt.Sprintf("%s=%v", option.Name, option.Value))
	}

	return strings.Join(parts, ", ")
}
