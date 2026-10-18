package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/buildscript"
)

func writeDoctorFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckStaleProjectMarkersWarnsForPackageLocalVmake(t *testing.T) {
	root := t.TempDir()
	writeDoctorFile(t, filepath.Join(root, "build.go"), "package main\n")
	writeDoctorFile(t, filepath.Join(root, ".vmake", "config.json"), "{}\n")
	pkgDir := filepath.Join(root, "firmware")
	writeDoctorFile(t, filepath.Join(pkgDir, "build.go"), "package main\n")
	writeDoctorFile(t, filepath.Join(pkgDir, ".vmake", "config.json"), "{}\n")

	sources, err := buildscript.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	findings := checkStaleProjectMarkers(root, sources)
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want exactly the package-local marker", findings)
	}
	finding := findings[0]
	if finding.Severity != "warn" || finding.Category != "staleProjectMarker" {
		t.Errorf("finding = %+v", finding)
	}
	if want := filepath.Join(pkgDir, ".vmake"); finding.File != want {
		t.Errorf("file = %q, want %q", finding.File, want)
	}
	for _, want := range []string{
		"package firmware contains a .vmake directory",
		"treats it as the project root",
		"Remove " + filepath.Join(pkgDir, ".vmake"),
	} {
		if !strings.Contains(finding.Message, want) {
			t.Errorf("message %q missing %q", finding.Message, want)
		}
	}
}

func TestCheckStaleProjectMarkersIgnoresRootAndCleanPackages(t *testing.T) {
	root := t.TempDir()
	writeDoctorFile(t, filepath.Join(root, "build.go"), "package main\n")
	writeDoctorFile(t, filepath.Join(root, ".vmake", "config.json"), "{}\n")
	writeDoctorFile(t, filepath.Join(root, "firmware", "build.go"), "package main\n")

	sources, err := buildscript.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if findings := checkStaleProjectMarkers(root, sources); len(findings) != 0 {
		t.Fatalf("root or clean package produced findings: %+v", findings)
	}
}
