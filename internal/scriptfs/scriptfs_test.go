package scriptfs

import (
	"io/fs"
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
		"Stat", "Lstat", "Mkdir", "MkdirAll", "MkdirTemp", "Remove", "RemoveAll", "Rename",
		"Symlink", "ReadDir", "Getwd", "Chdir", "TempDir", "CreateTemp"} {
		if _, ok := osSyms[name]; !ok {
			t.Errorf("os exports missing %s", name)
		}
	}
}

func TestReadOnlyRejectsWrites(t *testing.T) {
	base := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(base, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	keptFile := write("kept.txt", "keep")
	renamed := write("rename-old.txt", "old")
	if err := os.MkdirAll(filepath.Join(base, "kept-dir", "child"), 0755); err != nil {
		t.Fatal(err)
	}
	otherBase := t.TempDir()
	s := New(base)
	other := New(otherBase)

	SetReadOnly(true)
	defer SetReadOnly(false)

	cases := []struct {
		name string
		path string
		op   func() error
	}{
		{"WriteFile", filepath.Join(base, "out.txt"), func() error {
			return s.writeFile("out.txt", []byte("data"), 0644)
		}},
		{"Create", filepath.Join(base, "created.txt"), func() error {
			_, err := s.create("created.txt")
			return err
		}},
		{"CreateTemp", os.TempDir(), func() error {
			_, err := s.createTemp("", "vmake-readonly-*")
			return err
		}},
		{"Mkdir", filepath.Join(base, "newdir"), func() error {
			return s.mkdir("newdir", 0755)
		}},
		{"MkdirAll", filepath.Join(base, "newdir", "deep"), func() error {
			return s.mkdirAll("newdir/deep", 0755)
		}},
		{"MkdirTemp", os.TempDir(), func() error {
			_, err := s.mkdirTemp("", "vmake-readonly-dir-*")
			return err
		}},
		{"Remove", keptFile, func() error { return s.remove("kept.txt") }},
		{"RemoveAll", filepath.Join(base, "kept-dir"), func() error { return s.removeAll("kept-dir") }},
		{"Rename", renamed + " -> " + filepath.Join(base, "rename-new.txt"), func() error {
			return s.rename("rename-old.txt", "rename-new.txt")
		}},
		{"Symlink", filepath.Join(base, "link.txt"), func() error {
			return s.symlink("kept.txt", "link.txt")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.op()
			if err == nil {
				t.Fatalf("%s succeeded in read-only mode", c.name)
			}
			want := "read-only inspection: interpreted script write to " + c.path + " is not allowed"
			if err.Error() != want {
				t.Fatalf("%s error = %q, want %q", c.name, err, want)
			}
			if c.name == "CreateTemp" {
				return
			}
		})
	}
	for _, path := range []string{filepath.Join(base, "out.txt"), filepath.Join(base, "created.txt"), filepath.Join(base, "newdir")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("read-only mode created %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "rename-new.txt")); !os.IsNotExist(err) {
		t.Errorf("read-only rename created target: %v", err)
	}
	if data, err := os.ReadFile(keptFile); err != nil || string(data) != "keep" {
		t.Errorf("read-only Remove modified kept.txt: %q, %v", data, err)
	}
	if data, err := os.ReadFile(renamed); err != nil || string(data) != "old" {
		t.Errorf("read-only Rename modified the source: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(base, "kept-dir", "child")); err != nil {
		t.Errorf("read-only RemoveAll removed kept-dir: %v", err)
	}

	for _, flag := range []int{os.O_WRONLY, os.O_RDWR, os.O_CREATE, os.O_TRUNC, os.O_APPEND, os.O_WRONLY | os.O_CREATE | os.O_TRUNC} {
		if _, err := s.openFile("open-flag.txt", flag, 0644); err == nil {
			t.Errorf("OpenFile flag %#x succeeded in read-only mode", flag)
		} else if want := "read-only inspection: interpreted script write to " + filepath.Join(base, "open-flag.txt") + " is not allowed"; err.Error() != want {
			t.Errorf("OpenFile flag %#x error = %q, want %q", flag, err, want)
		}
	}
	if _, err := s.openFile("kept.txt", os.O_RDONLY, 0); err != nil {
		t.Errorf("read-only OpenFile(O_RDONLY): %v", err)
	}

	if err := other.writeFile("out.txt", []byte("data"), 0644); err == nil {
		t.Error("read-only mode is not process-wide")
	}
	if _, err := os.Stat(filepath.Join(otherBase, "out.txt")); !os.IsNotExist(err) {
		t.Errorf("process-wide read-only write created %s: %v", filepath.Join(otherBase, "out.txt"), err)
	}
}

func TestReadOnlyRecordsBlockedWrites(t *testing.T) {
	base := t.TempDir()
	s := New(base)
	SetReadOnly(true)
	defer SetReadOnly(false)
	if writes := TakeBlockedWrites(); len(writes) != 0 {
		t.Fatalf("blocked writes before any attempt = %v", writes)
	}
	err := s.writeFile("blocked.txt", []byte("data"), 0644)
	if err == nil {
		t.Fatal("writeFile succeeded in read-only mode")
	}
	writes := TakeBlockedWrites()
	if len(writes) != 1 || writes[0] != err.Error() {
		t.Fatalf("blocked writes = %v, want [%q]", writes, err)
	}
	if writes := TakeBlockedWrites(); len(writes) != 0 {
		t.Fatalf("blocked writes not drained: %v", writes)
	}
	if _, err := s.readFile("missing.txt"); err == nil {
		t.Fatal("readFile(missing) unexpectedly succeeded")
	}
	if writes := TakeBlockedWrites(); len(writes) != 0 {
		t.Fatalf("read recorded a blocked write: %v", writes)
	}
	if _, err := s.openFile("blocked-read.txt", os.O_WRONLY|os.O_CREATE, 0644); err == nil {
		t.Fatal("OpenFile write flag succeeded in read-only mode")
	}
	if writes := TakeBlockedWrites(); len(writes) != 1 {
		t.Fatalf("OpenFile blocked writes = %v, want one", writes)
	}
	SetReadOnly(false)
	if err := s.writeFile("allowed.txt", []byte("data"), 0644); err != nil {
		t.Fatalf("write after reset: %v", err)
	}
	if writes := TakeBlockedWrites(); len(writes) != 0 {
		t.Fatalf("write after reset recorded blocked writes: %v", writes)
	}
}

func TestReadOnlyAllowsReadsAndRestoresWrites(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "in.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "sub", "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}
	s := New(base)

	SetReadOnly(true)
	func() {
		defer SetReadOnly(false)
		if data, err := s.readFile("in.txt"); err != nil || string(data) != "hello" {
			t.Errorf("readFile = %q, %v", data, err)
		}
		if _, err := s.open("in.txt"); err != nil {
			t.Errorf("open: %v", err)
		}
		if info, err := s.stat("in.txt"); err != nil || info.IsDir() {
			t.Errorf("stat: %v, %v", info, err)
		}
		if info, err := s.lstat("in.txt"); err != nil || info.IsDir() {
			t.Errorf("lstat: %v, %v", info, err)
		}
		entries, err := s.readDir(".")
		if err != nil || len(entries) != 2 {
			t.Errorf("readDir(.) = %d entries, %v", len(entries), err)
		}
		walked := 0
		if err := s.walk(".", func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				walked++
			}
			return err
		}); err != nil {
			t.Errorf("walk: %v", err)
		} else if walked != 2 {
			t.Errorf("walk visited %d files, want 2", walked)
		}
		walked = 0
		if err := s.walkDir(".", func(path string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() {
				walked++
			}
			return err
		}); err != nil {
			t.Errorf("walkDir: %v", err)
		} else if walked != 2 {
			t.Errorf("walkDir visited %d files, want 2", walked)
		}
	}()

	if err := s.writeFile("out.txt", []byte("world"), 0644); err != nil {
		t.Fatalf("write after read-only restore: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(base, "out.txt")); err != nil || string(data) != "world" {
		t.Fatalf("restored write = %q, %v", data, err)
	}
}
