package tui

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
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
	GlobalPkgName      = "__global__"
	RemoteGroupPkgName = "__remote__"
)

type Model struct {
	tree           []*TreeNode
	flat           []*TreeNode
	options        map[string]map[string]*api.Option
	values         map[string]map[string]any
	derivedOptions map[string]map[string]any

	explicitValues     map[string]map[string]bool
	origExplicitValues map[string]map[string]bool
	explicitGlobals    map[string]bool
	origExplicitGlobal map[string]bool

	globalOptions  map[string]*api.Option
	globalValues   map[string]any
	chipProfiles   map[string]api.ChipProfile
	derivedGlobals map[string]bool
	origDerived    map[string]bool

	displayDefaults func(toolchain string) map[string]any

	kconfigs map[string][]*api.KConfigEntry

	selectedPkg string
	focusArea   int

	treeCursor int
	optCursor  int
	optItems   []OptionItem

	editing    bool
	editInput  string
	editCursor int

	width             int
	height            int
	saved             bool
	hasChanges        bool
	confirmQuit       bool
	confirmBtn        int
	origValues        map[string]map[string]any
	origGlobal        map[string]any
	workDir           string
	language          language
	languageSelectorX int
	languageSelectorY int
	languageSelectorW int

	description     string
	origDescription string
	descEditing     bool
	descInput       string
	descCursor      int
	descRowOffset   int

	runningMenuconfig bool
	menuconfigErr     error
	menuconfigRan     map[string]bool
	presetValues      map[string]string

	treeWidth       int
	treeOff         int
	optOff          int
	treeMouseRows   []panelRowTarget
	optionMouseRows []panelRowTarget
	renderedTreeW   int
	renderedMainH   int

	hideEmptyPkgs bool
	optCounts     map[string]int

	overlay      overlayKind
	choiceCursor int
	choiceOff    int
	choiceOpt    string
	choiceValues []string
	detailOff    int
	detailRows   int
	detailTotal  int

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
	overlayLanguage
)

type Input struct {
	Packages         []buildscript.Source
	Deps             map[string][]string
	Options          map[string]map[string]*api.Option
	Values           map[string]map[string]any
	DerivedOptions   map[string]map[string]any
	WorkDir          string
	CurrentToolchain string
	GlobalOptions    map[string]*api.Option
	GlobalValues     map[string]any
	ChipProfiles     map[string]api.ChipProfile
	KConfigs         map[string][]*api.KConfigEntry
	Description      string
	DisplayDefaults  func(toolchain string) map[string]any
}

func NewModel(in Input) Model {
	chipProfiles := in.ChipProfiles
	if chipProfiles == nil {
		chipProfiles = make(map[string]api.ChipProfile)
	}
	state := resolveInitialState(in)
	m := Model{
		options:            in.Options,
		values:             state.values,
		derivedOptions:     deepCopyValues(in.DerivedOptions),
		explicitValues:     deepCopyBoolValues(state.explicitValues),
		origExplicitValues: deepCopyBoolValues(state.explicitValues),
		explicitGlobals:    maps.Clone(state.explicitGlobals),
		origExplicitGlobal: maps.Clone(state.explicitGlobals),
		treeCursor:         0,
		optCursor:          0,
		focusArea:          0,
		workDir:            in.WorkDir,
		globalOptions:      in.GlobalOptions,
		globalValues:       state.globalValues,
		chipProfiles:       chipProfiles,
		derivedGlobals:     state.derivedGlobals,
		kconfigs:           in.KConfigs,
		description:        in.Description,
		origDescription:    in.Description,
		displayDefaults:    in.DisplayDefaults,
		language:           systemLanguage(),
		menuconfigRan:      make(map[string]bool),
		presetValues:       make(map[string]string),
		optCounts:          computeOptCounts(in.Options, in.GlobalOptions),
	}
	m.origValues = deepCopyValues(state.values)
	m.origGlobal = deepCopyGlobal(state.globalValues)
	m.origDerived = maps.Clone(state.derivedGlobals)
	m.tree = buildDepTree(in.Packages, in.Deps)
	m.rebuildFlat()

	if len(m.flat) > 0 {
		m.selectFirstPkg()
	}
	return m
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
	name := getToolchainValue(m.globalValues)
	return func() (string, error) { return resolveMakeTool(name) }
}

