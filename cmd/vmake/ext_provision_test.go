package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/toolchain"
)

func provisionToolName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func writeProvisionArchive(t *testing.T, repo, tool, payload string) {
	t.Helper()
	path := filepath.Join(repo, "assets", "toolchains", "toolchain.zip")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: filepath.ToSlash(filepath.Join("toolchain-root", "bin", tool))}
	header.SetMode(0755)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeProvisionDefinitionDir(t *testing.T, repo, dir string, def toolchain.ToolchainDef) {
	t.Helper()
	data, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	writeExtensionFile(t, filepath.Join(repo, dir, "toolchain.json"), string(data))
}

func writeProvisionDefinition(t *testing.T, repo string, def toolchain.ToolchainDef) {
	t.Helper()
	writeProvisionDefinitionDir(t, repo, def.Name, def)
}

func provisionToolchainDef(name, version, tool string, installations map[string]toolchain.InstallConfig) toolchain.ToolchainDef {
	return toolchain.ToolchainDef{
		Name: name, Version: version,
		Tools:         toolchain.Tools{CC: tool, CXX: tool, AR: tool, LD: tool},
		Installations: installations,
	}
}

func TestExtProvisionInstallsCurrentHostToolchain(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "extensions", "fixture")
	tool := provisionToolName("cross-tool")
	writeProvisionArchive(t, repo, tool, "first")
	host := runtime.GOOS + "/" + runtime.GOARCH
	def := provisionToolchainDef("fixture-tc", "1", tool, map[string]toolchain.InstallConfig{
		host: {Method: "lfs", File: "toolchain.zip", Format: "zip", RootDir: "toolchain-root"},
	})
	writeProvisionDefinition(t, repo, def)

	out, err := extensionCommand(t, dir, dir, "ext", "provision", "fixture")
	if err != nil {
		t.Fatalf("provision: %v\n%s", err, out)
	}
	installed := filepath.Join(dir, "toolchains", runtime.GOOS, runtime.GOARCH, def.Name, def.Version, "bin", tool)
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("installed tool: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Toolchain 'fixture-tc' is ready at") {
		t.Fatalf("provision output missing ready line:\n%s", out)
	}

	if err := os.RemoveAll(filepath.Join(repo, "assets")); err != nil {
		t.Fatal(err)
	}
	out, err = extensionCommand(t, dir, dir, "ext", "provision", "fixture")
	if err != nil || !strings.Contains(out, "Toolchain 'fixture-tc' is ready at") {
		t.Fatalf("second provision read the archive again: %v\n%s", err, out)
	}
}

func TestExtProvisionSkipsUnsupportedHost(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "extensions", "fixture")
	def := provisionToolchainDef("fixture-tc", "1", "cross-tool", map[string]toolchain.InstallConfig{
		"unsupported/amd64": {Method: "lfs", File: "toolchain.zip", RootDir: "."},
	})
	writeProvisionDefinition(t, repo, def)

	out, err := extensionCommand(t, dir, dir, "ext", "provision", "fixture")
	if err != nil {
		t.Fatalf("unsupported host failed provisioning: %v\n%s", err, out)
	}
	for _, want := range []string{"Skipping toolchain 'fixture-tc': toolchain fixture-tc: no installation for host", "(1 skipped)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("provision output missing %q:\n%s", want, out)
		}
	}
}

func TestExtProvisionRejectsDuplicateToolchainNames(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "extensions", "fixture")
	tool := provisionToolName("cross-tool")
	writeProvisionArchive(t, repo, tool, "payload")
	host := runtime.GOOS + "/" + runtime.GOARCH
	def := provisionToolchainDef("fixture-tc", "1", tool, map[string]toolchain.InstallConfig{
		host: {Method: "lfs", File: "toolchain.zip", Format: "zip", RootDir: "toolchain-root"},
	})
	writeProvisionDefinitionDir(t, repo, "first", def)
	writeProvisionDefinitionDir(t, repo, "second", def)

	out, err := extensionCommand(t, dir, dir, "ext", "provision", "fixture")
	if err == nil || !strings.Contains(out, "defined more than once") {
		t.Fatalf("duplicate definitions were provisioned: %v\n%s", err, out)
	}
	installed := filepath.Join(dir, "toolchains", runtime.GOOS, runtime.GOARCH, def.Name, def.Version)
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("duplicate definitions created %s: %v", installed, err)
	}
}

