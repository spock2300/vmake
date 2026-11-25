package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type TreeNode struct {
	Name       string
	PkgName    string
	Children   []*TreeNode
	Expanded   bool
	IsExternal bool
	IsGroup    bool
	Depth      int
	Prefix     string
}

type OptionItem struct {
	Name  string
	Group string
	Opt   *api.Option
}

const (
	GlobalPkgName        = "__global__"
	RemoteGroupPkgName   = "__remote__"
	InactiveGroupPkgName = "__inactive__"
)

type Model struct {
	editor *config.Editor
	tree   []*TreeNode
	flat   []*TreeNode

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
	confirmQuit bool
	confirmBtn  int
	workDir     string
	configFile  string
	language    language

	descEditing bool
	descInput   string
	descCursor  int

	runningMenuconfig bool
	menuconfigErr     error

	treeWidth int
	treeOff   int
	optOff    int

	hideEmptyPkgs bool
	optCounts     map[string]int

	overlay      overlayKind
	choiceCursor int
	choiceOff    int
	choiceOpt    string
	choiceValues []string
	detailOff    int

	filterActive bool
	filterInput  string

	deps             map[string][]string
	preview          config.Preview
	focusActive      bool
	baseConfig       *config.ConfigFile
	previewFn        func(cfg *config.ConfigFile) (config.Preview, bool)
	localPkgOrder    []string
	localPkgSet      map[string]bool
	navHistory       []string
	upEdgeIdx        int
	downEdgeIdx      int
	downEdgeSelected bool
}

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayChoice
	overlayDetail
	overlayLanguage
)

type Input struct {
	Workbench *config.Workbench
	WorkDir   string
}

func NewModel(in Input) Model {
	wb := in.Workbench
	ed := resolveInitialState(wb)
	localOrder, localSet := extractLocalPackages(wb.Packages)
	m := Model{
		editor:        ed,
		treeCursor:    0,
		optCursor:     0,
		focusArea:     0,
		workDir:       in.WorkDir,
		configFile:    wb.ConfigFile,
		language:      systemLanguage(),
		optCounts:     computeOptCounts(wb.Options, wb.GlobalOptions),
		deps:          normalizeGraphDeps(wb.Packages, wb.Deps),
		preview:       wb.InitialPreview,
		focusActive:   wb.FocusActive,
		baseConfig:    wb.BaseConfig,
		previewFn:     wb.Preview,
		localPkgOrder: localOrder,
		localPkgSet:   localSet,
	}
	if m.focusActive {
		m.tree = m.buildWorkbenchTree()
	} else {
		m.tree = buildDepGraph(wb.Packages, wb.Deps)
	}
	m.rebuildFlat()

	if len(m.flat) > 0 {
		m.selectFirstPkg()
	}
	return m
}

func (m *Model) ed() *config.Editor {
	if m.editor == nil {
		m.editor = &config.Editor{
			Values:          make(map[string]map[string]any),
			ExplicitValues:  make(map[string]map[string]bool),
			ExplicitGlobals: make(map[string]bool),
			GlobalValues:    make(map[string]any),
			DerivedGlobals:  make(map[string]bool),
			MenuconfigRan:   make(map[string]bool),
			PresetValues:    make(map[string]string),
		}
	}
	return m.editor
}

func resolveMakeTool(name string) (string, error) {
	tc, err := toolchain.GetManager().GetToolchain(name)
	if err != nil {
		return "", err
	}
	if tc.Tools.MAKE != "" {
		return toolchain.ResolveToolPath(tc.Tools.MAKE, tc.InstallPath)
	}
	return toolchain.ResolveToolPath(tc.MakeTool(), "")
}

func (m *Model) resolveMake() makeResolver {
	name := getToolchainValue(m.ed().GlobalValues)
	return func() (string, error) { return resolveMakeTool(name) }
}

