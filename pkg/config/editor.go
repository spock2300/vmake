package config

import (
	"maps"

	"github.com/spock2300/vmake/pkg/api"
)

const GlobalScope = "__global__"

type EditorInput struct {
	Options          map[string]map[string]*api.Option
	Values           map[string]map[string]any
	DerivedOptions   map[string]map[string]any
	CurrentToolchain string
	GlobalOptions    map[string]*api.Option
	GlobalValues     map[string]any
	ChipProfiles     map[string]api.ChipProfile
	KConfigs         map[string][]*api.KConfigEntry
	Description      string
	DisplayDefaults  func(toolchain string) map[string]any
}

type Editor struct {
	Options            map[string]map[string]*api.Option
	Values             map[string]map[string]any
	DerivedOptions     map[string]map[string]any
	ExplicitValues     map[string]map[string]bool
	OrigExplicitValues map[string]map[string]bool
	ExplicitGlobals    map[string]bool
	OrigExplicitGlobal map[string]bool

	GlobalOptions   map[string]*api.Option
	GlobalValues    map[string]any
	ChipProfiles    map[string]api.ChipProfile
	DerivedGlobals  map[string]bool
	OrigDerived     map[string]bool
	DisplayDefaults func(toolchain string) map[string]any

	KConfigs        map[string][]*api.KConfigEntry
	OrigValues      map[string]map[string]any
	OrigGlobal      map[string]any
	Desc            string
	OrigDesc        string
	MenuconfigRan   map[string]bool
	PresetValues    map[string]string
	changesDetected bool
}

func NewEditor(in EditorInput) *Editor {
	chipProfiles := in.ChipProfiles
	if chipProfiles == nil {
		chipProfiles = make(map[string]api.ChipProfile)
	}
	values := deepCopyValues(in.Values)
	explicitValues := make(map[string]map[string]bool)
	for pkgName, pkgVals := range in.Values {
		for k, v := range pkgVals {
			if v == nil {
				continue
			}
			if opt := in.Options[pkgName][k]; opt != nil && opt.IsGlobal() {
				continue
			}
			if explicitValues[pkgName] == nil {
				explicitValues[pkgName] = make(map[string]bool)
			}
			explicitValues[pkgName][k] = true
		}
	}
	for pkgName, opts := range in.Options {
		if values[pkgName] == nil {
			values[pkgName] = make(map[string]any)
		}
		for optName, opt := range opts {
			if opt.IsGlobal() {
				delete(values[pkgName], optName)
				continue
			}
			if _, ok := values[pkgName][optName]; !ok {
				if derivedPkg := in.DerivedOptions[pkgName]; derivedPkg != nil {
					if dv, hasDerived := derivedPkg[optName]; hasDerived && dv != nil {
						values[pkgName][optName] = dv
						continue
					}
				}
				if def := opt.Default(); def != nil {
					values[pkgName][optName] = def
				}
			}
		}
	}

	globalValues := deepCopyGlobal(in.GlobalValues)
	explicitGlobals := make(map[string]bool)
	for name, val := range in.GlobalValues {
		if val == nil {
			continue
		}
		if name == api.ToolchainOptionName || name == api.TargetOSOptionName || name == api.TargetTripleOptionName {
			if s, ok := val.(string); !ok || s == "" {
				continue
			}
		}
		explicitGlobals[name] = true
	}

	e := &Editor{
		Options:         in.Options,
		Values:          values,
		DerivedOptions:  deepCopyValues(in.DerivedOptions),
		GlobalOptions:   in.GlobalOptions,
		GlobalValues:    globalValues,
		ChipProfiles:    chipProfiles,
		DisplayDefaults: in.DisplayDefaults,
		KConfigs:        in.KConfigs,
		Desc:            in.Description,
		OrigDesc:        in.Description,
		MenuconfigRan:   make(map[string]bool),
		PresetValues:    make(map[string]string),
	}

	explicitToolchain := explicitGlobals[api.ToolchainOptionName]
	chipName := e.ChipFor(globalValues)
	profile, hasProfile := chipProfiles[chipName]
	derivedGlobals := make(map[string]bool)
	if !explicitToolchain {
		delete(globalValues, api.ToolchainOptionName)
		toolchainName := in.CurrentToolchain
		if toolchainName == "" && hasProfile {
			toolchainName = profile.PreferredToolchain
		}
		if toolchainName != "" {
			globalValues[api.ToolchainOptionName] = toolchainName
		}
		derivedGlobals[api.ToolchainOptionName] = true
	}
	for _, name := range []string{api.TargetOSOptionName, api.TargetTripleOptionName} {
		if value, ok := globalValues[name].(string); !ok || value == "" {
			derivedGlobals[name] = true
		}
	}
	if _, ok := globalValues[api.ModeOptionName]; !ok {
		if opt, ok := in.GlobalOptions[api.ModeOptionName]; ok {
			if d, ok := opt.Default().(string); ok {
				globalValues[api.ModeOptionName] = d
			}
		}
	}
	if _, ok := globalValues[api.ModeOptionName]; !ok {
		globalValues[api.ModeOptionName] = api.ModeRelease
	}
	fillOptionDefaults(globalValues, in.GlobalOptions)

	e.ExplicitValues = deepCopyBoolValues(explicitValues)
	e.OrigExplicitValues = deepCopyBoolValues(explicitValues)
	e.ExplicitGlobals = maps.Clone(explicitGlobals)
	e.OrigExplicitGlobal = maps.Clone(explicitGlobals)
	e.DerivedGlobals = derivedGlobals
	e.OrigDerived = maps.Clone(derivedGlobals)
	e.OrigValues = deepCopyValues(values)
	e.OrigGlobal = deepCopyGlobal(globalValues)
	return e
}

