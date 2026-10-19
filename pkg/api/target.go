package api

import (
	"path/filepath"
	"strings"
)

type PostLinkStep struct {
	Tool string
	Args []string
}

type Target struct {
	name               string
	pkgName            string
	kind               TargetKind
	isDefault          bool
	isTest             bool
	files              []string
	excludeFiles       []string
	includes           []string
	publicIncludes     []string
	includeRules       map[string][]string
	defines            []string
	publicDefines      []string
	links              []string
	publicLinks        []string
	providedLibs       []string
	deps               []string
	useRequires        bool
	noLink             bool
	cflags             []string
	publicCFlags       []string
	cxxflags           []string
	publicCxxFlags     []string
	ldflags            []string
	publicLdFlags      []string
	installDir         string
	noInstall          bool
	buildFunc          func(p *Package) error
	prebuilt           string
	linkerScript       string
	versionScript      string
	excludeLibs        []string
	symbolBinding      string
	symbolPrefix       string
	keepGlobalSymbols  string
	useDepLinkerScript bool
	postLinks          []PostLinkStep
	postLinkDeps       []string
	postLinkOutputs    []string
	inputs             []string
	outputs            []string
	genRules           []GenRule
}

func (t *Target) SetKind(kind TargetKind) *Target {
	t.kind = kind
	return t
}

func (t *Target) SetDefault(isDefault bool) *Target {
	t.isDefault = isDefault
	return t
}

func (t *Target) SetTest(v bool) *Target {
	t.isTest = v
	return t
}

func (t *Target) AddFiles(files ...any) *Target {
	t.files = append(t.files, flattenAny(files)...)
	return t
}

func (t *Target) RemoveFiles(files ...any) *Target {
	t.excludeFiles = append(t.excludeFiles, flattenAny(files)...)
	return t
}

func (t *Target) AddIncludes(dirs ...any) *Target {
	t.includes = append(t.includes, flattenAny(dirs)...)
	return t
}

func (t *Target) AddPublicIncludes(args ...any) *Target {
	strs := flattenAny(args)
	if len(strs) == 0 {
		return t
	}

	last := strs[len(strs)-1]
	if isRule(last) {
		pattern := last[1:]
		dirs := strs[:len(strs)-1]
		if len(dirs) == 0 {
			dirs = []string{"."}
		}
		t.publicIncludes = append(t.publicIncludes, dirs...)
		if t.includeRules == nil {
			t.includeRules = make(map[string][]string)
		}
		for _, d := range dirs {
			t.includeRules[d] = append(t.includeRules[d], pattern)
		}
	} else {
		t.publicIncludes = append(t.publicIncludes, strs...)
	}

	return t
}

func (t *Target) IncludeRule(dir string) []string {
	if t.includeRules == nil {
		return nil
	}
	return t.includeRules[dir]
}

func isRule(s string) bool {
	return strings.HasPrefix(s, "@")
}

func (t *Target) AddDefines(defines ...any) *Target {
	t.defines = append(t.defines, flattenAny(defines)...)
	return t
}

func (t *Target) AddPublicDefines(defines ...any) *Target {
	t.publicDefines = append(t.publicDefines, flattenAny(defines)...)
	return t
}

func (t *Target) AddLinks(libs ...any) *Target {
	t.links = append(t.links, flattenAny(libs)...)
	return t
}

func (t *Target) AddPublicLinks(libs ...any) *Target {
	t.publicLinks = append(t.publicLinks, flattenAny(libs)...)
	return t
}

func (t *Target) AddProvidedLibs(libs ...any) *Target {
	t.providedLibs = append(t.providedLibs, flattenAny(libs)...)
	return t
}

func (t *Target) AddDeps(targets ...any) *Target {
	for _, d := range flattenAny(targets) {
		if d == "" {
			continue
		}
		if _, err := ParseDepRef(d); err != nil {
			fatalScript(t.pkgName, "AddDeps", "%v", err)
		}
		t.deps = append(t.deps, d)
	}
	return t
}

func (t *Target) UseRequires() *Target {
	t.useRequires = true
	return t
}

