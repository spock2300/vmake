package api

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spock2300/vmake/internal/fs"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type TargetKind string

const (
	TargetBinary TargetKind = "binary"
	TargetStatic TargetKind = "static"
	TargetShared TargetKind = "shared"
	TargetObject TargetKind = "object"
	TargetVoid   TargetKind = "void"
)

func (k TargetKind) Ext() string {
	switch k {
	case TargetStatic:
		return ".a"
	case TargetShared:
		return ".so"
	case TargetObject:
		return ".o"
	default:
		return ""
	}
}

func (k TargetKind) Prefix() string {
	switch k {
	case TargetStatic, TargetShared:
		return "lib"
	default:
		return ""
	}
}

func (k TargetKind) InstallDir() string {
	switch k {
	case TargetBinary:
		return "bin"
	case TargetStatic, TargetShared:
		return "lib"
	default:
		return ""
	}
}

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
type CleanFunc func(ctx *CleanContext)
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

type PkgDirs struct {
	SourceDir  string
	BuildDir   string
	InstallDir string
}

type Package struct {
	PackageMeta
	ConfigAccessor
	*TargetRegistry
	*InstallItemHolder
	gitURLs              []string
	homepage             string
	description          string
	license              string
	versions             map[string]string
	submodules           bool
	requires             Requires
	requireFuncs         []RequireFunc
	configFuncs          []ConfigFunc
	buildFuncs           []BuildFunc
	installFuncs         []InstallFunc
	cleanFuncs           []CleanFunc
	packageFunc          PackageFunc
	scriptDir            string
	srcCodeDir           string
	dirs                 PkgDirs
	outputDir            string
	tc                   *toolchain.Toolchain
	globalCFlags         []string
	globalCxxFlags       []string
	globalLdFlags        []string
	globalLinks          []string
	deps                 map[string]*InstalledPackage
	patches              []string
	configFiles          []string
	kconfigEntries       []*KConfigEntry
	genConfigHdr         bool
	exportConfig         bool
	importConfigs        []string
	dryRun               bool
	isRoot               bool
	providedLinkerScript string
}

func NewPackage() *Package {
	return &Package{
		ConfigAccessor:    NewConfigAccessor(nil, nil),
		TargetRegistry:    NewTargetRegistry(),
		InstallItemHolder: &InstallItemHolder{},
		versions:          make(map[string]string),
		requires:          Requires{},
		deps:              make(map[string]*InstalledPackage),
	}
}

func (p *Package) SetProvidedLinkerScript(path string) *Package {
	if p.providedLinkerScript != "" {
		fatalScript(p.FullName(), "SetProvidedLinkerScript", "linker script already set to %s", p.providedLinkerScript)
	}
	p.providedLinkerScript = path
	return p
}

func (p *Package) ProvidedLinkerScript() string {
	return p.providedLinkerScript
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

func (p *Package) OnClean(fn CleanFunc) *Package {
	p.cleanFuncs = append(p.cleanFuncs, fn)
	return p
}

func (p *Package) OnPackage(fn PackageFunc) *Package {
	if p.packageFunc != nil {
		fatalScript(p.Name, "OnPackage", "already registered (single-slot callback); merge the implementations into one")
	}
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

func (p *Package) SelectVersionMulti(constraints []string) (string, error) {
	versions := p.GetVersions()
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions available for %s", p.FullName())
	}

	parsedConstraints := make([]Constraint, 0, len(constraints))
	for _, c := range constraints {
		pc, ok := ParseConstraint(c)
		if !ok {
			return "", fmt.Errorf("invalid constraint '%s' for %s", c, p.FullName())
		}
		parsedConstraints = append(parsedConstraints, pc)
	}

	candidates := make([]string, 0, len(versions))
	for _, v := range versions {
		pv, ok := ParseVersion(v)
		if !ok {
			continue
		}
		if matchesAll(pv, parsedConstraints) {
			candidates = append(candidates, v)
		}
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no version matching [%s] for %s (available: %v)",
			strings.Join(constraints, ", "), p.FullName(), versions)
	}

	selected, _ := MatchVersion(candidates, "")
	return selected, nil
}

func matchesAll(v Version, constraints []Constraint) bool {
	for _, c := range constraints {
		if !c.Match(v) {
			return false
		}
	}
	return true
}

