package buildscript

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestLoadManifestAndEffectiveRoots(t *testing.T) {
	t.Run("missing manifest falls back to root build.go", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "build.go"), []byte("package main\n"), 0644); err != nil {
			t.Fatal(err)
		}
		m, exists, err := LoadManifest(dir)
		if err != nil || exists {
			t.Fatalf("LoadManifest = (%+v, %v, %v)", m, exists, err)
		}
		roots, err := m.EffectiveRoots(dir)
		if err != nil || !reflect.DeepEqual(roots, []string{"//"}) {
			t.Fatalf("EffectiveRoots = (%v, %v), want [//]", roots, err)
		}
	})

	t.Run("missing manifest and root build.go fails", func(t *testing.T) {
		dir := t.TempDir()
		m, _, err := LoadManifest(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.EffectiveRoots(dir); err == nil {
			t.Fatal("EffectiveRoots should fail when neither roots nor root build.go exists")
		}
	})

	t.Run("valid manifest with roots modules and sources", func(t *testing.T) {
		dir := t.TempDir()
		content := `{
			"script": {"module": "hl-demo"},
			"roots": ["//firmware"],
			"modules": {
				"hl/hl-mcu-sdk": ">=1.0.0",
				"official": "*"
			},
			"sources": {
				"hl/hl-mcu-sdk/system/freertos/lvgl": "=9.6.0",
				"official/tinyexpr": ">=1.0.0"
			}
		}`
		if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		m, exists, err := LoadManifest(dir)
		if err != nil || !exists {
			t.Fatalf("LoadManifest = (%+v, %v, %v)", m, exists, err)
		}
		roots, err := m.EffectiveRoots(dir)
		if err != nil || !reflect.DeepEqual(roots, []string{"//firmware"}) {
			t.Fatalf("EffectiveRoots = (%v, %v)", roots, err)
		}
		cid, rel, c, ok := m.MatchModule("hl/hl-mcu-sdk/system/uniface")
		if !ok || cid != "hl/hl-mcu-sdk" || rel != "system/uniface" || c != ">=1.0.0" {
			t.Fatalf("MatchModule(native sub) = (%q, %q, %q, %v)", cid, rel, c, ok)
		}
		cid, rel, c, ok = m.MatchModule("hl/hl-mcu-sdk")
		if !ok || cid != "hl/hl-mcu-sdk" || rel != "" || c != ">=1.0.0" {
			t.Fatalf("MatchModule(native root) = (%q, %q, %q, %v)", cid, rel, c, ok)
		}
		cid, rel, c, ok = m.MatchModule("official/tinyexpr")
		if !ok || cid != "official" || rel != "tinyexpr" || c != "*" {
			t.Fatalf("MatchModule(registry) = (%q, %q, %q, %v)", cid, rel, c, ok)
		}
		if _, _, _, ok := m.MatchModule("other/pkg"); ok {
			t.Fatal("undeclared module should not match")
		}
	})

	t.Run("invalid manifest entries rejected", func(t *testing.T) {
		cases := []string{
			`{"roots": ["firmware"]}`,
			`{"roots": ["hl/sdk"]}`,
			`{"roots": ["//a", "//a"]}`,
			`{"modules": {"//": "*"}}`,
			`{"modules": {"hl/sdk": "not-a-constraint"}}`,
			`{"sources": {"bare": ">=1.0.0"}}`,
		}
		for _, body := range cases {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
			if _, _, err := LoadManifest(dir); err == nil {
				t.Errorf("LoadManifest(%s) = nil error, want error", body)
			}
		}
	})
}

func TestLoadPackageSource(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "system", "uniface")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "build.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	localRoot, err := LoadPackageSource(dir, api.LocalContainerID, "", api.SourceLocal)
	if err != nil || localRoot.Name != "//" || localRoot.Container != "//" || localRoot.RelPath != "" {
		t.Fatalf("local root = %+v, err=%v", localRoot, err)
	}
	localSub, err := LoadPackageSource(dir, api.LocalContainerID, "system/uniface", api.SourceLocal)
	if err != nil || localSub.Name != "//system/uniface" || localSub.Container != "//" || localSub.RelPath != "system/uniface" {
		t.Fatalf("local sub = %+v, err=%v", localSub, err)
	}
	remoteSub, err := LoadPackageSource(dir, "hl/hl-mcu-sdk", "system/uniface", api.SourceRemote)
	if err != nil || remoteSub.Name != "hl/hl-mcu-sdk/system/uniface" || remoteSub.Container != "hl/hl-mcu-sdk" || remoteSub.RelPath != "system/uniface" {
		t.Fatalf("remote sub = %+v, err=%v", remoteSub, err)
	}
}
