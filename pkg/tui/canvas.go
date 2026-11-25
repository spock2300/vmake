package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type egoNodeHitbox struct {
	pkgName    string
	x, y       int
	w, h       int
	isMore     bool
	isUpstream bool
}

type canvasCell struct {
	ch    rune
	style *lipgloss.Style
	wire  uint8
	wireP uint8
}

type egoCanvas struct {
	cells [][]canvasCell
	w, h  int
}

const (
	dirLeft uint8 = 1 << iota
	dirRight
	dirUp
	dirDown
)

var (
	egoWireUpstreamStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#00D9FF")).
				Bold(true)

	egoWireDownstreamStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#00D4AA")).
				Bold(true)

	egoWireInactiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#4E5166"))

	egoUpstreamBorderActiveStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#00D9FF")).
					Bold(true)

	egoUpstreamBorderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#00A8CC"))

	egoDownstreamBorderActiveStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#00D4AA")).
					Bold(true)

	egoDownstreamBorderStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#04B575"))

	egoInactiveCardStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#585B70"))

	egoActiveLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E8EAF6")).
				Bold(true)

	egoNormalLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#CDD6F4"))
)

func newEgoCanvas(w, h int) *egoCanvas {
	cells := make([][]canvasCell, h)
	for y := 0; y < h; y++ {
		cells[y] = make([]canvasCell, w)
		for x := 0; x < w; x++ {
			cells[y][x] = canvasCell{ch: ' '}
		}
	}
	return &egoCanvas{cells: cells, w: w, h: h}
}

func wireMaskRune(mask uint8) rune {
	switch mask {
	case dirLeft | dirRight, dirLeft, dirRight:
		return '─'
	case dirUp | dirDown, dirUp, dirDown:
		return '│'
	case dirDown | dirRight:
		return '╭'
	case dirDown | dirLeft:
		return '╮'
	case dirUp | dirRight:
		return '╰'
	case dirUp | dirLeft:
		return '╯'
	case dirUp | dirDown | dirRight:
		return '├'
	case dirUp | dirDown | dirLeft:
		return '┤'
	case dirLeft | dirRight | dirDown:
		return '┬'
	case dirLeft | dirRight | dirUp:
		return '┴'
	case dirLeft | dirRight | dirUp | dirDown:
		return '┼'
	default:
		return ' '
	}
}

func (m *Model) hasAnyDependencyEdges() bool {
	for _, list := range m.deps {
		if len(list) > 0 {
			return true
		}
	}
	for _, list := range m.preview.ActiveDeps {
		if len(list) > 0 {
			return true
		}
	}
	return false
}

func (m *Model) egoGraphInnerHeight(contentH int) int {
	if contentH < 14 || m.optionsContentWidth() < 46 || !m.hasAnyDependencyEdges() {
		return 0
	}
	switch {
	case contentH < 20:
		return 6
	case contentH < 28:
		return 8
	default:
		return 9
	}
}

func (m *Model) egoGraphOuterHeight(contentH int) int {
	inner := m.egoGraphInnerHeight(contentH)
	if inner <= 0 {
		return 0
	}
	return inner + 1
}

func visibleWindow(total, capacity, focusIdx int) (start, end int, hidden int) {
	if total <= capacity {
		return 0, total, 0
	}
	if capacity <= 1 {
		idx := clamp(focusIdx, 0, max(0, total-1))
		return idx, min(total, idx+1), max(0, total-1)
	}
	itemCap := capacity - 1
	start = 0
	if focusIdx >= itemCap {
		start = focusIdx - itemCap + 1
	}
	if start+itemCap > total {
		start = max(0, total-itemCap)
	}
	end = min(total, start+itemCap)
	hidden = total - (end - start)
	return start, end, hidden
}

