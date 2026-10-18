package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

func TestDoctorBootstrapChild(t *testing.T) {
	dir := os.Getenv("VMAKE_TEST_DOCTOR_DIR")
	if dir == "" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing command arguments")
	}
	vmakeDir = dir
	os.Args = append([]string{"vmake"}, os.Args[separator+1:]...)
	main()
	os.Exit(0)
}

func runDoctorCommand(t *testing.T, workDir string, args ...string) (string, error) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commandArgs := append([]string{"-test.run=^TestDoctorBootstrapChild$", "--"}, args...)
	cmd := exec.Command(exe, commandArgs...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"VMAKE_TEST_DOCTOR_DIR="+t.TempDir(),
		"VMAKE_CACHE="+t.TempDir(),
		"VMAKE_TRUST_ALL=1",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

const doctorRootBuildGo = `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
}
`

const doctorPackageBuildGo = `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
}
`

func writeDoctorProject(t *testing.T, poisoned bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	writeDoctorFile(t, filepath.Join(root, "build.go"), doctorRootBuildGo)
	writeDoctorFile(t, filepath.Join(root, ".vmake", "config.json"), "{}\n")
	pkg := filepath.Join(root, "firmware")
	writeDoctorFile(t, filepath.Join(pkg, "build.go"), doctorPackageBuildGo)
	if poisoned {
		writeDoctorFile(t, filepath.Join(pkg, ".vmake", "config.json"), "{}\n")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(root, "firmware")
}

func TestDoctorNestedProjectMarker(t *testing.T) {
	t.Run("from_poisoned_package", func(t *testing.T) {
		root, pkg := writeDoctorProject(t, true)
		marker := filepath.Join(pkg, ".vmake")
		out, err := runDoctorCommand(t, pkg, "doctor")
		if err != nil {
			t.Fatalf("doctor from %s: %v\n%s", pkg, err, out)
		}
		for _, want := range []string{
			"nestedProjectMarker",
			"enclosing project root " + root,
			"vmake -C " + root,
			"remove " + marker,
			"this nested marker shadows the project root",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("doctor output missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "staleProjectMarker") {
			t.Errorf("doctor from the poisoned package also reported staleProjectMarker:\n%s", out)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("doctor removed the nested marker: %v", err)
		}
	})

	t.Run("from_project_root", func(t *testing.T) {
		root, pkg := writeDoctorProject(t, true)
		marker := filepath.Join(pkg, ".vmake")
		out, err := runDoctorCommand(t, root, "doctor")
		if err != nil {
			t.Fatalf("doctor from %s: %v\n%s", root, err, out)
		}
		for _, want := range []string{"staleProjectMarker", marker} {
			if !strings.Contains(out, want) {
				t.Errorf("doctor output missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "nestedProjectMarker") {
			t.Errorf("doctor from the project root also reported nestedProjectMarker:\n%s", out)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("doctor removed the nested marker: %v", err)
		}
	})

	t.Run("clean_project", func(t *testing.T) {
		root, _ := writeDoctorProject(t, false)
		out, err := runDoctorCommand(t, root, "doctor")
		if err != nil {
			t.Fatalf("doctor from %s: %v\n%s", root, err, out)
		}
		for _, category := range []string{"nestedProjectMarker", "staleProjectMarker"} {
			if strings.Contains(out, category) {
				t.Errorf("clean project reported %s:\n%s", category, out)
			}
		}
	})

	t.Run("project_override", func(t *testing.T) {
		root, pkg := writeDoctorProject(t, true)
		out, err := runDoctorCommand(t, pkg, "-C", root, "doctor")
		if err != nil {
			t.Fatalf("doctor -C %s from %s: %v\n%s", root, pkg, err, out)
		}
		if !strings.Contains(out, "staleProjectMarker") {
			t.Errorf("-C did not resolve the enclosing project root:\n%s", out)
		}
		if strings.Contains(out, "nestedProjectMarker") {
			t.Errorf("-C reported nestedProjectMarker:\n%s", out)
		}
	})
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
