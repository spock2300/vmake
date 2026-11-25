package tui

import (
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

type gutterRole uint8

const (
	gutterNone gutterRole = iota
	gutterFocal
	gutterUpstream
	gutterUpstreamInactive
	gutterDownstream
	gutterDownstreamInactive
)

type egoNeighbor struct {
	PkgName   string
	Label     string
	OptCount  int
	Modified  bool
	Active    bool
	HasConfig bool
}

type egoGraphView struct {
	FocalPkg         string
	FocalLabel       string
	FocalOpts        int
	FocalMod         bool
	FocalActive      bool
	FocalIsRoot      bool
	IsGlobal         bool
	IsGroup          bool
	Upstream         []egoNeighbor
	Downstream       []egoNeighbor
	ActiveUpstream   int
	ActiveDownstream int
}

func isSyntheticGroupPkg(pkgName string) bool {
	return pkgName == LocalGroupPkgName ||
		pkgName == RemoteGroupPkgName ||
		pkgName == InactiveGroupPkgName ||
		strings.HasPrefix(pkgName, containerGroupPrefix) ||
		strings.HasPrefix(pkgName, dirGroupPrefix)
}

func isRealPackage(pkgName string) bool {
	return pkgName != "" && pkgName != GlobalPkgName && !isSyntheticGroupPkg(pkgName)
}

func normalizeGraphDeps(packages []buildscript.Source, deps map[string][]string) map[string][]string {
	out := make(map[string][]string, len(deps)+len(packages))
	for _, pkg := range packages {
		if pkg.Name != "" {
			out[pkg.Name] = nil
		}
	}
	for from, list := range deps {
		if from == "" {
			continue
		}
		seen := make(map[string]bool, len(list))
		var clean []string
		for _, to := range list {
			if to == "" || to == from || seen[to] {
				continue
			}
			seen[to] = true
			clean = append(clean, to)
		}
		out[from] = clean
	}
	return out
}

func extractLocalPackages(packages []buildscript.Source) ([]string, map[string]bool) {
	localSet := make(map[string]bool, len(packages))
	localOrder := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if pkg.Origin != api.SourceRemote && pkg.Name != "" && !localSet[pkg.Name] {
			localSet[pkg.Name] = true
			localOrder = append(localOrder, pkg.Name)
		}
	}
	return localOrder, localSet
}

func computeDAGLayers(nodes []string, edges map[string][]string) map[string]int {
	state := make(map[string]uint8, len(nodes))
	postOrder := make([]string, 0, len(nodes))
	var dfs func(u string)
	dfs = func(u string) {
		state[u] = 1
		for _, v := range edges[u] {
			if state[v] == 0 {
				dfs(v)
			}
		}
		state[u] = 2
		postOrder = append(postOrder, u)
	}
	for _, u := range nodes {
		if state[u] == 0 {
			dfs(u)
		}
	}
	layer := make(map[string]int, len(nodes))
	for i := len(postOrder) - 1; i >= 0; i-- {
		u := postOrder[i]
		for _, v := range edges[u] {
			if layer[u]+1 > layer[v] {
				layer[v] = layer[u] + 1
			}
		}
	}
	return layer
}

func sortLocalTopological(localOrder []string, localSet map[string]bool, normDeps map[string][]string, roots map[string]bool) []string {
	localIdx := make(map[string]int, len(localOrder))
	for i, name := range localOrder {
		localIdx[name] = i
	}
	localDeps := make(map[string][]string, len(localOrder))
	localCallers := make(map[string][]string, len(localOrder))
	allCallers := make(map[string][]string, len(normDeps))
	for _, from := range localOrder {
		for _, to := range normDeps[from] {
			allCallers[to] = append(allCallers[to], from)
			if localSet[to] {
				localDeps[from] = append(localDeps[from], to)
				localCallers[to] = append(localCallers[to], from)
			}
		}
	}
	for from, list := range normDeps {
		if localSet[from] {
			continue
		}
		for _, to := range list {
			allCallers[to] = append(allCallers[to], from)
		}
	}

	layer := computeDAGLayers(localOrder, localDeps)
	for _, u := range localOrder {
		if len(localCallers[u]) == 0 && len(localDeps[u]) > 0 && !roots[u] {
			minDepLayer := 1 << 30
			for _, v := range localDeps[u] {
				if layer[v] < minDepLayer {
					minDepLayer = layer[v]
				}
			}
			if minDepLayer > 1 && minDepLayer < (1<<30) {
				layer[u] = minDepLayer - 1
			}
		}
	}

	isolated := make(map[string]bool, len(localOrder))
	hasConnected := false
	for _, u := range localOrder {
		if len(normDeps[u]) == 0 && len(allCallers[u]) == 0 {
			isolated[u] = true
		} else {
			hasConnected = true
		}
	}

	sorted := make([]string, len(localOrder))
	copy(sorted, localOrder)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if roots[a] != roots[b] {
			return roots[a]
		}
		if hasConnected && isolated[a] != isolated[b] {
			return !isolated[a]
		}
		if layer[a] != layer[b] {
			return layer[a] < layer[b]
		}
		degA := len(localCallers[a]) + len(normDeps[a])
		degB := len(localCallers[b]) + len(normDeps[b])
		if degA != degB {
			return degA > degB
		}
		return localIdx[a] < localIdx[b]
	})
	return sorted
}

