package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

const inspectSchemaVersion = 1

type inspectEnvelope struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Command       string              `json:"command"`
	Status        string              `json:"status"`
	Data          *inspectData        `json:"data,omitempty"`
	Diagnostics   []inspectDiagnostic `json:"diagnostics"`
}

type inspectDiagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Fix      string `json:"fix,omitempty"`
}

type inspectData struct {
	Root            string            `json:"root"`
	ConfigFile      string            `json:"configFile"`
	ConfigSHA256    string            `json:"configSha256"`
	Mode            string            `json:"mode"`
	Toolchain       string            `json:"toolchain"`
	ToolchainInfo   *inspectToolchain `json:"toolchainInfo,omitempty"`
	Global          map[string]any    `json:"global"`
	Overrides       map[string]any    `json:"overrides,omitempty"`
	Roots           []string          `json:"roots"`
	Packages        []inspectPackage  `json:"packages"`
	Graph           []inspectEdge     `json:"graph"`
	CompileDatabase string            `json:"compileDatabase"`
	BuildReport     string            `json:"buildReport"`
}

type inspectToolchain struct {
	Name         string `json:"name"`
	TargetOS     string `json:"targetOs,omitempty"`
	TargetTriple string `json:"targetTriple,omitempty"`
	InstallPath  string `json:"installPath,omitempty"`
	CC           string `json:"cc,omitempty"`
	CXX          string `json:"cxx,omitempty"`
	AR           string `json:"ar,omitempty"`
	LD           string `json:"ld,omitempty"`
}

type inspectPackage struct {
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Repo        string          `json:"repo,omitempty"`
	Version     string          `json:"version,omitempty"`
	Commit      string          `json:"commit,omitempty"`
	URL         string          `json:"url,omitempty"`
	Path        string          `json:"path,omitempty"`
	SourceDir   string          `json:"sourceDir,omitempty"`
	Needed      bool            `json:"needed"`
	Description string          `json:"description,omitempty"`
	Toolchain   string          `json:"toolchain,omitempty"`
	Options     map[string]any  `json:"options,omitempty"`
	Deps        []string        `json:"deps"`
	Targets     []inspectTarget `json:"targets,omitempty"`
}

type inspectTarget struct {
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	Default         bool     `json:"default"`
	Test            bool     `json:"test"`
	Files           []string `json:"files,omitempty"`
	ExcludedFiles   []string `json:"excludedFiles,omitempty"`
	Includes        []string `json:"includes,omitempty"`
	PublicIncludes  []string `json:"publicIncludes,omitempty"`
	Defines         []string `json:"defines,omitempty"`
	Languages       []string `json:"languages,omitempty"`
	Links           []string `json:"links,omitempty"`
	Deps            []string `json:"deps,omitempty"`
	PostLinkOutputs []string `json:"postLinkOutputs,omitempty"`
	Output          string   `json:"output,omitempty"`
	InstallDir      string   `json:"installDir,omitempty"`
	NoInstall       bool     `json:"noInstall,omitempty"`
}

type inspectEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type inspectOverride struct {
	pkgName string
	optName string
	raw     string
	value   any
}

type inspectOptionDeclarations struct {
	packageOptions map[string]map[string]*api.Option
	globalOptions  map[string]*api.Option
}

func newInspectCmd() *cobra.Command {
	var setFlags []string
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Print a machine-readable project model",
		Long: `Print the resolved project model as one JSON document (schemaVersion 1).

The document reports the project root, active configuration, build mode,
toolchain, effective global values, roots, packages, targets, package
dependency graph, and the compile-database and build-report paths. Diagnostics
is always an array.

--set overrides a configuration option for this run only; nothing is written to
.vmake/config.json. Values are coerced by the declared option type: bool accepts
true/false, on/off, and 1/0; int requires an integer; string and choice values
keep their raw text, so a string can be "42" or "true". Unknown packages, unknown
options, wrong value types and invalid choice values are rejected.

On success the envelope has status "ok". On failure it has status "error" with
diagnostics, vmake exits 1, and stderr carries one human-readable error line.`,
		Args: cobra.NoArgs,
	}
	cmd.Flags().StringArrayVar(&setFlags, "set", nil, "override an option for this run only (format: [pkg/]option=value)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runInspect(cmd.Context(), setFlags)
	}
	return cmd
}

