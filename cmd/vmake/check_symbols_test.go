package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestParseVersionScriptGlobalsSimple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.map")
	content := `{
    global:
        foo_api;
        foo_init;
        foo_shutdown;
    local:
        *;
};`
	_ = os.WriteFile(path, []byte(content), 0644)

	got, err := parseVersionScriptGlobals(path)
	if err != nil {
		t.Fatalf("parseVersionScriptGlobals: %v", err)
	}
	want := map[string]bool{
		"foo_api":      true,
		"foo_init":     true,
		"foo_shutdown": true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseVersionScriptGlobalsVersioned(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.map")
	content := `V1_0 {
    global:
        api_one;
        api_two;
    local:
        *;
};

V2_0 {
    global:
        api_three;
} V1_0;`
	_ = os.WriteFile(path, []byte(content), 0644)

	got, _ := parseVersionScriptGlobals(path)
	if !got["api_one"] || !got["api_two"] || !got["api_three"] {
		t.Errorf("missing expected symbols, got %v", got)
	}
	if got["*"] {
		t.Error("wildcard * should not be collected")
	}
}

func TestParseVersionScriptGlobalsMissingFile(t *testing.T) {
	_, err := parseVersionScriptGlobals("/nonexistent.map")
	if err == nil {
		t.Error("missing file should error")
	}
}

func TestDetectMangledLeaks(t *testing.T) {
	artifacts := []scanArtifact{
		{
			pkgName:    "libfoo",
			targetName: "foo",
			exports:    []string{"foo_init", "_ZN3foo3barC1Ev", "_Z5hellov"},
		},
	}
	findings := detectMangledLeaks(artifacts)
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(findings))
	}
	for _, f := range findings {
		if f.category != "mangled-leak" {
			t.Errorf("category = %q", f.category)
		}
	}
}

func TestDetectMangledLeaksClean(t *testing.T) {
	artifacts := []scanArtifact{
		{exports: []string{"foo_init", "foo_cleanup", "normal_c_symbol"}},
	}
	if len(detectMangledLeaks(artifacts)) != 0 {
		t.Error("clean C symbols should produce no mangled-leak findings")
	}
}

func TestDetectReservedPrefixes(t *testing.T) {
	artifacts := []scanArtifact{
		{
			pkgName:    "libfoo",
			targetName: "foo",
			exports: []string{
				"normal_api",
				"__libc_init",
				"_IO_file_open",
				"_Jv_register",
				"__cxa_throw",
			},
		},
	}
	findings := detectReservedPrefixes(artifacts)
	if len(findings) != 4 {
		t.Fatalf("got %d findings, want 4", len(findings))
	}
}

func TestDetectReservedPrefixesClean(t *testing.T) {
	artifacts := []scanArtifact{
		{exports: []string{"normal_api", "another_one", "x_y_z"}},
	}
	if len(detectReservedPrefixes(artifacts)) != 0 {
		t.Error("clean symbols should produce no findings")
	}
}

func TestDetectDuplicateExports(t *testing.T) {
	artifacts := []scanArtifact{
		{pkgName: "libcore", targetName: "core", exports: []string{"helper_init", "core_api"}},
		{pkgName: "libnet", targetName: "net", exports: []string{"helper_init", "net_api"}},
		{pkgName: "libfoo", targetName: "foo", exports: []string{"foo_api"}},
	}
	findings := detectDuplicateExports(artifacts)
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1 (helper_init)", len(findings))
	}
	if findings[0].symbol != "helper_init" {
		t.Errorf("symbol = %q", findings[0].symbol)
	}
}

func TestDetectDuplicateExportsNone(t *testing.T) {
	artifacts := []scanArtifact{
		{pkgName: "a", targetName: "a", exports: []string{"sym_a"}},
		{pkgName: "b", targetName: "b", exports: []string{"sym_b"}},
	}
	if len(detectDuplicateExports(artifacts)) != 0 {
		t.Error("distinct symbols should not produce findings")
	}
}

func TestDetectNoVersionScript(t *testing.T) {
	artifacts := []scanArtifact{
		{pkgName: "libcore", targetName: "core", kind: api.TargetShared, versionScript: ""},
		{pkgName: "libnet", targetName: "net", kind: api.TargetShared, versionScript: "net.map"},
		{pkgName: "webapp", targetName: "webapp", kind: api.TargetBinary, versionScript: ""},
	}
	findings := detectNoVersionScript(artifacts)
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1 (libcore only)", len(findings))
	}
	if findings[0].subject != "libcore:core" {
		t.Errorf("subject = %q", findings[0].subject)
	}
}

