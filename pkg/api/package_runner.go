package api

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spock2300/vmake/internal/exec"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func (p *Package) MergedCFlags(extra ...string) string {
	all, _ := p.VisibilityFlags()
	all = append(all, p.globalCFlags...)
	all = append(all, extra...)
	return strings.Join(all, " ")
}

func (p *Package) MergedCxxFlags(extra ...string) string {
	_, all := p.VisibilityFlags()
	all = append(all, p.globalCxxFlags...)
	all = append(all, extra...)
	return strings.Join(all, " ")
}

func (p *Package) MergedLdFlags(extra ...string) string {
	all := append(append([]string{}, p.globalLdFlags...), extra...)
	return strings.Join(all, " ")
}

func (p *Package) Env() map[string]string {
	tc := *p.tc
	for _, tool := range []struct {
		name string
		path *string
	}{
		{"CC", &tc.Tools.CC}, {"CXX", &tc.Tools.CXX},
		{"AR", &tc.Tools.AR}, {"LD", &tc.Tools.LD},
		{"STRIP", &tc.Tools.STRIP}, {"RANLIB", &tc.Tools.RANLIB},
		{"OBJCOPY", &tc.Tools.OBJCOPY}, {"SIZE", &tc.Tools.SIZE},
		{"OBJDUMP", &tc.Tools.OBJDUMP}, {"NM", &tc.Tools.NM},
		{"MAKE", &tc.Tools.MAKE},
	} {
		if *tool.path == "" {
			continue
		}
		path, err := toolchain.ResolveToolPath(*tool.path, tc.InstallPath)
		if err != nil {
			fatalScript(p.Name, "Env", "resolve %s %q: %v", tool.name, *tool.path, err)
		}
		*tool.path = filepath.ToSlash(path)
	}
	if tc.Prefix != "" {
		if tc.InstallPath != "" && !filepath.IsAbs(tc.Prefix) {
			tc.Prefix = filepath.Join(tc.InstallPath, "bin", tc.Prefix)
		}
		tc.Prefix = filepath.ToSlash(tc.Prefix)
	}
	env := tc.Env()
	for key, value := range p.tc.CommandEnv() {
		env[key] = value
	}
	env["CFLAGS"] = strings.TrimSpace(p.CFlags() + " " + p.MergedCFlags())
	env["CXXFLAGS"] = strings.TrimSpace(p.CXXFlags() + " " + p.MergedCxxFlags())
	env["LDFLAGS"] = strings.TrimSpace(p.LDFlags() + " " + p.MergedLdFlags())
	if tc.Tools.STRIP != "" {
		env["STRIP"] = tc.Tools.STRIP
	}
	if tc.Tools.RANLIB != "" {
		env["RANLIB"] = tc.Tools.RANLIB
	}
	return env
}

func (p *Package) makeTool() string {
	if p.tc == nil || p.tc.Tools.MAKE == "" {
		return toolchain.MakeToolOf(p.tc)
	}
	path, err := toolchain.ResolveToolPath(p.tc.Tools.MAKE, p.tc.InstallPath)
	if err != nil {
		fatalScript(p.Name, "Make", "resolve %q: %v", p.tc.Tools.MAKE, err)
	}
	return path
}

func (p *Package) shellEnv(forMake bool) map[string]string {
	if p.tc == nil {
		return nil
	}
	env := p.Env()
	for _, key := range []string{"CC", "CXX", "AR", "LD", "STRIP", "RANLIB", "OBJCOPY", "SIZE", "OBJDUMP", "NM", "MAKE", "CROSS_COMPILE"} {
		if value := env[key]; value != "" {
			value = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(value) + `"`
			if forMake {
				value = strings.ReplaceAll(value, "$", "$$")
			}
			env[key] = value
		}
	}
	return env
}

func (p *Package) runMakeIn(dir string, args ...string) error {
	program := p.makeTool()
	if p.logAndDryRun(program, args) {
		return nil
	}
	return exec.RunWithEnv(dir, p.shellEnv(true), program, args...)
}

func (p *Package) Configure(extraArgs ...string) error {
	name, args := configureCommand(p.SrcDir())
	// Slash-separated prefix: it is an sh argv element, and the MSYS runtime
	// de-quotes backslashes there.
	args = append(args, "--prefix="+filepath.ToSlash(p.dirs.InstallDir))
	if p.TargetTriple() != "" {
		args = append(args, "--host="+p.TargetTriple())
	}
	args = append(args, extraArgs...)
	return p.RunEnv(p.shellEnv(false), name, args...)
}

func (p *Package) Make(args ...string) error {
	// Slash-separated -C path: MSYS make de-quotes backslashes in argv, and
	// forward slashes work for native make implementations too.
	makeArgs := []string{"-C", filepath.ToSlash(p.dirs.BuildDir)}
	makeArgs = append(makeArgs, args...)
	return p.runMakeIn(p.dirs.BuildDir, makeArgs...)
}

func (p *Package) logAndDryRun(name string, args []string) bool {
	vlog.Info("  %s %s", name, strings.Join(args, " "))
	return p.dryRun
}

func (p *Package) Run(name string, args ...string) {
	if p.logAndDryRun(name, args) {
		return
	}
	if err := p.runIn(p.dirs.BuildDir, nil, name, args...); err != nil {
		fatalScript(p.Name, "Run", "%v", err)
	}
}

func (p *Package) RunIn(dir, name string, args ...string) {
	vlog.Info("  cd %s && %s %s", dir, name, strings.Join(args, " "))
	if p.dryRun {
		return
	}
	if err := p.runIn(dir, nil, name, args...); err != nil {
		fatalScript(p.Name, "RunIn", "%v", err)
	}
}

func (p *Package) RunEnv(env map[string]string, name string, args ...string) error {
	if p.logAndDryRun(name, args) {
		return nil
	}
	return p.runIn(p.dirs.BuildDir, env, name, args...)
}

func (p *Package) runIn(dir string, extra map[string]string, name string, args ...string) error {
	env := p.tc.CommandEnv()
	for key, value := range extra {
		if runtime.GOOS == "windows" && strings.EqualFold(key, "PATH") {
			key = "PATH"
		}
		env[key] = value
	}
	if p.tc != nil {
		t := p.tc.Tools
		for _, configured := range []string{t.CC, t.CXX, t.AR, t.LD, t.STRIP, t.RANLIB, t.OBJCOPY, t.SIZE, t.OBJDUMP, t.NM, t.MAKE} {
			if configured != "" && name == configured {
				path, err := toolchain.ResolveToolPath(configured, p.tc.InstallPath)
				if err != nil {
					return err
				}
				name = path
				break
			}
		}
	}
	return exec.RunWithEnv(dir, env, name, args...)
}