func (m *Model) egoGraphBody(contentH int) (string, []egoNodeHitbox) {
	innerH := m.egoGraphInnerHeight(contentH)
	width := m.optionsContentWidth()
	if innerH <= 0 || width <= 0 {
		return "", nil
	}

	var boxes []egoNodeHitbox
	ego := m.egoGraphData()
	var out strings.Builder
	leftTitle := m.text(textDependencies)
	if !ego.IsGlobal && ego.FocalPkg != "" {
		leftTitle += " · " + m.packageLabel(ego.FocalPkg)
	}
	leftRendered := panelTitleStyle.Render(leftTitle)

	rightBadge := m.renderEgoSummaryBadge(ego)
	headerLine := leftRendered
	if rightBadge != "" {
		avail := width - lipgloss.Width(leftRendered) - 2
		if avail >= lipgloss.Width(rightBadge) {
			headerLine += lipgloss.PlaceHorizontal(width-lipgloss.Width(leftRendered), lipgloss.Right, rightBadge)
		} else {
			leftRendered = panelTitleStyle.Render(truncateDisplay(leftTitle, max(12, width-lipgloss.Width(rightBadge)-3)))
			if lipgloss.Width(leftRendered)+2+lipgloss.Width(rightBadge) <= width {
				headerLine = leftRendered + lipgloss.PlaceHorizontal(width-lipgloss.Width(leftRendered), lipgloss.Right, rightBadge)
			} else {
				headerLine = panelTitleStyle.Render(truncateDisplay(leftTitle, width))
			}
		}
	}
	out.WriteString(lipgloss.NewStyle().MaxWidth(width).Render(headerLine) + "\n")

	bodyH := innerH - 2
	if bodyH < 3 {
		bodyH = max(1, innerH-1)
	}

	gapW := 5
	if width < 62 {
		gapW = 3
	}
	usableW := max(18, width-2*gapW)
	col0W := usableW / 3
	col1W := usableW / 3
	col2W := usableW - col0W - col1W
	if col1W < col2W-1 {
		col1W++
		col2W--
	}

	if innerH >= 5 {
		h0 := truncateRunes(m.text(textUpstream), col0W)
		h1 := truncateRunes(m.text(textCurrentPkg), col1W)
		h2 := truncateRunes(m.text(textDownstream), col2W)
		subLine := padDisplayRight(egoSubheaderStyle.Render(h0), col0W+gapW) +
			padDisplayRight(egoSubheaderStyle.Render(h1), col1W+gapW) +
			egoSubheaderStyle.Render(h2)
		out.WriteString(lipgloss.NewStyle().MaxWidth(width).Render(subLine) + "\n")
	}

	bodyTopRow := 2
	if innerH < 5 {
		bodyTopRow = 1
	}

	cv := newEgoCanvas(width, bodyH)
	col0X := 0
	col1X := col0W + gapW
	col2X := col1X + col1W + gapW

	focalMidY := bodyH / 2
	if bodyH >= 3 {
		focalTopY := clamp(focalMidY-1, 0, bodyH-3)
		focalMidY = focalTopY + 1
		cv.stampFocalCard(col1X, focalTopY, col1W, ego)
	} else {
		cv.stampPillNode(col1X, focalMidY, col1W, "◆", ego.FocalLabel, ego.FocalOpts, ego.FocalMod, true, true, &canvasFocalHexStyle)
	}

	switch {
	case ego.IsGlobal:
		activeTotal := 0
		for _, p := range m.allKnownPackages() {
			if m.isActivePkg(p) {
				activeTotal++
			}
		}
		cv.stampCenteredNote(col0X, focalMidY, col0W, m.textf(textGlobalScope, activeTotal), &optionDescStyle)
	case ego.IsGroup || len(ego.Deps) == 0:
		cv.stampCenteredNote(col0X, focalMidY, col0W, m.text(textNoUpstream), &optionDescStyle)
	default:
		ports := m.stampSideColumn(cv, col0X, col0W, bodyTopRow, focalMidY, ego.Deps, ego.ActiveDep, true, &boxes)
		cv.drawChannelBus(col0X+col0W, gapW, ports, focalMidY, false, &egoWireUpstreamStyle, &egoWireInactiveStyle)
	}

	switch {
	case ego.IsGroup:
		cv.stampCenteredNote(col2X, focalMidY, col2W, m.text(textNoDownstream), &optionDescStyle)
	case len(ego.Callers) == 0:
		if ego.FocalIsRoot {
			cv.stampCenteredNote(col2X, focalMidY, col2W, "★ "+m.text(textRootEntry), &countBadgeStyle)
		} else {
			cv.stampCenteredNote(col2X, focalMidY, col2W, m.text(textNoDownstream), &optionDescStyle)
		}
	default:
		ports := m.stampSideColumn(cv, col2X, col2W, bodyTopRow, focalMidY, ego.Callers, ego.ActiveCaller, false, &boxes)
		cv.drawChannelBus(col1X+col1W, gapW, ports, focalMidY, true, &egoWireDownstreamStyle, &egoWireInactiveStyle)
	}

	cv.materializeWires()
	out.WriteString(cv.render())
	return out.String(), boxes
}

