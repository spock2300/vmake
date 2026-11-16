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

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

const (
	inspectSchemaVersion      = 1
	readOnlyTargetsWarningFix = "Guard generated-file work with p.DryRun()."
)

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
	Package  string `json:"package,omitempty"`
	Fix      string `json:"fix,omitempty"`
}

type inspectData struct {
	Root                   string                      `json:"root"`
	ConfigFile             string                      `json:"configFile"`
	ConfigSHA256           string                      `json:"configSha256"`
	Mode                   string                      `json:"mode"`
	Toolchain              string                      `json:"toolchain"`
	ToolchainInfo          *inspectToolchain           `json:"toolchainInfo,omitempty"`
	Global                 map[string]any              `json:"global"`
	GlobalOptionDefs       map[string]inspectOptionDef `json:"globalOptionDefs,omitempty"`
	GlobalCFlags           []string                    `json:"globalCFlags,omitempty"`
	GlobalCxxFlags         []string                    `json:"globalCxxFlags,omitempty"`
	GlobalLdFlags          []string                    `json:"globalLdFlags,omitempty"`
	GlobalLinks            []string                    `json:"globalLinks,omitempty"`
	GlobalDefines          []string                    `json:"globalDefines,omitempty"`
	Overrides              map[string]any              `json:"overrides,omitempty"`
	Roots                  []string                    `json:"roots"`
	Packages               []inspectPackage            `json:"packages"`
	Graph                  []inspectEdge               `json:"graph"`
	CompileDatabase        string                      `json:"compileDatabase"`
	CompileDatabasePresent bool                        `json:"compileDatabasePresent"`
	BuildReport            string                      `json:"buildReport"`
	BuildReportPresent     bool                        `json:"buildReportPresent"`
	TestReport             string                      `json:"testReport"`
	TestReportPresent      bool                        `json:"testReportPresent"`
	InstallPrefix          string                      `json:"installPrefix"`
	InstallManifest        string                      `json:"installManifest"`
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

type inspectOptionDef struct {
	Type        string   `json:"type"`
	Default     any      `json:"default,omitempty"`
	Description string   `json:"description,omitempty"`
	Values      []string `json:"values,omitempty"`
	Group       string   `json:"group,omitempty"`
}

type inspectPackage struct {
	Name          string                      `json:"name"`
	Kind          string                      `json:"kind"`
	Repo          string                      `json:"repo,omitempty"`
	Version       string                      `json:"version,omitempty"`
	Commit        string                      `json:"commit,omitempty"`
	URL           string                      `json:"url,omitempty"`
	Path          string                      `json:"path,omitempty"`
	SourceDir     string                      `json:"sourceDir,omitempty"`
	SourcePresent bool                        `json:"sourcePresent"`
	Linked        bool                        `json:"linked,omitempty"`
	LinkPath      string                      `json:"linkPath,omitempty"`
	BuildDir      string                      `json:"buildDir,omitempty"`
	BuildKey      string                      `json:"buildKey,omitempty"`
	Needed        bool                        `json:"needed"`
	Description   string                      `json:"description,omitempty"`
	Toolchain     string                      `json:"toolchain,omitempty"`
	Platform      *inspectPlatform            `json:"platform,omitempty"`
	Options       map[string]any              `json:"options,omitempty"`
	OptionDefs    map[string]inspectOptionDef `json:"optionDefs,omitempty"`
	Deps          []string                    `json:"deps"`
	Targets       []inspectTarget             `json:"targets,omitempty"`
}

type inspectPlatform struct {
	TargetOS     string `json:"targetOs"`
	TargetTriple string `json:"targetTriple"`
}

type inspectTarget struct {
	Name              string           `json:"name"`
	Kind              string           `json:"kind"`
	Default           bool             `json:"default"`
	Test              bool             `json:"test"`
	Files             []string         `json:"files,omitempty"`
	ExcludedFiles     []string         `json:"excludedFiles,omitempty"`
	Includes          []string         `json:"includes,omitempty"`
	PublicIncludes    []string         `json:"publicIncludes,omitempty"`
	Defines           []string         `json:"defines,omitempty"`
	PublicDefines     []string         `json:"publicDefines,omitempty"`
	CFlags            []string         `json:"cFlags,omitempty"`
	PublicCFlags      []string         `json:"publicCFlags,omitempty"`
	CxxFlags          []string         `json:"cxxFlags,omitempty"`
	PublicCxxFlags    []string         `json:"publicCxxFlags,omitempty"`
	LdFlags           []string         `json:"ldFlags,omitempty"`
	PublicLdFlags     []string         `json:"publicLdFlags,omitempty"`
	Links             []string         `json:"links,omitempty"`
	PublicLinks       []string         `json:"publicLinks,omitempty"`
	Deps              []string         `json:"deps,omitempty"`
	LinkerScript      string           `json:"linkerScript,omitempty"`
	KeepGlobalSymbols string           `json:"keepGlobalSymbols,omitempty"`
	Inputs            []string         `json:"inputs,omitempty"`
	Outputs           []string         `json:"outputs,omitempty"`
	PostLinkOutputs   []string         `json:"postLinkOutputs,omitempty"`
	Output            string           `json:"output,omitempty"`
	InstallDir        string           `json:"installDir,omitempty"`
	NoInstall         bool             `json:"noInstall,omitempty"`
	Installs          []inspectInstall `json:"installs,omitempty"`
	InstallPath       string           `json:"installPath,omitempty"`
}

type inspectInstall struct {
	Source string `json:"source"`
	Dest   string `json:"dest"`
}

type inspectEdge struct {
	From  string   `json:"from"`
	To    string   `json:"to"`
	Kinds []string `json:"kinds,omitempty"`
	Via   []string `json:"via,omitempty"`
}

type inspectOverride struct {
	pkgName string
	optName string
	raw     string
	value   any
}

func newInspectCmd() *cobra.Command {
	var setFlags []string
	var noTargets bool
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Print a machine-readable project model",
		Long: `Print the resolved project model as one JSON document (schemaVersion 1).

The document reports the project root, active configuration, build mode,
toolchain, effective global values, roots, packages, targets, package
dependency graph, the compile-database and build/test-report paths and
presence, and each needed package's active build directory. Every target reports
its declared file patterns and its own declared cFlags, cxxFlags and ldFlags
when non-empty, in declaration order: raw AddCFlags, AddCxxFlags and AddLdFlags
values without package/global flags, toolchain defaults or action signatures.
Top-level globalCFlags, globalCxxFlags, globalLdFlags, globalLinks and
globalDefines report merged AddGlobal* values from needed packages (not target
flags or toolchain defaults). Diagnostics is always an array.

Dry-run OnBuild runs with interpreted-script file writes disabled: wrapped os
write APIs fail with a read-only error instead of touching the filesystem, so a
script that writes produces a VMAKE_W_TARGET_DECLARE warning and that package's
targets are omitted. Any package whose target declaration fails is omitted and
warned. Reads, stats and globs still work. vmake's own state files are
unaffected.

--set overrides a configuration option for this run only; nothing is written to
configs/default.json. Values are coerced by the declared option type: bool accepts
true/false, on/off, and 1/0; int requires an integer; string and choice values
keep their raw text, so a string can be "42" or "true". Unknown packages, unknown
options, wrong value types and invalid choice values are rejected.

With --no-targets, dry-run OnBuild target declaration is skipped for every
package, so data.packages[].targets is omitted and no VMAKE_W_TARGET_DECLARE
diagnostics are produced. Package, option, graph, source-presence and directory
data are still reported. This mode does not execute OnBuild.

On success the envelope has status "ok". On failure it has status "error" with
diagnostics, vmake exits 1, and stderr carries one human-readable error line.`,
		Args: cobra.NoArgs,
	}
	addConfigFlag(cmd)
	cmd.Flags().StringArrayVarP(&setFlags, "set", "s", nil, "override an option for this run only (format: [pkg/]option=value)")
	cmd.Flags().BoolVar(&noTargets, "no-targets", false, "skip dry-run OnBuild target declaration")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runInspect(cmd.Context(), setFlags, !noTargets)
	}
	return cmd
}