func (p *Package) GitURLs() []string              { return p.gitURLs }
func (p *Package) Homepage() string               { return p.homepage }
func (p *Package) Description() string            { return p.description }
func (p *Package) License() string                { return p.license }
func (p *Package) Versions() map[string]string    { return p.versions }
func (p *Package) Submodules() bool               { return p.submodules }
func (p *Package) GetOptions() map[string]*Option { return p.Options }
func (p *Package) GetRequires() *Requires         { return &p.requires }
func (p *Package) GetRef(version string) string   { return p.versions[version] }
func (p *Package) GetRequireFuncs() []RequireFunc { return p.requireFuncs }
func (p *Package) GetPackageFunc() PackageFunc    { return p.packageFunc }

func normalizeScriptPanic(pkgName string, r any) *BuildScriptError {
	if existing, ok := r.(*BuildScriptError); ok {
		if existing.Package == "" {
			existing.Package = pkgName
		}
		return existing
	}
	return &BuildScriptError{Package: pkgName, Op: "panic", Err: fmt.Errorf("%v", r)}
}

func RunScriptSafe(pkgName string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			vlog.Fatal("%v", normalizeScriptPanic(pkgName, r))
		}
	}()
	fn()
}

func execFuncs[T any](pkgName, dir string, funcs []T, fn func(T)) {
	defer func() {
		if r := recover(); r != nil {
			vlog.Fatal("%v", normalizeScriptPanic(pkgName, r))
		}
	}()
	execInDir(dir, func() {
		for _, f := range funcs {
			fn(f)
		}
	})
}

func (p *Package) ExecConfigFuncs(dir string, fn func(ConfigFunc)) {
	execFuncs(p.Name, dir, p.configFuncs, fn)
}

func (p *Package) ExecBuildFuncs(dir string, fn func(BuildFunc)) {
	execFuncs(p.Name, dir, p.buildFuncs, fn)
}

func (p *Package) ExecInstallFuncs(dir string, fn func(InstallFunc)) {
	execFuncs(p.Name, dir, p.installFuncs, fn)
}

func (p *Package) ExecCleanFuncs(dir string, fn func(CleanFunc)) {
	execFuncs(p.Name, dir, p.cleanFuncs, fn)
}

func (p *Package) UpdateRequireContext(cfgVals map[string]any, options map[string]*Option) {
	if len(p.requireFuncs) == 0 {
		return
	}
	ctx := NewRequireContextForConfig(p.Name, cfgVals, options, nil)
	RunScriptSafe(p.Name, func() {
		for _, fn := range p.requireFuncs {
			fn(ctx)
		}
	})
	p.requires = Requires{requires: ctx.GetRequires()}
}

func (p *Package) SetDeps(deps map[string]*InstalledPackage) *Package {
	p.deps = deps
	return p
}

func (p *Package) SetDep(name string, pkg *InstalledPackage) *Package {
	if p.deps == nil {
		p.deps = make(map[string]*InstalledPackage)
	}
	p.deps[name] = pkg
	return p
}

func (p *Package) Deps() map[string]*InstalledPackage {
	return p.deps
}

func (p *Package) SetDirs(dirs PkgDirs) *Package {
	p.dirs = dirs
	return p
}

func (p *Package) SetScriptDir(dir string) *Package {
	p.scriptDir = dir
	return p
}

func (p *Package) SetSrcDir(dir string) *Package {
	p.srcCodeDir = dir
	return p
}

func (p *Package) AddPatches(paths ...string) *Package {
	p.patches = append(p.patches, paths...)
	return p
}

func (p *Package) SetPatches(paths ...string) *Package {
	p.patches = paths
	return p
}

func (p *Package) SetConfigFiles(files ...string) *Package {
	p.configFiles = files
	return p
}

func (p *Package) ConfigFiles() []string { return p.configFiles }

func (p *Package) SetOutputDir(dir string) *Package {
	p.outputDir = dir
	return p
}

func (p *Package) SetCfgVals(vals map[string]any) *Package {
	p.CfgVals = vals
	return p
}

func (p *Package) SetToolchain(tc *toolchain.Toolchain) *Package {
	p.tc = tc
	return p
}

func (p *Package) SetGlobalFlags(cflags, cxxflags, ldflags, links []string) *Package {
	p.globalCFlags = cflags
	p.globalCxxFlags = cxxflags
	p.globalLdFlags = ldflags
	p.globalLinks = links
	return p
}

func (p *Package) GlobalCFlags() []string {
	return p.globalCFlags
}

func (p *Package) GlobalCxxFlags() []string {
	return p.globalCxxFlags
}

func (p *Package) GlobalLdFlags() []string {
	return p.globalLdFlags
}

func (p *Package) GlobalLinks() []string {
	return p.globalLinks
}