func (m *Model) renderEgoSummaryBadge(ego egoGraphView) string {
	if ego.IsGlobal {
		activeTotal := 0
		for _, p := range m.allKnownPackages() {
			if m.isActivePkg(p) {
				activeTotal++
			}
		}
		return optionDescStyle.Render(m.textf(textGlobalScope, activeTotal))
	}
	if ego.IsGroup {
		return ""
	}
	actUp, inactUp := 0, 0
	for _, d := range ego.Deps {
		if d.Active {
			actUp++
		} else {
			inactUp++
		}
	}
	actDown, inactDown := 0, 0
	for _, c := range ego.Callers {
		if c.Active {
			actDown++
		} else {
			inactDown++
		}
	}

	var parts []string
	if !ego.FocalActive {
		parts = append(parts, egoInactiveCardStyle.Render("◌ "+m.text(textInactiveBadge)))
	} else if ego.FocalIsRoot {
		parts = append(parts, countBadgeStyle.Render("★ Root"))
	}
	upText := fmt.Sprintf("↑%d", actUp)
	if inactUp > 0 {
		upText += fmt.Sprintf("(+%d)", inactUp)
	}
	downText := fmt.Sprintf("↓%d", actDown)
	if inactDown > 0 {
		downText += fmt.Sprintf("(+%d)", inactDown)
	}
	parts = append(parts, egoWireUpstreamStyle.Render(upText)+" "+egoWireDownstreamStyle.Render(downText))
	return strings.Join(parts, helpSepStyle.Render(" │ "))
}

type busPort struct {
	y      int
	active bool
}

func (m *Model) stampSideColumn(
	cv *egoCanvas,
	colX, colW int,
	bodyTopRow int,
	focalMidY int,
	items []egoNeighbor,
	activeIdx int,
	upstream bool,
	boxes *[]egoNodeHitbox,
) []busPort {
	if len(items) == 1 && cv.h >= 3 {
		topY := clamp(focalMidY-1, 0, cv.h-3)
		midY := topY + 1
		it := items[0]
		cv.stampRoundedSideCard(colX, topY, colW, it, activeIdx == 0, upstream)
		*boxes = append(*boxes, egoNodeHitbox{
			pkgName:    it.PkgName,
			x:          colX,
			y:          bodyTopRow + topY,
			w:          colW,
			h:          3,
			isUpstream: upstream,
		})
		return []busPort{{y: midY, active: it.Active}}
	}
	if len(items) == 2 && cv.h >= 7 {
		topYs := [2]int{max(0, focalMidY-3), min(cv.h-3, focalMidY+1)}
		if topYs[1] < topYs[0]+4 {
			topYs = [2]int{0, 4}
		}
		var ports []busPort
		for i, it := range items {
			ty := topYs[i]
			cv.stampRoundedSideCard(colX, ty, colW, it, activeIdx == i, upstream)
			*boxes = append(*boxes, egoNodeHitbox{
				pkgName:    it.PkgName,
				x:          colX,
				y:          bodyTopRow + ty,
				w:          colW,
				h:          3,
				isUpstream: upstream,
			})
			ports = append(ports, busPort{y: ty + 1, active: it.Active})
		}
		return ports
	}

	start, end, hidden := visibleWindow(len(items), cv.h, activeIdx)
	shown := end - start
	totalRows := shown
	if hidden > 0 {
		totalRows++
	}
	startY := clamp(focalMidY-totalRows/2, 0, max(0, cv.h-totalRows))

	var ports []busPort
	for idx := start; idx < end; idx++ {
		y := startY + (idx - start)
		if y >= cv.h {
			break
		}
		it := items[idx]
		icon, bdrStyle := sideNeighborGlyph(it.Active, idx == activeIdx, upstream)
		cv.stampPillNode(colX, y, colW, icon, it.Label, it.OptCount, it.Modified, it.Active, idx == activeIdx, bdrStyle)
		*boxes = append(*boxes, egoNodeHitbox{
			pkgName:    it.PkgName,
			x:          colX,
			y:          bodyTopRow + y,
			w:          colW,
			h:          1,
			isUpstream: upstream,
		})
		ports = append(ports, busPort{y: y, active: it.Active})
	}

	if hidden > 0 {
		moreY := startY + shown
		if moreY < cv.h {
			cv.stampCenteredNote(colX, moreY, colW, m.textf(textMoreItems, hidden), &scrollIndicatorStyle)
			*boxes = append(*boxes, egoNodeHitbox{
				x:          colX,
				y:          bodyTopRow + moreY,
				w:          colW,
				h:          1,
				isMore:     true,
				isUpstream: upstream,
			})
		}
	}
	return ports
}

