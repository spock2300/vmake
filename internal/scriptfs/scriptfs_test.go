package scriptfs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolve(t *testing.T) {
	// "/abs/data.txt" is only absolute on Unix; Windows needs a volume.
	absPath := "/abs/data.txt"
	if runtime.GOOS == "windows" {
		absPath = `C:\abs\data.txt`
	}
	s := New("/p/pkg")
	cases := []struct{ in, want string }{
		{"data.txt", "/p/pkg/data.txt"},
		{"sub/data.txt", "/p/pkg/sub/data.txt"},
		{absPath, absPath},
		{"", ""},
		{"../x", "/p/x"},
	}
	for _, c := range cases {
		if got := s.resolve(c.in); got != filepath.FromSlash(c.want) {
			t.Errorf("resolve(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWrappedFileOpsAreScriptRelative(t *testing.T) {
	base := t.TempDir()
	os.WriteFile(filepath.Join(base, "in.txt"), []byte("hello"), 0644)
	s := New(base)

	data, err := s.readFile("in.txt")
	if err != nil || string(data) != "hello" {
		t.Fatalf("readFile = %q, %v", data, err)
	}
	if err := s.writeFile("out.txt", []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(base, "out.txt"))
	if err != nil || string(got) != "world" {
		t.Fatalf("writeFile landed at %q: %s, %v", filepath.Join(base, "out.txt"), got, err)
	}
	if _, err := s.stat("in.txt"); err != nil {
		t.Errorf("stat relative: %v", err)
	}
	entries, err := s.readDir(".")
	if err != nil || len(entries) != 2 {
		t.Errorf("readDir(.) = %d entries, %v", len(entries), err)
	}
	if err := s.mkdirAll("nested/deep", 0755); err != nil {
		t.Errorf("mkdirAll: %v", err)
	}
	if info, err := s.stat("nested/deep"); err != nil || !info.IsDir() {
		t.Errorf("mkdirAll nested stat: %v", err)
	}
}

func TestGetwdAndChdir(t *testing.T) {
	s := New("/p/pkg")
	wd, err := s.getwd()
	if err != nil || wd != "/p/pkg" {
		t.Errorf("getwd = %q, %v", wd, err)
	}
	if err := s.chdir("/tmp"); err == nil {
		t.Error("chdir should be rejected in interpreted scripts")
	}
}

func TestCommandDirDefaultsToBase(t *testing.T) {
	s := New("/p/pkg")
	cmd := s.command("echo", "hi")
	if cmd.Dir != "/p/pkg" {
		t.Errorf("cmd.Dir = %q, want %q", cmd.Dir, "/p/pkg")
	}
}

func TestWalkRootIsScriptRelative(t *testing.T) {
	base := t.TempDir()
	os.WriteFile(filepath.Join(base, "a.txt"), []byte("x"), 0644)
	os.MkdirAll(filepath.Join(base, "sub"), 0755)
	os.WriteFile(filepath.Join(base, "sub", "b.txt"), []byte("y"), 0644)
	s := New(base)

	var files []string
	err := s.walk(".", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("walk found %v, want 2 files", files)
	}
}

func TestExportsShape(t *testing.T) {
	s := New("/p")
	exports := s.Exports()
	for _, path := range []string{"os/os", "os/exec/exec", "path/filepath/filepath"} {
		if _, ok := exports[path]; !ok {
			t.Errorf("exports missing %s", path)
		}
	}
	osSyms := exports["os/os"]
	for _, name := range []string{"Open", "OpenFile", "Create", "ReadFile", "WriteFile",
		"Stat", "Lstat", "Mkdir", "MkdirAll", "Remove", "RemoveAll", "Rename",
		"ReadDir", "Getwd", "Chdir", "TempDir", "CreateTemp"} {
		if _, ok := osSyms[name]; !ok {
			t.Errorf("os exports missing %s", name)
		}
	}
}
