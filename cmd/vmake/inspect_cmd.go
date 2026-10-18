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
	"time"

	"github.com/spf13/cobra"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/glob"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

const (
	inspectSchemaVersion                = 1
	inspectFreshnessChangedSourcesLimit = 50
	inspectResolvedFilesLimit           = 200
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
	Root                   string                   `json:"root"`
	ConfigFile             string                   `json:"configFile"`
	ConfigSHA256           string                   `json:"configSha256"`
	Mode                   string                   `json:"mode"`
	Toolchain              string                   `json:"toolchain"`
	ToolchainInfo          *inspectToolchain        `json:"toolchainInfo,omitempty"`
	Global                 map[string]any           `json:"global"`
	Overrides              map[string]any           `json:"overrides,omitempty"`
	Roots                  []string                 `json:"roots"`
	Packages               []inspectPackage         `json:"packages"`
	Graph                  []inspectEdge            `json:"graph"`
	CompileDatabase        string                   `json:"compileDatabase"`
	CompileDatabasePresent bool                     `json:"compileDatabasePresent"`
	BuildReport            string                   `json:"buildReport"`
	BuildReportPresent     bool                     `json:"buildReportPresent"`
	BuildReportState       *inspectBuildReportState `json:"buildReportState,omitempty"`
	Freshness              *inspectFreshness        `json:"freshness,omitempty"`
	TestReport             string                   `json:"testReport"`
	TestReportPresent      bool                     `json:"testReportPresent"`
	TestReportState        *inspectBuildReportState `json:"testReportState,omitempty"`
	InstallPrefix          string                   `json:"installPrefix"`
	InstallManifest        string                   `json:"installManifest"`
}

type inspectBuildReportState struct {
	Status                 string          `json:"status"`
	BuildID                string          `json:"buildId"`
	Command                string          `json:"command,omitempty"`
	Started                string          `json:"started"`
	Finished               string          `json:"finished"`
	Mode                   string          `json:"mode,omitempty"`
	Toolchain              string          `json:"toolchain,omitempty"`
	ConfigMatches          bool            `json:"configMatches"`
	ModeMatches            bool            `json:"modeMatches"`
	ToolchainMatches       bool            `json:"toolchainMatches"`
	EffectiveConfigMatches bool            `json:"effectiveConfigMatches"`
	Summary                json.RawMessage `json:"summary,omitempty"`
	Error                  string          `json:"error,omitempty"`
}

type inspectFreshness struct {
	Known                  bool     `json:"known"`
	ReportStatus           string   `json:"reportStatus,omitempty"`
	ReportFinished         string   `json:"reportFinished,omitempty"`
	ConfigMatches          *bool    `json:"configMatches,omitempty"`
	EffectiveConfigMatches *bool    `json:"effectiveConfigMatches,omitempty"`
	SourceChanged          *bool    `json:"sourceChanged,omitempty"`
	ChangedSources         []string `json:"changedSources,omitempty"`
	Truncated              *bool    `json:"truncated,omitempty"`
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
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Repo          string          `json:"repo,omitempty"`
	Version       string          `json:"version,omitempty"`
	Commit        string          `json:"commit,omitempty"`
	URL           string          `json:"url,omitempty"`
	Path          string          `json:"path,omitempty"`
	SourceDir     string          `json:"sourceDir,omitempty"`
	SourcePresent bool            `json:"sourcePresent"`
	BuildDir      string          `json:"buildDir,omitempty"`
	BuildKey      string          `json:"buildKey,omitempty"`
	Needed        bool            `json:"needed"`
	Description   string          `json:"description,omitempty"`
	Toolchain     string          `json:"toolchain,omitempty"`
	Options       map[string]any  `json:"options,omitempty"`
	Deps          []string        `json:"deps"`
	Targets       []inspectTarget `json:"targets,omitempty"`
}

type inspectTarget struct {
	Name                   string           `json:"name"`
	Kind                   string           `json:"kind"`
	Default                bool             `json:"default"`
	Test                   bool             `json:"test"`
	Files                  []string         `json:"files,omitempty"`
	ExcludedFiles          []string         `json:"excludedFiles,omitempty"`
	ResolvedFiles          []string         `json:"resolvedFiles,omitempty"`
	ResolvedFilesTruncated bool             `json:"resolvedFilesTruncated,omitempty"`
	ResolvedFileCount      int              `json:"resolvedFileCount,omitempty"`
	Includes               []string         `json:"includes,omitempty"`
	PublicIncludes         []string         `json:"publicIncludes,omitempty"`
	Defines                []string         `json:"defines,omitempty"`
	Languages              []string         `json:"languages,omitempty"`
	Links                  []string         `json:"links,omitempty"`
	Deps                   []string         `json:"deps,omitempty"`
	PostLinkOutputs        []string         `json:"postLinkOutputs,omitempty"`
	Output                 string           `json:"output,omitempty"`
	InstallDir             string           `json:"installDir,omitempty"`
	NoInstall              bool             `json:"noInstall,omitempty"`
	Installs               []inspectInstall `json:"installs,omitempty"`
	InstallPath            string           `json:"installPath,omitempty"`
}

