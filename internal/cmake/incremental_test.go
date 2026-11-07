package cmake

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/toolchain"
)

func writeFakeCMake(t *testing.T, dir, body string) string {
	t.Helper()
	name := "cmake"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newIncrementalSettings(t *testing.T) Settings {
	t.Helper()
	root := t.TempDir()
	s := Settings{
		SourceDir:   filepath.Join(root, "source"),
		BuildRoot:   filepath.Join(root, "build", "key"),
		BuildDir:    filepath.Join(root, "build", "key", "cmake"),
		InstallDir:  filepath.Join(root, "install"),
		BuildType:   "Release",
		HostOS:      runtime.GOOS,
		TargetOS:    runtime.GOOS,
		Toolchain:   &toolchain.Toolchain{},
		GlobalFlags: nil,
		DryRun:      false,
	}
	bin := filepath.Join(root, "bin")
	writeFakeCMake(t, bin, "cmake executable one\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.MkdirAll(s.SourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	return s
}

func writeConfigureOutput(t *testing.T, s Settings, args []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(s.BuildDir, "CMakeFiles"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.BuildDir, "CMakeCache.txt"), []byte("cache\n"), 0644); err != nil {
		t.Fatal(err)
	}
	main := GeneratorMainFile(args)
	if main == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(s.BuildDir, main), []byte("generated\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func saveStamp(t *testing.T, s Settings, extra ...string) []string {
	t.Helper()
	args, err := ConfigureArgs(s, extra...)
	if err != nil {
		t.Fatal(err)
	}
	writeConfigureOutput(t, s, args)
	if err := SaveStamp(s, args, ""); err != nil {
		t.Fatal(err)
	}
	return args
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestStampRoundTripAndSourceInputs(t *testing.T) {
	s := newIncrementalSettings(t)
	writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.20)\n")
	args := saveStamp(t, s, "-G", "Ninja")
	if _, ok := State(s, args); !ok {
		t.Fatal("freshly saved configure stamp is not up to date")
	}
	if data, err := os.ReadFile(StampPath(s)); err != nil || len(data) == 0 {
		t.Fatalf("stamp not written: %q, %v", data, err)
	}
	writeFile(t, s.SourceDir, "probe.c", "int probe(void) { return 1; }\n")
	writeFile(t, s.SourceDir, "probe.h", "#define PROBE 1\n")
	if _, ok := State(s, args); !ok {
		t.Fatal("compiled source change invalidated the configure stamp")
	}
	writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.21)\n")
	if _, ok := State(s, args); ok {
		t.Fatal("changed CMakeLists.txt did not invalidate the configure stamp")
	}
	writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.20)\n")
	if _, ok := State(s, args); !ok {
		t.Fatal("restored CMakeLists.txt did not restore the fingerprint")
	}
	writeFile(t, s.SourceDir, "extra.cmake", "set(EXTRA 1)\n")
	if _, ok := State(s, args); ok {
		t.Fatal("new .cmake file did not invalidate the configure stamp")
	}
	writeFile(t, s.SourceDir, "CMakePresets.json", "{\"version\": 3}\n")
	if _, ok := State(s, args); ok {
		t.Fatal("new CMakePresets.json did not invalidate the configure stamp")
	}
}

func TestFingerprintExcludesBuildDirectories(t *testing.T) {
	s := newIncrementalSettings(t)
	writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.20)\n")
	args := saveStamp(t, s, "-G", "Ninja")
	writeFile(t, s.BuildDir, "generated.cmake", "set(GENERATED 1)\n")
	writeFile(t, s.BuildRoot, "other.cmake", "set(OTHER 1)\n")
	writeFile(t, filepath.Join(filepath.Dir(s.BuildRoot), "other-key"), "keyed.cmake", "set(KEYED 1)\n")
	writeFile(t, filepath.Join(s.SourceDir, ".git"), "hook.cmake", "set(HOOK 1)\n")
	writeFile(t, filepath.Join(s.SourceDir, ".vmake_deps", "dep"), "dep.cmake", "set(DEP 1)\n")
	writeFile(t, filepath.Join(s.SourceDir, "CMakeFiles"), "generated.cmake", "set(GENERATED 1)\n")
	writeFile(t, s.SourceDir, "cmake_install.cmake", "set(INSTALL 1)\n")
	if _, ok := State(s, args); !ok {
		t.Fatal("build directories or generated CMake files invalidated the configure stamp")
	}
}

