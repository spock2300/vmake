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
	descRowOffset   int
	optOff          int

	languageSelector dialogRect

	mainH int

	view string

	confirm     dialogLayout
	overlay     dialogLayout
	choiceOff   int
	detailRows  int
	detailTotal int
	detailOff   int
}

type treeLayout struct {
	content string
	rows    []panelRowTarget
}

type optionLayout struct {
	content       string
	rows          []panelRowTarget
	descRowOffset int
	off           int
	viewport      bool
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
	header, footer, headerH, footerH, contentH := m.chrome()
	s.headerH, s.footerH, s.contentH = headerH, footerH, contentH
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
	var rightCol string
	if egoInner := m.egoGraphInnerHeight(s.contentH); egoInner > 0 {
		graphStyle := egoGraphPanelStyle(m.focusArea == 0, s.optionsW)
		graphContent, boxes := m.egoGraphBody(s.contentH)
		graphPanel := graphStyle.MaxHeight(egoInner + 1).Render(graphContent)
		s.graphH = lipgloss.Height(graphPanel)
		s.egoBoxes = boxes
		s.graphLeft = s.treeW + graphStyle.GetBorderLeftSize() + graphStyle.GetPaddingLeft()
		s.optionsTopY = s.headerH + s.graphH
		opt := m.optionLayout(s.contentH)
		s.optionContent, s.optionRows = opt.content, opt.rows
		s.descRowOffset, s.optOff, s.optionViewport = opt.descRowOffset, opt.off, opt.viewport
		optPanel := optionStyle.MaxHeight(max(1, s.contentH-s.graphH)).Render(s.optionContent)
		rightCol = lipgloss.JoinVertical(lipgloss.Left, graphPanel, optPanel)
	} else {
		s.graphH = 0
		s.optionsTopY = s.headerH
		opt := m.optionLayout(s.contentH)
		s.optionContent, s.optionRows = opt.content, opt.rows
		s.descRowOffset, s.optOff, s.optionViewport = opt.descRowOffset, opt.off, opt.viewport
		rightCol = optionStyle.MaxHeight(s.contentH).Render(s.optionContent)
	}
	for i := range s.treeRows {
		s.treeRows[i].y += s.headerH
	}
	for i := range s.optionRows {
		s.optionRows[i].y += s.optionsTopY
	}
	main := lipgloss.JoinHorizontal(lipgloss.Top, treePanel, rightCol)
	s.mainH = lipgloss.Height(main)
	s.view = lipgloss.JoinVertical(lipgloss.Left, header, main, footer)
}

func (m *Model) fillOverlay(s *screen) {
	switch m.overlay {
	case overlayChoice, overlayLanguage:
		s.overlay, s.choiceOff = m.choiceDialogLayout()
	case overlayDetail:
		s.overlay, s.detailRows, s.detailTotal, s.detailOff = m.detailDialogLayout()
	}
}