func runInspect(execution context.Context, setFlags []string, includeTargets bool) error {
	envelope := &inspectEnvelope{SchemaVersion: inspectSchemaVersion, Command: "inspect", Status: "ok", Diagnostics: []inspectDiagnostic{}}

	overrides, err := parseInspectOverrides(setFlags)
	if err != nil {
		return failInspect(envelope, "VMAKE_E_OPTION", err)
	}
	root, err := resolveProjectRoot()
	if err != nil {
		if !errors.Is(err, errNotInProject) {
			fatalUsage("%v", err)
		}
		return failInspect(envelope, "VMAKE_E_INSPECT", err)
	}
	restoreStdout := silenceStdout()
	defer restoreStdout()
	restoreStderr := silenceStderr()
	defer restoreStderr()
	fail := func(code string, err error) error {
		restoreStdout()
		restoreStderr()
		return failInspect(envelope, code, err)
	}

	session, err := openInspection(inspectionRequest{project: projectOptions{execution: execution, root: root, overrides: overrides}, quiet: true, execQuiet: true})
	if err != nil {
		var overrideErr *optionOverrideError
		if errors.As(err, &overrideErr) {
			return fail("VMAKE_E_OPTION", overrideErr.err)
		}
		return fail("VMAKE_E_INSPECT", err)
	}
	defer session.Close()
	restoreStderr()
	data, diagnostics := buildInspectData(session.ctx, session.inspection, inspectOverrideValues(overrides), includeTargets)
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

func silenceStdout() func() {
	return silenceStream(&os.Stdout)
}

func silenceStderr() func() {
	return silenceStream(&os.Stderr)
}

func silenceStream(stream **os.File) func() {
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
		pkgName, optName, raw, err := config.ParseSetFlag(flag)
		if err != nil {
			return nil, err
		}
		overrides = append(overrides, inspectOverride{pkgName: pkgName, optName: optName, raw: raw})
	}
	return overrides, nil
}

