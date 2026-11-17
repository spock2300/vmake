package cmake

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spock2300/vmake/internal/envsig"
	"github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/jsonio"
	vlog "github.com/spock2300/vmake/pkg/log"
)

const (
	StampVersion = 1
	StampName    = ".vmake_configure.json"
)

type stamp struct {
	Version     int    `json:"version"`
	Fingerprint string `json:"fingerprint"`
	CMakePath   string `json:"cmakePath,omitempty"`
	GeneratedAt string `json:"generatedAt,omitempty"`
}

type fingerprint struct {
	Version     int               `json:"version"`
	HostOS      string            `json:"hostOS"`
	Args        []string          `json:"args"`
	CMakePath   string            `json:"cmakePath"`
	CMakeDigest string            `json:"cmakeDigest"`
	Env         map[string]string `json:"env"`
	Files       []fileDigest      `json:"files"`
}

type fileDigest struct {
	Rel  string `json:"rel"`
	Hash string `json:"hash"`
}

func stampPath(s Settings) string {
	return filepath.Join(s.BuildDir, StampName)
}

// stampState reports whether the recorded configure output can be reused and
// returns the fingerprint computed for the current tree. A required
// reconfigure can pass the returned fingerprint to writeStamp instead of
// walking the source tree a second time; it is empty when it was not
// computed (dry run, unusable stamp, missing configure output, hashing error).
func stampState(s Settings, args []string) (string, bool) {
	if s.DryRun {
		return "", false
	}
	var stamp stamp
	if err := jsonio.Load(stampPath(s), &stamp); err != nil {
		return "", false
	}
	if stamp.Version != StampVersion || stamp.Fingerprint == "" {
		return "", false
	}
	if !outputPresent(s, args) {
		return "", false
	}
	fingerprint, err := computeFingerprint(s, args)
	if err != nil {
		vlog.Debug("  cmake configure: fingerprint unavailable: %v", err)
		return "", false
	}
	if fingerprint != stamp.Fingerprint {
		vlog.Debug("  cmake configure: fingerprint changed for %s", filepath.ToSlash(s.BuildDir))
		return fingerprint, false
	}
	return fingerprint, true
}

// outputPresent reports whether the configured build tree still holds what
// `cmake --build` needs: the cache, the generator's CMakeFiles bookkeeping
// and, when the generator is known, its top-level build file. A hand-deleted
// build.ninja or Makefile must force configure rather than fail a later build.
func outputPresent(s Settings, args []string) bool {
	if !isRegularFile(filepath.Join(s.BuildDir, "CMakeCache.txt")) {
		return false
	}
	if info, err := os.Stat(filepath.Join(s.BuildDir, "CMakeFiles")); err != nil || !info.IsDir() {
		return false
	}
	main := generatorMainFileForHost(args, s.HostOS)
	if main == "" {
		return true
	}
	return isRegularFile(filepath.Join(s.BuildDir, main))
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// generatorMainFile returns the top-level build file the configured generator
// writes next to CMakeCache.txt, or "" when the generator cannot be determined
// (configure presets) or is not one VMake manages. An unknown generator keeps
// only the CMakeFiles requirement.
func generatorMainFile(args []string) string {
	return generatorMainFileForHost(args, runtime.GOOS)
}

func generatorMainFileForHost(args []string, hostOS string) string {
	if HasOption(args, "--preset") {
		return ""
	}
	generator := generatorFromArgs(args)
	if generator == "" {
		generator = os.Getenv("CMAKE_GENERATOR")
	}
	if generator == "" {
		generator, _ = generatorDefault(hostOS)
	}
	lower := strings.ToLower(generator)
	switch {
	case strings.Contains(lower, "ninja"):
		return "build.ninja"
	case strings.Contains(lower, "makefiles"):
		return "Makefile"
	default:
		return ""
	}
}

func generatorFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "-G" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if len(arg) > 2 && strings.HasPrefix(arg, "-G") {
			return arg[2:]
		}
	}
	return ""
}

