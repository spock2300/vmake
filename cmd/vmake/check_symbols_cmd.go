package main

import (
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func newCheckSymbolsCmd() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "check-symbols",
		Short: "Audit exported symbols of shared libraries and binaries via nm",
		Long: `Scan all built TargetShared and TargetBinary outputs using 'nm -D' and
report issues automatically — no per-target declaration required.

Detection categories:
  duplicate-export         Same symbol exported by multiple targets (runtime conflict risk)
  mangled-leak             C++ mangled symbol (_Z*) leaked into a C library or binary
  reserved-prefix          glibc/runtime internal symbol leaked (__libc_*, _IO_*, _Jv_*, ...)
  version-script-violation Target has SetVersionScript but exports symbols not in the .map
  no-version-script        TargetShared without version-script (info; --strict fails on this)

Requires a successful build first; scans existing build/ outputs.`,
		Run: func(cmd *cobra.Command, args []string) {
			runCheckSymbols(strict)
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "exit non-zero on any non-info finding")
	return cmd
}

type scanArtifact struct {
	pkgName       string
	targetName    string
	outputPath    string
	kind          api.TargetKind
	versionScript string
	exports       []string
}

type finding struct {
	category string
	severity string
	subject  string
	symbol   string
	detail   string
}

func runCheckSymbols(strict bool) {
	vlog.SetLevel(vlog.Quiet)

	ctx := resolveToConfig(false)
	globalValues := config.BuildGlobalValues(ctx.Config)

	insp, err := pipeline.Inspect(ctx)
	if err != nil {
		vlog.Fatal("inspect: %v", err)
	}

	artifacts := discoverArtifacts(ctx, insp.PkgDirs, insp.Tc, globalValues)
	if len(artifacts) == 0 {
		fmt.Println("No built Shared/Binary targets found. Run 'vmake build' first.")
		return
	}

	var findings []finding
	var scanned []scanArtifact

	for i := range artifacts {
		a := &artifacts[i]
		if _, err := os.Stat(a.outputPath); err != nil {
			findings = append(findings, finding{
				category: "missing-artifact",
				severity: "error",
				subject:  a.pkgName + ":" + a.targetName,
				detail:   fmt.Sprintf("build output not found: %s", a.outputPath),
			})
			continue
		}
		if err := checkDynamicSymbols(a.outputPath); err != nil {
			severity, category := "error", "tool-error"
			if errors.Is(err, errNoDynamicSymbols) || errors.Is(err, errNonELFArtifact) {
				severity, category = "info", "not-applicable"
			}
			findings = append(findings, finding{category: category, severity: severity, subject: a.pkgName + ":" + a.targetName, detail: err.Error()})
			continue
		}
		nm, err := toolchain.ResolveToolPath(insp.Tc.Tools.NM, insp.Tc.InstallPath)
		if err != nil {
			findings = append(findings, finding{category: "tool-error", severity: "error", subject: a.pkgName + ":" + a.targetName, detail: fmt.Sprintf("resolve nm %q: %v", insp.Tc.Tools.NM, err)})
			continue
		}
		exports, err := readExports(nm, a.outputPath)
		if err != nil {
			findings = append(findings, finding{
				category: "tool-error",
				severity: "error",
				subject:  a.pkgName + ":" + a.targetName,
				detail:   err.Error(),
			})
			continue
		}
		a.exports = exports
		scanned = append(scanned, *a)
	}

	findings = append(findings, detectMangledLeaks(scanned)...)
	findings = append(findings, detectReservedPrefixes(scanned)...)
	findings = append(findings, detectVersionScriptViolations(scanned)...)
	findings = append(findings, detectNoVersionScript(scanned)...)
	findings = append(findings, detectDuplicateExports(scanned)...)

	reportFindings(findings, len(artifacts))

	if strict && hasStrictFindings(findings) {
		os.Exit(1)
	}
}

