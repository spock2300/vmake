package api

import (
	"path/filepath"
	"strings"

	"github.com/spock2300/vmake/internal/exec"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func (p *Package) MergedCFlags(extra ...string) string {
	all := append(append([]string{}, p.globalCFlags...), extra...)
	return strings.Join(all, " ")
}

func (p *Package) MergedCxxFlags(extra ...string) string {
	all := append(append([]string{}, p.globalCxxFlags...), extra...)
	return strings.Join(all, " ")
}

func (p *Package) MergedLdFlags(extra ...string) string {
	all := append(append([]string{}, p.globalLdFlags...), extra...)
	return strings.Join(all, " ")
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
		)
	}
	return args
}

func (p *Package) CrossTarget() string { return p.tc.Host }

func (p *Package) Env() map[string]string {
	return p.tc.Env()
}

func (p *Package) cmakeBuildType() string {
	if m, ok := p.CfgVals[ModeOptionName].(string); ok && m == ModeDebug {
		return "Debug"
	}
	return "Release"
}

func (p *Package) CMakeConfigure(extraArgs ...string) {
	args := []string{
		"-S", p.SrcDir(),
		"-B", p.dirs.BuildDir,
		"-DCMAKE_INSTALL_PREFIX=" + p.dirs.InstallDir,
	}
	if p.tc.Tools.CC != "" {
		args = append(args, "-DCMAKE_C_COMPILER="+p.tc.Tools.CC)
	}
	if p.tc.Tools.CXX != "" {
		args = append(args, "-DCMAKE_CXX_COMPILER="+p.tc.Tools.CXX)
	}
	args = append(args, "-DCMAKE_BUILD_TYPE="+p.cmakeBuildType())
	if p.CrossTarget() != "" {
		args = append(args,
			"-DCMAKE_SYSTEM_NAME=Linux",
			"-DCMAKE_C_COMPILER_TARGET="+p.CrossTarget(),
			"-DCMAKE_CXX_COMPILER_TARGET="+p.CrossTarget())
	}
	args = append(args, extraArgs...)
	p.Run("cmake", args...)
}

func (p *Package) CMakeBuild(args ...string) {
	buildArgs := []string{"--build", p.dirs.BuildDir}
	buildArgs = append(buildArgs, args...)
	p.Run("cmake", buildArgs...)
}

func (p *Package) CMakeInstall() {
	p.Run("cmake", "--install", p.dirs.BuildDir)
}

func (p *Package) Configure(extraArgs ...string) error {
	args := []string{"--prefix=" + p.dirs.InstallDir}
	if p.CrossTarget() != "" {
		args = append(args, "--host="+p.CrossTarget())
	}
	args = append(args, extraArgs...)
	return p.RunEnv(p.Env(), filepath.Join(p.SrcDir(), "configure"), args...)
}

func (p *Package) Make(args ...string) error {
	makeArgs := []string{"-C", p.dirs.BuildDir}
	makeArgs = append(makeArgs, args...)
	return p.RunEnv(p.Env(), "make", makeArgs...)
}

func (p *Package) logAndDryRun(name string, args []string) bool {
	vlog.Info("  %s %s", name, strings.Join(args, " "))
	return p.dryRun
}

func (p *Package) Run(name string, args ...string) {
	if p.logAndDryRun(name, args) {
		return
	}
	exec.RunFatal(p.dirs.BuildDir, name, args...)
}

func (p *Package) RunIn(dir, name string, args ...string) {
	vlog.Info("  cd %s && %s %s", dir, name, strings.Join(args, " "))
	if p.dryRun {
		return
	}
	exec.RunFatal(dir, name, args...)
}

func (p *Package) RunEnv(env map[string]string, name string, args ...string) error {
	if p.logAndDryRun(name, args) {
		return nil
	}
	return exec.RunWithEnv(p.dirs.BuildDir, env, name, args...)
}