func isGlobalScope(pkg string) bool {
	return pkg == "" || pkg == GlobalScope
}

func (e *Editor) Get(pkg, name string) any {
	if isGlobalScope(pkg) {
		return e.EffectiveGlobalValue(e.GlobalValues, name)
	}
	return e.LookupPkgOptionValue(e.Values, pkg, name)
}

func (e *Editor) Orig(pkg, name string) any {
	if isGlobalScope(pkg) {
		return e.EffectiveGlobalValue(e.OrigGlobal, name)
	}
	return e.LookupPkgOptionValue(e.OrigValues, pkg, name)
}

func (e *Editor) Default(pkg, name string) any {
	if def := e.DeclaredDefault(pkg, name); def != nil && def != "" {
		return def
	}
	if isGlobalScope(pkg) {
		if def := e.DisplayFallback(name); def != nil {
			return def
		}
	}
	return e.DeclaredDefault(pkg, name)
}

func (e *Editor) DeclaredDefault(pkg, name string) any {
	if !isGlobalScope(pkg) {
		if derivedPkg := e.DerivedOptions[pkg]; derivedPkg != nil {
			if dv, ok := derivedPkg[name]; ok && dv != nil {
				return dv
			}
		}
		if opt, ok := e.Options[pkg][name]; ok {
			return opt.Default()
		}
		return nil
	}
	if profile, ok := e.CurrentChipProfile(); ok {
		switch name {
		case api.ToolchainOptionName:
			if profile.PreferredToolchain != "" {
				return profile.PreferredToolchain
			}
		case api.TargetOSOptionName, api.TargetTripleOptionName:
			toolchainName, _ := e.EffectiveGlobalValue(e.GlobalValues, api.ToolchainOptionName).(string)
			if e.ChipTargetsApply(profile, toolchainName) {
				if name == api.TargetOSOptionName && profile.TargetOS != "" {
					return profile.TargetOS
				}
				if name == api.TargetTripleOptionName && profile.TargetTriple != "" {
					return profile.TargetTriple
				}
			}
		}
	}
	if opt, ok := e.GlobalOptions[name]; ok {
		return opt.Default()
	}
	return nil
}

func (e *Editor) ChipTargetsApply(profile api.ChipProfile, toolchainName string) bool {
	return api.BindChipTargetsApply(profile, toolchainName, func(name string) string {
		if e.DisplayDefaults == nil {
			return ""
		}
		triple, _ := e.DisplayDefaults(name)[api.TargetTripleOptionName].(string)
		return triple
	})
}

