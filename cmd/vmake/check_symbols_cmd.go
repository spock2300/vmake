package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func newCheckSymbolsCmd() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "check-symbols",
		Short: "Audit exported symbols of shared libraries and binaries",
		Long: `Verify that build artifacts export only the symbols declared via
target.SetExpectedExports(...). Also reports duplicate exports across
libraries in the build graph.

Requires a successful build first; scans existing build/ outputs.`,
		Run: func(cmd *cobra.Command, args []string) {
			runCheckSymbols(strict)
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "exit non-zero on any discrepancy")
	return cmd
}

type targetAudit struct {
	pkgName    string
	targetName string
	outputPath string
	expected   []string
}

type auditFinding struct {
	pkgTarget string
	category  string
	symbol    string
	detail    string
}

func runCheckSymbols(strict bool) {
	vlog.SetLevel(vlog.Quiet)

	ctx := resolveToConfig(false)
	pkgDirs := ResolveAllPackageDirs(ctx.DepGraph)
	globalValues := config.BuildGlobalValues(ctx.Config)

	cfg, err := resolveBuildConfig(ctx)
	if err != nil {
		vlog.Fatal("resolve build config: %v", err)
	}
	resolvedTools, err := build.ResolveTools(cfg.Tc)
	if err != nil {
		vlog.Fatal("resolve tools: %v", err)
	}
	allOpts := collectAllPkgOptions(ctx, collectNeeded(ctx.DepGraph))
	for name, node := range ctx.DepGraph.Packages {
		if node == nil || node.Source == nil || !node.IsLocal() {
			continue
		}
		pkgDirs[name] = makeLocalPkgDirs(node.Source.Dir, resolvedTools.CC, cfg.Mode, allOpts[name])
	}

	audits := collectExpectedExports(ctx, pkgDirs, globalValues)
	if len(audits) == 0 {
		fmt.Println("No targets with SetExpectedExports found. Nothing to audit.")
		return
	}

	var findings []auditFinding
	exportOwners := make(map[string][]string)

	for _, a := range audits {
		if _, err := os.Stat(a.outputPath); err != nil {
			findings = append(findings, auditFinding{
				pkgTarget: a.pkgName + ":" + a.targetName,
				category:  "missing-artifact",
				detail:    fmt.Sprintf("build output not found: %s (run 'vmake build' first)", a.outputPath),
			})
			continue
		}

		actual, err := readExports(a.outputPath)
		if err != nil {
			findings = append(findings, auditFinding{
				pkgTarget: a.pkgName + ":" + a.targetName,
				category:  "tool-error",
				detail:    err.Error(),
			})
			continue
		}

		actualSet := make(map[string]bool, len(actual))
		for _, s := range actual {
			actualSet[s] = true
		}
		expectedSet := make(map[string]bool, len(a.expected))
		for _, s := range a.expected {
			expectedSet[s] = true
		}

		for _, sym := range a.expected {
			if !actualSet[sym] {
				findings = append(findings, auditFinding{
					pkgTarget: a.pkgName + ":" + a.targetName,
					category:  "missing-export",
					symbol:    sym,
				})
			}
		}

		unexpected := make([]string, 0)
		for _, sym := range actual {
			if !expectedSet[sym] {
				unexpected = append(unexpected, sym)
			}
		}
		sort.Strings(unexpected)
		for _, sym := range unexpected {
			findings = append(findings, auditFinding{
				pkgTarget: a.pkgName + ":" + a.targetName,
				category:  "unexpected-export",
				symbol:    sym,
			})
		}

		for _, sym := range actual {
			exportOwners[sym] = append(exportOwners[sym], a.pkgName+":"+a.targetName)
		}
	}

	for sym, owners := range exportOwners {
		if len(owners) > 1 {
			sort.Strings(owners)
			findings = append(findings, auditFinding{
				category: "duplicate-export",
				symbol:   sym,
				detail:   strings.Join(owners, ", "),
			})
		}
	}

	reportFindings(findings)

	if strict && len(findings) > 0 {
		os.Exit(1)
	}
}

