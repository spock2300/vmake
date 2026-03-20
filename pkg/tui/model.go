package tui

import (
	"path/filepath"
	"sort"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/plugin"
)

type TreeNode struct {
	Name       string
	PkgName    string
	Children   []*TreeNode
	Expanded   bool
	IsExternal bool
}

type OptionItem struct {
	Name  string
	Group string
	Opt   *api.Option
}

const GlobalPkgName = "__global__"

type Model struct {
	packages []plugin.Source
	tree     []*TreeNode
	options  map[string]map[string]*api.Option
	values   map[string]map[string]any

	globalOptions map[string]*api.Option
	globalValues  map[string]any

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
}

func NewModel(
	packages []plugin.Source,
	options map[string]map[string]*api.Option,
	values map[string]map[string]any,
	workDir string,
	currentToolchain string,
	globalOptions map[string]*api.Option,
	globalValues map[string]any,
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
		options:       options,
		values:        values,
		treeCursor:    0,
		optCursor:     0,
		focusArea:     0,
		workDir:       workDir,
		globalOptions: globalOptions,
		globalValues:  globalValues,
	}
	m.origValues = deepCopyValues(values)
	m.origGlobal = deepCopyGlobal(globalValues)
	m.tree = buildTreeWithPackages(packages, workDir, options)

	if len(m.tree) > 0 {
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

func buildTree(packages []plugin.Source, baseDir string) []*TreeNode {
	globalNode := &TreeNode{
		Name:     "[Global]",
		PkgName:  GlobalPkgName,
		Expanded: true,
	}

	nodeMap := make(map[string]*TreeNode)

	for _, pkg := range packages {
		relPath, err := filepath.Rel(baseDir, pkg.Dir)
		if err != nil {
			relPath = pkg.Dir
		}
		parts := strings.Split(relPath, string(filepath.Separator))

		for i := 0; i < len(parts); i++ {
			path := strings.Join(parts[:i+1], string(filepath.Separator))
			if _, ok := nodeMap[path]; !ok {
				nodeMap[path] = &TreeNode{
					Name:     parts[i],
					Expanded: true,
				}
			}
			if i == len(parts)-1 {
				nodeMap[path].PkgName = pkg.Name
			}
		}
	}

	childMap := make(map[string][]*TreeNode)
	var rootKeys []string

	for path, node := range nodeMap {
		parts := strings.Split(path, string(filepath.Separator))
		if len(parts) == 1 {
			rootKeys = append(rootKeys, path)
		} else {
			parentPath := strings.Join(parts[:len(parts)-1], string(filepath.Separator))
			childMap[parentPath] = append(childMap[parentPath], node)
		}
	}

	for path, children := range childMap {
		sort.Slice(children, func(i, j int) bool {
			return children[i].Name < children[j].Name
		})
		if node, ok := nodeMap[path]; ok {
			node.Children = children
		}
	}

	var roots []*TreeNode
	for _, key := range rootKeys {
		roots = append(roots, nodeMap[key])
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].Name < roots[j].Name
	})

	result := []*TreeNode{globalNode}
	return append(result, flattenChildren(roots)...)
}

func flattenChildren(nodes []*TreeNode) []*TreeNode {
	var result []*TreeNode
	for _, n := range nodes {
		result = append(result, n)
		if n.Expanded && len(n.Children) > 0 {
			result = append(result, flattenChildren(n.Children)...)
		}
	}
	return result
}

func (m *Model) selectFirstPkg() {
	if len(m.tree) > 0 && m.tree[0].PkgName == GlobalPkgName {
		m.selectedPkg = GlobalPkgName
		m.buildOptionItems()
		return
	}
	for _, node := range m.tree {
		if node.PkgName != "" {
			m.selectedPkg = node.PkgName
			m.buildOptionItems()
			return
		}
	}
}

func (m *Model) buildOptionItems() {
	m.optItems = nil

	if m.selectedPkg == GlobalPkgName {
		groups := make(map[string][]OptionItem)
		for name, opt := range m.globalOptions {
			group := opt.Group()
			if group == "" {
				group = "General"
			}
			groups[group] = append(groups[group], OptionItem{
				Name:  name,
				Group: group,
				Opt:   opt,
			})
		}

		var groupNames []string
		for g := range groups {
			groupNames = append(groupNames, g)
		}
		sort.Strings(groupNames)

		for _, g := range groupNames {
			items := groups[g]
			sort.Slice(items, func(i, j int) bool {
				return items[i].Name < items[j].Name
			})
			m.optItems = append(m.optItems, items...)
		}
		return
	}

	opts, ok := m.options[m.selectedPkg]
	if !ok {
		return
	}

	groups := make(map[string][]OptionItem)
	for name, opt := range opts {
		group := opt.Group()
		if group == "" {
			group = "General"
		}
		groups[group] = append(groups[group], OptionItem{
			Name:  name,
			Group: group,
			Opt:   opt,
		})
	}

	var groupNames []string
	for g := range groups {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)

	for _, g := range groupNames {
		items := groups[g]
		sort.Slice(items, func(i, j int) bool {
			return items[i].Name < items[j].Name
		})
		m.optItems = append(m.optItems, items...)
	}
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
		cfgCtx := api.NewConfigContext(GlobalPkgName)
		for name, val := range m.globalValues {
			cfgCtx.SetConfigValue(name, val)
		}
		for name, o := range m.globalOptions {
			cfgCtx.Option(name).SetType(o.Type()).SetDefault(o.Default())
		}
		return showIf(cfgCtx)
	}

	cfgCtx := api.NewConfigContext(m.selectedPkg)
	vals, ok := m.values[m.selectedPkg]
	if ok {
		for name, val := range vals {
			cfgCtx.SetConfigValue(name, val)
		}
	}
	opts, ok := m.options[m.selectedPkg]
	if ok {
		for name, o := range opts {
			cfgCtx.Option(name).SetType(o.Type()).SetDefault(o.Default())
		}
	}

	return showIf(cfgCtx)
}

func (m *Model) AddPackageOptions(pkgName string, opts map[string]*api.Option, values map[string]any) {
	if m.options == nil {
		m.options = make(map[string]map[string]*api.Option)
	}
	m.options[pkgName] = opts

	if m.values == nil {
		m.values = make(map[string]map[string]any)
	}
	if m.values[pkgName] == nil {
		m.values[pkgName] = make(map[string]any)
	}

	for name, val := range values {
		m.values[pkgName][name] = val
	}

	m.tree = buildTreeWithPackages(m.packages, m.workDir, m.options)
}

func (m *Model) GetRequireValues() map[string]map[string]any {
	result := make(map[string]map[string]any)
	for pkgName, vals := range m.values {
		if strings.Contains(pkgName, "/") {
			result[pkgName] = vals
		}
	}
	return result
}

func buildTreeWithPackages(packages []plugin.Source, baseDir string, allOptions map[string]map[string]*api.Option) []*TreeNode {
	result := buildTree(packages, baseDir)

	for pkgName := range allOptions {
		if strings.Contains(pkgName, "/") {
			node := &TreeNode{
				Name:       "[" + pkgName + "]",
				PkgName:    pkgName,
				Expanded:   true,
				IsExternal: true,
			}
			result = append(result, node)
		}
	}

	return result
}
