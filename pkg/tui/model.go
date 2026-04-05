package tui

import (
	"sort"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/buildscript"
)

type TreeNode struct {
	Name       string
	PkgName    string
	Children   []*TreeNode
	Expanded   bool
	IsExternal bool
	Depth      int
	Prefix     string
}

type OptionItem struct {
	Name  string
	Group string
	Opt   *api.Option
}

const GlobalPkgName = "__global__"

type Model struct {
	packages []buildscript.Source
	tree     []*TreeNode
	flat     []*TreeNode
	deps     map[string][]string
	options  map[string]map[string]*api.Option
	values   map[string]map[string]any

	globalOptions map[string]*api.Option
	globalValues  map[string]any

	kconfigs map[string][]*api.KConfigEntry

	selectedPkg string
	focusArea   int

	treeCursor int
	optCursor  int
	optItems   []OptionItem

	editing     bool
	editInput   string
	editChoices []string
	editIdx     int

	width       int
	height      int
	saved       bool
	hasChanges  bool
	confirmQuit bool
	origValues  map[string]map[string]any
	origGlobal  map[string]any
	workDir     string

	runningMenuconfig bool
	menuconfigRan     map[string]bool
	presetValues      map[string]string
}

func NewModel(
	packages []buildscript.Source,
	deps map[string][]string,
	options map[string]map[string]*api.Option,
	values map[string]map[string]any,
	workDir string,
	currentToolchain string,
	globalOptions map[string]*api.Option,
	globalValues map[string]any,
	kconfigs map[string][]*api.KConfigEntry,
) Model {
	if globalValues == nil {
		globalValues = make(map[string]any)
	}
	if _, ok := globalValues["toolchain"]; !ok && currentToolchain != "" {
		globalValues["toolchain"] = currentToolchain
	}
	if _, ok := globalValues["mode"]; !ok {
		if opt, ok := globalOptions["mode"]; ok {
			if d, ok := opt.Default().(string); ok {
				globalValues["mode"] = d
			}
		}
	}
	if _, ok := globalValues["mode"]; !ok {
		globalValues["mode"] = api.ModeDebug
	}

	m := Model{
		packages:      packages,
		deps:          deps,
		options:       options,
		values:        values,
		treeCursor:    0,
		optCursor:     0,
		focusArea:     0,
		workDir:       workDir,
		globalOptions: globalOptions,
		globalValues:  globalValues,
		kconfigs:      kconfigs,
		menuconfigRan: make(map[string]bool),
		presetValues:  make(map[string]string),
	}
	for name, entries := range kconfigs {
		if len(entries) > 0 && len(entries[0].Presets()) > 0 {
			m.presetValues[name] = entries[0].SelectedPreset()
		}
	}
	m.origValues = deepCopyValues(values)
	m.origGlobal = deepCopyGlobal(globalValues)
	m.tree = buildDepTree(packages, deps)
	m.flat = flattenTree(m.tree)

	if len(m.flat) > 0 {
		m.selectFirstPkg()
	}
	return m
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

func buildDepTree(packages []buildscript.Source, deps map[string][]string) []*TreeNode {
	globalNode := &TreeNode{
		Name:     "[Global]",
		PkgName:  GlobalPkgName,
		Expanded: true,
		Depth:    0,
	}

	localSet := make(map[string]bool)
	for _, pkg := range packages {
		localSet[pkg.Name] = true
	}

	pkgMap := make(map[string]buildscript.Source)
	for _, pkg := range packages {
		pkgMap[pkg.Name] = pkg
	}

	depSet := make(map[string]bool)
	for _, depList := range deps {
		for _, d := range depList {
			depSet[d] = true
		}
	}

	var roots []string
	for _, pkg := range packages {
		if !depSet[pkg.Name] {
			roots = append(roots, pkg.Name)
		}
	}

	var localNodes []*TreeNode
	for _, name := range roots {
		localNodes = append(localNodes, buildDepSubtree(name, localSet, pkgMap, deps, 1))
	}

	for i, child := range localNodes {
		if i < len(localNodes)-1 {
			child.Prefix = "├─"
		} else {
			child.Prefix = "└─"
		}
	}

	result := []*TreeNode{globalNode}
	result = append(result, localNodes...)
	return result
}

func buildDepSubtree(name string, localSet map[string]bool, pkgMap map[string]buildscript.Source, deps map[string][]string, depth int) *TreeNode {
	node := &TreeNode{
		Name:    name,
		PkgName: name,
		Depth:   depth,
	}

	if depth <= 2 {
		node.Expanded = true
	}

	if !localSet[name] {
		node.IsExternal = true
	}

	depNames := deps[name]
	if len(depNames) == 0 {
		return node
	}

	for _, depName := range depNames {
		child := buildDepSubtree(depName, localSet, pkgMap, deps, depth+1)
		if child != nil {
			node.Children = append(node.Children, child)
		}
	}

	for i, child := range node.Children {
		if i < len(node.Children)-1 {
			child.Prefix = "├─"
		} else {
			child.Prefix = "└─"
		}
	}

	return node
}

func flattenTree(nodes []*TreeNode) []*TreeNode {
	var result []*TreeNode
	for _, n := range nodes {
		result = append(result, n)
		if n.Expanded && len(n.Children) > 0 {
			result = append(result, flattenTree(n.Children)...)
		}
	}
	return result
}

func (m *Model) selectFirstPkg() {
	if len(m.flat) > 0 && m.flat[0].PkgName == GlobalPkgName {
		m.selectedPkg = GlobalPkgName
		m.buildOptionItems()
		return
	}
	for _, node := range m.flat {
		if node.PkgName != "" {
			m.selectedPkg = node.PkgName
			m.buildOptionItems()
			return
		}
	}
}

func groupAndSortOptions(opts map[string]*api.Option) []OptionItem {
	groups := make(map[string][]OptionItem)
	for name, opt := range opts {
		group := opt.Group()
		if group == "" {
			group = "General"
		}
		groups[group] = append(groups[group], OptionItem{Name: name, Group: group, Opt: opt})
	}
	var groupNames []string
	for g := range groups {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)
	var items []OptionItem
	for _, g := range groupNames {
		gItems := groups[g]
		sort.Slice(gItems, func(i, j int) bool { return gItems[i].Name < gItems[j].Name })
		items = append(items, gItems...)
	}
	return items
}

func buildShowIfContext(pkgName string, values map[string]any, options map[string]*api.Option) *api.ConfigContext {
	cfgCtx := api.NewConfigContext(pkgName)
	for name, val := range values {
		cfgCtx.SetConfigValue(name, val)
	}
	for name, o := range options {
		cfgCtx.Option(name).SetType(o.Type()).SetDefault(o.Default())
	}
	return cfgCtx
}

func (m *Model) buildOptionItems() {
	m.optItems = nil

	if m.selectedPkg == GlobalPkgName {
		m.optItems = groupAndSortOptions(m.globalOptions)
		return
	}

	opts, ok := m.options[m.selectedPkg]
	if !ok {
		return
	}

	m.optItems = groupAndSortOptions(opts)
}

func (m *Model) getValue(name string) any {
	if m.selectedPkg == GlobalPkgName {
		if v, ok := m.globalValues[name]; ok {
			return v
		}
		if opt, ok := m.globalOptions[name]; ok {
			return opt.Default()
		}
		return nil
	}

	if vals, ok := m.values[m.selectedPkg]; ok {
		if v, ok := vals[name]; ok {
			return v
		}
	}
	if opts, ok := m.options[m.selectedPkg]; ok {
		if opt, ok := opts[name]; ok {
			return opt.Default()
		}
	}
	return nil
}

func (m *Model) setValue(name string, val any) {
	if m.selectedPkg == GlobalPkgName {
		m.globalValues[name] = val
		m.checkChanges()
		return
	}

	if m.values[m.selectedPkg] == nil {
		m.values[m.selectedPkg] = make(map[string]any)
	}
	m.values[m.selectedPkg][name] = val
	m.checkChanges()
}

func (m *Model) checkChanges() {
	globalChanged := !globalValuesEqual(m.globalValues, m.origGlobal)
	m.hasChanges = !valuesEqual(m.values, m.origValues) || globalChanged
}

func globalValuesEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, vA := range a {
		vB, ok := b[k]
		if !ok || vA != vB {
			return false
		}
	}
	return true
}

