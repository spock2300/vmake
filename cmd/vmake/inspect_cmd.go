package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	quietpkg "github.com/spock2300/vmake/internal/quiet"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/pipeline"
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
	Containers             []inspectContainer          `json:"containers,omitempty"`
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

type inspectContainer struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Repo     string `json:"repo,omitempty"`
	Version  string `json:"version,omitempty"`
	Commit   string `json:"commit,omitempty"`
	URL      string `json:"url,omitempty"`
	Dir      string `json:"dir,omitempty"`
	Linked   bool   `json:"linked,omitempty"`
	LinkPath string `json:"linkPath,omitempty"`
}

type inspectPackage struct {
	Name          string                      `json:"name"`
	Kind          string                      `json:"kind"`
	Container     string                      `json:"container,omitempty"`
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
	cmd.Flags().StringArrayVarP(&setFlags, "set", "s", nil, "override an option for this run only (format: [pkg:]option=value)")
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
	restoreStdout := quietpkg.SilenceStdout()
	defer restoreStdout()
	restoreStderr := quietpkg.SilenceStderr()
	defer restoreStderr()
	fail := func(code string, err error) error {
		restoreStdout()
		restoreStderr()
		return failInspect(envelope, code, err)
	}

	session, err := openInspection(inspectionRequest{project: projectOptions{execution: execution, root: root, overrides: overrides}, quiet: true, execQuiet: true, facts: true})
	if err != nil {
		var overrideErr *optionOverrideError
		if errors.As(err, &overrideErr) {
			return fail("VMAKE_E_OPTION", overrideErr.err)
		}
		return fail("VMAKE_E_INSPECT", err)
	}
	defer session.Close()
	restoreStderr()
	data, diagnostics := buildInspectData(session.ctx, session.inspection, includeTargets)
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
	return o.pkgName + ":" + o.optName
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

func attachInspectionGraph(ctx *RuntimeContext, inspection *pipeline.Inspection, includeTargets bool) {
	attachInspectionGraphOptions(ctx, inspection, pipeline.GraphOptions{
		IncludeAll:     true,
		DeclareTargets: includeTargets,
	}, nil)
}

func attachInspectionGraphOptions(ctx *RuntimeContext, inspection *pipeline.Inspection, opts pipeline.GraphOptions, onBlockedWrite func(pkg string, messages []string)) *pipeline.UnifiedGraph {
	opts.OnBlockedWrite = onBlockedWrite
	graph, graphErr := pipeline.BuildUnifiedGraph(ctx, inspection, opts)
	inspection.AttachGraph(ctx, graph, graphErr)
	return graph
}

func buildInspectData(ctx *RuntimeContext, inspection *pipeline.Inspection, includeTargets bool) (*inspectData, []inspectDiagnostic) {
	attachInspectionGraph(ctx, inspection, includeTargets)
	return encodeInspect(inspection, includeTargets)
}

func encodeInspect(inspection *pipeline.Inspection, includeTargets bool) (*inspectData, []inspectDiagnostic) {
	if err := inspection.GraphError(); err != nil {
		return nil, []inspectDiagnostic{{
			Severity: "error",
			Code:     "VMAKE_E_INSPECT",
			Message:  err.Error(),
		}}
	}
	global := inspection.GlobalValues
	if global == nil {
		global = make(map[string]any)
	}
	data := &inspectData{
		Root:                   inspection.Root,
		ConfigFile:             inspection.ConfigFile,
		ConfigSHA256:           inspection.ConfigSHA256,
		Mode:                   inspection.Mode,
		Toolchain:              inspection.TcName,
		ToolchainInfo:          inspectToolchainInfo(inspection),
		Global:                 global,
		GlobalOptionDefs:       inspectOptionDefs(inspection.GlobalOptionDefs()),
		GlobalCFlags:           inspection.GlobalCFlags,
		GlobalCxxFlags:         inspection.GlobalCxxFlags,
		GlobalLdFlags:          inspection.GlobalLdFlags,
		GlobalLinks:            inspection.GlobalLinks,
		GlobalDefines:          inspection.GlobalMacroDefines,
		Overrides:              inspection.Overrides,
		Roots:                  inspection.GraphRoots(),
		Packages:               make([]inspectPackage, 0, len(inspection.Packages())),
		Graph:                  encodeEdges(inspection.GraphEdges()),
		CompileDatabase:        inspection.CompileDatabase,
		CompileDatabasePresent: inspection.CompileDatabasePresent,
		BuildReport:            inspection.BuildReport,
		BuildReportPresent:     inspection.BuildReportPresent,
		TestReport:             inspection.TestReport,
		TestReportPresent:      inspection.TestReportPresent,
		InstallPrefix:          inspection.InstallPrefix,
		InstallManifest:        inspection.InstallManifest,
	}
	containers := inspection.Containers()
	if len(containers) > 0 {
		data.Containers = make([]inspectContainer, 0, len(containers))
		for _, c := range containers {
			data.Containers = append(data.Containers, inspectContainer{
				ID:       c.ID,
				Kind:     c.Kind,
				Repo:     c.Repo,
				Version:  c.Version,
				Commit:   c.Commit,
				URL:      c.URL,
				Dir:      c.Dir,
				Linked:   c.Linked,
				LinkPath: c.LinkPath,
			})
		}
	}
	for _, name := range inspection.Packages() {
		facts, ok := inspection.Facts(name)
		if !ok {
			continue
		}
		view, viewOK := inspection.Package(name)
		pkg := inspectPackage{
			Name:          name,
			Kind:          facts.Kind,
			Container:     facts.Container,
			Repo:          facts.Repo,
			Version:       facts.Version,
			Commit:        facts.Commit,
			URL:           facts.URL,
			Path:          facts.Path,
			SourceDir:     view.SourceDir(),
			SourcePresent: facts.SourcePresent,
			Linked:        facts.Linked,
			LinkPath:      facts.LinkPath,
			Needed:        view.Needed,
			Description:   facts.Description,
			Deps:          inspectSortedStrings(facts.Deps),
		}
		if view.Toolchain != nil {
			pkg.Toolchain = view.Toolchain.Name
		}
		pkg.Options = inspectPackageOptions(view)
		if view.Platform != nil {
			pkg.Platform = &inspectPlatform{TargetOS: view.Platform.OS, TargetTriple: view.Platform.Triple}
		}
		pkg.OptionDefs = inspectOptionDefs(view.OptionDefs())
		if pkg.Needed {
			if viewOK && !view.Skipped && view.Dirs.BuildDir != "" {
				pkg.BuildDir = view.Dirs.BuildDir
				if view.Layout != nil {
					pkg.BuildKey = view.Layout.BuildKey
				}
			}
			if includeTargets && facts.SourcePresent {
				pkg.Targets = encodeTargets(inspection.Targets(name), inspection.Installs(name))
			}
		}
		data.Packages = append(data.Packages, pkg)
	}
	if includeTargets {
		for i := range data.Packages {
			data.Packages[i].Deps = inspectSortedStrings(inspection.PackageDeps(data.Packages[i].Name))
		}
	}
	diagnostics := make([]inspectDiagnostic, 0, len(inspection.Diagnostics()))
	for _, diagnostic := range inspection.Diagnostics() {
		diagnostics = append(diagnostics, inspectDiagnostic{
			Severity: diagnostic.Severity,
			Code:     diagnostic.Code,
			Message:  diagnostic.Message,
			Fix:      diagnostic.Fix,
			Package:  diagnostic.Package,
		})
	}
	return data, diagnostics
}

func encodeTargets(facts []pipeline.TargetFacts, installItems []api.InstallItem) []inspectTarget {
	if len(facts) == 0 {
		return nil
	}
	installs := make([]inspectInstall, 0, len(installItems))
	for _, item := range installItems {
		installs = append(installs, inspectInstall{Source: item.Src, Dest: item.Dest})
	}
	targets := make([]inspectTarget, 0, len(facts))
	for _, fact := range facts {
		target := fact.Target
		targets = append(targets, inspectTarget{
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
			CFlags:            fact.CFlags,
			PublicCFlags:      append([]string{}, target.PublicCFlags()...),
			CxxFlags:          fact.CxxFlags,
			PublicCxxFlags:    append([]string{}, target.PublicCxxFlags()...),
			LdFlags:           fact.LdFlags,
			PublicLdFlags:     append([]string{}, target.PublicLdFlags()...),
			Links:             append([]string{}, target.Links()...),
			PublicLinks:       append([]string{}, target.PublicLinks()...),
			LinkerScript:      target.LinkerScript(),
			KeepGlobalSymbols: target.KeepGlobalSymbols(),
			Deps:              fact.Deps,
			Inputs:            append([]string{}, target.Inputs()...),
			Outputs:           append([]string{}, target.Outputs()...),
			PostLinkOutputs:   append([]string{}, target.PostLinkOutputs()...),
			InstallDir:        target.InstallDir(),
			NoInstall:         target.NoInstall(),
			Installs:          installs,
			InstallPath:       fact.InstallPath,
			Output:            fact.Output,
		})
	}
	return targets
}

func encodeEdges(edges []pipeline.PackageEdge) []inspectEdge {
	out := make([]inspectEdge, 0, len(edges))
	for _, pe := range edges {
		out = append(out, inspectEdge{From: pe.From, To: pe.To, Kinds: pe.Kinds, Via: pe.Via})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}

func inspectPackageOptions(view pipeline.PackageView) map[string]any {
	projected := view.OptionValues(pipeline.OptionProfileInspect)
	out := make(map[string]any, len(projected))
	for _, option := range projected {
		if option.Resolved {
			out[option.Name] = option.Value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
