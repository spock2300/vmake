package toolchain_test

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/gitcmd"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type lfsInstallFixture struct {
	def           toolchain.ToolchainDef
	repo          string
	toolchainsDir string
	file          string
	oid           string
	otherFiles    []string
}

func runLFSGit(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	args = append([]string{"-c", "user.name=VMake Test", "-c", "user.email=vmake@example.invalid", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", gitcmd.Args(args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func writeLFSArchive(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	name := "bin/cross-tool"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	header := &zip.FileHeader{Name: name}
	header.SetMode(0755)
	entry, err := w.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func lfsFileURL(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func newLFSInstallFixture(t *testing.T) lfsInstallFixture {
	t.Helper()
	if out, err := exec.Command("git", "lfs", "version").CombinedOutput(); err != nil {
		t.Skipf("Git LFS unavailable: %v\n%s", err, out)
	}
	root := t.TempDir()
	global := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(global, nil, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("GIT_CONFIG_PARAMETERS", "")
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv("GIT_LFS_SKIP_SMUDGE", "1")
	remote := filepath.Join(root, "remote.git")
	source := filepath.Join(root, "source")
	repo := filepath.Join(root, "clone")
	runLFSGit(t, root, "init", "--bare", remote)
	runLFSGit(t, root, "init", "-b", "main", source)
	runLFSGit(t, source, "lfs", "install", "--local")
	if err := os.WriteFile(filepath.Join(source, ".gitattributes"), []byte("assets/** filter=lfs diff=lfs merge=lfs -text\n"), 0644); err != nil {
		t.Fatal(err)
	}
	selected := "assets/toolchains/selected.zip"
	otherFiles := []string{"assets/toolchains/other-host.zip", "assets/toolchains/other-toolchain.zip", "assets/unrelated.zip"}
	writeLFSArchive(t, filepath.Join(source, selected), "historical compiler")
	for _, file := range otherFiles {
		writeLFSArchive(t, filepath.Join(source, file), file)
	}
	runLFSGit(t, source, "add", ".")
	runLFSGit(t, source, "commit", "-m", "historical archives")
	runLFSGit(t, source, "branch", "recent")
	writeLFSArchive(t, filepath.Join(source, selected), "current compiler")
	runLFSGit(t, source, "add", ".")
	runLFSGit(t, source, "commit", "-m", "current compiler")
	runLFSGit(t, source, "remote", "add", "origin", lfsFileURL(remote))
	runLFSGit(t, source, "push", "origin", "main", "recent")
	runLFSGit(t, source, "lfs", "push", "--all", "origin")
	runLFSGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	runLFSGit(t, root, "clone", lfsFileURL(remote), repo)
	runLFSGit(t, repo, "lfs", "install", "--local")
	data, err := os.ReadFile(filepath.Join(source, selected))
	if err != nil {
		t.Fatal(err)
	}
	oid := fmt.Sprintf("%x", sha256.Sum256(data))
	host := runtime.GOOS + "/" + runtime.GOARCH
	otherHost := "windows/amd64"
	if host == otherHost {
		otherHost = "linux/amd64"
	}
	def := toolchain.ToolchainDef{
		Name: "selected", Version: "1",
		Tools: toolchain.Tools{CC: "cross-tool", CXX: "cross-tool", AR: "cross-tool", LD: "cross-tool"},
		Installations: map[string]toolchain.InstallConfig{
			host:      {Method: "lfs", File: "selected.zip", Format: "zip", RootDir: ".", Sha256: oid},
			otherHost: {Method: "lfs", File: "other-host.zip", Format: "zip", RootDir: "."},
		},
	}
	return lfsInstallFixture{def: def, repo: repo, toolchainsDir: filepath.Join(root, "installed"), file: selected, oid: oid, otherFiles: otherFiles}
}

func lfsCachedObjects(t *testing.T, repo string) []string {
	t.Helper()
	var objects []string
	root := filepath.Join(repo, ".git", "lfs", "objects")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			objects = append(objects, entry.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(objects)
	return objects
}

func assertNoLFSInstallation(t *testing.T, fixture lfsInstallFixture) {
	t.Helper()
	final := fixture.def.InstallDir(fixture.toolchainsDir)
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("failed installation was published: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(final))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed installation left staging files: %v, %v", entries, err)
	}
}

func TestLFSInstallDownloadsOnlySelectedHostArchive(t *testing.T) {
	f := newLFSInstallFixture(t)
	if objects := lfsCachedObjects(t, f.repo); len(objects) != 0 {
		t.Fatalf("clone downloaded LFS objects: %v", objects)
	}
	for key, value := range map[string]string{
		"lfs.fetchinclude": "assets/unrelated.zip", "lfs.fetchexclude": "*",
		"lfs.fetchrecentalways": "true", "lfs.fetchrecentcommitsdays": "30",
	} {
		runLFSGit(t, f.repo, "config", key, value)
	}
	trace := filepath.Join(t.TempDir(), "trace.json")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	if _, err := toolchain.Install(f.def, f.repo, f.toolchainsDir); err != nil {
		t.Fatal(err)
	}
	if objects := lfsCachedObjects(t, f.repo); !slices.Equal(objects, []string{f.oid}) {
		t.Fatalf("downloaded unrequested objects: %v", objects)
	}
	for _, path := range f.otherFiles {
		data, err := os.ReadFile(filepath.Join(f.repo, path))
		if err != nil || !strings.HasPrefix(string(data), "version https://git-lfs.github.com/spec/v1") {
			t.Fatalf("unrequested asset %s was materialized: %v", path, err)
		}
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	configured := false
	for _, line := range strings.Split(string(data), "\n") {
		var event struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		if json.Unmarshal([]byte(line), &event) == nil && event.Event == "start" && slices.Contains(event.Argv, "lfs.fetchrecentalways=false") {
			configured = true
		}
	}
	if !configured {
		t.Fatal("LFS pull did not explicitly disable recent fetching")
	}
	if err := os.RemoveAll(f.repo); err != nil {
		t.Fatal(err)
	}
	if _, err := toolchain.Install(f.def, f.repo, f.toolchainsDir); err != nil {
		t.Fatalf("installed toolchain required its repository: %v", err)
	}
}

func TestLFSInstallUsesCachedArchiveOffline(t *testing.T) {
	f := newLFSInstallFixture(t)
	runLFSGit(t, f.repo, "lfs", "fetch", "--include="+f.file, "--exclude=")
	runLFSGit(t, f.repo, "remote", "set-url", "origin", lfsFileURL(filepath.Join(t.TempDir(), "missing.git")))
	if _, err := toolchain.Install(f.def, f.repo, f.toolchainsDir); err != nil {
		t.Fatalf("cached archive required the remote: %v", err)
	}
	if objects := lfsCachedObjects(t, f.repo); !slices.Equal(objects, []string{f.oid}) {
		t.Fatalf("cache changed unexpectedly: %v", objects)
	}
}

func TestLFSInstallRejectsUnmaterializedArchive(t *testing.T) {
	for _, failure := range []string{"pointer", "missing", "unavailable"} {
		t.Run(failure, func(t *testing.T) {
			f := newLFSInstallFixture(t)
			want := "download toolchain archive"
			switch failure {
			case "pointer":
				data, err := os.ReadFile(filepath.Join(f.repo, f.file))
				if err != nil {
					t.Fatal(err)
				}
				data = []byte(strings.ReplaceAll(string(data), f.oid, strings.Repeat("0", 64)))
				if err := os.WriteFile(filepath.Join(f.repo, f.file), data, 0644); err != nil {
					t.Fatal(err)
				}
				want = "remains a Git LFS pointer"
			case "missing":
				key := runtime.GOOS + "/" + runtime.GOARCH
				install := f.def.Installations[key]
				install.File = "untracked.zip"
				f.def.Installations[key] = install
				want = "materialize toolchain archive"
			case "unavailable":
				runLFSGit(t, f.repo, "remote", "set-url", "origin", lfsFileURL(filepath.Join(t.TempDir(), "missing.git")))
			}
			if _, err := toolchain.Install(f.def, f.repo, f.toolchainsDir); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("unmaterialized archive error = %v, want %q", err, want)
			}
			assertNoLFSInstallation(t, f)
		})
	}
}
