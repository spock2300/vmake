package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

type screen struct {
	tooSmall bool

	headerH  int
	footerH  int
	contentH int

	treeContent string
	treeW       int
	treeRows    []panelRowTarget

	graphH    int
	graphLeft int
	egoBoxes  []egoNodeHitbox

	optionsW        int
	optionsLeft     int
	optionsTopY     int
	optionsContentW int
	optionContent   string
	optionRows      []panelRowTarget
	optionViewport  bool
	optOff          int

	languageSelector  dialogRect
	advancedToggle    dialogRect
	optAdvancedToggle dialogRect
	descRow           dialogRect

	mainH int

	view string

	confirm     dialogLayout
	overlay     dialogLayout
	choiceOff   int
	detailRows  int
	detailTotal int
	detailOff   int
}

type chromeLayout struct {
	header   string
	footer   string
	headerH  int
	footerH  int
	contentH int
	descRow  dialogRect
}

type headerMetrics struct {
	selector     string
	advToggle    string
	langSelector string
	style        lipgloss.Style
	headerW      int
	leftW        int
}

type treeLayout struct {
	content string
	rows    []panelRowTarget
}

type optionLayout struct {
	content   string
	rows      []panelRowTarget
	advToggle dialogRect
	off       int
	viewport  bool
}

func tooSmallSize(width, height int) bool {
	return width < 50 || height < 8
}

func (m *Model) layout() screen {
	var s screen
	if m.width == 0 || m.height == 0 {
		return s
	}
	s.tooSmall = tooSmallSize(m.width, m.height)
	if s.tooSmall && m.width < 50 {
		s.view = m.tooSmallView()
		return s
	}
	if !s.tooSmall {
		switch {
		case m.confirmQuit:
			s.confirm = m.confirmDialogLayout()
			s.view = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, s.confirm.view)
			return s
		case m.overlay != overlayNone:
			m.fillOverlay(&s)
			s.view = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, s.overlay.view)
			return s
		}
	}
	m.fillNormal(&s)
	if s.tooSmall {
		s.view = m.tooSmallView()
	}
	return s
}

func (m *Model) tooSmallView() string {
	return lipgloss.NewStyle().Width(m.width).Height(m.height).Render(m.text(textTerminalTooSmall))
}

func (m *Model) fillNormal(s *screen) {
	ch := m.chrome()
	s.headerH, s.footerH, s.contentH, s.descRow = ch.headerH, ch.footerH, ch.contentH, ch.descRow
	roles := m.gutterRoles()
	tree := m.treeLayout(s.contentH, roles)
	s.treeContent, s.treeRows = tree.content, tree.rows
	treeStyle := treePanelStyle(m.focusArea == 0, m.treePanelWidth())
	treePanel := treeStyle.MaxHeight(s.contentH).Render(s.treeContent)
	s.treeW = lipgloss.Width(treePanel)
	optionStyle := optionsPanelStyle(m.focusArea == 1, m.optionsPanelWidth())
	s.optionsW = m.optionsPanelWidth()
	s.optionsContentW = m.optionsContentWidth()
	s.optionsLeft = s.treeW + optionStyle.GetBorderLeftSize()
	s.languageSelector = m.languageSelectorRect()
	s.advancedToggle = m.advancedToggleRect()
	var rightCol string
	var opt optionLayout
	if egoInner := m.egoGraphInnerHeight(s.contentH); egoInner > 0 {
		graphStyle := egoGraphPanelStyle(m.focusArea == 0, s.optionsW)
		graphContent, boxes := m.egoGraphBody(s.contentH)
		graphPanel := graphStyle.MaxHeight(egoInner + 1).Render(graphContent)
		s.graphH = lipgloss.Height(graphPanel)
		s.egoBoxes = boxes
		s.graphLeft = s.treeW + graphStyle.GetBorderLeftSize() + graphStyle.GetPaddingLeft()
		s.optionsTopY = s.headerH + s.graphH
		opt = m.optionLayout(s.contentH)
		s.optionContent, s.optionRows = opt.content, opt.rows
		s.optOff, s.optionViewport = opt.off, opt.viewport
		optPanel := optionStyle.MaxHeight(max(1, s.contentH-s.graphH)).Render(s.optionContent)
		rightCol = lipgloss.JoinVertical(lipgloss.Left, graphPanel, optPanel)
	} else {
		s.graphH = 0
		s.optionsTopY = s.headerH
		opt = m.optionLayout(s.contentH)
		s.optionContent, s.optionRows = opt.content, opt.rows
		s.optOff, s.optionViewport = opt.off, opt.viewport
		rightCol = optionStyle.MaxHeight(s.contentH).Render(s.optionContent)
	}
	if opt.advToggle.width > 0 {
		s.optAdvancedToggle = dialogRect{
			x:      s.optionsLeft + optionStyle.GetPaddingLeft() + opt.advToggle.x,
			y:      s.optionsTopY + optionStyle.GetBorderTopSize() + optionStyle.GetPaddingTop() + opt.advToggle.y,
			width:  opt.advToggle.width,
			height: opt.advToggle.height,
		}
	}
	for i := range s.treeRows {
		s.treeRows[i].y += s.headerH
	}
	for i := range s.optionRows {
		s.optionRows[i].y += s.optionsTopY
	}
	main := lipgloss.JoinHorizontal(lipgloss.Top, treePanel, rightCol)
	s.mainH = lipgloss.Height(main)
	s.view = lipgloss.JoinVertical(lipgloss.Left, ch.header, main, ch.footer)
}