func (e *Editor) DisplayFallback(name string) any {
	return e.DisplayFallbackFor(e.GlobalValues, name)
}

func (e *Editor) DisplayFallbackFor(values map[string]any, name string) any {
	if name != api.TargetOSOptionName && name != api.TargetTripleOptionName {
		return e.toolchainDisplayFallback(values, name)
	}
	if profile, ok := e.ChipProfiles[e.ChipFor(values)]; ok {
		toolchainName, _ := e.EffectiveGlobalValue(values, api.ToolchainOptionName).(string)
		if e.ChipTargetsApply(profile, toolchainName) {
			switch name {
			case api.TargetOSOptionName:
				if profile.TargetOS != "" {
					return profile.TargetOS
				}
			case api.TargetTripleOptionName:
				if profile.TargetTriple != "" {
					return profile.TargetTriple
				}
			}
		}
	}
	return e.toolchainDisplayFallback(values, name)
}

func (e *Editor) toolchainDisplayFallback(values map[string]any, name string) any {
	if e.DisplayDefaults == nil {
		return nil
	}
	toolchainName, _ := values[api.ToolchainOptionName].(string)
	if toolchainName == "" {
		toolchainName = "host"
	}
	defaults := e.DisplayDefaults(toolchainName)
	if defaults == nil {
		return nil
	}
	return defaults[name]
}

func (e *Editor) EffectiveGlobalValue(values map[string]any, name string) any {
	if v, ok := values[name]; ok && v != "" {
		return v
	}
	if opt, ok := e.GlobalOptions[name]; ok {
		if def := opt.Default(); def != nil && def != "" {
			return def
		}
		if def := e.DisplayFallbackFor(values, name); def != nil {
			return def
		}
		if v, ok := values[name]; ok {
			return v
		}
		return opt.Default()
	}
	return nil
}

func (e *Editor) LookupPkgOptionValue(values map[string]map[string]any, pkgName, name string) any {
	if pkgVals := values[pkgName]; pkgVals != nil {
		if v, ok := pkgVals[name]; ok && v != nil {
			return v
		}
	}
	if derivedPkg := e.DerivedOptions[pkgName]; derivedPkg != nil {
		if dv, ok := derivedPkg[name]; ok && dv != nil {
			return dv
		}
	}
	if opts := e.Options[pkgName]; opts != nil {
		if opt, ok := opts[name]; ok {
			return opt.Default()
		}
	}
	return nil
}

func (e *Editor) Set(pkg, name string, val any) {
	if isGlobalScope(pkg) {
		e.GlobalValues[name] = val
		if e.OrigExplicitGlobal[name] || !SameValue(e.EffectiveGlobalValue(e.GlobalValues, name), e.EffectiveGlobalValue(e.OrigGlobal, name)) || (isChipGlobal(name) && e.OrigDerived[name]) {
			if e.ExplicitGlobals == nil {
				e.ExplicitGlobals = make(map[string]bool)
			}
			e.ExplicitGlobals[name] = true
		} else {
			delete(e.ExplicitGlobals, name)
		}
		switch name {
		case api.ToolchainOptionName, api.TargetOSOptionName, api.TargetTripleOptionName:
			e.DerivedGlobals[name] = false
		case api.ChipOptionName:
			if value, ok := val.(string); ok {
				e.ApplyChipProfile(value)
			}
		}
		e.CheckChanges()
		return
	}

	if e.Values[pkg] == nil {
		e.Values[pkg] = make(map[string]any)
	}
	e.Values[pkg][name] = val
	if e.OrigExplicitValues[pkg][name] || !SameValue(e.Get(pkg, name), e.Orig(pkg, name)) {
		if e.ExplicitValues == nil {
			e.ExplicitValues = make(map[string]map[string]bool)
		}
		if e.ExplicitValues[pkg] == nil {
			e.ExplicitValues[pkg] = make(map[string]bool)
		}
		e.ExplicitValues[pkg][name] = true
	} else if e.ExplicitValues[pkg] != nil {
		delete(e.ExplicitValues[pkg], name)
	}
	e.CheckChanges()
}

