package plugin

import (
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/gitcmd"
	"github.com/spock2300/vmake/pkg/repo"
)

func TestExtensionRepositoriesSkipLFSSmudge(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip(err)
	}
	if output, err := exec.Command("git", gitcmd.Args("lfs", "version")...).CombinedOutput(); err != nil {
		t.Skipf("git lfs unavailable: %v\n%s", err, output)
	}
	root := t.TempDir()
	config := filepath.Join(root, "gitconfig")
	filterConfig := "[filter \"lfs\"]\n\tclean = git-lfs clean -- %f\n\tsmudge = git-lfs smudge -- %f\n\tprocess = git-lfs filter-process\n\trequired = true\n"
	if err := os.WriteFile(config, []byte(filterConfig), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("GIT_CONFIG_PARAMETERS", "")
	t.Setenv("GIT_AUTHOR_NAME", "VMake test")
	t.Setenv("GIT_AUTHOR_EMAIL", "vmake-test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "VMake test")
	t.Setenv("GIT_COMMITTER_EMAIL", "vmake-test@example.invalid")
	t.Setenv("GIT_LFS_SKIP_SMUDGE", "0")
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	managerGit(t, source, "init", "--initial-branch=main")
	writePluginFixture(t, source, map[string]string{
		".gitattributes":                     "assets/toolchains/*.bin filter=lfs diff=lfs merge=lfs -text\n",
		"README.md":                          "revision one\n",
		"assets/toolchains/linux-tool.bin":   "linux compiler payload one\n",
		"assets/toolchains/windows-tool.bin": "windows compiler payload\n",
	})
	managerGit(t, source, "add", ".")
	managerGit(t, source, "commit", "-m", "initial")
	if pointer := managerGit(t, source, "show", "HEAD:assets/toolchains/linux-tool.bin"); !strings.HasPrefix(pointer, "version https://git-lfs.github.com/spec/v1\n") {
		t.Fatalf("fixture did not create an LFS pointer: %q", pointer)
	}
	urlPath := filepath.ToSlash(source)
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	gitURL := (&url.URL{Scheme: "file", Path: urlPath}).String()
	mgr := NewManager(filepath.Join(root, "vmake"))
	if err := mgr.AddRepo("tools", gitURL); err != nil {
		t.Fatal(err)
	}
	extension := mgr.Path("tools")
	assertExtensionPointers(t, extension, "revision one\n")
	assertSkipSmudgeUnchanged(t, extension)
	ordinary := filepath.Join(root, "ordinary")
	if err := repo.Clone(gitURL, ordinary); err != nil {
		t.Fatal(err)
	}
	assertManagerFile(t, ordinary, "assets/toolchains/linux-tool.bin", "linux compiler payload one\n")
	assertManagerFile(t, ordinary, "assets/toolchains/windows-tool.bin", "windows compiler payload\n")
	writePluginFixture(t, source, map[string]string{
		"README.md":                        "revision two\n",
		"assets/toolchains/linux-tool.bin": "linux compiler payload two\n",
	})
	managerGit(t, source, "add", ".")
	managerGit(t, source, "commit", "-m", "update")
	if err := mgr.UpdateRepo("tools"); err != nil {
		t.Fatal(err)
	}
	assertExtensionPointers(t, extension, "revision two\n")
	assertSkipSmudgeUnchanged(t, extension)
	if err := repo.Pull(ordinary); err != nil {
		t.Fatal(err)
	}
	assertManagerFile(t, ordinary, "README.md", "revision two\n")
	assertManagerFile(t, ordinary, "assets/toolchains/linux-tool.bin", "linux compiler payload two\n")
	if got := os.Getenv("GIT_LFS_SKIP_SMUDGE"); got != "0" {
		t.Fatalf("ordinary repository changed process GIT_LFS_SKIP_SMUDGE to %q", got)
	}
	writePluginFixture(t, extension, map[string]string{"local.txt": "local commit\n"})
	managerGit(t, extension, "add", "local.txt")
	managerGit(t, extension, "commit", "-m", "local")
	localHead := managerGit(t, extension, "rev-parse", "HEAD")
	writePluginFixture(t, source, map[string]string{
		"README.md":      "revision three\n",
		".gitattributes": "assets/toolchains/*.bin filter=lfs diff=lfs merge=lfs -text\ncheckout.bad filter=reject\n",
		"checkout.bad":   "failed checkout fixture\n",
	})
	managerGit(t, source, "add", ".")
	managerGit(t, source, "commit", "-m", "diverge")
	if err := mgr.UpdateRepo("tools"); err == nil {
		t.Fatal("extension update merged divergent history")
	}
	if head := managerGit(t, extension, "rev-parse", "HEAD"); head != localHead {
		t.Fatalf("failed fast-forward changed HEAD from %s to %s", localHead, head)
	}
	assertExtensionPointers(t, extension, "revision two\n")
	if err := os.WriteFile(config, []byte(filterConfig+"\n[filter \"reject\"]\n\tsmudge = false\n\trequired = true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddRepo("broken", gitURL); err == nil {
		t.Fatal("extension clone succeeded despite failing checkout filter")
	}
	if _, err := os.Stat(mgr.Path("broken")); !os.IsNotExist(err) {
		t.Fatalf("failed extension clone left its destination: %v", err)
	}
	assertSkipSmudgeUnchanged(t, extension)
}

func managerGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", gitcmd.Args(args...)...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func assertManagerFile(t *testing.T, dir, name, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil || string(data) != want {
		t.Errorf("%s = %q, %v; want %q", name, data, err, want)
	}
}

func assertExtensionPointers(t *testing.T, dir, readme string) {
	t.Helper()
	assertManagerFile(t, dir, "README.md", readme)
	for _, name := range []string{"assets/toolchains/linux-tool.bin", "assets/toolchains/windows-tool.bin"} {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		pointer := managerGit(t, dir, "show", "HEAD:"+name)
		if err != nil || string(data) != pointer || !strings.HasPrefix(string(data), "version https://git-lfs.github.com/spec/v1\n") {
			t.Errorf("extension asset %s was smudged: %q, %v", name, data, err)
		}
	}
	objects := filepath.Join(dir, ".git", "lfs", "objects")
	err := filepath.WalkDir(objects, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			t.Errorf("extension downloaded LFS object %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertSkipSmudgeUnchanged(t *testing.T, dir string) {
	t.Helper()
	if got := os.Getenv("GIT_LFS_SKIP_SMUDGE"); got != "0" {
		t.Errorf("extension command changed process GIT_LFS_SKIP_SMUDGE to %q", got)
	}
	config := managerGit(t, dir, "config", "--local", "--list")
	if strings.Contains(config, "filter.lfs.") || strings.Contains(config, "skip-smudge") {
		t.Errorf("extension command persisted LFS filter configuration: %s", config)
	}
}
