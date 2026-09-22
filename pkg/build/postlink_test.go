package build

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestPostLinkOutputPaths(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(t.TempDir(), "absolute.bin")
	target := api.NewTargetRegistry().Target("app").
		AddPostLink("objcopy", "--add-gnu-debuglink={output}.input", "{output}").
		AddPostLinkOutputs("{output}", "{output}.debug", "extra/relative.bin", abs, "{output}.debug", "./{output}.prefixed")
	got := postLinkOutputPaths(target, dir, filepath.Join("build space", "app"))
	want := []string{filepath.Join(dir, "build space", "app.debug"), filepath.Join(dir, "extra", "relative.bin"), abs, filepath.Join(dir, "build space", "app.prefixed")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("output paths=%v, want %v", got, want)
	}
}

func TestPostLinkDebuglinkIncrementalAndInstall(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GNU ELF debuglink integration requires a Linux host")
	}
	resolved := make(map[string]string)
	for _, name := range []string{"gcc", "g++", "ar", "objcopy", "strip", "size"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s unavailable", name)
		}
		resolved[name] = path
	}
	dir := filepath.Join(t.TempDir(), "project space")
	writeAssemblyFixture(t, dir, "main.c", "int main(void) { return 0; }\n")
	buildDir := filepath.Join(dir, "build")
	output := filepath.Join(buildDir, "app")
	input := output + ".input"
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(resolved["gcc"], filepath.Join(dir, "main.c"), "-o", input)
	if text, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("input fixture: %s: %v", text, err)
	}
	target := api.NewTargetRegistry().Target("app").SetKind(api.TargetBinary).SetDefault(true).AddFiles("main.c").
		AddPostLink("objcopy", "--only-keep-debug", "{output}", "{output}.debug").
		AddPostLinkOutputs("{output}.debug").
		AddPostLink("objcopy", "--add-gnu-debuglink={output}.debug", "{output}").
		AddPostLink("size", "{output}.input").
		AddPostLinkHex().AddPostLinkBin().AddPostLinkStrip()
	targets := map[string]map[string]*api.Target{"p": {"app": target}}
	graph, err := NewBuildGraph(targets, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]*api.PkgDirs{"p": {SourceDir: dir, BuildDir: buildDir}}
	tc := &toolchain.Toolchain{Name: "postlink-test", Tools: toolchain.Tools{
		CC: resolved["gcc"], CXX: resolved["g++"], AR: resolved["ar"], OBJCOPY: resolved["objcopy"], STRIP: resolved["strip"], SIZE: resolved["size"],
	}}
	scheduler, err := NewScheduler(graph, tc, dirs, api.ModeDebug, nil, api.Platform{OS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	links := 0
	scheduler.linker.run = func(name, dir string, args ...string) ([]byte, error) {
		links++
		return gnuRunner(nil)(name, dir, args...)
	}
	for count := 0; count < 2; count++ {
		if err := scheduler.Build("p:app"); err != nil {
			t.Fatal(err)
		}
	}
	if links != 1 {
		t.Fatalf("unchanged debuglink build linked %d times, want 1", links)
	}
	file, err := elf.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	if file.Section(".gnu_debuglink") == nil {
		t.Error("debuglink section missing")
	}
	file.Close()
	for index, suffix := range []string{".debug", ".hex", ".bin", ".stripped"} {
		if err := os.Remove(output + suffix); err != nil {
			t.Fatal(err)
		}
		if err := scheduler.Build("p:app"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(output + suffix); err != nil {
			t.Fatalf("missing output not restored: %v", err)
		}
		if err := scheduler.Build("p:app"); err != nil {
			t.Fatal(err)
		}
		if links != index+2 {
			t.Fatalf("after deleting %s: got %d links, want %d", suffix, links, index+2)
		}
	}
	abs := filepath.Join(t.TempDir(), "absolute.bin")
	writeAssemblyFixture(t, dir, "extra/relative.bin", "relative")
	if err := os.WriteFile(abs, []byte("absolute"), 0644); err != nil {
		t.Fatal(err)
	}
	target.AddPostLinkOutputs("extra/relative.bin", abs)
	prefix := filepath.Join(dir, "install")
	installer := NewArtifactInstaller(graph, dirs, prefix)
	installer.SetPackageInfo("p", &PkgInstallInfo{BuildDir: buildDir, TargetOS: "linux"})
	if err := installer.InstallAll(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app", "app.debug", "app.hex", "app.bin", "app.stripped", "relative.bin", "absolute.bin"} {
		if _, err := os.Stat(filepath.Join(prefix, "bin", name)); err != nil {
			t.Fatalf("declared output %s not installed: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(prefix, "bin", "app.input")); !os.IsNotExist(err) {
		t.Fatalf("undeclared post-link input was installed: %v", err)
	}
}
