package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func writeExtensionFlagsPlugin(t *testing.T, dir, name, source string) {
	t.Helper()
	pluginDir := filepath.Join(dir, "extensions", "fixture", name)
	writeExtensionFile(t, filepath.Join(pluginDir, "plugin.json"), fmt.Sprintf(`{"name":%q,"entry":"main.go","enabled":true}`, name))
	writeExtensionFile(t, filepath.Join(pluginDir, "main.go"), source)
}

func TestExtensionGlobalFlagCallbacks(t *testing.T) {
	for _, flags := range []struct{ name, add, get string }{
		{"c", "AddGlobalCFlags", "GetGlobalCFlags"},
		{"cxx", "AddGlobalCxxFlags", "GetGlobalCxxFlags"},
		{"ld", "AddGlobalLdFlags", "GetGlobalLdFlags"},
	} {
		for _, callback := range []string{"command", "missing"} {
			t.Run(flags.name+"/"+callback, func(t *testing.T) {
				dir := t.TempDir()
				action := `saved("late", "repeat"); ctx.${ADD}("repeat", "tail")`
				commandAction := action
				missingAction := ""
				if callback == "missing" {
					missingAction = action
					commandAction = `if _, err := toolchain.GetManager().SelectToolchain("deferred"); err != nil { panic(err) }`
				}
				source := strings.NewReplacer(
					"${COMMAND}", commandAction,
					"${MISSING}", missingAction,
				).Replace(`package main
import (
    "fmt"
    "os"
    "github.com/spf13/cobra"
    "github.com/spock2300/vmake/pkg/plugin"
    "github.com/spock2300/vmake/pkg/toolchain"
)
func Main(ctx *plugin.Context) {
    saved := ctx.${ADD}
    ctx.${ADD}("first", "repeat")
    saved("repeat", "last")
    fmt.Printf("INITIALIZING=%q\n", toolchain.GetManager().${GET}())
    ctx.SetOnMissing("deferred", func(name string) (*toolchain.Toolchain, error) {
        ${MISSING}
        executable, err := os.Executable()
        if err != nil { return nil, err }
        return &toolchain.Toolchain{Name:name, Tools:toolchain.Tools{CC:executable, CXX:executable, AR:executable, LD:executable}}, nil
    })
    ctx.AddSubCommand(&cobra.Command{Use:"show", Run:func(cmd *cobra.Command, args []string) {
        fmt.Printf("COMMITTED=%q\n", toolchain.GetManager().${GET}())
        ${COMMAND}
        fmt.Printf("AFTER=%q\n", toolchain.GetManager().${GET}())
    }})
}
`)
				source = strings.NewReplacer("${ADD}", flags.add, "${GET}", flags.get).Replace(source)
				writeExtensionFlagsPlugin(t, dir, "flags", source)
				out, err := extensionCommand(t, dir, dir, "flags", "show")
				if err != nil {
					t.Fatalf("plugin flags: %v\n%s", err, out)
				}
				for _, want := range []string{
					"INITIALIZING=[]",
					`COMMITTED=["first" "repeat" "repeat" "last"]`,
					`AFTER=["first" "repeat" "repeat" "last" "late" "repeat" "repeat" "tail"]`,
				} {
					if !strings.Contains(out, want+"\n") {
						t.Fatalf("missing %s:\n%s", want, out)
					}
				}
			})
		}
	}
}

func TestExtensionFailedInitializationDiscardsRegistrations(t *testing.T) {
	dir := t.TempDir()
	writeExtensionFlagsPlugin(t, dir, "a-failed", `package main
import (
    "os"
    "github.com/spock2300/vmake/pkg/plugin"
    "github.com/spock2300/vmake/pkg/toolchain"
)
func Main(ctx *plugin.Context) {
    cflags := ctx.AddGlobalCFlags
    cxxflags := ctx.AddGlobalCxxFlags
    ldflags := ctx.AddGlobalLdFlags
    cflags("initial-c")
    cxxflags("initial-cxx")
    ldflags("initial-ld")
    if err := ctx.RegisterToolchain("failed-tool", &toolchain.Toolchain{Name:"failed-tool"}); err != nil { panic(err) }
    ctx.SetOnMissing("failed", func(name string) (*toolchain.Toolchain, error) {
        cflags("late-c")
        cxxflags("late-cxx")
        ldflags("late-ld")
        executable, err := os.Executable()
        if err != nil { return nil, err }
        return &toolchain.Toolchain{Name:name, Tools:toolchain.Tools{CC:executable, CXX:executable, AR:executable, LD:executable}}, nil
    })
    panic("expected initialization failure")
}
`)
	writeExtensionFlagsPlugin(t, dir, "z-observer", `package main
import (
    "fmt"
    "github.com/spf13/cobra"
    "github.com/spock2300/vmake/pkg/plugin"
    "github.com/spock2300/vmake/pkg/toolchain"
)
func Main(ctx *plugin.Context) {
    ctx.AddSubCommand(&cobra.Command{Use:"show", Run:func(cmd *cobra.Command, args []string) {
        manager := toolchain.GetManager()
        fmt.Printf("BEFORE=%q/%q/%q\n", manager.GetGlobalCFlags(), manager.GetGlobalCxxFlags(), manager.GetGlobalLdFlags())
        if _, err := manager.SelectToolchain("failed"); err == nil { panic("failed plugin retained its missing handler") }
        if _, err := manager.GetToolchain("failed-tool"); err == nil { panic("failed plugin retained its toolchain") }
        fmt.Printf("AFTER=%q/%q/%q\n", manager.GetGlobalCFlags(), manager.GetGlobalCxxFlags(), manager.GetGlobalLdFlags())
    }})
}
`)
	out, err := extensionCommand(t, dir, dir, "z-observer", "show")
	if err != nil {
		t.Fatalf("observe failed plugin: %v\n%s", err, out)
	}
	for _, want := range []string{"expected initialization failure", "BEFORE=[]/[]/[]", "AFTER=[]/[]/[]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}
