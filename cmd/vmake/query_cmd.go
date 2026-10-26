package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

func newQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Show dependency tree",
		Long: `Show the resolved dependency tree.

Each package is expanded once; later occurrences are printed as "<label> (*)".
A package is labelled by its primary target as "<pkg> (<kind>)", or
"<pkg>:<target> (<kind>)" when that target is named differently. Extra targets
are appended to the line as "+<target> (<kind>)". Targets are shown for every
needed package with a materialized source tree, including remote packages.

Dry-run target declaration runs with interpreted-script writes disabled; a
callback that writes reports a warning on stderr and that package's targets are
omitted, without modifying the project. Any package whose target declaration
fails is omitted and warned.`,
		Run: runQuery,
	}
	cmd.PersistentFlags().StringArrayVarP(&ephemeralSetFlags, "set", "s", nil, "override an option for this run only (format: [pkg/]option=value)")
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
			return runQueryTargets()
		},
	}
}

func queryReadOnly() func() {
	scriptfs.SetReadOnly(true)
	return func() { scriptfs.SetReadOnly(false) }
}

func newQueryConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config <pkg>",
		Short: "Show effective option values and generated defines",
		Args:  cobra.ExactArgs(1),
		Run:   runQueryConfig,
	}
}

func runQueryTargets() error {
	vlog.SetLevel(vlog.Quiet)
	defer queryReadOnly()()

	ctx := resolveToConfig(false)
	defer ctx.Resolver.Close()
	inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true})
	fatalErr(err)
	scriptfs.TakeBlockedWrites()
	data, diagnostics := buildInspectData(ctx, inspection, nil, true)
	printInspectDiagnostics(diagnostics)
	packages := inspectPackageIndex(data)

	for _, name := range ctx.Resolver.GetOrder() {
		pkg := packages[name]
		if pkg == nil || !pkg.Needed {
			continue
		}
		for _, k := range targetKindsFromInspectTargets(pkg.Targets, name) {
			fmt.Printf("%s:%s (%s)\n", name, k.name, k.kind)
		}
	}
	return nil
}

func runQueryConfig(cmd *cobra.Command, args []string) {
	vlog.SetLevel(vlog.Quiet)
	defer queryReadOnly()()

	name := args[0]
	ctx := resolveToConfig(false)
	defer ctx.Resolver.Close()

	node := ctx.DepGraph.Packages[name]
	if node == nil {
		fatalMsg("package %q not found", name)
	}
	if node.IsNative() {
		fmt.Printf("%s@%s\n", name, node.Native.Selected)
	}

	opts := maps.Clone(ctx.GlobalOptions)
	if opts == nil {
		opts = make(map[string]*api.Option)
	}
	maps.Copy(opts, ctx.AllOptions[name])

	globalValues := config.BuildGlobalValues(ctx.Config)
	vals := packageConfigValues(ctx, name, globalValues)

	if s := formatOptions(ctx, name, globalValues); s != "" {
		fmt.Println(s)
	}
	for _, d := range ctx.GlobalMacroDefines {
		fmt.Printf("-D%s\n", d)
	}
	for _, d := range api.ConfigToDefines(opts, vals) {
		fmt.Printf("-D%s\n", d)
	}
}

func runQuery(cmd *cobra.Command, args []string) {
	vlog.SetLevel(vlog.Quiet)
	defer queryReadOnly()()

	ctx := resolveToConfig(false)
	defer ctx.Resolver.Close()

	inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true})
	fatalErr(err)
	scriptfs.TakeBlockedWrites()
	data, diagnostics := buildInspectData(ctx, inspection, nil, true)
	printInspectDiagnostics(diagnostics)
	workDir := ctx.WorkDir

	graph := ctx.DepGraph.Packages
	printer := &treePrinter{
		w:          os.Stdout,
		ctx:        ctx,
		inspection: inspection,
		graph:      graph,
		packages:   inspectPackageIndex(data),
		workDir:    workDir,
		visited:    make(map[string]bool),
	}
	printed := 0
	for _, root := range data.Roots {
		node := graph[root]
		if node == nil || (len(printer.targetKinds(root)) == 0 && len(node.Deps) == 0) {
			continue
		}
		if printed > 0 {
			fmt.Fprintln(os.Stdout)
		}
		printer.print(root, "", true, true)
		printed++
	}
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
	ctx        *RuntimeContext
	inspection *pipeline.Inspection
	graph      map[string]*resolver.PackageNode
	packages   map[string]*inspectPackage
	workDir    string

	visited map[string]bool
}

