package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/gitcmd"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestExtensionLFSDownloadsOnFirstBuild(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip(err)
	}
	if out, err := exec.Command(git, "lfs", "version").CombinedOutput(); err != nil {
		t.Skipf("git lfs unavailable: %v\n%s", err, out)
	}
	dir := t.TempDir()
	gitConfig := filepath.Join(dir, "gitconfig")
	writeExtensionFile(t, gitConfig, `[user]
    name = VMake Test
    email = vmake@example.invalid
[commit]
    gpgsign = false
[filter "lfs"]
    clean = git-lfs clean -- %f
    smudge = git-lfs smudge -- %f
    process = git-lfs filter-process
    required = true
`)
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("GIT_CONFIG_PARAMETERS", "")
	t.Setenv("GIT_LFS_SKIP_SMUDGE", "0")
	runGit := func(workDir string, args ...string) {
		t.Helper()
		cmd := exec.Command(git, gitcmd.Args(args...)...)
		cmd.Dir = workDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	upstream := filepath.Join(dir, "upstream with spaces")
	writeExtensionFile(t, filepath.Join(upstream, ".gitattributes"), "assets/** filter=lfs diff=lfs merge=lfs -text\n")
	runGit(upstream, "init", "--initial-branch=main")
	archives := make(map[string][]byte)
	writeArchive := func(name, payload string) toolchain.InstallConfig {
		t.Helper()
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		file, err := zw.Create("payload.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		data := append([]byte(nil), buf.Bytes()...)
		archives[name] = data
		writeExtensionFile(t, filepath.Join(upstream, "assets", "toolchains", name), string(data))
		return toolchain.InstallConfig{Method: "lfs", File: name, Format: "zip", RootDir: ".", Sha256: fmt.Sprintf("%x", sha256.Sum256(data))}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	def := toolchain.ToolchainDef{
		Name: "lazy", Version: "1",
		Tools: toolchain.Tools{CC: executable, CXX: executable, AR: executable, LD: executable},
		Installations: map[string]toolchain.InstallConfig{
			"linux/amd64":   writeArchive("linux compiler.zip", "linux version 1"),
			"windows/amd64": writeArchive("windows compiler.zip", "windows version 1"),
		},
	}
	host := runtime.GOOS + "/" + runtime.GOARCH
	if _, ok := def.Installations[host]; !ok {
		def.Installations[host] = writeArchive("host compiler.zip", "host version 1")
	}
	writeDefinition := func(def toolchain.ToolchainDef) {
		t.Helper()
		data, err := json.Marshal(def)
		if err != nil {
			t.Fatal(err)
		}
		writeExtensionFile(t, filepath.Join(upstream, def.Name, "toolchain.json"), string(data))
	}
	writeDefinition(def)
	unused := def
	unused.Name = "unused"
	unused.Installations = map[string]toolchain.InstallConfig{host: writeArchive("unused.zip", "unused")}
	writeDefinition(unused)
	writeExtensionFile(t, filepath.Join(upstream, "assets", "unrelated.dat"), "unrelated LFS resource")
	runGit(upstream, "add", ".")
	runGit(upstream, "commit", "-m", "initial toolchains")
	state := filepath.Join(dir, "state with spaces")
	remotePath := filepath.ToSlash(upstream)
	if !strings.HasPrefix(remotePath, "/") {
		remotePath = "/" + remotePath
	}
	remote := (&url.URL{Scheme: "file", Path: remotePath}).String()
	command := func(workDir string, args ...string) string {
		t.Helper()
		out, err := extensionCommand(t, state, workDir, args...)
		if err != nil {
			t.Fatalf("vmake %v: %v\n%s", args, err, out)
		}
		return out
	}
	command(dir, "ext", "add", "fixture", remote)
	clone := filepath.Join(state, "extensions", "fixture")
	assertAssets := func(materialized string, objectCount int) {
		t.Helper()
		for name := range archives {
			data, err := os.ReadFile(filepath.Join(clone, "assets", "toolchains", name))
			if err != nil {
				t.Fatal(err)
			}
			pointer := bytes.HasPrefix(data, []byte("version https://git-lfs.github.com/spec/v1"))
			if name == materialized {
				if !bytes.Equal(data, archives[name]) {
					t.Errorf("selected archive %s was not materialized", name)
				}
			} else if !pointer {
				t.Errorf("unused archive %s was materialized", name)
			}
		}
		data, err := os.ReadFile(filepath.Join(clone, "assets", "unrelated.dat"))
		if err != nil || !bytes.HasPrefix(data, []byte("version https://git-lfs.github.com/spec/v1")) {
			t.Errorf("unrelated resource was materialized: %v", err)
		}
		count := 0
		err = filepath.WalkDir(filepath.Join(clone, ".git", "lfs", "objects"), func(path string, entry fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				count++
			}
			return nil
		})
		if err != nil || count != objectCount {
			t.Errorf("LFS cache has %d objects, want %d: %v", count, objectCount, err)
		}
		if t.Failed() {
			t.FailNow()
		}
	}
	assertAssets("", 0)
	command(dir, "ext", "list")
	command(dir, "toolchain", "list")
	assertAssets("", 0)
	project := filepath.Join(dir, "project")
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
    p.SetRoot(true)
    p.OnBuild(func(ctx *api.BuildContext) { ctx.Target("ready").SetKind(api.TargetVoid) })
}
`)
	writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"version":"1","global":{"toolchain":"lazy"},"entries":{}}`)
	runGit(clone, "config", "lfs.fetchinclude", "assets/unrelated.dat")
	runGit(clone, "config", "lfs.fetchexclude", "*")
	runGit(clone, "config", "lfs.fetchrecentalways", "true")
	command(project, "build")
	assertAssets(def.Installations[host].File, 1)
	if _, err := os.Stat(filepath.Join(def.InstallDir(filepath.Join(state, "toolchains")), "payload.txt")); err != nil {
		t.Fatalf("first build did not publish toolchain: %v", err)
	}
	def.Version = "2"
	for key, install := range def.Installations {
		def.Installations[key] = writeArchive(install.File, key+" version 2")
	}
	writeDefinition(def)
	runGit(upstream, "add", ".")
	runGit(upstream, "commit", "-m", "new compiler version")
	command(dir, "ext", "update", "fixture")
	assertAssets("", 1)
	command(project, "build")
	assertAssets(def.Installations[host].File, 2)
	runGit(clone, "remote", "set-url", "origin", filepath.Join(dir, "missing-remote"))
	command(project, "build")
	assertAssets(def.Installations[host].File, 2)
}