func TestFingerprintScansUpstreamBuildDir(t *testing.T) {
	s := newIncrementalSettings(t)
	s.SourceDir = filepath.Join(t.TempDir(), "src", "sample")
	s.BuildRoot = filepath.Join(t.TempDir(), "out", "owner", "key", "build")
	s.BuildDir = filepath.Join(s.BuildRoot, "cmake")
	if err := os.MkdirAll(s.SourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.20)\n")
	args := saveStamp(t, s, "-G", "Ninja")
	if _, ok := State(s, args); !ok {
		t.Fatal("saved configure stamp is not up to date")
	}
	writeFile(t, filepath.Join(s.SourceDir, "build"), "upstream.cmake", "set(UPSTREAM 1)\n")
	if _, ok := State(s, args); ok {
		t.Fatal("an upstream build/ directory was excluded from the fingerprint")
	}
}

func TestFingerprintTracksArgumentsAndSettings(t *testing.T) {
	s := newIncrementalSettings(t)
	writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.20)\n")
	args := saveStamp(t, s, "-G", "Ninja")
	changed := append(append([]string{}, args...), "-DEXTRA=1")
	if _, ok := State(s, changed); ok {
		t.Fatal("argument change did not invalidate the configure stamp")
	}
	other := s
	other.InstallDir = filepath.Join(t.TempDir(), "stage")
	otherArgs, err := ConfigureArgs(other, "-G", "Ninja")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := State(s, otherArgs); ok {
		t.Fatal("install prefix change did not invalidate the configure stamp")
	}
	other = s
	other.GlobalFlags = []string{"-DCMAKE_C_FLAGS=-DGLOBAL_C"}
	otherArgs, err = ConfigureArgs(other, "-G", "Ninja")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := State(s, otherArgs); ok {
		t.Fatal("global flag change did not invalidate the configure stamp")
	}
}

func TestFingerprintTracksEnvironment(t *testing.T) {
	t.Run("cmake generator", func(t *testing.T) {
		s := newIncrementalSettings(t)
		writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.20)\n")
		t.Setenv("CMAKE_GENERATOR", "")
		args := saveStamp(t, s, "-G", "Ninja")
		t.Setenv("CMAKE_GENERATOR", "Ninja")
		if _, ok := State(s, args); ok {
			t.Fatal("CMAKE_GENERATOR change did not invalidate the configure stamp")
		}
	})
	t.Run("cmake prefix path", func(t *testing.T) {
		s := newIncrementalSettings(t)
		writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.20)\n")
		t.Setenv("CMAKE_PREFIX_PATH", "")
		args := saveStamp(t, s, "-G", "Ninja")
		t.Setenv("CMAKE_PREFIX_PATH", filepath.Join(t.TempDir(), "prefix"))
		if _, ok := State(s, args); ok {
			t.Fatal("CMAKE_PREFIX_PATH change did not invalidate the configure stamp")
		}
	})
	t.Run("path", func(t *testing.T) {
		s := newIncrementalSettings(t)
		writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.20)\n")
		args := saveStamp(t, s, "-G", "Ninja")
		t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+"unused")
		if _, ok := State(s, args); ok {
			t.Fatal("PATH change did not invalidate the configure stamp")
		}
	})
}

