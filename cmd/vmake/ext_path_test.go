package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestToolchainPathChild(t *testing.T) {
	if os.Getenv("VMAKE_TEST_PATH_CHILD") != "1" {
		return
	}
	fmt.Print(os.Getenv("PATH"))
	os.Exit(0)
}

func TestAddToolchainToPath(t *testing.T) {
	original := os.Getenv("PATH")
	t.Setenv("PATH", original)
	tc := &toolchain.Toolchain{Name: "cross", InstallPath: t.TempDir()}
	if err := addToolchainToPath(tc); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tc.InstallPath, "bin")
	if original != "" {
		want += string(os.PathListSeparator) + original
	}
	if err := addToolchainToPath(tc); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("PATH"); got != want {
		t.Fatalf("PATH = %q, want %q", got, want)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VMAKE_TEST_PATH_CHILD", "1")
	output, err := exec.Command(exe, "-test.run=^TestToolchainPathChild$").CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, output)
	}
	if string(output) != want {
		t.Fatalf("child PATH = %q, want %q", output, want)
	}
}

func TestToolchainPathMissingInstall(t *testing.T) {
	t.Setenv("PATH", "original")
	if err := addToolchainToPath(&toolchain.Toolchain{Name: "host"}); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("PATH"); got != "original" {
		t.Fatalf("PATH = %q", got)
	}
}

func TestToolchainPathAfterInstall(t *testing.T) {
	t.Setenv("PATH", "")
	tc := &toolchain.Toolchain{Name: "cross", InstallPath: t.TempDir()}
	install := withToolchainPath(func(name string) (*toolchain.Toolchain, error) {
		if name != tc.Name {
			t.Fatalf("name = %q", name)
		}
		return tc, nil
	})
	got, err := install(tc.Name)
	if err != nil || got != tc {
		t.Fatalf("install = %v, %v", got, err)
	}
	if got := os.Getenv("PATH"); got != filepath.Join(tc.InstallPath, "bin") {
		t.Fatalf("PATH = %q", got)
	}
}

func TestToolchainPathInstallFailure(t *testing.T) {
	t.Setenv("PATH", "original")
	want := errors.New("install failed")
	install := withToolchainPath(func(string) (*toolchain.Toolchain, error) {
		return nil, want
	})
	if _, err := install("cross"); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if got := os.Getenv("PATH"); got != "original" {
		t.Fatalf("PATH = %q", got)
	}
}
