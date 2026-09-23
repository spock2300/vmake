package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/buildruntime"
)

func TestBuildRuntimeBindingCopiesBudget(t *testing.T) {
	p := NewPackage()
	budget := &buildruntime.Budget{Jobs: 2}
	if err := BindBuildRuntime(p, budget, nil); err != nil {
		t.Fatal(err)
	}
	budget.Jobs = 20
	if p.executionBudget().Jobs != 2 {
		t.Fatal("caller changed a package's bound budget")
	}
	for _, invalid := range []*buildruntime.Budget{nil, {}, {Jobs: -1}} {
		if err := BindBuildRuntime(p, invalid, nil); err == nil {
			t.Fatalf("accepted invalid budget %+v", invalid)
		}
	}
	if err := BindBuildRuntime(nil, budget, nil); err == nil {
		t.Fatal("accepted nil package")
	}
}

func TestMakeExecutesWithBoundBudget(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip(err)
	}
	for _, key := range []string{"MAKEFLAGS", "MFLAGS", "GNUMAKEFLAGS"} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("all:\n\t$(file >parallel.txt,$(MAKEFLAGS))\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p := NewPackage().SetDirs(PkgDirs{BuildDir: dir})
	if err := BindBuildRuntime(p, &buildruntime.Budget{Jobs: 3}, nil); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"-j2"}} {
		if err := p.Make(args...); err != nil {
			t.Fatal(err)
		}
		want := "-j3"
		if len(args) > 0 {
			want = "-j2"
		}
		data, err := os.ReadFile(filepath.Join(dir, "parallel.txt"))
		if err != nil || !strings.Contains(string(data), want) {
			t.Fatalf("MAKEFLAGS = %q, %v; want %s", data, err, want)
		}
	}
	t.Setenv("MAKEFLAGS", "-j8")
	if err := p.Make(); err == nil || !strings.Contains(err.Error(), "MAKEFLAGS") {
		t.Fatalf("oversized inherited MAKEFLAGS: %v", err)
	}
	if os.Getenv("MAKEFLAGS") != "-j8" {
		t.Fatal("helper changed process environment")
	}
}
