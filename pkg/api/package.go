package api

import (
	"fmt"
	"path/filepath"
	"strings"

	"gitee.com/spock2300/vmake/internal/exec"
	"gitee.com/spock2300/vmake/internal/fs"
	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/toolchain"
)

type TargetKind string

const (
	TargetBinary TargetKind = "binary"
	TargetStatic TargetKind = "static"
	TargetShared TargetKind = "shared"
	TargetObject TargetKind = "object"
	TargetVoid   TargetKind = "void"
)

type OptionType int

const (
	OptionBool OptionType = iota
	OptionString
	OptionInt
	OptionChoice
)

func (t OptionType) String() string {
	switch t {
	case OptionBool:
		return "bool"
	case OptionString:
		return "string"
	case OptionInt:
		return "int"
	case OptionChoice:
		return "choice"
	default:
		return "unknown"
	}
}

type ConfigFunc func(ctx *ConfigContext)
type BuildFunc func(ctx *BuildContext)
type InstallFunc func(ctx *InstallContext)
type PackageFunc func(p *Package)

type SourceOrigin int

const (
	SourceLocal SourceOrigin = iota
	SourceRemote
)

type PackageMeta struct {
	Repo string
	Name string
}

func (m *PackageMeta) FullName() string {
	if m.Repo == "" {
		return m.Name
	}
	return m.Repo + "/" + m.Name
}

type Package struct {
	PackageMeta
	ConfigAccessor
	*TargetRegistry
	installHolder InstallItemHolder
	gitURLs       []string
	homepage      string
	description   string
	license       string
	versions      map[string]string
	submodules    bool
	requireCtx    *PackageRequireContext
	requireFuncs  []RequireFunc
	libs          []string
	configFuncs   []ConfigFunc
	buildFuncs    []BuildFunc
	installFuncs  []InstallFunc
	packageFunc   PackageFunc
	scriptDir     string
	sourceDir     string
	buildDir      string
	installDir    string
	outputDir     string
	sourceOrigin  SourceOrigin
	cfgVals       map[string]any
	tc            *toolchain.Toolchain
	deps          map[string]*InstalledPackage
	patches       []string
}

func NewPackage() *Package {
	return &Package{
		ConfigAccessor: NewConfigAccessor(nil, nil),
		TargetRegistry: NewTargetRegistry(),
		versions:       make(map[string]string),
		requireCtx:     NewPackageRequireContext(),
		deps:           make(map[string]*InstalledPackage),
	}
}

func (p *Package) OnRequire(fn RequireFunc) *Package {
	p.requireFuncs = append(p.requireFuncs, fn)
	return p
}

func (p *Package) OnConfig(fn ConfigFunc) *Package {
	p.configFuncs = append(p.configFuncs, fn)
	return p
}

func (p *Package) OnBuild(fn BuildFunc) *Package {
	p.buildFuncs = append(p.buildFuncs, fn)
	return p
}

func (p *Package) OnInstall(fn InstallFunc) *Package {
	p.installFuncs = append(p.installFuncs, fn)
	return p
}

func (p *Package) OnPackage(fn PackageFunc) *Package {
	p.packageFunc = fn
	return p
}

func (p *Package) SetGit(urls ...string) *Package {
	p.gitURLs = urls
	return p
}

func (p *Package) SetHomepage(url string) *Package {
	p.homepage = url
	return p
}

func (p *Package) SetDescription(desc string) *Package {
	p.description = desc
	return p
}

func (p *Package) SetLicense(license string) *Package {
	p.license = license
	return p
}

func (p *Package) AddVersion(version, ref string) *Package {
	p.versions[version] = ref
	return p
}

func (p *Package) SetVersions(versions map[string]string) *Package {
	p.versions = versions
	return p
}

func (p *Package) SetSubmodules(v bool) *Package {
	p.submodules = v
	return p
}

func (p *Package) SetLibs(libs ...string) *Package {
	p.libs = libs
	return p
}

func (p *Package) SetRepo(repo string) *Package {
	p.Repo = repo
	return p
}

func (p *Package) SetName(name string) *Package {
	p.Name = name
	return p
}

func (p *Package) GetVersions() []string {
	versions := make([]string, 0, len(p.versions))
	for v := range p.versions {
		versions = append(versions, v)
	}
	return versions
}

func (p *Package) SelectVersion(constraint string) (string, error) {
	versions := p.GetVersions()
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions available for %s", p.FullName())
	}
	version, ok := MatchVersion(versions, constraint)
	if !ok {
		return "", fmt.Errorf("no version matching %s for %s (available: %v)", constraint, p.FullName(), versions)
	}
	return version, nil
}

