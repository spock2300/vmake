package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/resolver"
)

const readOnlyTargetsFix = "Guard generated-file work with p.DryRun()."

// Diagnostic is one finding of the project model.
type Diagnostic struct {
	Severity string
	Code     string
	Message  string
	Fix      string
	Package  string
}

// PackageFacts is the per-package identity and source state of the model.
type PackageFacts struct {
	Kind          string
	Repo          string
	Version       string
	Commit        string
	URL           string
	Path          string
	SourcePresent bool
	Linked        bool
	LinkPath      string
	Description   string
	Deps          []string
}

// TargetFacts is one declared target plus the values the graph computes.
type TargetFacts struct {
	Target      *api.Target
	Output      string
	InstallPath string
	Deps        []string
	CFlags      []string
	CxxFlags    []string
	LdFlags     []string
}

func (in *Inspection) Facts(name string) (PackageFacts, bool) {
	if in == nil {
		return PackageFacts{}, false
	}
	facts, ok := in.packageFacts[name]
	return facts, ok
}

func (in *Inspection) Diagnostics() []Diagnostic {
	if in == nil {
		return nil
	}
	return append([]Diagnostic(nil), in.diagnostics...)
}

func (in *Inspection) GraphError() error {
	if in == nil {
		return nil
	}
	return in.graphErr
}

func (in *Inspection) GraphRoots() []string {
	return append([]string(nil), in.graphFacts.Roots...)
}

func (in *Inspection) GraphEdges() []PackageEdge {
	return append([]PackageEdge(nil), in.graphFacts.PackageEdges...)
}

func (in *Inspection) PackageDeps(name string) []string {
	if in == nil {
		return nil
	}
	if deps, ok := in.graphFacts.PackageDeps[name]; ok {
		return append([]string(nil), deps...)
	}
	return nil
}

func (in *Inspection) Targets(name string) []TargetFacts {
	if in == nil {
		return nil
	}
	return append([]TargetFacts(nil), in.packageTargets[name]...)
}

func (in *Inspection) Installs(name string) []api.InstallItem {
	if in == nil {
		return nil
	}
	return append([]api.InstallItem(nil), in.packageInstalls[name]...)
}

// AttachGraph records the graph the command layer built, so the model can
// report its facts, targets and diagnostics.
func (in *Inspection) AttachGraph(ctx *RuntimeContext, graph *UnifiedGraph, err error) {
	if in == nil {
		return
	}
	in.graphErr = err
	if err != nil || graph == nil {
		return
	}
	facts := graph.Facts()
	in.graphFacts = facts
	for _, issue := range facts.Issues {
		if issue.Kind == "target-declare" && facts.Needed.Has(issue.Package) {
			in.diagnostics = append(in.diagnostics, graphDiagnostic(issue))
		}
	}
	in.packageTargets = make(map[string][]TargetFacts)
	in.packageInstalls = make(map[string][]api.InstallItem)
	for _, name := range in.Packages() {
		view, ok := in.Package(name)
		if !ok || !view.Needed || !in.sourcePresent(ctx, name) {
			continue
		}
		buildCtx, ok := graph.BuildContext(name)
		if !ok {
			continue
		}
		in.packageTargets[name] = targetFactsOf(ctx, name, view, buildCtx)
		in.packageInstalls[name] = append([]api.InstallItem(nil), buildCtx.GetInstallItems()...)
	}
}

func (in *Inspection) sourcePresent(ctx *RuntimeContext, name string) bool {
	if facts, ok := in.packageFacts[name]; ok {
		return facts.SourcePresent
	}
	if ctx == nil || ctx.DepGraph == nil {
		return false
	}
	node := ctx.DepGraph.Packages[name]
	if node == nil {
		return false
	}
	return IsPackageSourcePresent(ctx, name, node)
}

