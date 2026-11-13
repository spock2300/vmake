package api

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/exec"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type pkgBase struct {
	pkgName string
}

func (b *pkgBase) PackageName() string {
	return b.pkgName
}

type ConfigContext struct {
	ConfigAccessor
	pkgBase
	pkg               *Package
	addGlobalCFlags   func(...string)
	addGlobalCxxFlags func(...string)
	addGlobalLdFlags  func(...string)
	addGlobalLinks    func(...string)
}

func NewConfigContext(pkgName string) *ConfigContext {
	ctx := &ConfigContext{
		ConfigAccessor: NewConfigAccessor(nil, nil),
		pkgBase:        pkgBase{pkgName: pkgName},
	}
	ctx.setStrictOwner(pkgName)
	return ctx
}

func NewConfigContextWithPackage(pkgName string, pkg *Package) *ConfigContext {
	ctx := &ConfigContext{
		ConfigAccessor: NewConfigAccessor(nil, nil),
		pkgBase:        pkgBase{pkgName: pkgName},
		pkg:            pkg,
	}
	ctx.setStrictOwner(pkgName)
	return ctx
}

func (ctx *ConfigContext) SetLenient() *ConfigContext {
	ctx.strict = false
	ctx.owner = ""
	ctx.lenientCross = true
	return ctx
}

func (ctx *ConfigContext) SetGlobalCFlagsFunc(fn func(...string)) *ConfigContext {
	ctx.addGlobalCFlags = fn
	return ctx
}

func (ctx *ConfigContext) SetGlobalCxxFlagsFunc(fn func(...string)) *ConfigContext {
	ctx.addGlobalCxxFlags = fn
	return ctx
}

func (ctx *ConfigContext) SetGlobalLdFlagsFunc(fn func(...string)) *ConfigContext {
	ctx.addGlobalLdFlags = fn
	return ctx
}

func (ctx *ConfigContext) SetGlobalLinksFunc(fn func(...string)) *ConfigContext {
	ctx.addGlobalLinks = fn
	return ctx
}

func (ctx *ConfigContext) AddGlobalCFlags(flags ...any) {
	if strs := flattenAny(flags); len(strs) > 0 && ctx.addGlobalCFlags != nil {
		ctx.addGlobalCFlags(strs...)
	}
}

func (ctx *ConfigContext) AddGlobalCxxFlags(flags ...any) {
	if strs := flattenAny(flags); len(strs) > 0 && ctx.addGlobalCxxFlags != nil {
		ctx.addGlobalCxxFlags(strs...)
	}
}

func (ctx *ConfigContext) AddGlobalLdFlags(flags ...any) {
	if strs := flattenAny(flags); len(strs) > 0 && ctx.addGlobalLdFlags != nil {
		ctx.addGlobalLdFlags(strs...)
	}
}

func (ctx *ConfigContext) AddGlobalLinks(links ...any) {
	if strs := flattenAny(links); len(strs) > 0 && ctx.addGlobalLinks != nil {
		ctx.addGlobalLinks(strs...)
	}
}

func (ctx *ConfigContext) AddGlobalDefines(defines ...any) {
	strs := flattenAny(defines)
	if len(strs) == 0 {
		return
	}
	flags := make([]string, len(strs))
	for i, d := range strs {
		if strings.HasPrefix(d, "-D") {
			flags[i] = d
		} else {
			flags[i] = "-D" + d
		}
	}
	ctx.AddGlobalCFlags(flags)
	ctx.AddGlobalCxxFlags(flags)
}

func (ctx *ConfigContext) SetDefaultVisibilityHidden() *ConfigContext {
	if ctx.pkg == nil {
		fatalScript(ctx.PackageName(), "SetDefaultVisibilityHidden", "no package associated with context")
	}
	ctx.pkg.defaultVisibilityHidden = true
	return ctx
}

func (ctx *ConfigContext) SetChipProfile(profile ChipProfile) *ConfigContext {
	if ctx.pkg == nil {
		fatalScript(ctx.PackageName(), "SetChipProfile", "no package associated with context")
	}
	ctx.pkg.SetChipProfile(profile)
	return ctx
}

func (ctx *ConfigContext) SetProvidedLinkerScript(path string) *ConfigContext {
	if ctx.pkg != nil {
		ctx.pkg.SetProvidedLinkerScript(path)
	}
	return ctx
}

