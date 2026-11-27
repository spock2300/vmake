package build

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResponseFileContent(t *testing.T) {
	got := formatResponseArgs([]string{"-c", `-DTEXT="a b"`, `C:\dir\file.o`})
	want := "\"-c\"\n\"-DTEXT=\\\"a b\\\"\"\n\"C:\\\\dir\\\\file.o\"\n"
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestResponseFileReuse(t *testing.T) {
	dir := t.TempDir()
	first := responseFileFor(dir, []string{"-Iinc", "-DVALUE=1"})
	second := responseFileFor(dir, []string{"-Iinc", "-DVALUE=1"})
	other := responseFileFor(dir, []string{"-Iinc", "-DVALUE=2"})
	if first.path != second.path {
		t.Fatalf("identical arguments produced different paths: %s vs %s", first.path, second.path)
	}
	if first.path == other.path {
		t.Fatalf("different arguments share a path: %s", first.path)
	}
	if err := ensureResponseFile(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(first.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != first.content {
		t.Fatalf("content = %q, want %q", data, first.content)
	}
	stamp := time.Unix(1000, 0)
	if err := os.Chtimes(first.path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := ensureResponseFile(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(first.path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(stamp) {
		t.Fatal("cache hit rewrote the response file")
	}
}

func TestResponseFileRepairsTruncatedCache(t *testing.T) {
	dir := t.TempDir()
	file := responseFileFor(dir, []string{"-Iinc"})
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file.path, []byte("short"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureResponseFile(t.Context(), file); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != file.content {
		t.Fatalf("content = %q, want %q", data, file.content)
	}
}

func TestResponseFileConcurrentEnsure(t *testing.T) {
	dir := t.TempDir()
	file := responseFileFor(dir, []string{"-Iinc", "-DVALUE=1"})
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := ensureResponseFile(t.Context(), file); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	data, err := os.ReadFile(file.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != file.content {
		t.Fatalf("content = %q, want %q", data, file.content)
	}
}

func TestCompileArgPartsKeepCommandOrder(t *testing.T) {
	opts := &CompileOptions{
		Includes: []string{"inc"},
		Defines:  []string{"A=1"},
		Language: "c",
	}
	got := buildCompileArgParts(opts, "obj.o", "src.c", []string{"-O2", "-Wall"}, "obj.o.d").args()
	want := []string{"-c", "-MMD", "-MP", "-MF", "obj.o.d", "-o", "obj.o", "-Iinc", "-DA=1", "-O2", "-Wall", "src.c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestHostLinkCommandHoistsWholeArgv(t *testing.T) {
	workDir := t.TempDir()
	rspDir := filepath.Join(workDir, "build", "state", "rsp")
	command := commandSpec{Program: "cc", Args: []string{"-o", "app", "a.o", "b.o", "-lm"}}
	hoisted := linkCommandForHost("windows", workDir, rspDir, command)
	if hoisted.response == nil || len(hoisted.Args) != 1 || !strings.HasPrefix(hoisted.Args[0], "@build/") {
		t.Fatalf("link command was not folded into a response file: %v", hoisted.Args)
	}
	for _, arg := range command.Args {
		if !strings.Contains(hoisted.response.content, arg) {
			t.Fatalf("response file lost argument %q: %q", arg, hoisted.response.content)
		}
	}
	again := linkCommandForHost("windows", workDir, rspDir, command)
	if again.response.path != hoisted.response.path {
		t.Fatal("identical link commands did not share one response file")
	}
	if inline := linkCommandForHost("linux", workDir, rspDir, command); inline.response != nil {
		t.Fatal("non-Windows hosts must keep the direct command")
	}
}

func TestHostCompileCommandHoistsSharedOptions(t *testing.T) {
	workDir := t.TempDir()
	rspDir := filepath.Join(workDir, "build", "state", "rsp")
	opts := &CompileOptions{
		Includes: []string{"include"},
		Defines:  []string{"A=1"},
		CFlags:   []string{"-O2"},
		Language: "c",
		rspDir:   rspDir,
	}
	for i := 0; i < 2400; i++ {
		opts.Defines = append(opts.Defines, fmt.Sprintf("PROBE_%d=1", i))
	}
	compiler := &Compiler{ccPath: "cc", cxxPath: "c++", targetOS: "linux"}
	first, err := compiler.planCompileHost("windows", "src/a.c", "obj/a.o", opts, workDir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Commands[0].response == nil {
		t.Fatal("shared options were not moved to a response file")
	}
	if !strings.HasPrefix(first.Commands[0].Args[len(first.Commands[0].Args)-2], "@build/") {
		t.Fatalf("response file argument missing next to the source: %v", first.Commands[0].Args)
	}
	if !strings.Contains(first.Display.Args[len(first.Display.Args)-1], "src/a.c") {
		t.Fatalf("display command lost the expanded source: %v", first.Display.Args)
	}
	if !containsArg(first.Display.Args, "-Iinclude") || !containsArg(first.Display.Args, "-DPROBE_2399=1") {
		t.Fatal("display command must stay expanded")
	}
	second, err := compiler.planCompileHost("windows", "src/b.c", "obj/b.o", opts, workDir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Commands[0].response.path != second.Commands[0].response.path {
		t.Fatal("translation units with identical options did not share one response file")
	}
}

func TestHostCompileCommandInlinesEmptyOptions(t *testing.T) {
	workDir := t.TempDir()
	opts := &CompileOptions{Language: "c", rspDir: filepath.Join(workDir, "state", "rsp")}
	compiler := &Compiler{ccPath: "cc", cxxPath: "c++", targetOS: "linux"}
	plan, err := compiler.planCompileHost("windows", "src/a.c", "obj/a.o", opts, workDir)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Commands[0].response != nil {
		t.Fatal("an empty options chunk must not create a response file")
	}
}

func TestHostCompileCommandFallsBackWhenLineIsHuge(t *testing.T) {
	workDir := t.TempDir()
	opts := &CompileOptions{Language: "c", rspDir: filepath.Join(workDir, "state", "rsp")}
	compiler := &Compiler{ccPath: "cc", cxxPath: "c++", targetOS: "linux"}
	longSource := strings.Repeat("a", safeInlineCommandUnits) + ".c"
	plan, err := compiler.planCompileHost("windows", longSource, "obj/a.o", opts, workDir)
	if err != nil {
		t.Fatal(err)
	}
	command := plan.Commands[0]
	if command.response == nil || len(command.Args) != 1 || !strings.HasPrefix(command.Args[0], "@") {
		t.Fatalf("huge command line was not folded into one response file: %v", command.Args)
	}
	if !strings.Contains(command.response.content, longSource) {
		t.Fatal("response file does not contain the source path")
	}
}

func TestCompileCommandWithoutResponseDirUsesTempCache(t *testing.T) {
	workDir := t.TempDir()
	opts := &CompileOptions{Defines: []string{"A=1"}, Language: "c"}
	compiler := &Compiler{ccPath: "cc", cxxPath: "c++", targetOS: "linux"}
	plan, err := compiler.planCompileHost("windows", "src/a.c", "obj/a.o", opts, workDir)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Commands[0].response == nil {
		t.Fatal("a direct Windows compile must keep the cached response form")
	}
	if !strings.Contains(plan.Commands[0].response.path, "vmake-rsp") {
		t.Fatalf("fallback cache path = %q", plan.Commands[0].response.path)
	}
}

func TestMixedLanguageTargetUsesDistinctResponseFiles(t *testing.T) {
	workDir := t.TempDir()
	rspDir := filepath.Join(workDir, "build", "state", "rsp")
	compiler := &Compiler{ccPath: "cc", cxxPath: "c++", targetOS: "linux"}
	cPlan, err := compiler.planCompileHost("windows", "src/a.c", "obj/a.o", &CompileOptions{CFlags: []string{"-O1"}, Language: "c", rspDir: rspDir}, workDir)
	if err != nil {
		t.Fatal(err)
	}
	cppPlan, err := compiler.planCompileHost("windows", "src/b.cpp", "obj/b.o", &CompileOptions{CxxFlags: []string{"-O2"}, Language: "cxx", rspDir: rspDir}, workDir)
	if err != nil {
		t.Fatal(err)
	}
	if cPlan.Commands[0].response == nil || cppPlan.Commands[0].response == nil {
		t.Fatal("expected response files for both languages")
	}
	if cPlan.Commands[0].response.path == cppPlan.Commands[0].response.path {
		t.Fatal("C and C++ flags must produce distinct response files")
	}
}

func TestHoistedCompileRuns(t *testing.T) {
	cc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("gcc unavailable")
	}
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, "obj"), 0755); err != nil {
		t.Fatal(err)
	}
	source := "#ifndef PROBE_0\n#error PROBE_0 missing\n#endif\nint probe(void) { return PROBE_0; }\n"
	if err := os.WriteFile(filepath.Join(workDir, "main.c"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	opts := &CompileOptions{Language: "c", rspDir: filepath.Join(workDir, "build", "state", "rsp")}
	for i := 0; i < 2400; i++ {
		opts.Defines = append(opts.Defines, fmt.Sprintf("PROBE_%d=1", i))
	}
	compiler := NewCompiler(&ResolvedTools{CC: cc})
	plan, err := compiler.planCompileHost("windows", "main.c", "obj/main.o", opts, workDir)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Commands[0].response == nil {
		t.Fatal("expected a hoisted compile command")
	}
	if _, err := compiler.ExecutePlanContext(context.Background(), plan, "main.c", "obj/main.o", workDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "obj", "main.o")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plan.Commands[0].response.path); err != nil {
		t.Fatal(err)
	}
}

func TestHoistedArchiveRuns(t *testing.T) {
	ar, err := exec.LookPath("ar")
	if err != nil {
		t.Skip("ar unavailable")
	}
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "a.o"), []byte("object"), 0644); err != nil {
		t.Fatal(err)
	}
	command := commandSpec{Program: ar, Args: []string{"rcs", "libx.a", "a.o"}}
	file := responseFileFor(filepath.Join(workDir, "state", "rsp"), command.Args)
	command = commandSpec{Program: command.Program, Args: []string{"@" + commandPath(workDir, file.path)}, response: file}
	linker := &Linker{arPath: ar, run: gnuRunner(nil)}
	if err := linker.execute(t.Context(), command, "libx.a", workDir, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "libx.a")); err != nil {
		t.Fatal(err)
	}
}

func TestHoistedCompileQuoting(t *testing.T) {
	cc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("gcc unavailable")
	}
	workDir := t.TempDir()
	includes := filepath.Join(workDir, "include space")
	if err := os.MkdirAll(includes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(includes, "value.h"), []byte("#define NUMBER 42\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source := "#include \"value.h\"\n#if NUMBER != 42\n#error NUMBER\n#endif\nconst char *text = TEXT;\nint probe(void) { return NUMBER; }\n"
	if err := os.WriteFile(filepath.Join(workDir, "main.c"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	opts := &CompileOptions{
		Includes: []string{"include space"},
		Defines:  []string{`TEXT="space and \\ slash"`, "NUMBER=42"},
		Language: "c",
		rspDir:   filepath.Join(workDir, "build", "state", "rsp"),
	}
	compiler := NewCompiler(&ResolvedTools{CC: cc})
	plan, err := compiler.planCompileHost("windows", "main.c", "obj/main.o", opts, workDir)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Commands[0].response == nil {
		t.Fatal("expected a hoisted compile command")
	}
	if _, err := compiler.ExecutePlanContext(context.Background(), plan, "main.c", "obj/main.o", workDir); err != nil {
		t.Fatal(err)
	}
}

func TestCompileCommandFormsAgree(t *testing.T) {
	cc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("gcc unavailable")
	}
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, "src space"), 0755); err != nil {
		t.Fatal(err)
	}
	source := "#ifndef GREETING\n#error GREETING missing\n#endif\nconst char *text = GREETING;\nint probe(void) { return 0; }\n"
	if err := os.WriteFile(filepath.Join(workDir, "src space", "main file.c"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	for _, hostOS := range []string{"linux", "windows"} {
		objPath := filepath.Join("obj", hostOS+".o")
		opts := &CompileOptions{
			Defines:  []string{`GREETING="你好 世界"`},
			Language: "c",
			rspDir:   filepath.Join(workDir, "build", hostOS, "state", "rsp"),
		}
		compiler := NewCompiler(&ResolvedTools{CC: cc})
		plan, err := compiler.planCompileHost(hostOS, filepath.Join("src space", "main file.c"), objPath, opts, workDir)
		if err != nil {
			t.Fatal(err)
		}
		if hostOS == "windows" && plan.Commands[0].response == nil {
			t.Fatal("windows form did not hoist the shared options")
		}
		if hostOS == "linux" && plan.Commands[0].response != nil {
			t.Fatal("linux form must stay direct")
		}
		if _, err := compiler.ExecutePlanContext(context.Background(), plan, filepath.Join("src space", "main file.c"), objPath, workDir); err != nil {
			t.Fatalf("%s form: %v", hostOS, err)
		}
	}
}

func TestCommandLineUnitsCountsUTF16(t *testing.T) {
	if got := utf16Units(strings.Repeat("中", 1000)); got != 1000 {
		t.Fatalf("CJK units = %d, want 1000", got)
	}
	if got := utf16Units("a\U0001F600"); got != 3 {
		t.Fatalf("surrogate pair units = %d, want 3", got)
	}
}

func containsArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func BenchmarkResponseFileCacheHit(b *testing.B) {
	dir := b.TempDir()
	file := responseFileFor(dir, []string{"-Iinc", "-DVALUE=1"})
	if err := ensureResponseFile(b.Context(), file); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ensureResponseFile(b.Context(), file); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResponseFileTempWrite(b *testing.B) {
	dir := b.TempDir()
	content := formatResponseArgs([]string{"-Iinc", "-DVALUE=1"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file, err := os.CreateTemp(dir, "vmake-*.rsp")
		if err != nil {
			b.Fatal(err)
		}
		if _, err := file.WriteString(content); err != nil {
			b.Fatal(err)
		}
		if err := file.Close(); err != nil {
			b.Fatal(err)
		}
		if err := os.Remove(file.Name()); err != nil {
			b.Fatal(err)
		}
	}
}
