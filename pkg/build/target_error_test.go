package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestTargetErrorStages(t *testing.T) {
	t.Run("compile", func(t *testing.T) {
		dir := t.TempDir()
		writeAssemblyFixture(t, dir, "main.c", "int main(void) { return 0; }\n")
		target := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c")
		s := nativeTestScheduler(t, dir, target)
		s.compiler.run = func(string, string, ...string) ([]byte, error) {
			return nil, errors.New("compile boom")
		}
		err := s.Build("p:app")
		var targetErr *TargetError
		if !errors.As(err, &targetErr) {
			t.Fatalf("error = %v, want *TargetError", err)
		}
		if targetErr.Target != "p:app" || targetErr.Stage != "compile" || !strings.Contains(err.Error(), "compile boom") {
			t.Fatalf("target error = %+v", targetErr)
		}
	})

	t.Run("prepare", func(t *testing.T) {
		dir := t.TempDir()
		writeAssemblyFixture(t, dir, "main.c", "int main(void) { return 0; }\n")
		if err := os.WriteFile(filepath.Join(dir, "build"), []byte("not a directory"), 0644); err != nil {
			t.Fatal(err)
		}
		target := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c")
		s := nativeTestScheduler(t, dir, target)
		err := s.Build("p:app")
		var targetErr *TargetError
		if !errors.As(err, &targetErr) {
			t.Fatalf("error = %v, want *TargetError", err)
		}
		if targetErr.Target != "p:app" || targetErr.Stage != "prepare" {
			t.Fatalf("target error = %+v", targetErr)
		}
	})

	t.Run("link", func(t *testing.T) {
		dir := t.TempDir()
		writeAssemblyFixture(t, dir, "main.c", "int main(void) { return 0; }\n")
		target := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c")
		s := nativeTestScheduler(t, dir, target)
		s.linker.run = func(string, string, ...string) ([]byte, error) {
			return nil, errors.New("link boom")
		}
		err := s.Build("p:app")
		var targetErr *TargetError
		if !errors.As(err, &targetErr) {
			t.Fatalf("error = %v, want *TargetError", err)
		}
		if targetErr.Target != "p:app" || targetErr.Stage != "link" || !strings.Contains(err.Error(), "link boom") {
			t.Fatalf("target error = %+v", targetErr)
		}
	})
}

func TestTargetErrorsDiscoverableInJoinedBuildFailures(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "a.c", "int a(void) { return 1; }\n")
	writeAssemblyFixture(t, dir, "b.c", "int b(void) { return 2; }\n")
	first := makeTargetWithDeps("first").SetKind(api.TargetBinary).AddFiles("a.c")
	second := makeTargetWithDeps("second").SetKind(api.TargetBinary).AddFiles("b.c")
	s := nativeTestScheduler(t, dir, first, second)
	s.keepGoing = true
	s.compiler.run = func(string, string, ...string) ([]byte, error) {
		return nil, errors.New("compile boom")
	}
	err := s.BuildAll()
	if err == nil {
		t.Fatal("BuildAll returned no error")
	}
	if _, ok := err.(interface{ Unwrap() []error }); !ok {
		t.Fatalf("joined error expected, got %T: %v", err, err)
	}
	joined := err.(interface{ Unwrap() []error }).Unwrap()
	if len(joined) != 2 {
		t.Fatalf("joined failures = %d, want 2: %v", len(joined), err)
	}
	for _, branch := range joined {
		var targetErr *TargetError
		if !errors.As(branch, &targetErr) || targetErr.Stage != "compile" {
			t.Fatalf("branch %v does not expose a compile TargetError", branch)
		}
	}
}

func TestTargetErrorDoesNotWrapCancellation(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "main.c", "int main(void) { return 0; }\n")
	target := makeTargetWithDeps("app").SetKind(api.TargetBinary).AddFiles("main.c")
	s := nativeTestScheduler(t, dir, target)
	s.compiler.run = func(string, string, ...string) ([]byte, error) {
		return nil, context.Canceled
	}
	err := s.Build("p:app")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not preserved: %v", err)
	}
	var targetErr *TargetError
	if errors.As(err, &targetErr) {
		t.Fatalf("cancellation was wrapped in TargetError: %v", err)
	}
}
