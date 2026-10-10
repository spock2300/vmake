package repo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSourcePreparationCommandsCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell Git fixture")
	}
	for _, stage := range []string{"clone", "fetch", "submodule", "patch"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			ready, late := filepath.Join(dir, "ready"), filepath.Join(dir, "late")
			// The fake git answers metadata queries and stalls on the action
			// under test until the context cancels it.
			body := "#!/bin/sh\n" +
				"for arg in \"$@\"; do\n" +
				" if [ \"$arg\" = \"ls-remote\" ]; then\n" +
				"  echo \"0123456789012345678901234567890123456789\tHEAD\"\n" +
				"  exit 0\n" +
				" fi\n" +
				"done\n" +
				"for arg in \"$@\"; do\n" +
				" if [ \"$arg\" = \"$VMAKE_CANCEL_ACTION\" ]; then\n" +
				"  echo started > \"$VMAKE_CANCEL_READY\"\n" +
				"  sleep 0.3\n" +
				"  echo late > \"$VMAKE_CANCEL_LATE\"\n" +
				" fi\n" +
				"done\n" +
				"exit 0\n"
			if err := os.WriteFile(filepath.Join(dir, "git"), []byte(body), 0755); err != nil {
				t.Fatal(err)
			}
			action := stage
			if action == "patch" {
				action = "apply"
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("VMAKE_CANCEL_ACTION", action)
			t.Setenv("VMAKE_CANCEL_READY", ready)
			t.Setenv("VMAKE_CANCEL_LATE", late)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			deps, cache := filepath.Join(dir, "deps"), filepath.Join(dir, "cache")
			done := make(chan error, 1)
			go func() {
				var err error
				switch stage {
				case "clone":
					_, err = NewSourceManager(deps, cache).WithContext(ctx).
						EnsureSource(SourceRequest{Key: "local/source", URLs: []string{"https://example.invalid/source.git"}})
				case "fetch":
					_, err = NewSourceManager(deps, cache).WithContext(ctx).
						EnsureSource(SourceRequest{
							Key:  "local/source",
							URLs: []string{"https://example.invalid/source.git"},
							Ref:  "0123456789012345678901234567890123456789",
						})
				case "submodule":
					err = InitSubmodulesContext(ctx, dir)
				case "patch":
					err = ApplyPatchContext(ctx, dir, "change.patch")
				}
				done <- err
			}()
			deadline := time.Now().Add(3 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("command ended before cancellation: %v", err)
				default:
				}
				if time.Now().After(deadline) {
					t.Fatal("command did not start")
				}
				time.Sleep(5 * time.Millisecond)
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("canceled source command remained running")
			}
			time.Sleep(350 * time.Millisecond)
			if _, err := os.Stat(late); !os.IsNotExist(err) {
				t.Fatalf("source command wrote after cancellation: %v", err)
			}
			if stage == "clone" || stage == "fetch" {
				if err := filepath.WalkDir(deps, func(path string, entry os.DirEntry, err error) error {
					if err != nil {
						if os.IsNotExist(err) {
							return nil
						}
						return err
					}
					if strings.HasSuffix(entry.Name(), ".tmp") {
						t.Errorf("canceled clone retained a temporary directory: %s", path)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestInitSubmodulesUsesShallowDepth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell Git fixture")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "args.log")
	body := "#!/bin/sh\necho \"$@\" >> \"$VMAKE_ARGS_LOG\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VMAKE_ARGS_LOG", log)
	if err := InitSubmodulesContext(context.Background(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	args := string(data)
	if !strings.Contains(args, "submodule update") || !strings.Contains(args, "--depth 1") {
		t.Fatalf("submodule update args = %q", args)
	}
}

func TestCanceledRefreshKeepsExistingTree(t *testing.T) {
	requireGit(t)
	src := writePatchableSource(t)
	runGit(t, src, "tag", "v1.0.0")
	manager := NewSourceManager(t.TempDir(), t.TempDir())
	first, err := manager.EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.WithContext(ctx).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v1.0.0", Refresh: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("refresh cancellation = %v", err)
	}
	manager.WithContext(context.Background())
	if commit := manager.CommitAt(first.Root); commit != first.Commit {
		t.Fatalf("canceled refresh changed tree commit: %s -> %s", first.Commit, commit)
	}
	if _, err := manager.WithContext(context.Background()).EnsureSource(SourceRequest{Key: "native/sample", URLs: []string{src}, Ref: "v1.0.0", Refresh: true}); err != nil {
		t.Fatalf("refresh retry: %v", err)
	}
}