func buildDepGraph(packages []buildscript.Source, deps map[string][]string) []*TreeNode {
	normDeps := normalizeGraphDeps(packages, deps)

	globalNode := &TreeNode{
		Name:     "Global",
		PkgName:  GlobalPkgName,
		Expanded: true,
		Depth:    0,
	}

	localOrder, localSet := extractLocalPackages(packages)
	sortedLocals := sortLocalTopological(localOrder, localSet, normDeps, nil)

	result := make([]*TreeNode, 0, 2+len(sortedLocals))
	result = append(result, globalNode)
	for _, name := range sortedLocals {
		result = append(result, &TreeNode{
			Name:    name,
			PkgName: name,
			Depth:   0,
		})
	}

	if remoteNode := buildRemoteGroupNode(packages, localSet, normDeps); remoteNode != nil {
		remoteNode.Depth = 0
		remoteNode.Prefix = ""
		remoteLayer := computeAllPkgLayers(normDeps)
		adjustRemoteDepths(remoteNode.Children, 1, remoteLayer)
		result = append(result, remoteNode)
	}

	return result
}

func (m *Model) allKnownPackages() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(name string) {
		if !isRealPackage(name) || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, name := range m.localPkgOrder {
		add(name)
	}
	for from, list := range m.deps {
		add(from)
		for _, to := range list {
			add(to)
		}
	}
	for pkg := range m.ed().Options {
		add(pkg)
	}
	for pkg := range m.ed().KConfigs {
		add(pkg)
	}
	sort.Strings(out)
	return out
}

func (m *Model) isActivePkg(name string) bool {
	if name == GlobalPkgName {
		return true
	}
	if !isRealPackage(name) {
		return false
	}
	if len(m.preview.ActivePackages) == 0 {
		return true
	}
	return m.preview.ActivePackages[name]
}

func (m *Model) isRootPkg(name string) bool {
	return slices.Contains(m.preview.Roots, name)
}

func (m *Model) hasPkgConfig(name string) bool {
	if name == GlobalPkgName {
		return len(m.ed().GlobalOptions) > 0
	}
	return m.optCounts[name] > 0 || len(m.ed().KConfigs[name]) > 0
}

func (m *Model) pkgHasModified(pkgName string) bool {
	return m.ed().PkgHasModified(pkgName)
}

func (m *Model) pkgModifiedCount(pkgName string) int {
	return m.ed().PkgModifiedCount(pkgName)
}

func (m *Model) pkgExplicitUnmodifiedCount(pkgName string) int {
	ed := m.ed()
	n := 0
	if pkgName == GlobalPkgName {
		for name, opt := range ed.GlobalOptions {
			if ed.Visible(GlobalPkgName, opt) && ed.IsExplicit(GlobalPkgName, name) && !ed.IsOptionModifiedGlobal(name) {
				n++
			}
		}
		return n
	}
	if !isRealPackage(pkgName) {
		return 0
	}
	for name, opt := range ed.Options[pkgName] {
		if opt != nil && !opt.IsGlobal() && ed.Visible(pkgName, opt) && ed.IsExplicit(pkgName, name) && !ed.IsOptionModifiedPkg(pkgName, name) {
			n++
		}
	}
	return n
}

func (m *Model) optionDerivedBy(pkgName, optName string) (string, bool) {
	if !isRealPackage(pkgName) || !m.ed().HasDerivedDefault(pkgName, optName) {
		return "", false
	}
	owner := ""
	if m.preview.DerivedOwners != nil && m.preview.DerivedOwners[pkgName] != nil {
		owner = m.preview.DerivedOwners[pkgName][optName]
	}
	return owner, true
}

