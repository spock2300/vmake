package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

func TestClearRemoteInstallDirs(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "install")
	stale := filepath.Join(staging, "include", "stale.h")
	if err := os.MkdirAll(filepath.Dir(stale), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	localBuild := filepath.Join(root, "local-build")
	localFile := filepath.Join(localBuild, "keep")
	if err := os.MkdirAll(localBuild, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localFile, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	layouts := map[string]*build.PackageLayout{
		"remote": {Dirs: api.PkgDirs{InstallDir: staging}},
		"local":  {Dirs: api.PkgDirs{BuildDir: localBuild}},
		"none":   nil,
	}
	if err := clearRemoteInstallDirs(layouts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale install file survived: %v", err)
	}
	if info, err := os.Stat(staging); err != nil || !info.IsDir() {
		t.Fatalf("install staging was not recreated: %v", err)
	}
	if _, err := os.Stat(localFile); err != nil {
		t.Fatalf("local package directory was modified: %v", err)
	}
}
