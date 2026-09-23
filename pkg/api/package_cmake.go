package api

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spock2300/vmake/pkg/toolchain"
)

func (p *Package) SetCMakeBuildDir(dir string) *Package {
	if dir == "" {
		fatalScript(p.Name, "SetCMakeBuildDir", "directory must not be empty")
	}
	p.cmakeBuildDir = dir
	return p
}

func (p *Package) SetCMakeInstallDir(dir string) *Package {
	if dir == "" {
		fatalScript(p.Name, "SetCMakeInstallDir", "directory must not be empty")
	}
	p.cmakeInstallDir = dir
	return p
}

func (p *Package) SetCMakeBuildType(config string) *Package {
	if strings.TrimSpace(config) == "" {
		fatalScript(p.Name, "SetCMakeBuildType", "build type must not be empty")
	}
	p.cmakeConfig = config
	return p
}

func (p *Package) CMakeBuildDir() string {
	if p.cmakeBuildDir == "" {
		return filepath.Join(p.dirs.BuildDir, "cmake")
	}
	return p.cmakeDir(p.cmakeBuildDir)
}

func (p *Package) CMakeInstallDir() string {
	if p.cmakeInstallDir != "" {
		return p.cmakeDir(p.cmakeInstallDir)
	}
	if p.dirs.InstallDir != "" {
		return p.dirs.InstallDir
	}
	return filepath.Join(p.dirs.BuildDir, "staging")
}

func (p *Package) CMakeGlobalFlagsArgs() []string {
	var args []string
	if cf := p.MergedCFlags(); cf != "" {
		args = append(args, "-DCMAKE_C_FLAGS="+cf)
	}
	if cxxf := p.MergedCxxFlags(); cxxf != "" {
		args = append(args, "-DCMAKE_CXX_FLAGS="+cxxf)
	}
	if ldf := p.MergedLdFlags(); ldf != "" {
		args = append(args,
			"-DCMAKE_EXE_LINKER_FLAGS="+ldf,
			"-DCMAKE_SHARED_LINKER_FLAGS="+ldf,
			"-DCMAKE_MODULE_LINKER_FLAGS="+ldf,
		)
	}
	return args
}

func (p *Package) CMakeConfigure(extraArgs ...string) {
	args, err := p.cmakeConfigureArgs(runtime.GOOS, extraArgs...)
	if err != nil {
		fatalScript(p.Name, "CMakeConfigure", "%v", err)
	}
	if hasCMakeOption(extraArgs, "--preset") {
		p.RunIn(p.SrcDir(), "cmake", args...)
		return
	}
	p.Run("cmake", args...)
}

func (p *Package) CMakeBuild(extraArgs ...string) {
	args, err := p.cmakeBuildArgs(extraArgs...)
	if err != nil {
		fatalScript(p.Name, "CMakeBuild", "%v", err)
	}
	p.Run("cmake", args...)
}

func (p *Package) CMakeInstall(extraArgs ...string) {
	args, err := p.cmakeInstallArgs(extraArgs...)
	if err != nil {
		fatalScript(p.Name, "CMakeInstall", "%v", err)
	}
	p.Run("cmake", args...)
}

func (p *Package) cmakeDir(dir string) string {
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(p.dirs.BuildDir, dir)
}

func (p *Package) cmakeBuildType() string {
	if p.cmakeConfig != "" {
		return p.cmakeConfig
	}
	if m, ok := p.CfgVals[ModeOptionName].(string); ok && m == ModeDebug {
		return "Debug"
	}
	return "Release"
}

func (p *Package) cmakeConfigureArgs(hostOS string, extraArgs ...string) ([]string, error) {
	if err := validateCMakeArgs(extraArgs); err != nil {
		return nil, err
	}
	if p.tc == nil {
		return nil, fmt.Errorf("toolchain is not configured")
	}
	args := []string{
		"-S", filepath.ToSlash(p.SrcDir()),
		"-B", filepath.ToSlash(p.CMakeBuildDir()),
		"-DCMAKE_INSTALL_PREFIX=" + filepath.ToSlash(p.CMakeInstallDir()),
	}
	for _, tool := range []struct{ key, name string }{
		{"CMAKE_C_COMPILER", p.tc.Tools.CC},
		{"CMAKE_CXX_COMPILER", p.tc.Tools.CXX},
		{"CMAKE_ASM_COMPILER", p.tc.Tools.CC},
		{"CMAKE_AR", p.tc.Tools.AR},
		{"CMAKE_RANLIB", p.tc.Tools.RANLIB},
		{"CMAKE_STRIP", p.tc.Tools.STRIP},
		{"CMAKE_OBJCOPY", p.tc.Tools.OBJCOPY},
		{"CMAKE_NM", p.tc.Tools.NM},
	} {
		if tool.name == "" {
			continue
		}
		path, err := toolchain.ResolveToolPath(tool.name, p.tc.InstallPath)
		if err != nil {
			return nil, fmt.Errorf("resolve %s %q: %w", tool.key, tool.name, err)
		}
		args = append(args, "-D"+tool.key+"="+filepath.ToSlash(path))
	}
	args = append(args, "-DCMAKE_BUILD_TYPE="+p.cmakeBuildType())
	targetOS := p.TargetOS()
	if p.TargetTriple() != "" || targetOS != hostOS {
		systemNames := map[string]string{
			"none": "Generic", "linux": "Linux", "windows": "Windows",
			"darwin": "Darwin", "freebsd": "FreeBSD", "openbsd": "OpenBSD",
			"netbsd": "NetBSD", "android": "Android",
		}
		systemName, ok := systemNames[targetOS]
		if !ok {
			return nil, fmt.Errorf("unsupported CMake target OS %q", targetOS)
		}
		args = append(args, "-DCMAKE_SYSTEM_NAME="+systemName)
	}
	if targetOS == "none" {
		args = append(args,
			"-DCMAKE_TRY_COMPILE_TARGET_TYPE=STATIC_LIBRARY",
			"-DCMAKE_FIND_ROOT_PATH_MODE_PROGRAM=NEVER",
			"-DCMAKE_FIND_ROOT_PATH_MODE_LIBRARY=ONLY",
			"-DCMAKE_FIND_ROOT_PATH_MODE_INCLUDE=ONLY",
		)
	}
	if p.TargetTriple() != "" {
		args = append(args,
			"-DCMAKE_C_COMPILER_TARGET="+p.TargetTriple(),
			"-DCMAKE_CXX_COMPILER_TARGET="+p.TargetTriple(),
			"-DCMAKE_ASM_COMPILER_TARGET="+p.TargetTriple(),
		)
	}
	if hostOS == "windows" && !hasCMakeGenerator(extraArgs) && os.Getenv("CMAKE_GENERATOR") == "" {
		args = append(args, "-G", "Ninja")
	}
	definitions := cmakeDefinitions(extraArgs)
	for _, arg := range p.CMakeGlobalFlagsArgs() {
		if !definitions[cmakeDefinitionKey(strings.TrimPrefix(arg, "-D"))] {
			args = append(args, arg)
		}
	}
	args = append(args, extraArgs...)
	return args, nil
}