func (m *Model) pkgDerivedCount(pkgName string) int {
	if !isRealPackage(pkgName) {
		return 0
	}
	ed := m.ed()
	n := 0
	for name, opt := range ed.Options[pkgName] {
		if opt == nil || opt.IsGlobal() || !ed.Visible(pkgName, opt) {
			continue
		}
		if _, ok := m.optionDerivedBy(pkgName, name); ok {
			n++
		}
	}
	return n
}

func localPkgDisplayName(pkgName string) string {
	if pkgName == api.LocalContainerID {
		return api.LocalContainerID
	}
	if strings.HasPrefix(pkgName, api.LocalContainerID) {
		return strings.TrimPrefix(pkgName, api.LocalContainerID)
	}
	return pkgName
}

func (m *Model) containerOf(pkgName string) string {
	if m.pkgContainer != nil {
		if c := m.pkgContainer[pkgName]; c != "" {
			return c
		}
	}
	if m.localPkgSet[pkgName] || api.IsLocalPackageID(pkgName) {
		return api.LocalContainerID
	}
	root, _ := remoteRootAndRel(pkgName)
	return root
}

func (m *Model) relPathInContainer(pkgName, containerID string) string {
	if containerID == api.LocalContainerID {
		if pkgName == api.LocalContainerID {
			return ""
		}
		if strings.HasPrefix(pkgName, api.LocalContainerID) {
			return strings.TrimPrefix(pkgName, api.LocalContainerID)
		}
		return pkgName
	}
	if pkgName == containerID {
		return ""
	}
	if strings.HasPrefix(pkgName, containerID+"/") {
		return strings.TrimPrefix(pkgName, containerID+"/")
	}
	return pkgName
}

func shortPkgDisplayName(pkgName string, localSet map[string]bool) string {
	if localSet[pkgName] || api.IsLocalPackageID(pkgName) {
		return localPkgDisplayName(pkgName)
	}
	if !strings.Contains(pkgName, "/") {
		return pkgName
	}
	_, rel := remoteRootAndRel(pkgName)
	if rel == "" {
		return pkgName
	}
	return path.Base(rel)
}

func disambiguatedPkgNames(pkgs []string, localSet map[string]bool) map[string]string {
	baseCounts := make(map[string]int, len(pkgs))
	bases := make(map[string]string, len(pkgs))
	for _, p := range pkgs {
		b := shortPkgDisplayName(p, localSet)
		bases[p] = b
		baseCounts[b]++
	}
	out := make(map[string]string, len(pkgs))
	for _, p := range pkgs {
		b := bases[p]
		if baseCounts[b] <= 1 || localSet[p] || api.IsLocalPackageID(p) {
			out[p] = b
			continue
		}
		_, rel := remoteRootAndRel(p)
		parts := strings.Split(rel, "/")
		if len(parts) >= 2 {
			out[p] = parts[len(parts)-2] + "/" + parts[len(parts)-1]
		} else {
			out[p] = p
		}
	}
	return out
}

func collectExpandedGroupStates(nodes []*TreeNode, out map[string]bool) {
	for _, n := range nodes {
		if len(n.Children) > 0 && n.PkgName != "" {
			out[n.PkgName] = n.Expanded
			collectExpandedGroupStates(n.Children, out)
		}
	}
}

func applyExpandedGroupStates(nodes []*TreeNode, states map[string]bool) {
	for _, n := range nodes {
		if len(n.Children) > 0 && n.PkgName != "" {
			if exp, ok := states[n.PkgName]; ok {
				n.Expanded = exp
			}
			applyExpandedGroupStates(n.Children, states)
		}
	}
}

func compactRemoteGroupChains(nodes []*TreeNode, depth int) []*TreeNode {
	for _, n := range nodes {
		for n.IsGroup && len(n.Children) == 1 && n.Children[0].IsGroup {
			child := n.Children[0]
			n.Name = n.Name + "/" + child.Name
			n.PkgName = child.PkgName
			n.Children = child.Children
		}
		n.Depth = depth
		n.Children = compactRemoteGroupChains(n.Children, depth+1)
	}
	setChildPrefixes(nodes)
	return nodes
}

