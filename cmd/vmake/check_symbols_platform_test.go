package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckSymbolsUsesProjectPlatformDefaults(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires a native ELF compiler")
	}
	for _, tool := range []string{"gcc", "g++", "ar", "ld", "strip", "ranlib", "objcopy", "size", "objdump", "nm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(err)
		}
	}
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	state := filepath.Join(dir, "state")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnConfig(func(ctx *api.ConfigContext) {
        ctx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none")
    })
    p.OnBuild(func(ctx *api.BuildContext) {
        ctx.Target("app_" + p.TargetOS()).SetKind(api.TargetBinary).AddFiles("main.c")
    })
}
`)
	writeExtensionFile(t, filepath.Join(project, "main.c"), "int main(void) { return 0; }\n")
	if out, err := extensionCommand(t, state, project, "build"); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	out, err := extensionCommand(t, state, project, "check-symbols", "--strict")
	if err != nil || strings.Contains(out, "missing-artifact") || !strings.Contains(out, "1 artifacts") {
		t.Fatalf("check-symbols: %v\n%s", err, out)
	}
}
