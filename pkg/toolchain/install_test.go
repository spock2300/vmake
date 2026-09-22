package toolchain_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/toolchain"
)

func toolchainArchiveFixture(t *testing.T, root string) (toolchain.ToolchainDef, string, string) {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	assets := filepath.Join(repo, "assets", "toolchains")
	if err := os.MkdirAll(assets, 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(assets, "toolchain.zip"))
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	tool := "cross-tool"
	if runtime.GOOS == "windows" {
		tool += ".exe"
	}
	header := &zip.FileHeader{Name: filepath.ToSlash(filepath.Join(root, "bin", tool))}
	header.SetMode(0755)
	entry, err := w.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("tool")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	def := toolchain.ToolchainDef{
		Name: "test-installed-toolchain", Version: "1",
		Tools: toolchain.Tools{CC: "cross-tool", CXX: "cross-tool", AR: "cross-tool", LD: "cross-tool"},
		Installations: map[string]toolchain.InstallConfig{
			runtime.GOOS + "/" + runtime.GOARCH: {Method: "lfs", File: "toolchain.zip", Format: "zip", RootDir: root},
		},
	}
	return def, repo, filepath.Join(dir, "toolchains")
}

func TestAutoDownloadPublishesValidatedToolchain(t *testing.T) {
	for _, root := range []string{"upstream-root", "."} {
		t.Run(root, func(t *testing.T) {
			def, repo, toolchainsDir := toolchainArchiveFixture(t, root)
			tc, err := toolchain.Install(def, repo, toolchainsDir)
			if err != nil {
				t.Fatal(err)
			}
			if tc.InstallPath != def.InstallDir(toolchainsDir) {
				t.Fatalf("install path = %s", tc.InstallPath)
			}
			if errs := toolchain.ValidateToolchain(tc); len(errs) != 0 {
				t.Fatalf("published invalid toolchain: %v", errs)
			}
			if err := os.RemoveAll(repo); err != nil {
				t.Fatal(err)
			}
			if _, err := toolchain.Install(def, repo, toolchainsDir); err != nil {
				t.Fatalf("already installed toolchain read archive again: %v", err)
			}
		})
	}
}

func TestAutoDownloadFailureDoesNotPublish(t *testing.T) {
	for _, failure := range []string{"root_dir", "tool", "sha256"} {
		t.Run(failure, func(t *testing.T) {
			def, repo, toolchainsDir := toolchainArchiveFixture(t, ".")
			key := runtime.GOOS + "/" + runtime.GOARCH
			install := def.Installations[key]
			switch failure {
			case "root_dir":
				install.RootDir = "missing"
			case "tool":
				def.Tools.CC = "missing-tool"
			case "sha256":
				install.Sha256 = strings.Repeat("0", 64)
			}
			def.Installations[key] = install
			if _, err := toolchain.Install(def, repo, toolchainsDir); err == nil {
				t.Fatal("invalid toolchain was installed")
			}
			final := def.InstallDir(toolchainsDir)
			if _, err := os.Stat(final); !os.IsNotExist(err) {
				t.Fatalf("failed installation was published: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(final))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("failed installation left staging files: %v", entries)
			}
		})
	}
}
