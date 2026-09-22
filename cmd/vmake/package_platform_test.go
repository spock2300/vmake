package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writePackagePlatformProject(t *testing.T, dir string, global, local map[string]any, testTarget bool) (string, string) {
	t.Helper()
	project := filepath.Join(dir, "project")
	state := filepath.Join(dir, "state")
	writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "tools", "toolchain.json"), "fixture")
	data, err := json.Marshal(map[string]any{
		"version": "1",
		"global":  map[string]any{"toolchain": "fixture", "options": global},
		"entries": map[string]any{"project": map[string]any{"options": local}},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), string(data))
	testSetting := ""
	if testTarget {
		testSetting = ".SetTest(true)"
	}
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import (
    "fmt"
    "path/filepath"
    "github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none")
        ctx.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString).SetDefault("arm-none-eabi")
    })
    p.OnBuild(func(ctx *api.BuildContext) {
        fmt.Printf("PLATFORM=%s/%s;OPTIONS=%s/%s\n", p.TargetOS(), p.TargetTriple(), ctx.String("target_os"), ctx.String("target_triple"))
        ctx.Target("app_" + p.TargetOS()).SetKind(api.TargetBinary).SetPrebuilt(filepath.Join(p.SourceDir(), "program"))`+testSetting+`
    })
    p.OnInstall(func(ctx *api.InstallContext) {
        fmt.Printf("PLATFORM=%s/%s;OPTIONS=%s/%s\n", p.TargetOS(), p.TargetTriple(), ctx.String("target_os"), ctx.String("target_triple"))
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "program"), "MZ"+strings.Repeat("\x00", 128))
	return project, state
}

func TestPackagePlatformQueryBuildInstallAndSymbols(t *testing.T) {
	for _, localDefault := range []bool{false, true} {
		name := "configured override"
		local := map[string]any{"target_os": "windows", "target_triple": "x86_64-w64-mingw32"}
		if localDefault {
			name = "package default"
			local = nil
		}
		t.Run(name, func(t *testing.T) {
			project, state := writePackagePlatformProject(t, t.TempDir(),
				map[string]any{"target_os": "none", "target_triple": "arm-none-eabi"}, local, false)
			if localDefault {
				path := filepath.Join(project, "build.go")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				source := strings.NewReplacer("ctx.GlobalOption", "ctx.Option", `SetDefault("none")`, `SetDefault("windows")`, `SetDefault("arm-none-eabi")`, `SetDefault("x86_64-w64-mingw32")`).Replace(string(data))
				writeExtensionFile(t, path, source)
			}
			for _, args := range [][]string{{"query", "targets"}, {"build", "--install"}, {"check-symbols", "--strict"}} {
				out, err := extensionCommand(t, state, project, args...)
				want := "PLATFORM=windows/x86_64-w64-mingw32;OPTIONS=windows/x86_64-w64-mingw32"
				if err != nil || !strings.Contains(out, want) {
					t.Fatalf("%v: %v\n%s", args, err, out)
				}
				for _, line := range strings.Split(out, "\n") {
					if strings.HasPrefix(line, "PLATFORM=") && line != want {
						t.Fatalf("%v mismatched platform: %s", args, line)
					}
				}
				if strings.Contains(out, "missing-artifact") {
					t.Fatalf("%v found wrong artifact:\n%s", args, out)
				}
			}
			if _, err := os.Stat(filepath.Join(project, "install", "bin", "app_windows.exe")); err != nil {
				t.Fatalf("installed package platform artifact: %v", err)
			}
		})
	}
}

func TestTestCommandUsesPackagePlatform(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses a native ELF test executable")
	}
	program, err := exec.LookPath("true")
	if err != nil {
		t.Skip(err)
	}
	content, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	for _, native := range []bool{false, true} {
		name := "reject package cross target"
		global := map[string]any{"target_os": "linux", "target_triple": ""}
		local := map[string]any{"target_os": "none", "target_triple": "arm-none-eabi"}
		if native {
			name = "allow package native override"
			global, local = local, global
		}
		t.Run(name, func(t *testing.T) {
			project, state := writePackagePlatformProject(t, t.TempDir(), global, local, true)
			if err := os.WriteFile(filepath.Join(project, "program"), content, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(project, "program"), 0755); err != nil {
				t.Fatal(err)
			}
			out, err := extensionCommand(t, state, project, "test")
			if native {
				if err != nil || !strings.Contains(out, "1/1 test(s) passed") {
					t.Fatalf("native package: %v\n%s", err, out)
				}
			} else if err == nil || !strings.Contains(out, "vmake build --tests") || strings.Contains(out, "Running 1 test") {
				t.Fatalf("cross package executed: %v\n%s", err, out)
			}
		})
	}
}
