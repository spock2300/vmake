package api

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
	"github.com/spock2300/vmake/internal/jsonio"
	vlog "github.com/spock2300/vmake/pkg/log"
)

const (
	cmakeConfigureStampVersion = 1
	cmakeConfigureStampName    = ".vmake_configure.json"
)

type cmakeConfigureStamp struct {
	Version     int    `json:"version"`
	Fingerprint string `json:"fingerprint"`
	CMakePath   string `json:"cmakePath,omitempty"`
	GeneratedAt string `json:"generatedAt,omitempty"`
}

type cmakeFingerprint struct {
	Version     int               `json:"version"`
	HostOS      string            `json:"hostOS"`
	Args        []string          `json:"args"`
	CMakePath   string            `json:"cmakePath"`
	CMakeDigest string            `json:"cmakeDigest"`
	Env         map[string]string `json:"env"`
	Files       []cmakeFileDigest `json:"files"`
}

type cmakeFileDigest struct {
	Rel  string `json:"rel"`
	Hash string `json:"hash"`
}

func (p *Package) cmakeConfigureStampPath() string {
	return filepath.Join(p.CMakeBuildDir(), cmakeConfigureStampName)
}

// cmakeConfigureState reports whether the recorded configure output can be
// reused and returns the fingerprint computed for the current tree. A required
// reconfigure can pass the returned fingerprint to saveCMakeConfigureStamp
// instead of walking the source tree a second time; it is empty when it was not
// computed (dry run, unusable stamp, missing configure output, hashing error).
func (p *Package) cmakeConfigureState(args []string) (string, bool) {
	if p.dryRun {
		return "", false
	}
	var stamp cmakeConfigureStamp
	if err := jsonio.Load(p.cmakeConfigureStampPath(), &stamp); err != nil {
		return "", false
	}
	if stamp.Version != cmakeConfigureStampVersion || stamp.Fingerprint == "" {
		return "", false
	}
	if !p.cmakeConfigureOutputPresent(args) {
		return "", false
	}
	fingerprint, err := p.cmakeConfigureFingerprint(args)
	if err != nil {
		vlog.Debug("  cmake configure: fingerprint unavailable: %v", err)
		return "", false
	}
	if fingerprint != stamp.Fingerprint {
		vlog.Debug("  cmake configure: fingerprint changed for %s", filepath.ToSlash(p.CMakeBuildDir()))
		return fingerprint, false
	}
	return fingerprint, true
}

