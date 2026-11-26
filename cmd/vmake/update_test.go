package main

import (
	"strings"
	"testing"
)

func TestPickLatestStableUsesModuleMajor(t *testing.T) {
	versions := []string{"v0.9.0", "v1.0.1", "v1.1.0", "v2.0.0", "v2.1.0", "v3.0.0", "v1.2.0-rc.1"}
	if got := pickLatestStable(versions); got != "v1.1.0" {
		t.Fatalf("pickLatestStable = %q, want v1.1.0", got)
	}
}

func TestVersionMajorCompatible(t *testing.T) {
	tests := []struct {
		major int
		want  bool
	}{
		{0, true},
		{1, true},
		{2, false},
		{3, false},
	}
	for _, tt := range tests {
		if got := versionMajorCompatible(tt.major); got != tt.want {
			t.Errorf("versionMajorCompatible(%d) = %v, want %v", tt.major, got, tt.want)
		}
	}
}

func TestValidateRequestedVersion(t *testing.T) {
	for _, version := range []string{"v0.9.0", "v1.0.91", "v1.1.0", "latest", "master"} {
		if err := validateRequestedVersion(version); err != nil {
			t.Errorf("validateRequestedVersion(%q) = %v, want nil", version, err)
		}
	}
	for _, version := range []string{"v2.0.0", "v3.0.0"} {
		err := validateRequestedVersion(version)
		if err == nil || !strings.Contains(err.Error(), "major version 1") {
			t.Errorf("validateRequestedVersion(%q) = %v, want major version error", version, err)
		}
	}
}

func TestValidateTagVersion(t *testing.T) {
	for _, version := range []string{"v0.9.0", "v1.0.0", "v1.1.0"} {
		if err := validateTagVersion(version); err != nil {
			t.Errorf("validateTagVersion(%q) = %v, want nil", version, err)
		}
	}
	for _, version := range []string{"invalid", "v2.0.0", "v3.0.0"} {
		if err := validateTagVersion(version); err == nil {
			t.Errorf("validateTagVersion(%q) = nil, want error", version)
		}
	}
}

func TestParseModuleVersions(t *testing.T) {
	got, err := parseModuleVersions(moduleRoot + " v1.0.0 v1.1.0")
	if err != nil {
		t.Fatalf("parseModuleVersions: %v", err)
	}
	if strings.Join(got, " ") != "v1.0.0 v1.1.0" {
		t.Fatalf("parseModuleVersions = %v", got)
	}
	for _, output := range []string{moduleRoot, "other/module v1.0.0", ""} {
		if _, err := parseModuleVersions(output); err == nil {
			t.Errorf("parseModuleVersions(%q) = nil error, want error", output)
		}
	}
}

func TestModuleRootMajor(t *testing.T) {
	if strings.HasSuffix(moduleRoot, "/v2") {
		t.Fatalf("moduleRoot %q unexpectedly ends with /v2", moduleRoot)
	}
	if got := moduleMajor(); got != 1 {
		t.Fatalf("moduleMajor() = %d, want 1", got)
	}
}
