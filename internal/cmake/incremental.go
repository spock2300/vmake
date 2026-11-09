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

type Stamp struct {
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
	Files       []FileDigest      `json:"files"`
}

type FileDigest struct {
	Rel  string `json:"rel"`
	Hash string `json:"hash"`
}

func StampPath(s Settings) string {
	return filepath.Join(s.BuildDir, StampName)
}

// State reports whether the recorded configure output can be reused and
// returns the fingerprint computed for the current tree. A required
// reconfigure can pass the returned fingerprint to SaveStamp instead of
// walking the source tree a second time; it is empty when it was not
// computed (dry run, unusable stamp, missing configure output, hashing error).
func State(s Settings, args []string) (string, bool) {
	if s.DryRun {
		return "", false
	}
	var stamp Stamp
	if err := jsonio.Load(StampPath(s), &stamp); err != nil {
		return "", false
	}
	if stamp.Version != StampVersion || stamp.Fingerprint == "" {
		return "", false
	}
	if !OutputPresent(s, args) {
		return "", false
	}
	fingerprint, err := Fingerprint(s, args)
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

// OutputPresent reports whether the configured build tree still holds what
// `cmake --build` needs: the cache, the generator's CMakeFiles bookkeeping
// and, when the generator is known, its top-level build file. A hand-deleted
// build.ninja or Makefile must force configure rather than fail a later build.
func OutputPresent(s Settings, args []string) bool {
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

// GeneratorMainFile returns the top-level build file the configured generator
// writes next to CMakeCache.txt, or "" when the generator cannot be determined
// (configure presets) or is not one VMake manages. An unknown generator keeps
// only the CMakeFiles requirement.
func GeneratorMainFile(args []string) string {
	return generatorMainFileForHost(args, runtime.GOOS)
}

func generatorMainFileForHost(args []string, hostOS string) string {
	if HasOption(args, "--preset") {
		return ""
	}
	generator := GeneratorFromArgs(args)
	if generator == "" {
		generator = os.Getenv("CMAKE_GENERATOR")
	}
	if generator == "" {
		if hostOS == "" {
			hostOS = runtime.GOOS
		}
		if hostOS == "windows" {
			// ConfigureArgs requests Ninja on Windows when the caller did not;
			// Unix hosts keep CMake's own default.
			generator = "Ninja"
		} else {
			generator = "Unix Makefiles"
		}
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

func GeneratorFromArgs(args []string) string {
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

func SaveStamp(s Settings, args []string, fingerprint string) error {
	if fingerprint == "" {
		var err error
		fingerprint, err = Fingerprint(s, args)
		if err != nil {
			return err
		}
	}
	cmakePath, _ := ResolveExecutable(s)
	return jsonio.Save(StampPath(s), Stamp{
		Version:     StampVersion,
		Fingerprint: fingerprint,
		CMakePath:   cmakePath,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func Fingerprint(s Settings, args []string) (string, error) {
	cmakePath, cmakeDigest := ResolveExecutable(s)
	if cmakePath == "" {
		return "", fmt.Errorf("cmake executable not found")
	}
	files, err := InputFiles(s)
	if err != nil {
		return "", err
	}
	return DigestValue(fingerprint{
		Version:     StampVersion,
		HostOS:      s.HostOS,
		Args:        args,
		CMakePath:   cmakePath,
		CMakeDigest: cmakeDigest,
		Env:         ConfigureEnv(s),
		Files:       files,
	})
}

func InputFiles(s Settings) ([]FileDigest, error) {
	if s.SourceDir == "" {
		return []FileDigest{}, nil
	}
	// SourceDir can be a symlink (local SetGit packages); WalkDir does not
	// descend into a symlinked root, so scan the resolved tree.
	scanRoot := ResolveSymlinks(s.SourceDir)
	// Exclude the invocation's build root instead of assuming <SourceDir>/build:
	// a remote package's SourceDir is its upstream checkout while its build root
	// is <pkgRoot>/out/<owner>/<key>. The parent of BuildRoot covers the sibling
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
		if clean, ok := NestedPath(scanRoot, ResolveSymlinks(dir)); ok {
			excluded[clean] = true
		}
	}
	files := []FileDigest{}
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
		if entry.Name() == "cmake_install.cmake" || !IsInputFile(entry.Name()) {
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
		files = append(files, FileDigest{Rel: filepath.ToSlash(rel), Hash: HashBytes(data)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	return files, nil
}

func ResolveSymlinks(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func IsInputFile(name string) bool {
	switch name {
	case "CMakeLists.txt", "CMakePresets.json", "CMakeUserPresets.json":
		return true
	}
	return strings.HasSuffix(name, ".cmake") || strings.HasSuffix(name, ".cmake.in")
}

func NestedPath(root, dir string) (string, bool) {
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
	path, err := exec.LookPathInEnvPath("cmake", CommandPath(s))
	if err != nil || path == "" {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return path, ""
	}
	return path, HashBytes(data)
}

// Environment returns the environment the runner applies to cmake: the
// inherited process environment overlaid with the toolchain's additions.
// On Windows PATHEXT is read from here, so a toolchain-provided value wins over
// the process one exactly as it would for the executed command.
func Environment(s Settings) map[string]string {
	env := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		env[key] = value
	}
	if s.Toolchain != nil {
		for key, value := range s.Toolchain.CommandEnv() {
			if runtime.GOOS == "windows" {
				key = strings.ToUpper(key)
			}
			env[key] = value
		}
	}
	return env
}

// CommandPath returns the PATH the runner uses to resolve cmake: the
// toolchain-augmented PATH when the toolchain contributes one, otherwise the
// inherited process PATH.
func CommandPath(s Settings) string {
	if s.Toolchain != nil {
		if path := s.Toolchain.CommandEnv()["PATH"]; path != "" {
			return path
		}
	}
	return os.Getenv("PATH")
}

func ConfigureEnv(s Settings) map[string]string {
	// Fingerprint the environment the command actually receives, including the
	// toolchain-augmented PATH, but not unrelated session variables.
	inherited := Environment(s)
	env := make(map[string]string)
	for key, value := range inherited {
		upper := strings.ToUpper(key)
		if EnvOverridden(upper) {
			continue
		}
		if upper == "PATH" || strings.HasPrefix(upper, "CMAKE_") || envsig.Allowed(upper) {
			if runtime.GOOS == "windows" {
				key = upper
			}
			env[key] = value
		}
	}
	return env
}

// EnvOverridden lists variables vmake sets explicitly with -D or that
// CMake only reads while seeding a fresh cache. Hashing them would trigger
// reconfigures that cannot change the configure result.
func EnvOverridden(key string) bool {
	switch key {
	case "CC", "CXX", "AR", "CFLAGS", "CXXFLAGS", "CPPFLAGS", "LDFLAGS":
		return true
	}
	return false
}

func DigestValue(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return HashBytes(data), nil
}

func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
