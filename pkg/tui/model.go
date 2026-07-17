package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
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

	editing    bool
	editInput  string
	editCursor int

	width       int
	height      int
	saved       bool
	hasChanges  bool
	confirmQuit bool
	confirmBtn  int
	origValues  map[string]map[string]any
	origGlobal  map[string]any
	workDir     string

	runningMenuconfig bool
	menuconfigRan     map[string]bool
	presetValues      map[string]string

	treeWidth int
	treeOff   int
	optOff    int

	hideEmptyPkgs bool
	optCounts     map[string]int

	overlay      overlayKind
	choiceCursor int
	choiceOpt    string
	choiceValues []string

	filterActive bool
	filterInput  string

	renderedHeaderH int
	renderedFooterH int
}

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayChoice
	overlayDetail
)

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
		globalValues["mode"] = api.ModeRelease
	}

	for name, opt := range globalOptions {
		if _, ok := globalValues[name]; !ok {
			if d := opt.Default(); d != nil {
				globalValues[name] = d
			}
		}
	}

	for pkgName, opts := range options {
		if values[pkgName] == nil {
			values[pkgName] = make(map[string]any)
		}
		for name, opt := range opts {
			if _, ok := values[pkgName][name]; !ok {
				if d := opt.Default(); d != nil {
					values[pkgName][name] = d
				}
			}
		}
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
		optCounts:     computeOptCounts(options, globalOptions),
	}
	m.origValues = deepCopyValues(values)
	m.origGlobal = deepCopyGlobal(globalValues)
	m.tree = buildDepTree(packages, deps)
	m.rebuildFlat()

	if len(m.flat) > 0 {
		m.selectFirstPkg()
	}
	return m
}

func computeOptCounts(options map[string]map[string]*api.Option, globalOptions map[string]*api.Option) map[string]int {
	counts := make(map[string]int)
	for pkgName, opts := range options {
		n := 0
		for _, opt := range opts {
			if !opt.IsGlobal() {
				n++
			}
		}
		counts[pkgName] = n
	}
	counts[GlobalPkgName] = len(globalOptions)
	return counts
}

func (m *Model) rebuildFlat() {
	switch {
	case m.filterInput != "":
		m.flat = flattenTreeSearch(m.tree, m.filterInput, m.options, m.globalOptions)
	case m.hideEmptyPkgs:
		m.flat = flattenTreeFiltered(m.tree, m.optCounts, m.kconfigs)
	default:
		m.flat = flattenTree(m.tree)
	}
	m.treeWidth = calcTreeWidth(m.flat, m.optCounts)
}

func flattenTreeSearch(nodes []*TreeNode, q string, options map[string]map[string]*api.Option, globalOptions map[string]*api.Option) []*TreeNode {
	q = strings.ToLower(q)
	var result []*TreeNode
	for _, n := range nodes {
		if searchVisible(n, q, options, globalOptions) {
			result = append(result, n)
			if len(n.Children) > 0 {
				result = append(result, flattenTreeSearch(n.Children, q, options, globalOptions)...)
			}
		}
	}
	return result
}

func searchVisible(n *TreeNode, q string, options map[string]map[string]*api.Option, globalOptions map[string]*api.Option) bool {
	if n.PkgName == "" {
		return true
	}
	if strings.Contains(strings.ToLower(n.Name), q) {
		return true
	}
	var opts map[string]*api.Option
	if n.PkgName == GlobalPkgName {
		opts = globalOptions
	} else {
		opts = options[n.PkgName]
	}
	for name, opt := range opts {
		if strings.Contains(strings.ToLower(name), q) {
			return true
		}
		if strings.Contains(strings.ToLower(opt.Description()), q) {
			return true
		}
	}
	for _, c := range n.Children {
		if searchVisible(c, q, options, globalOptions) {
			return true
		}
	}
	return false
}

func (m *Model) findFirstMatch(query string) (pkgName, optName string) {
	q := strings.ToLower(query)
	for _, node := range m.tree {
		if p, o := searchNodeMatch(node, q, m.options, m.globalOptions); p != "" {
			return p, o
		}
	}
	return "", ""
}

func searchNodeMatch(n *TreeNode, q string, options map[string]map[string]*api.Option, globalOptions map[string]*api.Option) (pkgName, optName string) {
	if n.PkgName != "" {
		if strings.Contains(strings.ToLower(n.Name), q) {
			return n.PkgName, ""
		}
		var opts map[string]*api.Option
		if n.PkgName == GlobalPkgName {
			opts = globalOptions
		} else {
			opts = options[n.PkgName]
		}
		for name, opt := range opts {
			if strings.Contains(strings.ToLower(name), q) || strings.Contains(strings.ToLower(opt.Description()), q) {
				return n.PkgName, name
			}
		}
	}
	for _, c := range n.Children {
		if p, o := searchNodeMatch(c, q, options, globalOptions); p != "" {
			return p, o
		}
	}
	return "", ""
}

func (m *Model) expandToPkg(target string) bool {
	for _, node := range m.tree {
		if expandToPkgRec(node, target) {
			return true
		}
	}
	return false
}

