package build

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spock2300/vmake/pkg/api"
)

func TestPublishedHeadersPreserveMakeIncremental(t *testing.T) {
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is unavailable")
	}
	version, err := exec.Command(makePath, "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(version), "GNU Make") {
		t.Skip("GNU Make is unavailable")
	}
	for _, key := range []string{"MAKEFLAGS", "MFLAGS", "GNUMAKEFLAGS", "MAKELEVEL", "MAKEOVERRIDES", "MAKEFILES"} {
		t.Setenv(key, "")
	}
	for _, route := range []string{"scheduler", "installer"} {
		for _, includes := range []string{"directory", "matching", "file"} {
			t.Run(route+"/"+includes, func(t *testing.T) {
				dir := t.TempDir()
				writeAssemblyFixture(t, dir, "api/value.h", "#define VALUE 41\n")
				writeAssemblyFixture(t, dir, "api/ignore.c", "not a header\n")
				writeAssemblyFixture(t, dir, "library.c", "int library(void) { return 1; }\n")
				target := makeTargetWithDeps("library").SetKind(api.TargetStatic).AddFiles("library.c")
				switch includes {
				case "directory":
					target.AddPublicIncludes("api")
				case "matching":
					target.AddPublicIncludes("api", "@*.h")
				case "file":
					target.AddPublicIncludes("api/value.h")
				}
				s := nativeTestScheduler(t, dir, target)
				staging := filepath.Join(dir, "staging")
				if route == "scheduler" {
					s.pkgs["p"].InstallDir = staging
				}
				publish := func() {
					t.Helper()
					if err := s.Build("p:library"); err != nil {
						t.Fatal(err)
					}
					if route == "installer" {
						installer := NewArtifactInstaller(s.graph, map[string]*api.PkgDirs{"p": &s.pkgs["p"].PkgDirs}, staging)
						installer.SetInstallType("sdk")
						installer.SetPackageInfo("p", &PkgInstallInfo{BuildDir: s.pkgs["p"].OutputDir, TargetOS: runtime.GOOS})
						if err := installer.InstallAll(context.Background()); err != nil {
							t.Fatal(err)
						}
					}
				}
				publish()
				if includes != "directory" {
					if _, err := os.Stat(filepath.Join(staging, "include", "ignore.c")); !os.IsNotExist(err) {
						t.Fatalf("non-header was installed: %v", err)
					}
				}
				writeAssemblyFixture(t, dir, "consumer.c", "#include <value.h>\nint consumer(void) { return VALUE; }\n")
				writeAssemblyFixture(t, dir, "Makefile", "consumer.o: consumer.c staging/include/value.h\n\t@echo compiled >> .make-compiles\n\t\"$(CC)\" -Istaging/include -c consumer.c -o consumer.o\n")
				runMake := func(wantCompiles int) {
					t.Helper()
					cmd := exec.Command(makePath, "CC="+filepath.ToSlash(s.resolvedTools.CC), "consumer.o")
					cmd.Dir = dir
					output, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("make: %v\n%s", err, output)
					}
					calls, err := os.ReadFile(filepath.Join(dir, ".make-compiles"))
					if err != nil || strings.Count(string(calls), "compiled") != wantCompiles {
						t.Fatalf("make compile count: %q, %v; want %d\n%s", calls, err, wantCompiles, output)
					}
				}
				header := filepath.Join(staging, "include", "value.h")
				object := filepath.Join(dir, "consumer.o")
				old := time.Now().Add(-time.Hour)
				settle := func() time.Time {
					t.Helper()
					for _, path := range []string{header, filepath.Join(dir, "consumer.c")} {
						if err := os.Chtimes(path, old, old); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Chtimes(object, old.Add(time.Minute), old.Add(time.Minute)); err != nil {
						t.Fatal(err)
					}
					info, err := os.Stat(header)
					if err != nil {
						t.Fatal(err)
					}
					return info.ModTime()
				}
				runMake(1)
				before := settle()
				publish()
				if after, err := os.Stat(header); err != nil || !after.ModTime().Equal(before) {
					t.Fatalf("unchanged published header mtime changed: %v", err)
				}
				runMake(1)
				writeAssemblyFixture(t, dir, "api/value.h", "#define VALUE 42\n")
				publish()
				if data, err := os.ReadFile(header); err != nil || string(data) != "#define VALUE 42\n" {
					t.Fatalf("changed header was not published: %q, %v", data, err)
				}
				runMake(2)
				settle()
				if err := os.Remove(header); err != nil {
					t.Fatal(err)
				}
				publish()
				runMake(3)
				settle()
				publish()
				runMake(3)
			})
		}
	}
}