func (m *Model) fillOverlay(s *screen) {
	switch m.overlay {
	case overlayChoice, overlayLanguage:
		s.overlay, s.choiceOff = m.choiceDialogLayout()
	case overlayDetail:
		s.overlay, s.detailRows, s.detailTotal, s.detailOff = m.detailDialogLayout()
	}
}

func (m *Model) chrome() chromeLayout {
	header, descRow := m.renderHeaderWithDesc(0, true)
	footer := m.renderFooterWithin(0)
	treeStyle := treePanelStyle(m.focusArea == 0, m.treePanelWidth())
	optionStyle := optionsPanelStyle(m.focusArea == 1, m.optionsPanelWidth())
	minTreeHeight := 2 + treeStyle.GetVerticalFrameSize()
	if m.filterActive || m.filterInput != "" {
		minTreeHeight++
	}
	minMainHeight := max(minTreeHeight, 2+optionStyle.GetVerticalFrameSize())
	if lipgloss.Height(header)+lipgloss.Height(footer)+max(minMainHeight, 3) > m.height {
		header, descRow = m.renderHeaderWithDesc(0, false)
	}
	if lipgloss.Height(header)+lipgloss.Height(footer)+minMainHeight > m.height {
		minFooterHeight := 1 + footerBorderStyle().GetVerticalFrameSize()
		header, descRow = m.renderHeaderWithDesc(m.height-minMainHeight-minFooterHeight, false)
		footer = m.renderFooterWithin(m.height - minMainHeight - lipgloss.Height(header))
	}
	headerH := lipgloss.Height(header)
	footerH := lipgloss.Height(footer)
	return chromeLayout{
		header:   header,
		footer:   footer,
		headerH:  headerH,
		footerH:  footerH,
		contentH: max(1, m.height-headerH-footerH),
		descRow:  descRow,
	}
}