type inspectInstall struct {
	Source string `json:"source"`
	Dest   string `json:"dest"`
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
	var allowDryRunWrites bool
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Print a machine-readable project model",
		Long: `Print the resolved project model as one JSON document (schemaVersion 1).

The document reports the project root, active configuration, build mode,
toolchain, effective global values, roots, packages, targets, package
dependency graph, the compile-database and build-report paths and presence, the
last build report's status and identity, its strict configuration-file match and
effective (override-aware) configuration match, each needed package's active
build directory, and a source-freshness heuristic. Every target keeps its
declared file patterns and adds a bounded, declaration-time resolution of those
patterns against the package source directory: resolvedFiles is sorted,
de-duplicated, slash-separated and capped at 200 entries, with
resolvedFileCount reporting the full pre-cap count and resolvedFilesTruncated
flagging the cap. These sets are declarations, not action decisions: generated
or conditional files can appear only after a build. The freshness object
compares the resolved declared files of needed packages with the build report's
finish time; it is not an up-to-date check or a build plan. These are
file-presence, resolved-layout and declared-source facts: inspect does not
verify action or output freshness. Diagnostics is always an array.

Dry-run OnBuild runs with interpreted-script file writes disabled: wrapped os
write APIs fail with a read-only error instead of touching the filesystem, so a
script that writes produces a VMAKE_W_TARGET_DECLARE warning and that package's
targets are omitted. Reads, stats and globs still work. vmake's own state files
are unaffected. --allow-dry-run-writes disables this guard for scripts that
must generate inputs before declaring targets; it can modify the build tree and
should be used only when those writes are expected.

--set overrides a configuration option for this run only; nothing is written to
.vmake/config.json. Values are coerced by the declared option type: bool accepts
true/false, on/off, and 1/0; int requires an integer; string and choice values
keep their raw text, so a string can be "42" or "true". Unknown packages, unknown
options, wrong value types and invalid choice values are rejected. Any --set
keeps the report states' configMatches false because that is the strict
configuration-file answer; effectiveConfigMatches stays true when every
override equals the value already in effect.

On success the envelope has status "ok". On failure it has status "error" with
diagnostics, vmake exits 1, and stderr carries one human-readable error line.`,
		Args: cobra.NoArgs,
	}
	cmd.Flags().StringArrayVar(&setFlags, "set", nil, "override an option for this run only (format: [pkg/]option=value)")
	cmd.Flags().BoolVar(&allowDryRunWrites, "allow-dry-run-writes", false, "let dry-run OnBuild callbacks write files (may modify the build tree)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runInspect(cmd.Context(), setFlags, allowDryRunWrites)
	}
	return cmd
}