func sideNeighborGlyph(active, highlighted, upstream bool) (string, *lipgloss.Style) {
	if !active {
		return "◌", &egoInactiveCardStyle
	}
	if upstream {
		if highlighted {
			return "▶", &egoUpstreamBorderActiveStyle
		}
		return "●", &egoUpstreamBorderStyle
	}
	if highlighted {
		return "◀", &egoDownstreamBorderActiveStyle
	}
	return "●", &egoDownstreamBorderStyle
}

func (cv *egoCanvas) stampFocalCard(x0, y0, w int, ego egoGraphView) {
	if w < 6 || y0 < 0 || y0+2 >= cv.h {
		return
	}
	bdr := &canvasFocalHexStyle
	if !ego.FocalActive {
		bdr = &egoInactiveCardStyle
	}
	cv.put(x0, y0, '╔', bdr)
	cv.put(x0+w-1, y0, '╗', bdr)
	cv.put(x0, y0+2, '╚', bdr)
	cv.put(x0+w-1, y0+2, '╝', bdr)
	for dx := 1; dx < w-1; dx++ {
		cv.put(x0+dx, y0, '═', bdr)
		cv.put(x0+dx, y0+2, '═', bdr)
		cv.put(x0+dx, y0+1, ' ', nil)
	}
	leftMid := '║'
	rightMid := '║'
	if len(ego.Deps) > 0 {
		leftMid = '╟'
	}
	if len(ego.Callers) > 0 {
		rightMid = '╢'
	}
	cv.put(x0, y0+1, leftMid, bdr)
	cv.put(x0+w-1, y0+1, rightMid, bdr)
	cv.stampCardInnerContent(x0+2, y0+1, w-4, "◆", ego.FocalLabel, ego.FocalOpts, ego.FocalMod, ego.FocalActive, true, bdr)
}

func (cv *egoCanvas) stampRoundedSideCard(x0, y0, w int, it egoNeighbor, highlighted, upstream bool) {
	if w < 6 || y0 < 0 || y0+2 >= cv.h {
		return
	}
	icon, bdr := sideNeighborGlyph(it.Active, highlighted, upstream)
	cv.put(x0, y0, '╭', bdr)
	cv.put(x0+w-1, y0, '╮', bdr)
	cv.put(x0, y0+2, '╰', bdr)
	cv.put(x0+w-1, y0+2, '╯', bdr)
	for dx := 1; dx < w-1; dx++ {
		cv.put(x0+dx, y0, '─', bdr)
		cv.put(x0+dx, y0+2, '─', bdr)
		cv.put(x0+dx, y0+1, ' ', nil)
	}
	leftMid, rightMid := '│', '│'
	if upstream {
		rightMid = '├'
	} else {
		leftMid = '┤'
	}
	cv.put(x0, y0+1, leftMid, bdr)
	cv.put(x0+w-1, y0+1, rightMid, bdr)
	cv.stampCardInnerContent(x0+2, y0+1, w-4, icon, it.Label, it.OptCount, it.Modified, it.Active, highlighted, bdr)
}