func targetFactsOf(ctx *RuntimeContext, name string, view PackageView, buildCtx *api.BuildContext) []TargetFacts {
	registered := buildCtx.GetTargets()
	if len(registered) == 0 {
		return nil
	}
	var node *resolver.PackageNode
	if ctx != nil && ctx.DepGraph != nil {
		node = ctx.DepGraph.Packages[name]
	}
	targetOS := ""
	if node != nil && node.Pkg != nil {
		targetOS = node.Pkg.TargetOS()
	}
	defaults := api.DefaultFlags{}
	if view.Toolchain != nil {
		defaults = api.DefaultBuildFlags(view.Toolchain.Name, targetOS)
	}
	out := make([]TargetFacts, 0, len(registered))
	for _, target := range registered {
		facts := TargetFacts{
			Target:      target,
			Deps:        targetDepsOf(target, node),
			CFlags:      declaredFlags(target.CFlags(), defaults.CFlags),
			CxxFlags:    declaredFlags(target.CxxFlags(), defaults.CxxFlags),
			LdFlags:     declaredFlags(target.LdFlags(), defaults.LdFlags),
			InstallPath: targetInstallPath(target, targetOS, view.PkgInfo()),
		}
		if target.Kind() != api.TargetVoid {
			facts.Output = api.TargetFilename(target.Kind(), target.Name(), targetOS)
		}
		out = append(out, facts)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target.Name() < out[j].Target.Name() })
	return out
}

func targetDepsOf(target *api.Target, node *resolver.PackageNode) []string {
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

func declaredFlags(flags, seeded []string) []string {
	if len(seeded) > 0 && len(flags) >= len(seeded) && slices.Equal(flags[:len(seeded)], seeded) {
		flags = flags[len(seeded):]
	}
	return append([]string{}, flags...)
}

func targetInstallPath(target *api.Target, targetOS string, pkgInfo *build.PkgInfo) string {
	placement := build.PlacementFor(target, pkgInfo, targetOS)
	if placement.InstallName == "" || placement.NoInstall || placement.IsTest ||
		placement.Kind == api.TargetVoid || placement.Kind == api.TargetObject {
		return ""
	}
	return filepath.ToSlash(filepath.Join(".", placement.InstallSubdir, placement.InstallName))
}

func graphDiagnostic(issue GraphIssue) Diagnostic {
	message := issue.Message
	fix := "Fix the package OnBuild callback, then rerun vmake inspect."
	if strings.Contains(message, "read-only inspection:") {
		message = strings.ReplaceAll(message, "panic: ", "")
		fix = readOnlyTargetsFix
	}
	return Diagnostic{
		Severity: "warning",
		Code:     "VMAKE_W_TARGET_DECLARE",
		Message:  message,
		Fix:      fix,
		Package:  issue.Package,
	}
}

func packageKind(node *resolver.PackageNode) string {
	if node.IsLocal() {
		return "local"
	}
	if node.IsNative() {
		return "native"
	}
	return "registry"
}

func nativeSelected(node *resolver.PackageNode) string {
	if node.Native != nil {
		return node.Native.Selected
	}
	return ""
}

func nativeCommit(node *resolver.PackageNode) string {
	if node.Native != nil {
		return node.Native.Commit
	}
	return ""
}

func remoteMetadata(ctx *RuntimeContext, name string, node *resolver.PackageNode, facts *PackageFacts) {
	facts.Repo = node.Source.Repo
	facts.Commit = node.Source.Commit
	facts.URL = node.Source.URL
	if node.Source.Link != nil {
		facts.Linked = true
		facts.LinkPath = node.Source.Link.Path
		facts.Commit = node.Source.Link.Commit
		facts.Version = resolver.LocalLinkVersion
	} else {
		query := resolver.LockQuery{Pins: ctx.Pins, Lock: ctx.Lock}
		facts.Version = query.Decide(name, nativeSelected(node), nativeCommit(node)).Version
		if facts.Commit == "" {
			if locked, ok := query.LockEntry(name); ok {
				facts.Commit = locked.Commit
			}
		}
		if facts.Commit == "" && node.Native != nil {
			facts.Commit = node.Native.Commit
		}
	}
	if node.Native != nil && facts.URL == "" {
		facts.URL = node.Native.GitURL
	}
}

func sourceReference(ctx *RuntimeContext, name string, node *resolver.PackageNode, facts PackageFacts) (string, string) {
	url, version := facts.URL, facts.Version
	if node.Pkg == nil {
		return url, version
	}
	if urls := node.Pkg.GitURLs(); url == "" && len(urls) > 0 {
		url = urls[0]
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

func sourceMissingMessage(name, url, version string) string {
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

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func configFileFor(root, configPath string) string {
	name := filepath.Base(configPath)
	if rel, err := filepath.Rel(root, configPath); err == nil {
		name = filepath.ToSlash(rel)
	}
	return name
}

func sortedStrings(values []string) []string {
	out := append([]string{}, values...)
	slices.Sort(out)
	return slices.Compact(out)
}
