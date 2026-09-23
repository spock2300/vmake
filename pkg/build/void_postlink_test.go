package build

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestVoidPostLinkLifecycle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GNU ELF post-link integration requires Linux")
	}
	objcopy, err := exec.LookPath("objcopy")
	if err != nil {
		t.Skip(err)
	}
	for _, scenario := range []string{"success", "callback failure", "post-link failure", "missing output", "missing primary output"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			writeAssemblyFixture(t, dir, "main.c", "int main(void) { return 0; }\n")
			seed := filepath.Join(dir, "input.elf")
			callbackErr := errors.New("external build failed")
			calls := 0
			target := makeTargetWithDeps("firmware").SetKind(api.TargetVoid).SetBuildFunc(func(p *api.Package) error {
				calls++
				if scenario == "callback failure" {
					return callbackErr
				}
				if scenario == "missing primary output" {
					return nil
				}
				return CopyFile(seed, filepath.Join(p.BuildDir(), "firmware"))
			})
			if scenario == "missing primary output" {
				target.AddPostLinkOutputs("{output}")
			} else {
				target.AddPostLinkBin().
					AddPostLink("objcopy", "-I", "binary", "-O", "binary", "{output}.bin", "{output}.copy").
					AddPostLinkOutputs("{output}.copy")
			}
			if scenario == "post-link failure" {
				target.AddPostLink("objcopy", "--vmake-invalid-option")
			}
			if scenario == "missing output" {
				target.AddPostLinkOutputs("{output}.missing")
			}
			native := nativeTestScheduler(t, dir, target)
			cmd := exec.Command(native.resolvedTools.CC, filepath.Join(dir, "main.c"), "-o", seed)
			if data, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("fixture: %s: %v", data, err)
			}
			tc := *native.toolchain
			tc.Tools.OBJCOPY = objcopy
			buildDir := filepath.Join(dir, "build")
			pipeline := NewBuildPipeline(native.graph, &tc, map[string]*api.PkgDirs{"p": {SourceDir: dir, BuildDir: buildDir}}, api.ModeDebug, nil, api.Platform{OS: runtime.GOOS})
			pipeline.Session, pipeline.RootDir = NewSession(nil), dir
			_, err := pipeline.Run()
			if scenario != "success" {
				if err == nil {
					t.Fatal("invalid Void build succeeded")
				}
				if scenario == "callback failure" {
					if !errors.Is(err, callbackErr) {
						t.Fatalf("callback error lost: %v", err)
					}
					if _, err := os.Stat(filepath.Join(buildDir, "firmware.bin")); !os.IsNotExist(err) {
						t.Fatalf("post-link ran after callback failure: %v", err)
					}
				}
				if strings.HasPrefix(scenario, "missing") && !strings.Contains(err.Error(), "post-link output") {
					t.Fatalf("missing output not diagnosed: %v", err)
				}
				if _, err := pipeline.Run(); err == nil || calls != 1 {
					t.Fatalf("failed target retried within session: calls=%d err=%v", calls, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(buildDir, "firmware.bin")
			if data, err := os.ReadFile(bin); err != nil || len(data) == 0 {
				t.Fatalf("binary output missing: %v", err)
			}
			if !sameFileContent(bin, filepath.Join(buildDir, "firmware.copy")) {
				t.Fatal("post-link steps did not run in order")
			}
			if _, err := pipeline.Run(); err != nil || calls != 1 {
				t.Fatalf("successful target repeated within session: calls=%d err=%v", calls, err)
			}
			if err := os.Remove(bin); err != nil {
				t.Fatal(err)
			}
			pipeline.Session = NewSession(nil)
			if _, err := pipeline.Run(); err != nil || calls != 2 {
				t.Fatalf("next session did not rebuild Void target: calls=%d err=%v", calls, err)
			}
			if _, err := os.Stat(bin); err != nil {
				t.Fatalf("deleted post-link output not recovered: %v", err)
			}
		})
	}
}