func (ctx *ConfigContext) KConfig(name string) *KConfigEntry {
	if ctx.pkg != nil {
		return ctx.pkg.AddKConfig(name)
	}
	fatalScript(ctx.PackageName(), "KConfig", "no package associated with context (name=%s)", name)
	return nil
}

func (ctx *ConfigContext) SetConfigValue(name string, val any) *ConfigContext {
	if strings.Contains(name, "/") {
		fatalScript(ctx.PackageName(), "SetConfigValue", "config key %q must not contain '/'; cross-package writes are not supported", name)
	}
	ctx.CfgVals[name] = val
	return ctx
}

func (ctx *ConfigContext) GetOptions() map[string]*Option {
	return ctx.Options
}

func (ctx *ConfigContext) Toolchains() []string {
	tcs, err := toolchain.GetManager().ListToolchains()
	if err != nil {
		fatalScript(ctx.PackageName(), "Toolchains", "%v", err)
	}
	names := make([]string, 0, len(tcs))
	for name := range tcs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (ctx *ConfigContext) GlobalOption(name string) *Option {
	opt := ctx.Option(name)
	opt.group = GroupGlobal
	return opt
}

func (ctx *ConfigContext) GlobalMode() *Option {
	if builtIn, ok := BuiltInGlobalOptions[ModeOptionName]; ok {
		return ctx.GlobalOption(ModeOptionName).
			SetType(builtIn.Type()).
			SetDefault(builtIn.Default()).
			SetDescription(builtIn.Description()).
			SetValues(builtIn.Values()...)
	}
	return ctx.GlobalOption(ModeOptionName)
}

func (ctx *ConfigContext) ToolchainOption() *Option {
	return ctx.Option(ToolchainOptionName).
		SetType(OptionChoice).
		SetDefault("host").
		SetDescription("Build toolchain").
		SetValues(ctx.Toolchains()...)
}

type DepOptionDefault struct {
	Dep    string
	Option string
	Value  any
}

type DepConfigContext struct {
	ConfigAccessor
	pkgBase
	sourceDir string
	defaults  []DepOptionDefault
}

func NewDepConfigContext(pkgName, sourceDir string, cfgVals map[string]any, options map[string]*Option) *DepConfigContext {
	ctx := &DepConfigContext{
		ConfigAccessor: NewConfigAccessor(cfgVals, options),
		pkgBase:        pkgBase{pkgName: pkgName},
		sourceDir:      sourceDir,
	}
	ctx.setStrictOwner(pkgName)
	ctx.LockOptionDeclaration()
	return ctx
}

func (ctx *DepConfigContext) SourceDir() string {
	return ctx.sourceDir
}

func (ctx *DepConfigContext) SetDefault(depPkg, option string, val any) *DepConfigContext {
	depPkg = strings.TrimSpace(depPkg)
	option = strings.TrimSpace(option)
	if depPkg == "" {
		fatalScript(ctx.PackageName(), "SetDefault", "dependency package name must not be empty")
	}
	if option == "" || strings.Contains(option, "/") {
		fatalScript(ctx.PackageName(), "SetDefault", "invalid dependency option name %q (must not be empty or contain '/')", option)
	}
	if val == nil {
		fatalScript(ctx.PackageName(), "SetDefault", "default value for %s/%s must not be nil", depPkg, option)
	}
	ctx.defaults = append(ctx.defaults, DepOptionDefault{
		Dep:    depPkg,
		Option: option,
		Value:  val,
	})
	return ctx
}

func (ctx *DepConfigContext) Defaults() []DepOptionDefault {
	return append([]DepOptionDefault(nil), ctx.defaults...)
}

type BuildContext struct {
	ConfigAccessor
	*TargetRegistry
	*InstallItemHolder
	pkgBase
	pkg               *Package
	genConfigHeader   bool
	genConfigDefines  bool
	importConfigs     []string
	subGraphs         []string
	buildSubGraphFunc func(pkgName string) error
	depOutputFunc     func(depRef string) string
	depSrcDirFunc     func(depRef string) string
	depPackageDirFunc func(depRef string) string
	dryRun            bool
}

func NewBuildContext(pkgName string, cfgVals map[string]any) *BuildContext {
	ctx := &BuildContext{
		ConfigAccessor:    NewConfigAccessor(cfgVals, nil),
		TargetRegistry:    newOwnedTargetRegistry(pkgName),
		InstallItemHolder: &InstallItemHolder{},
		pkgBase:           pkgBase{pkgName: pkgName},
	}
	ctx.setStrictOwner(pkgName)
	ctx.ConfigAccessor.LockOptionDeclaration()
	return ctx
}

func (ctx *BuildContext) SetDryRun(v bool) *BuildContext {
	ctx.dryRun = v
	return ctx
}

func (ctx *BuildContext) SetPackage(pkg *Package) *BuildContext {
	ctx.pkg = pkg
	return ctx
}

func (ctx *BuildContext) SetProvidedLinkerScript(path string) *BuildContext {
	if ctx.pkg != nil {
		ctx.pkg.SetProvidedLinkerScript(path)
	}
	return ctx
}

func (ctx *BuildContext) ExecutedHelperStages() int {
	if ctx.pkg == nil {
		return 0
	}
	return ctx.pkg.ExecutedHelperStages()
}

func (ctx *BuildContext) SetBuildSubGraphFunc(fn func(string) error) *BuildContext {
	ctx.buildSubGraphFunc = fn
	return ctx
}

func (ctx *BuildContext) SetDepOutputFunc(fn func(string) string) *BuildContext {
	ctx.depOutputFunc = fn
	return ctx
}

func (ctx *BuildContext) SetDepSrcDirFunc(fn func(string) string) *BuildContext {
	ctx.depSrcDirFunc = fn
	return ctx
}

func (ctx *BuildContext) SetDepPackageDirFunc(fn func(string) string) *BuildContext {
	ctx.depPackageDirFunc = fn
	return ctx
}

func (ctx *BuildContext) BuildSubGraph(pkgName string) {
	if pkgName != "" {
		ctx.subGraphs = append(ctx.subGraphs, pkgName)
	}
	if ctx.dryRun {
		return
	}
	if ctx.buildSubGraphFunc == nil {
		fatalScript(ctx.PackageName(), "BuildSubGraph", "not available")
	}
	if err := ctx.buildSubGraphFunc(pkgName); err != nil {
		fatalScript(ctx.PackageName(), "BuildSubGraph", "%v (pkgName=%s)", err, pkgName)
	}
}

func (ctx *BuildContext) DepOutput(depRef string) string {
	if ctx.dryRun {
		return ""
	}
	if ctx.depOutputFunc == nil {
		fatalScript(ctx.PackageName(), "DepOutput", "not available")
	}
	return ctx.depOutputFunc(depRef)
}

func (ctx *BuildContext) DepBuildDir(depRef string) string {
	out := ctx.DepOutput(depRef)
	if out == "" {
		return ""
	}
	return filepath.Dir(out)
}

// DepSrcDir returns the dependency package's upstream source directory
// (Package.SrcDir). It is the same root used by the dependency's own build
// script, so generators can read its checked-out sources without assuming a
// shared directory layout. The reference accepts "<package>" or
// "<package>:<target>"; a missing package or target yields an empty string.
func (ctx *BuildContext) DepSrcDir(depRef string) string {
	if ctx.dryRun {
		return ""
	}
	if ctx.depSrcDirFunc == nil {
		fatalScript(ctx.PackageName(), "DepSrcDir", "not available")
	}
	return ctx.depSrcDirFunc(depRef)
}

func (ctx *BuildContext) DepPackageDir(depRef string) string {
	if ctx.dryRun {
		return ""
	}
	if ctx.depPackageDirFunc == nil {
		fatalScript(ctx.PackageName(), "DepPackageDir", "not available")
	}
	return ctx.depPackageDirFunc(depRef)
}

func (ctx *BuildContext) GenerateConfigHeader() *BuildContext {
	ctx.genConfigHeader = true
	return ctx
}

func (ctx *BuildContext) GenerateConfigDefines() *BuildContext {
	ctx.genConfigDefines = true
	return ctx
}

func (ctx *BuildContext) ImportConfig(pkgNames ...string) *BuildContext {
	ctx.importConfigs = append(ctx.importConfigs, pkgNames...)
	return ctx
}

func (ctx *BuildContext) SyncConfigDefines(pkgNames ...string) *BuildContext {
	ctx.genConfigDefines = true
	ctx.importConfigs = append(ctx.importConfigs, pkgNames...)
	return ctx
}

func (ctx *BuildContext) GenConfigHeader() bool   { return ctx.genConfigHeader }
func (ctx *BuildContext) GenConfigDefines() bool  { return ctx.genConfigDefines }
func (ctx *BuildContext) ImportConfigs() []string { return ctx.importConfigs }
func (ctx *BuildContext) SubGraphs() []string     { return ctx.subGraphs }

func (ctx *BuildContext) Exec(name string, args ...string) {
	if ctx.dryRun {
		return
	}
	vlog.Info("  %s %s", name, strings.Join(args, " "))
	if ctx.pkg != nil {
		if err := ctx.pkg.runIn("", nil, name, args...); err != nil {
			fatalScript(ctx.PackageName(), "Exec", "%v", err)
		}
		return
	}
	if err := exec.RunToStdout("", name, args...); err != nil {
		fatalScript(ctx.PackageName(), "Exec", "%v", err)
	}
}

func (ctx *BuildContext) BuildPath(elem ...string) string {
	if ctx.pkg != nil {
		return ctx.pkg.BuildPath(elem...)
	}
	if len(elem) == 0 {
		return BuildDirToken
	}
	parts := make([]string, 0, len(elem)+1)
	parts = append(parts, BuildDirToken)
	parts = append(parts, elem...)
	return filepath.Join(parts...)
}

func (ctx *BuildContext) WriteBuildFile(relPath string, content any) string {
	if ctx.pkg == nil {
		fatalScript(ctx.PackageName(), "WriteBuildFile", "no package associated with context")
	}
	return ctx.pkg.WriteBuildFile(relPath, content)
}

func (ctx *BuildContext) GenerateFile(relPath string, fn func() ([]byte, error)) string {
	if ctx.pkg == nil {
		fatalScript(ctx.PackageName(), "GenerateFile", "no package associated with context")
	}
	return ctx.pkg.GenerateFile(relPath, fn)
}

func (ctx *BuildContext) Generate(fn func() error) *BuildContext {
	if ctx.pkg == nil {
		fatalScript(ctx.PackageName(), "Generate", "no package associated with context")
	}
	ctx.pkg.Generate(fn)
	return ctx
}

type InstallContext struct {
	ConfigAccessor
	*InstallItemHolder
	pkgBase
	prefix    string
	prefixSet bool
}

func NewInstallContext(pkgName string, cfgVals map[string]any) *InstallContext {
	ctx := &InstallContext{
		ConfigAccessor:    NewConfigAccessor(cfgVals, nil),
		InstallItemHolder: &InstallItemHolder{},
		pkgBase:           pkgBase{pkgName: pkgName},
	}
	ctx.setStrictOwner(pkgName)
	ctx.ConfigAccessor.LockOptionDeclaration()
	return ctx
}

func (ctx *InstallContext) Prefix() string  { return ctx.prefix }
func (ctx *InstallContext) PrefixSet() bool { return ctx.prefixSet }

func (ctx *InstallContext) SetPrefix(prefix string) *InstallContext {
	ctx.prefix = prefix
	ctx.prefixSet = true
	return ctx
}

type CleanContext struct {
	ConfigAccessor
	pkgBase
	pkg *Package
}

func NewCleanContext(pkgName string, cfgVals map[string]any) *CleanContext {
	ctx := &CleanContext{
		ConfigAccessor: NewConfigAccessor(cfgVals, nil),
		pkgBase:        pkgBase{pkgName: pkgName},
	}
	ctx.setStrictOwner(pkgName)
	ctx.ConfigAccessor.LockOptionDeclaration()
	return ctx
}

func (ctx *CleanContext) SetPackage(pkg *Package) *CleanContext {
	ctx.pkg = pkg
	return ctx
}

func (ctx *CleanContext) SourceDir() string { return ctx.pkg.SourceDir() }
func (ctx *CleanContext) BuildDir() string  { return ctx.pkg.BuildDir() }
func (ctx *CleanContext) SrcDir() string    { return ctx.pkg.SrcDir() }

func (ctx *CleanContext) Run(name string, args ...string) {
	ctx.pkg.Run(name, args...)
}

func (ctx *CleanContext) RunIn(dir, name string, args ...string) {
	ctx.pkg.RunIn(dir, name, args...)
}

func (ctx *CleanContext) RunEnv(env map[string]string, name string, args ...string) error {
	return ctx.pkg.RunEnv(env, name, args...)
}

func (ctx *CleanContext) Make(args ...string) error {
	return ctx.pkg.Make(args...)
}
