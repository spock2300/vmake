package build

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func recordingLinker() (*Linker, *[]string) {
	var got []string
	l := &Linker{
		ccPath: "cc",
		arPath: "ar",
		run: func(name, dir string, args ...string) ([]byte, error) {
			got = append([]string{name}, args...)
			return nil, nil
		},
	}
	return l, &got
}

func TestLinkSharedWindowsEmitsImportLibrary(t *testing.T) {
	l, got := recordingLinker()
	dir := t.TempDir()
	output := filepath.Join(dir, "libfoo.dll")

	if err := l.LinkShared([]string{"a.o"}, nil, output, LinkPolicy{TargetOS: "windows"}, dir); err != nil {
		t.Fatalf("LinkShared: %v", err)
	}
	if !slices.Contains(*got, "-Wl,--out-implib="+output+".a") {
		t.Fatalf("args = %v, want -Wl,--out-implib=%s.a", *got, output)
	}
}

func TestLinkSharedLinuxOmitsImportLibrary(t *testing.T) {
	l, got := recordingLinker()
	dir := t.TempDir()
	output := filepath.Join(dir, "libfoo.so")

	if err := l.LinkShared([]string{"a.o"}, nil, output, LinkPolicy{TargetOS: "linux"}, dir); err != nil {
		t.Fatalf("LinkShared: %v", err)
	}
	for _, arg := range *got {
		if len(arg) > 12 && arg[:12] == "-Wl,--out-i" {
			t.Fatalf("args = %v, want no --out-implib on ELF targets", *got)
		}
	}
}

func TestLinkPolicyValidateRejectsELFFeaturesOnPE(t *testing.T) {
	pe := LinkPolicy{TargetOS: "windows"}
	if err := pe.Validate(); err != nil {
		t.Fatalf("bare PE policy must be valid, got %v", err)
	}

	elf := LinkPolicy{TargetOS: "linux", VersionScript: "v.map", SymbolBinding: "static", ExcludeLibs: []string{"libfoo"}}
	if err := elf.Validate(); err != nil {
		t.Fatalf("ELF policy must be valid, got %v", err)
	}

	for name, policy := range map[string]LinkPolicy{
		"version script": {TargetOS: "windows", VersionScript: "v.map"},
		"symbol binding": {TargetOS: "windows", SymbolBinding: "static"},
		"exclude libs":   {TargetOS: "windows", ExcludeLibs: []string{"libfoo"}},
	} {
		if err := policy.Validate(); err == nil {
			t.Errorf("%s: expected an error for a PE target", name)
		}
	}
}

func TestIsLibraryArtifactAndWholeArchiveInput(t *testing.T) {
	for _, lib := range []string{"libfoo.a", "libfoo.so", "libfoo.dylib", "libfoo.dll", "libfoo.dll.a", "foo.lib"} {
		if !isLibraryArtifact(lib) {
			t.Errorf("isLibraryArtifact(%q) = false, want true", lib)
		}
	}
	if isLibraryArtifact("foo.o") {
		t.Error("isLibraryArtifact(foo.o) = true, want false")
	}

	if wholeArchiveInput("libfoo.dll") {
		t.Error("a PE DLL must not be placed inside -Wl,--whole-archive")
	}
	if wholeArchiveInput("libfoo.dll.a") {
		t.Error("a PE import library must not be placed inside -Wl,--whole-archive: it force-imports every DLL export")
	}
	if !wholeArchiveInput("libfoo.a") {
		t.Error("a static archive belongs inside -Wl,--whole-archive")
	}
	if !wholeArchiveInput("libfoo.so") {
		t.Error("an ELF shared object is a whole-archive input")
	}
}

func TestNeedRelinkMissingImportLibrary(t *testing.T) {
	dir := t.TempDir()
	dll := filepath.Join(dir, "libfoo.dll")
	if err := os.WriteFile(dll, []byte("dll"), 0644); err != nil {
		t.Fatal(err)
	}

	target := makeTargetWithDeps("foo")
	target.SetKind(api.TargetShared)
	graph, err := NewBuildGraph(makeTargets("p", target), nil, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	node, err := graph.GetNode("p:foo")
	if err != nil {
		t.Fatal(err)
	}

	s := &Scheduler{
		graph:     graph,
		pkgs:      map[string]*PkgInfo{"p": {PkgDirs: api.PkgDirs{SourceDir: dir}}},
		toolchain: &toolchain.Toolchain{},
		platform:  api.Platform{OS: "windows"},
	}
	resolved := &ResolvedTarget{Node: node, OutputPath: dll}

	if !s.needRelink(resolved, nil) {
		t.Error("a PE shared target without its import library must relink")
	}

	implib := importLibraryPath(dll, "windows", api.TargetShared)
	if implib != dll+".a" {
		t.Fatalf("importLibraryPath = %q, want %q", implib, dll+".a")
	}
	if err := os.WriteFile(implib, []byte("implib"), 0644); err != nil {
		t.Fatal(err)
	}
	if s.needRelink(resolved, nil) {
		t.Error("a PE shared target with DLL and import library present must not relink")
	}
}
