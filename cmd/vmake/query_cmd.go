package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func newQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Show dependency tree",
		Long: `Show the resolved dependency tree.

Each package is expanded once; later occurrences are printed as "<label> (*)".
A package is labelled by its primary target as "<pkg> (<kind>)", or
"<pkg>:<target> (<kind>)" when that target is named differently. Extra targets
are appended to the line as "+<target> (<kind>)".

Dry-run target declaration runs with interpreted-script writes disabled; a
callback that writes reports a warning on stderr and that package's targets are
omitted, without modifying the project. When packages are omitted for blocked
writes, one stderr summary line after the target listing reports the count and
the p.DryRun()/--allow-dry-run-writes remediation; the command still exits 0.`,
		Run: runQuery,
	}
	cmd.AddCommand(newQueryTargetsCmd())
	cmd.AddCommand(newQueryConfigCmd())
	return cmd
}

func newQueryTargetsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "targets",
		Short: "List build targets without building",
		Run:   runQueryTargets,
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

func runQueryTargets(cmd *cobra.Command, args []string) {
	vlog.SetLevel(vlog.Quiet)
	defer queryReadOnly()()

	ctx := resolveToConfig(false)
	inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true})
	fatalErr(err)
	scriptfs.TakeBlockedWrites()
	printInspectSourceWarnings(ctx, inspection)

	warned := make(map[string]bool)
	readOnlyOmitted := 0
	for _, name := range ctx.Resolver.GetOrder() {
		node := ctx.DepGraph.Packages[name]
		if node == nil || !node.IsLocal() || node.Pkg == nil {
			continue
		}
		if inspection.Needed != nil && !inspection.Needed[name] {
			continue
		}
		kinds, err := collectTargetKinds(name, inspection.PkgDirs[name], ctx, inspection.PackageToolchains[name], inspection.GlobalValues)
		if err != nil {
			if warnTargetDeclare(warned, name, err) {
				readOnlyOmitted++
			}
			continue
		}
		for _, k := range kinds {
			fmt.Printf("%s:%s (%s)\n", name, k.name, k.kind)
		}
	}
	printReadOnlyTargetsSummary(readOnlyOmitted)
}

func printReadOnlyTargetsSummary(count int) {
	if count == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "Warning: %s; fix: %s\n", readOnlyTargetsMessage(count), readOnlyTargetsWarningFix)
}

func runQueryConfig(cmd *cobra.Command, args []string) {
	vlog.SetLevel(vlog.Quiet)
	defer queryReadOnly()()

	name := args[0]
	ctx := resolveToConfig(false)

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
	entry := config.GetEntry(ctx.Config, name)
	vals := maps.Clone(globalValues)
	if vals == nil {
		vals = make(map[string]any)
	}
	maps.Copy(vals, entry.Options)

	if s := formatOptions(ctx, name, globalValues); s != "" {
		fmt.Println(s)
	}
	for _, d := range api.ConfigToDefines(opts, vals) {
		fmt.Printf("-D%s\n", d)
	}
}

func runQuery(cmd *cobra.Command, args []string) {
	vlog.SetLevel(vlog.Quiet)
	defer queryReadOnly()()

	ctx := resolveToConfig(false)

	inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true})
	fatalErr(err)
	scriptfs.TakeBlockedWrites()
	printInspectSourceWarnings(ctx, inspection)
	workDir := ctx.WorkDir

	graph := ctx.DepGraph.Packages
	order := ctx.Resolver.GetOrder()

	localSet := make(map[string]bool)
	dependedOn := make(map[string]bool)
	for _, name := range order {
		node := graph[name]
		if node == nil {
			continue
		}
		if node.IsLocal() {
			localSet[name] = true
		}
		for _, d := range node.Deps {
			dependedOn[d] = true
		}
	}

	roots := make([]string, 0)
	for name := range localSet {
		if dependedOn[name] {
			continue
		}
		if inspection.Needed != nil && !inspection.Needed[name] {
			continue
		}
		roots = append(roots, name)
	}
	sort.Strings(roots)

	printer := &treePrinter{
		w:       os.Stdout,
		ctx:     ctx,
		graph:   graph,
		pkgDirs: inspection.PkgDirs,
		values:  inspection.GlobalValues,
		tcs:     inspection.PackageToolchains,
		workDir: workDir,
		visited: make(map[string]bool),
		kinds:   make(map[string][]targetKindInfo),
		warned:  make(map[string]bool),
	}
	printed := 0
	for _, root := range roots {
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
	printReadOnlyTargetsSummary(printer.readOnlyOmitted)
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
	w       *os.File
	ctx     *RuntimeContext
	graph   map[string]*resolver.PackageNode
	pkgDirs map[string]*api.PkgDirs
	values  map[string]any
	tcs     map[string]*toolchain.Toolchain
	workDir string

	visited         map[string]bool
	kinds           map[string][]targetKindInfo
	warned          map[string]bool
	readOnlyOmitted int
}