func (t *Target) UsesRequires() bool { return t.useRequires }

func (t *Target) SetLinkable(v bool) *Target {
	t.noLink = !v
	return t
}

func (t *Target) IsLinkable() bool { return !t.noLink }

func (t *Target) AddCFlags(flags ...any) *Target {
	t.cflags = append(t.cflags, flattenAny(flags)...)
	return t
}

func (t *Target) AddPublicCFlags(flags ...any) *Target {
	t.publicCFlags = append(t.publicCFlags, flattenAny(flags)...)
	return t
}

func (t *Target) AddCxxFlags(flags ...any) *Target {
	t.cxxflags = append(t.cxxflags, flattenAny(flags)...)
	return t
}

func (t *Target) AddPublicCxxFlags(flags ...any) *Target {
	t.publicCxxFlags = append(t.publicCxxFlags, flattenAny(flags)...)
	return t
}

func (t *Target) AddLdFlags(flags ...any) *Target {
	t.ldflags = append(t.ldflags, flattenAny(flags)...)
	return t
}

func (t *Target) AddPublicLdFlags(flags ...any) *Target {
	t.publicLdFlags = append(t.publicLdFlags, flattenAny(flags)...)
	return t
}

func (t *Target) SetBuildFunc(fn func(p *Package) error) *Target {
	t.buildFunc = fn
	return t
}

func (t *Target) BuildFunc() func(p *Package) error {
	return t.buildFunc
}

func (t *Target) SetPrebuilt(path string) *Target {
	if t.prebuilt != "" {
		fatalScript(t.pkgName, "SetPrebuilt", "prebuilt already set to %s", t.prebuilt)
	}
	t.prebuilt = path
	return t
}

func (t *Target) Prebuilt() string {
	return t.prebuilt
}

func (t *Target) SetLinkerScript(path string) *Target {
	if t.linkerScript != "" {
		fatalScript(t.pkgName, "SetLinkerScript", "linker script already set to %s", t.linkerScript)
	}
	t.linkerScript = path
	return t
}

func (t *Target) SetVersionScript(path string) *Target {
	if t.versionScript != "" {
		fatalScript(t.pkgName, "SetVersionScript", "version script already set to %s", t.versionScript)
	}
	t.versionScript = path
	return t
}

func (t *Target) AddExcludeLibs(libs ...any) *Target {
	t.excludeLibs = append(t.excludeLibs, flattenAny(libs)...)
	return t
}

func (t *Target) SetSymbolBinding(mode string) *Target {
	switch mode {
	case "", "static", "static-functions":
		t.symbolBinding = mode
	default:
		fatalScript(t.pkgName, "SetSymbolBinding", "invalid mode %q (use \"static\" or \"static-functions\")", mode)
	}
	return t
}

func (t *Target) SetSymbolPrefix(prefix string) *Target {
	if t.symbolPrefix != "" {
		fatalScript(t.pkgName, "SetSymbolPrefix", "prefix already set to %s", t.symbolPrefix)
	}
	t.symbolPrefix = prefix
	t.postLinks = append(t.postLinks, PostLinkStep{
		Tool: "objcopy",
		Args: []string{"--prefix-symbols=" + prefix, "{output}"},
	})
	return t
}

func (t *Target) SetKeepGlobalSymbols(path string) *Target {
	if t.keepGlobalSymbols != "" {
		fatalScript(t.pkgName, "SetKeepGlobalSymbols", "keep-global-symbols file already set to %s", t.keepGlobalSymbols)
	}
	t.keepGlobalSymbols = path
	t.postLinks = append(t.postLinks, PostLinkStep{
		Tool: "objcopy",
		Args: []string{"--keep-global-symbols=" + path, "{output}"},
	})
	return t.AddPostLinkDeps(path)
}

func (t *Target) KeepGlobalSymbols() string { return t.keepGlobalSymbols }

func (t *Target) UseDependencyLinkerScript() *Target {
	t.useDepLinkerScript = true
	return t
}

func (t *Target) UseDepLinkerScript() bool { return t.useDepLinkerScript }