func TestExtProvisionSkipsManualToolchain(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "extensions", "fixture")
	def := provisionToolchainDef("fixture-tc", "", "cross-tool", nil)
	writeProvisionDefinition(t, repo, def)

	out, err := extensionCommand(t, dir, dir, "ext", "provision", "fixture")
	if err != nil {
		t.Fatalf("manual toolchain failed provisioning: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Skipping toolchain 'fixture-tc': no installations declared") {
		t.Fatalf("provision output missing manual toolchain skip:\n%s", out)
	}
}

func TestExtProvisionDoesNotExecutePlugins(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "extensions", "fixture")
	writeExtensionLoader(t, repo)
	tool := provisionToolName("cross-tool")
	writeProvisionArchive(t, repo, tool, "payload")
	host := runtime.GOOS + "/" + runtime.GOARCH
	def := provisionToolchainDef("fixture-tc", "1", tool, map[string]toolchain.InstallConfig{
		host: {Method: "lfs", File: "toolchain.zip", Format: "zip", RootDir: "toolchain-root"},
	})
	writeProvisionDefinition(t, repo, def)

	out, err := extensionCommand(t, dir, dir, "ext", "provision", "fixture")
	if err != nil {
		t.Fatalf("provision: %v\n%s", err, out)
	}
	if strings.Contains(out, "PLUGIN_EXECUTED") {
		t.Fatalf("ext provision executed a plugin:\n%s", out)
	}
}

func TestExtProvisionReportsBrokenManifest(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "extensions", "fixture")
	writeExtensionFile(t, filepath.Join(repo, "broken", "toolchain.json"), `{"name":`)

	out, err := extensionCommand(t, dir, dir, "ext", "provision", "fixture")
	if err == nil || !strings.Contains(out, "broken/toolchain.json") {
		t.Fatalf("broken manifest was not reported: %v\n%s", err, out)
	}
}

func initProvisionGitRepo(t *testing.T, repo string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init")
	runGit("add", ".")
	runGit("-c", "user.name=VMake Test", "-c", "user.email=vmake@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
}

func TestExtProvisionRequiresTrust(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "extensions", "fixture")
	tool := provisionToolName("cross-tool")
	writeProvisionArchive(t, repo, tool, "payload")
	host := runtime.GOOS + "/" + runtime.GOARCH
	def := provisionToolchainDef("fixture-tc", "1", tool, map[string]toolchain.InstallConfig{
		host: {Method: "lfs", File: "toolchain.zip", Format: "zip", RootDir: "toolchain-root"},
	})
	writeProvisionDefinition(t, repo, def)
	initProvisionGitRepo(t, repo)

	out, err := extensionCommandEnv(t, dir, dir, []string{"VMAKE_TRUST_ALL=0"}, "ext", "provision", "fixture")
	if err == nil || !strings.Contains(out, "not trusted") {
		t.Fatalf("untrusted extension was provisioned: %v\n%s", err, out)
	}
	installed := filepath.Join(dir, "toolchains", runtime.GOOS, runtime.GOARCH, def.Name, def.Version)
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("untrusted extension created %s: %v", installed, err)
	}
}

func TestExtProvisionRejectsDirtyWorktreeWithTrustAll(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "extensions", "fixture")
	tool := provisionToolName("cross-tool")
	writeProvisionArchive(t, repo, tool, "payload")
	host := runtime.GOOS + "/" + runtime.GOARCH
	def := provisionToolchainDef("fixture-tc", "1", tool, map[string]toolchain.InstallConfig{
		host: {Method: "lfs", File: "toolchain.zip", Format: "zip", RootDir: "toolchain-root"},
	})
	writeProvisionDefinition(t, repo, def)
	initProvisionGitRepo(t, repo)
	writeExtensionFile(t, filepath.Join(repo, "untracked.txt"), "dirty")

	out, err := extensionCommand(t, dir, dir, "ext", "provision", "fixture")
	if err == nil || !strings.Contains(out, "local changes") {
		t.Fatalf("dirty extension was provisioned: %v\n%s", err, out)
	}
	installed := filepath.Join(dir, "toolchains", runtime.GOOS, runtime.GOARCH, def.Name, def.Version)
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("dirty extension created %s: %v", installed, err)
	}
}
