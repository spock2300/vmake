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
	name           string
	kind           TargetKind
	isDefault      bool
	isTest         bool
	files          []string
	excludeFiles   []string
	includes       []string
	publicIncludes []string
	includeRules   map[string][]string
	defines        []string
	languages      []string
	links          []string
	deps           []string
	cflags         []string
	cxxflags       []string
	ldflags        []string
	installDir     string
	noInstall      bool
	buildFunc      func(p *Package) error
	linkerScript   string
	postLinks      []PostLinkStep
	genRules       []GenRule
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
	if v {
		t.isDefault = false
	}
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

func (t *Target) SetLanguages(langs ...string) *Target {
	t.languages = langs
	return t
}

func (t *Target) AddLinks(libs ...any) *Target {
	t.links = append(t.links, flattenAny(libs)...)
	return t
}

func (t *Target) AddDeps(targets ...string) *Target {
	for _, d := range targets {
		if d != "" {
			t.deps = append(t.deps, d)
		}
	}
	return t
}

func (t *Target) AddCFlags(flags ...any) *Target {
	t.cflags = append(t.cflags, flattenAny(flags)...)
	return t
}

func (t *Target) AddCxxFlags(flags ...any) *Target {
	t.cxxflags = append(t.cxxflags, flattenAny(flags)...)
	return t
}

func (t *Target) AddLdFlags(flags ...any) *Target {
	t.ldflags = append(t.ldflags, flattenAny(flags)...)
	return t
}

func (t *Target) SetBuildFunc(fn func(p *Package) error) *Target {
	t.buildFunc = fn
	return t
}

func (t *Target) BuildFunc() func(p *Package) error {
	return t.buildFunc
}

func (t *Target) SetLinkerScript(path string) *Target {
	t.linkerScript = path
	return t
}

func (t *Target) AddPostLink(tool string, args ...string) *Target {
	t.postLinks = append(t.postLinks, PostLinkStep{Tool: tool, Args: args})
	return t
}

func (t *Target) AddPostLinkHex() *Target {
	t.postLinks = append(t.postLinks, PostLinkStep{Tool: "objcopy", Args: []string{"-O", "ihex", "{output}", "{output}.hex"}})
	return t
}

func (t *Target) AddPostLinkBin() *Target {
	t.postLinks = append(t.postLinks, PostLinkStep{Tool: "objcopy", Args: []string{"-O", "binary", "{output}", "{output}.bin"}})
	return t
}

func (t *Target) AddPostLinkSize() *Target {
	t.postLinks = append(t.postLinks, PostLinkStep{Tool: "size", Args: []string{"{output}"}})
	return t
}

func (t *Target) AddPostLinkStrip() *Target {
	t.postLinks = append(t.postLinks, PostLinkStep{Tool: "strip", Args: []string{"-o", "{output}.stripped", "{output}"}})
	return t
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
func (t *Target) Languages() []string      { return t.languages }
func (t *Target) Links() []string          { return t.links }
func (t *Target) Deps() []string           { return t.deps }

func (t *Target) HasDep(depRef string) bool {
	for _, d := range t.deps {
		if d == depRef {
			return true
		}
	}
	return false
}
func (t *Target) CFlags() []string   { return t.cflags }
func (t *Target) CxxFlags() []string { return t.cxxflags }
func (t *Target) LdFlags() []string  { return t.ldflags }
func (t *Target) InstallDir() string { return t.installDir }

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
func (t *Target) PostLinkSteps() []PostLinkStep { return t.postLinks }

func (t *Target) RemoveCFlags(flags ...string) *Target {
	t.cflags = removeStrings(t.cflags, flags...)
	return t
}

func (t *Target) RemoveCxxFlags(flags ...string) *Target {
	t.cxxflags = removeStrings(t.cxxflags, flags...)
	return t
}

func (t *Target) RemoveLdFlags(flags ...string) *Target {
	t.ldflags = removeStrings(t.ldflags, flags...)
	return t
}

func (t *Target) RemoveDefines(defines ...string) *Target {
	t.defines = removeStrings(t.defines, defines...)
	return t
}

func (t *Target) RemoveIncludes(dirs ...string) *Target {
	t.includes = removeStrings(t.includes, dirs...)
	return t
}

func (t *Target) RemovePublicIncludes(dirs ...string) *Target {
	t.publicIncludes = removeStrings(t.publicIncludes, dirs...)
	return t
}

func (t *Target) RemoveLinks(libs ...string) *Target {
	t.links = removeStrings(t.links, libs...)
	return t
}

func (t *Target) RemoveDeps(targets ...string) *Target {
	t.deps = removeStrings(t.deps, targets...)
	return t
}

func flattenAny(items []any) []string {
	var result []string
	for _, item := range items {
		switch v := item.(type) {
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