func (t *Target) AddPostLink(tool string, args ...string) *Target {
	t.postLinks = append(t.postLinks, PostLinkStep{Tool: tool, Args: args})
	return t
}

func (t *Target) AddPostLinkDeps(files ...string) *Target {
	t.postLinkDeps = append(t.postLinkDeps, files...)
	return t
}

func (t *Target) AddPostLinkOutputs(paths ...string) *Target {
	t.postLinkOutputs = append(t.postLinkOutputs, paths...)
	return t
}

func (t *Target) AddInputs(files ...any) *Target {
	t.inputs = append(t.inputs, flattenAny(files)...)
	return t
}

func (t *Target) RemoveInputs(files ...any) *Target {
	t.inputs = removeStrings(t.inputs, flattenAny(files)...)
	return t
}

func (t *Target) Inputs() []string { return t.inputs }

func (t *Target) AddOutputs(files ...any) *Target {
	t.outputs = append(t.outputs, flattenAny(files)...)
	return t
}

func (t *Target) RemoveOutputs(files ...any) *Target {
	t.outputs = removeStrings(t.outputs, flattenAny(files)...)
	return t
}

func (t *Target) Outputs() []string { return t.outputs }

func (t *Target) AddPostLinkHex() *Target {
	t.postLinks = append(t.postLinks, PostLinkStep{Tool: "objcopy", Args: []string{"-O", "ihex", "{output}", "{output}.hex"}})
	return t.AddPostLinkOutputs("{output}.hex")
}

func (t *Target) AddPostLinkBin() *Target {
	t.postLinks = append(t.postLinks, PostLinkStep{Tool: "objcopy", Args: []string{"-O", "binary", "{output}", "{output}.bin"}})
	return t.AddPostLinkOutputs("{output}.bin")
}

func (t *Target) AddPostLinkSize() *Target {
	t.postLinks = append(t.postLinks, PostLinkStep{Tool: "size", Args: []string{"{output}"}})
	return t
}

func (t *Target) AddPostLinkStrip() *Target {
	t.postLinks = append(t.postLinks, PostLinkStep{Tool: "strip", Args: []string{"-o", "{output}.stripped", "{output}"}})
	return t.AddPostLinkOutputs("{output}.stripped")
}

func (t *Target) AddBinHeader(inputs ...any) *Target {
	for _, input := range flattenAny(inputs) {
		stem := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
		t.genRules = append(t.genRules, GenRule{
			kind:       GenRuleBinHeader,
			input:      input,
			outputStem: stem,
		})
	}
	return t
}

func (t *Target) GenRules() []GenRule { return t.genRules }

func (t *Target) Name() string             { return t.name }
func (t *Target) Kind() TargetKind         { return t.kind }
func (t *Target) IsDefault() bool          { return t.isDefault }
func (t *Target) IsTest() bool             { return t.isTest }
func (t *Target) Files() []string          { return t.files }
func (t *Target) ExcludedFiles() []string  { return t.excludeFiles }
func (t *Target) Includes() []string       { return t.includes }
func (t *Target) PublicIncludes() []string { return t.publicIncludes }
func (t *Target) Defines() []string        { return t.defines }
func (t *Target) PublicDefines() []string  { return t.publicDefines }
func (t *Target) Links() []string          { return t.links }
func (t *Target) PublicLinks() []string    { return t.publicLinks }
func (t *Target) ProvidedLibs() []string   { return t.providedLibs }
func (t *Target) Deps() []string           { return t.deps }

func (t *Target) HasDep(depRef string) bool {
	for _, d := range t.deps {
		if d == depRef {
			return true
		}
	}
	return false
}
func (t *Target) CFlags() []string         { return t.cflags }
func (t *Target) PublicCFlags() []string   { return t.publicCFlags }
func (t *Target) CxxFlags() []string       { return t.cxxflags }
func (t *Target) PublicCxxFlags() []string { return t.publicCxxFlags }
func (t *Target) LdFlags() []string        { return t.ldflags }
func (t *Target) PublicLdFlags() []string  { return t.publicLdFlags }
func (t *Target) InstallDir() string       { return t.installDir }