func buildContainerTreeNodes(t *remoteTrieNode, containerID string, pkgSet map[string]bool, depth int) []*TreeNode {
	if len(t.children) == 0 {
		return nil
	}
	sort.Slice(t.children, func(i, j int) bool {
		return t.children[i].name < t.children[j].name
	})
	var nodes []*TreeNode
	for _, c := range t.children {
		isReal := pkgSet[c.fullPath]
		pkgName := c.fullPath
		if !isReal {
			rel := strings.TrimPrefix(c.fullPath, containerID+"/")
			pkgName = dirGroupPkgName(containerID, rel)
		}
		node := &TreeNode{
			Name:       c.name,
			PkgName:    pkgName,
			IsExternal: true,
			IsGroup:    !isReal,
			Depth:      depth,
		}
		node.Children = buildContainerTreeNodes(c, containerID, pkgSet, depth+1)
		if len(node.Children) > 0 {
			node.Expanded = true
		}
		nodes = append(nodes, node)
	}
	setChildPrefixes(nodes)
	return nodes
}

func (m *Model) buildContainerNode(containerID string, pkgs []string, remoteLayer map[string]int) *TreeNode {
	pkgSet := make(map[string]bool, len(pkgs))
	hasRootPkg := false
	trieRoot := &remoteTrieNode{
		name:     containerID,
		fullPath: containerID,
		byName:   make(map[string]*remoteTrieNode),
	}
	for _, full := range pkgs {
		pkgSet[full] = true
		rel := m.relPathInContainer(full, containerID)
		if rel == "" {
			hasRootPkg = true
			continue
		}
		cur := trieRoot
		curPath := containerID
		for _, seg := range strings.Split(rel, "/") {
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

	var children []*TreeNode
	if hasRootPkg {
		children = append(children, &TreeNode{
			Name:       path.Base(containerID),
			PkgName:    containerID,
			IsExternal: true,
			IsGroup:    false,
			Depth:      1,
		})
	}
	subChildren := buildContainerTreeNodes(trieRoot, containerID, pkgSet, 1)
	adjustRemoteDepths(subChildren, 1, remoteLayer)
	subChildren = compactRemoteGroupChains(subChildren, 1)
	children = append(children, subChildren...)
	setChildPrefixes(children)

	return &TreeNode{
		Name:        containerID,
		PkgName:     containerGroupPkgName(containerID),
		ContainerID: containerID,
		IsExternal:  true,
		IsGroup:     true,
		Expanded:    true,
		Depth:       0,
		Children:    children,
	}
}

func (m *Model) buildWorkbenchTree() []*TreeNode {
	if !m.focusActive {
		var sources []buildscript.Source
		for _, name := range m.localPkgOrder {
			sources = append(sources, buildscript.Source{Name: name})
		}
		return buildDepGraph(sources, m.deps)
	}

	expandedStates := make(map[string]bool)
	collectExpandedGroupStates(m.tree, expandedStates)

	rootMap := make(map[string]bool, len(m.preview.Roots))
	for _, r := range m.preview.Roots {
		rootMap[r] = true
	}

	effectiveDeps := m.deps
	if len(m.preview.ActiveDeps) > 0 {
		effectiveDeps = m.preview.ActiveDeps
	}
	sortedLocals := sortLocalTopological(m.localPkgOrder, m.localPkgSet, effectiveDeps, rootMap)

	var result []*TreeNode
	result = append(result, &TreeNode{
		Name:     "Global",
		PkgName:  GlobalPkgName,
		Expanded: true,
		Depth:    0,
	})

	var inactivePkgs []string
	seenInactive := make(map[string]bool)
	markInactive := func(name string) {
		if name == "" || seenInactive[name] {
			return
		}
		seenInactive[name] = true
		inactivePkgs = append(inactivePkgs, name)
	}

	var localChildren []*TreeNode
	for _, name := range sortedLocals {
		if !m.isActivePkg(name) {
			markInactive(name)
			continue
		}
		if m.hideEmptyPkgs && !m.hasPkgConfig(name) {
			continue
		}
		localChildren = append(localChildren, &TreeNode{
			Name:    localPkgDisplayName(name),
			PkgName: name,
			Depth:   1,
		})
	}
	if len(localChildren) > 0 {
		setChildPrefixes(localChildren)
		result = append(result, &TreeNode{
			Name:        "Local",
			PkgName:     LocalGroupPkgName,
			ContainerID: api.LocalContainerID,
			Expanded:    true,
			IsGroup:     true,
			Depth:       0,
			Children:    localChildren,
		})
	}

	allPkgs := m.allKnownPackages()
	activeByContainer := make(map[string][]string)
	for _, name := range allPkgs {
		if m.localPkgSet[name] {
			continue
		}
		if !m.isActivePkg(name) {
			markInactive(name)
			continue
		}
		if m.hideEmptyPkgs && !m.hasPkgConfig(name) {
			continue
		}
		cID := m.containerOf(name)
		activeByContainer[cID] = append(activeByContainer[cID], name)
	}

	if len(activeByContainer) > 0 {
		containerIDs := make([]string, 0, len(activeByContainer))
		for cID := range activeByContainer {
			containerIDs = append(containerIDs, cID)
		}
		sort.Strings(containerIDs)
		remoteLayer := computeAllPkgLayers(effectiveDeps)
		for _, cID := range containerIDs {
			result = append(result, m.buildContainerNode(cID, activeByContainer[cID], remoteLayer))
		}
	}

	var inactiveFiltered []string
	for _, name := range inactivePkgs {
		if m.hideEmptyPkgs && !m.hasPkgConfig(name) {
			continue
		}
		inactiveFiltered = append(inactiveFiltered, name)
	}
	if len(inactiveFiltered) > 0 {
		shortNames := disambiguatedPkgNames(allPkgs, m.localPkgSet)
		sort.SliceStable(inactiveFiltered, func(i, j int) bool {
			a, b := inactiveFiltered[i], inactiveFiltered[j]
			cfgA, cfgB := m.hasPkgConfig(a), m.hasPkgConfig(b)
			if cfgA != cfgB {
				return cfgA
			}
			locA, locB := m.localPkgSet[a], m.localPkgSet[b]
			if locA != locB {
				return locA
			}
			if m.optCounts[a] != m.optCounts[b] {
				return m.optCounts[a] > m.optCounts[b]
			}
			return shortNames[a] < shortNames[b]
		})

		inactiveGroup := &TreeNode{
			Name:     "Inactive",
			PkgName:  InactiveGroupPkgName,
			Expanded: false,
			IsGroup:  true,
			Depth:    0,
		}
		for _, name := range inactiveFiltered {
			lbl := shortNames[name]
			if lbl == "" {
				lbl = name
			}
			inactiveGroup.Children = append(inactiveGroup.Children, &TreeNode{
				Name:       lbl,
				PkgName:    name,
				IsExternal: !m.localPkgSet[name],
				Depth:      1,
			})
		}
		setChildPrefixes(inactiveGroup.Children)
		result = append(result, inactiveGroup)
	}

	applyExpandedGroupStates(result, expandedStates)
	return result
}

func computeAllPkgLayers(normDeps map[string][]string) map[string]int {
	var allNames []string
	seen := make(map[string]bool)
	for from, list := range normDeps {
		if !seen[from] {
			seen[from] = true
			allNames = append(allNames, from)
		}
		for _, to := range list {
			if !seen[to] {
				seen[to] = true
				allNames = append(allNames, to)
			}
		}
	}
	sort.Strings(allNames)
	return computeDAGLayers(allNames, normDeps)
}

func adjustRemoteDepths(nodes []*TreeNode, depth int, remoteLayer map[string]int) {
	if len(nodes) > 1 {
		sort.SliceStable(nodes, func(i, j int) bool {
			li, lj := remoteLayer[nodes[i].PkgName], remoteLayer[nodes[j].PkgName]
			if li != lj {
				return li < lj
			}
			return nodes[i].Name < nodes[j].Name
		})
		setChildPrefixes(nodes)
	}
	for _, n := range nodes {
		n.Depth = depth
		adjustRemoteDepths(n.Children, depth+1, remoteLayer)
	}
}

func nodeFixedPrefixRunes(node *TreeNode) int {
	indentLen := 0
	if node.Depth > 0 {
		indentLen = (node.Depth - 1) * 2
	}
	prefixLen := 0
	for range node.Prefix {
		prefixLen++
	}
	return indentLen + prefixLen + 2
}

func collectSubtreePackages(node *TreeNode, out map[string]bool) {
	if node == nil {
		return
	}
	if !node.IsGroup && isRealPackage(node.PkgName) {
		out[node.PkgName] = true
	}
	for _, c := range node.Children {
		collectSubtreePackages(c, out)
	}
}

func subtreePackageCount(node *TreeNode) int {
	out := make(map[string]bool)
	collectSubtreePackages(node, out)
	return len(out)
}

func rowRepresentedPackages(node *TreeNode) map[string]bool {
	out := make(map[string]bool)
	if node == nil || node.PkgName == GlobalPkgName {
		return out
	}
	if len(node.Children) > 0 && !node.Expanded {
		collectSubtreePackages(node, out)
		return out
	}
	if !node.IsGroup && isRealPackage(node.PkgName) {
		out[node.PkgName] = true
	}
	return out
}

func (m *Model) hasEagerEdge(fromPkg, toPkg string) bool {
	if fromPkg == "" || toPkg == "" || fromPkg == toPkg {
		return false
	}
	return slices.Contains(m.deps[fromPkg], toPkg)
}

func (m *Model) isEdgeActive(fromPkg, toPkg string) bool {
	if !m.isActivePkg(fromPkg) || !m.isActivePkg(toPkg) {
		return false
	}
	if len(m.preview.ActiveDeps) > 0 {
		return slices.Contains(m.preview.ActiveDeps[fromPkg], toPkg)
	}
	return m.hasEagerEdge(fromPkg, toPkg)
}

func (m *Model) egoGraphData() egoGraphView {
	if cur := m.currentTreeNode(); cur != nil && cur.IsGroup {
		return egoGraphView{
			FocalPkg:         cur.PkgName,
			FocalLabel:       m.treeNodeLabel(cur),
			FocalActive:      cur.PkgName != InactiveGroupPkgName,
			IsGroup:          true,
			ActiveUpstream:   -1,
			ActiveDownstream: -1,
		}
	}
	focal := m.selectedPkg
	if focal == "" && m.treeCursor >= 0 && m.treeCursor < len(m.flat) {
		focal = m.flat[m.treeCursor].PkgName
	}
	allPkgs := m.allKnownPackages()
	shortNames := disambiguatedPkgNames(allPkgs, m.localPkgSet)

	makeNeighbor := func(pkg string, edgeActive bool) egoNeighbor {
		lbl := shortNames[pkg]
		if lbl == "" {
			lbl = shortPkgDisplayName(pkg, m.localPkgSet)
		}
		return egoNeighbor{
			PkgName:   pkg,
			Label:     lbl,
			OptCount:  m.optCountFor(pkg),
			Modified:  m.pkgHasModified(pkg),
			Active:    edgeActive,
			HasConfig: m.hasPkgConfig(pkg),
		}
	}

	sortNeighbors := func(list []egoNeighbor) {
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if a.Active != b.Active {
				return a.Active
			}
			locA, locB := m.localPkgSet[a.PkgName], m.localPkgSet[b.PkgName]
			if locA != locB && a.HasConfig == b.HasConfig {
				return locA
			}
			if a.HasConfig != b.HasConfig {
				return a.HasConfig
			}
			if a.OptCount != b.OptCount {
				return a.OptCount > b.OptCount
			}
			return a.Label < b.Label
		})
	}

	if focal == "" || focal == GlobalPkgName {
		var roots []egoNeighbor
		for _, r := range m.preview.Roots {
			roots = append(roots, makeNeighbor(r, m.isActivePkg(r)))
		}
		if len(roots) == 0 {
			for _, p := range m.localPkgOrder {
				if m.isActivePkg(p) && m.hasPkgConfig(p) {
					roots = append(roots, makeNeighbor(p, true))
				}
			}
		}
		sortNeighbors(roots)
		activeCaller := -1
		if len(roots) > 0 {
			activeCaller = ((m.downEdgeIdx % len(roots)) + len(roots)) % len(roots)
		}
		return egoGraphView{
			FocalPkg:         GlobalPkgName,
			FocalLabel:       m.packageLabel(GlobalPkgName),
			FocalOpts:        m.optCountFor(GlobalPkgName),
			FocalMod:         m.pkgHasModified(GlobalPkgName),
			FocalActive:      true,
			IsGlobal:         true,
			Downstream:       roots,
			ActiveUpstream:   -1,
			ActiveDownstream: activeCaller,
		}
	}

	if isSyntheticGroupPkg(focal) {
		return egoGraphView{
			FocalPkg:         focal,
			FocalLabel:       m.packageLabel(focal),
			FocalActive:      focal == RemoteGroupPkgName,
			IsGroup:          true,
			ActiveUpstream:   -1,
			ActiveDownstream: -1,
		}
	}

	seenCaller := make(map[string]bool)
	var callers []egoNeighbor
	for _, p := range allPkgs {
		if p == focal {
			continue
		}
		if m.hasEagerEdge(p, focal) || m.isEdgeActive(p, focal) {
			if !seenCaller[p] {
				seenCaller[p] = true
				callers = append(callers, makeNeighbor(p, m.isEdgeActive(p, focal)))
			}
		}
	}
	sortNeighbors(callers)

	seenDep := make(map[string]bool)
	var deps []egoNeighbor
	for _, d := range m.deps[focal] {
		if d != "" && d != focal && !seenDep[d] {
			seenDep[d] = true
			deps = append(deps, makeNeighbor(d, m.isEdgeActive(focal, d)))
		}
	}
	if len(m.preview.ActiveDeps) > 0 {
		for _, d := range m.preview.ActiveDeps[focal] {
			if d != "" && d != focal && !seenDep[d] {
				seenDep[d] = true
				deps = append(deps, makeNeighbor(d, m.isEdgeActive(focal, d)))
			}
		}
	}
	sortNeighbors(deps)

	activeCaller := -1
	if len(callers) > 0 {
		if !m.downEdgeSelected && len(m.navHistory) > 0 {
			back := m.navHistory[len(m.navHistory)-1]
			for i, c := range callers {
				if c.PkgName == back {
					activeCaller = i
					break
				}
			}
		}
		if activeCaller < 0 {
			activeCaller = ((m.downEdgeIdx % len(callers)) + len(callers)) % len(callers)
		}
	}
	activeDep := -1
	if len(deps) > 0 {
		activeDep = ((m.upEdgeIdx % len(deps)) + len(deps)) % len(deps)
	}

	focalLbl := shortNames[focal]
	if focalLbl == "" {
		focalLbl = shortPkgDisplayName(focal, m.localPkgSet)
	}
	return egoGraphView{
		FocalPkg:         focal,
		FocalLabel:       focalLbl,
		FocalOpts:        m.optCountFor(focal),
		FocalMod:         m.pkgHasModified(focal),
		FocalActive:      m.isActivePkg(focal),
		FocalIsRoot:      m.isRootPkg(focal),
		Downstream:       callers,
		Upstream:         deps,
		ActiveUpstream:   activeDep,
		ActiveDownstream: activeCaller,
	}
}

