package main

import (
	"context"
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
	"github.com/spock2300/vmake/pkg/version"
)

var queryAllowWrites bool

const (
	queryTargetsCommand     = "query targets"
	queryTargetsErrorCode   = "VMAKE_E_QUERY"
	queryTargetsWarnCode    = "VMAKE_W_TARGET_DECLARE"
	queryTargetsReadOnlyFix = "Guard generated-file work with p.DryRun(), or run vmake query --allow-dry-run-writes when those writes are expected."
	queryTargetsDeclareFix  = "Fix the package OnBuild callback, then rerun vmake query targets."
	queryDeclareFix         = "Fix the package OnBuild callback, then rerun vmake query."
)

type queryTargetsEnvelope struct {
	SchemaVersion   int                 `json:"schemaVersion"`
	Command         string              `json:"command"`
	Status          string              `json:"status"`
	VMakeVersion    string              `json:"vmakeVersion,omitempty"`
	VMakeCommit     string              `json:"vmakeCommit,omitempty"`
	ProjectRevision string              `json:"projectRevision,omitempty"`
	ProjectDirty    *bool               `json:"projectDirty,omitempty"`
	Data            *queryTargetsData   `json:"data"`
	Diagnostics     []inspectDiagnostic `json:"diagnostics"`
}

type queryTargetsData struct {
	Targets []queryTargetsEntry `json:"targets"`
}

type queryTargetsEntry struct {
	Package string `json:"package"`
	Target  string `json:"target"`
	Kind    string `json:"kind"`
}

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
omitted, without modifying the project. Any package whose target declaration
fails is omitted and warned. When packages are omitted for any declaration
failure (including blocked writes), one stderr summary line after the target
listing reports the total count plus the read-only-write count and the
p.DryRun()/--allow-dry-run-writes remediation; the command still exits 0.

Use "vmake query targets --json" for a machine-readable target list; the tree
itself has no JSON form.`,
		Run: runQuery,
	}
	cmd.PersistentFlags().BoolVar(&queryAllowWrites, "allow-dry-run-writes", false, "let dry-run OnBuild callbacks write files (may modify the build tree)")
	cmd.PersistentFlags().StringArrayVarP(&ephemeralSetFlags, "set", "s", nil, "override an option for this run only (format: [pkg/]option=value)")
	cmd.AddCommand(newQueryTargetsCmd())
	cmd.AddCommand(newQueryConfigCmd())
	return cmd
}

func newQueryTargetsCmd() *cobra.Command {
	var targetsJSON bool
	cmd := &cobra.Command{
		Use:   "targets",
		Short: "List build targets without building",
		Long: `List the declared targets of needed local packages without building.

Each target prints as "<package>:<target> (<kind>)". Dry-run target
declaration runs with interpreted-script writes disabled unless
--allow-dry-run-writes is set. A package whose expected source tree is missing
keeps its warning; a package whose OnBuild fails to declare targets prints one
warning and the command continues. Packages omitted because the read-only guard
rejected a write or because the declaration failed for another reason are
counted together in one aggregate warning after the listing, which also reports
how many failures were read-only write rejections. All warnings go to stderr and
the command exits 0.

With --json, stdout is exactly one schemaVersion 1 document:

  {"schemaVersion":1,"command":"query targets","status":"ok",
   "vmakeVersion":"...","vmakeCommit":"...",
   "projectRevision":"...","projectDirty":false,
   "data":{"targets":[{"package":"firmware","target":"firmware","kind":"binary"}]},
   "diagnostics":[]}

Targets are sorted by package then target and are exactly the targets the human
command prints. Missing-source, failed-declaration and omission warnings become
diagnostics with severity "warning", codes VMAKE_W_SOURCE_MISSING,
VMAKE_W_TARGET_DECLARE and VMAKE_W_OMITTED_TARGETS, and are not printed as
Warning lines on stderr. The aggregate omission diagnostic reports every package
whose targets were omitted and how many of them failed on blocked read-only
writes. A resolution error produces status "error", data null, one VMAKE_E_QUERY
diagnostic, exit code 1, and no partial target list on stdout.
--allow-dry-run-writes lets dry-run OnBuild callbacks materialize inputs; it can
modify the build tree and removes the read-only-omission diagnostics.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQueryTargets(targetsJSON)
		},
	}
	cmd.Flags().BoolVar(&targetsJSON, "json", false, "emit one JSON document on stdout (warnings become diagnostics)")
	return cmd
}

func queryReadOnly() func() {
	if queryAllowWrites {
		return func() {}
	}
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

func runQueryTargets(asJSON bool) error {
	vlog.SetLevel(vlog.Quiet)
	defer queryReadOnly()()
	if asJSON {
		return runQueryTargetsJSON()
	}

	ctx := resolveToConfig(false)
	defer ctx.Resolver.Close()
	inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true})
	fatalErr(err)
	scriptfs.TakeBlockedWrites()
	printInspectSourceWarnings(ctx, inspection)

	warned := make(map[string]bool)
	omitted := 0
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
			if counted, readOnly := warnTargetDeclare(warned, name, err); counted {
				omitted++
				if readOnly {
					readOnlyOmitted++
				}
			}
			continue
		}
		for _, k := range kinds {
			fmt.Printf("%s:%s (%s)\n", name, k.name, k.kind)
		}
	}
	printOmittedTargetsSummary(omitted, readOnlyOmitted)
	return nil
}

