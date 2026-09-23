package build

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
)

func TestGNUResponseArguments(t *testing.T) {
	cc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("gcc unavailable")
	}
	dir := filepath.Join(t.TempDir(), "source space")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source.c"), []byte("const char *value = TEXT;\nint n = NUMBER;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	args := []string{"-c", "source.c", "-o", "output space.o", `-DTEXT="space and \\ slash"`}
	for i := 0; i < 6000; i++ {
		args = append(args, "-DNUMBER=42")
	}
	if _, err := runGNUResponse(cc, dir, args, iexec.RunInDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "output space.o")); err != nil {
		t.Fatal(err)
	}
}

func TestAssemblyIncludesInvalidateObjects(t *testing.T) {
	cc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("gcc unavailable")
	}
	for _, ext := range []string{".s", ".S"} {
		t.Run(ext, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{
				"asm.inc":      ".byte 7\n",
				"pre.h":        "#define VALUE 5\n",
				"source" + ext: ".include \"asm.inc\"\n",
			}
			if ext == ".S" {
				files["source"+ext] += "#include \"pre.h\"\n.byte VALUE\n"
			}
			for name, text := range files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0644); err != nil {
					t.Fatal(err)
				}
			}
			compiler := NewCompiler(&ResolvedTools{CC: cc})
			deps, err := compiler.Compile("source"+ext, "source.o", &CompileOptions{Language: sourceLanguage("source" + ext)}, dir)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(deps, "asm.inc") || ext == ".S" && !slices.Contains(deps, "pre.h") {
				t.Fatalf("missing include dependencies: %v", deps)
			}
			record := filepath.Join(dir, "compile.json")
			inputs := append([]string{"source" + ext}, deps...)
			if err := saveActionRecord(record, "assembly", dir, inputs, []string{"source.o"}); err != nil {
				t.Fatal(err)
			}
			if !actionUpToDate(record, "assembly", dir, inputs, []string{"source.o"}) {
				t.Fatal("unchanged object marked stale")
			}
			if err := os.WriteFile(filepath.Join(dir, "asm.inc"), []byte(".byte 8\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if actionUpToDate(record, "assembly", dir, inputs, []string{"source.o"}) {
				t.Fatal("assembly include edit did not invalidate object")
			}
		})
	}
}

func TestMissingPostLinkOutputRelinks(t *testing.T) {
	dir := t.TempDir()
	target := api.NewBuildContext("p", nil).Target("firmware").SetKind(api.TargetBinary).AddPostLinkHex().AddPostLinkBin()
	resolved := &ResolvedTarget{Node: &BuildNode{PkgName: "p", Target: target}, OutputPath: "firmware"}
	s := &Scheduler{ctx: context.Background(), linker: NewLinker(&ResolvedTools{CC: "cc", AR: "ar"}), resolvedTools: &ResolvedTools{OBJCOPY: "objcopy"}, pkgs: map[string]*PkgInfo{"p": {PkgDirs: api.PkgDirs{SourceDir: dir}}}}
	for _, file := range []string{"firmware", "firmware.hex", "firmware.bin"} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	saveTestLinkRecord(t, s, resolved, nil)
	if s.needRelink(resolved, nil) {
		t.Fatal("unchanged outputs trigger relink")
	}
	if err := os.Remove(filepath.Join(dir, "firmware.hex")); err != nil {
		t.Fatal(err)
	}
	if !s.needRelink(resolved, nil) {
		t.Fatal("missing hex does not trigger relink")
	}
}

func TestDepFileIgnoresPhonyRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "object.d")
	if err := os.WriteFile(path, []byte("o: source.c header.h\nheader.h:\n"), 0644); err != nil {
		t.Fatal(err)
	}
	deps, err := ParseDepFile(path)
	if err != nil || strings.Join(deps, ",") != "header.h" {
		t.Fatalf("deps=%v err=%v", deps, err)
	}
}

func TestSourceLanguage(t *testing.T) {
	for source, language := range map[string]string{
		"plain.s": "asm", "pre.S": "asm-cpp", "source.c": "c",
		"source.C": "cxx", "source.CPP": "cxx", "source.Cc": "cxx",
	} {
		if got := sourceLanguage(source); got != language {
			t.Errorf("%s: got %s, want %s", source, got, language)
		}
	}
}