func (p *Package) SetDryRun(v bool) *Package {
	p.dryRun = v
	return p
}

func (p *Package) DryRun() bool { return p.dryRun }

func (p *Package) SetRoot(v bool) *Package {
	p.isRoot = v
	return p
}

func (p *Package) IsRoot() bool { return p.isRoot }

func (p *Package) SetGenConfigHeader(v bool) *Package { p.genConfigHdr = v; return p }
func (p *Package) GenConfigHeader() bool              { return p.genConfigHdr }

func (p *Package) SetExportConfig(v bool) *Package { p.exportConfig = v; return p }
func (p *Package) ExportConfig() bool              { return p.exportConfig }

func (p *Package) SetImportConfigs(names []string) *Package { p.importConfigs = names; return p }
func (p *Package) ImportConfigs() []string                  { return p.importConfigs }

func (p *Package) ScriptDir() string { return p.scriptDir }
func (p *Package) SrcDir() string {
	if p.srcCodeDir != "" {
		return p.srcCodeDir
	}
	return p.dirs.SourceDir
}
func (p *Package) SrcDirRaw() string    { return p.srcCodeDir }
func (p *Package) SourceDir() string    { return p.dirs.SourceDir }
func (p *Package) BuildDir() string     { return p.dirs.BuildDir }
func (p *Package) InstallDir() string   { return p.dirs.InstallDir }
func (p *Package) OutputDir() string    { return p.outputDir }
func (p *Package) GetPatches() []string { return p.patches }

func (p *Package) CC() string       { return p.tc.Tools.CC }
func (p *Package) CXX() string      { return p.tc.Tools.CXX }
func (p *Package) AR() string       { return p.tc.Tools.AR }
func (p *Package) Prefix() string   { return p.tc.Prefix }
func (p *Package) CFlags() string   { return strings.Join(p.tc.DefaultFlags.CFlags, " ") }
func (p *Package) CXXFlags() string { return strings.Join(p.tc.DefaultFlags.CxxFlags, " ") }
func (p *Package) LDFlags() string  { return strings.Join(p.tc.DefaultFlags.LdFlags, " ") }
func (p *Package) ObjCopy() string  { return p.tc.Tools.OBJCOPY }
func (p *Package) Size() string     { return p.tc.Tools.SIZE }
func (p *Package) ObjDump() string  { return p.tc.Tools.OBJDUMP }
func (p *Package) NM() string       { return p.tc.Tools.NM }

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

func (p *Package) AddKConfig(name string) *KConfigEntry {
	if len(p.kconfigEntries) > 0 {
		fatalScript(p.Name, "AddKConfig", "only one kconfig entry per package is supported (existing: %q)", p.kconfigEntries[0].name)
	}
	k := &KConfigEntry{name: name}
	p.kconfigEntries = append(p.kconfigEntries, k)
	return k
}

func (p *Package) KConfigEntries() []*KConfigEntry { return p.kconfigEntries }

func (p *Package) SelectedPreset() string {
	if len(p.kconfigEntries) == 0 {
		return ""
	}
	k := p.kconfigEntries[0]
	if k.selectedPreset != "" {
		return k.selectedPreset
	}
	return k.defaultPreset
}

func (p *Package) EnsureConfig(srcDir string) bool {
	configPath := filepath.Join(srcDir, ".config")
	if info, err := os.Stat(configPath); err == nil && info.Size() > 0 {
		return false
	}
	preset := p.SelectedPreset()
	if preset == "" {
		fatalScript(p.Name, "EnsureConfig", "no kconfig preset selected; configure one via AddKConfig().SetDefaultPreset(...) or vmake config")
	}
	p.RunIn(srcDir, "make", preset)
	if len(p.kconfigEntries) > 0 {
		if err := ApplyKConfigPatches(configPath, p.kconfigEntries[0].Patches()); err != nil {
			fatalScript(p.Name, "EnsureConfig", "apply kconfig patches: %v", err)
		}
	}
	return true
}

func ApplyKConfigPatches(configPath string, patches map[string]string) error {
	if len(patches) == 0 {
		return nil
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", configPath, err)
	}
	lines := strings.Split(string(data), "\n")
	keys := make([]string, 0, len(patches))
	for k := range patches {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, line := range lines {
		for _, old := range keys {
			if patchLineMatches(line, old) {
				lines[i] = patches[old]
				break
			}
		}
	}
	if err := os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	return nil
}

func patchLineMatches(line, key string) bool {
	if strings.Contains(key, "=") {
		return strings.HasPrefix(line, key)
	}
	return strings.HasPrefix(line, key+"=")
}