func (o inspectOverride) key() string {
	if o.pkgName == "" {
		return o.optName
	}
	return o.pkgName + "/" + o.optName
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

func buildInspectData(ctx *RuntimeContext, inspection *pipeline.Inspection, overrides map[string]any, includeTargets bool) (*inspectData, []inspectDiagnostic) {
	root := ctx.Paths.ProjectDir
	installRoot := filepath.Join(root, "install")
	global := inspection.GlobalValues
	if global == nil {
		global = make(map[string]any)
	}
	compileDatabase := filepath.Join(root, "build", "compile_commands.json")
	buildReport := reportPath(root)
	testReport := testReportPath(root)
	configFile := filepath.Base(ctx.ConfigPath)
	if rel, err := filepath.Rel(root, ctx.ConfigPath); err == nil {
		configFile = filepath.ToSlash(rel)
	}
	graph, graphErr := pipeline.BuildUnifiedGraph(ctx, inspection, pipeline.GraphOptions{
		IncludeAll:     true,
		DeclareTargets: includeTargets,
	})
	if graphErr != nil {
		return nil, []inspectDiagnostic{{
			Severity: "error",
			Code:     "VMAKE_E_INSPECT",
			Message:  graphErr.Error(),
		}}
	}
	facts := graph.Facts()
	roots := facts.Roots
	edges := inspectEdgesFromGraph(facts)

	data := &inspectData{
		Root:                   root,
		ConfigFile:             configFile,
		ConfigSHA256:           ctx.ConfigDigest,
		Mode:                   inspection.Mode,
		Toolchain:              inspection.TcName,
		ToolchainInfo:          inspectToolchainInfo(inspection),
		Global:                 global,
		GlobalOptionDefs:       inspectOptionDefs(ctx.GlobalOptions()),
		GlobalCFlags:           inspection.GlobalCFlags,
		GlobalCxxFlags:         inspection.GlobalCxxFlags,
		GlobalLdFlags:          inspection.GlobalLdFlags,
		GlobalLinks:            inspection.GlobalLinks,
		GlobalDefines:          inspection.GlobalMacroDefines,
		Overrides:              overrides,
		Roots:                  roots,
		Packages:               make([]inspectPackage, 0, len(ctx.DepGraph.Packages)),
		Graph:                  edges,
		CompileDatabase:        compileDatabase,
		CompileDatabasePresent: inspectPathExists(compileDatabase),
		BuildReport:            buildReport,
		BuildReportPresent:     inspectPathExists(buildReport),
		TestReport:             testReport,
		TestReportPresent:      inspectPathExists(testReport),
		InstallPrefix:          installRoot,
		InstallManifest:        filepath.Join(installRoot, "manifest.json"),
	}
	sources := inspectSourceStates(ctx, inspection)
	diagnostics := make([]inspectDiagnostic, 0)
	for _, name := range inspectPackageNames(ctx) {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil {
			continue
		}
		source := sources[name]
		view, viewOK := inspection.Package(name)
		pkg := inspectPackage{
			Name:          name,
			Kind:          inspectPackageKind(node),
			SourceDir:     inspectSourceDir(inspection, name, node),
			SourcePresent: source.Present,
			Needed:        view.Needed,
			Deps:          inspectSortedStrings(node.Deps),
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
		if view.Toolchain != nil {
			pkg.Toolchain = view.Toolchain.Name
		}
		pkg.Options = inspectPackageOptions(ctx, name, view.Values)
		if view.Platform != nil {
			pkg.Platform = &inspectPlatform{TargetOS: view.Platform.OS, TargetTriple: view.Platform.Triple}
		}
		pkg.OptionDefs = inspectOptionDefs(ctx.PackageOptions(name))
		if source.Diagnostic != nil {
			diagnostics = append(diagnostics, *source.Diagnostic)
		}
		if pkg.Needed {
			if viewOK && !view.Skipped && view.Dirs.BuildDir != "" {
				pkg.BuildDir = view.Dirs.BuildDir
				if view.Layout != nil {
					pkg.BuildKey = view.Layout.BuildKey
				}
			}
			if includeTargets && source.Present {
				if buildCtx, ok := graph.BuildContext(name); ok {
					pkg.Targets = renderInspectTargets(name, node, view, buildCtx)
				}
			}
		}
		data.Packages = append(data.Packages, pkg)
	}
	for _, issue := range facts.Issues {
		// Packages outside the active roots are declared only to report their
		// targets in inspect; a rejection of the current configuration there is
		// not a diagnostic for this invocation.
		if issue.Kind == "target-declare" && facts.Needed.Has(issue.Package) {
			diagnostics = append(diagnostics, inspectGraphDiagnostic(issue))
		}
	}
	if includeTargets {
		for i := range data.Packages {
			data.Packages[i].Deps = inspectSortedStrings(facts.PackageDeps[data.Packages[i].Name])
		}
	}
	return data, diagnostics
}

func inspectPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
	if view, ok := inspection.Package(name); ok && !view.Skipped && view.Dirs.SourceDir != "" {
		return view.Dirs.SourceDir
	}
	if node.Source != nil && node.Source.Link != nil {
		return node.Source.Link.Path
	}
	if node.Source != nil {
		return node.Source.Dir
	}
	return ""
}

type inspectSourceState struct {
	Present    bool
	Diagnostic *inspectDiagnostic
}

func inspectSourceStates(ctx *RuntimeContext, inspection *pipeline.Inspection) map[string]inspectSourceState {
	states := make(map[string]inspectSourceState, len(ctx.DepGraph.Packages))
	for _, name := range inspectPackageNames(ctx) {
		node := ctx.DepGraph.Packages[name]
		if node == nil || node.Source == nil {
			continue
		}
		view, _ := inspection.Package(name)
		state := inspectSourceState{Present: inspectSourcePresent(ctx, name, node)}
		if view.Needed && !state.Present {
			url, version := inspectSourceReference(ctx, name, node)
			state.Diagnostic = &inspectDiagnostic{
				Severity: "warning",
				Code:     "VMAKE_W_SOURCE_MISSING",
				Message:  inspectSourceMissingMessage(name, url, version),
				Package:  name,
			}
		}
		states[name] = state
	}
	return states
}

func inspectSourcePresent(ctx *RuntimeContext, name string, node *resolver.PackageNode) bool {
	return pipeline.IsPackageSourcePresent(ctx, name, node)
}

func inspectExpectedSourceDir(ctx *RuntimeContext, name string, node *resolver.PackageNode) string {
	return pipeline.ExpectedPackageSourceDir(ctx, name, node)
}

func inspectSourceReference(ctx *RuntimeContext, name string, node *resolver.PackageNode) (string, string) {
	pkg := inspectPackage{}
	if node.Source != nil && !node.IsLocal() {
		inspectRemoteMetadata(ctx, name, node, &pkg)
	}
	url, version := pkg.URL, pkg.Version
	if node.Pkg == nil {
		return url, version
	}
	if urls := node.Pkg.GitURLs(); url == "" && len(urls) > 0 {
		url = urls[0]
	}
	if version != "" {
		return url, version
	}
	if ctx.Pins != nil {
		version = ctx.Pins[name]
	}
	if version == "" && ctx.Lock != nil {
		if locked, ok := ctx.Lock.Get(name); ok {
			version = locked.Version
		}
	}
	if version != "" {
		return url, version
	}
	if node.IsLocal() || len(node.Constraints) > 0 {
		if selected, err := node.Pkg.SelectVersionMulti(node.Constraints); err == nil {
			version = selected
		}
		return url, version
	}
	if selected, err := node.Pkg.SelectVersion(""); err == nil {
		version = selected
	}
	return url, version
}

func inspectSourceMissingMessage(name, url, version string) string {
	message := fmt.Sprintf("package %s source is not materialized; the next build will fetch", name)
	switch {
	case url != "" && version != "":
		return fmt.Sprintf("%s %s (version %s)", message, url, version)
	case url != "":
		return message + " " + url
	case version != "":
		return message + " version " + version
	default:
		return message + " it"
	}
}

func inspectRemoteMetadata(ctx *RuntimeContext, name string, node *resolver.PackageNode, pkg *inspectPackage) {
	pkg.Repo = node.Source.Repo
	pkg.Commit = node.Source.Commit
	pkg.URL = node.Source.URL
	if node.Source.Link != nil {
		pkg.Linked = true
		pkg.LinkPath = node.Source.Link.Path
		pkg.Commit = node.Source.Link.Commit
		pkg.Version = resolver.LocalLinkVersion
	}
	if node.Source.Link == nil {
		if ctx.Pins != nil && ctx.Pins[name] != "" {
			pkg.Version = ctx.Pins[name]
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
			if pkg.Version == "" {
				pkg.Version = node.Native.Selected
			}
			if pkg.Commit == "" {
				pkg.Commit = node.Native.Commit
			}
		}
	}
	if node.Native != nil && pkg.URL == "" {
		pkg.URL = node.Native.GitURL
	}
}

func inspectPackageOptions(ctx *RuntimeContext, name string, values map[string]any) map[string]any {
	opts := ctx.PackageOptions(name)
	if len(opts) == 0 || len(values) == 0 {
		return nil
	}
	acc := api.NewConfigAccessor(values, opts)
	out := make(map[string]any, len(opts))
	for optName, opt := range opts {
		raw, exists := values[optName]
		if !exists || raw == nil {
			continue
		}
		switch opt.Type() {
		case api.OptionBool:
			out[optName] = acc.Bool(optName)
		case api.OptionInt:
			out[optName] = acc.Int(optName)
		default:
			out[optName] = acc.String(optName)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func packageConfigAccessor(ctx *RuntimeContext, name string, globalValues map[string]any) api.ConfigAccessor {
	acc := api.NewConfigAccessor(pipeline.DisplayPackageValues(ctx, name, globalValues), ctx.PackageOptions(name))
	acc.MergeGlobals(ctx.GlobalOptions(), globalValues)
	return acc
}

func inspectOptionDefs(opts map[string]*api.Option) map[string]inspectOptionDef {
	if len(opts) == 0 {
		return nil
	}
	defs := make(map[string]inspectOptionDef, len(opts))
	for optName, opt := range opts {
		if opt == nil {
			continue
		}
		var def any
		if opt.Default() != nil {
			def = api.NormalizeOptionValue(opt, opt.Default())
		}
		defs[optName] = inspectOptionDef{
			Type:        opt.Type().String(),
			Default:     def,
			Description: opt.Description(),
			Values:      append([]string(nil), opt.Values()...),
			Group:       opt.Group(),
		}
	}
	if len(defs) == 0 {
		return nil
	}
	return defs
}

func inspectTargetDeps(target *api.Target, node *resolver.PackageNode) []string {
	deps := target.Deps()
	if !target.UsesRequires() || node == nil || len(node.Deps) == 0 {
		return append([]string{}, deps...)
	}
	seen := make(map[string]bool, len(node.Deps)+len(deps))
	out := make([]string, 0, len(node.Deps)+len(deps))
	for _, depPkg := range node.Deps {
		item := depPkg + ":*"
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	for _, dep := range deps {
		if !seen[dep] {
			seen[dep] = true
			out = append(out, dep)
		}
	}
	return out
}

// renderInspectTargets projects an already-declared build context into the
// inspect schema. The declaration itself belongs to the unified graph, so
// OnBuild runs exactly once per package.
func renderInspectTargets(name string, node *resolver.PackageNode, view pipeline.PackageView, buildCtx *api.BuildContext) []inspectTarget {
	registered := buildCtx.GetTargets()
	if len(registered) == 0 {
		return nil
	}
	targetOS := ""
	if node.Pkg != nil {
		targetOS = node.Pkg.TargetOS()
	}
	defaults := api.DefaultFlags{}
	if view.Toolchain != nil {
		defaults = api.DefaultBuildFlags(view.Toolchain.Name, targetOS)
	}
	installItems := buildCtx.GetInstallItems()
	installs := make([]inspectInstall, 0, len(installItems))
	for _, item := range installItems {
		installs = append(installs, inspectInstall{Source: item.Src, Dest: item.Dest})
	}
	targets := make([]inspectTarget, 0, len(registered))
	for _, target := range registered {
		entry := inspectTarget{
			Name:              target.Name(),
			Kind:              string(target.Kind()),
			Default:           target.IsDefault(),
			Test:              target.IsTest(),
			Files:             append([]string{}, target.Files()...),
			ExcludedFiles:     append([]string{}, target.ExcludedFiles()...),
			Includes:          append([]string{}, target.Includes()...),
			PublicIncludes:    append([]string{}, target.PublicIncludes()...),
			Defines:           append([]string{}, target.Defines()...),
			PublicDefines:     append([]string{}, target.PublicDefines()...),
			CFlags:            inspectDeclaredFlags(target.CFlags(), defaults.CFlags),
			PublicCFlags:      append([]string{}, target.PublicCFlags()...),
			CxxFlags:          inspectDeclaredFlags(target.CxxFlags(), defaults.CxxFlags),
			PublicCxxFlags:    append([]string{}, target.PublicCxxFlags()...),
			LdFlags:           inspectDeclaredFlags(target.LdFlags(), defaults.LdFlags),
			PublicLdFlags:     append([]string{}, target.PublicLdFlags()...),
			Links:             append([]string{}, target.Links()...),
			PublicLinks:       append([]string{}, target.PublicLinks()...),
			LinkerScript:      target.LinkerScript(),
			KeepGlobalSymbols: target.KeepGlobalSymbols(),
			Deps:              inspectTargetDeps(target, node),
			Inputs:            append([]string{}, target.Inputs()...),
			Outputs:           append([]string{}, target.Outputs()...),
			PostLinkOutputs:   append([]string{}, target.PostLinkOutputs()...),
			InstallDir:        target.InstallDir(),
			NoInstall:         target.NoInstall(),
			Installs:          installs,
			InstallPath:       inspectTargetInstallPath(target, targetOS, inspectPkgInfo(view)),
		}
		if target.Kind() != api.TargetVoid {
			entry.Output = api.TargetFilename(target.Kind(), target.Name(), targetOS)
		}
		targets = append(targets, entry)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	return targets
}

// inspectEdgesFromGraph projects the unified graph's package edges into the
// inspect schema.
func inspectEdgesFromGraph(facts pipeline.GraphFacts) []inspectEdge {
	edges := make([]inspectEdge, 0, len(facts.PackageEdges))
	for _, pe := range facts.PackageEdges {
		edges = append(edges, inspectEdge{From: pe.From, To: pe.To, Kinds: pe.Kinds, Via: pe.Via})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return edges
}

func inspectGraphDiagnostic(issue pipeline.GraphIssue) inspectDiagnostic {
	message := issue.Message
	fix := "Fix the package OnBuild callback, then rerun vmake inspect."
	if strings.Contains(message, "read-only inspection:") {
		message = strings.ReplaceAll(message, "panic: ", "")
		fix = readOnlyTargetsWarningFix
	}
	return inspectDiagnostic{
		Severity: "warning",
		Code:     "VMAKE_W_TARGET_DECLARE",
		Message:  message,
		Fix:      fix,
		Package:  issue.Package,
	}
}

func inspectDeclaredFlags(flags, seeded []string) []string {
	if len(seeded) > 0 && len(flags) >= len(seeded) && slices.Equal(flags[:len(seeded)], seeded) {
		flags = flags[len(seeded):]
	}
	return append([]string{}, flags...)
}

func inspectPkgInfo(view pipeline.PackageView) *build.PkgInfo {
	return view.PkgInfo()
}

func inspectTargetInstallPath(target *api.Target, targetOS string, pkgInfo *build.PkgInfo) string {
	placement := build.PlacementFor(target, pkgInfo, targetOS)
	if placement.InstallName == "" || placement.NoInstall || placement.IsTest ||
		placement.Kind == api.TargetVoid || placement.Kind == api.TargetObject {
		return ""
	}
	return filepath.ToSlash(filepath.Join(".", placement.InstallSubdir, placement.InstallName))
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

func inspectSortedStrings(values []string) []string {
	out := append([]string{}, values...)
	slices.Sort(out)
	return slices.Compact(out)
}
