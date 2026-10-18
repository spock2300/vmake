package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestInspectBootstrapChild(t *testing.T) {
	dir := os.Getenv("VMAKE_TEST_INSPECT_DIR")
	if dir == "" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing command arguments")
	}
	vmakeDir = dir
	os.Args = append([]string{"vmake"}, os.Args[separator+1:]...)
	main()
	os.Exit(0)
}

func runInspectCommand(t *testing.T, projectDir string, args ...string) (string, string, int) {
	t.Helper()
	return runInspectCommandWithState(t, projectDir, t.TempDir(), args...)
}

func runInspectCommandWithState(t *testing.T, projectDir, vmakeDir string, args ...string) (string, string, int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commandArgs := append([]string{"-test.run=^TestInspectBootstrapChild$", "--"}, args...)
	cmd := exec.Command(exe, commandArgs...)
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(),
		"VMAKE_TEST_INSPECT_DIR="+vmakeDir,
		"VMAKE_CACHE="+t.TempDir(),
		"VMAKE_TRUST_ALL=1",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run inspect: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

func decodeInspectEnvelope(t *testing.T, stdout string) *inspectEnvelope {
	t.Helper()
	var envelope inspectEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	return &envelope
}

func inspectPackageNamed(t *testing.T, data *inspectData, name string) *inspectPackage {
	t.Helper()
	for i := range data.Packages {
		if data.Packages[i].Name == name {
			return &data.Packages[i]
		}
	}
	t.Fatalf("package %q missing from inspect model", name)
	return nil
}

func inspectTargetNamed(t *testing.T, pkg *inspectPackage, name string) *inspectTarget {
	t.Helper()
	for i := range pkg.Targets {
		if pkg.Targets[i].Name == name {
			return &pkg.Targets[i]
		}
	}
	t.Fatalf("target %q missing from package %q", name, pkg.Name)
	return nil
}

func writeInspectProject(t *testing.T, dir string) string {
	t.Helper()
	project := filepath.Join(dir, "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("with_dep").
			SetType(api.OptionBool).
			SetDefault(true).
			SetDescription("Link the dependency")
		ctx.Option("retries").
			SetType(api.OptionInt).
			SetDefault(3)
		ctx.Option("label").
			SetType(api.OptionString).
			SetDefault("default")
	})
	p.OnRequire(func(ctx *api.RequireContext) {
		if ctx.When("with_dep", true) {
			ctx.AddRequires("dep")
		}
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		target := ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/*.c").
			AddIncludes("include").
			AddPublicIncludes("include").
			AddDefines("APP_DEFINE=1")
		if ctx.When("with_dep", true) {
			target.AddDeps("dep:deplib")
		}
		ctx.Target("docs").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error { return nil })
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	writeExtensionFile(t, filepath.Join(project, "include", "app.h"), "#define APP 1\n")
	writeExtensionFile(t, filepath.Join(project, "dep", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("dep_mode").
			SetType(api.OptionChoice).
			SetValues("fast", "slow").
			SetDefault("fast")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("deplib").
			SetKind(api.TargetStatic).
			AddFiles("src/*.c").
			AddPublicIncludes("include")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "dep", "src", "lib.c"), "int dep(void);\nint dep(void) { return 1; }\n")
	writeExtensionFile(t, filepath.Join(project, "dep", "include", "dep.h"), "#define DEP 1\n")
	return project
}

func writeInspectFreshnessProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	return project
}

func writeInspectConfig(t *testing.T, project string) string {
	t.Helper()
	configPath := filepath.Join(project, ".vmake", "config.json")
	writeExtensionFile(t, configPath, `{"version":"1"}`)
	digest, _, err := fileDigest(configPath)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func writeInspectBuildReport(t *testing.T, project, finished, configSHA256 string) {
	t.Helper()
	writeExtensionFile(t, filepath.Join(project, "build", "vmake-build.json"), fmt.Sprintf(
		`{"schemaVersion":2,"buildId":"freshness","status":"succeeded","command":"build","started":"2020-01-01T00:00:00Z","finished":%q,"configSha256":%q}`,
		finished, configSHA256))
}

func setInspectSourceTime(t *testing.T, paths []string, modTime time.Time) {
	t.Helper()
	for _, path := range paths {
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInspectProjectModel(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %s", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("successful inspect wrote stderr: %q", stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.SchemaVersion != inspectSchemaVersion || envelope.Command != "inspect" || envelope.Status != "ok" {
		t.Fatalf("envelope = %+v", envelope)
	}
	if envelope.Data == nil {
		t.Fatal("successful inspect has no data")
	}
	if envelope.Diagnostics == nil || len(envelope.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want []", envelope.Diagnostics)
	}
	data := envelope.Data
	if data.Root != project || data.ConfigFile != "config.json" || data.Mode != "debug" || data.Toolchain != "host" {
		t.Fatalf("data header = %+v", data)
	}
	if want := filepath.Join(project, "build", "compile_commands.json"); data.CompileDatabase != want {
		t.Errorf("compileDatabase = %q, want %q", data.CompileDatabase, want)
	}
	if want := filepath.Join(project, "build", "vmake-build.json"); data.BuildReport != want {
		t.Errorf("buildReport = %q, want %q", data.BuildReport, want)
	}
	if want := filepath.Join(project, "install"); data.InstallPrefix != want {
		t.Errorf("installPrefix = %q, want %q", data.InstallPrefix, want)
	}
	if want := filepath.Join(project, "install", "manifest.json"); data.InstallManifest != want {
		t.Errorf("installManifest = %q, want %q", data.InstallManifest, want)
	}
	if !slices.Equal(data.Roots, []string{"proj"}) {
		t.Errorf("roots = %v", data.Roots)
	}
	if data.ToolchainInfo == nil || data.ToolchainInfo.Name != "host" || data.ToolchainInfo.CC == "" {
		t.Errorf("toolchainInfo = %+v", data.ToolchainInfo)
	}
	if data.Global["mode"] != "debug" || data.Global["toolchain"] != "host" {
		t.Errorf("global = %v", data.Global)
	}
	if data.Overrides != nil {
		t.Errorf("unexpected overrides: %v", data.Overrides)
	}
	if data.GlobalCFlags != nil || data.GlobalCxxFlags != nil || data.GlobalLdFlags != nil ||
		data.GlobalLinks != nil || data.GlobalDefines != nil {
		t.Errorf("empty global flag arrays = %v/%v/%v/%v/%v, want omitted",
			data.GlobalCFlags, data.GlobalCxxFlags, data.GlobalLdFlags, data.GlobalLinks, data.GlobalDefines)
	}
	for _, key := range []string{"globalCFlags", "globalCxxFlags", "globalLdFlags", "globalLinks", "globalDefines"} {
		if strings.Contains(stdout, `"`+key+`"`) {
			t.Errorf("inspect emitted %q for a project without global flags", key)
		}
	}
	if !slices.Equal(data.Graph, []inspectEdge{{From: "proj", To: "dep"}}) {
		t.Errorf("graph = %v", data.Graph)
	}

	proj := inspectPackageNamed(t, data, "proj")
	if !proj.Needed || proj.Kind != "local" || proj.Path != "." || proj.Toolchain != "host" {
		t.Errorf("root package = %+v", proj)
	}
	if !slices.Equal(proj.Deps, []string{"dep"}) {
		t.Errorf("root deps = %v", proj.Deps)
	}
	if proj.Options["with_dep"] != true || proj.Options["retries"] != float64(3) || proj.Options["label"] != "default" {
		t.Errorf("root options = %v", proj.Options)
	}
	app := inspectTargetNamed(t, proj, "app")
	if app.Kind != "binary" || !app.Default || app.Test || app.Output != "app" {
		t.Errorf("app target = %+v", app)
	}
	if app.InstallPath != "bin/app" {
		t.Errorf("app installPath = %q, want bin/app", app.InstallPath)
	}
	if app.Installs != nil {
		t.Errorf("app installs = %+v, want none", app.Installs)
	}
	if !slices.Equal(app.Files, []string{"src/*.c"}) || !slices.Equal(app.Includes, []string{"include"}) ||
		!slices.Equal(app.PublicIncludes, []string{"include"}) || !slices.Equal(app.Defines, []string{"APP_DEFINE=1"}) ||
		!slices.Equal(app.Deps, []string{"dep:deplib"}) {
		t.Errorf("app target fields = %+v", app)
	}
	docs := inspectTargetNamed(t, proj, "docs")
	if docs.Kind != "void" || docs.Output != "" || docs.Files != nil || docs.InstallPath != "" {
		t.Errorf("docs target = %+v", docs)
	}

	dep := inspectPackageNamed(t, data, "dep")
	if !dep.Needed || dep.Kind != "local" || dep.Path != "dep" || dep.Toolchain != "host" {
		t.Errorf("dep package = %+v", dep)
	}
	if dep.Options["dep_mode"] != "fast" {
		t.Errorf("dep options = %v", dep.Options)
	}
	deplib := inspectTargetNamed(t, dep, "deplib")
	if deplib.Kind != "static" || deplib.Output != "libdeplib.a" || !slices.Equal(deplib.PublicIncludes, []string{"include"}) {
		t.Errorf("deplib target = %+v", deplib)
	}
	if deplib.InstallPath != "lib/libdeplib.a" {
		t.Errorf("deplib installPath = %q, want lib/libdeplib.a", deplib.InstallPath)
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("consecutive inspect runs are not byte-identical")
	}
	for _, dir := range []string{"build", "install"} {
		if _, err := os.Stat(filepath.Join(project, dir)); !os.IsNotExist(err) {
			t.Fatalf("inspect created %s: %v", dir, err)
		}
	}
}

func TestInspectTargetFlags(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeProject := func(extraCFlag string) {
		t.Helper()
		writeExtensionFile(t, filepath.Join(project, "build.go"), fmt.Sprintf(`package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("turbo").SetType(api.OptionBool).SetDefault(true).
			SetOnApply(func(ctx *api.ConfigContext, val any) {
				ctx.AddGlobalCFlags("-DGLOBAL_TURBO")
			})
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).
			AddFiles("src/main.c").
			AddCFlags("-Wall", "-Wextra").
			AddIncludes("include").
			AddCxxFlags("-std=c++20").
			AddCFlags(%q).
			AddLdFlags("-Wl,--gc-sections", "-lm").
			AddDefines("APP_FLAGS=1")
		ctx.Target("plain").SetKind(api.TargetObject).AddFiles("src/plain.c")
		ctx.Target("linkonly").SetKind(api.TargetBinary).AddLdFlags("-nostdlib")
	})
}
`, extraCFlag))
	}
	writeProject("-Werror")
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	writeExtensionFile(t, filepath.Join(project, "src", "plain.c"), "int plain(void) { return 0; }\n")

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	proj := inspectPackageNamed(t, envelope.Data, "proj")
	app := inspectTargetNamed(t, proj, "app")
	if want := []string{"-Wall", "-Wextra", "-Werror"}; !slices.Equal(app.CFlags, want) {
		t.Errorf("app cFlags = %v, want %v", app.CFlags, want)
	}
	if want := []string{"-std=c++20"}; !slices.Equal(app.CxxFlags, want) {
		t.Errorf("app cxxFlags = %v, want %v", app.CxxFlags, want)
	}
	if want := []string{"-Wl,--gc-sections", "-lm"}; !slices.Equal(app.LdFlags, want) {
		t.Errorf("app ldFlags = %v, want %v", app.LdFlags, want)
	}
	plain := inspectTargetNamed(t, proj, "plain")
	if plain.CFlags != nil || plain.CxxFlags != nil || plain.LdFlags != nil {
		t.Errorf("plain flag fields = %v/%v/%v, want omitted", plain.CFlags, plain.CxxFlags, plain.LdFlags)
	}
	linkonly := inspectTargetNamed(t, proj, "linkonly")
	if linkonly.CFlags != nil || linkonly.CxxFlags != nil || !slices.Equal(linkonly.LdFlags, []string{"-nostdlib"}) {
		t.Errorf("linkonly flag fields = %v/%v/%v, want only ldFlags", linkonly.CFlags, linkonly.CxxFlags, linkonly.LdFlags)
	}
	for _, target := range []*inspectTarget{app, plain, linkonly} {
		for _, flag := range slices.Concat(target.CFlags, target.CxxFlags, target.LdFlags) {
			if flag == "-DGLOBAL_TURBO" {
				t.Errorf("%s flags contain the merged global flag %q", target.Name, flag)
			}
		}
	}
	if strings.Contains(stdout, `"cFlags": []`) || strings.Contains(stdout, `"cxxFlags": []`) || strings.Contains(stdout, `"ldFlags": []`) {
		t.Fatalf("inspect emitted an empty flag array:\n%s", stdout)
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("target flag lists are not deterministic")
	}

	writeProject("-Wpedantic")
	changed, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect after flag edit exit = %d, stderr = %q", code, stderr)
	}
	if changed == stdout {
		t.Fatal("an AddCFlags edit did not change the inspect document")
	}
	app = inspectTargetNamed(t, inspectPackageNamed(t, decodeInspectEnvelope(t, changed).Data, "proj"), "app")
	if want := []string{"-Wall", "-Wextra", "-Wpedantic"}; !slices.Equal(app.CFlags, want) {
		t.Fatalf("app cFlags after edit = %v, want %v", app.CFlags, want)
	}
}

func TestInspectGlobalFlags(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("with_extra").SetType(api.OptionBool).SetDefault(false)
		ctx.Option("turbo").SetType(api.OptionBool).SetDefault(true).
			SetOnApply(func(ctx *api.ConfigContext, _ any) {
				ctx.AddGlobalCFlags("-DTURBO")
			})
		ctx.GlobalOption("a_board").SetType(api.OptionString).SetDefault("evb")
		ctx.GlobalOption("z_feature").SetType(api.OptionBool).SetDefault(true)
		ctx.AddGlobalCFlags("-DROOT_GLOBAL", "-DROOT_GLOBAL")
		ctx.AddGlobalCxxFlags("-std=c++20")
		ctx.AddGlobalLdFlags("-Wl,--gc-sections")
		ctx.AddGlobalLinks("-lm")
	})
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("dep")
		if ctx.When("with_extra", true) {
			ctx.AddRequires("extra")
		}
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).
			AddFiles("src/main.c").
			AddDeps("dep:deplib")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	writeExtensionFile(t, filepath.Join(project, "dep", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("dep_turbo").SetType(api.OptionBool).SetDefault(true).
			SetOnApply(func(ctx *api.ConfigContext, _ any) {
				ctx.AddGlobalCFlags("-DDEP_APPLY")
			})
		ctx.AddGlobalCFlags("-DDEP_GLOBAL")
		ctx.AddGlobalLdFlags("-ldep")
		ctx.AddGlobalLinks("-ldep")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("deplib").SetKind(api.TargetStatic).AddFiles("src/lib.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "dep", "src", "lib.c"), "int dep(void) { return 0; }\n")
	writeExtensionFile(t, filepath.Join(project, "extra", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.AddGlobalCFlags("-DEXTRA_GLOBAL")
		ctx.AddGlobalCxxFlags("-DEXTRA_CXX")
		ctx.AddGlobalLdFlags("-DEXTRA_LD")
		ctx.AddGlobalLinks("-lextra")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("extralib").SetKind(api.TargetStatic).AddFiles("src/extra.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "extra", "src", "extra.c"), "int extra(void) { return 0; }\n")

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	data := envelope.Data
	if want := []string{"-DDEP_GLOBAL", "-DDEP_APPLY", "-DROOT_GLOBAL", "-DROOT_GLOBAL", "-DTURBO"}; !slices.Equal(data.GlobalCFlags, want) {
		t.Errorf("globalCFlags = %v, want %v", data.GlobalCFlags, want)
	}
	if want := []string{"-std=c++20"}; !slices.Equal(data.GlobalCxxFlags, want) {
		t.Errorf("globalCxxFlags = %v, want %v", data.GlobalCxxFlags, want)
	}
	if want := []string{"-ldep", "-Wl,--gc-sections"}; !slices.Equal(data.GlobalLdFlags, want) {
		t.Errorf("globalLdFlags = %v, want %v", data.GlobalLdFlags, want)
	}
	if want := []string{"-ldep", "-lm"}; !slices.Equal(data.GlobalLinks, want) {
		t.Errorf("globalLinks = %v, want %v", data.GlobalLinks, want)
	}
	if want := []string{`CONFIG_A_BOARD="evb"`, "CONFIG_Z_FEATURE=1"}; !slices.Equal(data.GlobalDefines, want) {
		t.Errorf("globalDefines = %v, want %v", data.GlobalDefines, want)
	}
	for _, array := range [][]string{data.GlobalCFlags, data.GlobalCxxFlags, data.GlobalLdFlags, data.GlobalLinks} {
		for _, flag := range array {
			if strings.Contains(flag, "EXTRA") {
				t.Errorf("unneeded package flag %q leaked into global flags", flag)
			}
		}
	}
	extra := inspectPackageNamed(t, data, "extra")
	if extra.Needed {
		t.Errorf("package extra is needed despite with_extra=false")
	}
	if extra.Targets != nil {
		t.Errorf("unneeded package extra declared targets: %+v", extra.Targets)
	}
	if strings.Contains(stdout, `"globalCFlags": []`) || strings.Contains(stdout, `"globalCxxFlags": []`) ||
		strings.Contains(stdout, `"globalLdFlags": []`) || strings.Contains(stdout, `"globalLinks": []`) ||
		strings.Contains(stdout, `"globalDefines": []`) {
		t.Fatalf("inspect emitted an empty global flag array:\n%s", stdout)
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("global flag lists are not deterministic")
	}
}

func TestInspectBuildStateAndPackageDirs(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())

	checkAbsent := func(t *testing.T) *inspectData {
		t.Helper()
		stdout, stderr, code := runInspectCommand(t, project, "inspect")
		if code != 0 {
			t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
		}
		envelope := decodeInspectEnvelope(t, stdout)
		if envelope.Status != "ok" || envelope.Data == nil {
			t.Fatalf("envelope = %+v", envelope)
		}
		if len(envelope.Diagnostics) != 0 {
			t.Fatalf("diagnostics = %+v, want none", envelope.Diagnostics)
		}
		data := envelope.Data
		if data.CompileDatabasePresent || data.BuildReportPresent || data.BuildReportState != nil {
			t.Fatalf("build state present before a build: db=%v report=%v state=%+v",
				data.CompileDatabasePresent, data.BuildReportPresent, data.BuildReportState)
		}
		for _, want := range []struct {
			pkg  string
			base string
		}{{"proj", project}, {"dep", filepath.Join(project, "dep")}} {
			pkg := inspectPackageNamed(t, data, want.pkg)
			if !pkg.Needed || pkg.BuildDir == "" || !filepath.IsAbs(pkg.BuildDir) {
				t.Fatalf("%s buildDir = %q (needed %v)", want.pkg, pkg.BuildDir, pkg.Needed)
			}
			if !strings.HasPrefix(pkg.BuildDir, filepath.Join(want.base, "build")+string(filepath.Separator)) {
				t.Fatalf("%s buildDir = %q, want a path under %s", want.pkg, pkg.BuildDir, filepath.Join(want.base, "build"))
			}
			if pkg.BuildKey == "" || pkg.BuildKey != filepath.Base(pkg.BuildDir) {
				t.Fatalf("%s buildKey = %q, buildDir = %q", want.pkg, pkg.BuildKey, pkg.BuildDir)
			}
			if _, err := os.Stat(pkg.BuildDir); !os.IsNotExist(err) {
				t.Fatalf("inspect materialized %s build dir %s: %v", want.pkg, pkg.BuildDir, err)
			}
		}
		return data
	}
	checkAbsent(t)

	if _, stderr, code := runInspectCommand(t, project, "build"); code != 0 {
		t.Fatalf("build exit = %d, stderr = %q", code, stderr)
	}
	rawReport, err := os.ReadFile(filepath.Join(project, "build", "vmake-build.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		BuildID string          `json:"buildId"`
		Status  string          `json:"status"`
		Summary json.RawMessage `json:"summary"`
	}
	if err := json.Unmarshal(rawReport, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "succeeded" || report.BuildID == "" || len(report.Summary) == 0 {
		t.Fatalf("build report = %+v", report)
	}

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect after build exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", envelope.Diagnostics)
	}
	data := envelope.Data
	if !data.CompileDatabasePresent || !data.BuildReportPresent {
		t.Fatalf("presence after build: db=%v report=%v", data.CompileDatabasePresent, data.BuildReportPresent)
	}
	state := data.BuildReportState
	if state == nil {
		t.Fatal("buildReportState is absent after a successful build")
	}
	if state.Status != "succeeded" || state.BuildID != report.BuildID || state.Command != "build" {
		t.Fatalf("buildReportState = %+v, report = %+v", state, report)
	}
	if state.Started == "" || state.Finished == "" || state.Error != "" {
		t.Fatalf("buildReportState identity = %+v", state)
	}
	if !state.ConfigMatches {
		t.Fatal("buildReportState.configMatches = false for the build's own configuration")
	}
	if !state.EffectiveConfigMatches {
		t.Fatal("buildReportState.effectiveConfigMatches = false for the build's own configuration")
	}
	var wantSummary, gotSummary any
	if err := json.Unmarshal(report.Summary, &wantSummary); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(state.Summary, &gotSummary); err != nil {
		t.Fatalf("buildReportState.summary = %s: %v", state.Summary, err)
	}
	if !reflect.DeepEqual(gotSummary, wantSummary) {
		t.Fatalf("buildReportState.summary = %s, report summary = %s", state.Summary, report.Summary)
	}
	for _, name := range []string{"proj", "dep"} {
		pkg := inspectPackageNamed(t, data, name)
		if pkg.BuildDir == "" || pkg.BuildKey == "" {
			t.Fatalf("%s buildDir/buildKey missing after build: %+v", name, pkg)
		}
		info, err := os.Stat(pkg.BuildDir)
		if err != nil || !info.IsDir() {
			t.Fatalf("%s buildDir %q is not a directory: %v", name, pkg.BuildDir, err)
		}
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("consecutive inspect runs after a build are not byte-identical")
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect", "--set", "proj/with_dep=false")
	if code != 0 || stderr != "" {
		t.Fatalf("hypothetical inspect exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	if data.BuildReportState == nil || data.BuildReportState.ConfigMatches {
		t.Fatalf("--set buildReportState = %+v, want configMatches false", data.BuildReportState)
	}
	if data.BuildReportState.EffectiveConfigMatches {
		t.Fatalf("changed --set buildReportState = %+v, want effectiveConfigMatches false", data.BuildReportState)
	}
	if dep := inspectPackageNamed(t, data, "dep"); dep.Needed || dep.BuildDir != "" || dep.BuildKey != "" {
		t.Fatalf("pruned package kept build fields: %+v", dep)
	}

	if stdout, stderr, code := runInspectCommand(t, project, "clean"); code != 0 {
		t.Fatalf("clean exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	data = checkAbsent(t)
	for _, name := range []string{"proj", "dep"} {
		pkg := inspectPackageNamed(t, data, name)
		if _, err := os.Stat(pkg.BuildDir); !os.IsNotExist(err) {
			t.Fatalf("%s build dir survived clean: %q", name, pkg.BuildDir)
		}
	}
}

func TestInspectBuildDirFields(t *testing.T) {
	key := "debug-abc"
	local := &api.PkgDirs{SourceDir: "/src/lib", BuildDir: filepath.Join("/src/lib", "build", key)}
	dir, buildKey := inspectBuildDirFields(local, true)
	if dir != local.BuildDir || buildKey != key {
		t.Fatalf("local fields = %q/%q", dir, buildKey)
	}
	if dir, buildKey := inspectBuildDirFields(local, false); dir != local.BuildDir || buildKey != "" {
		t.Fatalf("remote-like fields = %q/%q", dir, buildKey)
	}
	remote := &api.PkgDirs{SourceDir: "/deps/owner/src", BuildDir: filepath.Join("/deps/owner/out", "member", key, "build")}
	if dir, buildKey := inspectBuildDirFields(remote, false); dir != remote.BuildDir || buildKey != "" {
		t.Fatalf("remote fields = %q/%q", dir, buildKey)
	}
	odd := &api.PkgDirs{BuildDir: filepath.Join("/tmp", key)}
	if _, buildKey := inspectBuildDirFields(odd, true); buildKey != "" {
		t.Fatalf("non-layout build dir got buildKey %q", buildKey)
	}
	if dir, buildKey := inspectBuildDirFields(nil, true); dir != "" || buildKey != "" {
		t.Fatalf("nil dirs = %q/%q", dir, buildKey)
	}
	if dir, buildKey := inspectBuildDirFields(&api.PkgDirs{SourceDir: "/src"}, true); dir != "" || buildKey != "" {
		t.Fatalf("empty build dir = %q/%q", dir, buildKey)
	}
}

func TestInspectReportStateProvenance(t *testing.T) {
	project := writeInspectFreshnessProject(t)
	revision := initProvenanceGitRepo(t, project)

	if _, stderr, code := runInspectCommand(t, project, "build"); code != 0 {
		t.Fatalf("build exit = %d, stderr = %q", code, stderr)
	}
	buildReport := loadBuildReport(t, project)
	assertReportProvenance(t, buildReport, revision, false)

	if _, stderr, code := runInspectCommand(t, project, "test"); code != 0 {
		t.Fatalf("test exit = %d, stderr = %q", code, stderr)
	}
	testReport := loadTestReport(t, project)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	if data == nil || data.BuildReportState == nil || data.TestReportState == nil {
		t.Fatalf("inspect report state missing: %+v", data)
	}
	buildState, testState := data.BuildReportState, data.TestReportState
	if buildState.VMakeVersion != buildReport.VMakeVersion || buildState.VMakeCommit != buildReport.VMakeCommit ||
		buildState.ProjectRevision != buildReport.ProjectRevision || buildState.ProjectDirty == nil ||
		*buildState.ProjectDirty != *buildReport.ProjectDirty {
		t.Fatalf("buildReportState provenance = %+v, report provenance = %+v", buildState, buildReport)
	}
	if testState.VMakeVersion != testReport.VMakeVersion || testState.VMakeCommit != testReport.VMakeCommit ||
		testState.ProjectRevision != testReport.ProjectRevision || testState.ProjectDirty == nil ||
		testReport.ProjectDirty == nil || *testState.ProjectDirty != *testReport.ProjectDirty {
		t.Fatalf("testReportState provenance = %+v, report provenance = %+v", testState, testReport)
	}
}

func TestInspectBuildReportConfigMatch(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) { p.SetRoot(true) }
`)
	configPath := filepath.Join(project, ".vmake", "config.json")
	writeExtensionFile(t, configPath, `{"version":"1","global":{"mode":"release"}}`)
	digest, _, err := fileDigest(configPath)
	if err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(project, "build", "vmake-build.json")
	writeReport := func(configSHA256 string) {
		t.Helper()
		writeExtensionFile(t, reportPath, `{"schemaVersion":2,"buildId":"abc123","status":"succeeded","command":"build","started":"2026-01-01T00:00:00Z","finished":"2026-01-01T00:00:01Z","configSha256":"`+configSHA256+`","summary":{"targetsTotal":1,"targetsBuilt":1}}`)
	}

	writeReport(digest)
	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	state := data.BuildReportState
	if state == nil || !state.ConfigMatches || !state.EffectiveConfigMatches || state.BuildID != "abc123" || state.Status != "succeeded" {
		t.Fatalf("matching report state = %+v", state)
	}
	if string(state.Summary) == "" {
		t.Fatal("matching report state lost the summary")
	}
	if data.Freshness == nil || data.Freshness.EffectiveConfigMatches == nil || !*data.Freshness.EffectiveConfigMatches {
		t.Fatalf("matching freshness effectiveConfigMatches = %+v", data.Freshness)
	}

	writeReport("not-the-active-digest")
	stdout, stderr, code = runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	if data.BuildReportState == nil || data.BuildReportState.ConfigMatches || data.BuildReportState.EffectiveConfigMatches {
		t.Fatalf("mismatched report state = %+v, want both config match fields false", data.BuildReportState)
	}
	if data.Freshness == nil || data.Freshness.EffectiveConfigMatches == nil || *data.Freshness.EffectiveConfigMatches {
		t.Fatalf("mismatched freshness effectiveConfigMatches = %+v", data.Freshness)
	}
}

func TestInspectEffectiveConfigMatches(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("retries").SetType(api.OptionInt).SetDefault(3)
		ctx.Option("label").SetType(api.OptionString).SetDefault("default")
		ctx.GlobalOption("board").SetType(api.OptionString)
	})
}
`)
	configPath := filepath.Join(project, ".vmake", "config.json")
	writeExtensionFile(t, configPath, `{"version":"1","global":{"mode":"release","options":{"board":"demo"}},"entries":{"proj":{"options":{"retries":5}}}}`)
	digest, _, err := fileDigest(configPath)
	if err != nil {
		t.Fatal(err)
	}
	report := `{"schemaVersion":2,"buildId":"effective","status":"succeeded","command":"%s","started":"2026-01-01T00:00:00Z","finished":"2026-01-01T00:00:01Z","configSha256":"` + digest + `","summary":{"targetsTotal":1}}`
	writeExtensionFile(t, filepath.Join(project, "build", "vmake-build.json"), fmt.Sprintf(report, "build"))
	writeExtensionFile(t, filepath.Join(project, "build", "vmake-test.json"), fmt.Sprintf(report, "test"))

	check := func(t *testing.T, args []string, wantConfig, wantEffective bool) {
		t.Helper()
		stdout, stderr, code := runInspectCommand(t, project, append([]string{"inspect"}, args...)...)
		if code != 0 || stderr != "" {
			t.Fatalf("inspect %v exit = %d, stderr = %q", args, code, stderr)
		}
		data := decodeInspectEnvelope(t, stdout).Data
		for _, state := range []struct {
			name  string
			value *inspectBuildReportState
		}{
			{"buildReportState", data.BuildReportState},
			{"testReportState", data.TestReportState},
		} {
			if state.value == nil {
				t.Fatalf("%s missing for inspect %v", state.name, args)
			}
			if state.value.ConfigMatches != wantConfig || state.value.EffectiveConfigMatches != wantEffective {
				t.Fatalf("%s configMatches/effectiveConfigMatches = %v/%v, want %v/%v for inspect %v",
					state.name, state.value.ConfigMatches, state.value.EffectiveConfigMatches, wantConfig, wantEffective, args)
			}
		}
		if data.Freshness == nil || data.Freshness.ConfigMatches == nil || data.Freshness.EffectiveConfigMatches == nil {
			t.Fatalf("freshness config match fields missing for inspect %v: %+v", args, data.Freshness)
		}
		if *data.Freshness.ConfigMatches != wantConfig || *data.Freshness.EffectiveConfigMatches != wantEffective {
			t.Fatalf("freshness configMatches/effectiveConfigMatches = %v/%v, want %v/%v for inspect %v",
				*data.Freshness.ConfigMatches, *data.Freshness.EffectiveConfigMatches, wantConfig, wantEffective, args)
		}
	}

	check(t, nil, true, true)
	check(t, []string{"--set", "proj/retries=5"}, false, true)
	check(t, []string{"--set", "proj/label=default"}, false, true)
	check(t, []string{"--set", "mode=release"}, false, true)
	check(t, []string{"--set", "board=demo"}, false, true)
	check(t, []string{"--set", "proj/retries=5", "--set", "mode=release"}, false, true)
	check(t, []string{"--set", "proj/retries=4"}, false, false)
	check(t, []string{"--set", "mode=debug"}, false, false)
	check(t, []string{"--set", "proj/retries=5", "--set", "proj/retries=4"}, false, false)
	check(t, []string{"--set", "board=other"}, false, false)

	mismatched := `{"schemaVersion":2,"buildId":"other","status":"succeeded","command":"build","started":"2026-01-01T00:00:00Z","finished":"2026-01-01T00:00:01Z","configSha256":"not-the-active-digest"}`
	writeExtensionFile(t, filepath.Join(project, "build", "vmake-build.json"), mismatched)
	writeExtensionFile(t, filepath.Join(project, "build", "vmake-test.json"), mismatched)
	check(t, []string{"--set", "proj/retries=5"}, false, false)

	writeExtensionFile(t, configPath, `{"version":"1"}`)
	digest, _, err = fileDigest(configPath)
	if err != nil {
		t.Fatal(err)
	}
	report = `{"schemaVersion":2,"buildId":"effective","status":"succeeded","command":"%s","started":"2026-01-01T00:00:00Z","finished":"2026-01-01T00:00:01Z","configSha256":"` + digest + `","summary":{"targetsTotal":1}}`
	writeExtensionFile(t, filepath.Join(project, "build", "vmake-build.json"), fmt.Sprintf(report, "build"))
	writeExtensionFile(t, filepath.Join(project, "build", "vmake-test.json"), fmt.Sprintf(report, "test"))
	check(t, nil, true, true)
	check(t, []string{"--set", "mode=debug"}, false, true)
	check(t, []string{"--set", "mode=release"}, false, false)
}

func TestInspectFreshnessWithoutReport(t *testing.T) {
	project := writeInspectFreshnessProject(t)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	if data == nil || data.BuildReportPresent || data.BuildReportState != nil {
		t.Fatalf("build report state without a report: %+v", data)
	}
	freshness := data.Freshness
	if freshness == nil || freshness.Known {
		t.Fatalf("freshness = %+v, want present with known false", freshness)
	}
	if freshness.ReportStatus != "" || freshness.ReportFinished != "" || freshness.ConfigMatches != nil ||
		freshness.EffectiveConfigMatches != nil || freshness.SourceChanged != nil ||
		freshness.ChangedSources != nil || freshness.Truncated != nil {
		t.Fatalf("unknown freshness exposed report/source fields: %+v", freshness)
	}
}

func TestInspectFreshnessUnparsableFinish(t *testing.T) {
	project := writeInspectFreshnessProject(t)
	digest := writeInspectConfig(t, project)

	for _, finished := range []string{"", "not-a-timestamp"} {
		writeInspectBuildReport(t, project, finished, digest)
		stdout, stderr, code := runInspectCommand(t, project, "inspect")
		if code != 0 || stderr != "" {
			t.Fatalf("inspect exit = %d, stderr = %q (finished %q)", code, stderr, finished)
		}
		data := decodeInspectEnvelope(t, stdout).Data
		if data == nil || data.BuildReportState == nil {
			t.Fatalf("parsable report without state: %+v (finished %q)", data, finished)
		}
		freshness := data.Freshness
		if freshness == nil || freshness.Known {
			t.Fatalf("freshness = %+v, want known false (finished %q)", freshness, finished)
		}
		if freshness.ReportStatus != "succeeded" || freshness.ConfigMatches == nil || !*freshness.ConfigMatches {
			t.Fatalf("freshness identity = %+v (finished %q)", freshness, finished)
		}
		if freshness.EffectiveConfigMatches == nil || !*freshness.EffectiveConfigMatches {
			t.Fatalf("freshness effectiveConfigMatches = %+v (finished %q)", freshness.EffectiveConfigMatches, finished)
		}
		if freshness.ReportFinished != "" || freshness.SourceChanged != nil ||
			freshness.ChangedSources != nil || freshness.Truncated != nil {
			t.Fatalf("unparsable finish exposed source fields: %+v (finished %q)", freshness, finished)
		}
	}
}

func TestInspectFreshnessAfterBuildAndSourceEdit(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())
	if _, stderr, code := runBuildCommand(t, project, "build"); code != 0 {
		t.Fatalf("build exit = %d, stderr = %q", code, stderr)
	}
	report := loadBuildReport(t, project)
	finished, err := time.Parse(time.RFC3339Nano, report.Finished)
	if err != nil {
		t.Fatalf("report finished %q: %v", report.Finished, err)
	}

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	freshness := data.Freshness
	if freshness == nil || !freshness.Known {
		t.Fatalf("freshness after build = %+v", freshness)
	}
	if freshness.ReportStatus != report.Status || freshness.ReportFinished != report.Finished {
		t.Fatalf("freshness report identity = %+v, report = %+v", freshness, report)
	}
	if freshness.ConfigMatches == nil || !*freshness.ConfigMatches {
		t.Fatalf("freshness configMatches = %+v, want true", freshness.ConfigMatches)
	}
	if freshness.EffectiveConfigMatches == nil || !*freshness.EffectiveConfigMatches {
		t.Fatalf("freshness effectiveConfigMatches = %+v, want true", freshness.EffectiveConfigMatches)
	}
	if freshness.SourceChanged == nil || *freshness.SourceChanged {
		t.Fatalf("freshness sourceChanged = %+v, want false", freshness.SourceChanged)
	}
	if len(freshness.ChangedSources) != 0 {
		t.Fatalf("changedSources after build = %v, want none", freshness.ChangedSources)
	}
	if freshness.Truncated == nil || *freshness.Truncated {
		t.Fatalf("freshness truncated = %+v, want false", freshness.Truncated)
	}

	edited := finished.Add(2 * time.Second)
	for _, rel := range []string{"src/main.c", "dep/src/lib.c"} {
		writeExtensionFile(t, filepath.Join(project, filepath.FromSlash(rel)), "int edited(void) { return 1; }\n")
	}
	setInspectSourceTime(t, []string{filepath.Join(project, "src", "main.c"), filepath.Join(project, "dep", "src", "lib.c")}, edited)

	stdout, stderr, code = runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect after edit exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	freshness = data.Freshness
	if freshness == nil || !freshness.Known {
		t.Fatalf("freshness after edit = %+v", freshness)
	}
	if freshness.ConfigMatches == nil || !*freshness.ConfigMatches {
		t.Fatalf("source edit changed configMatches: %+v", freshness.ConfigMatches)
	}
	if freshness.EffectiveConfigMatches == nil || !*freshness.EffectiveConfigMatches {
		t.Fatalf("source edit changed effectiveConfigMatches: %+v", freshness.EffectiveConfigMatches)
	}
	if freshness.SourceChanged == nil || !*freshness.SourceChanged {
		t.Fatalf("sourceChanged after edit = %+v, want true", freshness.SourceChanged)
	}
	if want := []string{"dep:src/lib.c", "proj:src/main.c"}; !slices.Equal(freshness.ChangedSources, want) {
		t.Fatalf("changedSources = %v, want %v", freshness.ChangedSources, want)
	}
	if freshness.Truncated == nil || *freshness.Truncated {
		t.Fatalf("freshness truncated after edit = %+v, want false", freshness.Truncated)
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("repeat inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("consecutive inspect runs after a source edit are not byte-identical")
	}
}

func TestInspectFreshnessConfigMismatch(t *testing.T) {
	project := writeInspectFreshnessProject(t)
	writeInspectConfig(t, project)
	writeInspectBuildReport(t, project, "2020-01-01T00:00:00Z", "not-the-active-digest")
	source := filepath.Join(project, "src", "main.c")
	setInspectSourceTime(t, []string{source}, time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	if data.BuildReportState == nil || data.BuildReportState.ConfigMatches {
		t.Fatalf("buildReportState configMatches = %+v, want false", data.BuildReportState)
	}
	freshness := data.Freshness
	if freshness == nil || !freshness.Known {
		t.Fatalf("freshness with mismatched config = %+v", freshness)
	}
	if freshness.ConfigMatches == nil || *freshness.ConfigMatches {
		t.Fatalf("freshness configMatches = %+v, want false", freshness.ConfigMatches)
	}
	if freshness.EffectiveConfigMatches == nil || *freshness.EffectiveConfigMatches {
		t.Fatalf("freshness effectiveConfigMatches = %+v, want false", freshness.EffectiveConfigMatches)
	}
	if freshness.SourceChanged == nil || !*freshness.SourceChanged {
		t.Fatalf("freshness sourceChanged = %+v, want true despite config mismatch", freshness.SourceChanged)
	}
	if want := []string{"proj:src/main.c"}; !slices.Equal(freshness.ChangedSources, want) {
		t.Fatalf("changedSources = %v, want %v", freshness.ChangedSources, want)
	}
}

func TestInspectFreshnessTruncatesChangedSources(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
		ctx.Target("app_again").SetKind(api.TargetBinary).AddFiles("src/*.c")
	})
}
`)
	paths := make([]string, 0, 55)
	for i := 0; i < 55; i++ {
		path := filepath.Join(project, "src", fmt.Sprintf("f%02d.c", i))
		writeExtensionFile(t, path, fmt.Sprintf("int f%d(void) { return %d; }\n", i, i))
		paths = append(paths, path)
	}
	setInspectSourceTime(t, paths, time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	digest := writeInspectConfig(t, project)
	writeInspectBuildReport(t, project, "2020-01-01T00:00:00Z", digest)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	freshness := data.Freshness
	if freshness == nil || !freshness.Known {
		t.Fatalf("freshness = %+v", freshness)
	}
	if freshness.ConfigMatches == nil || !*freshness.ConfigMatches {
		t.Fatalf("freshness configMatches = %+v, want true", freshness.ConfigMatches)
	}
	if freshness.EffectiveConfigMatches == nil || !*freshness.EffectiveConfigMatches {
		t.Fatalf("freshness effectiveConfigMatches = %+v, want true", freshness.EffectiveConfigMatches)
	}
	if freshness.SourceChanged == nil || !*freshness.SourceChanged {
		t.Fatalf("freshness sourceChanged = %+v, want true", freshness.SourceChanged)
	}
	if freshness.Truncated == nil || !*freshness.Truncated {
		t.Fatalf("freshness truncated = %+v, want true", freshness.Truncated)
	}
	if len(freshness.ChangedSources) != inspectFreshnessChangedSourcesLimit {
		t.Fatalf("changedSources length = %d, want %d", len(freshness.ChangedSources), inspectFreshnessChangedSourcesLimit)
	}
	if !slices.IsSorted(freshness.ChangedSources) {
		t.Fatalf("changedSources is not sorted: %v", freshness.ChangedSources)
	}
	seen := make(map[string]bool, len(freshness.ChangedSources))
	for _, source := range freshness.ChangedSources {
		if seen[source] {
			t.Fatalf("changedSources has duplicate %q: %v", source, freshness.ChangedSources)
		}
		seen[source] = true
	}
	if first, last := freshness.ChangedSources[0], freshness.ChangedSources[len(freshness.ChangedSources)-1]; first != "proj:src/f00.c" || last != "proj:src/f49.c" {
		t.Fatalf("changedSources bounds = %q..%q", first, last)
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("repeat inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("freshness output is not deterministic")
	}
}

func TestInspectResolvedFiles(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c").RemoveFiles("src/skip.c")
		ctx.Target("docs").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error { return nil })
		ctx.Target("nomatch").SetKind(api.TargetObject).AddFiles("missing/*.c")
	})
}
`)
	for _, name := range []string{"b.c", "a.c", "skip.c"} {
		writeExtensionFile(t, filepath.Join(project, "src", name), "int f(void) { return 0; }\n")
	}
	writeExtensionFile(t, filepath.Join(project, "src", "notes.txt"), "not a source\n")

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || len(envelope.Diagnostics) != 0 || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	proj := inspectPackageNamed(t, envelope.Data, "proj")
	app := inspectTargetNamed(t, proj, "app")
	if !slices.Equal(app.Files, []string{"src/*.c"}) || !slices.Equal(app.ExcludedFiles, []string{"src/skip.c"}) {
		t.Fatalf("declared patterns = %v / %v", app.Files, app.ExcludedFiles)
	}
	if want := []string{"src/a.c", "src/b.c"}; !slices.Equal(app.ResolvedFiles, want) {
		t.Fatalf("resolvedFiles = %v, want %v", app.ResolvedFiles, want)
	}
	if app.ResolvedFileCount != 2 || app.ResolvedFilesTruncated {
		t.Fatalf("resolvedFileCount/truncated = %d/%v", app.ResolvedFileCount, app.ResolvedFilesTruncated)
	}

	docs := inspectTargetNamed(t, proj, "docs")
	if docs.ResolvedFiles != nil || docs.ResolvedFileCount != 0 || docs.ResolvedFilesTruncated {
		t.Fatalf("docs resolved fields = %v/%d/%v, want omitted", docs.ResolvedFiles, docs.ResolvedFileCount, docs.ResolvedFilesTruncated)
	}
	nomatch := inspectTargetNamed(t, proj, "nomatch")
	if nomatch.ResolvedFiles != nil || nomatch.ResolvedFileCount != 0 || nomatch.ResolvedFilesTruncated {
		t.Fatalf("nomatch resolved fields = %v/%d/%v, want omitted", nomatch.ResolvedFiles, nomatch.ResolvedFileCount, nomatch.ResolvedFilesTruncated)
	}
	if _, err := os.Stat(filepath.Join(project, "build")); !os.IsNotExist(err) {
		t.Fatalf("inspect created build/: %v", err)
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("resolved file lists are not deterministic")
	}
}

func TestInspectResolvedFilesCap(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
	})
}
`)
	const total = inspectResolvedFilesLimit + 5
	for i := 0; i < total; i++ {
		writeExtensionFile(t, filepath.Join(project, "src", fmt.Sprintf("f%03d.c", i)), "int f(void) { return 0; }\n")
	}

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	app := inspectTargetNamed(t, inspectPackageNamed(t, envelope.Data, "proj"), "app")
	if len(app.ResolvedFiles) != inspectResolvedFilesLimit {
		t.Fatalf("resolvedFiles length = %d, want %d", len(app.ResolvedFiles), inspectResolvedFilesLimit)
	}
	if !slices.IsSorted(app.ResolvedFiles) {
		t.Fatalf("resolvedFiles is not sorted: %v", app.ResolvedFiles)
	}
	if app.ResolvedFiles[0] != "src/f000.c" || app.ResolvedFiles[len(app.ResolvedFiles)-1] != "src/f199.c" {
		t.Fatalf("resolvedFiles bounds = %q..%q", app.ResolvedFiles[0], app.ResolvedFiles[len(app.ResolvedFiles)-1])
	}
	if app.ResolvedFileCount != total || !app.ResolvedFilesTruncated {
		t.Fatalf("resolvedFileCount/truncated = %d/%v, want %d/true", app.ResolvedFileCount, app.ResolvedFilesTruncated, total)
	}
}

func TestInspectResolvedFilesGeneratedAfterBuild(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import (
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		generated, err := filepath.Rel(p.SourceDir(), filepath.Join(p.BuildDir(), "generated.c"))
		if err != nil {
			panic(err)
		}
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c").AddFiles(filepath.ToSlash(generated))
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	proj := inspectPackageNamed(t, data, "proj")
	app := inspectTargetNamed(t, proj, "app")
	if !slices.Equal(app.ResolvedFiles, []string{"src/main.c"}) {
		t.Fatalf("resolvedFiles before generated file = %v, want src/main.c", app.ResolvedFiles)
	}
	if proj.BuildDir == "" {
		t.Fatal("inspect did not resolve a build directory")
	}

	generated := filepath.Join(proj.BuildDir, "generated.c")
	writeExtensionFile(t, generated, "int generated(void) { return 0; }\n")
	rel, err := filepath.Rel(project, generated)
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect after generated file exit = %d, stderr = %q", code, stderr)
	}
	app = inspectTargetNamed(t, inspectPackageNamed(t, decodeInspectEnvelope(t, stdout).Data, "proj"), "app")
	if want := []string{filepath.ToSlash(rel), "src/main.c"}; !slices.Equal(app.ResolvedFiles, want) {
		t.Fatalf("resolvedFiles after generated file = %v, want %v", app.ResolvedFiles, want)
	}
	if app.ResolvedFileCount != 2 {
		t.Fatalf("resolvedFileCount after generated file = %d, want 2", app.ResolvedFileCount)
	}
}

func TestInspectResolvedFilesMissingSource(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary)
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "lib", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://example.invalid/lib.git")
		p.AddVersion("1.0.0", "v1.0.0")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("lib").SetKind(api.TargetStatic).AddFiles("src/*.c")
	})
}
`)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	lib := inspectPackageNamed(t, envelope.Data, "lib")
	if lib.SourcePresent || lib.Targets != nil {
		t.Fatalf("lib source=%v targets=%+v, want absent source without targets", lib.SourcePresent, lib.Targets)
	}
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_W_SOURCE_MISSING" {
		t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
	}
}

func TestInspectResolvedFilesFirmwareSample(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".work", "arena", "project"))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Skipf("firmware sample project %s unavailable", root)
	}
	stdout, stderr, code := runInspectCommand(t, root, "inspect", "--set", "toolchain=host")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	var firmware *inspectTarget
	for i := range envelope.Data.Packages {
		pkg := &envelope.Data.Packages[i]
		if pkg.Name != "firmware" {
			continue
		}
		for j := range pkg.Targets {
			if pkg.Targets[j].Name == "firmware" {
				firmware = &pkg.Targets[j]
			}
		}
	}
	if firmware == nil {
		t.Skip("firmware sample has no firmware:firmware target")
	}
	if !slices.Contains(firmware.ResolvedFiles, "src/main.c") {
		t.Fatalf("firmware resolvedFiles = %v, want src/main.c", firmware.ResolvedFiles)
	}
}

func TestInspectTargetFlagsFirmwareSample(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".work", "arena", "project"))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Skipf("firmware sample project %s unavailable", root)
	}
	stdout, stderr, code := runInspectCommand(t, root, "inspect", "--set", "toolchain=host")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	var gpu *inspectTarget
	for i := range envelope.Data.Packages {
		pkg := &envelope.Data.Packages[i]
		for j := range pkg.Targets {
			target := &pkg.Targets[j]
			for _, flag := range slices.Concat(target.CFlags, target.CxxFlags, target.LdFlags) {
				if flag == "-DCONFIG_GRAPHICS_ACCEL" || flag == "-DCONFIG_FIRMWARE_LVGL" {
					t.Fatalf("%s:%s flags contain the sample's merged global flag %q", pkg.Name, target.Name, flag)
				}
			}
		}
		if pkg.Name == "py32-gpu" {
			for j := range pkg.Targets {
				if pkg.Targets[j].Name == "py32-gpu" {
					gpu = &pkg.Targets[j]
				}
			}
		}
	}
	if gpu == nil || len(gpu.CFlags) == 0 {
		t.Skip("firmware sample has no flag-bearing py32-gpu target")
	}
	for _, want := range []string{"-fshort-enums", "-fshort-wchar"} {
		if !slices.Contains(gpu.CFlags, want) {
			t.Fatalf("py32-gpu cFlags = %v, missing declared flag %q", gpu.CFlags, want)
		}
	}
	for _, toolchainDefault := range []string{"-Wstrict-prototypes", "-fstack-protector-strong"} {
		if slices.Contains(gpu.CFlags, toolchainDefault) {
			t.Fatalf("py32-gpu cFlags = %v, contains host toolchain default %q", gpu.CFlags, toolchainDefault)
		}
	}
}

func TestInspectUnreadableBuildReport(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	reportPath := filepath.Join(project, "build", "vmake-build.json")
	for _, corrupt := range []string{"{this is not a build report\n", "null", "[]", ""} {
		writeExtensionFile(t, reportPath, corrupt)
		stdout, stderr, code := runInspectCommand(t, project, "inspect")
		if code != 0 {
			t.Fatalf("inspect exit = %d, stderr = %q (report %q)", code, stderr, corrupt)
		}
		envelope := decodeInspectEnvelope(t, stdout)
		if envelope.Status != "ok" || envelope.Data == nil {
			t.Fatalf("envelope = %+v (report %q)", envelope, corrupt)
		}
		data := envelope.Data
		if !data.BuildReportPresent || data.BuildReportState != nil {
			t.Fatalf("corrupt report %q: present=%v state=%+v", corrupt, data.BuildReportPresent, data.BuildReportState)
		}
		if data.CompileDatabasePresent {
			t.Fatal("compileDatabasePresent = true without a compile database")
		}
		if data.Freshness == nil || data.Freshness.Known {
			t.Fatalf("corrupt report freshness = %+v, want known false (report %q)", data.Freshness, corrupt)
		}
		if data.Freshness.ReportStatus != "" || data.Freshness.ReportFinished != "" ||
			data.Freshness.ConfigMatches != nil || data.Freshness.EffectiveConfigMatches != nil ||
			data.Freshness.SourceChanged != nil ||
			data.Freshness.ChangedSources != nil || data.Freshness.Truncated != nil {
			t.Fatalf("corrupt report freshness exposed fields: %+v (report %q)", data.Freshness, corrupt)
		}
		if len(envelope.Diagnostics) != 1 {
			t.Fatalf("diagnostics = %+v, want one warning (report %q)", envelope.Diagnostics, corrupt)
		}
		diagnostic := envelope.Diagnostics[0]
		if diagnostic.Severity != "warning" || diagnostic.Code != "VMAKE_W_REPORT_UNREADABLE" || !strings.Contains(diagnostic.Message, reportPath) {
			t.Fatalf("diagnostic = %+v (report %q)", diagnostic, corrupt)
		}
		if stderr != "Warning: "+diagnostic.Message+"\n" {
			t.Fatalf("stderr = %q, want one warning line (report %q)", stderr, corrupt)
		}
		after, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != corrupt {
			t.Fatalf("inspect rewrote the corrupt build report: %q", after)
		}
	}
}

func writeInspectStdoutProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import (
	"fmt"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		fmt.Println("INSPECT_ONCONFIG_STDOUT")
		ctx.Option("gate").SetType(api.OptionBool).SetDefault(true)
	})
	p.OnRequire(func(ctx *api.RequireContext) {
		fmt.Println("INSPECT_ONREQUIRE_STDOUT")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		fmt.Println("INSPECT_ONBUILD_STDOUT")
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	return project
}

func TestInspectInstallPlan(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnInstall(func(ctx *api.InstallContext) {
		ctx.AddInstalls("oninstall.conf", "etc/oninstall.conf")
		panic("OnInstall must not run during inspect")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.AddInstalls("config/app.conf", "etc/app.conf")
		ctx.AddInstalls("docs/readme.txt", "share/doc/readme.txt")
		ctx.Target("app").SetKind(api.TargetBinary).SetInstallDir("sbin")
		ctx.Target("lib").SetKind(api.TargetStatic)
		ctx.Target("void").SetKind(api.TargetVoid)
		ctx.Target("obj").SetKind(api.TargetObject)
		ctx.Target("off").SetKind(api.TargetBinary).SetInstall(false)
		ctx.Target("tests").SetKind(api.TargetBinary).SetTest(true)
		ctx.Target("fw").SetKind(api.TargetBinary).
			AddPostLinkOutputs("{output}.hex", "{output}.bin")
	})
}
`)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("successful inspect wrote stderr: %q", stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	data := envelope.Data
	if want := filepath.Join(project, "install"); data.InstallPrefix != want {
		t.Errorf("installPrefix = %q, want %q", data.InstallPrefix, want)
	}
	if want := filepath.Join(project, "install", "manifest.json"); data.InstallManifest != want {
		t.Errorf("installManifest = %q, want %q", data.InstallManifest, want)
	}
	proj := inspectPackageNamed(t, data, "proj")
	wantInstalls := []inspectInstall{
		{Source: "config/app.conf", Dest: "etc/app.conf"},
		{Source: "docs/readme.txt", Dest: "share/doc/readme.txt"},
	}
	for _, test := range []struct {
		target      string
		installPath string
	}{
		{"app", "sbin/app"},
		{"lib", "lib/liblib.a"},
		{"void", ""},
		{"obj", ""},
		{"off", ""},
		{"tests", ""},
		{"fw", "bin/fw"},
	} {
		target := inspectTargetNamed(t, proj, test.target)
		if target.InstallPath != test.installPath {
			t.Errorf("%s installPath = %q, want %q", test.target, target.InstallPath, test.installPath)
		}
		if !slices.Equal(target.Installs, wantInstalls) {
			t.Errorf("%s installs = %+v, want the package-level entries", test.target, target.Installs)
		}
	}
	fw := inspectTargetNamed(t, proj, "fw")
	if !slices.Equal(fw.PostLinkOutputs, []string{"{output}.hex", "{output}.bin"}) {
		t.Errorf("fw postLinkOutputs = %v", fw.PostLinkOutputs)
	}
	if strings.Contains(stdout, "oninstall.conf") {
		t.Fatalf("inspect executed OnInstall:\n%s", stdout)
	}
	for _, dir := range []string{"build", "install"} {
		if _, err := os.Stat(filepath.Join(project, dir)); !os.IsNotExist(err) {
			t.Fatalf("inspect created %s: %v", dir, err)
		}
	}

	again, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
	}
	if again != stdout {
		t.Fatal("consecutive inspect runs are not byte-identical")
	}
}

func TestInspectSuppressesBuildscriptStdout(t *testing.T) {
	project := writeInspectStdoutProject(t)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "INSPECT_") {
		t.Fatalf("buildscript stdout leaked into the JSON document:\n%s", stdout)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Data.Roots) != 1 || envelope.Data.Roots[0] != "proj" {
		t.Fatalf("roots = %v", envelope.Data.Roots)
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect", "--set", "proj/gate=maybe")
	if code != 1 {
		t.Fatalf("invalid --set exit = %d, want 1 (stderr %q)", code, stderr)
	}
	if strings.Contains(stdout, "INSPECT_") {
		t.Fatalf("buildscript stdout leaked into the error envelope:\n%s", stdout)
	}
	envelope = decodeInspectEnvelope(t, stdout)
	if envelope.Status != "error" || envelope.Data != nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_E_OPTION" {
		t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
	}
	if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: ") {
		t.Fatalf("stderr = %q, want one human error line", stderr)
	}

	t.Run("configure_failure", func(t *testing.T) {
		broken := filepath.Join(t.TempDir(), "broken")
		writeExtensionFile(t, filepath.Join(broken, "build.go"), `package main

import (
	"fmt"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		fmt.Println("INSPECT_BROKEN_STDOUT")
		panic("configure failure")
	})
}
`)
		stdout, stderr, code := runInspectCommand(t, broken, "inspect")
		if code != 1 {
			t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
		}
		if strings.Contains(stdout, "INSPECT_") {
			t.Fatalf("buildscript stdout leaked into the error envelope:\n%s", stdout)
		}
		envelope := decodeInspectEnvelope(t, stdout)
		if envelope.Status != "error" || envelope.Data != nil {
			t.Fatalf("envelope = %+v", envelope)
		}
		if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_E_INSPECT" {
			t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
		}
		if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: ") {
			t.Fatalf("stderr = %q, want one human error line", stderr)
		}
	})
}

func TestInspectSetOverridesAreHypothetical(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())
	configPath := filepath.Join(project, ".vmake", "config.json")

	stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/with_dep=false")
	if code != 0 || stderr != "" {
		t.Fatalf("override inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("override envelope = %+v", envelope)
	}
	data := envelope.Data
	if data.Overrides["proj/with_dep"] != false {
		t.Errorf("overrides = %v", data.Overrides)
	}
	proj := inspectPackageNamed(t, data, "proj")
	if proj.Options["with_dep"] != false {
		t.Errorf("effective with_dep = %v", proj.Options["with_dep"])
	}
	dep := inspectPackageNamed(t, data, "dep")
	if dep.Needed || dep.Targets != nil {
		t.Errorf("dep should not be needed: %+v", dep)
	}
	if len(data.Graph) != 0 {
		t.Errorf("graph = %v, want empty", data.Graph)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("%s was written by inspect --set: %v", configPath, err)
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect", "--set", "dep/dep_mode=slow")
	if code != 0 || stderr != "" {
		t.Fatalf("choice override exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	if data.Overrides["dep/dep_mode"] != "slow" {
		t.Errorf("overrides = %v", data.Overrides)
	}
	if got := inspectPackageNamed(t, data, "dep").Options["dep_mode"]; got != "slow" {
		t.Errorf("effective dep_mode = %v", got)
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect", "--set", "mode=release")
	if code != 0 || stderr != "" {
		t.Fatalf("global override exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	if data.Mode != "release" || data.Global["mode"] != "release" || data.Overrides["mode"] != "release" {
		t.Errorf("global override not applied: mode=%q global=%v overrides=%v", data.Mode, data.Global, data.Overrides)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("%s was written by inspect --set: %v", configPath, err)
	}
}

func TestInspectSetTypedCoercion(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())
	configPath := filepath.Join(project, ".vmake", "config.json")

	for _, raw := range []string{"42", "true", "on", "false", ""} {
		t.Run("string_"+raw, func(t *testing.T) {
			stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/label="+raw)
			if code != 0 || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			data := decodeInspectEnvelope(t, stdout).Data
			if data.Overrides["proj/label"] != raw {
				t.Errorf("overrides = %#v, want string %q", data.Overrides, raw)
			}
			if got := inspectPackageNamed(t, data, "proj").Options["label"]; got != raw {
				t.Errorf("effective label = %#v, want string %q", got, raw)
			}
		})
	}

	for _, test := range []struct {
		raw  string
		want bool
	}{
		{"true", true}, {"false", false},
		{"on", true}, {"off", false},
		{"1", true}, {"0", false},
	} {
		t.Run("bool_"+test.raw, func(t *testing.T) {
			stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/with_dep="+test.raw)
			if code != 0 || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			data := decodeInspectEnvelope(t, stdout).Data
			if data.Overrides["proj/with_dep"] != test.want {
				t.Errorf("overrides = %#v, want bool %v", data.Overrides, test.want)
			}
			if got := inspectPackageNamed(t, data, "proj").Options["with_dep"]; got != test.want {
				t.Errorf("effective with_dep = %#v, want bool %v", got, test.want)
			}
		})
	}

	stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/retries=42")
	if code != 0 || stderr != "" {
		t.Fatalf("int override exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	if data.Overrides["proj/retries"] != float64(42) {
		t.Errorf("overrides = %#v, want number 42", data.Overrides)
	}
	if got := inspectPackageNamed(t, data, "proj").Options["retries"]; got != float64(42) {
		t.Errorf("effective retries = %#v, want number 42", got)
	}

	first, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/label=42")
	if code != 0 || stderr != "" {
		t.Fatalf("first typed run exit = %d, stderr = %q", code, stderr)
	}
	again, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/label=42")
	if code != 0 || stderr != "" {
		t.Fatalf("second typed run exit = %d, stderr = %q", code, stderr)
	}
	if again != first {
		t.Fatal("consecutive inspect --set runs are not byte-identical")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("%s was written by inspect --set: %v", configPath, err)
	}
	for _, dir := range []string{"build", "install"} {
		if _, err := os.Stat(filepath.Join(project, dir)); !os.IsNotExist(err) {
			t.Fatalf("inspect --set created %s: %v", dir, err)
		}
	}
}

func TestCoerceInspectOverridesLeavesUndeclaredOptionsRaw(t *testing.T) {
	overrides, err := coerceInspectOverrides([]inspectOverride{
		{pkgName: "proj", optName: "lazy", raw: "on"},
		{pkgName: "proj", optName: "label", raw: "42"},
	}, &inspectOptionDeclarations{
		packageOptions: map[string]map[string]*api.Option{
			"proj": {"label": (&api.Option{}).SetType(api.OptionString)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if overrides[0].value != "on" {
		t.Errorf("undeclared option value = %#v, want raw string \"on\"", overrides[0].value)
	}
	if overrides[1].value != "42" {
		t.Errorf("declared string option value = %#v, want \"42\"", overrides[1].value)
	}

	overrides, err = coerceInspectOverrides([]inspectOverride{{pkgName: "proj", optName: "lazy", raw: "on"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if overrides[0].value != "on" {
		t.Errorf("nil declarations value = %#v, want raw string \"on\"", overrides[0].value)
	}
}

func TestInspectSetErrors(t *testing.T) {
	project := writeInspectProject(t, t.TempDir())
	configPath := filepath.Join(project, ".vmake", "config.json")
	for _, test := range []struct {
		name string
		args []string
	}{
		{"syntax", []string{"--set", "proj/with_dep"}},
		{"unknown_package", []string{"--set", "nope/x=1"}},
		{"unknown_option", []string{"--set", "proj/nope=1"}},
		{"unknown_global", []string{"--set", "nope=1"}},
		{"wrong_type", []string{"--set", "proj/with_dep=maybe"}},
		{"wrong_type_string", []string{"--set", "proj/retries=quick"}},
		{"invalid_choice", []string{"--set", "dep/dep_mode=warp"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, code := runInspectCommand(t, project, append([]string{"inspect"}, test.args...)...)
			if code != 1 {
				t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
			}
			envelope := decodeInspectEnvelope(t, stdout)
			if envelope.Status != "error" || envelope.Data != nil {
				t.Fatalf("envelope = %+v", envelope)
			}
			if len(envelope.Diagnostics) != 1 {
				t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
			}
			diagnostic := envelope.Diagnostics[0]
			if diagnostic.Severity != "error" || diagnostic.Code != "VMAKE_E_OPTION" || diagnostic.Message == "" {
				t.Fatalf("diagnostic = %+v", diagnostic)
			}
			if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: ") {
				t.Fatalf("stderr = %q, want one human error line", stderr)
			}
			if _, err := os.Stat(configPath); !os.IsNotExist(err) {
				t.Fatalf("%s was written by an invalid --set: %v", configPath, err)
			}
		})
	}
}

func TestInspectSetInvalidBoolValue(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("gate").
			SetType(api.OptionBool).
			SetDefault(true).
			SetOnApply(func(ctx *api.ConfigContext, val any) {
				if !ctx.Bool("gate") {
					return
				}
			})
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetVoid)
	})
}
`)
	stdout, stderr, code := runInspectCommand(t, project, "inspect", "--set", "proj/gate=maybe")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "error" || envelope.Data != nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_E_OPTION" || envelope.Diagnostics[0].Severity != "error" {
		t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
	}
	if !strings.Contains(envelope.Diagnostics[0].Message, "invalid bool value") {
		t.Fatalf("diagnostic = %+v, want an invalid bool value message", envelope.Diagnostics[0])
	}
	if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "Error: ") || strings.Contains(stderr, "panic:") {
		t.Fatalf("stderr = %q, want one human error line", stderr)
	}
}

func TestInspectScriptErrorEnvelope(t *testing.T) {
	for _, test := range []struct {
		name   string
		script string
	}{
		{"load", "package main\n\nfunc Main(p *api.Package) {\n"},
		{"configure", `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) { panic("configure failure") })
}
`},
	} {
		t.Run(test.name, func(t *testing.T) {
			project := filepath.Join(t.TempDir(), "broken")
			writeExtensionFile(t, filepath.Join(project, "build.go"), test.script)
			stdout, stderr, code := runInspectCommand(t, project, "inspect")
			if code != 1 {
				t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
			}
			envelope := decodeInspectEnvelope(t, stdout)
			if envelope.Status != "error" || envelope.Data != nil {
				t.Fatalf("envelope = %+v", envelope)
			}
			if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_E_INSPECT" || envelope.Diagnostics[0].Severity != "error" {
				t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
			}
			lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
			if len(lines) == 0 || strings.Count(stderr, "Error: ") != 1 || !strings.HasPrefix(lines[len(lines)-1], "Error: ") {
				t.Fatalf("stderr = %q, want one human error line", stderr)
			}
		})
	}
}

func TestInspectTargetDeclarationWarning(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("broken") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetVoid).AddDeps("broken:*")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "broken", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) { panic("declaration failure") })
}
`)
	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v, want the declaration warning and the aggregate", envelope.Diagnostics)
	}
	diagnostic := envelope.Diagnostics[0]
	if diagnostic.Severity != "warning" || diagnostic.Code != "VMAKE_W_TARGET_DECLARE" ||
		!strings.Contains(diagnostic.Message, "broken") || diagnostic.Fix == "" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	aggregate := envelope.Diagnostics[1]
	if aggregate.Code != "VMAKE_W_OMITTED_TARGETS" ||
		aggregate.Message != "omitted targets for 1 package(s) because target declaration failed" {
		t.Fatalf("aggregate diagnostic = %+v", aggregate)
	}
	broken := inspectPackageNamed(t, envelope.Data, "broken")
	if !broken.Needed || broken.Targets != nil {
		t.Fatalf("broken package = %+v", broken)
	}
	if !strings.Contains(stderr, "Warning: cannot declare targets for broken") ||
		!strings.HasSuffix(stderr, "Warning: "+aggregate.Message+"\n") {
		t.Fatalf("stderr = %q, want the per-package warning and the aggregate", stderr)
	}
}

func TestInspectReadOnlyDryRun(t *testing.T) {
	project := filepath.Join(t.TempDir(), "ro")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import (
	"os"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		if err := os.WriteFile("generated.txt", []byte("generated"), 0644); err != nil {
			panic(err)
		}
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	generated := filepath.Join(project, "generated.txt")
	wantReadOnly := "read-only inspection: interpreted script write to " + generated + " is not allowed"

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v, want one target-declaration warning and one read-only aggregate", envelope.Diagnostics)
	}
	diagnostic := envelope.Diagnostics[0]
	if diagnostic.Severity != "warning" || diagnostic.Code != "VMAKE_W_TARGET_DECLARE" || !strings.Contains(diagnostic.Message, wantReadOnly) {
		t.Fatalf("diagnostic = %+v, want read-only declaration warning", diagnostic)
	}
	aggregate := envelope.Diagnostics[1]
	if aggregate.Severity != "warning" || aggregate.Code != "VMAKE_W_OMITTED_TARGETS" ||
		aggregate.Message != "omitted targets for 1 package(s) (1 due to read-only writes)" ||
		aggregate.Fix != "Guard generated-file work with p.DryRun(), or run vmake inspect --allow-dry-run-writes when those writes are expected." {
		t.Fatalf("aggregate diagnostic = %+v", aggregate)
	}
	if stderr != "Warning: "+diagnostic.Message+"\nWarning: "+aggregate.Message+"\n" {
		t.Fatalf("stderr = %q, want declaration and aggregate warning lines", stderr)
	}
	if proj := inspectPackageNamed(t, envelope.Data, "ro"); proj.Targets != nil {
		t.Fatalf("ro targets = %+v, want omitted after read-only declaration failure", proj.Targets)
	}
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Fatalf("inspect created generated.txt: %v", err)
	}

	if buildStdout, buildStderr, buildCode := runBuildCommand(t, project, "build"); buildCode != 0 {
		t.Fatalf("build exit = %d, stderr = %q\n%s", buildCode, buildStderr, buildStdout)
	}
	if data, err := os.ReadFile(generated); err != nil || string(data) != "generated" {
		t.Fatalf("normal build did not write generated.txt: %q, %v", data, err)
	}

	stdout, stderr, code = runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect after build exit = %d, stderr = %q", code, stderr)
	}
	envelope = decodeInspectEnvelope(t, stdout)
	if len(envelope.Diagnostics) != 2 || !strings.Contains(envelope.Diagnostics[0].Message, wantReadOnly) ||
		envelope.Diagnostics[1].Code != "VMAKE_W_OMITTED_TARGETS" {
		t.Fatalf("diagnostics after build = %+v", envelope.Diagnostics)
	}
	if data, err := os.ReadFile(generated); err != nil || string(data) != "generated" {
		t.Fatalf("inspect after build changed generated.txt: %q, %v", data, err)
	}
}

func TestInspectReadOnlyBlocksIgnoredWrite(t *testing.T) {
	project := filepath.Join(t.TempDir(), "ignored")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import (
	"os"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		_ = os.WriteFile("ignored.txt", []byte("ignored"), 0644)
		_ = os.RemoveAll("kept")
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")
	kept := filepath.Join(project, "kept", "keep.txt")
	writeExtensionFile(t, kept, "keep")
	wantReadOnly := "read-only inspection: interpreted script write to " + filepath.Join(project, "ignored.txt") + " is not allowed"

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v, want one blocked-write warning and one aggregate", envelope.Diagnostics)
	}
	diagnostic := envelope.Diagnostics[0]
	if diagnostic.Code != "VMAKE_W_TARGET_DECLARE" || !strings.Contains(diagnostic.Message, wantReadOnly) {
		t.Fatalf("diagnostic = %+v, want read-only declaration warning", diagnostic)
	}
	if aggregate := envelope.Diagnostics[1]; aggregate.Code != "VMAKE_W_OMITTED_TARGETS" ||
		aggregate.Message != "omitted targets for 1 package(s) (1 due to read-only writes)" {
		t.Fatalf("aggregate diagnostic = %+v", aggregate)
	}
	if proj := inspectPackageNamed(t, envelope.Data, "ignored"); proj.Targets != nil {
		t.Fatalf("ignored targets = %+v, want omitted after blocked write", proj.Targets)
	}
	if _, err := os.Stat(filepath.Join(project, "ignored.txt")); !os.IsNotExist(err) {
		t.Fatalf("inspect created ignored.txt: %v", err)
	}
	if data, err := os.ReadFile(kept); err != nil || string(data) != "keep" {
		t.Fatalf("inspect removed kept/keep.txt: %q, %v", data, err)
	}
}

func TestInspectReadOnlyAggregateOmission(t *testing.T) {
	project := filepath.Join(t.TempDir(), "aggregate")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import (
	"os"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib") })
	p.OnBuild(func(ctx *api.BuildContext) {
		_ = os.WriteFile("root-generated.txt", []byte("root"), 0644)
		ctx.Target("app").SetKind(api.TargetBinary).AddDeps("lib:lib")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "lib", "build.go"), `package main

import (
	"os"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		_ = os.WriteFile("lib-generated.txt", []byte("lib"), 0644)
		ctx.Target("lib").SetKind(api.TargetStatic)
	})
}
`)
	rootGenerated := filepath.Join(project, "root-generated.txt")
	libGenerated := filepath.Join(project, "lib", "lib-generated.txt")
	wantAggregate := "omitted targets for 2 package(s) (2 due to read-only writes)"

	var first *inspectEnvelope
	for attempt := 0; attempt < 2; attempt++ {
		stdout, stderr, code := runInspectCommand(t, project, "inspect")
		if code != 0 {
			t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
		}
		envelope := decodeInspectEnvelope(t, stdout)
		if envelope.Status != "ok" || envelope.Data == nil {
			t.Fatalf("envelope = %+v", envelope)
		}
		if len(envelope.Diagnostics) != 3 {
			t.Fatalf("diagnostics = %+v, want two per-package warnings and one aggregate", envelope.Diagnostics)
		}
		for i, name := range []string{"aggregate", "lib"} {
			if got := envelope.Diagnostics[i]; got.Code != "VMAKE_W_TARGET_DECLARE" || !strings.Contains(got.Message, "read-only inspection:") ||
				!strings.Contains(got.Message, "cannot declare targets for "+name+":") {
				t.Fatalf("per-package diagnostic[%d] = %+v", i, got)
			}
		}
		aggregate := envelope.Diagnostics[2]
		if aggregate.Severity != "warning" || aggregate.Code != "VMAKE_W_OMITTED_TARGETS" ||
			aggregate.Message != wantAggregate ||
			aggregate.Fix != "Guard generated-file work with p.DryRun(), or run vmake inspect --allow-dry-run-writes when those writes are expected." {
			t.Fatalf("aggregate diagnostic = %+v", aggregate)
		}
		if !strings.HasSuffix(stderr, "Warning: "+wantAggregate+"\n") {
			t.Fatalf("stderr = %q, want aggregate warning last", stderr)
		}
		if got := strings.Count(stderr, "Warning: cannot declare targets for "); got != 2 {
			t.Fatalf("stderr has %d per-package warnings, want 2:\n%s", got, stderr)
		}
		if attempt == 0 {
			first = envelope
			continue
		}
		if !reflect.DeepEqual(first.Diagnostics, envelope.Diagnostics) {
			t.Fatalf("diagnostics are not deterministic:\nfirst:  %+v\nsecond: %+v", first.Diagnostics, envelope.Diagnostics)
		}
	}
	for _, path := range []string{rootGenerated, libGenerated} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("inspect wrote %s: %v", path, err)
		}
	}
}

func TestInspectOmissionAggregateCountsAllDeclarationFailures(t *testing.T) {
	project := writeQueryMixedOmissionProject(t)
	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 3 {
		t.Fatalf("diagnostics = %+v, want two per-package warnings and one aggregate", envelope.Diagnostics)
	}
	var readOnly, broken, aggregate *inspectDiagnostic
	for i := range envelope.Diagnostics {
		diagnostic := &envelope.Diagnostics[i]
		switch diagnostic.Code {
		case "VMAKE_W_TARGET_DECLARE":
			if diagnostic.Severity != "warning" || diagnostic.Fix == "" {
				t.Fatalf("per-package diagnostic = %+v", diagnostic)
			}
			if strings.Contains(diagnostic.Message, "read-only inspection:") {
				readOnly = diagnostic
			} else {
				broken = diagnostic
			}
		case "VMAKE_W_OMITTED_TARGETS":
			aggregate = diagnostic
		default:
			t.Fatalf("unexpected diagnostic %+v", diagnostic)
		}
	}
	if readOnly == nil || broken == nil || aggregate == nil {
		t.Fatalf("diagnostics = %+v, want both omission causes and the aggregate", envelope.Diagnostics)
	}
	if !strings.Contains(broken.Message, "broken") || !strings.Contains(broken.Message, "declaration failure") {
		t.Fatalf("non-read-only diagnostic = %+v", broken)
	}
	if aggregate.Severity != "warning" ||
		aggregate.Message != "omitted targets for 2 package(s) (1 due to read-only writes)" ||
		aggregate.Fix != "Guard generated-file work with p.DryRun(), or run vmake inspect --allow-dry-run-writes when those writes are expected." {
		t.Fatalf("aggregate diagnostic = %+v", aggregate)
	}
	if !strings.HasSuffix(stderr, "Warning: "+aggregate.Message+"\n") {
		t.Fatalf("stderr = %q, want the aggregate warning last", stderr)
	}
	for _, name := range []string{"lib", "broken"} {
		if pkg := inspectPackageNamed(t, envelope.Data, name); pkg.Targets != nil {
			t.Fatalf("%s targets = %+v, want omitted after declaration failure", name, pkg.Targets)
		}
	}
	if _, err := os.Stat(filepath.Join(project, "lib", "generated.txt")); !os.IsNotExist(err) {
		t.Fatalf("inspect wrote lib/generated.txt: %v", err)
	}
}

func TestInspectReadOnlyAllowsReads(t *testing.T) {
	project := filepath.Join(t.TempDir(), "reads")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import (
	"os"
	"path/filepath"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		if _, err := os.ReadFile("src/main.c"); err != nil {
			panic(err)
		}
		if _, err := os.Stat("src"); err != nil {
			panic(err)
		}
		if err := filepath.Walk("src", func(path string, info os.FileInfo, err error) error { return err }); err != nil {
			panic(err)
		}
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/*.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "src", "main.c"), "int main(void) { return 0; }\n")

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || len(envelope.Diagnostics) != 0 || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	app := inspectTargetNamed(t, inspectPackageNamed(t, envelope.Data, "reads"), "app")
	if !slices.Equal(app.ResolvedFiles, []string{"src/main.c"}) || app.ResolvedFileCount != 1 {
		t.Fatalf("app resolved files = %v (%d), want src/main.c", app.ResolvedFiles, app.ResolvedFileCount)
	}
}

func TestInspectSourceMissingWarning(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary)
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "lib", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://example.invalid/lib.git")
		p.AddVersion("1.0.0", "v1.0.0")
	})
}
`)
	libDir := filepath.Join(project, "lib")
	checkWarning := func(t *testing.T) {
		t.Helper()
		stdout, stderr, code := runInspectCommand(t, project, "inspect")
		if code != 0 {
			t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
		}
		envelope := decodeInspectEnvelope(t, stdout)
		if envelope.Status != "ok" || envelope.Data == nil {
			t.Fatalf("envelope = %+v", envelope)
		}
		if len(envelope.Diagnostics) != 1 {
			t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
		}
		diagnostic := envelope.Diagnostics[0]
		if diagnostic.Severity != "warning" || diagnostic.Code != "VMAKE_W_SOURCE_MISSING" || diagnostic.Package != "lib" {
			t.Fatalf("diagnostic = %+v", diagnostic)
		}
		for _, want := range []string{"package lib", "https://example.invalid/lib.git", "version 1.0.0"} {
			if !strings.Contains(diagnostic.Message, want) {
				t.Errorf("diagnostic message %q missing %q", diagnostic.Message, want)
			}
		}
		if stderr != "Warning: "+diagnostic.Message+"\n" {
			t.Fatalf("stderr = %q, want one warning line", stderr)
		}
		lib := inspectPackageNamed(t, envelope.Data, "lib")
		if !lib.Needed || lib.SourcePresent {
			t.Fatalf("lib package = %+v", lib)
		}
		if proj := inspectPackageNamed(t, envelope.Data, "proj"); !proj.SourcePresent {
			t.Fatalf("plain local package sourcePresent = false")
		}
	}

	checkWarning(t)
	if err := os.MkdirAll(filepath.Join(libDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	checkWarning(t)

	writeExtensionFile(t, filepath.Join(libDir, "src", "lib.c"), "int lib(void) { return 1; }\n")
	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("materialized inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if len(envelope.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", envelope.Diagnostics)
	}
	if lib := inspectPackageNamed(t, envelope.Data, "lib"); !lib.SourcePresent {
		t.Fatalf("populated lib source reported as absent")
	}
	if _, err := os.Stat(filepath.Join(project, ".vmake_deps")); !os.IsNotExist(err) {
		t.Fatalf("inspect materialized sources: %v", err)
	}
}

func TestInspectSourceMissingWarningRemoteRegistry(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	state := t.TempDir()
	repoDir := filepath.Join(state, "repos", "official")
	writeExtensionFile(t, filepath.Join(repoDir, "packages", "d", "demo", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://example.invalid/demo.git")
		p.AddVersion("1.0.0", "v1.0.0")
	})
}
`)
	runGitLine(t, repoDir, "init", "-q")
	runGitLine(t, repoDir, "config", "user.email", "test@example.com")
	runGitLine(t, repoDir, "config", "user.name", "test")
	runGitLine(t, repoDir, "add", "-A")
	runGitLine(t, repoDir, "commit", "-q", "-m", "registry")

	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("official/demo") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddDeps("official/demo:*")
	})
}
`)
	stdout, stderr, code := runInspectCommandWithState(t, project, state, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", envelope.Diagnostics)
	}
	diagnostic := envelope.Diagnostics[0]
	if diagnostic.Severity != "warning" || diagnostic.Code != "VMAKE_W_SOURCE_MISSING" || diagnostic.Package != "official/demo" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	for _, want := range []string{"package official/demo", "https://example.invalid/demo.git", "version 1.0.0"} {
		if !strings.Contains(diagnostic.Message, want) {
			t.Errorf("diagnostic message %q missing %q", diagnostic.Message, want)
		}
	}
	if stderr != "Warning: "+diagnostic.Message+"\n" {
		t.Fatalf("stderr = %q, want one warning line", stderr)
	}
	demo := inspectPackageNamed(t, envelope.Data, "official/demo")
	if !demo.Needed || demo.SourcePresent {
		t.Fatalf("remote package = %+v", demo)
	}
	if _, err := os.Stat(filepath.Join(project, ".vmake_deps")); !os.IsNotExist(err) {
		t.Fatalf("inspect materialized the remote source tree: %v", err)
	}
}

func TestInspectExpectedSourceDirectories(t *testing.T) {
	deps := t.TempDir()
	res := resolver.NewResolver(nil, deps)
	ctx := &RuntimeContext{Paths: &pipeline.Paths{DepsDir: deps}, Resolver: res}
	remoteNode := func(name, dir string) *resolver.PackageNode {
		return resolver.NewPackageNode(name, buildscript.NewSource(name, filepath.Join(dir, "build.go"), dir, api.SourceRemote), api.NewPackage())
	}

	registry := remoteNode("official/demo", filepath.Join(deps, "wrapper"))
	registryDir := filepath.Join(deps, "official", "demo", "src")
	if got := inspectExpectedSourceDir(ctx, "official/demo", registry); got != registryDir {
		t.Fatalf("registry source dir = %q, want %q", got, registryDir)
	}
	if inspectSourcePresent(ctx, "official/demo", registry) {
		t.Fatal("absent registry tree reported present")
	}
	if err := os.MkdirAll(registryDir, 0755); err != nil {
		t.Fatal(err)
	}
	if inspectSourcePresent(ctx, "official/demo", registry) {
		t.Fatal("empty registry tree reported present")
	}
	writeExtensionFile(t, filepath.Join(registryDir, "payload.c"), "int demo(void) { return 0; }\n")
	if !inspectSourcePresent(ctx, "official/demo", registry) {
		t.Fatal("populated registry tree reported absent")
	}

	res.SubParents()["native/root/member"] = "native/root"
	member := remoteNode("native/root/member", filepath.Join(deps, "native", "root", "src", "member"))
	memberDir := filepath.Join(deps, "native", "root", "src", "member")
	if got := inspectExpectedSourceDir(ctx, "native/root/member", member); got != memberDir {
		t.Fatalf("shared native member dir = %q, want %q", got, memberDir)
	}
	writeExtensionFile(t, filepath.Join(memberDir, "build.go"), "package main\n")
	if !inspectSourcePresent(ctx, "native/root/member", member) {
		t.Fatal("shared native member tree reported absent")
	}
	member.Pkg.SetGit("https://example.invalid/member.git")
	if got, want := inspectExpectedSourceDir(ctx, "native/root/member", member), filepath.Join(memberDir, "src"); got != want {
		t.Fatalf("own-git native member dir = %q, want %q", got, want)
	}
	if inspectSourcePresent(ctx, "native/root/member", member) {
		t.Fatal("own-git native member tree reported present before materialization")
	}

	localDir := filepath.Join(t.TempDir(), "lib")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatal(err)
	}
	local := resolver.NewPackageNode("lib", buildscript.NewSource("lib", filepath.Join(localDir, "build.go"), localDir, api.SourceLocal), api.NewPackage())
	local.Pkg.SetGit("https://example.invalid/lib.git")
	if got, want := inspectExpectedSourceDir(ctx, "lib", local), filepath.Join(localDir, "src"); got != want {
		t.Fatalf("local SetGit source dir = %q, want %q", got, want)
	}
	if inspectSourcePresent(ctx, "lib", local) {
		t.Fatal("absent local SetGit tree reported present")
	}

	plainDir := filepath.Join(t.TempDir(), "plain")
	writeExtensionFile(t, filepath.Join(plainDir, "build.go"), "package main\n")
	plain := resolver.NewPackageNode("plain", buildscript.NewSource("plain", filepath.Join(plainDir, "build.go"), plainDir, api.SourceLocal), api.NewPackage())
	if got := inspectExpectedSourceDir(ctx, "plain", plain); got != plainDir {
		t.Fatalf("plain local source dir = %q, want %q", got, plainDir)
	}
	if !inspectSourcePresent(ctx, "plain", plain) {
		t.Fatal("plain local package dir reported absent")
	}
}

func checkInspectWarnings(t *testing.T, envelope *inspectEnvelope, stderr string) {
	t.Helper()
	if len(envelope.Diagnostics) == 0 {
		if stderr != "" {
			t.Fatalf("inspect wrote stderr without diagnostics: %q", stderr)
		}
		return
	}
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	if len(lines) != len(envelope.Diagnostics) {
		t.Fatalf("stderr = %q, want one warning line per diagnostic", stderr)
	}
	for i, diagnostic := range envelope.Diagnostics {
		if diagnostic.Severity != "warning" {
			t.Fatalf("diagnostic = %+v, want warning", diagnostic)
		}
		if want := "Warning: " + diagnostic.Message; lines[i] != want {
			t.Fatalf("stderr line %d = %q, want %q", i, lines[i], want)
		}
	}
}

func TestInspectFixtures(t *testing.T) {
	fixtures := []string{
		filepath.Join("..", "..", "test_data", "01_simple_c"),
		filepath.Join("..", "..", "test_data", "03_multi_target"),
		filepath.Join("..", "..", "test_data", "04_multi_module"),
	}
	if runtime.GOOS == "linux" {
		fixtures = append(fixtures, filepath.Join("..", "..", "test_linux", "17_firmware"))
	}
	for _, fixture := range fixtures {
		root, err := filepath.Abs(fixture)
		if err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			t.Skipf("fixture %s unavailable", root)
		}
		t.Run(filepath.Base(root), func(t *testing.T) {
			before, err := os.ReadFile(filepath.Join(root, ".vmake", "config.json"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			stdout, stderr, code := runInspectCommand(t, root, "inspect")
			if code != 0 {
				t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
			}
			envelope := decodeInspectEnvelope(t, stdout)
			if envelope.SchemaVersion != 1 || envelope.Command != "inspect" || envelope.Status != "ok" || envelope.Data == nil {
				t.Fatalf("envelope = %+v", envelope)
			}
			checkInspectWarnings(t, envelope, stderr)
			data := envelope.Data
			if data.Root != root || data.Mode == "" || data.Toolchain == "" || data.CompileDatabase == "" || data.BuildReport == "" {
				t.Fatalf("data header = %+v", data)
			}
			if len(data.Packages) == 0 || len(data.Roots) == 0 {
				t.Fatalf("empty model: roots=%v packages=%d", data.Roots, len(data.Packages))
			}
			for _, pkg := range data.Packages {
				if pkg.Name == "" || pkg.Kind == "" || pkg.SourceDir == "" || pkg.Deps == nil {
					t.Fatalf("incomplete package entry: %+v", pkg)
				}
				for _, target := range pkg.Targets {
					if target.Name == "" || target.Kind == "" {
						t.Fatalf("incomplete target entry: %+v", target)
					}
				}
			}
			if data.ToolchainInfo == nil || data.ToolchainInfo.Name == "" || data.ToolchainInfo.CC == "" {
				t.Fatalf("toolchainInfo = %+v", data.ToolchainInfo)
			}
			if data.Global == nil || data.Graph == nil {
				t.Fatalf("global = %v, graph = %v", data.Global, data.Graph)
			}
			again, stderr, code := runInspectCommand(t, root, "inspect")
			if code != 0 {
				t.Fatalf("second inspect exit = %d, stderr = %q", code, stderr)
			}
			if again != stdout {
				t.Fatal("consecutive inspect runs are not byte-identical")
			}
			checkInspectWarnings(t, decodeInspectEnvelope(t, again), stderr)
			after, err := os.ReadFile(filepath.Join(root, ".vmake", "config.json"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("inspect modified .vmake/config.json")
			}
		})
	}
}

func TestInspectCommandRegistration(t *testing.T) {
	cmd, _, err := RootCmd.Find([]string{"inspect"})
	if err != nil {
		t.Fatalf("inspect not registered: %v", err)
	}
	if cmd.Name() != "inspect" {
		t.Fatalf("command = %q", cmd.Name())
	}
	if cmd.Flags().Lookup("set") == nil {
		t.Fatal("inspect has no --set flag")
	}
	ref := generateCLIRef(RootCmd)
	if !strings.Contains(ref, "vmake inspect") || !strings.Contains(ref, "--set <value>") {
		t.Fatalf("CLI reference missing inspect:\n%s", ref)
	}
}

func TestInspectSkipsTargetDeclarationForMissingSource(t *testing.T) {
	project := filepath.Join(t.TempDir(), "proj")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("lib") })
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddDeps("lib:lib")
	})
}
`)
	writeExtensionFile(t, filepath.Join(project, "lib", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://example.invalid/lib.git")
		p.AddVersion("1.0.0", "v1.0.0")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		panic("declaration failure")
	})
}
`)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	envelope := decodeInspectEnvelope(t, stdout)
	if envelope.Status != "ok" || envelope.Data == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "VMAKE_W_SOURCE_MISSING" {
		t.Fatalf("diagnostics = %+v, want only the source-missing warning", envelope.Diagnostics)
	}
	lib := inspectPackageNamed(t, envelope.Data, "lib")
	if !lib.Needed || lib.SourcePresent || lib.Targets != nil {
		t.Fatalf("lib package = %+v, want needed missing source without targets", lib)
	}
	if strings.Contains(stderr, "panic") || strings.Contains(stderr, "VMAKE_W_TARGET_DECLARE") {
		t.Fatalf("stderr = %q, want only the source warning", stderr)
	}
	if stderr != "Warning: "+envelope.Diagnostics[0].Message+"\n" {
		t.Fatalf("stderr = %q, want one warning line", stderr)
	}
	proj := inspectPackageNamed(t, envelope.Data, "proj")
	if targets := proj.Targets; len(targets) != 1 || targets[0].Name != "app" {
		t.Fatalf("proj targets = %+v, want app declared for its present source", targets)
	}
}

func TestInspectReportsBuildAndTestStateSeparately(t *testing.T) {
	project := writeTestCommandProject(t, passingTestSource)
	if stdout, stderr, code := runBuildCommand(t, project, "build"); code != 0 {
		t.Fatalf("build exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	if stdout, stderr, code := runBuildCommand(t, project, "test"); code != 0 {
		t.Fatalf("test exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	buildStateBefore := loadBuildReport(t, project)
	testStateBefore := loadTestReport(t, project)

	stdout, stderr, code := runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit = %d, stderr = %q", code, stderr)
	}
	data := decodeInspectEnvelope(t, stdout).Data
	if data == nil {
		t.Fatal("inspect returned no data")
	}
	if !data.BuildReportPresent || data.BuildReportState == nil {
		t.Fatalf("build report state missing: present=%v state=%+v", data.BuildReportPresent, data.BuildReportState)
	}
	if data.BuildReportState.Command != reportCommandBuild || data.BuildReportState.BuildID != buildStateBefore.BuildID ||
		data.BuildReportState.Status != "succeeded" || !data.BuildReportState.ConfigMatches ||
		!data.BuildReportState.EffectiveConfigMatches {
		t.Fatalf("buildReportState = %+v, report = %+v", data.BuildReportState, buildStateBefore)
	}
	if data.TestReport != testReportPath(project) || !data.TestReportPresent || data.TestReportState == nil {
		t.Fatalf("test report state missing: path=%q present=%v state=%+v", data.TestReport, data.TestReportPresent, data.TestReportState)
	}
	if data.TestReportState.Command != reportCommandTest || data.TestReportState.BuildID != testStateBefore.BuildID ||
		data.TestReportState.Status != "succeeded" || !data.TestReportState.ConfigMatches ||
		!data.TestReportState.EffectiveConfigMatches {
		t.Fatalf("testReportState = %+v, report = %+v", data.TestReportState, testStateBefore)
	}
	if string(data.TestReportState.Summary) == "" {
		t.Fatal("testReportState lost the summary")
	}

	if stdout, stderr, code := runInspectCommand(t, project, "clean"); code != 0 {
		t.Fatalf("clean exit = %d, stderr = %q\n%s", code, stderr, stdout)
	}
	stdout, stderr, code = runInspectCommand(t, project, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect after clean exit = %d, stderr = %q", code, stderr)
	}
	data = decodeInspectEnvelope(t, stdout).Data
	if data == nil {
		t.Fatal("inspect after clean returned no data")
	}
	if data.BuildReportPresent || data.BuildReportState != nil || data.TestReportPresent || data.TestReportState != nil {
		t.Fatalf("reports survived clean: build=%v/%+v test=%v/%+v",
			data.BuildReportPresent, data.BuildReportState, data.TestReportPresent, data.TestReportState)
	}
}
