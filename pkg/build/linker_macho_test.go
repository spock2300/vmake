package build

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

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
	if !slices.Contains(args, "-Wl,-force_load,"+archive) {
		t.Fatalf("args = %v, want -Wl,-force_load,%s", args, archive)
	}
	if slices.Contains(args, "-Wl,-force_load,"+dylib) {
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