func (m *Model) chrome() (header, footer string, headerH, footerH, contentH int) {
	header = m.renderHeaderWithin(0)
	footer = m.renderFooterWithin(0)
	treeStyle := treePanelStyle(m.focusArea == 0, m.treePanelWidth())
	optionStyle := optionsPanelStyle(m.focusArea == 1, m.optionsPanelWidth())
	minTreeHeight := 2 + treeStyle.GetVerticalFrameSize()
	if m.filterActive || m.filterInput != "" {
		minTreeHeight++
	}
	minMainHeight := max(minTreeHeight, 2+optionStyle.GetVerticalFrameSize())
	if lipgloss.Height(header)+lipgloss.Height(footer)+minMainHeight > m.height {
		minFooterHeight := 1 + footerBorderStyle().GetVerticalFrameSize()
		header = m.renderHeaderWithin(m.height - minMainHeight - minFooterHeight)
		footer = m.renderFooterWithin(m.height - minMainHeight - lipgloss.Height(header))
	}
	headerH = lipgloss.Height(header)
	footerH = lipgloss.Height(footer)
	contentH = max(1, m.height-headerH-footerH)
	return
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

		icon := "●"
		if node.PkgName == GlobalPkgName {
			icon = "◈"
		} else if len(node.Children) > 0 {
			if node.Expanded {
				icon = "▾"
			} else {
				icon = "▸"
			}
		} else if role == gutterFocal || isSelected {
			icon = "◆"
		} else if role == gutterUpstream {
			icon = "◀"
		} else if role == gutterDownstream {
			icon = "▶"
		} else if !m.isActivePkg(node.PkgName) || (!m.hasPkgConfig(node.PkgName) && !node.IsGroup) {
			icon = "◌"
		} else if node.IsExternal {
			icon = "○"
		}

		name := node.Name
		if node.PkgName == GlobalPkgName || isSyntheticGroupPkg(node.PkgName) {
			name = m.packageLabel(node.PkgName)
		}

		count := 0
		if isSyntheticGroupPkg(node.PkgName) {
			if m.focusActive {
				count = subtreePackageCount(node)
			}
		} else if node.PkgName != "" {
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
			if node.PkgName == InactiveGroupPkgName || (!node.IsGroup && !m.isActivePkg(node.PkgName)) {
				name = externalPkgStyle.Render(name)
			} else if node.IsExternal {
				name = externalPkgStyle.Render(name)
			}
		}

		if count > 0 {
			if node.PkgName == InactiveGroupPkgName {
				name += externalPkgStyle.Render(fmt.Sprintf(" (%d)", count))
			} else {
				name += countBadgeStyle.Render(fmt.Sprintf(" (%d)", count))
			}
		}
		if hasMod {
			name += modifiedMarkStyle.Render(" *")
		}

		if !(isSelected && isFocused) {
			switch role {
			case gutterFocal:
				icon = trackFocalStyle.Render(icon)
			case gutterUpstream:
				icon = trackUpstreamActiveStyle.Render(icon)
			case gutterDownstream:
				icon = trackDownstreamStyle.Render(icon)
			case gutterUpstreamInactive, gutterDownstreamInactive:
				icon = trackPassStyle.Render(icon)
			}
		}

		line := indent + node.Prefix + icon + " " + name

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
			badges = append(badges, externalPkgStyle.Render("◌ "+m.text(textInactiveBadge)))
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
		if len(badges) > 0 {
			rightStr := strings.Join(badges, "  ")
			if lipgloss.Width(titleLine)+2+lipgloss.Width(rightStr) <= contentW {
				titleLine += lipgloss.PlaceHorizontal(contentW-lipgloss.Width(titleLine), lipgloss.Right, rightStr)
			}
		}
	}
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().MaxWidth(contentW).Render(titleLine) + "\n")

	if m.optionsContentHeight(contentH) >= 3 {
		b.WriteString(m.renderDescriptionRow())
		out.descRowOffset = 1
	}

	if m.selectedPkg == "" {
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

	if len(visible) == 0 && !hasKConfig {
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

	panelH := m.optionsContentHeight(contentH) - 1 - out.descRowOffset
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
		out.rows = append(out.rows, panelRowTarget{y: i - start + 1 + out.descRowOffset, height: 1, index: line.row.navIdx})
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
		case activeCaller:
			roles[i] = gutterUpstream
		case activeDep:
			roles[i] = gutterDownstream
		case anyCaller:
			roles[i] = gutterUpstreamInactive
		case anyDep:
			roles[i] = gutterDownstreamInactive
		}
	}
	return roles
}

func (m *Model) headerSelector() (string, lipgloss.Style, int) {
	languageSelector := languageSelectorStyle.Render("[" + m.text(textLanguage) + ": " + m.languageLabel() + " ▼]")
	headerStyle := headerBorderStyle(true).Width(m.width - 4)
	headerWidth := headerStyle.GetWidth() - headerStyle.GetHorizontalFrameSize()
	leftWidth := max(1, headerWidth-lipgloss.Width(languageSelector)-2)
	return languageSelector, headerStyle, leftWidth
}

func (m *Model) languageSelectorRect() dialogRect {
	languageSelector, headerStyle, leftWidth := m.headerSelector()
	return dialogRect{
		x:      headerStyle.GetBorderLeftSize() + headerStyle.GetPaddingLeft() + leftWidth + 2,
		y:      headerStyle.GetBorderTopSize() + headerStyle.GetPaddingTop(),
		width:  lipgloss.Width(languageSelector),
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

func (s screen) descriptionRowAt(x, y int) bool {
	if s.descRowOffset == 0 {
		return false
	}
	if x < s.optionsLeft || x >= s.optionsLeft+s.optionsW {
		return false
	}
	return y == s.optionsTopY+1
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
