package main

import (
	"path/filepath"
	"testing"
)

func writeSessionProject(t *testing.T, dir string, withMarker bool) {
	t.Helper()
	if withMarker {
		writeExtensionFile(t, filepath.Join(dir, ".vmake", "config.json"), `{"version":"1"}`)
	}
	writeExtensionFile(t, filepath.Join(dir, "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
}
`)
}

func TestInspectionSessionRootsAreIndependent(t *testing.T) {
	t.Setenv("VMAKE_CACHE", t.TempDir())
	for _, name := range []string{"project-a", "project-b"} {
		dir := filepath.Join(t.TempDir(), name)
		writeSessionProject(t, dir, false)
		root, err := resolveProjectRootFrom(dir, "")
		if err != nil {
			t.Fatalf("%s: resolveProjectRoot: %v", name, err)
		}
		if root != dir {
			t.Fatalf("%s: root = %q, want %q", name, root, dir)
		}
		session, err := openInspection(inspectionRequest{project: projectOptions{root: root}})
		if err != nil {
			t.Fatalf("%s: openInspection: %v", name, err)
		}
		if session.ctx.WorkDir != dir {
			t.Errorf("%s: WorkDir = %q, want %q", name, session.ctx.WorkDir, dir)
		}
		session.Close()
	}
}

func TestResolveProjectRootRejectsShadowedRoot(t *testing.T) {
	outer := filepath.Join(t.TempDir(), "outer")
	writeSessionProject(t, outer, true)
	inner := filepath.Join(outer, "pkg")
	writeSessionProject(t, inner, true)
	if _, err := resolveProjectRootFrom(inner, ""); err == nil {
		t.Fatal("nested .vmake marker did not report the shadowed root")
	}
}
