package build

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestVoidCallbackRunsEachSessionWithoutInvalidatingUnchangedNativeInputs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell compiler counter")
	}
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "main.c", "#include <stdio.h>\n#include <generated.h>\nint main(void) { printf(\"%d\\n\", GENERATED_VALUE); return 0; }\n")
	value, callbacks := 1, 0
	header := filepath.Join(dir, "include", "generated.h")
	generate := makeTargetWithDeps("generate").SetKind(api.TargetVoid).AddPublicIncludes("include").SetBuildFunc(func(*api.Package) error {
		callbacks++
		content := fmt.Sprintf("#define GENERATED_VALUE %d\n", value)
		previous, err := os.ReadFile(header)
		if err == nil && string(previous) == content {
			return nil
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		before, _ := os.Stat(header)
		if err := os.MkdirAll(filepath.Dir(header), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(header, []byte(content), 0644); err != nil {
			return err
		}
		if before != nil {
			return os.Chtimes(header, before.ModTime(), before.ModTime())
		}
		return nil
	})
	app := makeTargetWithDeps("app", "generate").SetKind(api.TargetBinary).AddFiles("main.c")
	native := nativeTestScheduler(t, dir, generate, app)
	countFile := filepath.Join(dir, "tool-calls")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	wrapper := filepath.Join(dir, "compiler.sh")
	command := "#!/bin/sh\nif [ \"$1\" != --version ]; then\n action=link\n for arg in \"$@\"; do if [ \"$arg\" = -c ]; then action=compile; fi; done\n printf '%s\\n' \"$action\" >> " + quote(countFile) + "\nfi\nexec " + quote(native.resolvedTools.CC) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(command), 0755); err != nil {
		t.Fatal(err)
	}
	tc := *native.toolchain
	tc.Tools.CC = wrapper
	dirs := map[string]*api.PkgDirs{"p": {SourceDir: dir, BuildDir: filepath.Join(dir, "build")}}
	build := func(wantCallbacks, wantActions int, wantValue string) {
		t.Helper()
		pipeline := NewBuildPipeline(native.graph, &tc, dirs, api.ModeDebug, nil, api.Platform{OS: runtime.GOOS})
		pipeline.RootDir, pipeline.Session = dir, NewSession(nil)
		scheduler, err := pipeline.Run()
		if err != nil {
			t.Fatal(err)
		}
		if callbacks != wantCallbacks {
			t.Fatalf("callback count = %d, want %d", callbacks, wantCallbacks)
		}
		calls, err := os.ReadFile(countFile)
		if err != nil {
			t.Fatal(err)
		}
		compiles, links := strings.Count(string(calls), "compile\n"), strings.Count(string(calls), "link\n")
		if compiles != wantActions || links != wantActions {
			t.Fatalf("after callback %d: compiles=%d links=%d, want %d each", callbacks, compiles, links, wantActions)
		}
		if got := readNativeValue(t, scheduler, "app"); got != wantValue {
			t.Fatalf("native output = %s, want %s", got, wantValue)
		}
	}
	build(1, 1, "1")
	build(2, 1, "1")
	value = 2
	build(3, 2, "2")
	build(4, 2, "2")
}
