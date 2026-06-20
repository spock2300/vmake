package build

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestComputeConfigHashEmpty(t *testing.T) {
	got, err := computeConfigHash(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("computeConfigHash: %v", err)
	}
	if got != "" {
		t.Errorf("empty config files should produce empty hash, got %q", got)
	}
}

func TestComputeConfigHashMissingFile(t *testing.T) {
	dir := t.TempDir()
	got, err := computeConfigHash(dir, []string{"missing.cfg"})
	if err != nil {
		t.Fatalf("missing file should be skipped, not error: %v", err)
	}
	// sha256 of nothing (all files skipped) is still a valid hash of empty input.
	emptyHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != emptyHash {
		t.Errorf("only-missing file should hash empty input, got %q", got)
	}
}

func TestComputeConfigHashStable(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.cfg"), []byte("content-a"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "b.cfg"), []byte("content-b"), 0644)

	h1, _ := computeConfigHash(dir, []string{"a.cfg", "b.cfg"})
	h2, _ := computeConfigHash(dir, []string{"a.cfg", "b.cfg"})
	if h1 != h2 {
		t.Errorf("config hash not stable: %q vs %q", h1, h2)
	}
	if h1 == "" {
		t.Error("config hash should be non-empty for non-empty inputs")
	}
}

func TestComputeConfigHashChangesOnContentChange(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.cfg"), []byte("v1"), 0644)
	h1, _ := computeConfigHash(dir, []string{"a.cfg"})

	_ = os.WriteFile(filepath.Join(dir, "a.cfg"), []byte("v2"), 0644)
	h2, _ := computeConfigHash(dir, []string{"a.cfg"})
	if h1 == h2 {
		t.Error("config hash should change when content changes")
	}
}

func TestIsStampUpToDateMissingStamp(t *testing.T) {
	dir := t.TempDir()
	if isStampUpToDate(filepath.Join(dir, "missing.stamp"), dir, nil) {
		t.Error("missing stamp should be not up-to-date")
	}
}

func TestIsStampUpToDateEmptyStamp(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, ".stamp")
	_ = os.WriteFile(stamp, []byte{}, 0644)
	if isStampUpToDate(stamp, dir, nil) {
		t.Error("empty stamp should be not up-to-date")
	}
}

func TestIsStampUpToDateCorruptStamp(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, ".stamp")
	_ = os.WriteFile(stamp, []byte("not json"), 0644)
	if isStampUpToDate(stamp, dir, nil) {
		t.Error("corrupt stamp should be not up-to-date")
	}
}

func TestIsStampUpToDateHashMismatch(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, ".stamp")
	_ = os.WriteFile(filepath.Join(dir, "a.cfg"), []byte("v1"), 0644)
	stampContent := `{"config_hash":"deadbeef","source_rev":""}`
	_ = os.WriteFile(stamp, []byte(stampContent), 0644)

	if isStampUpToDate(stamp, dir, []string{"a.cfg"}) {
		t.Error("hash mismatch should be not up-to-date")
	}
}

func TestIsStampUpToDateMatch(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, ".stamp")
	_ = os.WriteFile(filepath.Join(dir, "a.cfg"), []byte("content"), 0644)
	hash, _ := computeConfigHash(dir, []string{"a.cfg"})
	stampContent := `{"config_hash":"` + hash + `"}`
	_ = os.WriteFile(stamp, []byte(stampContent), 0644)

	if !isStampUpToDate(stamp, dir, []string{"a.cfg"}) {
		t.Error("matching hash should be up-to-date")
	}
}

func TestWriteStamp(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, ".stamp")
	writeStamp(stamp, stampData{ConfigHash: "abc", SourceRev: "deadbeef"})

	data, err := os.ReadFile(stamp)
	if err != nil {
		t.Fatalf("read stamp: %v", err)
	}
	if !contains(string(data), `"config_hash":"abc"`) {
		t.Errorf("stamp content = %q", string(data))
	}
	if !contains(string(data), `"source_rev":"deadbeef"`) {
		t.Errorf("stamp content = %q", string(data))
	}
}

func TestBuildStampDataNoConfigFiles(t *testing.T) {
	dir := t.TempDir()
	data := buildStampData(dir, nil)
	if data.ConfigHash != "" {
		t.Errorf("no config files should produce empty ConfigHash, got %q", data.ConfigHash)
	}
}

func TestBuildStampDataWithConfig(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.cfg"), []byte("x"), 0644)
	data := buildStampData(dir, []string{"a.cfg"})
	if data.ConfigHash == "" {
		t.Error("non-empty config should produce non-empty hash")
	}
}

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

