package build

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func hasSequence(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		match := true
		for j, want := range seq {
			if args[i+j] != want {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestLinkBinaryMachOForceLoadsArchives(t *testing.T) {
	l, got := recordingLinker()
	dir := t.TempDir()
	object := filepath.Join(dir, "main.o")
	archive := filepath.Join(dir, "libdep.a")
	dylib := filepath.Join(dir, "libshared.dylib")
	output := filepath.Join(dir, "app")

	err := l.LinkBinary(
		[]string{object, archive, dylib},
		[]string{"m"},
		[]string{"-Wl,-dead_strip", "-L" + dir, "-lz"},
		output, "", LinkPolicy{TargetOS: "darwin"}, dir,
	)
	if err != nil {
		t.Fatalf("LinkBinary: %v", err)
	}
	args := *got
	for _, flag := range []string{"--start-group", "--end-group", "--whole-archive", "--no-whole-archive"} {
		if slices.Contains(args, "-Wl,"+flag) {
			t.Fatalf("args = %v, want no GNU group/whole-archive flags on Mach-O", args)
		}
	}
	if !hasSequence(args, "-Xlinker", "-force_load", "-Xlinker", archive) {
		t.Fatalf("args = %v, want -force_load %s", args, archive)
	}
	if hasSequence(args, "-Xlinker", "-force_load", "-Xlinker", dylib) {
		t.Fatalf("args = %v, want dynamic libraries passed directly, not force-loaded", args)
	}
	if !slices.Contains(args, dylib) {
		t.Fatalf("args = %v, want dylib input %s", args, dylib)
	}
	for _, want := range []string{object, "-lm", "-lz", "-L" + dir, "-Wl,-dead_strip", output} {
		if !slices.Contains(args, want) {
			t.Fatalf("args = %v, want %q", args, want)
		}
	}
	for _, rpath := range []string{dir, "@loader_path", "@loader_path/../lib"} {
		if !hasSequence(args, "-Xlinker", "-rpath", "-Xlinker", rpath) {
			t.Fatalf("args = %v, want rpath %s for in-tree and installed dylib lookup", args, rpath)
		}
	}
}

func TestLinkBinaryMachOForceLoadKeepsCommaPathsIntact(t *testing.T) {
	l, got := recordingLinker()
	dir := t.TempDir()
	archive := filepath.Join(dir, "lib,comma.a")

	if err := l.LinkBinary([]string{"a.o", archive}, nil, nil, filepath.Join(dir, "app"), "", LinkPolicy{TargetOS: "darwin"}, dir); err != nil {
		t.Fatalf("LinkBinary: %v", err)
	}
	args := *got
	if !hasSequence(args, "-Xlinker", "-force_load", "-Xlinker", archive) {
		t.Fatalf("args = %v, want the comma path kept as a single -force_load argument", args)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-Wl,-force_load") {
			t.Fatalf("args = %v, want no -Wl form that clang splits on commas", args)
		}
	}
}

func TestLinkBinaryMachOSkipsRPathsWithoutDylibs(t *testing.T) {
	l, got := recordingLinker()
	dir := t.TempDir()
	if err := l.LinkBinary([]string{filepath.Join(dir, "main.o"), filepath.Join(dir, "libdep.a")}, nil, nil, filepath.Join(dir, "app"), "", LinkPolicy{TargetOS: "darwin"}, dir); err != nil {
		t.Fatalf("LinkBinary: %v", err)
	}
	if strings.Contains(strings.Join(*got, " "), "-rpath") {
		t.Fatalf("args = %v, want no rpath entries for exclusively static inputs", *got)
	}
}

func TestLinkBinaryMachORejectsLinkerScript(t *testing.T) {
	l, _ := recordingLinker()
	dir := t.TempDir()
	err := l.LinkBinary([]string{"a.o"}, nil, nil, filepath.Join(dir, "app"), filepath.Join(dir, "link.ld"), LinkPolicy{TargetOS: "darwin"}, dir)
	if err == nil || !strings.Contains(err.Error(), "linker scripts") {
		t.Fatalf("LinkBinary with a linker script = %v, want an unsupported error", err)
	}
}

func TestLinkSharedMachOUsesDynamiclib(t *testing.T) {
	l, got := recordingLinker()
	dir := t.TempDir()
	output := filepath.Join(dir, "libfoo.dylib")

	if err := l.LinkShared([]string{"a.o"}, []string{"-pie", "-Wl,-dead_strip"}, output, LinkPolicy{TargetOS: "darwin"}, dir); err != nil {
		t.Fatalf("LinkShared: %v", err)
	}
	args := *got
	if len(args) < 2 || args[1] != "-dynamiclib" {
		t.Fatalf("args = %v, want -dynamiclib as the driver flag", args)
	}
	if !hasSequence(args, "-Xlinker", "-install_name", "-Xlinker", "@rpath/libfoo.dylib") {
		t.Fatalf("args = %v, want a relocatable @rpath install name", args)
	}
	if slices.Contains(args, "-pie") {
		t.Fatalf("args = %v, want no -pie on Mach-O shared libraries", args)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-Wl,--out-implib") {
			t.Fatalf("args = %v, want no PE import library flag on Mach-O", args)
		}
	}
	for _, want := range []string{"-o", output, "-Wl,-dead_strip"} {
		if !slices.Contains(args, want) {
			t.Fatalf("args = %v, want %q", args, want)
		}
	}
}

func TestLinkSharedMachORPathsDylibDependencies(t *testing.T) {
	l, got := recordingLinker()
	dir := t.TempDir()
	depDir := filepath.Join(dir, "other build")
	dylib := filepath.Join(depDir, "libbar.dylib")
	output := filepath.Join(dir, "libfoo.dylib")

	if err := l.LinkShared([]string{filepath.Join(dir, "a.o"), dylib}, nil, output, LinkPolicy{TargetOS: "darwin"}, dir); err != nil {
		t.Fatalf("LinkShared: %v", err)
	}
	args := *got
	for _, rpath := range []string{depDir, "@loader_path"} {
		if !hasSequence(args, "-Xlinker", "-rpath", "-Xlinker", rpath) {
			t.Fatalf("args = %v, want rpath %s", args, rpath)
		}
	}
}

func TestMachoRPathArgsDeduplicates(t *testing.T) {
	args := machoRPathArgs([]string{"/a", "/a", "", "/b"})
	want := []string{"-Xlinker", "-rpath", "-Xlinker", "/a", "-Xlinker", "-rpath", "-Xlinker", "/b"}
	if !slices.Equal(args, want) {
		t.Fatalf("machoRPathArgs = %v, want %v", args, want)
	}
}

func TestLinkPolicyValidateRejectsELFFeaturesOnMachO(t *testing.T) {
	if err := (LinkPolicy{TargetOS: "darwin"}).Validate(); err != nil {
		t.Fatalf("bare Mach-O policy must be valid, got %v", err)
	}
	elf := LinkPolicy{TargetOS: "linux", VersionScript: "v.map", SymbolBinding: "static", ExcludeLibs: []string{"libfoo"}}
	if err := elf.Validate(); err != nil {
		t.Fatalf("ELF policy must be valid, got %v", err)
	}

	for name, policy := range map[string]LinkPolicy{
		"version script": {TargetOS: "darwin", VersionScript: "v.map"},
		"symbol binding": {TargetOS: "darwin", SymbolBinding: "static"},
		"exclude libs":   {TargetOS: "darwin", ExcludeLibs: []string{"libfoo"}},
	} {
		if err := policy.Validate(); err == nil {
			t.Errorf("%s: expected an error for a Mach-O target", name)
		}
	}
}