func (p *Package) PackageName() string { return "" }

func (p *Package) GitURLs() []string                         { return p.gitURLs }
func (p *Package) Homepage() string                          { return p.homepage }
func (p *Package) Description() string                       { return p.description }
func (p *Package) License() string                           { return p.license }
func (p *Package) Versions() map[string]string               { return p.versions }
func (p *Package) Submodules() bool                          { return p.submodules }
func (p *Package) GetOptions() map[string]*Option            { return p.Options }
func (p *Package) GetRequireContext() *PackageRequireContext { return p.requireCtx }
func (p *Package) GetRef(version string) string              { return p.versions[version] }
func (p *Package) Libs() []string                            { return p.libs }
func (p *Package) GetRequireFuncs() []RequireFunc            { return p.requireFuncs }
func (p *Package) GetConfigFuncs() []ConfigFunc              { return p.configFuncs }
func (p *Package) GetBuildFuncs() []BuildFunc                { return p.buildFuncs }
func (p *Package) GetInstallFuncs() []InstallFunc            { return p.installFuncs }
func (p *Package) GetPackageFunc() PackageFunc               { return p.packageFunc }

func execFuncs[T any](dir string, funcs []T, fn func(T)) {
	execInDir(dir, func() {
		for _, f := range funcs {
			fn(f)
		}
	})
}

func (p *Package) ExecConfigFuncs(dir string, fn func(ConfigFunc)) {
	execFuncs(dir, p.configFuncs, fn)
}

func (p *Package) ExecBuildFuncs(dir string, fn func(BuildFunc)) {
	execFuncs(dir, p.buildFuncs, fn)
}

func (p *Package) ExecInstallFuncs(dir string, fn func(InstallFunc)) {
	execFuncs(dir, p.installFuncs, fn)
}

func (p *Package) ExecPackageFunc(dir string) {
	if p.packageFunc == nil {
		return
	}
	execInDir(dir, func() {
		p.packageFunc(p)
	})
}

func (p *Package) UpdateRequireContext(cfgVals map[string]any, options map[string]*Option) {
	if len(p.requireFuncs) == 0 {
		return
	}
	ctx := NewRequireContextForConfig(cfgVals, options, nil)
	for _, fn := range p.requireFuncs {
		fn(ctx)
	}
	p.requireCtx = &PackageRequireContext{requiresHolder: requiresHolder{requires: ctx.GetRequires()}}
}

func (p *Package) AddInstalls(src, dest string) *Package {
	p.installHolder.addInstall(src, dest)
	return p
}

func (p *Package) GetInstallItems() []InstallItem {
	return p.installHolder.getInstallItems()
}

func (p *Package) SetInstallFilter(filter InstallFilterFunc) *Package {
	p.installHolder.setInstallFilter(filter)
	return p
}

func (p *Package) GetInstallFilter() InstallFilterFunc {
	return p.installHolder.getInstallFilter()
}

func (p *Package) SetDeps(deps map[string]*InstalledPackage) *Package {
	p.deps = deps
	return p
}

func (p *Package) SetDep(name string, pkg *InstalledPackage) {
	if p.deps == nil {
		p.deps = make(map[string]*InstalledPackage)
	}
	p.deps[name] = pkg
}

func (p *Package) Deps() map[string]*InstalledPackage {
	return p.deps
}

func (p *Package) SetDirs(sourceDir, buildDir, installDir string) *Package {
	p.sourceDir = sourceDir
	p.buildDir = buildDir
	p.installDir = installDir
	return p
}

func (p *Package) SetScriptDir(dir string) {
	p.scriptDir = dir
}

func (p *Package) AddPatches(paths ...string) *Package {
	p.patches = append(p.patches, paths...)
	return p
}

func (p *Package) SetPatches(paths ...string) *Package {
	p.patches = paths
	return p
}

func (p *Package) SetOutputDir(dir string) *Package {
	p.outputDir = dir
	return p
}

func (p *Package) SetSourceOrigin(o SourceOrigin) *Package {
	p.sourceOrigin = o
	return p
}

func (p *Package) SetCfgVals(vals map[string]any) *Package {
	p.cfgVals = vals
	return p
}

func (p *Package) SetToolchain(tc *toolchain.Toolchain) *Package {
	p.tc = tc
	return p
}

