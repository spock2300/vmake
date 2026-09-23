package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNonbuildCommandsDoNotInstallToolchains(t *testing.T) {
	for _, state := range []string{"installed", "missing tools", "unregistered"} {
		for _, command := range [][]string{{"clean"}, {"clean", "--all"}, {"lock", "update"}} {
			t.Run(state+"/"+strings.Join(command, " "), func(t *testing.T) {
				dir := t.TempDir()
				stateDir := filepath.Join(dir, "state")
				project := filepath.Join(dir, "project")
				registration := `ctx.RegisterToolchain("pending", tc)`
				if state == "missing tools" {
					registration = `tc.Tools.CC = filepath.Join(ctx.VMakeDir, "missing-cc"); ` + registration
				} else if state == "unregistered" {
					registration = ""
				}
				writeExtensionFlagsPlugin(t, stateDir, "loader", strings.ReplaceAll(`package main
import (
    "fmt"
    "os"
    "path/filepath"
    "github.com/spock2300/vmake/pkg/plugin"
    "github.com/spock2300/vmake/pkg/toolchain"
)
func Main(ctx *plugin.Context) {
    executable, err := os.Executable()
    if err != nil { panic(err) }
    tc := &toolchain.Toolchain{Name: "pending", Tools: toolchain.Tools{CC: executable, CXX: executable, AR: executable, LD: executable}}
    _ = tc
    __REGISTER__
    for _, name := range []string{"pending", "unrelated"} {
        ctx.SetOnMissing(name, func(name string) (*toolchain.Toolchain, error) {
            if err := os.WriteFile(filepath.Join(ctx.VMakeDir, "installer-called"), []byte(name), 0644); err != nil { return nil, err }
            return nil, fmt.Errorf("unexpected installer invocation: %s", name)
        })
    }
}
`, "__REGISTER__", registration))
				writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"version":"1","global":{"toolchain":"pending"},"entries":{"unused":{"options":{"toolchain":"unrelated"}}}}`)
				writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) { p.SetRoot(true) }
`)
				artifact := filepath.Join(project, "build", "old", "artifact")
				writeExtensionFile(t, artifact, "artifact")
				out, err := extensionCommand(t, stateDir, project, command...)
				cleanAll := len(command) == 2 && command[1] == "--all"
				if state == "installed" || cleanAll {
					if err != nil {
						t.Errorf("%v: %v\n%s", command, err, out)
					}
					if command[0] == "clean" {
						if cleanAll {
							if _, err := os.Stat(artifact); !os.IsNotExist(err) {
								t.Errorf("clean --all left build artifact: %v\n%s", err, out)
							}
						} else if _, err := os.Stat(artifact); err != nil {
							t.Errorf("clean removed a directory outside the current configuration: %v\n%s", err, out)
						}
					} else if !strings.Contains(out, "Downloading package sources") {
						t.Errorf("lock update did not complete:\n%s", out)
					}
				} else {
					if err == nil || !strings.Contains(out, "vmake build --toolchain pending") {
						t.Errorf("missing toolchain did not fail with build hint: %v\n%s", err, out)
					}
					if _, err := os.Stat(artifact); err != nil {
						t.Errorf("failed command changed build artifact: %v", err)
					}
				}
				if _, err := os.Stat(filepath.Join(stateDir, "installer-called")); !os.IsNotExist(err) {
					t.Errorf("%v called an installer: %v\n%s", command, err, out)
				}
			})
		}
	}
}

func TestCleanHooksWithExistingToolchains(t *testing.T) {
	for _, installed := range []bool{false, true} {
		for _, command := range [][]string{{"clean"}, {"clean", "--all"}} {
			name := "missing/"
			if installed {
				name = "installed/"
			}
			t.Run(name+strings.Join(command, " "), func(t *testing.T) {
				dir := t.TempDir()
				stateDir := filepath.Join(dir, "state")
				project := filepath.Join(dir, "project")
				if installed {
					writeHealthyDefinition(t, filepath.Join(stateDir, "extensions", "fixture", "pending", "toolchain.json"), "pending")
				}
				writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"version":"1","global":{"toolchain":"pending"},"entries":{}}`)
				writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import (
    "os"
    "github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnClean(func(ctx *api.CleanContext) {
        if err := os.WriteFile("clean-hook", []byte(p.CC()), 0644); err != nil { panic(err) }
    })
}
`)
				artifact := filepath.Join(project, "build", "old", "artifact")
				writeExtensionFile(t, artifact, "artifact")
				out, err := extensionCommand(t, stateDir, project, command...)
				cleanAll := len(command) == 2
				if installed || cleanAll {
					if err != nil {
						t.Errorf("%v: %v\n%s", command, err, out)
					}
					if cleanAll {
						if _, err := os.Stat(artifact); !os.IsNotExist(err) {
							t.Errorf("clean --all left build artifact: %v\n%s", err, out)
						}
					} else if _, err := os.Stat(artifact); err != nil {
						t.Errorf("clean removed a directory outside the current configuration: %v\n%s", err, out)
					}
				} else if err == nil {
					t.Errorf("clean succeeded with unavailable hook toolchain:\n%s", out)
				}
				if installed {
					if content, err := os.ReadFile(filepath.Join(project, "clean-hook")); err != nil || len(content) == 0 {
						t.Errorf("OnClean did not run with existing compiler: %q, %v\n%s", content, err, out)
					}
				} else {
					if !strings.Contains(out, "OnClean") || !strings.Contains(out, "vmake build --toolchain pending") {
						t.Errorf("missing hook toolchain had no actionable diagnostic:\n%s", out)
					}
					if _, err := os.Stat(filepath.Join(project, "clean-hook")); !os.IsNotExist(err) {
						t.Errorf("unavailable OnClean hook ran: %v", err)
					}
				}
			})
		}
	}
}

