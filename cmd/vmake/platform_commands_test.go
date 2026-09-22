package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestTestExecutionRejectsCrossTargets(t *testing.T) {
	for _, platform := range []api.Platform{
		{OS: "none", Triple: "arm-none-eabi"},
		{OS: "linux", Triple: "aarch64-linux-gnu"},
		{OS: "windows"},
	} {
		if err := validateTestExecution(platform, "linux"); err == nil || !strings.Contains(err.Error(), "vmake build --tests") {
			t.Errorf("platform %v: %v", platform, err)
		}
	}
	if err := validateTestExecution(api.Platform{OS: "linux"}, "linux"); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorChecksSelectedToolsWithoutNativeGCC(t *testing.T) {
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	tc := &toolchain.Toolchain{
		Name:  "arm",
		Tools: toolchain.Tools{CC: program, CXX: program, AR: program, LD: program, MAKE: program},
	}
	findings := checkToolchain(tc)
	for _, finding := range findings {
		if finding.Severity != "ok" {
			t.Fatalf("unexpected native tool requirement: %+v", finding)
		}
	}
	tc.Tools.CC = filepath.Join(t.TempDir(), "missing-arm-gcc")
	findings = checkToolchain(tc)
	for _, finding := range findings {
		if finding.Severity == "error" && strings.Contains(finding.Message, "missing-arm-gcc") {
			return
		}
	}
	t.Fatalf("selected compiler failure missing: %+v", findings)
}

func TestCheckSymbolsRecognizesELFCapability(t *testing.T) {
	gcc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "symbols.c")
	if err := os.WriteFile(source, []byte("int public_api(void) { return 42; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	object := filepath.Join(dir, "symbols.o")
	if out, err := exec.Command(gcc, "-c", source, "-o", object).CombinedOutput(); err != nil {
		t.Fatalf("compile object: %v\n%s", err, out)
	}
	if err := checkDynamicSymbols(object); errors.Is(err, errNonELFArtifact) {
		t.Skip("compiler produces non-ELF objects")
	} else if !errors.Is(err, errNoDynamicSymbols) {
		t.Fatalf("static object: %v", err)
	}
	shared := filepath.Join(dir, "symbols.so")
	if out, err := exec.Command(gcc, "-shared", "-fPIC", source, "-o", shared).CombinedOutput(); err != nil {
		t.Fatalf("compile shared library: %v\n%s", err, out)
	}
	if err := checkDynamicSymbols(shared); err != nil {
		t.Fatalf("shared ELF: %v", err)
	}
	pe := filepath.Join(dir, "windows.exe")
	if err := os.WriteFile(pe, append([]byte("MZ"), make([]byte, 128)...), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkDynamicSymbols(pe); !errors.Is(err, errNonELFArtifact) {
		t.Fatalf("PE artifact: %v", err)
	}
}

func TestReadExportsIncludesConfiguredToolFailure(t *testing.T) {
	program := filepath.Join(t.TempDir(), "missing arm nm")
	_, err := readExports(program, "test firmware.elf")
	if err == nil || !strings.Contains(err.Error(), "missing arm nm") || !strings.Contains(err.Error(), "-D") || !strings.Contains(err.Error(), "test firmware.elf") {
		t.Fatalf("error = %v", err)
	}
}
