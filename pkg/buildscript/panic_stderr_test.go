package buildscript

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/api"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = writer
	defer func() { os.Stderr = previous }()
	fn()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func runScratchScript(t *testing.T, script string, run func(*api.Package, string)) (string, error, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "build.go")
	if err := os.WriteFile(path, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	src := Source{Name: "panicky", Path: path, Dir: dir, Origin: api.SourceLocal}
	var runErr error
	stderr := captureStderr(t, func() {
		pkg, err := LoadBuildScript(src)
		if err != nil {
			runErr = err
			return
		}
		runErr = scriptcall.Run(func() { run(pkg, dir) })
	})
	return dir, runErr, stderr
}

func execBuildCallback(pkg *api.Package, dir string) {
	pkg.ExecBuildFuncs(dir, func(fn api.BuildFunc) {
		fn(api.NewBuildContext("panicky", nil))
	})
}

func TestScriptPanicsKeepConciseErrorWithoutInterpreterFrame(t *testing.T) {
	const prelude = `package main

import "github.com/spock2300/vmake/pkg/api"

`
	scripts := map[string]struct {
		script string
		run    func(*api.Package, string)
	}{
		"main": {
			script: prelude + `func Main(p *api.Package) {
	panic("boom")
}
`,
			run: func(*api.Package, string) {},
		},
		"config": {
			script: prelude + `func Main(p *api.Package) {
	p.OnConfig(func(ctx *api.ConfigContext) {
		panic("boom")
	})
}
`,
			run: func(pkg *api.Package, dir string) {
				pkg.ExecConfigFuncs(dir, func(fn api.ConfigFunc) {
					fn(api.NewConfigContext("panicky"))
				})
			},
		},
		"build": {
			script: prelude + `func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		panic("boom")
	})
}
`,
			run: execBuildCallback,
		},
	}
	for name, testCase := range scripts {
		t.Run(name, func(t *testing.T) {
			_, runErr, stderr := runScratchScript(t, testCase.script, testCase.run)
			if runErr == nil {
				t.Fatal("script panic was not reported as an error")
			}
			var scriptErr *api.BuildScriptError
			if !errors.As(runErr, &scriptErr) {
				t.Fatalf("script panic error = %T (%v), want *api.BuildScriptError", runErr, runErr)
			}
			if !strings.Contains(runErr.Error(), "boom") {
				t.Fatalf("script panic error lost its message: %v", runErr)
			}
			if stderr != "" {
				t.Fatalf("recovered script panic leaked interpreter output to stderr: %q", stderr)
			}
		})
	}
}

func TestScriptStderrWriteRemainsVisible(t *testing.T) {
	script := `package main

import (
	"os"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	os.Stderr.WriteString("main-stderr-marker\n")
	p.OnBuild(func(ctx *api.BuildContext) {
		os.Stderr.WriteString("onbuild-stderr-marker\n")
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
}
`
	_, runErr, stderr := runScratchScript(t, script, execBuildCallback)
	if runErr != nil {
		t.Fatalf("script stderr write failed: %v", runErr)
	}
	for _, marker := range []string{"main-stderr-marker\n", "onbuild-stderr-marker\n"} {
		if !strings.Contains(stderr, marker) {
			t.Fatalf("script stderr write %q missing from stderr: %q", marker, stderr)
		}
	}
	if strings.Contains(stderr, "panic: main.") {
		t.Fatalf("interpreter frame leaked to stderr: %q", stderr)
	}
}

func TestReadOnlyBlockedScriptCallbackDoesNotLeakInterpreterFrame(t *testing.T) {
	script := `package main

import (
	"os"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		if err := os.MkdirAll("generated", 0755); err != nil {
			panic(err)
		}
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("src/main.c")
	})
}
`
	scriptfs.SetReadOnly(true)
	t.Cleanup(func() { scriptfs.SetReadOnly(false) })
	dir, runErr, stderr := runScratchScript(t, script, execBuildCallback)
	if runErr == nil {
		t.Fatal("read-only blocked callback was not reported as an error")
	}
	if !strings.Contains(runErr.Error(), "read-only inspection:") {
		t.Fatalf("read-only error lost its diagnostic: %v", runErr)
	}
	if stderr != "" {
		t.Fatalf("read-only blocked callback leaked interpreter output to stderr: %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "generated")); !os.IsNotExist(err) {
		t.Fatalf("read-only callback created generated dir: %v", err)
	}
}
