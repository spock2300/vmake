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

	"github.com/spock2300/vmake/pkg/api"
)

func TestSourcePreparationCommandsCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell Git fixture")
	}
	for _, stage := range []string{"clone", "fetch", "submodule", "patch"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			ready, late := filepath.Join(dir, "ready"), filepath.Join(dir, "late")
			body := "#!/bin/sh\nfor arg in \"$@\"; do\n if [ \"$arg\" = \"$VMAKE_CANCEL_ACTION\" ]; then\n echo started > \"$VMAKE_CANCEL_READY\"\n sleep 0.3\n echo late > \"$VMAKE_CANCEL_LATE\"\n fi\ndone\nexit 0\n"
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
			done := make(chan error, 1)
			go func() {
				var err error
				switch stage {
				case "clone":
					_, err = NewSourceManager(filepath.Join(dir, "deps"), filepath.Join(dir, "cache")).WithContext(ctx).EnsureURL("https://example.invalid/source.git")
				case "fetch":
					err = FetchTagsContext(ctx, dir)
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
			if stage == "clone" {
				if err := filepath.WalkDir(filepath.Join(dir, "cache"), func(path string, entry os.DirEntry, err error) error {
					if err == nil && (entry.Name() == "src" || strings.HasSuffix(entry.Name(), ".tmp")) {
						t.Errorf("canceled source published or retained temporary checkout: %s", path)
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestCanceledSourceRefreshKeepsExistingCheckout(t *testing.T) {
	source := writePatchableSource(t)
	manager := NewSourceManager(t.TempDir(), t.TempDir())
	pkg := api.NewPackage().SetGit(source)
	pkg.Repo, pkg.Name = "native", "source"
	refs, err := manager.EnsureRefsClone(pkg, false)
	if err != nil {
		t.Fatal(err)
	}
	before, err := GetCurrentCommit(refs)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = manager.WithContext(ctx).EnsureRefsClone(pkg, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("refresh cancellation = %v", err)
	}
	after, err := GetCurrentCommit(refs)
	if err != nil || after != before {
		t.Fatalf("canceled refresh changed cached checkout: %s, %v", after, err)
	}
	if _, err := manager.WithContext(context.Background()).EnsureRefsClone(pkg, true); err != nil {
		t.Fatalf("refresh retry: %v", err)
	}
}