func discoverArtifacts(ctx *RuntimeContext, pkgDirs map[string]*api.PkgDirs, tc *toolchain.Toolchain, globalValues map[string]any) []scanArtifact {
	var out []scanArtifact
	for name, node := range ctx.DepGraph.Packages {
		if node == nil || node.Pkg == nil || !node.IsLocal() {
			continue
		}
		dirs := pkgDirs[name]
		if dirs == nil {
			continue
		}
		buildCtx := pipeline.DeclareTargets(ctx, name, dirs, tc, globalValues)

		for _, t := range buildCtx.GetTargets() {
			kind := t.Kind()
			if kind != api.TargetShared && kind != api.TargetBinary {
				continue
			}
			if !t.IsDefault() {
				continue
			}
			filename := api.TargetFilename(kind, t.Name(), toolchain.TargetOSOf(tc))
			outputPath := findBuiltOutput(dirs.BuildDir, filename)
			out = append(out, scanArtifact{
				pkgName:       name,
				targetName:    t.Name(),
				outputPath:    outputPath,
				kind:          kind,
				versionScript: t.VersionScript(),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].pkgName != out[j].pkgName {
			return out[i].pkgName < out[j].pkgName
		}
		return out[i].targetName < out[j].targetName
	})
	return out
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

var (
	crtSymbols = map[string]bool{
		"_init": true, "_fini": true,
		"_GLOBAL_OFFSET_TABLE_": true,
		"__bss_start":           true, "_edata": true, "_end": true,
		"_DYNAMIC": true, "_PROCEDURE_LINKAGE_TABLE_": true,
		"_Jv_RegisterClasses": true,
	}

	mangledRe = regexp.MustCompile(`^_Z`)

	reservedPrefixes = []string{
		"__libc_",
		"_IO_",
		"_Jv_",
		"_ITM_",
		"__cxa_",
		"__gxx_",
		"__gnu_",
	}
)

var errNoDynamicSymbols = errors.New("not applicable: ELF artifact has no dynamic symbol table")
var errNonELFArtifact = errors.New("not applicable: artifact is not ELF; PE export analysis is not supported")

func checkDynamicSymbols(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var magic [4]byte
	if _, err := io.ReadFull(file, magic[:]); err != nil {
		return fmt.Errorf("read artifact header %s: %w", path, err)
	}
	if string(magic[:]) != elf.ELFMAG {
		return errNonELFArtifact
	}
	f, err := elf.NewFile(file)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, section := range f.Sections {
		if section.Type == elf.SHT_DYNSYM && section.Size > 0 {
			return nil
		}
	}
	return errNoDynamicSymbols
}

func readExports(nm, path string) ([]string, error) {
	out, err := iexec.RunWithOptions(nm, []string{"-D", "--defined-only", path}, iexec.RunOptions{Quiet: true})
	if err != nil {
		return nil, err
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
		if crtSymbols[sym] {
			continue
		}
		seen[sym] = true
		syms = append(syms, sym)
	}
	return syms, nil
}

func detectMangledLeaks(artifacts []scanArtifact) []finding {
	var findings []finding
	for i := range artifacts {
		a := &artifacts[i]
		for _, sym := range a.exports {
			if mangledRe.MatchString(sym) {
				findings = append(findings, finding{
					category: "mangled-leak",
					severity: "warn",
					subject:  a.pkgName + ":" + a.targetName,
					symbol:   sym,
					detail:   "C++ mangled symbol detected (missing visibility=hidden or version-script?)",
				})
			}
		}
	}
	return findings
}

func detectReservedPrefixes(artifacts []scanArtifact) []finding {
	var findings []finding
	for i := range artifacts {
		a := &artifacts[i]
		for _, sym := range a.exports {
			for _, pfx := range reservedPrefixes {
				if strings.HasPrefix(sym, pfx) {
					findings = append(findings, finding{
						category: "reserved-prefix",
						severity: "warn",
						subject:  a.pkgName + ":" + a.targetName,
						symbol:   sym,
						detail:   fmt.Sprintf("reserved/internal prefix %q (likely runtime leak)", pfx),
					})
					break
				}
			}
		}
	}
	return findings
}

func detectVersionScriptViolations(artifacts []scanArtifact) []finding {
	var findings []finding
	for i := range artifacts {
		a := &artifacts[i]
		if a.versionScript == "" {
			continue
		}
		scriptPath := a.versionScript
		if !filepath.IsAbs(scriptPath) {
			for _, dir := range []string{".", filepath.Dir(a.outputPath)} {
				candidate := filepath.Join(dir, scriptPath)
				if _, err := os.Stat(candidate); err == nil {
					scriptPath = candidate
					break
				}
			}
		}
		declared, err := parseVersionScriptGlobals(scriptPath)
		if err != nil {
			findings = append(findings, finding{
				category: "version-script-violation",
				severity: "warn",
				subject:  a.pkgName + ":" + a.targetName,
				detail:   fmt.Sprintf("could not parse %s: %v", a.versionScript, err),
			})
			continue
		}
		if declared == nil {
			continue
		}
		for _, sym := range a.exports {
			if !declared[sym] {
				findings = append(findings, finding{
					category: "version-script-violation",
					severity: "warn",
					subject:  a.pkgName + ":" + a.targetName,
					symbol:   sym,
					detail:   fmt.Sprintf("exported but not in %s global section", a.versionScript),
				})
			}
		}
	}
	return findings
}

func detectNoVersionScript(artifacts []scanArtifact) []finding {
	var findings []finding
	for i := range artifacts {
		a := &artifacts[i]
		if a.kind == api.TargetShared && a.versionScript == "" {
			findings = append(findings, finding{
				category: "no-version-script",
				severity: "info",
				subject:  a.pkgName + ":" + a.targetName,
				detail:   "TargetShared without version-script; consider adding one to restrict exports",
			})
		}
	}
	return findings
}

func detectDuplicateExports(artifacts []scanArtifact) []finding {
	owners := make(map[string][]string)
	for i := range artifacts {
		a := &artifacts[i]
		for _, sym := range a.exports {
			owners[sym] = append(owners[sym], a.pkgName+":"+a.targetName)
		}
	}
	var findings []finding
	for sym, list := range owners {
		if len(list) <= 1 {
			continue
		}
		sort.Strings(list)
		findings = append(findings, finding{
			category: "duplicate-export",
			severity: "warn",
			symbol:   sym,
			detail:   strings.Join(list, ", "),
		})
	}
	return findings
}

func parseVersionScriptGlobals(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)
	inGlobal := false
	result := make(map[string]bool)
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "global:") || strings.Contains(line, "global:") {
			inGlobal = true
			rest := strings.SplitN(line, "global:", 2)[1]
			collectGlobalSymbols(rest, result)
			continue
		}
		if strings.HasPrefix(line, "local:") || strings.Contains(line, "local:") {
			inGlobal = false
			continue
		}
		if strings.HasPrefix(line, "};") || strings.HasPrefix(line, "}") {
			inGlobal = false
			continue
		}
		if inGlobal {
			collectGlobalSymbols(line, result)
		}
	}
	return result, nil
}

