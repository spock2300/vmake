package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/toolchain"
)

func writeFakeTool(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
}

func fakeToolName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func TestFormatToolchainTarget(t *testing.T) {
	for _, test := range []struct {
		tc   *toolchain.Toolchain
		want string
	}{
		{&toolchain.Toolchain{TargetOS: "none", TargetTriple: "arm-none-eabi"}, "none/arm-none-eabi"},
		{&toolchain.Toolchain{TargetOS: "linux"}, "linux"},
		{&toolchain.Toolchain{TargetTriple: "aarch64-linux-gnu"}, "aarch64-linux-gnu"},
		{&toolchain.Toolchain{}, ""},
	} {
		if got := formatToolchainTarget(test.tc); got != test.want {
			t.Errorf("formatToolchainTarget(%+v) = %q, want %q", test.tc, got, test.want)
		}
	}
}

func TestFormatToolchainTool(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("PATH", binDir)
	for _, name := range []string{"gcc", "make"} {
		writeFakeTool(t, filepath.Join(binDir, fakeToolName(name)))
	}

	tc := &toolchain.Toolchain{Name: "host", Tools: toolchain.Tools{CC: "gcc"}}
	got := formatToolchainTool(tc, "CC", tc.Tools.CC)
	if !strings.Contains(got, "gcc -> ") || !strings.Contains(got, "[PATH]") {
		t.Fatalf("PATH tool = %q", got)
	}
	if !strings.Contains(got, filepath.ToSlash(filepath.Join(binDir, fakeToolName("gcc")))) {
		t.Fatalf("PATH tool missing resolved path: %q", got)
	}

	installDir := t.TempDir()
	writeFakeTool(t, filepath.Join(installDir, "bin", fakeToolName("mycc")))
	tc = &toolchain.Toolchain{
		Name:        "cross",
		InstallPath: installDir,
		Tools:       toolchain.Tools{CC: "mycc"},
	}
	got = formatToolchainTool(tc, "CC", tc.Tools.CC)
	if !strings.Contains(got, "[install]") {
		t.Fatalf("installed tool = %q", got)
	}
	if !strings.Contains(got, filepath.ToSlash(filepath.Join(installDir, "bin", fakeToolName("mycc")))) {
		t.Fatalf("installed tool missing resolved path: %q", got)
	}

	absDir := t.TempDir()
	absTool := filepath.Join(absDir, fakeToolName("abscc"))
	writeFakeTool(t, absTool)
	tc = &toolchain.Toolchain{Name: "abs", Tools: toolchain.Tools{CC: absTool}}
	got = formatToolchainTool(tc, "CC", tc.Tools.CC)
	if !strings.Contains(got, "[absolute]") {
		t.Fatalf("absolute tool = %q", got)
	}

	got = formatToolchainTool(tc, "CXX", "")
	if !strings.Contains(got, "(not configured)") {
		t.Fatalf("unconfigured tool = %q", got)
	}

	got = formatToolchainTool(tc, "CXX", "missing-vmake-tool")
	if !strings.Contains(got, "(not found:") {
		t.Fatalf("missing tool = %q", got)
	}

	tc = &toolchain.Toolchain{Name: "cross", InstallPath: installDir}
	got = formatToolchainTool(tc, "MAKE", tc.MakeTool())
	if !strings.Contains(got, "[PATH]") {
		t.Fatalf("default make should resolve from PATH, got %q", got)
	}
}