func expandToPkgRec(n *TreeNode, target string) bool {
	if n.PkgName == target {
		return true
	}
	for _, c := range n.Children {
		if expandToPkgRec(c, target) {
			n.Expanded = true
			return true
		}
	}
	return false
}

func (m *Model) jumpToMatch(pkgName, optName string) bool {
	if pkgName == "" {
		return false
	}
	m.expandToPkg(pkgName)
	m.rebuildFlat()
	for i, n := range m.flat {
		if n.PkgName == pkgName {
			m.treeCursor = i
			m.selectCurrentNode()
			break
		}
	}
	if optName != "" {
		visible := m.visibleOptions()
		for i, item := range visible {
			if item.Name == optName {
				m.optCursor = i
				break
			}
		}
	}
	m.focusArea = 0
	m.ensureTreeCursorVisible()
	return true
}

func (m *Model) filteredMatchCount() int {
	n := 0
	for _, node := range m.flat {
		if node.PkgName != "" && node.PkgName != GlobalPkgName {
			n++
		}
	}
	return n
}

func (m *Model) matchedOptionIn(pkg, query string) string {
	q := strings.ToLower(query)
	var opts map[string]*api.Option
	if pkg == GlobalPkgName {
		opts = m.globalOptions
	} else {
		opts = m.options[pkg]
	}
	for name, opt := range opts {
		if strings.Contains(strings.ToLower(name), q) || strings.Contains(strings.ToLower(opt.Description()), q) {
			return name
		}
	}
	return ""
}

func flattenTreeFiltered(nodes []*TreeNode, optCounts map[string]int, kconfigs map[string][]*api.KConfigEntry) []*TreeNode {
	var result []*TreeNode
	for _, n := range nodes {
		if isDisplayable(n, optCounts, kconfigs) {
			result = append(result, n)
			if n.Expanded && len(n.Children) > 0 {
				result = append(result, flattenTreeFiltered(n.Children, optCounts, kconfigs)...)
			}
		}
	}
	return result
}

func isDisplayable(n *TreeNode, optCounts map[string]int, kconfigs map[string][]*api.KConfigEntry) bool {
	if n.PkgName == "" || n.PkgName == GlobalPkgName {
		return true
	}
	if optCounts[n.PkgName] > 0 {
		return true
	}
	if len(kconfigs[n.PkgName]) > 0 {
		return true
	}
	for _, c := range n.Children {
		if isDisplayable(c, optCounts, kconfigs) {
			return true
		}
	}
	return false
}

func (m *Model) collapseAll() {
	setExpandedAll(m.tree, false, true)
	m.rebuildFlat()
	m.ensureTreeCursorVisible()
}

func (m *Model) expandAll() {
	setExpandedAll(m.tree, true, false)
	m.rebuildFlat()
	m.ensureTreeCursorVisible()
}