func collectGlobalSymbols(line string, result map[string]bool) {
	line = strings.TrimRight(line, ";")
	line = strings.TrimSpace(line)
	if line == "" || line == "*" {
		return
	}
	if strings.ContainsAny(line, " \t{};\"") {
		for _, tok := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '{' || r == '}' || r == ';' || r == '"'
		}) {
			if tok != "" && tok != "*" {
				result[tok] = true
			}
		}
		return
	}
	result[line] = true
}

func hasStrictFindings(findings []finding) bool {
	for _, f := range findings {
		if f.severity == "warn" || f.severity == "error" {
			return true
		}
	}
	return false
}

func reportFindings(findings []finding, artifactCount int) {
	if len(findings) == 0 {
		fmt.Printf("Symbol audit: OK (%d artifacts scanned, no discrepancies)\n", artifactCount)
		return
	}

	byCategory := make(map[string][]finding)
	for _, f := range findings {
		byCategory[f.category] = append(byCategory[f.category], f)
	}

	fmt.Printf("Symbol audit: %d finding(s) across %d artifacts\n", len(findings), artifactCount)

	categoryOrder := []string{
		"not-applicable",
		"missing-artifact",
		"tool-error",
		"duplicate-export",
		"mangled-leak",
		"reserved-prefix",
		"version-script-violation",
		"no-version-script",
	}
	for _, cat := range categoryOrder {
		items := byCategory[cat]
		if len(items) == 0 {
			continue
		}
		severity := items[0].severity
		fmt.Printf("\n[%s] (%s, %d)\n", cat, severity, len(items))
		for _, f := range items {
			switch f.category {
			case "duplicate-export":
				fmt.Printf("  %s exported by: %s\n", f.symbol, f.detail)
			case "no-version-script":
				fmt.Printf("  %s: %s\n", f.subject, f.detail)
			case "missing-artifact", "tool-error":
				fmt.Printf("  %s: %s\n", f.subject, f.detail)
			default:
				if f.symbol != "" {
					fmt.Printf("  %s: %s — %s\n", f.subject, f.symbol, f.detail)
				} else {
					fmt.Printf("  %s: %s\n", f.subject, f.detail)
				}
			}
		}
	}
}
