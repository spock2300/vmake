package api

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	cmake "github.com/spock2300/vmake/internal/cmake"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func newCMakeStampFixture(t *testing.T) (*Package, string, string) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{source, bin} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeFakeCMake(t, bin, "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	p := NewPackage().
		SetDirs(PkgDirs{SourceDir: source, BuildDir: filepath.Join(source, "build", "key")}).
		SetToolchain(&toolchain.Toolchain{})
	if err := os.MkdirAll(p.CMakeBuildDir(), 0755); err != nil {
		t.Fatal(err)
	}
	return p, source, bin
}

// stampFile is where CMakeConfigure records a successful configure.
func stampFile(p *Package) string {
	return filepath.Join(p.CMakeBuildDir(), cmake.StampName)
}

func TestCMakeConfigureFailureRemovesStamp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cmake shell script")
	}
	p, source, bin := newCMakeStampFixture(t)
	writeCMakeFile(t, source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	writeFakeCMake(t, bin, "#!/bin/sh\nmkdir -p CMakeFiles\ntouch CMakeCache.txt build.ninja\n")
	p.CMakeConfigure()
	if _, err := os.Stat(stampFile(p)); err != nil {
		t.Fatalf("successful configure did not record a stamp: %v", err)
	}

	writeCMakeFile(t, source, "CMakeLists.txt", "changed project()\n")
	writeFakeCMake(t, bin, "#!/bin/sh\necho partial > \"$PWD/CMakeCache.txt\"\nmkdir -p CMakeFiles\nexit 1\n")
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("CMakeConfigure did not report the configure failure")
			}
		}()
		p.CMakeConfigure()
	}()
	if _, err := os.Stat(stampFile(p)); !os.IsNotExist(err) {
		t.Fatalf("failed configure left a stamp: %v", err)
	}
}

func TestCMakeConfigureDryRunWritesNoStamp(t *testing.T) {
	p, source, _ := newCMakeStampFixture(t)
	writeCMakeFile(t, source, "CMakeLists.txt", "cmake_minimum_required(VERSION 3.20)\n")
	p.SetDryRun(true)
	out := captureCMakeStdout(t, func() { p.CMakeConfigure("-G", "Ninja") })
	if !strings.Contains(out, "-G Ninja") {
		t.Fatalf("dry-run did not print the configure command:\n%s", out)
	}
	if _, err := os.Stat(stampFile(p)); !os.IsNotExist(err) {
		t.Fatalf("dry-run recorded a configure stamp: %v", err)
	}
}

func writeFakeCMake(t *testing.T, bin, body string) {
	t.Helper()
	name := "cmake"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
}

func writeCMakeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