func (e *Editor) Reset(pkg, name string) {
	if isGlobalScope(pkg) {
		wasExplicit := e.OrigExplicitGlobal[name]
		if isChipGlobal(name) && e.OrigDerived[name] {
			e.ClearGlobalValue(name)
			return
		}
		if value, ok := e.OrigGlobal[name]; ok {
			e.GlobalValues[name] = value
			if wasExplicit {
				if e.ExplicitGlobals == nil {
					e.ExplicitGlobals = make(map[string]bool)
				}
				e.ExplicitGlobals[name] = true
			} else {
				delete(e.ExplicitGlobals, name)
			}
			if isChipGlobal(name) {
				e.DerivedGlobals[name] = e.OrigDerived[name]
			}
			if name == api.ChipOptionName {
				if chipVal, ok := value.(string); ok {
					e.ApplyChipProfile(chipVal)
					return
				}
			}
			e.CheckChanges()
			return
		}
		e.ClearGlobalValue(name)
		return
	}
	if e.Values[pkg] == nil {
		e.Values[pkg] = make(map[string]any)
	}
	e.Values[pkg][name] = e.Orig(pkg, name)
	if e.OrigExplicitValues[pkg][name] {
		if e.ExplicitValues == nil {
			e.ExplicitValues = make(map[string]map[string]bool)
		}
		if e.ExplicitValues[pkg] == nil {
			e.ExplicitValues[pkg] = make(map[string]bool)
		}
		e.ExplicitValues[pkg][name] = true
	} else if e.ExplicitValues[pkg] != nil {
		delete(e.ExplicitValues[pkg], name)
	}
	e.CheckChanges()
}

func (e *Editor) ResetToDefault(pkg, name string) {
	def := e.DeclaredDefault(pkg, name)
	if !isGlobalScope(pkg) {
		if e.Values[pkg] == nil {
			e.Values[pkg] = make(map[string]any)
		}
		if def != nil {
			e.Values[pkg][name] = def
		} else {
			delete(e.Values[pkg], name)
		}
		if e.ExplicitValues[pkg] != nil {
			delete(e.ExplicitValues[pkg], name)
		}
		e.CheckChanges()
		return
	}
	delete(e.ExplicitGlobals, name)
	if def != nil && def != "" {
		e.GlobalValues[name] = def
		if isChipGlobal(name) {
			e.DerivedGlobals[name] = true
		}
		if name == api.ChipOptionName {
			if chipVal, ok := def.(string); ok {
				e.ApplyChipProfile(chipVal)
				return
			}
		}
		e.CheckChanges()
		return
	}
	e.ClearGlobalValue(name)
}

func (e *Editor) ClearGlobalValue(name string) {
	delete(e.ExplicitGlobals, name)
	switch name {
	case api.ToolchainOptionName:
		if profile, ok := e.CurrentChipProfile(); ok && profile.PreferredToolchain != "" {
			e.GlobalValues[name] = profile.PreferredToolchain
		} else {
			delete(e.GlobalValues, name)
		}
		e.DerivedGlobals[name] = true
	case api.TargetOSOptionName, api.TargetTripleOptionName:
		delete(e.GlobalValues, name)
		e.DerivedGlobals[name] = true
	case api.ChipOptionName:
		delete(e.GlobalValues, name)
		e.ApplyChipProfile(e.ChipFor(e.GlobalValues))
	default:
		delete(e.GlobalValues, name)
	}
	e.CheckChanges()
}

func (e *Editor) ChipFor(values map[string]any) string {
	if value, ok := values[api.ChipOptionName].(string); ok && value != "" {
		return value
	}
	if opt, ok := e.GlobalOptions[api.ChipOptionName]; ok {
		if value, ok := opt.Default().(string); ok {
			return value
		}
	}
	return ""
}

func (e *Editor) CurrentChipProfile() (api.ChipProfile, bool) {
	chip := e.ChipFor(e.GlobalValues)
	if chip == "" {
		return api.ChipProfile{}, false
	}
	profile, ok := e.ChipProfiles[chip]
	return profile, ok
}

