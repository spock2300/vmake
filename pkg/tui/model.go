package tui

import (
	"path/filepath"
	"sort"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/plugin"
)

type TreeNode struct {
	Name     string
	PkgName  string
	Children []*TreeNode
	Expanded bool
}

type OptionItem struct {
	Name  string
	Group string
	Opt   *api.Option
}

type Model struct {
	packages []plugin.Package
	tree     []*TreeNode
	options  map[string]map[string]*api.Option
	values   map[string]map[string]any

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
	workDir     string
}

func NewModel(packages []plugin.Package, options map[string]map[string]*api.Option, values map[string]map[string]any, workDir string) Model {
	m := Model{
		packages:   packages,
		options:    options,
		values:     values,
		treeCursor: 0,
		optCursor:  0,
		focusArea:  0,
		workDir:    workDir,
	}
	m.origValues = deepCopyValues(values)
	m.tree = buildTree(packages, workDir)
	if len(m.tree) > 0 {
		m.selectFirstPkg()
	}
	return m
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

func buildTree(packages []plugin.Package, baseDir string) []*TreeNode {
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

	return flattenChildren(roots)
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
	if m.values[m.selectedPkg] == nil {
		m.values[m.selectedPkg] = make(map[string]any)
	}
	m.values[m.selectedPkg][name] = val
	m.checkChanges()
}

func (m *Model) checkChanges() {
	m.hasChanges = !valuesEqual(m.values, m.origValues)
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

	cfgCtx := api.NewConfigContext(m.selectedPkg)
	for name, val := range m.values[m.selectedPkg] {
		cfgCtx.SetConfigValue(name, val)
	}
	for name, o := range m.options[m.selectedPkg] {
		cfgCtx.Option(name).SetType(o.Type()).SetDefault(o.Default())
	}

	return showIf(cfgCtx)
}
