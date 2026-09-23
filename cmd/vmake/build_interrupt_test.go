//go:build !windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spock2300/vmake/internal/flock"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestBuildInterruptReapsProcessesAndAllowsRetry(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip(err)
	}
	ar, err := exec.LookPath("ar")
	if err != nil {
		t.Skip(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"compile", "link", "postlink", "helper"} {
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
			t.Run(stage+"/"+sig.String(), func(t *testing.T) {
				dir := t.TempDir()
				home := filepath.Join(dir, "home")
				project := filepath.Join(dir, "project")
				writeExtensionFile(t, filepath.Join(project, "block"), "block")
				wrapper := filepath.Join(dir, "compiler.sh")
				body := `#!/bin/sh
stage=link
case "$1" in --version) exec REAL "$@";; esac
for arg in "$@"; do
 if [ "$arg" = "-c" ]; then stage=compile; fi
done
if [ "$stage" = "$VMAKE_INTERRUPT_STAGE" ] && [ -f "$VMAKE_INTERRUPT_PROJECT/block" ]; then
 echo $$ > "$VMAKE_INTERRUPT_PROJECT/ready.$$"
 /bin/sh -c 'sleep 2; echo late > "$VMAKE_INTERRUPT_PROJECT/late"' &
 wait
fi
exec REAL "$@"
`
				body = strings.ReplaceAll(body, "REAL", "'"+strings.ReplaceAll(cc, "'", "'\\''")+"'")
				if err := os.WriteFile(wrapper, []byte(body), 0755); err != nil {
					t.Fatal(err)
				}
				worker := filepath.Join(dir, "worker.sh")
				workerBody := `#!/bin/sh
if [ -f "$VMAKE_INTERRUPT_PROJECT/block" ]; then
 echo $$ > "$VMAKE_INTERRUPT_PROJECT/ready.$$"
 /bin/sh -c 'sleep 2; echo late > "$VMAKE_INTERRUPT_PROJECT/late"' &
 wait
fi
`
				if err := os.WriteFile(worker, []byte(workerBody), 0755); err != nil {
					t.Fatal(err)
				}
				def := toolchain.ToolchainDef{Name: "interrupt", Tools: toolchain.Tools{CC: wrapper, CXX: wrapper, AR: ar, LD: wrapper, SIZE: worker}}
				data, err := json.Marshal(def)
				if err != nil {
					t.Fatal(err)
				}
				writeExtensionFile(t, filepath.Join(home, "extensions", "fixture", "compiler", "toolchain.json"), string(data))
				post := ""
				if stage == "postlink" {
					post = "target.AddPostLinkSize()"
				}
				target := `target := ctx.Target("a").SetKind(api.TargetBinary).AddFiles("main.c", "one.c", "two.c"); _ = target`
				if stage == "helper" {
					target = fmt.Sprintf(`target := ctx.Target("a").SetKind(api.TargetVoid).SetBuildFunc(func(p *api.Package) error { p.Run(%q); return nil }); _ = target`, worker)
				}
				script := fmt.Sprintf(`package main
import ("os"; "github.com/spock2300/vmake/pkg/api")
func Main(p *api.Package) {
 p.SetRoot(true)
 p.OnBuild(func(ctx *api.BuildContext) {
  %s
  %s
  ctx.Target("z").SetKind(api.TargetVoid).SetBuildFunc(func(pkg *api.Package) error { return os.WriteFile("later",[]byte("ran"),0644) })
 })
 p.OnInstall(func(ctx *api.InstallContext) { if err:=os.WriteFile("installed-hook",[]byte("ran"),0644);err!=nil {panic(err)} })
}`, target, post)
				writeExtensionFile(t, filepath.Join(project, "build.go"), script)
				writeExtensionFile(t, filepath.Join(project, "main.c"), "int one(void); int two(void); int main(void) { return one()+two()-3; }\n")
				writeExtensionFile(t, filepath.Join(project, "one.c"), "int one(void) { return 1; }\n")
				writeExtensionFile(t, filepath.Join(project, "two.c"), "int two(void) { return 2; }\n")
				writeExtensionFile(t, filepath.Join(project, ".vmake", "config.json"), `{"version":"1","global":{"toolchain":"interrupt"},"entries":{}}`)
				args := []string{"-test.run=^TestExtensionBootstrapChild$", "--", "build", "-i", "-k", "-j2"}
				env := append(os.Environ(), "VMAKE_TEST_EXT_DIR="+home, "VMAKE_CACHE="+filepath.Join(dir, "cache"), "VMAKE_TRUST_ALL=1", "VMAKE_INTERRUPT_PROJECT="+project, "VMAKE_INTERRUPT_STAGE="+stage)
				cmd := exec.Command(exe, args...)
				cmd.Dir = project
				cmd.Env = env
				cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				var out bytes.Buffer
				cmd.Stdout = &out
				cmd.Stderr = &out
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { done <- cmd.Wait() }()
				var pids []int
				t.Cleanup(func() {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					for _, pid := range pids {
						_ = syscall.Kill(-pid, syscall.SIGKILL)
					}
				})
				expected := 1
				if stage == "compile" {
					expected = 2
				}
				deadline := time.Now().Add(20 * time.Second)
				for len(pids) < expected && time.Now().Before(deadline) {
					select {
					case err := <-done:
						t.Fatalf("build exited before ready: %v\n%s", err, out.String())
					default:
					}
					files, _ := filepath.Glob(filepath.Join(project, "ready.*"))
					pids = nil
					for _, file := range files {
						data, _ := os.ReadFile(file)
						pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
						if pid > 0 {
							pids = append(pids, pid)
						}
					}
					time.Sleep(10 * time.Millisecond)
				}
				if len(pids) != expected {
					t.Fatal("workers not ready")
				}
				if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					failure, ok := err.(*exec.ExitError)
					if !ok || failure.ExitCode() != 128+int(sig) {
						t.Fatalf("exit=%v, want %d\n%s", err, 128+int(sig), out.String())
					}
				case <-time.After(10 * time.Second):
					t.Fatal("interrupted build did not exit")
				}
				acquired := make(chan error, 1)
				go func() {
					lock, err := flock.Acquire(filepath.Join(project, ".vmake", "_locks", "project.lock"))
					if err == nil {
						err = lock.Release()
					}
					acquired <- err
				}()
				select {
				case err := <-acquired:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("project lock remained held")
				}
				time.Sleep(2200 * time.Millisecond)
				for _, name := range []string{"late", "later", "installed-hook"} {
					if _, err := os.Stat(filepath.Join(project, name)); !os.IsNotExist(err) {
						t.Fatalf("unexpected %s after cancellation: %v\n%s", name, err, out.String())
					}
				}
				filepath.WalkDir(filepath.Join(project, "build"), func(path string, e os.DirEntry, err error) error {
					if err == nil && !e.IsDir() && strings.HasSuffix(e.Name(), ".vmake.json") && !strings.HasSuffix(e.Name(), ".o.vmake.json") {
						t.Errorf("unfinished link success record: %s", path)
					}
					return nil
				})
				if err := os.Remove(filepath.Join(project, "block")); err != nil {
					t.Fatal(err)
				}
				retry := exec.Command(exe, args...)
				retry.Dir = project
				retry.Env = env
				if data, err := retry.CombinedOutput(); err != nil {
					t.Fatalf("retry: %v\n%s", err, data)
				}
				for _, name := range []string{"later", "installed-hook"} {
					if _, err := os.Stat(filepath.Join(project, name)); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
