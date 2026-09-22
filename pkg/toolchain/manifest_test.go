package toolchain

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testToolchainDef() ToolchainDef {
	return ToolchainDef{
		Name: "arm-none-eabi", Version: "15.3.rel1", Prefix: "arm-none-eabi-",
		Installations: map[string]InstallConfig{
			"linux/amd64":   {Method: "lfs", File: "linux.tar.xz", RootDir: "linux-root"},
			"windows/amd64": {Method: "lfs", File: "windows.zip", RootDir: "."},
		},
	}
}

func TestInstallationSelectsHost(t *testing.T) {
	def := testToolchainDef()
	for _, test := range []struct{ host, file, root string }{
		{"linux", "linux.tar.xz", "linux-root"}, {"windows", "windows.zip", "."},
	} {
		install, err := def.installationFor(test.host, "amd64")
		if err != nil || install.File != test.file || install.RootDir != test.root {
			t.Fatalf("%s installation = %v, %v", test.host, install, err)
		}
	}
	if _, err := def.installationFor("darwin", "arm64"); err == nil || !strings.Contains(err.Error(), "darwin/arm64") {
		t.Fatalf("unsupported host error = %v", err)
	}
}

func TestLoadToolchainDefRejectsLegacyAndIncompleteDefinitions(t *testing.T) {
	for _, test := range []struct{ name, data, want string }{
		{"host", `{"name":"arm","target_os":"none","host":"arm-none-eabi"}`, "legacy field \"host\""},
		{"install", `{"name":"arm","target_os":"none","install":null}`, "legacy field \"install\""},
		{"target", `{"name":"arm","target_os":"none"}`, "build.go"},
		{"triple", `{"name":"arm","target_triple":"arm-none-eabi"}`, "build.go"},
		{"flags", `{"name":"arm","default_flags":{}}`, "build.go"},
		{"root", `{"name":"arm","version":"1","installations":{"linux/amd64":{"method":"lfs","file":"a.zip"}}}`, "root_dir"},
		{"version", `{"name":"arm","installations":{"linux/amd64":{"method":"lfs","file":"a.zip","root_dir":"."}}}`, "version"},
		{"escape", `{"name":"../arm"}`, "invalid name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "toolchain.json")
			if err := os.WriteFile(path, []byte(test.data), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadToolchainDef(path); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadToolchainDef error = %v, want %s", err, test.want)
			}
		})
	}
}

func TestToolchainInstallationIsHostIsolated(t *testing.T) {
	def := testToolchainDef()
	def.Installations[runtime.GOOS+"/"+runtime.GOARCH] = InstallConfig{Method: "lfs", File: "archive.zip", RootDir: "."}
	root := t.TempDir()
	legacy := filepath.Join(root, def.Name+"-"+def.Version)
	if err := os.MkdirAll(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	tc, err := def.ToToolchain(root)
	if err != nil || tc.InstallPath != "" {
		t.Fatalf("legacy installation accepted: %v, %v", tc, err)
	}
	want := filepath.Join(root, runtime.GOOS, runtime.GOARCH, def.Name, def.Version)
	if err := os.MkdirAll(want, 0755); err != nil {
		t.Fatal(err)
	}
	tc, err = def.ToToolchain(root)
	if err != nil || tc.InstallPath != want || tc.Prefix != "arm-none-eabi-" {
		t.Fatalf("current installation = %v, %v", tc, err)
	}
	data, err := json.Marshal(tc)
	if err != nil || strings.Contains(string(data), `"target_triple":`) || strings.Contains(string(data), `"target_os":`) {
		t.Fatalf("serialized toolchain = %s, %v", data, err)
	}
}

func TestScanRepoToolchainsReportsInvalidDefinition(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "arm")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "toolchain.json"), []byte(`{"name":"arm","host":"arm"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanRepoToolchains(root); err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("scan silently skipped invalid definition: %v", err)
	}
}

func TestDefinitionErrorUsesOnlyReliableNames(t *testing.T) {
	for _, test := range []struct{ data, name string }{
		{`{"name":"old-arm","host":"arm-none-eabi"}`, "old-arm"},
		{`{"name":"bad-schema","target_os":"none","unknown":true}`, "bad-schema"},
		{`{"name":"bad-target","target_os":"none"}`, "bad-target"},
		{`{"name":"truncated",`, ""},
		{`{"name":"../invalid","target_os":"none"}`, ""},
		{`{"name":42,"target_os":"none"}`, ""},
	} {
		path := filepath.Join(t.TempDir(), "not-a-toolchain-name", "toolchain.json")
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(test.data), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadToolchainDef(path)
		var defErr *DefinitionError
		if !errors.As(err, &defErr) || defErr.Name() != test.name || defErr.Path() != path || defErr.Unwrap() == nil {
			t.Fatalf("definition %s: error = %#v", test.data, err)
		}
	}
}

func TestLFSArchiveNamesCannotExpandPathFilters(t *testing.T) {
	for _, file := range []string{"one,two.zip", "arm*.zip", "arm?.zip", "arm[1].zip", "arm].zip", " arm.zip", "arm.zip ", "\tarm.zip", "arm.zip\n"} {
		t.Run(file, func(t *testing.T) {
			def := testToolchainDef()
			def.Installations = map[string]InstallConfig{
				"linux/amd64": {Method: "lfs", File: file, RootDir: "."},
			}
			if err := def.Validate(); err == nil || !strings.Contains(err.Error(), "LFS archive file") {
				t.Fatalf("LFS filename %q validation = %v", file, err)
			}
			def.Installations["linux/amd64"] = InstallConfig{Method: "http", File: file, RootDir: ".", URL: "https://example.invalid/archive"}
			if err := def.Validate(); err != nil {
				t.Fatalf("HTTP filename %q unexpectedly rejected: %v", file, err)
			}
		})
	}
}