func (p *treePrinter) targetKinds(name string) []targetKindInfo {
	if kinds, ok := p.kinds[name]; ok {
		return kinds
	}
	kinds, err := collectTargetKinds(name, p.pkgDirs[name], p.ctx, p.tcs[name], p.values)
	if err != nil {
		if warnTargetDeclare(p.warned, name, err) {
			p.readOnlyOmitted++
		}
	}
	p.kinds[name] = kinds
	return kinds
}

func warnTargetDeclare(warned map[string]bool, name string, err error) bool {
	if warned[name] {
		return false
	}
	warned[name] = true
	message := err.Error()
	readOnly := strings.Contains(message, "read-only inspection:")
	if readOnly {
		message = strings.ReplaceAll(message, "panic: ", "")
		message += " (guard generated-file work with p.DryRun())"
	}
	fmt.Fprintf(os.Stderr, "Warning: cannot declare targets for %s: %s\n", name, message)
	return readOnly
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

	label := name
	var kinds []targetKindInfo
	if node.IsLocal() {
		kinds = p.targetKinds(name)
		label = p.label(name, kinds)
	}
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
	if node.IsLocal() && p.pkgDirs[name] != nil {
		pkgDir = p.pkgDirs[name].SourceDir
	}
	if dir := formatPkgDir(pkgDir, p.workDir); dir != "" {
		parts = append(parts, dir)
	}
	if opts := formatOptions(p.ctx, name, p.values); opts != "" {
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

func collectTargetKinds(name string, dirs *api.PkgDirs, ctx *RuntimeContext, tc *toolchain.Toolchain, globalValues map[string]any) ([]targetKindInfo, error) {
	node := ctx.DepGraph.Packages[name]
	if node == nil || node.Pkg == nil {
		return nil, nil
	}
	if !inspectSourcePresent(ctx, name, node) {
		return nil, nil
	}

	buildCtx, err := pipeline.DeclareTargets(ctx, name, dirs, tc, globalValues)
	if blocked := scriptfs.TakeBlockedWrites(); len(blocked) > 0 && err == nil {
		if len(blocked) == 1 {
			err = errors.New(blocked[0])
		} else {
			err = fmt.Errorf("%s (and %d more blocked write attempts)", blocked[0], len(blocked)-1)
		}
	}
	if err != nil {
		return nil, err
	}

	targets := buildCtx.GetTargets()
	if len(targets) == 0 {
		return nil, nil
	}

	result := make([]targetKindInfo, 0, len(targets))
	for _, t := range targets {
		result = append(result, targetKindInfo{name: t.Name(), kind: string(t.Kind())})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].name != result[j].name {
			return result[i].name < result[j].name
		}
		return result[i].kind < result[j].kind
	})
	for i, k := range result {
		if k.name == name {
			primary := result[i]
			copy(result[1:i+1], result[0:i])
			result[0] = primary
			break
		}
	}
	return result, nil
}

func formatOptions(ctx *RuntimeContext, name string, globalValues map[string]any) string {
	opts, ok := ctx.AllOptions[name]
	if !ok {
		return ""
	}

	entry := config.GetEntry(ctx.Config, name)
	acc := api.NewConfigAccessor(entry.Options, opts)
	acc.MergeGlobals(ctx.GlobalOptions, globalValues)

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