func (e *Editor) ApplyChipProfile(chip string) {
	profile, ok := e.ChipProfiles[chip]
	if !ok {
		if e.DerivedGlobals[api.ToolchainOptionName] {
			delete(e.GlobalValues, api.ToolchainOptionName)
		}
		for _, name := range []string{api.TargetOSOptionName, api.TargetTripleOptionName} {
			if e.DerivedGlobals[name] {
				delete(e.GlobalValues, name)
			}
		}
		e.CheckChanges()
		return
	}
	if e.DerivedGlobals[api.ToolchainOptionName] {
		if profile.PreferredToolchain != "" {
			e.GlobalValues[api.ToolchainOptionName] = profile.PreferredToolchain
		} else {
			delete(e.GlobalValues, api.ToolchainOptionName)
		}
	}
	for _, name := range []string{api.TargetOSOptionName, api.TargetTripleOptionName} {
		if e.DerivedGlobals[name] {
			delete(e.GlobalValues, name)
		}
	}
	e.CheckChanges()
}

func (e *Editor) Visible(pkg string, opt *api.Option) bool {
	if opt == nil {
		return false
	}
	showIf := opt.ShowIf()
	if showIf == nil {
		return true
	}
	if isGlobalScope(pkg) {
		return showIf(buildShowIfContext(GlobalScope, e.GlobalValues, e.GlobalOptions))
	}
	vals := e.Values[pkg]
	opts := e.Options[pkg]
	return showIf(buildShowIfContext(pkg, vals, opts))
}

func buildShowIfContext(pkgName string, values map[string]any, options map[string]*api.Option) *api.ConfigContext {
	cfgCtx := api.NewConfigContext(pkgName)
	api.BindSetLenient(cfgCtx)
	for name, val := range values {
		cfgCtx.SetConfigValue(name, val)
	}
	for name, o := range options {
		cfgCtx.Option(name).SetType(o.Type()).SetDefault(o.Default())
	}
	return cfgCtx
}

func (e *Editor) IsModified(pkg, name string) bool {
	if isGlobalScope(pkg) {
		return e.IsOptionModifiedGlobal(name)
	}
	return e.IsOptionModifiedPkg(pkg, name)
}

func (e *Editor) IsOptionModifiedGlobal(name string) bool {
	if !SameValue(e.EffectiveGlobalValue(e.GlobalValues, name), e.EffectiveGlobalValue(e.OrigGlobal, name)) {
		return true
	}
	return e.ExplicitGlobals[name] != e.OrigExplicitGlobal[name]
}

func (e *Editor) IsOptionModifiedPkg(pkgName, name string) bool {
	cur := e.Values[pkgName][name]
	orig := e.OrigValues[pkgName][name]
	if orig == nil {
		if derivedPkg := e.DerivedOptions[pkgName]; derivedPkg != nil {
			orig = derivedPkg[name]
		}
		if orig == nil {
			if opt, ok := e.Options[pkgName][name]; ok {
				orig = opt.Default()
			}
		}
	}
	if !SameValue(cur, orig) {
		return true
	}
	return e.ExplicitValues[pkgName][name] != e.OrigExplicitValues[pkgName][name]
}

func (e *Editor) CheckChanges() {
	e.changesDetected = e.pkgValuesChanged() || e.globalValuesChanged() || e.ModifiedPresetCount() > 0 || e.Desc != e.OrigDesc
}

func (e *Editor) HasChanges() bool {
	return e.changesDetected
}

func (e *Editor) SetHasChanges(v bool) {
	e.changesDetected = v
}

func (e *Editor) ModifiedCount() int {
	n := e.ModifiedPresetCount()
	if e.Desc != e.OrigDesc {
		n++
	}
	for name := range e.GlobalOptions {
		if e.IsOptionModifiedGlobal(name) {
			n++
		}
	}
	for pkgName, opts := range e.Options {
		for name, opt := range opts {
			if opt.IsGlobal() {
				continue
			}
			if e.IsOptionModifiedPkg(pkgName, name) {
				n++
			}
		}
	}
	return n
}