func TestStampInvalidation(t *testing.T) {
	newFixture := func(t *testing.T) (Settings, []string) {
		t.Helper()
		s := newIncrementalSettings(t)
		writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.20)\n")
		return s, saveStamp(t, s, "-G", "Ninja")
	}
	t.Run("missing stamp", func(t *testing.T) {
		s, args := newFixture(t)
		if err := os.Remove(StampPath(s)); err != nil {
			t.Fatal(err)
		}
		if _, ok := State(s, args); ok {
			t.Fatal("missing stamp reported up to date")
		}
	})
	t.Run("garbage stamp", func(t *testing.T) {
		s, args := newFixture(t)
		if err := os.WriteFile(StampPath(s), []byte("not json"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, ok := State(s, args); ok {
			t.Fatal("garbage stamp reported up to date")
		}
	})
	t.Run("version mismatch", func(t *testing.T) {
		s, args := newFixture(t)
		if err := os.WriteFile(StampPath(s), []byte(`{"version":999,"fingerprint":"stale"}`), 0644); err != nil {
			t.Fatal(err)
		}
		if _, ok := State(s, args); ok {
			t.Fatal("stamp with an unknown version reported up to date")
		}
	})
	t.Run("missing cmake cache", func(t *testing.T) {
		s, args := newFixture(t)
		if _, ok := State(s, args); !ok {
			t.Fatal("saved configure stamp is not up to date")
		}
		if err := os.Remove(filepath.Join(s.BuildDir, "CMakeCache.txt")); err != nil {
			t.Fatal(err)
		}
		if _, ok := State(s, args); ok {
			t.Fatal("missing CMakeCache.txt reported up to date")
		}
	})
	t.Run("missing generator file", func(t *testing.T) {
		s, args := newFixture(t)
		if err := os.Remove(filepath.Join(s.BuildDir, "build.ninja")); err != nil {
			t.Fatal(err)
		}
		if _, ok := State(s, args); ok {
			t.Fatal("missing build.ninja reported up to date")
		}
	})
	t.Run("missing CMakeFiles", func(t *testing.T) {
		s, args := newFixture(t)
		if err := os.RemoveAll(filepath.Join(s.BuildDir, "CMakeFiles")); err != nil {
			t.Fatal(err)
		}
		if _, ok := State(s, args); ok {
			t.Fatal("missing CMakeFiles reported up to date")
		}
	})
	t.Run("dry run", func(t *testing.T) {
		s, args := newFixture(t)
		s.DryRun = true
		if _, ok := State(s, args); ok {
			t.Fatal("dry-run reported configure up to date")
		}
	})
	t.Run("preset needs CMakeFiles only", func(t *testing.T) {
		s := newIncrementalSettings(t)
		writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.20)\n")
		args, err := ConfigureArgs(s, "--preset", "release")
		if err != nil {
			t.Fatal(err)
		}
		writeConfigureOutput(t, s, args)
		if err := SaveStamp(s, args, ""); err != nil {
			t.Fatal(err)
		}
		if _, ok := State(s, args); !ok {
			t.Fatal("preset configure stamp is not up to date")
		}
		if err := os.RemoveAll(filepath.Join(s.BuildDir, "CMakeFiles")); err != nil {
			t.Fatal(err)
		}
		if _, ok := State(s, args); ok {
			t.Fatal("preset configure without CMakeFiles reported up to date")
		}
	})
}

func TestStateReusesFingerprint(t *testing.T) {
	s := newIncrementalSettings(t)
	writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.20)\n")
	args := saveStamp(t, s, "-G", "Ninja")
	writeCMakeLists(t, s.SourceDir, "cmake_minimum_required(VERSION 3.21)\n")
	fingerprint, ok := State(s, args)
	if ok || fingerprint == "" {
		t.Fatalf("fingerprint miss did not return the current fingerprint: %q, %v", fingerprint, ok)
	}
	if err := SaveStamp(s, args, fingerprint); err != nil {
		t.Fatal(err)
	}
	want, err := Fingerprint(s, args)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint != want {
		t.Fatalf("reused fingerprint %q != recomputed %q", fingerprint, want)
	}
	if _, ok := State(s, args); !ok {
		t.Fatal("reused fingerprint is not up to date")
	}
}