func runInspect(execution context.Context, setFlags []string) error {
	vlog.SetLevel(vlog.Quiet)
	iexec.SetQuiet(true)
	envelope := &inspectEnvelope{SchemaVersion: inspectSchemaVersion, Command: "inspect", Status: "ok", Diagnostics: []inspectDiagnostic{}}

	overrides, err := parseInspectOverrides(setFlags)
	if err != nil {
		return failInspect(envelope, "VMAKE_E_OPTION", err)
	}
	if findProjectDirSoft() == "" {
		return failInspect(envelope, "VMAKE_E_INSPECT", errors.New("not inside a vmake project (no .vmake/, build.go or */build.go found in the current directory or any parent directory)"))
	}
	restoreStdout := silenceInspectStdout()
	defer restoreStdout()
	restoreStderr := silenceInspectStderr()
	defer restoreStderr()
	fail := func(code string, err error) error {
		restoreStdout()
		restoreStderr()
		return failInspect(envelope, code, err)
	}

	if len(overrides) > 0 {
		overrides, err = coerceInspectOverrides(overrides, collectInspectOptionDeclarations(execution))
		if err != nil {
			return fail("VMAKE_E_OPTION", err)
		}
	}

	ctx, err := newInspectRuntime(execution)
	if err != nil {
		return fail("VMAKE_E_INSPECT", err)
	}
	defer ctx.Resolver.Close()

	for _, override := range overrides {
		applySetValue(ctx, override.pkgName, override.optName, override.value)
	}
	if err := pipeline.Configure(ctx); err != nil {
		if ctx.AllOptions != nil {
			for _, override := range overrides {
				if override.pkgName == "" && ctx.GlobalOptions == nil {
					continue
				}
				if overrideErr := validateInspectOverride(ctx, override); overrideErr != nil {
					return fail("VMAKE_E_OPTION", overrideErr)
				}
			}
		}
		return fail("VMAKE_E_INSPECT", err)
	}
	restoreStderr()
	for _, override := range overrides {
		if err := validateInspectOverride(ctx, override); err != nil {
			return fail("VMAKE_E_OPTION", err)
		}
	}
	inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true})
	if err != nil {
		return fail("VMAKE_E_INSPECT", err)
	}
	data, diagnostics := buildInspectData(ctx, inspection, inspectOverrideValues(overrides))
	envelope.Data = data
	envelope.Diagnostics = diagnostics
	restoreStdout()
	restoreStderr()
	if err := writeInspectEnvelope(envelope); err != nil {
		return err
	}
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", diagnostic.Message)
	}
	return nil
}

func silenceInspectStdout() func() {
	return silenceInspectStream(&os.Stdout)
}

func silenceInspectStderr() func() {
	return silenceInspectStream(&os.Stderr)
}

func silenceInspectStream(stream **os.File) func() {
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return func() {}
	}
	original := *stream
	*stream = devNull
	restored := false
	return func() {
		if restored {
			return
		}
		restored = true
		*stream = original
		devNull.Close()
	}
}

func newInspectRuntime(execution context.Context) (*RuntimeContext, error) {
	inputs, err := loadResolveInputs(false, false)
	if err != nil {
		return nil, err
	}
	ctx := newRuntimeContext(inputs, commandStorageLocks())
	ctx.Context = execution
	if err := pipeline.Require(ctx); err != nil {
		if ctx.Resolver != nil {
			ctx.Resolver.Close()
		}
		return nil, err
	}
	return ctx, nil
}

func failInspect(envelope *inspectEnvelope, code string, err error) error {
	message := inspectOneLine(err.Error())
	envelope.Status = "error"
	envelope.Data = nil
	envelope.Diagnostics = []inspectDiagnostic{{Severity: "error", Code: code, Message: message}}
	if writeErr := writeInspectEnvelope(envelope); writeErr != nil {
		return writeErr
	}
	return errors.New(message)
}

func writeInspectEnvelope(envelope *inspectEnvelope) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(envelope)
}

func inspectOneLine(message string) string {
	return strings.Join(strings.Fields(message), " ")
}