func TestCleanHooksUsePackagePlatform(t *testing.T) {
	for _, test := range []struct {
		name      string
		overrides map[string]any
		os        string
		triple    string
		packageOS string
	}{
		{name: "global", os: "none", triple: "arm-none-eabi", packageOS: "none"},
		{name: "null", overrides: map[string]any{"target_os": nil, "target_triple": nil}, os: "none", triple: "arm-none-eabi", packageOS: "none"},
		{name: "override", overrides: map[string]any{"target_os": "windows", "target_triple": "x86_64-w64-mingw32"}, os: "windows", triple: "x86_64-w64-mingw32", packageOS: "windows"},
		{name: "empty", overrides: map[string]any{"target_os": "", "target_triple": ""}, packageOS: runtime.GOOS},
	} {
		for _, command := range [][]string{{"clean"}, {"clean", "--all"}} {
			t.Run(test.name+"/"+strings.Join(command, " "), func(t *testing.T) {
				dir := t.TempDir()
				state := filepath.Join(dir, "state")
				project := filepath.Join(dir, "project")
				writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "pending", "toolchain.json"), "pending")
				cfg := map[string]any{
					"version": "1", "global": map[string]any{"toolchain": "pending"},
					"entries": map[string]any{"project": map[string]any{"options": test.overrides}},
				}
				data, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), string(data))
				writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import (
    "fmt"
    "github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none")
        ctx.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString).SetDefault("arm-none-eabi")
    })
    p.OnClean(func(ctx *api.CleanContext) {
        fmt.Printf("CLEAN-PLATFORM=%q/%q/%q/%q/%q/%q\n", ctx.String(api.TargetOSOptionName), ctx.String(api.TargetTripleOptionName), p.TargetOS(), p.TargetTriple(), p.String(api.TargetOSOptionName), p.String(api.TargetTripleOptionName))
    })
}
`)
				artifact := filepath.Join(project, "build", "old", "artifact")
				writeExtensionFile(t, artifact, "artifact")
				out, err := extensionCommand(t, state, project, command...)
				if err != nil {
					t.Fatalf("%v: %v\n%s", command, err, out)
				}
				want := fmt.Sprintf("CLEAN-PLATFORM=%q/%q/%q/%q/%q/%q\n", test.os, test.triple, test.packageOS, test.triple, test.os, test.triple)
				if strings.Count(out, want) != 1 {
					t.Fatalf("OnClean platform: want %q once:\n%s", want, out)
				}
				if len(command) == 2 {
					if _, err := os.Stat(artifact); !os.IsNotExist(err) {
						t.Fatalf("clean --all left artifact: %v\n%s", err, out)
					}
				} else if _, err := os.Stat(artifact); err != nil {
					t.Fatalf("clean removed a directory outside the current configuration: %v\n%s", err, out)
				}
			})
		}
	}
}

func TestCleanHooksRejectInvalidPackagePlatform(t *testing.T) {
	for _, option := range []string{"target_os", "target_triple"} {
		for _, command := range [][]string{{"clean"}, {"clean", "--all"}} {
			t.Run(option+"/"+strings.Join(command, " "), func(t *testing.T) {
				dir := t.TempDir()
				state := filepath.Join(dir, "state")
				project := filepath.Join(dir, "project")
				writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "pending", "toolchain.json"), "pending")
				writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), fmt.Sprintf(`{"version":"1","global":{"toolchain":"pending"},"entries":{"project":{"options":{%q:true}}}}`, option))
				writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import (
    "fmt"
    "github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnClean(func(ctx *api.CleanContext) { fmt.Println("CLEAN-HOOK-RAN") })
}
`)
				artifact := filepath.Join(project, "build", "old", "artifact")
				writeExtensionFile(t, artifact, "artifact")
				out, err := extensionCommand(t, state, project, command...)
				all := len(command) == 2
				if (err == nil) != all {
					t.Fatalf("%v: %v\n%s", command, err, out)
				}
				if !strings.Contains(out, "OnClean") || !strings.Contains(out, "project") || !strings.Contains(out, option) || !strings.Contains(out, "must be a string") || strings.Contains(out, "CLEAN-HOOK-RAN") {
					t.Fatalf("invalid platform was not rejected before OnClean:\n%s", out)
				}
				_, err = os.Stat(artifact)
				if all && !os.IsNotExist(err) || !all && err != nil {
					t.Fatalf("unexpected artifact state after %v: %v", command, err)
				}
			})
		}
	}
}