func valuesEqual(a, b map[string]map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for pkg, optsA := range a {
		optsB, ok := b[pkg]
		if !ok || len(optsA) != len(optsB) {
			return false
		}
		for k, vA := range optsA {
			vB, ok := optsB[k]
			if !ok || vA != vB {
				return false
			}
		}
	}
	return true
}

func (m *Model) visibleOptions() []OptionItem {
	var result []OptionItem
	for _, item := range m.optItems {
		if m.shouldShow(item.Opt) {
			result = append(result, item)
		}
	}
	return result
}

func (m *Model) shouldShow(opt *api.Option) bool {
	showIf := opt.ShowIf()
	if showIf == nil {
		return true
	}

	if m.selectedPkg == GlobalPkgName {
		return showIf(buildShowIfContext(GlobalPkgName, m.globalValues, m.globalOptions))
	}

	vals, _ := m.values[m.selectedPkg]
	opts, _ := m.options[m.selectedPkg]
	return showIf(buildShowIfContext(m.selectedPkg, vals, opts))
}

func (m *Model) hasKConfig() bool {
	if m.selectedPkg == "" || m.selectedPkg == GlobalPkgName {
		return false
	}
	entries, ok := m.kconfigs[m.selectedPkg]
	return ok && len(entries) > 0
}

func (m *Model) hasPresets() bool {
	if !m.hasKConfig() {
		return false
	}
	entries := m.kconfigs[m.selectedPkg]
	return len(entries[0].Presets()) > 0
}

func (m *Model) currentPreset() string {
	entries, ok := m.kconfigs[m.selectedPkg]
	if !ok || len(entries) == 0 {
		return ""
	}
	if v, ok := m.presetValues[m.selectedPkg]; ok {
		return v
	}
	return entries[0].SelectedPreset()
}

func (m *Model) selectPreset(name string) {
	if m.presetValues == nil {
		m.presetValues = make(map[string]string)
	}
	m.presetValues[m.selectedPkg] = name
	m.hasChanges = true
}

func (m *Model) presetOptions() []string {
	entries, ok := m.kconfigs[m.selectedPkg]
	if !ok || len(entries) == 0 {
		return nil
	}
	return entries[0].Presets()
}
