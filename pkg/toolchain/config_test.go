package toolchain

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCommandEnvCompilerDirectories(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, "installed tools")
	installBin := filepath.Join(install, "bin")
	ccDir := filepath.Join(root, "C compiler")
	cxxDir := filepath.Join(root, "CXX compiler")
	inherited := filepath.Join(root, "host tools") + string(os.PathListSeparator) + filepath.Join(root, "other tools")
	t.Setenv("PATH", inherited)
	for _, test := range []struct {
		name string
		tc   *Toolchain
		dirs []string
	}{
		{name: "nil"},
		{name: "empty", tc: &Toolchain{}},
		{name: "relative tools", tc: &Toolchain{Tools: Tools{CC: "cross-gcc", CXX: filepath.Join("relative", "cross-g++")}}},
		{name: "installation", tc: &Toolchain{InstallPath: install}, dirs: []string{installBin}},
		{name: "absolute CC", tc: &Toolchain{Tools: Tools{CC: filepath.Join(ccDir, "cross-gcc")}}, dirs: []string{ccDir}},
		{name: "absolute CXX", tc: &Toolchain{Tools: Tools{CXX: filepath.Join(cxxDir, "cross-g++")}}, dirs: []string{cxxDir}},
		{name: "distinct compiler directories", tc: &Toolchain{Tools: Tools{CC: filepath.Join(ccDir, "cross-gcc"), CXX: filepath.Join(cxxDir, "cross-g++")}}, dirs: []string{ccDir, cxxDir}},
		{name: "installation first", tc: &Toolchain{InstallPath: install, Tools: Tools{CC: filepath.Join(ccDir, "cross-gcc"), CXX: filepath.Join(cxxDir, "cross-g++")}}, dirs: []string{installBin, ccDir, cxxDir}},
		{name: "duplicate compiler directory", tc: &Toolchain{Tools: Tools{CC: filepath.Join(ccDir, "cross-gcc"), CXX: filepath.Join(ccDir, "cross-g++")}}, dirs: []string{ccDir}},
		{name: "duplicate installation directory", tc: &Toolchain{InstallPath: install, Tools: Tools{CC: filepath.Join(installBin, "cross-gcc"), CXX: filepath.Join(installBin, "cross-g++")}}, dirs: []string{installBin}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var before Toolchain
			if test.tc != nil {
				before = *test.tc
			}
			got := test.tc.CommandEnv()
			want := map[string]string{}
			if len(test.dirs) > 0 {
				want["PATH"] = strings.Join(test.dirs, string(os.PathListSeparator)) + string(os.PathListSeparator) + inherited
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("CommandEnv() = %v, want %v", got, want)
			}
			if os.Getenv("PATH") != inherited {
				t.Error("CommandEnv changed the process PATH")
			}
			if test.tc != nil && *test.tc != before {
				t.Error("CommandEnv changed the toolchain")
			}
		})
	}
}

func TestCommandEnvEmptyInheritedPath(t *testing.T) {
	t.Setenv("PATH", "")
	bin := filepath.Join(t.TempDir(), "compiler bin")
	tc := &Toolchain{Tools: Tools{CC: filepath.Join(bin, "cross-gcc")}}
	if got := tc.CommandEnv()["PATH"]; got != bin {
		t.Errorf("PATH = %q, want %q", got, bin)
	}
}

func TestCommandEnvDirectoryCase(t *testing.T) {
	t.Setenv("PATH", "")
	root := t.TempDir()
	upper := filepath.Join(root, "Compiler")
	lower := filepath.Join(root, "compiler")
	tc := &Toolchain{Tools: Tools{CC: filepath.Join(upper, "cross-gcc"), CXX: filepath.Join(lower, "cross-g++")}}
	want := upper + string(os.PathListSeparator) + lower
	if runtime.GOOS == "windows" {
		want = upper
	}
	if got := tc.CommandEnv()["PATH"]; got != want {
		t.Errorf("PATH = %q, want %q", got, want)
	}
}