func (m *Model) toolchainDiagnostics() string {
	mgr := toolchain.GetManager()
	var messages []string
	if errs := mgr.ToolchainErrors(); len(errs) > 0 {
		messages = append(messages, m.textf(textToolchainErrors, len(errs)))
	}
	name := getToolchainValue(m.ed().GlobalValues)
	if _, err := mgr.GetToolchain(name); err != nil {
		messages = append(messages, m.textf(textSelectedToolchain, name, err))
	}
	return strings.Join(messages, "\n")
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
	ed := m.ed()
	if m.focusActive && m.filterInput == "" {
		m.tree = m.buildWorkbenchTree()
	}
	switch {
	case m.filterInput != "":
		m.flat = flattenTreeSearch(m.tree, m.filterInput, ed.Options, ed.GlobalOptions)
	case m.hideEmptyPkgs && !m.focusActive:
		m.flat = flattenTreeFiltered(m.tree, m.optCounts, ed.KConfigs)
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

func nodeDirectMatch(n *TreeNode, q string, options map[string]map[string]*api.Option, globalOptions map[string]*api.Option) bool {
	if n == nil || n.IsGroup || n.PkgName == "" || isSyntheticGroupPkg(n.PkgName) {
		return false
	}
	q = strings.ToLower(q)
	if strings.Contains(strings.ToLower(n.Name), q) {
		return true
	}
	if n.PkgName != GlobalPkgName && strings.Contains(strings.ToLower(n.PkgName), q) {
		return true
	}
	var opts map[string]*api.Option
	if n.PkgName == GlobalPkgName {
		opts = globalOptions
	} else {
		opts = options[n.PkgName]
	}
	for name, opt := range opts {
		if strings.Contains(strings.ToLower(name), q) || strings.Contains(strings.ToLower(opt.Description()), q) {
			return true
		}
	}
	return false
}

func searchVisible(n *TreeNode, q string, options map[string]map[string]*api.Option, globalOptions map[string]*api.Option) bool {
	if n.PkgName == "" && !n.IsGroup {
		return true
	}
	if nodeDirectMatch(n, q, options, globalOptions) {
		return true
	}
	for _, c := range n.Children {
		if searchVisible(c, q, options, globalOptions) {
			return true
		}
	}
	return false
}

func (m *Model) findFirstMatch(query string) (pkgName, optName string) {
	ed := m.ed()
	q := strings.ToLower(query)
	for _, node := range m.tree {
		if p, o := searchNodeMatch(node, q, ed.Options, ed.GlobalOptions); p != "" {
			return p, o
		}
	}
	return "", ""
}

func searchNodeMatch(n *TreeNode, q string, options map[string]map[string]*api.Option, globalOptions map[string]*api.Option) (pkgName, optName string) {
	if !n.IsGroup && n.PkgName != "" && !isSyntheticGroupPkg(n.PkgName) {
		if strings.Contains(strings.ToLower(n.Name), q) || (n.PkgName != GlobalPkgName && strings.Contains(strings.ToLower(n.PkgName), q)) {
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
		if isSyntheticGroupPkg(target) {
			n.Expanded = true
		}
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
	locate := func() int {
		m.expandToPkg(pkgName)
		m.rebuildFlat()
		for i, n := range m.flat {
			if !n.IsGroup && n.PkgName == pkgName {
				return i
			}
		}
		return -1
	}
	idx := locate()
	if idx < 0 && m.hideEmptyPkgs {
		m.hideEmptyPkgs = false
		m.rebuildFlat()
		idx = locate()
	}
	if idx < 0 {
		return false
	}
	m.treeCursor = idx
	m.selectCurrentNode()
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
	ed := m.ed()
	n := 0
	seen := make(map[string]bool)
	for _, node := range m.flat {
		if node.IsGroup || !isRealPackage(node.PkgName) {
			continue
		}
		if seen[node.PkgName] {
			continue
		}
		if m.filterInput != "" && !nodeDirectMatch(node, m.filterInput, ed.Options, ed.GlobalOptions) {
			continue
		}
		seen[node.PkgName] = true
		n++
	}
	return n
}

func (m *Model) matchedOptionIn(pkg, query string) string {
	ed := m.ed()
	q := strings.ToLower(query)
	var opts map[string]*api.Option
	if pkg == GlobalPkgName {
		opts = ed.GlobalOptions
	} else {
		opts = ed.Options[pkg]
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
	if n.PkgName == GlobalPkgName || (n.PkgName == "" && !n.IsGroup) {
		return true
	}
	if !n.IsGroup {
		if optCounts[n.PkgName] > 0 {
			return true
		}
		if len(kconfigs[n.PkgName]) > 0 {
			return true
		}
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

func calcTreeWidth(flat []*TreeNode, optCounts map[string]int) int {
	maxW := 0
	for _, node := range flat {
		w := nodeFixedPrefixRunes(node) + utf8.RuneCountInString(node.Name)
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

func buildRemoteGroupNode(packages []buildscript.Source, localSet map[string]bool, deps map[string][]string) *TreeNode {
	remoteSet := make(map[string]bool)
	for _, pkg := range packages {
		if !localSet[pkg.Name] && pkg.Name != "" {
			remoteSet[pkg.Name] = true
		}
	}
	for name, depList := range deps {
		if !localSet[name] && name != "" {
			remoteSet[name] = true
		}
		for _, dep := range depList {
			if !localSet[dep] && dep != "" {
				remoteSet[dep] = true
			}
		}
	}
	return buildRemoteGroupFromSet(remoteSet, false)
}

func buildRemoteGroupFromSet(remoteSet map[string]bool, expanded bool) *TreeNode {
	if len(remoteSet) == 0 {
		return nil
	}

	type subEntry struct {
		rel  string
		full string
	}
	grouped := make(map[string][]subEntry)
	var roots []string
	seenRoot := make(map[string]bool)

	for name := range remoteSet {
		root, rel := remoteRootAndRel(name)
		if !seenRoot[root] {
			seenRoot[root] = true
			roots = append(roots, root)
		}
		if rel != "" {
			grouped[root] = append(grouped[root], subEntry{rel: rel, full: name})
		}
	}
	sort.Strings(roots)

	remoteNode := &TreeNode{
		Name:     "Remote",
		PkgName:  RemoteGroupPkgName,
		Expanded: expanded,
		IsGroup:  true,
		Depth:    1,
	}

	for _, root := range roots {
		trieRoot := &remoteTrieNode{
			name:     root,
			fullPath: root,
			byName:   make(map[string]*remoteTrieNode),
		}
		for _, sub := range grouped[root] {
			cur := trieRoot
			curPath := root
			for _, seg := range strings.Split(sub.rel, "/") {
				if seg == "" {
					continue
				}
				curPath += "/" + seg
				next, ok := cur.byName[seg]
				if !ok {
					next = &remoteTrieNode{
						name:     seg,
						fullPath: curPath,
						byName:   make(map[string]*remoteTrieNode),
					}
					cur.children = append(cur.children, next)
					cur.byName[seg] = next
				}
				cur = next
			}
		}
		rootNode := &TreeNode{
			Name:       root,
			PkgName:    root,
			IsExternal: true,
			IsGroup:    !remoteSet[root],
			Depth:      2,
			Children:   buildRemoteTreeNodes(trieRoot, remoteSet, 3),
		}
		if len(rootNode.Children) > 0 {
			rootNode.Expanded = true
		}
		remoteNode.Children = append(remoteNode.Children, rootNode)
	}
	setChildPrefixes(remoteNode.Children)
	return remoteNode
}

type remoteTrieNode struct {
	name     string
	fullPath string
	children []*remoteTrieNode
	byName   map[string]*remoteTrieNode
}

func buildRemoteTreeNodes(t *remoteTrieNode, remoteSet map[string]bool, depth int) []*TreeNode {
	if len(t.children) == 0 {
		return nil
	}
	sort.Slice(t.children, func(i, j int) bool {
		return t.children[i].name < t.children[j].name
	})
	var nodes []*TreeNode
	for _, c := range t.children {
		node := &TreeNode{
			Name:       c.name,
			PkgName:    c.fullPath,
			IsExternal: true,
			IsGroup:    !remoteSet[c.fullPath],
			Depth:      depth,
		}
		node.Children = buildRemoteTreeNodes(c, remoteSet, depth+1)
		if len(node.Children) > 0 {
			node.Expanded = true
		}
		nodes = append(nodes, node)
	}
	setChildPrefixes(nodes)
	return nodes
}

func remoteRootAndRel(name string) (root, rel string) {
	parts := strings.SplitN(name, "/", 3)
	if len(parts) < 3 {
		return name, ""
	}
	return parts[0] + "/" + parts[1], parts[2]
}

func setChildPrefixes(children []*TreeNode) {
	for i, child := range children {
		if i < len(children)-1 {
			child.Prefix = "├─"
		} else {
			child.Prefix = "└─"
		}
	}
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

func (m *Model) buildOptionItems() {
	ed := m.ed()
	m.optItems = nil

	if m.selectedPkg == GlobalPkgName {
		m.optItems = groupAndSortOptions(ed.GlobalOptions)
		return
	}

	opts, ok := ed.Options[m.selectedPkg]
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

func (m *Model) effectiveGlobalValue(values map[string]any, name string) any {
	return m.ed().EffectiveGlobalValue(values, name)
}

func (m *Model) selectedOptions() map[string]*api.Option {
	ed := m.ed()
	if m.selectedPkg == GlobalPkgName {
		return ed.GlobalOptions
	}
	return ed.Options[m.selectedPkg]
}

func (m *Model) getValue(name string) any {
	return m.ed().Get(m.selectedPkg, name)
}

func (m *Model) lookupPkgOptionValue(values map[string]map[string]any, pkgName, name string) any {
	return m.ed().LookupPkgOptionValue(values, pkgName, name)
}

func (m *Model) setValue(name string, val any) {
	m.ed().Set(m.selectedPkg, name, val)
	m.refreshPreview()
}

func (m *Model) refreshPreview() {
	if m.previewFn == nil {
		return
	}
	snap, err := m.result().Commit(m.baseConfig, m.ed().KConfigs, nil)
	if err != nil {
		return
	}
	prev, ok := m.previewFn(snap)
	if !ok {
		return
	}
	m.preview = prev
	m.ed().ApplyDerivedOptions(prev.DerivedOptions)
	if m.focusActive && m.filterInput == "" {
		curPkg := m.selectedPkg
		m.rebuildFlat()
		if curPkg != "" {
			found := false
			for i, n := range m.flat {
				if n.PkgName == curPkg {
					m.treeCursor = i
					found = true
					break
				}
			}
			if !found {
				m.expandToPkg(curPkg)
				m.rebuildFlat()
				for i, n := range m.flat {
					if n.PkgName == curPkg {
						m.treeCursor = i
						break
					}
				}
			}
		}
		m.ensureTreeCursorVisible()
	} else {
	}
}

func (m *Model) chipFor(values map[string]any) string {
	return m.ed().ChipFor(values)
}

func (m *Model) currentChipProfile() (api.ChipProfile, bool) {
	return m.ed().CurrentChipProfile()
}

func (m *Model) applyChipProfile(chip string) {
	m.ed().ApplyChipProfile(chip)
}

func (m *Model) checkChanges() {
	m.ed().CheckChanges()
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
	return m.ed().Visible(m.selectedPkg, opt)
}

func (m *Model) hasKConfig() bool {
	if m.selectedPkg == "" || m.selectedPkg == GlobalPkgName {
		return false
	}
	entries, ok := m.ed().KConfigs[m.selectedPkg]
	return ok && len(entries) > 0
}

func (m *Model) hasPresets() bool {
	if !m.hasKConfig() {
		return false
	}
	entries := m.ed().KConfigs[m.selectedPkg]
	return len(entries[0].Presets()) > 0
}

func (m *Model) currentPreset() string {
	return m.ed().CurrentPreset(m.selectedPkg)
}

func (m *Model) selectPreset(name string) {
	m.ed().SelectPreset(m.selectedPkg, name)
	m.refreshPreview()
}

func (m *Model) modifiedPresetCount() int {
	return m.ed().ModifiedPresetCount()
}

func (m *Model) presetOptions() []string {
	entries, ok := m.ed().KConfigs[m.selectedPkg]
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
	contentH := m.chrome().contentH
	drawH := treeItemRows(contentH, m.filterActive || m.filterInput != "", len(m.flat))
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

func (m *Model) optCountFor(pkgName string) int {
	if m.optCounts == nil {
		return 0
	}
	return m.optCounts[pkgName]
}

func (m *Model) origValue(name string) any {
	return m.ed().Orig(m.selectedPkg, name)
}

func (m *Model) isOptionModified(name string) bool {
	return m.ed().IsModified(m.selectedPkg, name)
}

func (m *Model) resetOption(name string) {
	m.ed().Reset(m.selectedPkg, name)
	m.refreshPreview()
}

func (m *Model) resetOptionToDefault(name string) {
	m.ed().ResetToDefault(m.selectedPkg, name)
	m.refreshPreview()
}

func (m *Model) clearGlobalValue(name string) {
	m.ed().ClearGlobalValue(name)
}

func (m *Model) declaredDefault(name string) any {
	return m.ed().DeclaredDefault(m.selectedPkg, name)
}

func (m *Model) chipTargetsApply(profile api.ChipProfile, toolchainName string) bool {
	return m.ed().ChipTargetsApply(profile, toolchainName)
}

func (m *Model) displayFallback(name string) any {
	return m.ed().DisplayFallback(name)
}

func (m *Model) displayFallbackFor(values map[string]any, name string) any {
	return m.ed().DisplayFallbackFor(values, name)
}

func (m *Model) defaultFor(name string) any {
	return m.ed().Default(m.selectedPkg, name)
}

func (m *Model) modifiedCount() int {
	return m.ed().ModifiedCount()
}

func (m *Model) isOptionModifiedGlobal(name string) bool {
	return m.ed().IsOptionModifiedGlobal(name)
}

func (m *Model) isOptionModifiedPkg(pkgName, name string) bool {
	return m.ed().IsOptionModifiedPkg(pkgName, name)
}

func (m *Model) sparseValues() map[string]map[string]any {
	return m.ed().SparseValues()
}

func (m *Model) sparseGlobalValues() map[string]any {
	return m.ed().SparseGlobalValues()
}

func (m *Model) openChoiceOverlay(name string, choices []string) {
	m.overlay = overlayChoice
	m.choiceOpt = name
	m.choiceValues = choices
	m.choiceOff = 0
	current := formatValue(m.getValue(name))
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
	m.choiceOff = 0
	m.detailOff = 0
}

func (m *Model) openDetailOverlay(item OptionItem) {
	m.overlay = overlayDetail
	m.choiceOpt = item.Name
	m.detailOff = 0
}

func (m *Model) detailOption() *api.Option {
	ed := m.ed()
	if m.selectedPkg == GlobalPkgName {
		return ed.GlobalOptions[m.choiceOpt]
	}
	if opts, ok := ed.Options[m.selectedPkg]; ok {
		return opts[m.choiceOpt]
	}
	return nil
}