func runInspect(execution context.Context, setFlags []string, allowDryRunWrites bool) error {
	if !allowDryRunWrites {
		scriptfs.SetReadOnly(true)
		defer scriptfs.SetReadOnly(false)
	}
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

	baselines := captureInspectOverrideBaselines(ctx, overrides)
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
	scriptfs.TakeBlockedWrites()
	overridesChanged := !overridesNoOp(ctx, baselines, overrides)
	data, diagnostics := buildInspectData(ctx, inspection, inspectOverrideValues(overrides), overridesChanged)
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

type inspectOverrideBaseline struct {
	value   any
	present bool
}

func captureInspectOverrideBaselines(ctx *RuntimeContext, overrides []inspectOverride) []inspectOverrideBaseline {
	baselines := make([]inspectOverrideBaseline, len(overrides))
	for i, override := range overrides {
		baseline := &baselines[i]
		switch {
		case override.pkgName == "" && override.optName == api.ModeOptionName:
			baseline.value, baseline.present = pipeline.ResolveMode(ctx.Config, ctx.ModeOverride), true
		case override.pkgName == "" && override.optName == api.ToolchainOptionName:
			baseline.value, baseline.present = pipeline.ResolveToolchainName(ctx.Config, ctx.ToolchainOverride), true
		case override.pkgName == "":
			if ctx.Config != nil && ctx.Config.Global != nil {
				baseline.value, baseline.present = ctx.Config.Global.Options[override.optName]
			}
		case ctx.Config != nil:
			baseline.value, baseline.present = config.GetEntry(ctx.Config, override.pkgName).Options[override.optName]
		}
		if baseline.value == nil {
			baseline.present = false
		}
	}
	return baselines
}

func overridesNoOp(ctx *RuntimeContext, baselines []inspectOverrideBaseline, overrides []inspectOverride) bool {
	if len(baselines) != len(overrides) {
		return false
	}
	for i, override := range overrides {
		opt := inspectDeclaredOption(ctx, override)
		if opt == nil {
			return false
		}
		effective := baselines[i].value
		if !baselines[i].present || (override.pkgName == "" && effective == "" && inspectTargetOption(override.optName)) {
			effective = opt.Default()
		}
		if effective == nil {
			return false
		}
		if api.NormalizeOptionValue(opt, effective) != api.NormalizeOptionValue(opt, override.value) {
			return false
		}
	}
	return true
}

func inspectDeclaredOption(ctx *RuntimeContext, override inspectOverride) *api.Option {
	if override.pkgName == "" {
		return ctx.GlobalOptions[override.optName]
	}
	return ctx.AllOptions[override.pkgName][override.optName]
}

func inspectTargetOption(name string) bool {
	return name == api.TargetOSOptionName || name == api.TargetTripleOptionName
}

func buildInspectData(ctx *RuntimeContext, inspection *pipeline.Inspection, overrides map[string]any, overridesChanged bool) (*inspectData, []inspectDiagnostic) {
	root := ctx.Paths.ProjectDir
	installRoot := filepath.Join(root, "install")
	global := inspection.GlobalValues
	if global == nil {
		global = make(map[string]any)
	}
	compileDatabase := filepath.Join(root, "build", "compile_commands.json")
	buildReport := reportPath(root)
	hypothetical := len(overrides) > 0
	buildReportPresent, buildReportState, reportDiagnostic := loadInspectReportState(buildReport, ctx.ConfigDigest, inspection.Mode, inspection.TcName, hypothetical, overridesChanged, "build")
	testReport := testReportPath(root)
	testReportPresent, testReportState, testDiagnostic := loadInspectReportState(testReport, ctx.ConfigDigest, inspection.Mode, inspection.TcName, hypothetical, overridesChanged, "test")
	data := &inspectData{
		Root:                   root,
		ConfigFile:             filepath.Base(ctx.ConfigPath),
		ConfigSHA256:           ctx.ConfigDigest,
		Mode:                   inspection.Mode,
		Toolchain:              inspection.TcName,
		ToolchainInfo:          inspectToolchainInfo(inspection),
		Global:                 global,
		Overrides:              overrides,
		Roots:                  inspectRoots(ctx, inspection.Needed),
		Packages:               make([]inspectPackage, 0, len(ctx.DepGraph.Packages)),
		Graph:                  inspectGraph(ctx),
		CompileDatabase:        compileDatabase,
		CompileDatabasePresent: inspectPathExists(compileDatabase),
		BuildReport:            buildReport,
		BuildReportPresent:     buildReportPresent,
		BuildReportState:       buildReportState,
		TestReport:             testReport,
		TestReportPresent:      testReportPresent,
		TestReportState:        testReportState,
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
		pkg := inspectPackage{
			Name:          name,
			Kind:          inspectPackageKind(node),
			SourceDir:     inspectSourceDir(inspection, name, node),
			SourcePresent: source.Present,
			Needed:        inspection.Needed[name],
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
		if tc := inspection.PackageToolchains[name]; tc != nil {
			pkg.Toolchain = tc.Name
		}
		pkg.Options = inspectPackageOptions(ctx, name, inspection)
		if source.Diagnostic != nil {
			diagnostics = append(diagnostics, *source.Diagnostic)
		}
		if pkg.Needed {
			pkg.BuildDir, pkg.BuildKey = inspectBuildDirFields(inspection.PkgDirs[name], node.IsLocal())
			if source.Present {
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
		}
		data.Packages = append(data.Packages, pkg)
	}
	data.Freshness = inspectFreshnessFor(buildReportState, data.Packages)
	if reportDiagnostic != nil {
		diagnostics = append(diagnostics, *reportDiagnostic)
	}
	if testDiagnostic != nil {
		diagnostics = append(diagnostics, *testDiagnostic)
	}
	return data, diagnostics
}

func inspectPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func inspectBuildDirFields(dirs *api.PkgDirs, local bool) (string, string) {
	if dirs == nil || dirs.BuildDir == "" {
		return "", ""
	}
	if !local || filepath.Base(filepath.Dir(dirs.BuildDir)) != "build" {
		return dirs.BuildDir, ""
	}
	return dirs.BuildDir, filepath.Base(dirs.BuildDir)
}

func loadInspectReportState(path, configDigest, mode, toolchain string, hypothetical, overridesChanged bool, kind string) (bool, *inspectBuildReportState, *inspectDiagnostic) {
	if !inspectPathExists(path) {
		return false, nil, nil
	}
	var report struct {
		Status       string          `json:"status"`
		BuildID      string          `json:"buildId"`
		Command      string          `json:"command"`
		Mode         string          `json:"mode"`
		Toolchain    string          `json:"toolchain"`
		Started      string          `json:"started"`
		Finished     string          `json:"finished"`
		ConfigSHA256 string          `json:"configSha256"`
		Summary      json.RawMessage `json:"summary"`
		Error        string          `json:"error"`
	}
	data, err := os.ReadFile(path)
	if err == nil && !strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		err = errors.New("report is not a JSON object")
	}
	if err == nil {
		err = json.Unmarshal(data, &report)
	}
	if err != nil {
		return true, nil, &inspectDiagnostic{
			Severity: "warning",
			Code:     "VMAKE_W_REPORT_UNREADABLE",
			Message:  fmt.Sprintf("cannot read %s report %s: %v", kind, path, err),
			Fix:      fmt.Sprintf("Rerun vmake %s to rewrite build/vmake-%s.json.", kind, kind),
		}
	}
	digestMatches := report.ConfigSHA256 == configDigest
	modeMatches := report.Mode == "" || report.Mode == mode
	toolchainMatches := report.Toolchain == "" || report.Toolchain == toolchain
	return true, &inspectBuildReportState{
		Status:                 report.Status,
		BuildID:                report.BuildID,
		Command:                report.Command,
		Started:                report.Started,
		Finished:               report.Finished,
		Mode:                   report.Mode,
		Toolchain:              report.Toolchain,
		ConfigMatches:          !hypothetical && digestMatches,
		ModeMatches:            modeMatches,
		ToolchainMatches:       toolchainMatches,
		EffectiveConfigMatches: !overridesChanged && digestMatches && modeMatches && toolchainMatches,
		Summary:                report.Summary,
		Error:                  report.Error,
	}, nil
}

func inspectFreshnessFor(state *inspectBuildReportState, packages []inspectPackage) *inspectFreshness {
	freshness := &inspectFreshness{}
	if state == nil {
		return freshness
	}
	freshness.ReportStatus = state.Status
	configMatches := state.ConfigMatches
	freshness.ConfigMatches = &configMatches
	effectiveConfigMatches := state.EffectiveConfigMatches
	freshness.EffectiveConfigMatches = &effectiveConfigMatches
	finished, err := time.Parse(time.RFC3339Nano, state.Finished)
	if err != nil {
		return freshness
	}
	freshness.ReportFinished = state.Finished
	changed := inspectChangedSources(packages, finished)
	sources := make([]string, 0, len(changed))
	for source := range changed {
		sources = append(sources, source)
	}
	slices.Sort(sources)
	truncated := len(sources) > inspectFreshnessChangedSourcesLimit
	if truncated {
		sources = sources[:inspectFreshnessChangedSourcesLimit]
	}
	sourceChanged := len(changed) > 0
	freshness.Known = true
	freshness.SourceChanged = &sourceChanged
	freshness.ChangedSources = sources
	freshness.Truncated = &truncated
	return freshness
}

func inspectTargetSourceFiles(sourceDir string, patterns, excluded []string) []string {
	if sourceDir == "" {
		return nil
	}
	seen := make(map[string]bool)
	files := make([]string, 0)
	for _, pattern := range patterns {
		matches, err := glob.Match(pattern, sourceDir)
		if err != nil {
			continue
		}
		for _, match := range matches {
			if inspectSourceExcluded(match, excluded) {
				continue
			}
			source := match
			if !filepath.IsAbs(source) {
				source = filepath.Join(sourceDir, filepath.FromSlash(source))
			}
			info, err := os.Stat(source)
			if err != nil || info.IsDir() {
				continue
			}
			rel := match
			if filepath.IsAbs(rel) {
				if within, err := filepath.Rel(sourceDir, rel); err == nil {
					rel = within
				}
			}
			rel = filepath.ToSlash(rel)
			if seen[rel] {
				continue
			}
			seen[rel] = true
			files = append(files, rel)
		}
	}
	sort.Strings(files)
	return files
}

func inspectChangedSources(packages []inspectPackage, finished time.Time) map[string]bool {
	changed := make(map[string]bool)
	for _, pkg := range packages {
		for _, target := range pkg.Targets {
			for _, match := range inspectTargetSourceFiles(pkg.SourceDir, target.Files, target.ExcludedFiles) {
				source := match
				if !filepath.IsAbs(source) {
					source = filepath.Join(pkg.SourceDir, filepath.FromSlash(source))
				}
				if inspectPathUnder(source, pkg.BuildDir) {
					continue
				}
				info, err := os.Stat(source)
				if err != nil || info.IsDir() {
					continue
				}
				if info.ModTime().After(finished) {
					changed[pkg.Name+":"+match] = true
				}
			}
		}
	}
	return changed
}

func inspectPathUnder(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func inspectSourceExcluded(source string, patterns []string) bool {
	for _, pattern := range patterns {
		if glob.MatchPath(pattern, source) {
			return true
		}
	}
	return false
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
		state := inspectSourceState{Present: inspectSourcePresent(ctx, name, node)}
		if inspection.Needed[name] && !state.Present {
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

func inspectSourceDiagnostics(ctx *RuntimeContext, inspection *pipeline.Inspection) []inspectDiagnostic {
	states := inspectSourceStates(ctx, inspection)
	diagnostics := make([]inspectDiagnostic, 0)
	for _, name := range inspectPackageNames(ctx) {
		if diagnostic := states[name].Diagnostic; diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	return diagnostics
}

func printInspectSourceWarnings(ctx *RuntimeContext, inspection *pipeline.Inspection) {
	for _, diagnostic := range inspectSourceDiagnostics(ctx, inspection) {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", diagnostic.Message)
	}
}

func inspectSourcePresent(ctx *RuntimeContext, name string, node *resolver.PackageNode) bool {
	return inspectSourceTreePopulated(inspectExpectedSourceDir(ctx, name, node))
}

func inspectExpectedSourceDir(ctx *RuntimeContext, name string, node *resolver.PackageNode) string {
	if node.Source == nil {
		return ""
	}
	if node.IsLocal() {
		if node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 {
			return filepath.Join(node.Source.Dir, "src")
		}
		return node.Source.Dir
	}
	owner := name
	member := ""
	if parent, ok := ctx.Resolver.SubParents()[name]; ok {
		owner = parent
		member = strings.TrimPrefix(name, parent+"/")
	}
	dir := filepath.Join(ctx.Paths.DepsDir, filepath.FromSlash(owner), "src", filepath.FromSlash(member))
	if member != "" && node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 {
		dir = filepath.Join(dir, "src")
	}
	return dir
}

func inspectSourceTreePopulated(dir string) bool {
	if dir == "" {
		return false
	}
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
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
	if ctx.Config != nil {
		version = config.GetEntry(ctx.Config, name).Version
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
	registered := buildCtx.GetTargets()
	if len(registered) == 0 {
		return nil, nil
	}
	targetOS := ""
	if node.Pkg != nil {
		targetOS = node.Pkg.TargetOS()
	}
	installItems := buildCtx.GetInstallItems()
	installs := make([]inspectInstall, 0, len(installItems))
	for _, item := range installItems {
		installs = append(installs, inspectInstall{Source: item.Src, Dest: item.Dest})
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
			Installs:        installs,
			InstallPath:     inspectTargetInstallPath(target, targetOS),
		}
		if resolved := inspectTargetSourceFiles(dirs.SourceDir, target.Files(), target.ExcludedFiles()); len(resolved) > 0 {
			entry.ResolvedFileCount = len(resolved)
			if len(resolved) > inspectResolvedFilesLimit {
				resolved = resolved[:inspectResolvedFilesLimit]
				entry.ResolvedFilesTruncated = true
			}
			entry.ResolvedFiles = resolved
		}
		if target.Kind() != api.TargetVoid {
			entry.Output = api.TargetFilename(target.Kind(), target.Name(), targetOS)
		}
		targets = append(targets, entry)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	return targets, nil
}

func inspectTargetInstallPath(target *api.Target, targetOS string) string {
	kind := target.Kind()
	if target.NoInstall() || target.IsTest() || kind == api.TargetVoid || kind == api.TargetObject {
		return ""
	}
	output := api.TargetFilename(kind, target.Name(), targetOS)
	if output == "" {
		return ""
	}
	dir := target.InstallDir()
	if dir == "" {
		dir = kind.InstallDir()
	}
	return filepath.ToSlash(filepath.Join(".", dir, output))
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
