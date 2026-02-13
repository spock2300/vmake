package api

type Target struct {
	name      string
	kind      TargetKind
	isDefault bool
	files     []string
	includes  []string
	defines   []string
	languages []string
	links     []string
	deps      []string
	cflags    []string
	cxxflags  []string
	ldflags   []string
}

func (t *Target) SetKind(kind TargetKind) *Target {
	t.kind = kind
	return t
}

func (t *Target) SetDefault(isDefault bool) *Target {
	t.isDefault = isDefault
	return t
}

func (t *Target) AddFiles(files ...any) *Target {
	t.files = append(t.files, flattenAny(files)...)
	return t
}

func (t *Target) AddIncludes(dirs ...any) *Target {
	t.includes = append(t.includes, flattenAny(dirs)...)
	return t
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
	t.deps = append(t.deps, targets...)
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

func (t *Target) Name() string        { return t.name }
func (t *Target) Kind() TargetKind    { return t.kind }
func (t *Target) IsDefault() bool     { return t.isDefault }
func (t *Target) Files() []string     { return t.files }
func (t *Target) Includes() []string  { return t.includes }
func (t *Target) Defines() []string   { return t.defines }
func (t *Target) Languages() []string { return t.languages }
func (t *Target) Links() []string     { return t.links }
func (t *Target) Deps() []string      { return t.deps }
func (t *Target) CFlags() []string    { return t.cflags }
func (t *Target) CxxFlags() []string  { return t.cxxflags }
func (t *Target) LdFlags() []string   { return t.ldflags }

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