func (p *Package) cmakeBuildArgs(extraArgs ...string) ([]string, error) {
	if err := validateCMakeArgs(extraArgs); err != nil {
		return nil, err
	}
	if hasCMakeOption(extraArgs, "--preset") {
		return nil, fmt.Errorf("CMake build presets override the managed build directory; pass configure presets to CMakeConfigure and use --target or --config with CMakeBuild")
	}
	args := []string{"--build", filepath.ToSlash(p.CMakeBuildDir())}
	if !hasCMakeOption(extraArgs, "--config") {
		args = append(args, "--config", p.cmakeBuildType())
	}
	parallelArgs, err := p.executionBudget().CMakeBuildArgs(extraArgs, parallelEnvironment())
	if err != nil {
		return nil, err
	}
	return append(args, parallelArgs...), nil
}

func (p *Package) cmakeInstallArgs(extraArgs ...string) ([]string, error) {
	if err := validateCMakeArgs(extraArgs); err != nil {
		return nil, err
	}
	args := []string{"--install", filepath.ToSlash(p.CMakeBuildDir()), "--prefix", filepath.ToSlash(p.CMakeInstallDir())}
	if !hasCMakeOption(extraArgs, "--config") {
		args = append(args, "--config", p.cmakeBuildType())
	}
	parallelArgs, err := p.executionBudget().CMakeInstallArgs(extraArgs, parallelEnvironment())
	if err != nil {
		return nil, err
	}
	return append(args, parallelArgs...), nil
}

func validateCMakeArgs(args []string) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "-D") {
			definition := strings.TrimPrefix(arg, "-D")
			if definition == "" && i+1 < len(args) {
				i++
				definition = args[i]
			}
			switch cmakeDefinitionKey(definition) {
			case "CMAKE_INSTALL_PREFIX":
				return fmt.Errorf("CMAKE_INSTALL_PREFIX is managed by VMake; use SetCMakeInstallDir")
			case "CMAKE_BUILD_TYPE":
				return fmt.Errorf("CMAKE_BUILD_TYPE is managed by VMake; use SetCMakeBuildType")
			}
			continue
		}
		if strings.HasPrefix(arg, "-B") || arg == "--build" || strings.HasPrefix(arg, "--build=") || arg == "--install" || strings.HasPrefix(arg, "--install=") {
			return fmt.Errorf("CMake build directory is managed by VMake; use SetCMakeBuildDir instead of %q", arg)
		}
		if arg == "--prefix" || strings.HasPrefix(arg, "--prefix=") || arg == "--install-prefix" || strings.HasPrefix(arg, "--install-prefix=") {
			return fmt.Errorf("CMake install prefix is managed by VMake; use SetCMakeInstallDir instead of %q", arg)
		}
	}
	return nil
}

func hasCMakeGenerator(args []string) bool {
	return hasCMakeOption(args, "-G", "--preset")
}

func hasCMakeOption(args []string, options ...string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "-D" {
			i++
			continue
		}
		for _, option := range options {
			if arg == option || strings.HasPrefix(arg, option+"=") || len(option) == 2 && strings.HasPrefix(arg, option) {
				return true
			}
		}
	}
	return false
}

func cmakeDefinitions(args []string) map[string]bool {
	definitions := make(map[string]bool)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-D") {
			continue
		}
		definition := strings.TrimPrefix(arg, "-D")
		if definition == "" && i+1 < len(args) {
			i++
			definition = args[i]
		}
		definitions[cmakeDefinitionKey(definition)] = true
	}
	return definitions
}

func cmakeDefinitionKey(definition string) string {
	key, _, _ := strings.Cut(definition, "=")
	key, _, _ = strings.Cut(key, ":")
	return key
}