func treePanelHeight(contentH int, filtered bool) int {
	h := contentH - 1
	if filtered {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

func treeItemRows(contentH int, filtered bool, total int) int {
	h := treePanelHeight(contentH, filtered)
	if total > h {
		return max(1, h-1)
	}
	return h
}

func (m *Model) optionsContentHeight(contentH int) int {
	h := contentH - m.egoGraphOuterHeight(contentH)
	if h < 1 {
		return 1
	}
	return h
}

func optionScrollOffset(anchor, cursorRow, total, drawH int) int {
	off := anchor
	if cursorRow >= 0 {
		if cursorRow < off {
			off = cursorRow
		}
		if cursorRow >= off+drawH {
			off = cursorRow - drawH + 1
		}
	}
	return clamp(off, 0, max(0, total-drawH))
}

func choiceScrollOffset(anchor, cursor, total, visible int) int {
	off := clamp(anchor, 0, max(0, total-visible))
	if cursor < off {
		off = cursor
	} else if cursor >= off+visible {
		off = cursor - visible + 1
	}
	return off
}

func detailScrollOffset(anchor, rows, total int) int {
	return clamp(anchor, 0, max(0, total-rows))
}

func (m *Model) syncScroll() {
	if m.width == 0 || m.height == 0 || tooSmallSize(m.width, m.height) {
		return
	}
	s := m.layout()
	switch {
	case m.confirmQuit:
	case m.overlay == overlayChoice || m.overlay == overlayLanguage:
		m.choiceOff = s.choiceOff
	case m.overlay == overlayDetail:
		m.detailOff = s.detailOff
	default:
		if s.optionViewport {
			m.optOff = s.optOff
		} else if m.optOff < 0 {
			m.optOff = 0
		}
	}
}

func (m *Model) treeLayout(contentH int, roles []gutterRole) treeLayout {
	var out treeLayout
	contentWidth := max(1, m.treePanelWidth()-2)
	var b strings.Builder
	b.WriteString(panelTitleStyle.MaxWidth(contentWidth).Render(m.text(textPackages)) + "\n")

	var topBar string
	if m.filterActive {
		topBar = filterBoxActiveStyle.Render("/"+m.filterInput+"▎") + "\n"
	} else if m.filterInput != "" {
		n := m.filteredMatchCount()
		topBar = filterBoxAppliedStyle.Render(m.textf(textFilter, m.filterInput, n)) + "\n"
	}

	filtered := m.filterActive || m.filterInput != ""
	panelH := treePanelHeight(contentH, filtered)
	drawH := treeItemRows(contentH, filtered, len(m.flat))

	if len(m.flat) == 0 {
		b.WriteString(topBar)
		b.WriteString(filterBoxAppliedStyle.Render("  " + m.text(textNoMatches)))
		out.content = b.String()
		return out
	}

	total := len(m.flat)
	start := m.treeOff
	end := min(start+drawH, total)

	if topBar != "" {
		topBar = lipgloss.NewStyle().MaxWidth(contentWidth).Render(strings.TrimSuffix(topBar, "\n")) + "\n"
	}
	b.WriteString(topBar)
	rowY := 1 + strings.Count(topBar, "\n")

	for i := start; i < end; i++ {
		node := m.flat[i]
		role := roles[i]
		indent := ""
		if node.Depth > 0 {
			indent = strings.Repeat("  ", node.Depth-1)
		}

		isSelected := i == m.treeCursor
		isFocused := m.focusArea == 0
		isInactive := node.PkgName == InactiveGroupPkgName || (!node.IsGroup && !m.isActivePkg(node.PkgName))
		isActiveNoOpt := !node.IsGroup && node.PkgName != GlobalPkgName && m.isActivePkg(node.PkgName) && !m.hasPkgConfig(node.PkgName)

		icon, iconStyle := treeRowIconAndStyle(node, role, isSelected, isInactive, isActiveNoOpt)

		name := m.treeNodeLabel(node)

		count := 0
		if isGroupBadgeNode(node) {
			if m.focusActive || node.ContainerID != "" {
				count = subtreePackageCount(node)
			}
		} else if !node.IsGroup && node.PkgName != "" {
			count = m.optCountFor(node.PkgName)
		}
		hasMod := !node.IsGroup && node.PkgName != "" && m.pkgHasModified(node.PkgName)

		fixed := nodeFixedPrefixRunes(node)
		badgeLen := 0
		if count > 0 {
			badgeLen = utf8.RuneCountInString(fmt.Sprintf(" (%d)", count))
		}
		modLen := 0
		if hasMod {
			modLen = 2
		}
		avail := contentWidth - fixed - badgeLen - modLen
		if avail < 1 {
			avail = 1
		}
		if utf8.RuneCountInString(name) > avail {
			name = truncateRunes(name, avail)
		}

		if !isSelected || !isFocused {
			switch {
			case isInactive:
				name = inactivePkgStyle.Render(name)
			case isActiveNoOpt:
				name = treeActiveNoOptNameStyle.Render(name)
			case node.IsExternal:
				name = treeActiveRemoteNameStyle.Render(name)
			}
		}

		if count > 0 {
			if isInactive {
				name += inactivePkgStyle.Render(fmt.Sprintf(" (%d)", count))
			} else {
				name += countBadgeStyle.Render(fmt.Sprintf(" (%d)", count))
			}
		}
		if hasMod {
			name += modifiedMarkStyle.Render(" *")
		}

		prefix := node.Prefix
		if !(isSelected && isFocused) {
			if prefix != "" {
				prefix = treeBranchStyle.Render(prefix)
			}
			if iconStyle != nil {
				icon = iconStyle.Render(icon)
			}
		}

		line := indent + prefix + icon + " " + name

		if isSelected && isFocused {
			line = selectedRowStyle.Render(line)
		}
		line = lipgloss.NewStyle().MaxWidth(contentWidth).Render(line)
		out.rows = append(out.rows, panelRowTarget{y: rowY, height: 1, index: i})
		b.WriteString(line + "\n")
		rowY++
	}

	if total > panelH && panelH > 1 {
		pct := float64(m.treeOff+drawH) / float64(total) * 100
		indicator := scrollIndicatorStyle.Render(fmt.Sprintf("  %d/%d  %.0f%%", min(m.treeCursor+1, total), total, pct))
		b.WriteString(indicator)
	}

	out.content = strings.TrimSuffix(b.String(), "\n")
	return out
}

func (m *Model) optionLayout(contentH int) optionLayout {
	out := optionLayout{off: m.optOff}
	contentW := m.optionsContentWidth()
	title := m.text(textOptions)
	if m.selectedPkg != "" {
		title += " · " + m.packageLabel(m.selectedPkg)
	}
	titleLine := panelTitleStyle.Render(title)
	if m.selectedPkg != "" && !isSyntheticGroupPkg(m.selectedPkg) {
		var badges []string
		if !m.isActivePkg(m.selectedPkg) {
			badges = append(badges, inactivePkgStyle.Render("◌ "+m.text(textInactiveBadge)))
		}
		if mc := m.pkgModifiedCount(m.selectedPkg); mc > 0 {
			badges = append(badges, modifiedMarkStyle.Render(fmt.Sprintf("* %d", mc)))
		}
		if ec := m.pkgExplicitUnmodifiedCount(m.selectedPkg); ec > 0 {
			badges = append(badges, explicitMarkStyle.Render(m.textf(textExplicitBadge, ec)))
		}
		if dc := m.pkgDerivedCount(m.selectedPkg); dc > 0 {
			badges = append(badges, derivedFromStyle.Render(m.textf(textDerivedBadge, dc)))
		}
		var advBtn string
		if hiddenAdv := m.hiddenAdvancedCount(); hiddenAdv > 0 {
			advBtn = languageSelectorStyle.Render("[" + m.textf(textAdvancedHiddenBadge, hiddenAdv) + "]")
		} else if m.showAdvanced && m.hasAdvancedOptions() {
			advBtn = optionNameStyle.Bold(true).Render("[" + m.text(textAdvancedShownBadge) + "]")
		}
		if advBtn != "" {
			badges = append(badges, advBtn)
		}
		for len(badges) > 0 {
			rightStr := strings.Join(badges, "  ")
			if lipgloss.Width(titleLine)+2+lipgloss.Width(rightStr) <= contentW {
				titleLine += lipgloss.PlaceHorizontal(contentW-lipgloss.Width(titleLine), lipgloss.Right, rightStr)
				if advBtn != "" {
					advW := lipgloss.Width(advBtn)
					out.advToggle = dialogRect{x: contentW - advW, y: 0, width: advW, height: 1}
				}
				break
			}
			// Drop secondary badges before dropping the interactive advanced toggle.
			if advBtn != "" && len(badges) > 1 {
				badges = badges[1:]
				continue
			}
			break
		}
	}
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().MaxWidth(contentW).Render(titleLine) + "\n")

	if m.selectedPkg == "" {
		if cur := m.currentTreeNode(); cur != nil && cur.IsGroup {
			return m.groupSummaryLayout(cur, contentW)
		}
		b.WriteString(lipgloss.PlaceHorizontal(m.width-m.treeWidth-6, lipgloss.Center, m.text(textSelectPackage)))
		out.content = b.String()
		return out
	}

	if m.runningMenuconfig {
		b.WriteString("\n  " + m.textf(textRunningMenuconfig, m.packageLabel(m.selectedPkg)) + "\n")
		out.content = b.String()
		return out
	}

	visible := m.visibleOptions()
	hasKConfig := m.hasKConfig()

	if len(visible) == 0 && !hasKConfig && !m.hasAdvancedToggleRow() {
		b.WriteString(m.text(textNoOptions))
		out.content = b.String()
		return out
	}

	allRows := m.optionLines()

	cursorRowIdx := -1
	for ri, r := range allRows {
		if r.row.navIdx == m.optCursor && cursorRowIdx < 0 {
			cursorRowIdx = ri
		}
		if r.editCursor {
			cursorRowIdx = ri
			break
		}
	}

	panelH := m.optionsContentHeight(contentH) - 1
	if panelH < 1 {
		panelH = 1
	}
	total := len(allRows)
	drawH := panelH
	if total > panelH {
		drawH = max(1, panelH-1)
	}
	out.viewport = true
	out.off = optionScrollOffset(m.optOff, cursorRowIdx, total, drawH)

	start := out.off
	end := min(start+drawH, total)

	for i := start; i < end; i++ {
		line := allRows[i]
		out.rows = append(out.rows, panelRowTarget{y: i - start + 1, height: 1, index: line.row.navIdx})
		b.WriteString(line.text + "\n")
	}

	totalRows := len(allRows)
	if totalRows > panelH && panelH > 1 {
		pct := float64(out.off+drawH) / float64(totalRows) * 100
		indicator := scrollIndicatorStyle.Render(fmt.Sprintf("  %d/%d  %.0f%%", min(m.optCursor+1, m.totalOptRows()), m.totalOptRows(), pct))
		b.WriteString(indicator)
	}

	out.content = strings.TrimSuffix(b.String(), "\n")
	return out
}

func (m *Model) groupSummaryLayout(node *TreeNode, contentW int) optionLayout {
	var title string
	switch {
	case node.ContainerID != "":
		title = m.text(textModuleContainer) + " · " + m.treeNodeLabel(node)
	case node.PkgName == RemoteGroupPkgName || node.PkgName == InactiveGroupPkgName:
		title = m.text(textPackageGroup) + " · " + m.treeNodeLabel(node)
	default:
		title = m.text(textDirectory) + " · " + m.treeNodeLabel(node)
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().MaxWidth(contentW).Render(panelTitleStyle.Render(title)) + "\n\n")

	if node.ContainerID != "" {
		info := m.containers[node.ContainerID]
		kind := info.Kind
		if kind == "" {
			if node.ContainerID == "//" {
				kind = "local"
			} else {
				kind = "external"
			}
		}
		b.WriteString(fmt.Sprintf("  %s: %s\n", optionDescStyle.Render(m.text(textContainerID)), node.ContainerID))
		b.WriteString(fmt.Sprintf("  %s: %s\n", optionDescStyle.Render(m.text(textContainerKind)), kind))
		if info.LinkPath != "" {
			linkVal := info.LinkPath
			if info.Commit != "" {
				short := info.Commit
				if len(short) > 12 {
					short = short[:12]
				}
				linkVal += " (" + short + ")"
			}
			b.WriteString(fmt.Sprintf("  %s: %s\n", optionDescStyle.Render(m.text(textContainerLink)), linkVal))
		} else if info.Version != "" || info.Commit != "" {
			ver := info.Version
			if info.Commit != "" {
				short := info.Commit
				if len(short) > 12 {
					short = short[:12]
				}
				if ver != "" {
					ver += " (" + short + ")"
				} else {
					ver = short
				}
			}
			b.WriteString(fmt.Sprintf("  %s: %s\n", optionDescStyle.Render(m.text(textContainerVersion)), ver))
		}
	}

	var pkgs map[string]bool
	if node.ContainerID != "" {
		pkgs = make(map[string]bool)
		for _, p := range m.allKnownPackages() {
			if m.containerOf(p) == node.ContainerID {
				pkgs[p] = true
			}
		}
	} else {
		pkgs = make(map[string]bool)
		collectSubtreePackages(node, pkgs)
	}
	activeCount, configCount := 0, 0
	for p := range pkgs {
		if m.isActivePkg(p) {
			activeCount++
		}
		if m.hasPkgConfig(p) {
			configCount++
		}
	}
	b.WriteString(fmt.Sprintf("  %s: %s\n", optionDescStyle.Render(m.text(textGroupPackages)), m.textf(textGroupSummaryCount, activeCount, len(pkgs), configCount)))
	b.WriteString("\n  " + optionDescStyle.Render(m.text(textGroupHint)))

	return optionLayout{content: b.String()}
}

func (m *Model) gutterRoles() []gutterRole {
	roles := make([]gutterRole, len(m.flat))
	focal := m.selectedPkg
	if len(m.flat) == 0 || !isRealPackage(focal) {
		return roles
	}
	for i, node := range m.flat {
		if !node.IsGroup && node.PkgName == focal {
			roles[i] = gutterFocal
			continue
		}
		rowPkgs := rowRepresentedPackages(node)
		if len(rowPkgs) == 0 {
			continue
		}
		activeCaller, anyCaller := false, false
		activeDep, anyDep := false, false
		for rp := range rowPkgs {
			if rp == focal {
				continue
			}
			if m.isEdgeActive(rp, focal) {
				activeCaller = true
			} else if m.hasEagerEdge(rp, focal) {
				anyCaller = true
			}
			if m.isEdgeActive(focal, rp) {
				activeDep = true
			} else if m.hasEagerEdge(focal, rp) {
				anyDep = true
			}
		}
		switch {
		case activeDep:
			roles[i] = gutterUpstream
		case activeCaller:
			roles[i] = gutterDownstream
		case anyDep:
			roles[i] = gutterUpstreamInactive
		case anyCaller:
			roles[i] = gutterDownstreamInactive
		}
	}
	return roles
}

func gutterRoleStyle(role gutterRole) *lipgloss.Style {
	switch role {
	case gutterFocal:
		return &trackFocalStyle
	case gutterUpstream:
		return &trackUpstreamActiveStyle
	case gutterDownstream:
		return &trackDownstreamStyle
	case gutterUpstreamInactive:
		return &trackUpstreamInactiveStyle
	case gutterDownstreamInactive:
		return &trackDownstreamInactiveStyle
	default:
		return nil
	}
}

func treeRowIconAndStyle(node *TreeNode, role gutterRole, isSelected, isInactive, isActiveNoOpt bool) (string, *lipgloss.Style) {
	roleStyle := gutterRoleStyle(role)
	switch {
	case node.PkgName == GlobalPkgName:
		return "◈", &trackFocalStyle
	case len(node.Children) > 0:
		icon := "▸"
		if node.Expanded {
			icon = "▾"
		}
		if roleStyle != nil {
			return icon, roleStyle
		}
		if isInactive {
			return icon, &treeInactiveIconStyle
		}
		return icon, &treeGroupIconStyle
	case role == gutterFocal || isSelected:
		return "◆", &trackFocalStyle
	case role == gutterUpstream:
		return "◀", roleStyle
	case role == gutterDownstream:
		return "▶", roleStyle
	case role == gutterUpstreamInactive:
		return "◁", roleStyle
	case role == gutterDownstreamInactive:
		return "▷", roleStyle
	case isInactive:
		return "◌", &treeInactiveIconStyle
	case isActiveNoOpt:
		return "○", &treeActiveNoOptIconStyle
	case node.IsExternal:
		return "○", &treeActiveRemoteIconStyle
	default:
		return "●", &treeActiveLocalIconStyle
	}
}

func (m *Model) headerSelector() headerMetrics {
	langSelector := languageSelectorStyle.Render("[" + m.text(textLanguage) + ": " + m.languageLabel() + " ▼]")
	style := headerBorderStyle(true).Width(m.width - 4)
	headerW := style.GetWidth() - style.GetHorizontalFrameSize()
	var advToggle string
	if m.projectHasAdvancedOptions() {
		var btn string
		if m.showAdvanced {
			btn = optionNameStyle.Bold(true).Render("[● " + m.text(textAllOptions) + "]")
		} else {
			btn = languageSelectorStyle.Render("[○ " + m.text(textAllOptions) + "]")
		}
		if headerW >= lipgloss.Width(btn)+1+lipgloss.Width(langSelector)+18 {
			advToggle = btn
		}
	}
	selector := langSelector
	if advToggle != "" {
		selector = advToggle + " " + langSelector
	}
	leftW := max(1, headerW-lipgloss.Width(selector)-2)
	return headerMetrics{
		selector:     selector,
		advToggle:    advToggle,
		langSelector: langSelector,
		style:        style,
		headerW:      headerW,
		leftW:        leftW,
	}
}

func (m *Model) advancedToggleRect() dialogRect {
	hm := m.headerSelector()
	if hm.advToggle == "" {
		return dialogRect{}
	}
	return dialogRect{
		x:      hm.style.GetBorderLeftSize() + hm.style.GetPaddingLeft() + hm.leftW + 2,
		y:      hm.style.GetBorderTopSize() + hm.style.GetPaddingTop(),
		width:  lipgloss.Width(hm.advToggle),
		height: 1,
	}
}

func (m *Model) languageSelectorRect() dialogRect {
	hm := m.headerSelector()
	offset := 0
	if hm.advToggle != "" {
		offset = lipgloss.Width(hm.advToggle) + 1
	}
	return dialogRect{
		x:      hm.style.GetBorderLeftSize() + hm.style.GetPaddingLeft() + hm.leftW + 2 + offset,
		y:      hm.style.GetBorderTopSize() + hm.style.GetPaddingTop(),
		width:  lipgloss.Width(hm.langSelector),
		height: 1,
	}
}

func (s screen) optionRowAt(x, y int) (int, bool) {
	if x < s.optionsLeft || x >= s.optionsLeft+s.optionsW {
		return 0, false
	}
	for _, target := range s.optionRows {
		if y >= target.y && y < target.y+target.height {
			return target.index, target.index >= 0
		}
	}
	return 0, false
}

func (s screen) egoHitAt(mx, my int) (egoNodeHitbox, bool) {
	if s.graphH <= 0 {
		return egoNodeHitbox{}, false
	}
	if mx < s.graphLeft || mx >= s.graphLeft+s.optionsContentW {
		return egoNodeHitbox{}, false
	}
	if my < s.headerH || my >= s.headerH+s.graphH-1 {
		return egoNodeHitbox{}, false
	}
	rx := mx - s.graphLeft
	ry := my - s.headerH
	for _, b := range s.egoBoxes {
		if rx >= b.x && rx < b.x+b.w && ry >= b.y && ry < b.y+b.h {
			return b, true
		}
	}
	return egoNodeHitbox{}, false
}