func setExpandedAll(nodes []*TreeNode, expanded, keepGlobal bool) {
	for _, n := range nodes {
		if keepGlobal && n.PkgName == GlobalPkgName {
			continue
		}
		n.Expanded = expanded
		setExpandedAll(n.Children, expanded, keepGlobal)
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

func calcTreeWidth(flat []*TreeNode, optCounts map[string]int) int {
	maxW := 0
	for _, node := range flat {
		w := node.Depth*2 + utf8.RuneCountInString(node.Prefix) + 2 + utf8.RuneCountInString(node.Name)
		if node.PkgName != "" {
			if c := optCounts[node.PkgName]; c > 0 {
				w += utf8.RuneCountInString(fmt.Sprintf(" (%d)", c))
			}
		}
		if w > maxW {
			maxW = w
		}
	}
	return clamp(maxW+4, 20, 40)
}

func buildDepTree(packages []buildscript.Source, deps map[string][]string) []*TreeNode {
	globalNode := &TreeNode{
		Name:     "Global",
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
		localNodes = append(localNodes, buildDepSubtree(name, localSet, pkgMap, deps, 1, make(map[string]bool)))
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

func buildDepSubtree(name string, localSet map[string]bool, pkgMap map[string]buildscript.Source, deps map[string][]string, depth int, visited map[string]bool) *TreeNode {
	if visited[name] {
		return nil
	}
	visited[name] = true

	node := &TreeNode{
		Name:    name,
		PkgName: name,
		Depth:   depth,
	}

	if !localSet[name] {
		node.IsExternal = true
	}

	depNames := deps[name]
	if len(depNames) == 0 {
		return node
	}

	for _, depName := range depNames {
		child := buildDepSubtree(depName, localSet, pkgMap, deps, depth+1, visited)
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

	filtered := make(map[string]*api.Option, len(opts))
	for name, opt := range opts {
		if opt.IsGlobal() {
			continue
		}
		filtered[name] = opt
	}
	m.optItems = groupAndSortOptions(filtered)
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
		if !ok || !sameValue(vA, vB) {
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
			if !ok || !sameValue(vA, vB) {
				return false
			}
		}
	}
	return true
}

func sameValue(a, b any) bool {
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
	m.checkChanges()
}

func (m *Model) presetOptions() []string {
	entries, ok := m.kconfigs[m.selectedPkg]
	if !ok || len(entries) == 0 {
		return nil
	}
	return entries[0].Presets()
}

func (m *Model) totalOptRows() int {
	visible := m.visibleOptions()
	n := len(visible)
	if m.hasKConfig() {
		if m.hasPresets() {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func (m *Model) ensureTreeCursorVisible() {
	drawH := m.treeItemRows()
	total := len(m.flat)
	if total == 0 {
		return
	}
	if drawH >= total {
		m.treeOff = 0
		return
	}
	if m.treeCursor < m.treeOff {
		m.treeOff = m.treeCursor
	}
	if m.treeCursor >= m.treeOff+drawH {
		m.treeOff = m.treeCursor - drawH + 1
	}
	maxOff := total - drawH
	if m.treeOff > maxOff {
		m.treeOff = maxOff
	}
	if m.treeOff < 0 {
		m.treeOff = 0
	}
}

func (m *Model) contentHeight() int {
	h := m.height - m.headerHeight() - m.footerHeight()
	if h < 1 {
		return 1
	}
	return h
}

func (m *Model) treePanelHeight() int {
	h := m.contentHeight()
	if m.filterActive || m.filterInput != "" {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) treeItemRows() int {
	h := m.treePanelHeight()
	if len(m.flat) > h {
		return h - 1
	}
	return h
}

func (m *Model) optItemRows() int {
	h := m.contentHeight()
	if m.totalOptRows() > h {
		return h - 1
	}
	return h
}

func (m *Model) optCountFor(pkgName string) int {
	if m.optCounts == nil {
		return 0
	}
	return m.optCounts[pkgName]
}

func (m *Model) origValue(name string) any {
	if m.selectedPkg == GlobalPkgName {
		if v, ok := m.origGlobal[name]; ok {
			return v
		}
		if opt, ok := m.globalOptions[name]; ok {
			return opt.Default()
		}
		return nil
	}
	if vals, ok := m.origValues[m.selectedPkg]; ok {
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

func (m *Model) isOptionModified(name string) bool {
	return !sameValue(m.getValue(name), m.origValue(name))
}

func (m *Model) resetOption(name string) {
	m.setValue(name, m.origValue(name))
}

func (m *Model) resetOptionToDefault(name string) {
	def := m.defaultFor(name)
	if def != nil {
		m.setValue(name, def)
	}
}

func (m *Model) defaultFor(name string) any {
	if m.selectedPkg == GlobalPkgName {
		if opt, ok := m.globalOptions[name]; ok {
			return opt.Default()
		}
		return nil
	}
	if opts, ok := m.options[m.selectedPkg]; ok {
		if opt, ok := opts[name]; ok {
			return opt.Default()
		}
	}
	return nil
}

func (m *Model) modifiedCount() int {
	n := 0
	for name := range m.globalOptions {
		if m.isOptionModifiedGlobal(name) {
			n++
		}
	}
	for pkgName, opts := range m.options {
		for name, opt := range opts {
			if opt.IsGlobal() {
				continue
			}
			if m.isOptionModifiedPkg(pkgName, name) {
				n++
			}
		}
	}
	return n
}

func (m *Model) isOptionModifiedGlobal(name string) bool {
	cur := m.globalValues[name]
	orig := m.origGlobal[name]
	if orig == nil {
		if opt, ok := m.globalOptions[name]; ok {
			orig = opt.Default()
		}
	}
	return !sameValue(cur, orig)
}

func (m *Model) isOptionModifiedPkg(pkgName, name string) bool {
	cur := m.values[pkgName][name]
	var orig any
	if vals, ok := m.origValues[pkgName]; ok {
		orig = vals[name]
	}
	if orig == nil {
		if opts, ok := m.options[pkgName]; ok {
			if opt, ok := opts[name]; ok {
				orig = opt.Default()
			}
		}
	}
	return !sameValue(cur, orig)
}

func (m *Model) openChoiceOverlay(name string, choices []string) {
	m.overlay = overlayChoice
	m.choiceOpt = name
	m.choiceValues = choices
	current := fmt.Sprintf("%v", m.getValue(name))
	m.choiceCursor = 0
	for i, v := range choices {
		if v == current {
			m.choiceCursor = i
			break
		}
	}
}

func (m *Model) closeOverlay() {
	m.overlay = overlayNone
	m.choiceOpt = ""
	m.choiceValues = nil
	m.choiceCursor = 0
}

func (m *Model) openDetailOverlay(item OptionItem) {
	m.overlay = overlayDetail
	m.choiceOpt = item.Name
}

func (m *Model) detailOption() *api.Option {
	if m.selectedPkg == GlobalPkgName {
		return m.globalOptions[m.choiceOpt]
	}
	if opts, ok := m.options[m.selectedPkg]; ok {
		return opts[m.choiceOpt]
	}
	return nil
}