func runQueryTargetsJSON() error {
	rejectShadowedProjectRootFromCWD()
	root := findProjectDirSoft()
	envelope := newQueryTargetsEnvelope(root)
	if root == "" {
		return failQueryTargets(envelope, errors.New("not inside a vmake project (no .vmake/, build.go or */build.go found in the current directory or any parent directory)"))
	}

	restoreStdout := silenceInspectStdout()
	restoreStderr := silenceInspectStderr()
	fail := func(err error) error {
		restoreStdout()
		restoreStderr()
		return failQueryTargets(envelope, err)
	}
	defer restoreStdout()
	defer restoreStderr()

	ctx, err := resolveToConfigContext(context.Background(), false)
	if err != nil {
		return fail(err)
	}
	defer ctx.Resolver.Close()
	inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true})
	if err != nil {
		return fail(err)
	}
	scriptfs.TakeBlockedWrites()

	diagnostics := inspectSourceDiagnostics(ctx, inspection)
	targets := make([]queryTargetsEntry, 0)
	warned := make(map[string]bool)
	omitted := 0
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
			if !warned[name] {
				warned[name] = true
				diagnostic, readOnly := queryTargetDeclareDiagnostic(name, err)
				omitted++
				if readOnly {
					readOnlyOmitted++
				}
				diagnostics = append(diagnostics, diagnostic)
			}
			continue
		}
		for _, k := range kinds {
			targets = append(targets, queryTargetsEntry{Package: name, Target: k.name, Kind: k.kind})
		}
	}
	if omitted > 0 {
		fix := queryTargetsDeclareFix
		if readOnlyOmitted > 0 {
			fix = queryTargetsReadOnlyFix
		}
		diagnostics = append(diagnostics, inspectDiagnostic{
			Severity: "warning",
			Code:     omittedTargetsWarningCode,
			Message:  omittedTargetsMessage(omitted, readOnlyOmitted),
			Fix:      fix,
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Package != targets[j].Package {
			return targets[i].Package < targets[j].Package
		}
		if targets[i].Target != targets[j].Target {
			return targets[i].Target < targets[j].Target
		}
		return targets[i].Kind < targets[j].Kind
	})

	envelope.Data = &queryTargetsData{Targets: targets}
	envelope.Diagnostics = diagnostics
	restoreStdout()
	restoreStderr()
	return encodeJSONDocument(os.Stdout, envelope)
}

func newQueryTargetsEnvelope(root string) *queryTargetsEnvelope {
	var revision string
	var dirty *bool
	if root != "" {
		revision, dirty = projectProvenance(root)
	}
	return &queryTargetsEnvelope{
		SchemaVersion:   inspectSchemaVersion,
		Command:         queryTargetsCommand,
		Status:          "ok",
		VMakeVersion:    version.Version,
		VMakeCommit:     version.GitCommit,
		ProjectRevision: revision,
		ProjectDirty:    dirty,
		Data:            &queryTargetsData{Targets: []queryTargetsEntry{}},
		Diagnostics:     []inspectDiagnostic{},
	}
}

func failQueryTargets(envelope *queryTargetsEnvelope, err error) error {
	message := inspectOneLine(err.Error())
	envelope.Status = "error"
	envelope.Data = nil
	envelope.Diagnostics = []inspectDiagnostic{{Severity: "error", Code: queryTargetsErrorCode, Message: message}}
	if writeErr := encodeJSONDocument(os.Stdout, envelope); writeErr != nil {
		return writeErr
	}
	return errors.New(message)
}

func printOmittedTargetsSummary(omitted, readOnlyOmitted int) {
	if omitted == 0 {
		return
	}
	fix := queryDeclareFix
	if readOnlyOmitted > 0 {
		fix = queryTargetsReadOnlyFix
	}
	fmt.Fprintf(os.Stderr, "Warning: %s; fix: %s\n", omittedTargetsMessage(omitted, readOnlyOmitted), fix)
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
	entry := config.GetEntry(ctx.Config, name)
	vals := maps.Clone(globalValues)
	if vals == nil {
		vals = make(map[string]any)
	}
	maps.Copy(vals, entry.Options)

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
	printOmittedTargetsSummary(printer.omitted, printer.readOnlyOmitted)
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
	omitted         int
	readOnlyOmitted int
}

func (p *treePrinter) targetKinds(name string) []targetKindInfo {
	if kinds, ok := p.kinds[name]; ok {
		return kinds
	}
	kinds, err := collectTargetKinds(name, p.pkgDirs[name], p.ctx, p.tcs[name], p.values)
	if err != nil {
		if counted, readOnly := warnTargetDeclare(p.warned, name, err); counted {
			p.omitted++
			if readOnly {
				p.readOnlyOmitted++
			}
		}
	}
	p.kinds[name] = kinds
	return kinds
}

func warnTargetDeclare(warned map[string]bool, name string, err error) (bool, bool) {
	if warned[name] {
		return false, false
	}
	warned[name] = true
	diagnostic, readOnly := queryTargetDeclareDiagnostic(name, err)
	fmt.Fprintf(os.Stderr, "Warning: %s\n", diagnostic.Message)
	return true, readOnly
}

func queryTargetDeclareDiagnostic(name string, err error) (inspectDiagnostic, bool) {
	message := err.Error()
	readOnly := strings.Contains(message, "read-only inspection:")
	fix := queryTargetsDeclareFix
	if readOnly {
		message = strings.ReplaceAll(message, "panic: ", "")
		message += " (guard generated-file work with p.DryRun())"
		fix = queryTargetsReadOnlyFix
	}
	return inspectDiagnostic{
		Severity: "warning",
		Code:     queryTargetsWarnCode,
		Message:  fmt.Sprintf("cannot declare targets for %s: %s", name, message),
		Package:  name,
		Fix:      fix,
	}, readOnly
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
