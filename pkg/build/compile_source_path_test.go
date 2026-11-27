package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestCompilerSourcePaths(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "package")
	out := filepath.Join(root, "objects", "file.o")
	src := filepath.Join(root, "vendor", "source file.c")
	pwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeDir, relErr := filepath.Rel(pwd, dir)
	type example struct {
		name, host, language, dir, src, want string
		clang                                bool
	}
	cases := []example{
		{"external C", "windows", "c", dir, "../vendor/source file.c", filepath.ToSlash(src), false},
		{"external C++", "windows", "cxx", dir, src, filepath.ToSlash(src), false},
		{"GCC assembly", "windows", "asm-cpp", dir, "../vendor/source file.c", filepath.ToSlash(src), false},
		{"Clang assembly", "windows", "asm-cpp", dir, src, filepath.ToSlash(src), true},
		{"leaves and reenters package", "windows", "c", dir, "../package/local.c", "local.c", false},
		{"internal source", "windows", "c", dir, filepath.Join(dir, "a.c"), "a.c", false},
		{"dot-prefixed component", "windows", "c", dir, "..vendor/a.c", "..vendor/a.c", false},
		{"Unix source", "linux", "c", dir, "../vendor/source file.c", "../vendor/source file.c", false},
	}
	if relErr == nil {
		cases = append(cases, example{"relative working directory", "windows", "c", relativeDir, "../vendor/source file.c", filepath.ToSlash(src), false})
	}
	if runtime.GOOS == "windows" {
		cases = append(cases,
			example{"another drive", "windows", "c", `C:\work\pkg`, `D:\sdk\x\..\a.c`, "D:/sdk/a.c", false},
			example{"UNC", "windows", "c", `\\server\share\work\pkg`, `..\..\sdk\a.c`, "//server/share/sdk/a.c", false})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Compiler{ccPath: "cc", cxxPath: "c++", clangCC: tc.clang, targetOS: "none"}
			opts := &CompileOptions{Language: tc.language, Includes: []string{"../public"}, rspDir: filepath.Join(dir, "rsp")}
			plan, err := c.planCompileHost(tc.host, tc.src, out, opts, tc.dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, cmd := range []commandSpec{plan.Display, plan.Commands[0]} {
				if got := cmd.Args[len(cmd.Args)-1]; got != tc.want {
					t.Fatalf("source = %q, want %q", got, tc.want)
				}
			}
			if !slices.Contains(plan.Display.Args, "-I../public") || !slices.Contains(plan.Display.Args, commandPathFor(tc.host, tc.dir, out)) {
				t.Fatalf("package-relative include or original output changed: %v", plan.Display.Args)
			}
			if tc.clang {
				as := plan.Commands[1].Args
				if got, want := as[len(as)-1], commandPathFor(tc.host, tc.dir, out+".s"); got != want {
					t.Fatalf("generated assembler input = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestExternalCompileSourceDepfile(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "package")
	if err := os.Mkdir(work, 0755); err != nil {
		t.Fatal(err)
	}
	writeAssemblyFixture(t, root, "vendor/source file.c", "#include \"value.h\"\nint probe(void) { return VALUE; }\n")
	writeAssemblyFixture(t, root, "vendor/value.h", "#define VALUE 1\n")
	tool, err := exec.LookPath("gcc")
	if err != nil {
		tool, err = exec.LookPath("arm-none-eabi-gcc")
	}
	if err != nil {
		t.Skip("GNU-compatible compiler unavailable")
	}
	bin := filepath.Dir(tool)
	ar := filepath.Join(bin, strings.Replace(filepath.Base(tool), "gcc", "ar", 1))
	if _, err := os.Stat(ar); err != nil {
		t.Skipf("archiver for %s unavailable: %v", tool, err)
	}
	src := "../vendor/source file.c"
	target := makeTargetWithDeps("driver").SetKind(api.TargetStatic).AddFiles(src).AddIncludes("../vendor")
	graph, err := NewBuildGraph(makeTargets("p", target), nil)
	if err != nil {
		t.Fatal(err)
	}
	dirs := api.PkgDirs{SourceDir: work, BuildDir: filepath.Join(work, "build")}
	tc := &toolchain.Toolchain{Name: "test", Tools: toolchain.Tools{CC: tool, AR: ar}}
	s := newScheduler(schedulerConfig{
		Graph: graph, Toolchain: tc, Tools: &ResolvedTools{CC: tool, AR: ar, env: tc.CommandEnv()},
		Layouts: map[string]*PackageLayout{"p": {Dirs: dirs, BuildKey: "fixture", Ready: true}},
		RootDir: work,
	})
	build := func(wantCompiles int) {
		t.Helper()
		if err := s.Build("p:driver"); err != nil {
			t.Fatal(err)
		}
		if got := s.stats.Snapshot().Compiled; got != wantCompiles {
			t.Fatalf("compile count = %d, want %d", got, wantCompiles)
		}
	}
	build(1)
	cmd := s.ccWriter.commands[0]
	source := filepath.Join(root, "vendor", "source file.c")
	if runtime.GOOS == "windows" && cmd.Arguments[len(cmd.Arguments)-1] != filepath.ToSlash(source) {
		t.Fatalf("source in compilation database = %v", cmd.Arguments)
	}
	output := cmd.Arguments[slices.Index(cmd.Arguments, "-o")+1]
	inputs, err := readDepInputs(resolveWorkPath(work, output+".d"))
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 || filepath.Clean(resolveWorkPath(work, inputs[0])) != source ||
		filepath.Clean(resolveWorkPath(work, inputs[1])) != filepath.Join(root, "vendor", "value.h") {
		t.Fatalf("source/header in real depfile = %q", inputs)
	}
	build(1)
	writeAssemblyFixture(t, root, "vendor/value.h", "#define VALUE 42\n")
	build(2)
	build(2)
}