func (t *Target) SetInstallDir(dir string) *Target {
	t.installDir = dir
	return t
}

func (t *Target) SetInstall(install bool) *Target {
	t.noInstall = !install
	return t
}

func (t *Target) NoInstall() bool               { return t.noInstall }
func (t *Target) LinkerScript() string          { return t.linkerScript }
func (t *Target) VersionScript() string         { return t.versionScript }
func (t *Target) ExcludeLibs() []string         { return t.excludeLibs }
func (t *Target) SymbolBinding() string         { return t.symbolBinding }
func (t *Target) SymbolPrefix() string          { return t.symbolPrefix }
func (t *Target) PostLinkSteps() []PostLinkStep { return t.postLinks }
func (t *Target) PostLinkDeps() []string        { return t.postLinkDeps }
func (t *Target) PostLinkOutputs() []string     { return t.postLinkOutputs }

func (t *Target) RemoveCFlags(flags ...any) *Target {
	t.cflags = removeStrings(t.cflags, flattenAny(flags)...)
	return t
}

func (t *Target) RemovePublicCFlags(flags ...any) *Target {
	t.publicCFlags = removeStrings(t.publicCFlags, flattenAny(flags)...)
	return t
}

func (t *Target) RemoveCxxFlags(flags ...any) *Target {
	t.cxxflags = removeStrings(t.cxxflags, flattenAny(flags)...)
	return t
}

func (t *Target) RemovePublicCxxFlags(flags ...any) *Target {
	t.publicCxxFlags = removeStrings(t.publicCxxFlags, flattenAny(flags)...)
	return t
}

func (t *Target) RemoveLdFlags(flags ...any) *Target {
	t.ldflags = removeStrings(t.ldflags, flattenAny(flags)...)
	return t
}

func (t *Target) RemovePublicLdFlags(flags ...any) *Target {
	t.publicLdFlags = removeStrings(t.publicLdFlags, flattenAny(flags)...)
	return t
}

func (t *Target) RemoveDefines(defines ...any) *Target {
	t.defines = removeStrings(t.defines, flattenAny(defines)...)
	return t
}

func (t *Target) RemovePublicDefines(defines ...any) *Target {
	t.publicDefines = removeStrings(t.publicDefines, flattenAny(defines)...)
	return t
}

func (t *Target) RemoveIncludes(dirs ...any) *Target {
	t.includes = removeStrings(t.includes, flattenAny(dirs)...)
	return t
}

func (t *Target) RemovePublicIncludes(dirs ...any) *Target {
	t.publicIncludes = removeStrings(t.publicIncludes, flattenAny(dirs)...)
	return t
}

func (t *Target) RemoveLinks(libs ...any) *Target {
	t.links = removeStrings(t.links, flattenAny(libs)...)
	return t
}

func (t *Target) RemovePublicLinks(libs ...any) *Target {
	t.publicLinks = removeStrings(t.publicLinks, flattenAny(libs)...)
	return t
}

func (t *Target) RemoveProvidedLibs(libs ...any) *Target {
	t.providedLibs = removeStrings(t.providedLibs, flattenAny(libs)...)
	return t
}

func (t *Target) RemoveDeps(targets ...any) *Target {
	t.deps = removeStrings(t.deps, flattenAny(targets)...)
	return t
}

func flattenAny(items []any) []string {
	var result []string
	for _, item := range items {
		switch v := item.(type) {
		case nil:
		case string:
			if v != "" {
				result = append(result, v)
			}
		case []string:
			for _, s := range v {
				if s != "" {
					result = append(result, s)
				}
			}
		case []any:
			result = append(result, flattenAny(v)...)
		default:
			fatalScript("", "Add*", "unsupported argument type %T (%v); only string, []string and []any are accepted", item, item)
		}
	}
	return result
}

func removeStrings(slice []string, remove ...string) []string {
	if len(remove) == 0 {
		return slice
	}
	removeSet := make(map[string]bool, len(remove))
	for _, r := range remove {
		removeSet[r] = true
	}
	var result []string
	for _, s := range slice {
		if !removeSet[s] {
			result = append(result, s)
		}
	}
	return result
}