func TestDetectVersionScriptViolations(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "test.map")
	content := `{
    global:
        good_api;
        another_good;
    local:
        *;
};`
	_ = os.WriteFile(mapPath, []byte(content), 0644)

	artifacts := []scanArtifact{{
		pkgName:       "libfoo",
		targetName:    "foo",
		outputPath:    "/dummy/path",
		versionScript: mapPath,
		exports:       []string{"good_api", "another_good", "leaked_internal"},
	}}
	findings := detectVersionScriptViolations(artifacts)
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1 (leaked_internal)", len(findings))
	}
	if findings[0].symbol != "leaked_internal" {
		t.Errorf("symbol = %q", findings[0].symbol)
	}
}

func TestDetectVersionScriptViolationsCompliant(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "test.map")
	content := `{global: foo_api; local: *;};`
	_ = os.WriteFile(mapPath, []byte(content), 0644)

	artifacts := []scanArtifact{{
		pkgName:       "libfoo",
		targetName:    "foo",
		versionScript: mapPath,
		exports:       []string{"foo_api"},
	}}
	if len(detectVersionScriptViolations(artifacts)) != 0 {
		t.Error("all exports in version-script → no findings")
	}
}

func TestDetectVersionScriptViolationsNoScript(t *testing.T) {
	artifacts := []scanArtifact{{
		pkgName:       "libfoo",
		targetName:    "foo",
		exports:       []string{"foo_api", "anything"},
		versionScript: "",
	}}
	if len(detectVersionScriptViolations(artifacts)) != 0 {
		t.Error("no version-script → no violations")
	}
}

func TestHasStrictFindings(t *testing.T) {
	tests := []struct {
		name     string
		findings []finding
		want     bool
	}{
		{"empty", nil, false},
		{"only-info", []finding{{severity: "info"}}, false},
		{"has-warn", []finding{{severity: "info"}, {severity: "warn"}}, true},
		{"has-error", []finding{{severity: "error"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasStrictFindings(tt.findings); got != tt.want {
				t.Errorf("hasStrictFindings = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadExportsFiltersCRTAndDuplicates(t *testing.T) {
	dir := t.TempDir()
	nm := filepath.Join(dir, "nm")
	script := "#!/bin/sh\ncat <<'OUT'\n0000 T _init\n0000 T real_sym\n0000 T real_sym\n0000 T _Z5hellov@GLIBC_2.2.5\n0000 A absolute_sym\n0000 T \nOUT\n"
	if err := os.WriteFile(nm, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	syms, err := readExports(nm, "artifact")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"real_sym", "_Z5hellov"}
	if !reflect.DeepEqual(syms, want) {
		t.Fatalf("readExports = %v, want %v", syms, want)
	}
}

func TestMangledReBasic(t *testing.T) {
	tests := []struct {
		sym  string
		want bool
	}{
		{"_Z5hellov", true},
		{"_ZN3foo3barC1Ev", true},
		{"normal_c_symbol", false},
		{"foo_init", false},
		{"_underscore", false},
	}
	for _, tt := range tests {
		if got := mangledRe.MatchString(tt.sym); got != tt.want {
			t.Errorf("mangledRe.Match(%q) = %v, want %v", tt.sym, got, tt.want)
		}
	}
}

func TestReportFindingsGroupsInCategoryOrder(t *testing.T) {
	findings := []finding{
		{category: "mangled-leak", subject: "app:app", symbol: "_Z5hellov", detail: "mangled"},
		{category: "blocked-write", subject: "app:app", detail: "blocked"},
		{category: "duplicate-export", subject: "app:app", symbol: "dup", detail: "a, b"},
	}
	output := captureStdout(t, func() { reportFindings(findings, 1) })

	blocked := strings.Index(output, "[blocked-write]")
	duplicate := strings.Index(output, "[duplicate-export]")
	mangled := strings.Index(output, "[mangled-leak]")
	if blocked < 0 || duplicate < 0 || mangled < 0 {
		t.Fatalf("report is missing a category group:\n%s", output)
	}
	if !(blocked < duplicate && duplicate < mangled) {
		t.Fatalf("groups are out of order (blocked=%d duplicate=%d mangled=%d):\n%s", blocked, duplicate, mangled, output)
	}
	if !strings.Contains(output, "3 finding(s) across 1 artifacts") {
		t.Fatalf("summary missing:\n%s", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	fn()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