func writeStamp(s Settings, args []string, fingerprint string) error {
	if fingerprint == "" {
		var err error
		fingerprint, err = computeFingerprint(s, args)
		if err != nil {
			return err
		}
	}
	cmakePath, _ := ResolveExecutable(s)
	return jsonio.Save(stampPath(s), stamp{
		Version:     StampVersion,
		Fingerprint: fingerprint,
		CMakePath:   cmakePath,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func computeFingerprint(s Settings, args []string) (string, error) {
	cmakePath, cmakeDigest := ResolveExecutable(s)
	if cmakePath == "" {
		return "", fmt.Errorf("cmake executable not found")
	}
	files, err := inputFiles(s)
	if err != nil {
		return "", err
	}
	return digestValue(fingerprint{
		Version:     StampVersion,
		HostOS:      s.HostOS,
		Args:        args,
		CMakePath:   cmakePath,
		CMakeDigest: cmakeDigest,
		Env:         configureEnv(s),
		Files:       files,
	})
}

func inputFiles(s Settings) ([]fileDigest, error) {
	if s.SourceDir == "" {
		return []fileDigest{}, nil
	}
	// SourceDir can be a symlink (local SetGit packages); WalkDir does not
	// descend into a symlinked root, so scan the resolved tree.
	scanRoot := resolveSymlinks(s.SourceDir)
	// Exclude the invocation's build root instead of assuming <SourceDir>/build:
	// a remote package's SourceDir is its upstream checkout while its build root
	// is <pkgRoot>/out/<memberDir>/<keyDir>. The parent of BuildRoot covers the sibling
	// build keys of the same project; an unrelated upstream build/ directory must
	// stay in the fingerprint.
	roots := []string{s.BuildRoot, s.BuildDir}
	if s.BuildRoot != "" {
		roots = append(roots, filepath.Dir(s.BuildRoot))
	}
	excluded := make(map[string]bool)
	for _, dir := range roots {
		if dir == "" {
			continue
		}
		if clean, ok := nestedPath(scanRoot, resolveSymlinks(dir)); ok {
			excluded[clean] = true
		}
	}
	files := []fileDigest{}
	err := filepath.WalkDir(scanRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path == scanRoot {
				return nil
			}
			name := entry.Name()
			if name == ".git" || name == ".vmake_deps" || name == "CMakeFiles" || excluded[filepath.Clean(path)] {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "cmake_install.cmake" || !isInputFile(entry.Name()) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(scanRoot, path)
		if err != nil {
			return nil
		}
		files = append(files, fileDigest{Rel: filepath.ToSlash(rel), Hash: hashBytes(data)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	return files, nil
}

func resolveSymlinks(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func isInputFile(name string) bool {
	switch name {
	case "CMakeLists.txt", "CMakePresets.json", "CMakeUserPresets.json":
		return true
	}
	return strings.HasSuffix(name, ".cmake") || strings.HasSuffix(name, ".cmake.in")
}

func nestedPath(root, dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	cleanRoot := filepath.Clean(root)
	cleanDir := filepath.Clean(dir)
	if cleanDir == cleanRoot {
		return "", false
	}
	if !strings.HasPrefix(cleanDir, cleanRoot+string(filepath.Separator)) {
		return "", false
	}
	return cleanDir, true
}

func ResolveExecutable(s Settings) (string, string) {
	path, err := exec.LookPathInEnvPath("cmake", commandPath(s))
	if err != nil || path == "" {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return path, ""
	}
	return path, hashBytes(data)
}

// hostOS is the host Settings.HostOS names, defaulting to the running host.
func (s Settings) hostOS() string {
	if s.HostOS != "" {
		return s.HostOS
	}
	return runtime.GOOS
}

// environment returns the environment the runner applies to cmake: the
// inherited process environment overlaid with the toolchain's additions.
// On Windows PATHEXT is read from here, so a toolchain-provided value wins over
// the process one exactly as it would for the executed command.
func environment(s Settings) map[string]string {
	env := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if s.hostOS() == "windows" {
			key = strings.ToUpper(key)
		}
		env[key] = value
	}
	if s.Toolchain != nil {
		for key, value := range s.Toolchain.CommandEnv() {
			if s.hostOS() == "windows" {
				key = strings.ToUpper(key)
			}
			env[key] = value
		}
	}
	return env
}

// generatorDefault is the one declaration of the generator vmake assumes when
// neither the caller nor CMAKE_GENERATOR picks one. pinned reports whether
// ConfigureArgs must pass it explicitly: CMake would pick a Visual Studio
// generator on Windows, while on Unix its own default already applies.
func generatorDefault(hostOS string) (generator string, pinned bool) {
	if hostOS == "" {
		hostOS = runtime.GOOS
	}
	if hostOS == "windows" {
		return "Ninja", true
	}
	return "Unix Makefiles", false
}

// commandPath returns the PATH the runner uses to resolve cmake: the
// toolchain-augmented PATH when the toolchain contributes one, otherwise the
// inherited process PATH.
func commandPath(s Settings) string {
	if s.Toolchain != nil {
		if path := s.Toolchain.CommandEnv()["PATH"]; path != "" {
			return path
		}
	}
	return os.Getenv("PATH")
}

func configureEnv(s Settings) map[string]string {
	// Fingerprint the environment the command actually receives, including the
	// toolchain-augmented PATH, but not unrelated session variables.
	inherited := environment(s)
	env := make(map[string]string)
	for key, value := range inherited {
		upper := strings.ToUpper(key)
		if envOverridden(upper) {
			continue
		}
		if upper == "PATH" || strings.HasPrefix(upper, "CMAKE_") || envsig.Allowed(upper) {
			if s.hostOS() == "windows" {
				key = upper
			}
			env[key] = value
		}
	}
	return env
}

// envOverridden lists variables vmake sets explicitly with -D or that
// CMake only reads while seeding a fresh cache. Hashing them would trigger
// reconfigures that cannot change the configure result.
func envOverridden(key string) bool {
	switch key {
	case "CC", "CXX", "AR", "CFLAGS", "CXXFLAGS", "CPPFLAGS", "LDFLAGS":
		return true
	}
	return false
}

func digestValue(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return hashBytes(data), nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
