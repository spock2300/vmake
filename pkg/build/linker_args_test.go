package build

import (
	"slices"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestLinkBinaryELFArgv(t *testing.T) {
	l, got := recordingLinker()
	dir := t.TempDir()
	objs := []string{"a.o", "libfoo.a", "libbar.so", "libbaz.dll.a"}
	ldflags := []string{"-Wl,-z,now", "-L/opt/lib", "-lz", "-pthread"}

	err := l.LinkBinary(
		objs,
		[]string{"m"},
		ldflags,
		"out/app",
		"link.ld",
		LinkPolicy{
			TargetOS:      "linux",
			VersionScript: "v.map",
			ExcludeLibs:   []string{"libfoo"},
			SymbolBinding: "static",
		},
		dir,
	)
	if err != nil {
		t.Fatalf("LinkBinary: %v", err)
	}

	want := []string{
		"cc",
		"-o", "out/app",
		"-T", "link.ld",
		"-Wl,--version-script=v.map",
		"-Wl,--exclude-libs=libfoo",
		"-Wl,--start-group",
		"a.o",
		"libbaz.dll.a",
		"-Wl,--whole-archive",
		"libfoo.a",
		"libbar.so",
		"-Wl,--no-whole-archive",
		"-lm",
		"-L/opt/lib",
		"-lz",
		"-Wl,--end-group",
		"-Wl,-z,now",
		"-pthread",
		"-Wl,-Bsymbolic",
	}
	if !slices.Equal(*got, want) {
		t.Fatalf("argv = %#v\nwant     %#v", *got, want)
	}
	if !slices.Equal(objs, []string{"a.o", "libfoo.a", "libbar.so", "libbaz.dll.a"}) {
		t.Errorf("LinkBinary modified objs: %#v", objs)
	}
	if !slices.Equal(ldflags, []string{"-Wl,-z,now", "-L/opt/lib", "-lz", "-pthread"}) {
		t.Errorf("LinkBinary modified ldflags: %#v", ldflags)
	}
}

func TestPlanLinkBinaryArgv(t *testing.T) {
	target := makeTargetWithDeps("app").SetKind(api.TargetBinary)
	s, graph := newTestScheduler(t, makeTargets("p", target))
	s.platform = api.Platform{OS: "linux"}
	s.linker = &Linker{ccPath: "cc", arPath: "ar", run: func(string, string, ...string) ([]byte, error) { return nil, nil }}
	node, err := graph.GetNode("p:app")
	if err != nil {
		t.Fatal(err)
	}
	action, err := s.planLink(&ResolvedTarget{
		Node:          node,
		AllLinks:      []string{"m"},
		AllLdFlags:    []string{"-Wl,-z,now", "-L/opt/lib", "-lz", "-pthread"},
		OutputPath:    "out/app",
		VersionScript: "v.map",
		LinkerScript:  "link.ld",
		ExcludeLibs:   []string{"libfoo"},
		SymbolBinding: "static",
	}, []string{"a.o", "libfoo.a", "libbar.so", "libbaz.dll.a"})
	if err != nil {
		t.Fatalf("planLink: %v", err)
	}
	want := []string{
		"-o", "out/app",
		"-T", "link.ld",
		"-Wl,--version-script=v.map",
		"-Wl,--exclude-libs=libfoo",
		"-Wl,--start-group",
		"a.o",
		"libbaz.dll.a",
		"-Wl,--whole-archive",
		"libfoo.a",
		"libbar.so",
		"-Wl,--no-whole-archive",
		"-lm",
		"-L/opt/lib",
		"-lz",
		"-Wl,--end-group",
		"-Wl,-z,now",
		"-pthread",
		"-Wl,-Bsymbolic",
	}
	if !slices.Equal(action.command.Args, want) {
		t.Fatalf("planLink args = %#v\nwant          %#v", action.command.Args, want)
	}
}

func TestLinkBinaryMachOArgv(t *testing.T) {
	l, got := recordingLinker()
	dir := t.TempDir()
	object := "main.o"
	archive := "libdep.a"
	dylib := "libshared.dylib"
	output := "app"

	err := l.LinkBinary(
		[]string{object, archive, dylib},
		[]string{"m"},
		[]string{"-Wl,-dead_strip", "-L.", "-lz"},
		output, "", LinkPolicy{TargetOS: "darwin"}, dir,
	)
	if err != nil {
		t.Fatalf("LinkBinary: %v", err)
	}

	want := []string{
		"cc",
		"-o", output,
		object,
		"-Xlinker", "-force_load", "-Xlinker", archive,
		dylib,
		"-Xlinker", "-rpath", "-Xlinker", ".",
		"-Xlinker", "-rpath", "-Xlinker", "@loader_path",
		"-Xlinker", "-rpath", "-Xlinker", "@loader_path/../lib",
		"-lm",
		"-L.",
		"-lz",
		"-Wl,-dead_strip",
	}
	if !slices.Equal(*got, want) {
		t.Fatalf("argv = %#v\nwant     %#v", *got, want)
	}
}
