package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	vlog "gitee.com/spock2300/vmake/pkg/log"
)

type Package struct {
	gitURLs          []string
	homepage         string
	description      string
	license          string
	versions         map[string]string
	options          map[string]*Option
	requireCtx       *PackageRequireContext
	requireFuncs     []RequireFunc
	libs             []string
	declaredPackages []string
	configFuncs      []ConfigFunc
	buildFuncs       []BuildFunc
	installFuncs     []InstallFunc
}

func NewPackage() *Package {
	return &Package{
		versions:   make(map[string]string),
		options:    make(map[string]*Option),
		requireCtx: NewPackageRequireContext(),
	}
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

func (p *Package) OnRequire(fn func(ctx *PackageRequireContext)) *Package {
	fn(p.requireCtx)
	return p
}

func (p *Package) Option(name string) *Option {
	if opt, ok := p.options[name]; ok {
		return opt
	}
	opt := &Option{name: name}
	p.options[name] = opt
	return opt
}

func (p *Package) SetConfigFuncs(funcs []ConfigFunc) *Package {
	p.configFuncs = funcs
	return p
}

func (p *Package) SetBuildFuncs(funcs []BuildFunc) *Package {
	p.buildFuncs = funcs
	return p
}

func (p *Package) SetInstallFuncs(funcs []InstallFunc) *Package {
	p.installFuncs = funcs
	return p
}

func (p *Package) SetRequireFuncs(funcs []RequireFunc) *Package {
	p.requireFuncs = funcs
	return p
}

func (p *Package) GetRequireFuncs() []RequireFunc { return p.requireFuncs }

// UpdateRequireContext re-runs OnRequire callbacks with actual config values.
// The result replaces the existing requireCtx.
func (p *Package) UpdateRequireContext(cfgVals map[string]any, options map[string]*Option) {
	if len(p.requireFuncs) == 0 {
		return
	}
	ctx := NewRequireContextForConfig(cfgVals, options, nil)
	for _, fn := range p.requireFuncs {
		fn(ctx)
	}
	p.requireCtx = &PackageRequireContext{requires: ctx.GetRequires()}
}

func (p *Package) SetLibs(libs ...string) *Package {
	p.libs = libs
	return p
}

func (p *Package) DeclarePackages(packages ...string) *Package {
	p.declaredPackages = append(p.declaredPackages, packages...)
	return p
}

func (p *Package) GitURLs() []string                         { return p.gitURLs }
func (p *Package) Homepage() string                          { return p.homepage }
func (p *Package) Description() string                       { return p.description }
func (p *Package) License() string                           { return p.license }
func (p *Package) Versions() map[string]string               { return p.versions }
func (p *Package) GetOptions() map[string]*Option            { return p.options }
func (p *Package) GetRequireContext() *PackageRequireContext { return p.requireCtx }
func (p *Package) GetRef(version string) string              { return p.versions[version] }
func (p *Package) Libs() []string                            { return p.libs }
func (p *Package) GetDeclaredPackages() []string             { return p.declaredPackages }
func (p *Package) GetConfigFuncs() []ConfigFunc              { return p.configFuncs }
func (p *Package) GetBuildFuncs() []BuildFunc                { return p.buildFuncs }
func (p *Package) GetInstallFuncs() []InstallFunc            { return p.installFuncs }

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

func NewInstalledPackage(name, version, installDir string, libs []string) *InstalledPackage {
	libDir := filepath.Join(installDir, "lib")
	lib64Dir := filepath.Join(installDir, "lib64")
	if _, err := os.Stat(lib64Dir); err == nil {
		libDir = lib64Dir
	}
	return &InstalledPackage{
		Name:       name,
		Version:    version,
		InstallDir: installDir,
		IncludeDir: filepath.Join(installDir, "include"),
		LibDir:     libDir,
		BinDir:     filepath.Join(installDir, "bin"),
		Libs:       libs,
	}
}

type OnDemandInstaller interface {
	EnsureInstalled(name string) *InstalledPackage
}

type PackageContext struct {
	ConfigAccessor
	gitURLs          []string
	homepage         string
	description      string
	license          string
	versions         map[string]string
	libs             []string
	declaredPackages []string
	pkgName          string
	version          string
	toolchain        *Toolchain
	deps             map[string]*InstalledPackage
	sourceDir        string
	buildDir         string
	installDir       string
	targets          map[string]*Target
	installer        OnDemandInstaller
}

func NewPackageContext(pkgName, version string, tc *Toolchain, cfgVals map[string]any) *PackageContext {
	return &PackageContext{
		ConfigAccessor: NewConfigAccessor(cfgVals, nil),
		pkgName:        pkgName,
		version:        version,
		toolchain:      tc,
		deps:           make(map[string]*InstalledPackage),
		targets:        make(map[string]*Target),
		versions:       make(map[string]string),
	}
}

func NewPackageContextForDefinition() *PackageContext {
	return &PackageContext{
		ConfigAccessor: NewConfigAccessor(nil, nil),
		versions:       make(map[string]string),
	}
}

func (ctx *PackageContext) SetGit(urls ...string) *PackageContext {
	ctx.gitURLs = urls
	return ctx
}

func (ctx *PackageContext) SetHomepage(url string) *PackageContext {
	ctx.homepage = url
	return ctx
}

func (ctx *PackageContext) SetDescription(desc string) *PackageContext {
	ctx.description = desc
	return ctx
}

func (ctx *PackageContext) SetLicense(license string) *PackageContext {
	ctx.license = license
	return ctx
}

func (ctx *PackageContext) AddVersion(version, ref string) *PackageContext {
	ctx.versions[version] = ref
	return ctx
}

func (ctx *PackageContext) SetLibs(libs ...string) *PackageContext {
	ctx.libs = libs
	return ctx
}

func (ctx *PackageContext) DeclarePackages(packages ...string) *PackageContext {
	ctx.declaredPackages = append(ctx.declaredPackages, packages...)
	return ctx
}

func (ctx *PackageContext) GitURLs() []string   { return ctx.gitURLs }
func (ctx *PackageContext) Homepage() string    { return ctx.homepage }
func (ctx *PackageContext) Description() string { return ctx.description }
func (ctx *PackageContext) License() string     { return ctx.license }
func (ctx *PackageContext) Versions() map[string]string {
	return ctx.versions
}
func (ctx *PackageContext) Libs() []string { return ctx.libs }
func (ctx *PackageContext) DeclaredPackages() []string {
	return ctx.declaredPackages
}
func (ctx *PackageContext) PackageName() string { return ctx.pkgName }
func (ctx *PackageContext) GetOptions() map[string]*Option {
	return ctx.Options
}
func (ctx *PackageContext) GetConfigValues() map[string]any {
	return ctx.CfgVals
}

func (ctx *PackageContext) SetOptions(options map[string]*Option) {
	ctx.Options = options
}

func (ctx *PackageContext) SetDeps(deps map[string]*InstalledPackage) {
	ctx.deps = deps
}

func (ctx *PackageContext) SetInstaller(installer OnDemandInstaller) {
	ctx.installer = installer
}

func (ctx *PackageContext) SetDirs(sourceDir, buildDir, installDir string) {
	ctx.sourceDir = sourceDir
	ctx.buildDir = buildDir
	ctx.installDir = installDir
}

func (ctx *PackageContext) Dep(name string) *InstalledPackage {
	if pkg, ok := ctx.deps[name]; ok {
		return pkg
	}
	if ctx.installer != nil {
		pkg := ctx.installer.EnsureInstalled(name)
		if pkg != nil {
			ctx.deps[name] = pkg
		}
		return pkg
	}
	return nil
}

func (ctx *PackageContext) Deps() map[string]*InstalledPackage {
	return ctx.deps
}

func (ctx *PackageContext) CC() string          { return ctx.toolchain.CC }
func (ctx *PackageContext) CXX() string         { return ctx.toolchain.CXX }
func (ctx *PackageContext) AR() string          { return ctx.toolchain.AR }
func (ctx *PackageContext) CrossTarget() string { return ctx.toolchain.Target }
func (ctx *PackageContext) SysRoot() string     { return ctx.toolchain.SysRoot }
func (ctx *PackageContext) CFlags() string      { return ctx.toolchain.CFlags }
func (ctx *PackageContext) CXXFlags() string    { return ctx.toolchain.CXXFlags }
func (ctx *PackageContext) LDFlags() string     { return ctx.toolchain.LDFlags }

func (ctx *PackageContext) Env() map[string]string {
	return ctx.toolchain.Env()
}

func (ctx *PackageContext) SourceDir() string  { return ctx.sourceDir }
func (ctx *PackageContext) BuildDir() string   { return ctx.buildDir }
func (ctx *PackageContext) InstallDir() string { return ctx.installDir }

func (ctx *PackageContext) CMakeConfigure(extraArgs ...string) error {
	args := []string{
		"-S", ctx.sourceDir,
		"-B", ctx.buildDir,
		"-DCMAKE_INSTALL_PREFIX=" + ctx.installDir,
	}
	if ctx.toolchain.CC != "" {
		args = append(args, "-DCMAKE_C_COMPILER="+ctx.toolchain.CC)
	}
	if ctx.toolchain.CXX != "" {
		args = append(args, "-DCMAKE_CXX_COMPILER="+ctx.toolchain.CXX)
	}
	args = append(args, "-DCMAKE_BUILD_TYPE=Release")
	if ctx.CrossTarget() != "" {
		args = append(args,
			"-DCMAKE_SYSTEM_NAME=Linux",
			"-DCMAKE_C_COMPILER_TARGET="+ctx.CrossTarget(),
			"-DCMAKE_CXX_COMPILER_TARGET="+ctx.CrossTarget())
	}
	if ctx.toolchain.SysRoot != "" {
		args = append(args, "-DCMAKE_SYSROOT="+ctx.toolchain.SysRoot)
	}
	args = append(args, extraArgs...)
	return ctx.Run("cmake", args...)
}

func (ctx *PackageContext) CMakeBuild(args ...string) error {
	buildArgs := []string{"--build", ctx.buildDir}
	buildArgs = append(buildArgs, args...)
	return ctx.Run("cmake", buildArgs...)
}

func (ctx *PackageContext) CMakeInstall() error {
	return ctx.Run("cmake", "--install", ctx.buildDir)
}

func (ctx *PackageContext) Configure(extraArgs ...string) error {
	args := []string{"--prefix=" + ctx.installDir}
	if ctx.CrossTarget() != "" {
		args = append(args, "--host="+ctx.CrossTarget())
	}
	args = append(args, extraArgs...)
	return ctx.RunWithEnv(ctx.Env(), ctx.sourceDir+"/configure", args...)
}

func (ctx *PackageContext) Make(args ...string) error {
	makeArgs := []string{"-C", ctx.buildDir}
	makeArgs = append(makeArgs, args...)
	return ctx.Run("make", makeArgs...)
}

func (ctx *PackageContext) Run(name string, args ...string) error {
	vlog.Info("  %s %s", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Dir = ctx.buildDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		vlog.Fatal("command failed: %s %s", name, strings.Join(args, " "))
	}
	return nil
}

func (ctx *PackageContext) RunIn(dir, name string, args ...string) error {
	vlog.Info("  cd %s && %s %s", dir, name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		vlog.Fatal("command failed: %s %s", name, strings.Join(args, " "))
	}
	return nil
}

func (ctx *PackageContext) RunWithEnv(env map[string]string, name string, args ...string) error {
	vlog.Info("  %s %s", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Dir = ctx.buildDir
	cmd.Env = append([]string{}, cmd.Environ()...)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		vlog.Fatal("command failed: %s %s", name, strings.Join(args, " "))
	}
	return nil
}

func (ctx *PackageContext) Target(name string) *Target {
	if t, ok := ctx.targets[name]; ok {
		return t
	}
	t := &Target{name: name, isDefault: true}
	ctx.targets[name] = t
	return t
}

func (ctx *PackageContext) GetTargets() map[string]*Target {
	return ctx.targets
}