func parseInspectOverrides(flags []string) ([]inspectOverride, error) {
	overrides := make([]inspectOverride, 0, len(flags))
	for _, flag := range flags {
		pkgName, optName, raw, err := parseSetFlag(flag)
		if err != nil {
			return nil, err
		}
		overrides = append(overrides, inspectOverride{pkgName: pkgName, optName: optName, raw: raw})
	}
	return overrides, nil
}

func collectInspectOptionDeclarations(execution context.Context) *inspectOptionDeclarations {
	ctx, err := newInspectRuntime(execution)
	if err != nil {
		return nil
	}
	defer ctx.Resolver.Close()
	_ = pipeline.Configure(ctx) // best effort; pass 2 reports the authoritative error
	if ctx.AllOptions == nil && ctx.GlobalOptions == nil {
		return nil
	}
	return &inspectOptionDeclarations{packageOptions: ctx.AllOptions, globalOptions: ctx.GlobalOptions}
}

func coerceInspectOverrides(overrides []inspectOverride, declarations *inspectOptionDeclarations) ([]inspectOverride, error) {
	for i := range overrides {
		override := &overrides[i]
		opt := declarations.declared(override.pkgName, override.optName)
		if opt == nil {
			override.value = override.raw
			continue
		}
		value, err := coerceValue(override.raw, opt.Type())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", override.key(), err)
		}
		override.value = value
	}
	return overrides, nil
}

func (d *inspectOptionDeclarations) declared(pkgName, optName string) *api.Option {
	if d == nil {
		return nil
	}
	if pkgName == "" {
		return d.globalOptions[optName]
	}
	return d.packageOptions[pkgName][optName]
}

func (o inspectOverride) key() string {
	if o.pkgName == "" {
		return o.optName
	}
	return o.pkgName + "/" + o.optName
}

func validateInspectOverride(ctx *RuntimeContext, override inspectOverride) error {
	opt, err := inspectOverrideOption(ctx, override.pkgName, override.optName)
	if err != nil {
		return err
	}
	key := override.key()
	switch opt.Type() {
	case api.OptionBool:
		if _, ok := override.value.(bool); !ok {
			return fmt.Errorf("%s: expected bool, got %v", key, override.value)
		}
	case api.OptionInt:
		if _, ok := override.value.(float64); !ok {
			return fmt.Errorf("%s: expected int, got %v", key, override.value)
		}
	case api.OptionString:
		if _, ok := override.value.(string); !ok {
			return fmt.Errorf("%s: expected string, got %v", key, override.value)
		}
	case api.OptionChoice:
		value, ok := override.value.(string)
		if !ok {
			return fmt.Errorf("%s: expected choice, got %v", key, override.value)
		}
		if !isValidChoice(value, opt.Values()) {
			return fmt.Errorf("%s: invalid choice %q (available: %s)", key, value, strings.Join(opt.Values(), ", "))
		}
	}
	return nil
}

func inspectOverrideOption(ctx *RuntimeContext, pkgName, optName string) (*api.Option, error) {
	if pkgName == "" {
		opt, ok := ctx.GlobalOptions[optName]
		if !ok {
			return nil, fmt.Errorf("global option %q not found (available: %s)", optName, strings.Join(sortedOptionNames(ctx.GlobalOptions), ", "))
		}
		return opt, nil
	}
	if ctx.DepGraph == nil || ctx.DepGraph.Packages[pkgName] == nil {
		return nil, fmt.Errorf("package %q not found", pkgName)
	}
	opts := ctx.AllOptions[pkgName]
	opt, ok := opts[optName]
	if !ok {
		return nil, fmt.Errorf("option %q not found in package %q (available: %s)", optName, pkgName, strings.Join(sortedOptionNames(opts), ", "))
	}
	if opt.IsGlobal() {
		return nil, fmt.Errorf("option %q in package %q is global; set it without a package prefix: %s=...", optName, pkgName, optName)
	}
	return opt, nil
}

func inspectOverrideValues(overrides []inspectOverride) map[string]any {
	if len(overrides) == 0 {
		return nil
	}
	values := make(map[string]any, len(overrides))
	for _, override := range overrides {
		values[override.key()] = override.value
	}
	return values
}