func (m *Model) toolchainDiagnostics() string {
	mgr := toolchain.GetManager()
	var messages []string
	if errs := mgr.ToolchainErrors(); len(errs) > 0 {
		messages = append(messages, m.textf(textToolchainErrors, len(errs)))
	}
	name := getToolchainValue(m.globalValues)
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

func nodeDirectMatch(n *TreeNode, q string, options map[string]map[string]*api.Option, globalOptions map[string]*api.Option) bool {
	if n == nil || n.IsGroup || n.PkgName == "" || n.PkgName == RemoteGroupPkgName {
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
	q := strings.ToLower(query)
	for _, node := range m.tree {
		if p, o := searchNodeMatch(node, q, m.options, m.globalOptions); p != "" {
			return p, o
		}
	}
	return "", ""
}

func searchNodeMatch(n *TreeNode, q string, options map[string]map[string]*api.Option, globalOptions map[string]*api.Option) (pkgName, optName string) {
	if !n.IsGroup && n.PkgName != "" && n.PkgName != RemoteGroupPkgName {
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
		if target == RemoteGroupPkgName {
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
		if node.IsGroup || node.PkgName == "" || node.PkgName == GlobalPkgName || node.PkgName == RemoteGroupPkgName {
			continue
		}
		if m.filterInput != "" && !nodeDirectMatch(node, m.filterInput, m.options, m.globalOptions) {
			continue
		}
		n++
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
		if pkg.Origin != api.SourceRemote {
			localSet[pkg.Name] = true
		}
	}

	depSet := make(map[string]bool)
	for from, depList := range deps {
		if !localSet[from] {
			continue
		}
		for _, d := range depList {
			if localSet[d] {
				depSet[d] = true
			}
		}
	}

	var roots []string
	for _, pkg := range packages {
		if localSet[pkg.Name] && !depSet[pkg.Name] {
			roots = append(roots, pkg.Name)
		}
	}

	var topNodes []*TreeNode
	for _, name := range roots {
		topNodes = append(topNodes, buildDepSubtree(name, localSet, deps, 1, make(map[string]bool)))
	}

	if remoteNode := buildRemoteGroupNode(packages, localSet, deps); remoteNode != nil {
		topNodes = append(topNodes, remoteNode)
	}

	setChildPrefixes(topNodes)

	result := []*TreeNode{globalNode}
	result = append(result, topNodes...)
	return result
}

func buildDepSubtree(name string, localSet map[string]bool, deps map[string][]string, depth int, visited map[string]bool) *TreeNode {
	if visited[name] {
		return nil
	}
	visited[name] = true

	node := &TreeNode{
		Name:    name,
		PkgName: name,
		Depth:   depth,
	}

	for _, depName := range deps[name] {
		if !localSet[depName] {
			continue
		}
		child := buildDepSubtree(depName, localSet, deps, depth+1, visited)
		if child != nil {
			node.Children = append(node.Children, child)
		}
	}

	setChildPrefixes(node.Children)
	return node
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
		Expanded: false,
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

func buildShowIfContext(pkgName string, values map[string]any, options map[string]*api.Option) *api.ConfigContext {
	cfgCtx := api.NewConfigContext(pkgName).SetLenient()
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

func (m *Model) effectiveGlobalValue(values map[string]any, name string) any {
	if v, ok := values[name]; ok && v != "" {
		return v
	}
	if opt, ok := m.globalOptions[name]; ok {
		if def := opt.Default(); def != nil && def != "" {
			return def
		}
		if def := m.displayFallbackFor(values, name); def != nil {
			return def
		}
		if v, ok := values[name]; ok {
			return v
		}
		return opt.Default()
	}
	return nil
}

func (m *Model) selectedOptions() map[string]*api.Option {
	if m.selectedPkg == GlobalPkgName {
		return m.globalOptions
	}
	return m.options[m.selectedPkg]
}

func lookupOptionValue(values map[string]any, opts map[string]*api.Option, name string) any {
	if v, ok := values[name]; ok {
		return v
	}
	if opt, ok := opts[name]; ok {
		return opt.Default()
	}
	return nil
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

func (m *Model) getValue(name string) any {
	if m.selectedPkg == GlobalPkgName {
		return m.effectiveGlobalValue(m.globalValues, name)
	}
	return m.lookupPkgOptionValue(m.values, m.selectedPkg, name)
}

func (m *Model) lookupPkgOptionValue(values map[string]map[string]any, pkgName, name string) any {
	if pkgVals := values[pkgName]; pkgVals != nil {
		if v, ok := pkgVals[name]; ok && v != nil {
			return v
		}
	}
	if derivedPkg := m.derivedOptions[pkgName]; derivedPkg != nil {
		if dv, ok := derivedPkg[name]; ok && dv != nil {
			return dv
		}
	}
	if opts := m.options[pkgName]; opts != nil {
		if opt, ok := opts[name]; ok {
			return opt.Default()
		}
	}
	return nil
}

func (m *Model) setValue(name string, val any) {
	if m.selectedPkg == GlobalPkgName {
		m.globalValues[name] = val
		if m.origExplicitGlobal[name] || !sameValue(m.effectiveGlobalValue(m.globalValues, name), m.effectiveGlobalValue(m.origGlobal, name)) || (isChipGlobal(name) && m.origDerived[name]) {
			if m.explicitGlobals == nil {
				m.explicitGlobals = make(map[string]bool)
			}
			m.explicitGlobals[name] = true
		} else {
			delete(m.explicitGlobals, name)
		}
		switch name {
		case api.ToolchainOptionName, api.TargetOSOptionName, api.TargetTripleOptionName:
			m.derivedGlobals[name] = false
		case api.ChipOptionName:
			if value, ok := val.(string); ok {
				m.applyChipProfile(value)
			}
		}
		m.checkChanges()
		return
	}

	if m.values[m.selectedPkg] == nil {
		m.values[m.selectedPkg] = make(map[string]any)
	}
	m.values[m.selectedPkg][name] = val
	if m.origExplicitValues[m.selectedPkg][name] || !sameValue(m.getValue(name), m.origValue(name)) {
		if m.explicitValues == nil {
			m.explicitValues = make(map[string]map[string]bool)
		}
		if m.explicitValues[m.selectedPkg] == nil {
			m.explicitValues[m.selectedPkg] = make(map[string]bool)
		}
		m.explicitValues[m.selectedPkg][name] = true
	} else if m.explicitValues[m.selectedPkg] != nil {
		delete(m.explicitValues[m.selectedPkg], name)
	}
	m.checkChanges()
}

func (m *Model) chipFor(values map[string]any) string {
	if value, ok := values[api.ChipOptionName].(string); ok && value != "" {
		return value
	}
	if opt, ok := m.globalOptions[api.ChipOptionName]; ok {
		if value, ok := opt.Default().(string); ok {
			return value
		}
	}
	return ""
}

func (m *Model) currentChipProfile() (api.ChipProfile, bool) {
	chip := m.chipFor(m.globalValues)
	if chip == "" {
		return api.ChipProfile{}, false
	}
	profile, ok := m.chipProfiles[chip]
	return profile, ok
}

func (m *Model) applyChipProfile(chip string) {
	profile, ok := m.chipProfiles[chip]
	if !ok {
		if m.derivedGlobals[api.ToolchainOptionName] {
			delete(m.globalValues, api.ToolchainOptionName)
		}
		for _, name := range []string{api.TargetOSOptionName, api.TargetTripleOptionName} {
			if m.derivedGlobals[name] {
				delete(m.globalValues, name)
			}
		}
		m.checkChanges()
		return
	}
	if m.derivedGlobals[api.ToolchainOptionName] {
		if profile.PreferredToolchain != "" {
			m.globalValues[api.ToolchainOptionName] = profile.PreferredToolchain
		} else {
			delete(m.globalValues, api.ToolchainOptionName)
		}
	}
	for _, name := range []string{api.TargetOSOptionName, api.TargetTripleOptionName} {
		if m.derivedGlobals[name] {
			delete(m.globalValues, name)
		}
	}
	m.checkChanges()
}

func (m *Model) checkChanges() {
	m.hasChanges = m.pkgValuesChanged() || m.globalValuesChanged() || m.modifiedPresetCount() > 0 || m.description != m.origDescription
}

func (m *Model) pkgValuesChanged() bool {
	if !valuesEqual(m.values, m.origValues) {
		return true
	}
	for pkgName, opts := range m.options {
		for name, opt := range opts {
			if opt.IsGlobal() {
				continue
			}
			if m.explicitValues[pkgName][name] != m.origExplicitValues[pkgName][name] {
				return true
			}
		}
	}
	return false
}

func (m *Model) globalValuesChanged() bool {
	seen := make(map[string]bool)
	for name := range m.globalOptions {
		seen[name] = true
		if m.isOptionModifiedGlobal(name) {
			return true
		}
	}
	for name, v := range m.globalValues {
		if seen[name] {
			continue
		}
		seen[name] = true
		if !sameValue(v, m.origGlobal[name]) {
			return true
		}
	}
	for name, v := range m.origGlobal {
		if seen[name] {
			continue
		}
		if !sameValue(m.globalValues[name], v) {
			return true
		}
	}
	return false
}

func globalValuesEqual(a, b map[string]any) bool {
	return maps.EqualFunc(a, b, sameValue)
}

func valuesEqual(a, b map[string]map[string]any) bool {
	return maps.EqualFunc(a, b, func(x, y map[string]any) bool {
		return maps.EqualFunc(x, y, sameValue)
	})
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
	if entries := m.kconfigs[m.selectedPkg]; len(entries) > 0 && name == entries[0].SelectedPreset() {
		delete(m.presetValues, m.selectedPkg)
	}
	m.checkChanges()
}

func (m *Model) modifiedPresetCount() int {
	n := 0
	for pkgName, preset := range m.presetValues {
		if entries := m.kconfigs[pkgName]; len(entries) > 0 && preset != entries[0].SelectedPreset() {
			n++
		}
	}
	return n
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
	h := m.contentHeight() - 1
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
		return max(1, h-1)
	}
	return h
}

func (m *Model) optItemRows() int {
	h := m.contentHeight() - 1 - m.descRowOffset
	if h < 1 {
		h = 1
	}
	if len(m.optionLines()) > h {
		return max(1, h-1)
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
		return m.effectiveGlobalValue(m.origGlobal, name)
	}
	return m.lookupPkgOptionValue(m.origValues, m.selectedPkg, name)
}

func (m *Model) isOptionModified(name string) bool {
	if m.selectedPkg == GlobalPkgName {
		return m.isOptionModifiedGlobal(name)
	}
	return m.isOptionModifiedPkg(m.selectedPkg, name)
}

func (m *Model) resetOption(name string) {
	if m.selectedPkg == GlobalPkgName {
		wasExplicit := m.origExplicitGlobal[name]
		if isChipGlobal(name) && m.origDerived[name] {
			m.clearGlobalValue(name)
			return
		}
		if value, ok := m.origGlobal[name]; ok {
			m.globalValues[name] = value
			if wasExplicit {
				if m.explicitGlobals == nil {
					m.explicitGlobals = make(map[string]bool)
				}
				m.explicitGlobals[name] = true
			} else {
				delete(m.explicitGlobals, name)
			}
			if isChipGlobal(name) {
				m.derivedGlobals[name] = m.origDerived[name]
			}
			if name == api.ChipOptionName {
				if chipVal, ok := value.(string); ok {
					m.applyChipProfile(chipVal)
					return
				}
			}
			m.checkChanges()
			return
		}
		m.clearGlobalValue(name)
		return
	}
	if m.values[m.selectedPkg] == nil {
		m.values[m.selectedPkg] = make(map[string]any)
	}
	m.values[m.selectedPkg][name] = m.origValue(name)
	if m.origExplicitValues[m.selectedPkg][name] {
		if m.explicitValues == nil {
			m.explicitValues = make(map[string]map[string]bool)
		}
		if m.explicitValues[m.selectedPkg] == nil {
			m.explicitValues[m.selectedPkg] = make(map[string]bool)
		}
		m.explicitValues[m.selectedPkg][name] = true
	} else if m.explicitValues[m.selectedPkg] != nil {
		delete(m.explicitValues[m.selectedPkg], name)
	}
	m.checkChanges()
}

func (m *Model) resetOptionToDefault(name string) {
	def := m.declaredDefault(name)
	if m.selectedPkg != GlobalPkgName {
		if m.values[m.selectedPkg] == nil {
			m.values[m.selectedPkg] = make(map[string]any)
		}
		if def != nil {
			m.values[m.selectedPkg][name] = def
		} else {
			delete(m.values[m.selectedPkg], name)
		}
		if m.explicitValues[m.selectedPkg] != nil {
			delete(m.explicitValues[m.selectedPkg], name)
		}
		m.checkChanges()
		return
	}
	delete(m.explicitGlobals, name)
	if def != nil && def != "" {
		m.globalValues[name] = def
		if isChipGlobal(name) {
			m.derivedGlobals[name] = true
		}
		if name == api.ChipOptionName {
			if chipVal, ok := def.(string); ok {
				m.applyChipProfile(chipVal)
				return
			}
		}
		m.checkChanges()
		return
	}
	m.clearGlobalValue(name)
}

func isChipGlobal(name string) bool {
	return name == api.ToolchainOptionName ||
		name == api.TargetOSOptionName ||
		name == api.TargetTripleOptionName
}

func (m *Model) clearGlobalValue(name string) {
	delete(m.explicitGlobals, name)
	switch name {
	case api.ToolchainOptionName:
		if profile, ok := m.currentChipProfile(); ok && profile.PreferredToolchain != "" {
			m.globalValues[name] = profile.PreferredToolchain
		} else {
			delete(m.globalValues, name)
		}
		m.derivedGlobals[name] = true
	case api.TargetOSOptionName, api.TargetTripleOptionName:
		delete(m.globalValues, name)
		m.derivedGlobals[name] = true
	case api.ChipOptionName:
		delete(m.globalValues, name)
		m.applyChipProfile(m.chipFor(m.globalValues))
	default:
		delete(m.globalValues, name)
	}
	m.checkChanges()
}

func (m *Model) declaredDefault(name string) any {
	if m.selectedPkg != GlobalPkgName {
		if derivedPkg := m.derivedOptions[m.selectedPkg]; derivedPkg != nil {
			if dv, ok := derivedPkg[name]; ok && dv != nil {
				return dv
			}
		}
		if opt, ok := m.selectedOptions()[name]; ok {
			return opt.Default()
		}
		return nil
	}
	if profile, ok := m.currentChipProfile(); ok {
		switch name {
		case api.ToolchainOptionName:
			if profile.PreferredToolchain != "" {
				return profile.PreferredToolchain
			}
		case api.TargetOSOptionName, api.TargetTripleOptionName:
			toolchainName, _ := m.effectiveGlobalValue(m.globalValues, api.ToolchainOptionName).(string)
			if m.chipTargetsApply(profile, toolchainName) {
				if name == api.TargetOSOptionName && profile.TargetOS != "" {
					return profile.TargetOS
				}
				if name == api.TargetTripleOptionName && profile.TargetTriple != "" {
					return profile.TargetTriple
				}
			}
		}
	}
	if opt, ok := m.selectedOptions()[name]; ok {
		return opt.Default()
	}
	return nil
}

func (m *Model) chipTargetsApply(profile api.ChipProfile, toolchainName string) bool {
	if profile.Name == "" {
		return false
	}
	if profile.PreferredToolchain == "" || toolchainName == profile.PreferredToolchain {
		return true
	}
	if toolchainName == "" || toolchainName == "host" {
		return false
	}
	if m.displayDefaults == nil {
		return true
	}
	triple, _ := m.displayDefaults(toolchainName)[api.TargetTripleOptionName].(string)
	return triple == "" || triple == profile.TargetTriple
}

func (m *Model) displayFallback(name string) any {
	return m.displayFallbackFor(m.globalValues, name)
}

func (m *Model) displayFallbackFor(values map[string]any, name string) any {
	if name != api.TargetOSOptionName && name != api.TargetTripleOptionName {
		return m.toolchainDisplayFallback(values, name)
	}
	if profile, ok := m.chipProfiles[m.chipFor(values)]; ok {
		toolchainName, _ := m.effectiveGlobalValue(values, api.ToolchainOptionName).(string)
		if m.chipTargetsApply(profile, toolchainName) {
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
	return m.toolchainDisplayFallback(values, name)
}

func (m *Model) toolchainDisplayFallback(values map[string]any, name string) any {
	if m.displayDefaults == nil {
		return nil
	}
	toolchainName, _ := values[api.ToolchainOptionName].(string)
	if toolchainName == "" {
		toolchainName = "host"
	}
	defaults := m.displayDefaults(toolchainName)
	if defaults == nil {
		return nil
	}
	return defaults[name]
}

func (m *Model) defaultFor(name string) any {
	if def := m.declaredDefault(name); def != nil && def != "" {
		return def
	}
	if m.selectedPkg == GlobalPkgName {
		if def := m.displayFallback(name); def != nil {
			return def
		}
	}
	return m.declaredDefault(name)
}

func (m *Model) modifiedCount() int {
	n := m.modifiedPresetCount()
	if m.description != m.origDescription {
		n++
	}
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
	if !sameValue(m.effectiveGlobalValue(m.globalValues, name), m.effectiveGlobalValue(m.origGlobal, name)) {
		return true
	}
	return m.explicitGlobals[name] != m.origExplicitGlobal[name]
}

func (m *Model) isOptionModifiedPkg(pkgName, name string) bool {
	cur := m.values[pkgName][name]
	orig := m.origValues[pkgName][name]
	if orig == nil {
		if derivedPkg := m.derivedOptions[pkgName]; derivedPkg != nil {
			orig = derivedPkg[name]
		}
		if orig == nil {
			if opt, ok := m.options[pkgName][name]; ok {
				orig = opt.Default()
			}
		}
	}
	if !sameValue(cur, orig) {
		return true
	}
	return m.explicitValues[pkgName][name] != m.origExplicitValues[pkgName][name]
}

func (m *Model) sparseValues() map[string]map[string]any {
	out := make(map[string]map[string]any)
	for pkgName, expMap := range m.explicitValues {
		for name, isExplicit := range expMap {
			if !isExplicit {
				continue
			}
			val, ok := m.values[pkgName][name]
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

func (m *Model) sparseGlobalValues() map[string]any {
	out := make(map[string]any)
	for name, isExplicit := range m.explicitGlobals {
		if !isExplicit {
			continue
		}
		if m.derivedGlobals[name] {
			continue
		}
		val, ok := m.globalValues[name]
		if !ok || val == nil {
			continue
		}
		out[name] = val
	}
	return out
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
	m.detailRows = 0
	m.detailTotal = 0
}

func (m *Model) openDetailOverlay(item OptionItem) {
	m.overlay = overlayDetail
	m.choiceOpt = item.Name
	m.detailOff = 0
	m.detailRows = 0
	m.detailTotal = 0
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