// cmakeConfigureOutputPresent reports whether the configured build tree still
// holds what `cmake --build` needs: the cache, the generator's CMakeFiles
// bookkeeping and, when the generator is known, its top-level build file. A
// hand-deleted build.ninja or Makefile must force configure rather than fail a
// later build.
func (p *Package) cmakeConfigureOutputPresent(args []string) bool {
	buildDir := p.CMakeBuildDir()
	if !isRegularFile(filepath.Join(buildDir, "CMakeCache.txt")) {
		return false
	}
	if info, err := os.Stat(filepath.Join(buildDir, "CMakeFiles")); err != nil || !info.IsDir() {
		return false
	}
	main := cmakeGeneratorMainFile(args)
	if main == "" {
		return true
	}
	return isRegularFile(filepath.Join(buildDir, main))
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// cmakeGeneratorMainFile returns the top-level build file the configured
// generator writes next to CMakeCache.txt, or "" when the generator cannot be
// determined (configure presets) or is not one VMake manages. An unknown
// generator keeps only the CMakeFiles requirement.
func cmakeGeneratorMainFile(args []string) string {
	if hasCMakeOption(args, "--preset") {
		return ""
	}
	generator := cmakeGeneratorFromArgs(args)
	if generator == "" {
		generator = os.Getenv("CMAKE_GENERATOR")
	}
	if generator == "" {
		if runtime.GOOS == "windows" {
			// cmakeConfigureArgs requests Ninja on Windows when the caller did
			// not; Unix hosts keep CMake's own default.
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

func cmakeGeneratorFromArgs(args []string) string {
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

func (p *Package) saveCMakeConfigureStamp(args []string, fingerprint string) error {
	if fingerprint == "" {
		var err error
		fingerprint, err = p.cmakeConfigureFingerprint(args)
		if err != nil {
			return err
		}
	}
	cmakePath, _ := p.resolveCMakeExecutable()
	return jsonio.Save(p.cmakeConfigureStampPath(), cmakeConfigureStamp{
		Version:     cmakeConfigureStampVersion,
		Fingerprint: fingerprint,
		CMakePath:   cmakePath,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func (p *Package) cmakeConfigureFingerprint(args []string) (string, error) {
	cmakePath, cmakeDigest := p.resolveCMakeExecutable()
	if cmakePath == "" {
		return "", fmt.Errorf("cmake executable not found")
	}
	files, err := p.cmakeConfigureInputFiles()
	if err != nil {
		return "", err
	}
	return digestCMakeValue(cmakeFingerprint{
		Version:     cmakeConfigureStampVersion,
		HostOS:      runtime.GOOS,
		Args:        args,
		CMakePath:   cmakePath,
		CMakeDigest: cmakeDigest,
		Env:         p.cmakeConfigureEnv(),
		Files:       files,
	})
}

func (p *Package) cmakeConfigureInputFiles() ([]cmakeFileDigest, error) {
	root := p.SrcDir()
	if root == "" {
		return []cmakeFileDigest{}, nil
	}
	// SrcDir() can be a symlink (local SetGit packages); WalkDir does not descend
	// into a symlinked root, so scan the resolved tree.
	scanRoot := resolveCMakeSymlinks(root)
	// Exclude the invocation's build root instead of assuming <SourceDir>/build:
	// a remote package's SourceDir is its upstream checkout while its build root
	// is <pkgRoot>/out/<owner>/<key>. The parent of BuildDir covers the sibling
	// build keys of the same project; an unrelated upstream build/ directory must
	// stay in the fingerprint.
	roots := []string{p.dirs.BuildDir, p.CMakeBuildDir()}
	if p.dirs.BuildDir != "" {
		roots = append(roots, filepath.Dir(p.dirs.BuildDir))
	}
	excluded := make(map[string]bool)
	for _, dir := range roots {
		if dir == "" {
			continue
		}
		if clean, ok := nestedCMakePath(scanRoot, resolveCMakeSymlinks(dir)); ok {
			excluded[clean] = true
		}
	}
	files := []cmakeFileDigest{}
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
		if entry.Name() == "cmake_install.cmake" || !isCMakeInputFile(entry.Name()) {
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
		files = append(files, cmakeFileDigest{Rel: filepath.ToSlash(rel), Hash: hashCMakeBytes(data)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	return files, nil
}

func resolveCMakeSymlinks(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func isCMakeInputFile(name string) bool {
	switch name {
	case "CMakeLists.txt", "CMakePresets.json", "CMakeUserPresets.json":
		return true
	}
	return strings.HasSuffix(name, ".cmake") || strings.HasSuffix(name, ".cmake.in")
}

func nestedCMakePath(root, dir string) (string, bool) {
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

func (p *Package) resolveCMakeExecutable() (string, string) {
	path, err := lookPathInPathValue("cmake", p.cmakeCommandEnvironment(), p.cmakeCommandPath())
	if err != nil || path == "" {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return path, ""
	}
	return path, hashCMakeBytes(data)
}

// cmakeCommandEnvironment returns the environment the runner applies to cmake:
// the inherited process environment overlaid with the toolchain's additions.
// On Windows PATHEXT is read from here, so a toolchain-provided value wins over
// the process one exactly as it would for the executed command.
func (p *Package) cmakeCommandEnvironment() map[string]string {
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
	if p.tc != nil {
		for key, value := range p.tc.CommandEnv() {
			if runtime.GOOS == "windows" {
				key = strings.ToUpper(key)
			}
			env[key] = value
		}
	}
	return env
}

// cmakeCommandPath returns the PATH the runner uses to resolve cmake: the
// toolchain-augmented PATH when the toolchain contributes one, otherwise the
// inherited process PATH.
func (p *Package) cmakeCommandPath() string {
	if p.tc != nil {
		if path := p.tc.CommandEnv()["PATH"]; path != "" {
			return path
		}
	}
	return os.Getenv("PATH")
}

// lookPathInPathValue resolves name against pathValue with the same candidate
// generation as internal/exec's command lookup: with PATHEXT in effect an
// extensionless bare name is not a candidate.
func lookPathInPathValue(name string, env map[string]string, pathValue string) (string, error) {
	var suffixes []string
	if runtime.GOOS == "windows" {
		suffixes = pathextSuffixes(env["PATHEXT"])
	}
	candidates := lookPathCandidates(name, suffixes)
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			continue
		}
		for _, candidate := range candidates {
			path := filepath.Join(dir, candidate)
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				continue
			}
			if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
				continue
			}
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found in PATH", name)
}

// pathextSuffixes splits PATHEXT into lookup suffixes, falling back to the
// Windows default when the value is empty. Every suffix keeps a leading dot.
func pathextSuffixes(value string) []string {
	if value == "" {
		value = ".COM;.EXE;.BAT;.CMD"
	}
	var suffixes []string
	for _, extension := range strings.Split(value, ";") {
		if extension == "" {
			continue
		}
		if extension[0] != '.' {
			extension = "." + extension
		}
		suffixes = append(suffixes, extension)
	}
	return suffixes
}

// lookPathCandidates lists the file names to try for name. The bare name is
// only tried when it already carries an extension or no PATHEXT suffixes apply.
func lookPathCandidates(name string, suffixes []string) []string {
	candidates := make([]string, 0, len(suffixes)+1)
	if filepath.Ext(name) != "" || len(suffixes) == 0 {
		candidates = append(candidates, name)
	}
	for _, suffix := range suffixes {
		candidates = append(candidates, name+suffix)
	}
	return candidates
}

func (p *Package) cmakeConfigureEnv() map[string]string {
	// Fingerprint the environment the command actually receives, including the
	// toolchain-augmented PATH, but not unrelated session variables.
	inherited := p.cmakeCommandEnvironment()
	env := make(map[string]string)
	for key, value := range inherited {
		upper := strings.ToUpper(key)
		if cmakeEnvOverridden(upper) {
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

// cmakeEnvOverridden lists variables vmake sets explicitly with -D or that
// CMake only reads while seeding a fresh cache. Hashing them would trigger
// reconfigures that cannot change the configure result.
func cmakeEnvOverridden(key string) bool {
	switch key {
	case "CC", "CXX", "AR", "CFLAGS", "CXXFLAGS", "CPPFLAGS", "LDFLAGS":
		return true
	}
	return false
}

func digestCMakeValue(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return hashCMakeBytes(data), nil
}

func hashCMakeBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