func (p *treePrinter) targetKinds(name string) []targetKindInfo {
	pkg := p.packages[name]
	if pkg == nil {
		return nil
	}
	return targetKindsFromInspectTargets(pkg.Targets, name)
}

func (p *treePrinter) print(name, prefix string, isRoot, isLast bool) {
	node := p.graph[name]
	if node == nil {
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

	fmt.Fprintf(p.w, "%s%s%s\n", prefix, connector, strings.Join(p.parts(name, kinds, node), " "))

	deps := make([]string, 0, len(node.Deps))
	seen := make(map[string]bool, len(node.Deps))
	for _, dep := range node.Deps {
		if seen[dep] {
			continue
		}
		seen[dep] = true
		deps = append(deps, dep)
	}
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

func (p *treePrinter) parts(name string, kinds []targetKindInfo, node *resolver.PackageNode) []string {
	var parts []string
	parts = append(parts, p.label(name, kinds))
	if len(kinds) > 1 {
		parts = append(parts, formatExtraTargets(kinds[1:]))
	}
	if node.IsNative() {
		parts = append(parts, fmt.Sprintf("@%s", node.Native.Selected))
	}
	if node.Pkg != nil {
		if desc := node.Pkg.Description(); desc != "" {
			parts = append(parts, fmt.Sprintf("- %q", desc))
		}
	}
	var pkgDir string
	if node.IsLocal() && p.inspection.PkgDirs[name] != nil {
		pkgDir = p.inspection.PkgDirs[name].SourceDir
	}
	if dir := formatPkgDir(pkgDir, p.workDir); dir != "" {
		parts = append(parts, dir)
	}
	if opts := formatOptions(p.ctx, name, p.inspection.GlobalValues); opts != "" {
		parts = append(parts, opts)
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

func targetKindsFromInspectTargets(targets []inspectTarget, name string) []targetKindInfo {
	if len(targets) == 0 {
		return nil
	}
	result := make([]targetKindInfo, 0, len(targets))
	for _, t := range targets {
		result = append(result, targetKindInfo{name: t.Name, kind: t.Kind})
	}
	for i, k := range result {
		if k.name == name {
			primary := result[i]
			copy(result[1:i+1], result[0:i])
			result[0] = primary
			break
		}
	}
	return result
}

func inspectPackageIndex(data *inspectData) map[string]*inspectPackage {
	packages := make(map[string]*inspectPackage, len(data.Packages))
	for i := range data.Packages {
		packages[data.Packages[i].Name] = &data.Packages[i]
	}
	return packages
}

func printInspectDiagnostics(diagnostics []inspectDiagnostic) {
	for _, diagnostic := range diagnostics {
		message := diagnostic.Message
		if diagnostic.Code == "VMAKE_W_TARGET_DECLARE" && strings.Contains(message, "read-only inspection:") {
			message += " (guard generated-file work with p.DryRun())"
		}
		fmt.Fprintf(os.Stderr, "Warning: %s\n", message)
	}
}

func formatOptions(ctx *RuntimeContext, name string, globalValues map[string]any) string {
	opts, ok := ctx.AllOptions[name]
	if !ok {
		return ""
	}

	acc := packageConfigAccessor(ctx, name, globalValues)

	var parts []string
	for _, oname := range slices.Sorted(maps.Keys(opts)) {
		o := opts[oname]
		if o.IsGlobal() {
			continue
		}
		var val any
		switch o.Type() {
		case api.OptionBool:
			val = acc.Bool(oname)
		case api.OptionInt:
			val = acc.Int(oname)
		default:
			val = acc.String(oname)
		}
		parts = append(parts, fmt.Sprintf("%s=%v", oname, val))
	}

	return strings.Join(parts, ", ")
}