func (p *Package) ScriptDir() string    { return p.scriptDir }
func (p *Package) SourceDir() string    { return p.sourceDir }
func (p *Package) BuildDir() string     { return p.buildDir }
func (p *Package) InstallDir() string   { return p.installDir }
func (p *Package) OutputDir() string    { return p.outputDir }
func (p *Package) IsLocal() bool        { return p.sourceOrigin == SourceLocal }
func (p *Package) GetPatches() []string { return p.patches }

func (p *Package) CC() string          { return p.tc.Tools.CC }
func (p *Package) CXX() string         { return p.tc.Tools.CXX }
func (p *Package) AR() string          { return p.tc.Tools.AR }
func (p *Package) CrossTarget() string { return p.tc.Host }
func (p *Package) Prefix() string      { return p.tc.Prefix }
func (p *Package) CFlags() string      { return strings.Join(p.tc.DefaultFlags.CFlags, " ") }
func (p *Package) CXXFlags() string    { return strings.Join(p.tc.DefaultFlags.CxxFlags, " ") }
func (p *Package) LDFlags() string     { return strings.Join(p.tc.DefaultFlags.LdFlags, " ") }

func (p *Package) Env() map[string]string {
	return p.tc.Env()
}

func (p *Package) CMakeConfigure(extraArgs ...string) error {
	args := []string{
		"-S", p.sourceDir,
		"-B", p.buildDir,
		"-DCMAKE_INSTALL_PREFIX=" + p.installDir,
	}
	if p.tc.Tools.CC != "" {
		args = append(args, "-DCMAKE_C_COMPILER="+p.tc.Tools.CC)
	}
	if p.tc.Tools.CXX != "" {
		args = append(args, "-DCMAKE_CXX_COMPILER="+p.tc.Tools.CXX)
	}
	args = append(args, "-DCMAKE_BUILD_TYPE=Release")
	if p.CrossTarget() != "" {
		args = append(args,
			"-DCMAKE_SYSTEM_NAME=Linux",
			"-DCMAKE_C_COMPILER_TARGET="+p.CrossTarget(),
			"-DCMAKE_CXX_COMPILER_TARGET="+p.CrossTarget())
	}
	args = append(args, extraArgs...)
	return p.Run("cmake", args...)
}

func (p *Package) CMakeBuild(args ...string) error {
	buildArgs := []string{"--build", p.buildDir}
	buildArgs = append(buildArgs, args...)
	return p.Run("cmake", buildArgs...)
}

func (p *Package) CMakeInstall() error {
	return p.Run("cmake", "--install", p.buildDir)
}

func (p *Package) Configure(extraArgs ...string) error {
	args := []string{"--prefix=" + p.installDir}
	if p.CrossTarget() != "" {
		args = append(args, "--host="+p.CrossTarget())
	}
	args = append(args, extraArgs...)
	return p.RunEnv(p.Env(), p.sourceDir+"/configure", args...)
}

func (p *Package) Make(args ...string) error {
	makeArgs := []string{"-C", p.buildDir}
	makeArgs = append(makeArgs, args...)
	return p.Run("make", makeArgs...)
}

func (p *Package) Run(name string, args ...string) error {
	vlog.Info("  %s %s", name, strings.Join(args, " "))
	exec.RunFatal(p.buildDir, name, args...)
	return nil
}

func (p *Package) RunIn(dir, name string, args ...string) error {
	vlog.Info("  cd %s && %s %s", dir, name, strings.Join(args, " "))
	exec.RunFatal(dir, name, args...)
	return nil
}

func (p *Package) RunEnv(env map[string]string, name string, args ...string) error {
	vlog.Info("  %s %s", name, strings.Join(args, " "))
	return exec.RunWithEnv(p.buildDir, env, name, args...)
}

type InstalledPackage struct {
	Name       string
	Version    string
	InstallDir string
	IncludeDir string
	LibDir     string
	BinDir     string
	Libs       []string
	Deps       []string
}

func (ip *InstalledPackage) UpdateLibDir() {
	ip.LibDir = fs.DetectLibDir(ip.InstallDir)
}

func NewInstalledPackage(name, version, installDir string, libs []string) *InstalledPackage {
	return &InstalledPackage{
		Name:       name,
		Version:    version,
		InstallDir: installDir,
		IncludeDir: filepath.Join(installDir, "include"),
		LibDir:     fs.DetectLibDir(installDir),
		BinDir:     filepath.Join(installDir, "bin"),
		Libs:       libs,
	}
}

func SplitPackageRef(ref string) (repo, name string, ok bool) {
	idx := strings.Index(ref, "/")
	if idx < 0 {
		return "", ref, false
	}
	return ref[:idx], ref[idx+1:], true
}