func (cv *egoCanvas) stampPillNode(x0, y0, w int, icon, label string, optCount int, modified, active, highlighted bool, bdr *lipgloss.Style) {
	if w < 6 || y0 < 0 || y0 >= cv.h {
		return
	}
	for dx := 0; dx < w; dx++ {
		cv.put(x0+dx, y0, ' ', nil)
	}
	cv.put(x0, y0, '[', bdr)
	cv.put(x0+w-1, y0, ']', bdr)
	cv.stampCardInnerContent(x0+2, y0, w-4, icon, label, optCount, modified, active, highlighted, bdr)
}

func (cv *egoCanvas) stampCardInnerContent(xStart, y, innerW int, icon, label string, optCount int, modified, active, highlighted bool, iconStyle *lipgloss.Style) {
	if innerW <= 0 || y < 0 || y >= cv.h {
		return
	}
	badge := ""
	if optCount > 0 {
		badge = fmt.Sprintf(" (%d)", optCount)
	}
	modSuffix := ""
	if modified {
		modSuffix = " *"
	}
	suffixCols := lipgloss.Width(badge) + lipgloss.Width(modSuffix)
	iconCols := lipgloss.Width(icon) + 1
	availName := max(3, innerW-iconCols-suffixCols)
	nameDisp := truncateDisplay(label, availName)

	cx := xStart
	for _, r := range icon {
		cv.putWide(&cx, y, r, iconStyle)
	}
	cv.putWide(&cx, y, ' ', nil)

	nameStyle := &egoNormalLabelStyle
	if !active {
		nameStyle = &egoInactiveCardStyle
	} else if highlighted {
		nameStyle = &egoActiveLabelStyle
	}
	for _, r := range nameDisp {
		if cx >= xStart+innerW-suffixCols {
			break
		}
		cv.putWide(&cx, y, r, nameStyle)
	}
	if badge != "" {
		bStyle := &countBadgeStyle
		if !active {
			bStyle = &egoInactiveCardStyle
		}
		for _, r := range badge {
			if cx >= xStart+innerW {
				break
			}
			cv.putWide(&cx, y, r, bStyle)
		}
	}
	if modSuffix != "" {
		for _, r := range modSuffix {
			if cx >= xStart+innerW {
				break
			}
			cv.putWide(&cx, y, r, &modifiedMarkStyle)
		}
	}
}

func (cv *egoCanvas) stampCenteredNote(colX, y, colW int, text string, style *lipgloss.Style) {
	if y < 0 || y >= cv.h || colW <= 0 {
		return
	}
	disp := truncateDisplay(text, colW)
	tw := lipgloss.Width(disp)
	cx := colX + max(0, (colW-tw)/2)
	for _, r := range disp {
		if cx >= colX+colW {
			break
		}
		cv.putWide(&cx, y, r, style)
	}
}