func buildInspectData(ctx *RuntimeContext, inspection *pipeline.Inspection, overrides map[string]any) (*inspectData, []inspectDiagnostic) {
	root := ctx.Paths.ProjectDir
	global := inspection.GlobalValues
	if global == nil {
		global = make(map[string]any)
	}
	data := &inspectData{
		Root:            root,
		ConfigFile:      filepath.Base(ctx.ConfigPath),
		ConfigSHA256:    ctx.ConfigDigest,
		Mode:            inspection.Mode,
		Toolchain:       inspection.TcName,
		ToolchainInfo:   inspectToolchainInfo(inspection),
		Global:          global,
		Overrides:       overrides,
		Roots:           inspectRoots(ctx, inspection.Needed),
		Packages:        make([]inspectPackage, 0, len(ctx.DepGraph.Packages)),
		Graph:           inspectGraph(ctx),
		CompileDatabase: filepath.Join(root, "build", "compile_commands.json"),
		BuildReport:     filepath.Join(root, "build", "vmake-build.json"),
	}
	diagnostics := make([]inspectDiagnostic, 0)
	for _, name := range inspectPackageNames(ctx) {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil {
			continue
		}
		pkg := inspectPackage{
			Name:      name,
			Kind:      inspectPackageKind(node),
			SourceDir: inspectSourceDir(inspection, name, node),
			Needed:    inspection.Needed[name],
			Deps:      inspectSortedStrings(node.Deps),
		}
		if node.IsLocal() && node.Source.Dir != "" {
			if rel, err := filepath.Rel(root, node.Source.Dir); err == nil {
				pkg.Path = filepath.ToSlash(rel)
			}
		} else {
			inspectRemoteMetadata(ctx, name, node, &pkg)
		}
		if node.Pkg != nil {
			pkg.Description = node.Pkg.Description()
		}
		if tc := inspection.PackageToolchains[name]; tc != nil {
			pkg.Toolchain = tc.Name
		}
		pkg.Options = inspectPackageOptions(ctx, name, inspection)
		if pkg.Needed {
			targets, err := inspectTargets(ctx, inspection, name, node)
			if err != nil {
				diagnostics = append(diagnostics, inspectDiagnostic{
					Severity: "warning",
					Code:     "VMAKE_W_TARGET_DECLARE",
					Message:  fmt.Sprintf("cannot declare targets for %s: %v", name, err),
					Fix:      "Fix the package OnBuild callback, then rerun vmake inspect.",
				})
			} else {
				pkg.Targets = targets
			}
		}
		data.Packages = append(data.Packages, pkg)
	}
	return data, diagnostics
}

