package main

import (
	"bytes"
	"encoding/json"
	"errors"
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

func runDoctorCommandSplit(t *testing.T, workDir string, args ...string) (string, string, int) {
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run doctor: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
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

func writeDoctorCleanProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	writeDoctorFile(t, filepath.Join(root, "build.go"), doctorPackageBuildGo)
	writeDoctorFile(t, filepath.Join(root, ".vmake", "config.json"), "{}\n")
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func decodeDoctorJSON(t *testing.T, stdout string) doctorJSONDocument {
	t.Helper()
	var document doctorJSONDocument
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	return document
}

func TestDoctorJSONCleanProject(t *testing.T) {
	root := writeDoctorCleanProject(t)
	human, _, code := runDoctorCommandSplit(t, root, "doctor")
	if code != 0 || strings.Contains(human, "[warn]") || strings.Contains(human, "[error]") {
		t.Skipf("host platform checks are not clean, cannot assert an empty findings list:\n%s", human)
	}
	stdout, stderr, code := runDoctorCommandSplit(t, root, "doctor", "--json")
	if code != 0 {
		t.Fatalf("doctor --json exit = %d, stderr = %q", code, stderr)
	}
	document := decodeDoctorJSON(t, stdout)
	if document.SchemaVersion != 1 {
		t.Errorf("schemaVersion = %d, want 1", document.SchemaVersion)
	}
	if document.Status != "ok" {
		t.Errorf("status = %q, want ok (stderr %q)", document.Status, stderr)
	}
	if document.Root != root {
		t.Errorf("root = %q, want %q", document.Root, root)
	}
	if len(document.Findings) != 0 {
		t.Errorf("findings = %+v, want [] for a clean project", document.Findings)
	}
	if strings.Contains(stdout, "[ok]") || strings.Contains(stdout, "OK: no issues") {
		t.Errorf("human output leaked into the JSON document:\n%s", stdout)
	}
}

func TestDoctorJSONNestedProjectMarker(t *testing.T) {
	root, pkg := writeDoctorProject(t, true)
	stdout, stderr, code := runDoctorCommandSplit(t, pkg, "doctor", "--json")
	if code != 0 {
		t.Fatalf("doctor --json from the poisoned package exit = %d, stderr = %q", code, stderr)
	}
	document := decodeDoctorJSON(t, stdout)
	if document.Status != "warnings" {
		t.Fatalf("status = %q, want warnings (findings %+v)", document.Status, document.Findings)
	}
	if document.Root != pkg {
		t.Errorf("root = %q, want the nested root %q", document.Root, pkg)
	}
	found := false
	for _, finding := range document.Findings {
		if finding.Category != "nestedProjectMarker" {
			continue
		}
		found = true
		if finding.Severity != "warn" {
			t.Errorf("nestedProjectMarker severity = %q", finding.Severity)
		}
		if finding.File != ".vmake" {
			t.Errorf("nestedProjectMarker file = %q, want .vmake", finding.File)
		}
		if !strings.Contains(finding.Message, "enclosing project root "+root) {
			t.Errorf("nestedProjectMarker message = %q", finding.Message)
		}
	}
	if !found {
		t.Fatalf("nestedProjectMarker missing from findings: %+v", document.Findings)
	}
	if strings.Contains(stdout, "[warn]") || strings.Contains(stdout, "Summary:") {
		t.Errorf("human output leaked into the JSON document:\n%s", stdout)
	}

	again, _, code := runDoctorCommandSplit(t, pkg, "doctor", "--json")
	if code != 0 {
		t.Fatalf("second doctor --json exit = %d", code)
	}
	if again != stdout {
		t.Errorf("doctor --json is not deterministic:\nfirst:\n%s\nsecond:\n%s", stdout, again)
	}
}

func TestDoctorJSONErrorFindingExitsOne(t *testing.T) {
	root := writeDoctorCleanProject(t)
	stdout, stderr, code := runDoctorCommandSplit(t, root, "doctor", "--json", "--toolchain", "nonexistent")
	if code != 1 {
		t.Fatalf("doctor --json with an error finding exit = %d, want 1 (stderr %q)", code, stderr)
	}
	document := decodeDoctorJSON(t, stdout)
	if document.Status != "error" {
		t.Fatalf("status = %q, want error (findings %+v)", document.Status, document.Findings)
	}
	found := false
	for _, finding := range document.Findings {
		if finding.Category == "toolchain" {
			found = true
			if finding.Severity != "error" {
				t.Errorf("toolchain severity = %q", finding.Severity)
			}
		}
	}
	if !found {
		t.Fatalf("toolchain error missing from findings: %+v", document.Findings)
	}
}

func TestDoctorJSONNoBuildGoExitsOne(t *testing.T) {
	dir := t.TempDir()
	stdout, stderr, code := runDoctorCommandSplit(t, dir, "doctor", "--json")
	if code != 1 {
		t.Fatalf("doctor --json without build.go exit = %d, want 1 (stderr %q)", code, stderr)
	}
	document := decodeDoctorJSON(t, stdout)
	if document.Status != "error" {
		t.Fatalf("status = %q, want error (findings %+v)", document.Status, document.Findings)
	}
	found := false
	for _, finding := range document.Findings {
		if finding.Category == "noBuildGo" && finding.Severity == "error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("noBuildGo error missing from findings: %+v", document.Findings)
	}
}