func TestIsSourceValidMissingObj(t *testing.T) {
	dir := t.TempDir()
	valid, _ := IsSourceValid(
		filepath.Join(dir, "src.c"),
		filepath.Join(dir, "obj.o"),
	)
	if valid {
		t.Error("missing obj should be invalid (not up-to-date)")
	}
}

func TestIsSourceValidFresh(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.c")
	objPath := filepath.Join(dir, "obj.o")
	depPath := objPath + ".d"

	_ = os.WriteFile(srcPath, []byte("int main(){return 0;}"), 0644)
	_ = os.WriteFile(depPath, []byte("obj.o: src.c\n"), 0644)
	_ = os.WriteFile(objPath, []byte("obj content"), 0644)

	setMtime(t, srcPath, "2020-01-01T00:00:00Z")
	setMtime(t, objPath, "2020-01-02T00:00:00Z")

	valid, _ := IsSourceValid(srcPath, objPath)
	if !valid {
		t.Error("obj newer than src should be valid")
	}
}

func TestIsSourceValidStaleSrc(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.c")
	objPath := filepath.Join(dir, "obj.o")
	depPath := objPath + ".d"

	_ = os.WriteFile(srcPath, []byte("x"), 0644)
	_ = os.WriteFile(depPath, []byte("obj.o: src.c\n"), 0644)
	_ = os.WriteFile(objPath, []byte("obj"), 0644)

	setMtime(t, srcPath, "2020-01-02T00:00:00Z")
	setMtime(t, objPath, "2020-01-01T00:00:00Z")

	valid, _ := IsSourceValid(srcPath, objPath)
	if valid {
		t.Error("src newer than obj should be invalid")
	}
}

func TestIsSourceValidStaleDep(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.c")
	hdrPath := filepath.Join(dir, "hdr.h")
	objPath := filepath.Join(dir, "obj.o")
	depPath := objPath + ".d"

	_ = os.WriteFile(srcPath, []byte("x"), 0644)
	_ = os.WriteFile(hdrPath, []byte("y"), 0644)
	_ = os.WriteFile(depPath, []byte("obj.o: src.c hdr.h\n"), 0644)
	_ = os.WriteFile(objPath, []byte("obj"), 0644)

	setMtime(t, srcPath, "2020-01-01T00:00:00Z")
	setMtime(t, hdrPath, "2020-01-03T00:00:00Z")
	setMtime(t, objPath, "2020-01-02T00:00:00Z")

	valid, _ := IsSourceValid(srcPath, objPath)
	if valid {
		t.Error("dep newer than obj should be invalid")
	}
}

func TestIsSourceValidStaleExtraDep(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.c")
	buildGo := filepath.Join(dir, "build.go")
	objPath := filepath.Join(dir, "obj.o")
	depPath := objPath + ".d"

	_ = os.WriteFile(srcPath, []byte("x"), 0644)
	_ = os.WriteFile(buildGo, []byte("y"), 0644)
	_ = os.WriteFile(depPath, []byte("obj.o: src.c\n"), 0644)
	_ = os.WriteFile(objPath, []byte("obj"), 0644)

	setMtime(t, srcPath, "2020-01-01T00:00:00Z")
	setMtime(t, buildGo, "2020-01-03T00:00:00Z")
	setMtime(t, objPath, "2020-01-02T00:00:00Z")

	valid, _ := IsSourceValid(srcPath, objPath, buildGo)
	if valid {
		t.Error("extra dep newer than obj should be invalid")
	}
}

func TestSelectCompilerAndFlagsCxx(t *testing.T) {
	ccPath, flags := selectCompilerAndFlags(
		"cc", "cxx",
		[]string{"-gstd"}, []string{"-gxxstd"},
		[]string{"-cflag"}, []string{"-cxxflag"},
		&CompileOptions{Language: "cxx"},
	)
	if ccPath != "cxx" {
		t.Errorf("compiler = %q, want cxx", ccPath)
	}
	want := []string{"-gxxstd", "-cxxflag"}
	if !sliceEqual(flags, want) {
		t.Errorf("flags = %v, want %v", flags, want)
	}
}

func TestSelectCompilerAndFlagsC(t *testing.T) {
	ccPath, flags := selectCompilerAndFlags(
		"cc", "cxx",
		[]string{"-gstd"}, []string{"-gxxstd"},
		[]string{"-cflag"}, []string{"-cxxflag"},
		&CompileOptions{Language: "c"},
	)
	if ccPath != "cc" {
		t.Errorf("compiler = %q, want cc", ccPath)
	}
	want := []string{"-gstd", "-cflag"}
	if !sliceEqual(flags, want) {
		t.Errorf("flags = %v, want %v", flags, want)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsInner(s, substr))
}

func containsInner(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func setMtime(t *testing.T, path, ts string) {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, tm, tm); err != nil {
		t.Fatal(err)
	}
}