func (e *Editor) pkgValuesChanged() bool {
	if !valuesEqual(e.Values, e.OrigValues) {
		return true
	}
	for pkgName, opts := range e.Options {
		for name, opt := range opts {
			if opt.IsGlobal() {
				continue
			}
			if e.ExplicitValues[pkgName][name] != e.OrigExplicitValues[pkgName][name] {
				return true
			}
		}
	}
	return false
}

func (e *Editor) globalValuesChanged() bool {
	seen := make(map[string]bool)
	for name := range e.GlobalOptions {
		seen[name] = true
		if e.IsOptionModifiedGlobal(name) {
			return true
		}
	}
	for name, v := range e.GlobalValues {
		if seen[name] {
			continue
		}
		seen[name] = true
		if !SameValue(v, e.OrigGlobal[name]) {
			return true
		}
	}
	for name, v := range e.OrigGlobal {
		if seen[name] {
			continue
		}
		if !SameValue(e.GlobalValues[name], v) {
			return true
		}
	}
	return false
}

func (e *Editor) CurrentPreset(pkg string) string {
	entries, ok := e.KConfigs[pkg]
	if !ok || len(entries) == 0 {
		return ""
	}
	if v, ok := e.PresetValues[pkg]; ok {
		return v
	}
	return entries[0].SelectedPreset()
}

func (e *Editor) SelectPreset(pkg, name string) {
	if e.PresetValues == nil {
		e.PresetValues = make(map[string]string)
	}
	e.PresetValues[pkg] = name
	if entries := e.KConfigs[pkg]; len(entries) > 0 && name == entries[0].SelectedPreset() {
		delete(e.PresetValues, pkg)
	}
	e.CheckChanges()
}

func (e *Editor) ModifiedPresetCount() int {
	n := 0
	for pkgName, preset := range e.PresetValues {
		if entries := e.KConfigs[pkgName]; len(entries) > 0 && preset != entries[0].SelectedPreset() {
			n++
		}
	}
	return n
}

func (e *Editor) MarkMenuconfigRan(pkg string) {
	if e.MenuconfigRan == nil {
		e.MenuconfigRan = make(map[string]bool)
	}
	e.MenuconfigRan[pkg] = true
	e.changesDetected = true
}

func (e *Editor) Description() string     { return e.Desc }
func (e *Editor) OrigDescription() string { return e.OrigDesc }
func (e *Editor) SetDescription(desc string) {
	e.Desc = desc
	e.CheckChanges()
}

func (e *Editor) SelectedToolchain() string {
	if v, ok := e.GlobalValues[api.ToolchainOptionName]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return "host"
}

func (e *Editor) SparseValues() map[string]map[string]any {
	out := make(map[string]map[string]any)
	for pkgName, expMap := range e.ExplicitValues {
		for name, isExplicit := range expMap {
			if !isExplicit {
				continue
			}
			val, ok := e.Values[pkgName][name]
			if !ok || val == nil {
				continue
			}
			if out[pkgName] == nil {
				out[pkgName] = make(map[string]any)
			}
			out[pkgName][name] = val
		}
	}
	return out
}

func (e *Editor) SparseGlobalValues() map[string]any {
	out := make(map[string]any)
	for name, isExplicit := range e.ExplicitGlobals {
		if !isExplicit {
			continue
		}
		if e.DerivedGlobals[name] {
			continue
		}
		val, ok := e.GlobalValues[name]
		if !ok || val == nil {
			continue
		}
		out[name] = val
	}
	return out
}

type CommitSnapshotInput struct {
	Description    string
	Values         map[string]map[string]any
	Toolchain      string
	GlobalValues   map[string]any
	DerivedGlobals map[string]bool
	MenuconfigRan  map[string]bool
	PresetValues   map[string]string
	KConfigs       map[string][]*api.KConfigEntry
}

