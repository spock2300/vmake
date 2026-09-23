package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDepFile(t *testing.T) {
	dir := t.TempDir()
	depPath := filepath.Join(dir, "obj.d")
	content := "obj.o: src.c include/header.h include/other.h\n"
	_ = os.WriteFile(depPath, []byte(content), 0644)

	deps, err := ParseDepFile(depPath)
	if err != nil {
		t.Fatalf("ParseDepFile: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("deps = %v, want 2 entries (src.c dropped)", deps)
	}
	if deps[0] != "include/header.h" {
		t.Errorf("deps[0] = %q", deps[0])
	}
}

func TestParseDepFileMissing(t *testing.T) {
	_, err := ParseDepFile("/nonexistent/file.d")
	if err == nil {
		t.Error("ParseDepFile should error on missing file")
	}
}

func TestParseDepFileContinuationLines(t *testing.T) {
	dir := t.TempDir()
	depPath := filepath.Join(dir, "obj.d")
	content := `obj.o: src.c \
 include/a.h \
 include/b.h
`
	_ = os.WriteFile(depPath, []byte(content), 0644)

	deps, err := ParseDepFile(depPath)
	if err != nil {
		t.Fatalf("ParseDepFile: %v", err)
	}
	// ParseDepFile drops the first entry (the .o target name), so 3 entries → 2 returned.
	if len(deps) != 2 {
		t.Errorf("deps = %v, want 2 entries (a.h + b.h; src.c dropped as first)", deps)
	}
}

func TestParseDepFileWindowsPaths(t *testing.T) {
	dir := t.TempDir()
	depPath := filepath.Join(dir, "obj.d")
	content := `obj.o: C:\Users\build\src.c C:\Users\build\inc\a.h C:\Users\build\inc\b.h` + "\n"
	_ = os.WriteFile(depPath, []byte(content), 0644)

	deps, err := ParseDepFile(depPath)
	if err != nil {
		t.Fatalf("ParseDepFile: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("deps = %v, want 2 entries (src.c dropped)", deps)
	}
	if deps[0] != `C:\Users\build\inc\a.h` || deps[1] != `C:\Users\build\inc\b.h` {
		t.Errorf("deps = %v, want Windows separators preserved", deps)
	}
}

func TestParseDepFileEscapedSpaceAndBackslash(t *testing.T) {
	dir := t.TempDir()
	depPath := filepath.Join(dir, "obj.d")
	content := `obj.o: C:\src\main.c C:\Program\ Files\sdk\a.h C:\\double\\b.h` + "\n"
	_ = os.WriteFile(depPath, []byte(content), 0644)

	deps, err := ParseDepFile(depPath)
	if err != nil {
		t.Fatalf("ParseDepFile: %v", err)
	}
	want := []string{`C:\Program Files\sdk\a.h`, `C:\double\b.h`}
	if len(deps) != len(want) {
		t.Fatalf("deps = %v, want %v", deps, want)
	}
	for i := range want {
		if deps[i] != want[i] {
			t.Errorf("deps[%d] = %q, want %q", i, deps[i], want[i])
		}
	}
}

func TestParseDepFileCRLFContinuation(t *testing.T) {
	dir := t.TempDir()
	depPath := filepath.Join(dir, "obj.d")
	content := "obj.o: src.c \\\r\nC:\\x\\a.h\r\n"
	_ = os.WriteFile(depPath, []byte(content), 0644)

	deps, err := ParseDepFile(depPath)
	if err != nil {
		t.Fatalf("ParseDepFile: %v", err)
	}
	if len(deps) != 1 || deps[0] != `C:\x\a.h` {
		t.Errorf("deps = %v, want [C:\\x\\a.h]", deps)
	}
}

func TestSelectCompilerAndFlagsCxx(t *testing.T) {
	ccPath, flags := selectCompilerAndFlags(
		"cc", "cxx",
		[]string{"-cflag"}, []string{"-cxxflag"},
		&CompileOptions{Language: "cxx"},
	)
	if ccPath != "cxx" {
		t.Errorf("compiler = %q, want cxx", ccPath)
	}
	want := []string{"-cxxflag"}
	if !sliceEqual(flags, want) {
		t.Errorf("flags = %v, want %v", flags, want)
	}
}

func TestSelectCompilerAndFlagsC(t *testing.T) {
	ccPath, flags := selectCompilerAndFlags(
		"cc", "cxx",
		[]string{"-cflag"}, []string{"-cxxflag"},
		&CompileOptions{Language: "c"},
	)
	if ccPath != "cc" {
		t.Errorf("compiler = %q, want cc", ccPath)
	}
	want := []string{"-cflag"}
	if !sliceEqual(flags, want) {
		t.Errorf("flags = %v, want %v", flags, want)
	}
}