func collectExpectedExports(ctx *RuntimeContext, pkgDirs map[string]*api.PkgDirs, globalValues map[string]any) []targetAudit {
	var audits []targetAudit

	for name, node := range ctx.DepGraph.Packages {
		if node == nil || node.Pkg == nil || !node.IsLocal() {
			continue
		}
		dirs := pkgDirs[name]
		if dirs == nil {
			continue
		}

		buildCtx := newBuildContext(ctx, name, globalValues)
		buildCtx.SetDryRun(true)
		node.Pkg.ExecBuildFuncs(dirs.SourceDir, func(fn api.BuildFunc) {
			fn(buildCtx)
		})

		for _, t := range buildCtx.GetTargets() {
			exp := t.ExpectedExports()
			if len(exp) == 0 {
				continue
			}
			kind := t.Kind()
			if kind != api.TargetShared && kind != api.TargetBinary {
				continue
			}
			audits = append(audits, targetAudit{
				pkgName:    name,
				targetName: t.Name(),
				outputPath: findBuiltOutput(dirs.BuildDir, string(kind.Prefix())+t.Name()+string(kind.Ext())),
				expected:   append([]string{}, exp...),
			})
		}
	}

	sort.Slice(audits, func(i, j int) bool {
		if audits[i].pkgName != audits[j].pkgName {
			return audits[i].pkgName < audits[j].pkgName
		}
		return audits[i].targetName < audits[j].targetName
	})
	return audits
}

func findBuiltOutput(buildDir, filename string) string {
	if buildDir == "" {
		return ""
	}
	direct := filepath.Join(buildDir, filename)
	if _, err := os.Stat(direct); err == nil {
		return direct
	}
	pattern := filepath.Join(filepath.Dir(buildDir), "*", filename)
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		return direct
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

func readExports(path string) ([]string, error) {
	out, err := exec.Command("nm", "-D", "--defined-only", path).Output()
	if err != nil {
		return nil, fmt.Errorf("nm %s: %w", path, err)
	}
	seen := make(map[string]bool)
	var syms []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		typ := fields[1]
		if typ == "A" || typ == "a" {
			continue
		}
		sym := fields[len(fields)-1]
		if idx := strings.Index(sym, "@"); idx >= 0 {
			sym = sym[:idx]
		}
		if sym == "" || seen[sym] {
			continue
		}
		if strings.HasPrefix(sym, "_init") || strings.HasPrefix(sym, "_fini") {
			continue
		}
		if strings.HasPrefix(sym, "__") {
			continue
		}
		seen[sym] = true
		syms = append(syms, sym)
	}
	return syms, nil
}

func reportFindings(findings []auditFinding) {
	if len(findings) == 0 {
		fmt.Println("Symbol audit: OK (no discrepancies)")
		return
	}

	byCategory := make(map[string][]auditFinding)
	for _, f := range findings {
		byCategory[f.category] = append(byCategory[f.category], f)
	}

	fmt.Println("Symbol audit: issues found")
	for _, cat := range []string{"missing-artifact", "missing-export", "unexpected-export", "duplicate-export", "tool-error"} {
		items := byCategory[cat]
		if len(items) == 0 {
			continue
		}
		fmt.Printf("\n[%s] (%d)\n", cat, len(items))
		for _, f := range items {
			switch f.category {
			case "missing-export":
				fmt.Printf("  %s: expected %s but not exported\n", f.pkgTarget, f.symbol)
			case "unexpected-export":
				fmt.Printf("  %s: unexpected export %s\n", f.pkgTarget, f.symbol)
			case "duplicate-export":
				fmt.Printf("  %s exported by multiple targets: %s\n", f.symbol, f.detail)
			default:
				fmt.Printf("  %s: %s\n", f.pkgTarget, f.detail)
			}
		}
	}
}