func CommitSnapshot(base *ConfigFile, snap CommitSnapshotInput, readKConfig func(pkg string, entry *api.KConfigEntry) (string, error)) (*ConfigFile, error) {
	var cfg *ConfigFile
	if base != nil {
		cfg = base.Clone()
	} else {
		cfg = newConfigFile()
	}
	cfg.Description = snap.Description

	configured := make(map[string]bool)
	for pkgName := range cfg.Entries {
		configured[pkgName] = true
	}
	for pkgName := range snap.Values {
		configured[pkgName] = true
	}
	for pkgName := range snap.KConfigs {
		configured[pkgName] = true
	}

	for pkgName := range configured {
		entry := GetEntry(cfg, pkgName)
		entry.Options = snap.Values[pkgName]

		if snap.PresetValues != nil {
			if preset, ok := snap.PresetValues[pkgName]; ok {
				entry.SelectedPreset = preset
			}
		}

		if snap.MenuconfigRan[pkgName] {
			if kconfigs, ok := snap.KConfigs[pkgName]; ok && len(kconfigs) > 0 && readKConfig != nil {
				content, err := readKConfig(pkgName, kconfigs[0])
				if err == nil {
					entry.KConfig = content
				}
			}
		} else if snap.PresetValues != nil {
			if _, presetChanged := snap.PresetValues[pkgName]; presetChanged {
				entry.KConfig = ""
			}
		}

		SetEntry(cfg, pkgName, entry)
	}

	cfg.Global = &GlobalConfig{Options: make(map[string]any)}
	if !snap.DerivedGlobals[api.ToolchainOptionName] {
		if snap.GlobalValues != nil {
			if tc, ok := snap.GlobalValues[api.ToolchainOptionName].(string); ok {
				cfg.Global.Toolchain = tc
			} else if snap.Toolchain != "" {
				cfg.Global.Toolchain = snap.Toolchain
			}
		} else if snap.Toolchain != "" {
			cfg.Global.Toolchain = snap.Toolchain
		}
	}

	for k, v := range snap.GlobalValues {
		if k == api.ToolchainOptionName || snap.DerivedGlobals[k] {
			continue
		}
		if k == api.ModeOptionName {
			if s, ok := v.(string); ok {
				cfg.Global.Mode = s
			}
		} else {
			cfg.Global.Options[k] = v
		}
	}

	PruneEmptyEntries(cfg)
	return cfg, nil
}

func (e *Editor) Commit(base *ConfigFile, readKConfig func(pkg string, entry *api.KConfigEntry) (string, error)) (*ConfigFile, error) {
	return CommitSnapshot(base, CommitSnapshotInput{
		Description:    e.Desc,
		Values:         e.SparseValues(),
		Toolchain:      e.SelectedToolchain(),
		GlobalValues:   e.SparseGlobalValues(),
		DerivedGlobals: e.DerivedGlobals,
		MenuconfigRan:  e.MenuconfigRan,
		PresetValues:   e.PresetValues,
		KConfigs:       e.KConfigs,
	}, readKConfig)
}

func isChipGlobal(name string) bool {
	return name == api.ToolchainOptionName ||
		name == api.TargetOSOptionName ||
		name == api.TargetTripleOptionName
}

func fillOptionDefaults(values map[string]any, opts map[string]*api.Option) {
	for name, opt := range opts {
		if _, ok := values[name]; !ok {
			if def := opt.Default(); def != nil {
				values[name] = def
			}
		}
	}
}

func deepCopyGlobal(src map[string]any) map[string]any {
	dst := make(map[string]any)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func deepCopyValues(src map[string]map[string]any) map[string]map[string]any {
	dst := make(map[string]map[string]any)
	for pkg, opts := range src {
		dst[pkg] = make(map[string]any)
		for k, v := range opts {
			dst[pkg][k] = v
		}
	}
	return dst
}

func deepCopyBoolValues(src map[string]map[string]bool) map[string]map[string]bool {
	dst := make(map[string]map[string]bool)
	for pkg, opts := range src {
		dst[pkg] = maps.Clone(opts)
	}
	return dst
}

func valuesEqual(a, b map[string]map[string]any) bool {
	return maps.EqualFunc(a, b, func(x, y map[string]any) bool {
		return maps.EqualFunc(x, y, SameValue)
	})
}

func SameValue(a, b any) bool {
	if a == b {
		return true
	}
	if af, ok := toFloat(a); ok {
		if bf, ok2 := toFloat(b); ok2 {
			return af == bf
		}
	}
	return false
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