func (cv *egoCanvas) drawChannelBus(gapX, gapW int, ports []busPort, focalY int, arrowAtFocal bool, activeStyle, inactiveStyle *lipgloss.Style) {
	if len(ports) == 0 || gapW < 2 {
		return
	}
	trunkX := gapX + gapW/2
	farX := gapX + gapW - 1

	for pass := 0; pass < 2; pass++ {
		wantActive := pass == 1
		prio := uint8(1)
		st := inactiveStyle
		if wantActive {
			prio = 2
			st = activeStyle
		}

		for _, p := range ports {
			if p.active != wantActive {
				continue
			}
			nearY, farY := p.y, focalY
			if arrowAtFocal {
				nearY, farY = focalY, p.y
			}
			cv.putArrow(gapX, nearY, '◄', prio, st)
			if nearY == farY {
				for x := gapX + 1; x <= farX; x++ {
					cv.addWire(x, nearY, dirLeft|dirRight, prio, st)
				}
				continue
			}
			for x := gapX + 1; x < trunkX; x++ {
				cv.addWire(x, nearY, dirLeft|dirRight, prio, st)
			}
			if nearY < farY {
				cv.addWire(trunkX, nearY, dirLeft|dirDown, prio, st)
				for y := nearY + 1; y < farY; y++ {
					cv.addWire(trunkX, y, dirUp|dirDown, prio, st)
				}
				cv.addWire(trunkX, farY, dirUp|dirRight, prio, st)
			} else {
				cv.addWire(trunkX, nearY, dirLeft|dirUp, prio, st)
				for y := farY + 1; y < nearY; y++ {
					cv.addWire(trunkX, y, dirUp|dirDown, prio, st)
				}
				cv.addWire(trunkX, farY, dirDown|dirRight, prio, st)
			}
			for x := trunkX + 1; x <= farX; x++ {
				cv.addWire(x, farY, dirLeft|dirRight, prio, st)
			}
		}
	}
}

func (cv *egoCanvas) addWire(x, y int, mask, prio uint8, style *lipgloss.Style) {
	if x < 0 || x >= cv.w || y < 0 || y >= cv.h {
		return
	}
	c := &cv.cells[y][x]
	if c.ch != ' ' {
		return
	}
	c.wire |= mask
	if prio >= c.wireP {
		c.wireP = prio
		c.style = style
	}
}

func (cv *egoCanvas) putArrow(x, y int, ch rune, prio uint8, style *lipgloss.Style) {
	if x < 0 || x >= cv.w || y < 0 || y >= cv.h {
		return
	}
	c := &cv.cells[y][x]
	if prio >= c.wireP {
		c.ch = ch
		c.wire = 0
		c.wireP = prio
		c.style = style
	}
}

func (cv *egoCanvas) put(x, y int, ch rune, style *lipgloss.Style) {
	if x < 0 || x >= cv.w || y < 0 || y >= cv.h {
		return
	}
	cv.cells[y][x] = canvasCell{ch: ch, style: style}
}

func (cv *egoCanvas) putWide(x *int, y int, ch rune, style *lipgloss.Style) {
	rw := max(1, lipgloss.Width(string(ch)))
	if *x < 0 || *x+rw > cv.w || y < 0 || y >= cv.h {
		*x += rw
		return
	}
	cv.cells[y][*x] = canvasCell{ch: ch, style: style}
	for k := 1; k < rw; k++ {
		cv.cells[y][*x+k] = canvasCell{ch: 0, style: style}
	}
	*x += rw
}

func (cv *egoCanvas) materializeWires() {
	for y := 0; y < cv.h; y++ {
		for x := 0; x < cv.w; x++ {
			c := &cv.cells[y][x]
			if c.wire != 0 && c.ch == ' ' {
				c.ch = wireMaskRune(c.wire)
			}
		}
	}
}

func (cv *egoCanvas) render() string {
	var out strings.Builder
	for y := 0; y < cv.h; y++ {
		var rowBuf strings.Builder
		var run strings.Builder
		var curStyle *lipgloss.Style
		flush := func() {
			if run.Len() == 0 {
				return
			}
			s := run.String()
			if curStyle != nil {
				rowBuf.WriteString(curStyle.Render(s))
			} else {
				rowBuf.WriteString(s)
			}
			run.Reset()
		}
		for x := 0; x < cv.w; {
			cell := cv.cells[y][x]
			ch := cell.ch
			if ch == 0 {
				x++
				continue
			}
			if cell.style != curStyle {
				flush()
				curStyle = cell.style
			}
			run.WriteRune(ch)
			rw := max(1, lipgloss.Width(string(ch)))
			x += rw
		}
		flush()
		out.WriteString(rowBuf.String())
		if y < cv.h-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func padDisplayRight(s string, targetCols int) string {
	w := lipgloss.Width(s)
	if w >= targetCols {
		return s
	}
	return s + strings.Repeat(" ", targetCols-w)
}