func TestGeneratorMainFile(t *testing.T) {
	t.Setenv("CMAKE_GENERATOR", "")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"ninja flag", []string{"-G", "Ninja"}, "build.ninja"},
		{"joined ninja flag", []string{"-GNinja"}, "build.ninja"},
		{"makefiles flag", []string{"-G", "Unix Makefiles"}, "Makefile"},
		{"multi config ninja", []string{"-G", "Ninja Multi-Config"}, "build.ninja"},
		{"preset generator unknown", []string{"--preset", "release"}, ""},
		{"foreign generator unknown", []string{"-G", "Visual Studio 17 2022"}, ""},
	}
	for _, tc := range cases {
		if got := GeneratorMainFile(tc.args); got != tc.want {
			t.Errorf("GeneratorMainFile(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := GeneratorFromArgs([]string{"-G", "Ninja"}); got != "Ninja" {
		t.Errorf("GeneratorFromArgs = %q", got)
	}
	t.Setenv("CMAKE_GENERATOR", "Ninja")
	if got := GeneratorMainFile(nil); got != "build.ninja" {
		t.Errorf("CMAKE_GENERATOR Ninja -> %q, want build.ninja", got)
	}
	t.Setenv("CMAKE_GENERATOR", "")
	want := "Makefile"
	if runtime.GOOS == "windows" {
		want = "build.ninja"
	}
	if got := GeneratorMainFile(nil); got != want {
		t.Errorf("host default -> %q, want %q", got, want)
	}
}

func TestEnvironmentMergesToolchain(t *testing.T) {
	t.Setenv("VMAKE_TEST_SENTINEL", "process")
	s := Settings{Toolchain: &toolchain.Toolchain{}}
	if got := Environment(s)["VMAKE_TEST_SENTINEL"]; got != "process" {
		t.Fatalf("process environment missing from the synthetic env: %q", got)
	}
	install := filepath.Join(t.TempDir(), "toolchain")
	if err := os.MkdirAll(filepath.Join(install, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	s.Toolchain = &toolchain.Toolchain{InstallPath: install}
	if got := Environment(s)["PATH"]; !strings.HasPrefix(got, filepath.Join(install, "bin")) {
		t.Fatalf("toolchain PATH did not override the process PATH: %q", got)
	}
	if got := CommandPath(s); !strings.HasPrefix(got, filepath.Join(install, "bin")) {
		t.Fatalf("CommandPath = %q", got)
	}
}

func TestConfigureEnvFiltersVariables(t *testing.T) {
	s := newIncrementalSettings(t)
	t.Setenv("MY_CMAKE_FLAG", "value")
	t.Setenv("VMAKE_ENV_EXTRA", "MY_CMAKE_FLAG")
	t.Setenv("CC", "cc-override")
	t.Setenv("CFLAGS", "-Dignored")
	t.Setenv("LDFLAGS", "-Lignored")
	t.Setenv("PKG_CONFIG_PATH", "/tmp/pkgconfig")
	env := ConfigureEnv(s)
	for _, key := range []string{"CC", "CFLAGS", "LDFLAGS"} {
		if _, ok := env[key]; ok {
			t.Errorf("%s must not participate in the CMake fingerprint: %#v", key, env)
		}
	}
	if env["PKG_CONFIG_PATH"] != "/tmp/pkgconfig" {
		t.Errorf("PKG_CONFIG_PATH missing from CMake fingerprint: %#v", env)
	}
	if env["MY_CMAKE_FLAG"] != "value" {
		t.Errorf("VMAKE_ENV_EXTRA key missing from CMake fingerprint: %#v", env)
	}
	if !EnvOverridden("CFLAGS") || EnvOverridden("PKG_CONFIG_PATH") {
		t.Error("EnvOverridden mismatch")
	}
}

func TestResolveExecutableUsesCommandPath(t *testing.T) {
	s := newIncrementalSettings(t)
	install := filepath.Join(t.TempDir(), "toolchain space")
	installBin := filepath.Join(install, "bin")
	toolPath := writeFakeCMake(t, installBin, "toolchain cmake one\n")
	s.Toolchain = &toolchain.Toolchain{InstallPath: install}
	path, digest := ResolveExecutable(s)
	if path != toolPath || digest == "" {
		t.Fatalf("ResolveExecutable = %q, %q, want %q", path, digest, toolPath)
	}
	writeFakeCMake(t, installBin, "toolchain cmake two\n")
	if _, changed := ResolveExecutable(s); changed == digest {
		t.Fatal("toolchain cmake change did not change its digest")
	}
}

func TestHelpers(t *testing.T) {
	root := t.TempDir()
	if got, ok := NestedPath(root, filepath.Join(root, "child")); !ok || got != filepath.Join(root, "child") {
		t.Errorf("NestedPath child = %q, %v", got, ok)
	}
	if _, ok := NestedPath(root, root); ok {
		t.Error("NestedPath accepted the root itself")
	}
	if _, ok := NestedPath(root, filepath.Join(filepath.Dir(root), "elsewhere")); ok {
		t.Error("NestedPath accepted a sibling path")
	}
	if _, ok := NestedPath(root, ""); ok {
		t.Error("NestedPath accepted an empty path")
	}
	for _, name := range []string{"CMakeLists.txt", "CMakePresets.json", "CMakeUserPresets.json", "module.cmake", "config.cmake.in"} {
		if !IsInputFile(name) {
			t.Errorf("IsInputFile(%q) = false", name)
		}
	}
	for _, name := range []string{"probe.c", "build.ninja"} {
		if IsInputFile(name) {
			t.Errorf("IsInputFile(%q) = true", name)
		}
	}
	if got := HashBytes([]byte("abc")); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("HashBytes = %q", got)
	}
	first, err := DigestValue(map[string]string{"a": "b"})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := DigestValue(map[string]string{"a": "b"})
	if first == "" || first != second {
		t.Errorf("DigestValue is not deterministic: %q, %q", first, second)
	}
	symlinkOrSkip(t, root, filepath.Join(t.TempDir(), "link"))
	link := filepath.Join(t.TempDir(), "source link")
	symlinkOrSkip(t, root, link)
	if got, want := ResolveSymlinks(link), ResolveSymlinks(root); got != want {
		t.Errorf("ResolveSymlinks(link) = %q, want %q", got, want)
	}
}

func TestInputFilesFollowsSymlinkedSource(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "actual source")
	if err := os.MkdirAll(actual, 0755); err != nil {
		t.Fatal(err)
	}
	writeCMakeLists(t, actual, "cmake_minimum_required(VERSION 3.20)\n")
	link := filepath.Join(root, "src")
	symlinkOrSkip(t, actual, link)
	s := newIncrementalSettings(t)
	s.SourceDir = link
	files, err := InputFiles(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Rel != "CMakeLists.txt" {
		t.Fatalf("input files = %#v", files)
	}
	writeCMakeLists(t, actual, "cmake_minimum_required(VERSION 3.21)\n")
	changed, err := InputFiles(s)
	if err != nil {
		t.Fatal(err)
	}
	if changed[0].Hash == files[0].Hash {
		t.Fatal("symlinked source change did not change the input digest")
	}
	if files, err := InputFiles(Settings{}); err != nil || len(files) != 0 {
		t.Fatalf("empty SourceDir input files = %#v, %v", files, err)
	}
}

func symlinkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, fs.ErrPermission) {
			t.Skipf("symlink creation requires Developer Mode or elevation: %v", err)
		}
		t.Fatal(err)
	}
}

func TestStampAndFingerprintJSONFields(t *testing.T) {
	stampJSON, err := json.Marshal(Stamp{Version: StampVersion, Fingerprint: "f", CMakePath: "/cmake", GeneratedAt: "now"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"version":1,"fingerprint":"f","cmakePath":"/cmake","generatedAt":"now"}`; string(stampJSON) != want {
		t.Fatalf("stamp JSON = %s, want %s", stampJSON, want)
	}
	fingerprintJSON, err := json.Marshal(fingerprint{
		Version:     StampVersion,
		HostOS:      "linux",
		Args:        []string{"-S", "src"},
		CMakePath:   "/cmake",
		CMakeDigest: "digest",
		Env:         map[string]string{"PATH": "bin"},
		Files:       []FileDigest{{Rel: "CMakeLists.txt", Hash: "hash"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"hostOS":"linux","args":["-S","src"],"cmakePath":"/cmake","cmakeDigest":"digest","env":{"PATH":"bin"},"files":[{"rel":"CMakeLists.txt","hash":"hash"}]}`
	if string(fingerprintJSON) != want {
		t.Fatalf("fingerprint JSON = %s, want %s", fingerprintJSON, want)
	}
}
