package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func writeNativeLibrary(t *testing.T, s *Scheduler, dir, folder, body string) {
	t.Helper()
	writeAssemblyFixture(t, dir, folder+"/value.c", "int value(void) { return "+body+"; }\n")
	for _, args := range [][]string{{s.resolvedTools.CC, "-c", "value.c", "-o", "value.o"}, {s.resolvedTools.AR, "rcs", "libvalue.a", "value.o"}} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = filepath.Join(dir, folder)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %s: %v", args[0], out, err)
		}
	}
}

func TestNativeLinkTracksExternalLibraryAndSearchPath(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "main.c", "#include <stdio.h>\nint value(void); int main(void) { printf(\"%d\\n\",value()); return 0; }\n")
	app := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c").AddLdFlags("-Lfirst", "-L", "second").AddLinks("value")
	s := nativeTestScheduler(t, dir, app)
	if err := os.Mkdir(filepath.Join(dir, "first"), 0755); err != nil {
		t.Fatal(err)
	}
	build := func(want string) {
		t.Helper()
		if err := s.BuildAll(); err != nil {
			t.Fatal(err)
		}
		if got := readNativeValue(t, s, "app"); got != want {
			t.Fatalf("linked value = %s, want %s", got, want)
		}
	}
	writeNativeLibrary(t, s, dir, "second", "1")
	build("1")
	writeNativeLibrary(t, s, dir, "second", "2")
	build("2")
	writeNativeLibrary(t, s, dir, "first", "3")
	build("3")
	if err := os.Remove(filepath.Join(dir, "first", "libvalue.a")); err != nil {
		t.Fatal(err)
	}
	build("2")
}

func TestNativeLinkTracksForwardedLibraries(t *testing.T) {
	cases := map[string][]string{
		"driver":      {"vendor/libvalue.a"},
		"forwarded":   {"-Wl,vendor/libvalue.a"},
		"separate":    {"-Xlinker", "vendor/libvalue.a"},
		"environment": nil,
	}
	if runtime.GOOS == "darwin" {
		cases["force-load"] = []string{"-Wl,-force_load,vendor/libvalue.a"}
	}
	for name, flags := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeAssemblyFixture(t, dir, "main.c", "#include <stdio.h>\nint value(void); int main(void){printf(\"%d\\n\",value());return 0;}\n")
			app := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c").AddLdFlags(flags)
			if name == "environment" {
				app.AddLinks("value")
			}
			s := nativeTestScheduler(t, dir, app)
			if name == "environment" {
				t.Setenv("LIBRARY_PATH", t.TempDir())
				s.resolvedTools.env["LIBRARY_PATH"] = filepath.Join(dir, "vendor")
			}
			for _, want := range []string{"1", "2"} {
				writeNativeLibrary(t, s, dir, "vendor", want)
				if err := s.BuildAll(); err != nil {
					t.Fatal(err)
				}
				if got := readNativeValue(t, s, "app"); got != want {
					t.Fatalf("linked %s, want %s", got, want)
				}
			}
		})
	}
}

func TestNativeCXXLinkDrivers(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "cxx/value.cpp", "#include <iostream>\nextern \"C\" void hello(void) { std::cout << 42; }\n")
	writeAssemblyFixture(t, dir, "direct.cpp", "#include <iostream>\nint main() { std::cout << 42; }\n")
	writeAssemblyFixture(t, dir, "main.c", "void hello(void); int main(void) { hello(); return 0; }\n")
	lib := makeTargetWithDeps("library").SetKind(api.TargetStatic).AddFiles("cxx/*")
	app := makeTargetWithDeps("app", "library").SetKind(api.TargetBinary).AddFiles("main.c")
	direct := makeTargetWithDeps("direct").SetKind(api.TargetBinary).AddFiles("direct.cpp")
	s := nativeTestScheduler(t, dir, lib, app, direct)
	if err := s.BuildAll(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app", "direct"} {
		if got := readNativeValue(t, s, name); got != "42" {
			t.Fatalf("%s = %q", name, got)
		}
	}
}

func TestNativeHeaderWithDollarInName(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "main.c", "#include <stdio.h>\n#include \"cost$unit.h\"\nint main(void){printf(\"%d\\n\",VALUE);return 0;}\n")
	s := nativeTestScheduler(t, dir, makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c"))
	for _, want := range []string{"12", "34"} {
		writeAssemblyFixture(t, dir, "cost$unit.h", "#define VALUE "+want+"\n")
		if err := s.BuildAll(); err != nil {
			t.Fatal(err)
		}
		if got := readNativeValue(t, s, "app"); got != want {
			t.Fatalf("included value = %s, want %s", got, want)
		}
	}
}

func TestSharedLibraryRuntimeNameIsNotAFileInput(t *testing.T) {
	var flag, name string
	switch runtime.GOOS {
	case "darwin":
		name = "@rpath/consumer-name.dylib"
		flag = "-Wl,-install_name," + name
	case "linux":
		name = "consumer-name.so"
		flag = "-Wl,-soname," + name
	default:
		t.Skip("ELF and Mach-O shared library names")
	}
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "symbol.c", "int value(void) {return 1;}\n")
	lib := makeTargetWithDeps("symbol").SetKind(api.TargetShared).AddFiles("symbol.c").AddLdFlags(flag)
	s := nativeTestScheduler(t, dir, lib)
	for _, contents := range []string{"one", "two"} {
		writeAssemblyFixture(t, dir, name, contents)
		if err := s.BuildAll(); err != nil {
			t.Fatal(err)
		}
	}
	if linked := s.Stats().Snapshot().Linked; linked != 1 {
		t.Fatalf("runtime name change caused %d links", linked)
	}
}

func TestSplitLinkFlagsAndDarwinInstallName(t *testing.T) {
	group, other := splitLinkFlags([]string{"-L", "dir", "-lfoo", "-l", "bar", "-lazy_framework", "Metal", "-lazy_library", "a.dylib"})
	if len(group) != 5 || len(other) != 4 {
		t.Fatalf("splitLinkFlags = (%v, %v)", group, other)
	}
	linker := &Linker{ccPath: "clang", cxxPath: "clang++"}
	cmd, err := linker.sharedCommand([]string{"a.o"}, []string{"-Wl,-install_name,@rpath/custom.dylib"}, "liba.dylib", LinkPolicy{TargetOS: "darwin", CXX: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Program != "clang++" {
		t.Fatalf("driver = %q, want clang++", cmd.Program)
	}
	count := 0
	for _, arg := range cmd.Args {
		if arg == "-install_name" || arg == "-Wl,-install_name,@rpath/custom.dylib" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected single install_name in %v", cmd.Args)
	}
}