func (m *Model) jumpToGraphNeighbor(targetPkg string, pushHistory bool) bool {
	if targetPkg == "" {
		return false
	}
	prevPkg := m.selectedPkg
	if !m.jumpToMatch(targetPkg, "") {
		return false
	}
	if pushHistory && prevPkg != "" && prevPkg != targetPkg && !isSyntheticGroupPkg(prevPkg) {
		m.navHistory = append(m.navHistory, prevPkg)
	}
	m.upEdgeIdx = 0
	m.downEdgeIdx = 0
	m.downEdgeSelected = false
	return true
}

func (m *Model) stepUpstream() bool {
	ego := m.egoGraphData()
	if len(ego.Upstream) == 0 || ego.ActiveUpstream < 0 {
		return false
	}
	return m.jumpToGraphNeighbor(ego.Upstream[ego.ActiveUpstream].PkgName, true)
}

func (m *Model) stepDownstream() bool {
	ego := m.egoGraphData()
	if ego.IsGlobal {
		if len(ego.Downstream) == 0 || ego.ActiveDownstream < 0 {
			return false
		}
		return m.jumpToGraphNeighbor(ego.Downstream[ego.ActiveDownstream].PkgName, true)
	}
	if !m.downEdgeSelected && len(m.navHistory) > 0 {
		back := m.navHistory[len(m.navHistory)-1]
		for _, c := range ego.Downstream {
			if c.PkgName == back {
				m.navHistory = m.navHistory[:len(m.navHistory)-1]
				return m.jumpToGraphNeighbor(back, false)
			}
		}
		if back == GlobalPkgName {
			m.navHistory = m.navHistory[:len(m.navHistory)-1]
			return m.jumpToGraphNeighbor(GlobalPkgName, false)
		}
	}
	if len(ego.Downstream) == 0 || ego.ActiveDownstream < 0 {
		return false
	}
	return m.jumpToGraphNeighbor(ego.Downstream[ego.ActiveDownstream].PkgName, false)
}