func inspectPackageNames(ctx *RuntimeContext) []string {
	names := make([]string, 0, len(ctx.DepGraph.Packages))
	for name := range ctx.DepGraph.Packages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func inspectPackageKind(node *resolver.PackageNode) string {
	if node.IsLocal() {
		return "local"
	}
	if node.IsNative() {
		return "native"
	}
	return "registry"
}

func inspectSourceDir(inspection *pipeline.Inspection, name string, node *resolver.PackageNode) string {
	if dirs := inspection.PkgDirs[name]; dirs != nil && dirs.SourceDir != "" {
		return dirs.SourceDir
	}
	if node.Source != nil {
		return node.Source.Dir
	}
	return ""
}

func inspectRemoteMetadata(ctx *RuntimeContext, name string, node *resolver.PackageNode, pkg *inspectPackage) {
	pkg.Repo = node.Source.Repo
	pkg.Commit = node.Source.Commit
	pkg.URL = node.Source.URL
	if entry := config.GetEntry(ctx.Config, name); entry.Version != "" {
		pkg.Version = entry.Version
	}
	if ctx.Lock != nil {
		if locked, ok := ctx.Lock.Get(name); ok {
			if pkg.Version == "" {
				pkg.Version = locked.Version
			}
			if pkg.Commit == "" {
				pkg.Commit = locked.Commit
			}
		}
	}
	if node.Native != nil {
		if pkg.URL == "" {
			pkg.URL = node.Native.GitURL
		}
		if pkg.Version == "" {
			pkg.Version = node.Native.Selected
		}
		if pkg.Commit == "" {
			pkg.Commit = node.Native.Commit
		}
	}
}

func inspectPackageOptions(ctx *RuntimeContext, name string, inspection *pipeline.Inspection) map[string]any {
	opts := ctx.AllOptions[name]
	if len(opts) == 0 {
		return nil
	}
	entry := config.GetEntry(ctx.Config, name)
	values := make(map[string]any, len(opts))
	for optName, opt := range opts {
		var value any
		if opt.IsGlobal() {
			value = inspection.GlobalValues[optName]
		} else {
			value = entry.Options[optName]
		}
		if value == nil {
			value = opt.Default()
		}
		if value == nil {
			continue
		}
		values[optName] = api.NormalizeOptionValue(opt, value)
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func inspectTargets(ctx *RuntimeContext, inspection *pipeline.Inspection, name string, node *resolver.PackageNode) ([]inspectTarget, error) {
	dirs := inspection.PkgDirs[name]
	if dirs == nil {
		dirs = &api.PkgDirs{SourceDir: inspectSourceDir(inspection, name, node)}
	}
	buildCtx, err := pipeline.DeclareTargets(ctx, name, dirs, inspection.PackageToolchains[name], inspection.GlobalValues)
	if err != nil {
		return nil, err
	}
	registered := buildCtx.GetTargets()
	if len(registered) == 0 {
		return nil, nil
	}
	targetOS := ""
	if node.Pkg != nil {
		targetOS = node.Pkg.TargetOS()
	}
	targets := make([]inspectTarget, 0, len(registered))
	for _, target := range registered {
		entry := inspectTarget{
			Name:            target.Name(),
			Kind:            string(target.Kind()),
			Default:         target.IsDefault(),
			Test:            target.IsTest(),
			Files:           append([]string{}, target.Files()...),
			ExcludedFiles:   append([]string{}, target.ExcludedFiles()...),
			Includes:        append([]string{}, target.Includes()...),
			PublicIncludes:  append([]string{}, target.PublicIncludes()...),
			Defines:         append([]string{}, target.Defines()...),
			Languages:       append([]string{}, target.Languages()...),
			Links:           append([]string{}, target.Links()...),
			Deps:            append([]string{}, target.Deps()...),
			PostLinkOutputs: append([]string{}, target.PostLinkOutputs()...),
			InstallDir:      target.InstallDir(),
			NoInstall:       target.NoInstall(),
		}
		if target.Kind() != api.TargetVoid {
			entry.Output = api.TargetFilename(target.Kind(), target.Name(), targetOS)
		}
		targets = append(targets, entry)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	return targets, nil
}

func inspectToolchainInfo(inspection *pipeline.Inspection) *inspectToolchain {
	tc := inspection.Tc
	if tc == nil {
		return nil
	}
	name := tc.Name
	if name == "" {
		name = inspection.TcName
	}
	info := &inspectToolchain{
		Name:         name,
		TargetOS:     inspectStringValue(inspection.GlobalValues["target_os"]),
		TargetTriple: inspectStringValue(inspection.GlobalValues["target_triple"]),
		InstallPath:  tc.InstallPath,
		CC:           tc.Tools.CC,
		CXX:          tc.Tools.CXX,
		AR:           tc.Tools.AR,
		LD:           tc.Tools.LD,
	}
	if info.TargetOS == "" {
		info.TargetOS = tc.TargetOS
	}
	if info.TargetTriple == "" {
		info.TargetTriple = tc.TargetTriple
	}
	return info
}

func inspectStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func inspectRoots(ctx *RuntimeContext, needed map[string]bool) []string {
	dependedOn := make(map[string]bool)
	for _, node := range ctx.DepGraph.Packages {
		if node == nil {
			continue
		}
		for _, dep := range node.Deps {
			dependedOn[dep] = true
		}
	}
	roots := make([]string, 0)
	for name, node := range ctx.DepGraph.Packages {
		if node == nil || !needed[name] || !node.IsLocal() || dependedOn[name] {
			continue
		}
		roots = append(roots, name)
	}
	sort.Strings(roots)
	return roots
}

func inspectGraph(ctx *RuntimeContext) []inspectEdge {
	edges := make([]inspectEdge, 0)
	seen := make(map[inspectEdge]bool)
	for name, node := range ctx.DepGraph.Packages {
		if node == nil {
			continue
		}
		for _, dep := range node.Deps {
			edge := inspectEdge{From: name, To: dep}
			if seen[edge] {
				continue
			}
			seen[edge] = true
			edges = append(edges, edge)
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return edges
}

func inspectSortedStrings(values []string) []string {
	out := append([]string{}, values...)
	slices.Sort(out)
	return slices.Compact(out)
}
