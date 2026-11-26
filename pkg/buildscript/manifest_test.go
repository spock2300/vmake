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
		cid, rel, spec, ok := m.MatchModule("hl/hl-mcu-sdk/system/uniface")
		if !ok || cid != "hl/hl-mcu-sdk" || rel != "system/uniface" || spec.Version != ">=1.0.0" {
			t.Fatalf("MatchModule(native sub) = (%q, %q, %+v, %v)", cid, rel, spec, ok)
		}
		cid, rel, spec, ok = m.MatchModule("hl/hl-mcu-sdk")
		if !ok || cid != "hl/hl-mcu-sdk" || rel != "" || spec.Version != ">=1.0.0" {
			t.Fatalf("MatchModule(native root) = (%q, %q, %+v, %v)", cid, rel, spec, ok)
		}
		cid, rel, spec, ok = m.MatchModule("official/tinyexpr")
		if !ok || cid != "official" || rel != "tinyexpr" || spec.Version != "*" {
			t.Fatalf("MatchModule(registry) = (%q, %q, %+v, %v)", cid, rel, spec, ok)
		}
		if _, _, _, ok := m.MatchModule("other/pkg"); ok {
			t.Fatal("undeclared module should not match")
		}
	})

	t.Run("repos and local path modules parse and validate", func(t *testing.T) {
		dir := t.TempDir()
		content := `{
			"roots": ["//app"],
			"repos": {
				"hl": {"native": "https://atomgit.com/henlin/{name}"}
			},
			"modules": {
				"hl/sdk": {"version": ">=1.0.0"},
				"local/sdk": {"path": "./vendor/sdk"}
			}
		}`
		if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		m, _, err := LoadManifest(dir)
		if err != nil {
			t.Fatalf("LoadManifest: %v", err)
		}
		if m.Repos["hl"].Native != "https://atomgit.com/henlin/{name}" {
			t.Fatalf("repos = %+v", m.Repos)
		}
		cid, rel, spec, ok := m.MatchModule("local/sdk/core")
		if !ok || cid != "local/sdk" || rel != "core" || spec.Path != "./vendor/sdk" {
			t.Fatalf("MatchModule(path) = (%q, %q, %+v, %v)", cid, rel, spec, ok)
		}
		cid, rel, spec, ok = m.MatchModule("hl/sdk/core")
		if !ok || cid != "hl/sdk" || rel != "core" || spec.Version != ">=1.0.0" {
			t.Fatalf("MatchModule(obj version) = (%q, %q, %+v, %v)", cid, rel, spec, ok)
		}
	})

	t.Run("dev_roots used when roots omitted and validated", func(t *testing.T) {
		dir := t.TempDir()
		content := `{"dev_roots": ["//tests/target-app"]}`
		if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		m, exists, err := LoadManifest(dir)
		if err != nil || !exists {
			t.Fatalf("LoadManifest = (%+v, %v, %v)", m, exists, err)
		}
		roots, err := m.EffectiveRoots(dir)
		if err != nil || !reflect.DeepEqual(roots, []string{"//tests/target-app"}) {
			t.Fatalf("EffectiveRoots = (%v, %v), want [//tests/target-app]", roots, err)
		}
	})

	t.Run("roots takes precedence over dev_roots", func(t *testing.T) {
		dir := t.TempDir()
		content := `{"roots": ["//app"], "dev_roots": ["//tests/runner"]}`
		if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		m, _, err := LoadManifest(dir)
		if err != nil {
			t.Fatal(err)
		}
		roots, err := m.EffectiveRoots(dir)
		if err != nil || !reflect.DeepEqual(roots, []string{"//app"}) {
			t.Fatalf("EffectiveRoots = (%v, %v), want [//app]", roots, err)
		}
	})

	t.Run("exports whitelist resolution and uniqueness", func(t *testing.T) {
		dir := t.TempDir()
		content := `{
			"exports": {
				".": "//system",
				"graphics": "//sdk/graphics",
				"drivers/ek79007": "//system/driver/panel/ek79007"
			}
		}`
		if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		m, _, err := LoadManifest(dir)
		if err != nil {
			t.Fatalf("LoadManifest: %v", err)
		}
		if !m.HasExports() {
			t.Fatal("expected HasExports() == true")
		}
		if rel, ok := m.ResolveExport("graphics"); !ok || rel != "sdk/graphics" {
			t.Fatalf("ResolveExport(graphics) = (%q, %v), want (sdk/graphics, true)", rel, ok)
		}
		if rel, ok := m.ResolveExport(""); !ok || rel != "system" {
			t.Fatalf("ResolveExport(root) = (%q, %v), want (system, true)", rel, ok)
		}
		if _, ok := m.ResolveExport("sdk/graphics"); ok {
			t.Fatal("unexported raw path sdk/graphics must be rejected when exports is present")
		}
		if id := m.CanonicalPackageID("hl/sdk", "sdk/graphics"); id != "hl/sdk/graphics" {
			t.Fatalf("CanonicalPackageID(sdk/graphics) = %q, want hl/sdk/graphics", id)
		}
		if id := m.CanonicalPackageID("hl/sdk", "system"); id != "hl/sdk" {
			t.Fatalf("CanonicalPackageID(system) = %q, want hl/sdk", id)
		}
		if id := m.CanonicalPackageID("hl/sdk", "system/internal-leaf"); id != "hl/sdk/system/internal-leaf" {
			t.Fatalf("CanonicalPackageID(unexported) = %q, want hl/sdk/system/internal-leaf", id)
		}
	})

	t.Run("invalid manifest entries rejected", func(t *testing.T) {
		cases := []string{
			`{"roots": ["firmware"]}`,
			`{"roots": ["hl/sdk"]}`,
			`{"roots": ["//a", "//a"]}`,
			`{"dev_roots": ["tests"]}`,
			`{"dev_roots": ["hl/sdk"]}`,
			`{"dev_roots": ["//t", "//t"]}`,
			`{"exports": {"//bad": "//a"}}`,
			`{"exports": {"../escape": "//a"}}`,
			`{"exports": {"a:b": "//a"}}`,
			`{"exports": {"a": "not-local"}}`,
			`{"exports": {"a": "//same", "b": "//same"}}`,
			`{"modules": {"//": "*"}}`,
			`{"modules": {"hl/sdk": "not-a-constraint"}}`,
			`{"modules": {"hl/sdk": {"path": "./sdk", "version": "^1.0.0"}}}`,
			`{"modules": {"hl/sdk": {"path": "/abs/sdk"}}}`,
			`{"repos": {"hl": {"native": "https://example.com/no-placeholder"}}}`,
			`{"repos": {"bad/name": {"native": "https://example.com/{name}"}}}`,
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
	t.Run("empty exports map blocks all paths while omitted exports stays open", func(t *testing.T) {
		openDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(openDir, ManifestFileName), []byte(`{"roots":["//"]}`), 0644); err != nil {
			t.Fatal(err)
		}
		openM, _, err := LoadManifest(openDir)
		if err != nil {
			t.Fatalf("LoadManifest(open): %v", err)
		}
		if openM.HasExports() {
			t.Fatal("omitted exports must have HasExports() == false")
		}

		closedDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(closedDir, ManifestFileName), []byte(`{"exports":{}}`), 0644); err != nil {
			t.Fatal(err)
		}
		closedM, _, err := LoadManifest(closedDir)
		if err != nil {
			t.Fatalf("LoadManifest(empty exports): %v", err)
		}
		if !closedM.HasExports() {
			t.Fatal("explicit empty exports must have HasExports() == true")
		}
		if _, ok := closedM.ResolveExport("anything"); ok {
			t.Fatal("empty exports must deny all paths")
		}

		collDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(collDir, ManifestFileName), []byte(`{"exports":{"core":"//sdk/core"}}`), 0644); err != nil {
			t.Fatal(err)
		}
		collM, _, err := LoadManifest(collDir)
		if err != nil {
			t.Fatal(err)
		}
		if key, target, collides := collM.ExportKeyCollision("core"); !collides || key != "core" || target != "//sdk/core" {
			t.Fatalf("ExportKeyCollision(core) = (%q,%q,%v), want collision", key, target, collides)
		}
		if _, _, collides := collM.ExportKeyCollision("sdk/core"); collides {
			t.Fatal("exported target sdk/core must not report collision")
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
