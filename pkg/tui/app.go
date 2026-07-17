package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

type ConfigResult struct {
	Saved         bool
	Values        map[string]map[string]any
	Toolchain     string
	GlobalValues  map[string]any
	MenuconfigRan map[string]bool
	PresetValues  map[string]string
}

func Run(
	packages []buildscript.Source,
	deps map[string][]string,
	options map[string]map[string]*api.Option,
	values map[string]map[string]any,
	workDir string,
	currentToolchain string,
	globalOptions map[string]*api.Option,
	globalValues map[string]any,
	kconfigs map[string][]*api.KConfigEntry,
) (*ConfigResult, error) {
	m := NewModel(packages, deps, options, values, workDir, currentToolchain, globalOptions, globalValues, kconfigs)
	p := tea.NewProgram(&m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return nil, err
	}
	return &ConfigResult{
		Saved:         m.saved,
		Values:        m.values,
		Toolchain:     getToolchainValue(m.globalValues),
		GlobalValues:  m.globalValues,
		MenuconfigRan: m.menuconfigRan,
		PresetValues:  m.presetValues,
	}, nil
}

func getToolchainValue(globalValues map[string]any) string {
	if tc, ok := globalValues["toolchain"].(string); ok {
		return tc
	}
	return ""
}

type menuconfigDone struct {
	pkgName string
	err     error
	ensured bool
}

func ensureConfigCmd(pkgName string, entries []*api.KConfigEntry, workDir string) tea.Cmd {
	return func() tea.Msg {
		if len(entries) == 0 {
			return menuconfigDone{pkgName: pkgName}
		}
		e := entries[0]
		srcDir := e.SrcDir()
		if srcDir == "" {
			srcDir = workDir
		}
		configPath := filepath.Join(srcDir, e.ConfigPath())
		if _, err := os.Stat(configPath); err == nil {
			return menuconfigDone{pkgName: pkgName, ensured: true}
		}
		presetName := e.SelectedPreset()
		if presetName == "" {
			presetName = e.DefaultPreset()
		}
		if presetName == "" {
			return menuconfigDone{pkgName: pkgName, ensured: true}
		}
		makeCmd := e.MenuconfigCmd()
		if makeCmd == "" {
			makeCmd = "make"
		}
		parts := strings.Fields(makeCmd)
		args := []string{"-C", srcDir}
		args = append(args, parts[1:]...)
		args = append(args, presetName)
		cmd := exec.Command(parts[0], args...)
		err := cmd.Run()
		if err == nil {
			api.ApplyKConfigPatches(configPath, e.Patches())
		}
		return menuconfigDone{pkgName: pkgName, ensured: true, err: err}
	}
}

func runMenuconfigCmd(pkgName string, entries []*api.KConfigEntry, workDir string) tea.Cmd {
	if len(entries) == 0 {
		return func() tea.Msg { return menuconfigDone{pkgName: pkgName} }
	}
	e := entries[0]
	srcDir := e.SrcDir()
	if srcDir == "" {
		srcDir = workDir
	}
	menuconfigCmd := e.MenuconfigCmd()
	if menuconfigCmd == "" {
		menuconfigCmd = "make menuconfig"
	}
	parts := strings.Fields(menuconfigCmd)
	args := []string{"-C", srcDir}
	args = append(args, parts[1:]...)
	cmd := exec.Command(parts[0], args...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return menuconfigDone{pkgName: pkgName, err: err}
	})
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case menuconfigDone:
		if msg.ensured {
			if msg.err != nil {
				m.runningMenuconfig = false
				m.hasChanges = true
				return m, nil
			}
			entries := m.kconfigs[msg.pkgName]
			return m, runMenuconfigCmd(msg.pkgName, entries, m.workDir)
		}
		m.runningMenuconfig = false
		if msg.err != nil {
			m.hasChanges = true
		} else {
			m.menuconfigRan[msg.pkgName] = true
			m.hasChanges = true
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ensureTreeCursorVisible()
		if m.optOff < 0 {
			m.optOff = 0
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmQuit {
		return m.handleConfirmKey(msg)
	}

	if m.overlay != overlayNone {
		return m.handleOverlayKey(msg)
	}

	if m.editing {
		return m.handleEditKey(msg)
	}

	if m.runningMenuconfig {
		return m, nil
	}

	if m.filterActive {
		return m.handleFilterKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		if m.hasChanges {
			m.confirmQuit = true
			m.confirmBtn = 0
			return m, nil
		}
		m.saved = false
		return m, tea.Quit
	case "ctrl+s":
		m.saved = true
		return m, tea.Quit
	case "/":
		m.filterActive = true
		m.rebuildFlat()
		return m, nil
	case "esc":
		if m.filterInput != "" && !m.filterActive {
			m.filterInput = ""
			m.rebuildFlat()
			if len(m.flat) > 0 {
				if m.treeCursor >= len(m.flat) {
					m.treeCursor = max(0, len(m.flat)-1)
				}
				m.selectCurrentNode()
			}
			m.ensureTreeCursorVisible()
			return m, nil
		}
		if m.focusArea == 0 {
			if m.hasChanges {
				m.confirmQuit = true
				m.confirmBtn = 0
				return m, nil
			}
			m.saved = false
			return m, tea.Quit
		}
		m.focusArea = 0
		return m, nil
	case "tab":
		m.focusArea = (m.focusArea + 1) % 2
		return m, nil
	case "shift+tab":
		m.focusArea = (m.focusArea - 1 + 2) % 2
		return m, nil
	}

	if m.focusArea == 0 {
		return m.handleTreeKey(msg)
	}
	return m.handleOptionsKey(msg)
}

func (m *Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filterActive = false
		m.filterInput = ""
		m.rebuildFlat()
		if len(m.flat) > 0 {
			m.selectFirstPkg()
		}
		return m, nil
	case "enter":
		query := m.filterInput
		if m.filteredMatchCount() <= 1 {
			m.filterActive = false
			m.filterInput = ""
			m.rebuildFlat()
			if query != "" {
				pkg, opt := m.findFirstMatch(query)
				m.jumpToMatch(pkg, opt)
			} else if len(m.flat) > 0 {
				m.selectFirstPkg()
			}
		} else {
			m.filterActive = false
			m.focusArea = 0
			if len(m.flat) > 0 {
				m.treeCursor = 0
				m.selectCurrentNode()
				m.ensureTreeCursorVisible()
			}
		}
		return m, nil
	case "backspace":
		if len(m.filterInput) > 0 {
			_, sz := utf8.DecodeLastRuneInString(m.filterInput)
			m.filterInput = m.filterInput[:len(m.filterInput)-sz]
			m.rebuildFlat()
		}
		return m, nil
	case "ctrl+u":
		m.filterInput = ""
		m.rebuildFlat()
		return m, nil
	}

	if msg.Type == tea.KeyRunes {
		m.filterInput += string(msg.Runes)
		m.rebuildFlat()
	}
	return m, nil
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.confirmQuit || m.overlay != overlayNone || m.editing || m.filterActive || m.runningMenuconfig {
		return m, nil
	}

	mx, my := msg.X, msg.Y
	headerH := m.headerHeight()
	footerH := m.footerHeight()
	if my < headerH || my >= m.height-footerH {
		return m, nil
	}
	contentY := my - headerH
	if contentY < 0 {
		return m, nil
	}

	inTree := mx < m.treeWidth

	switch {
	case msg.Button == tea.MouseButtonLeft && msg.Type == tea.MouseLeft:
		if inTree {
			row := contentY
			if m.filterActive || m.filterInput != "" {
				row--
			}
			if row < 0 {
				return m, nil
			}
			idx := row + m.treeOff
			if idx >= 0 && idx < len(m.flat) {
				m.focusArea = 0
				m.treeCursor = idx
				m.selectCurrentNode()
				if len(m.flat[idx].Children) > 0 {
					m.flat[idx].Expanded = !m.flat[idx].Expanded
					m.rebuildFlat()
				}
				m.ensureTreeCursorVisible()
			}
		} else {
			m.focusArea = 1
			visible := m.visibleOptions()
			row := contentY
			idx := row + m.optOff
			maxIdx := m.totalOptRows() - 1
			if idx < 0 {
				idx = 0
			}
			if idx > maxIdx {
				idx = maxIdx
			}
			if len(visible) > 0 {
				m.optCursor = idx
			}
		}
	case msg.Type == tea.MouseWheelUp:
		if inTree {
			if m.treeOff > 0 {
				m.treeOff--
			}
		} else {
			if m.optOff > 0 {
				m.optOff--
			}
		}
	case msg.Type == tea.MouseWheelDown:
		if inTree {
			drawH := m.treeItemRows()
			total := len(m.flat)
			maxOff := total - drawH
			if maxOff < 0 {
				maxOff = 0
			}
			if m.treeOff < maxOff {
				m.treeOff++
			}
		} else {
			drawH := m.optItemRows()
			total := m.totalOptRows()
			maxOff := total - drawH
			if maxOff < 0 {
				maxOff = 0
			}
			if m.optOff < maxOff {
				m.optOff++
			}
		}
	}
	return m, nil
}

func (m *Model) headerHeight() int {
	if m.renderedHeaderH > 0 {
		return m.renderedHeaderH
	}
	return 2
}

func (m *Model) footerHeight() int {
	if m.renderedFooterH > 0 {
		return m.renderedFooterH
	}
	return 2
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.saved = true
		return m, tea.Quit
	case "n", "N":
		m.saved = false
		return m, tea.Quit
	case "tab", "right", "l":
		m.confirmBtn = (m.confirmBtn + 1) % 2
		return m, nil
	case "shift+tab", "left", "h":
		m.confirmBtn = (m.confirmBtn - 1 + 2) % 2
		return m, nil
	case "enter":
		if m.confirmBtn == 0 {
			m.saved = true
		} else {
			m.saved = false
		}
		return m, tea.Quit
	case "esc":
		m.confirmQuit = false
		return m, nil
	}
	return m, nil
}

func (m *Model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closeOverlay()
		return m, nil
	case "?", "d":
		if m.overlay == overlayDetail {
			m.closeOverlay()
			return m, nil
		}
	case "up", "k":
		if m.overlay == overlayChoice && m.choiceCursor > 0 {
			m.choiceCursor--
		}
	case "down", "j":
		if m.overlay == overlayChoice && m.choiceCursor < len(m.choiceValues)-1 {
			m.choiceCursor++
		}
	case "home", "g":
		if m.overlay == overlayChoice {
			m.choiceCursor = 0
		}
	case "end", "G":
		if m.overlay == overlayChoice && len(m.choiceValues) > 0 {
			m.choiceCursor = len(m.choiceValues) - 1
		}
	case "enter":
		if m.overlay == overlayChoice && m.choiceCursor < len(m.choiceValues) {
			m.setValue(m.choiceOpt, m.choiceValues[m.choiceCursor])
		}
		m.closeOverlay()
	}
	return m, nil
}

func (m *Model) handleTreeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	browsing := m.filterInput != "" && !m.filterActive

	if browsing && msg.String() == "enter" {
		pkg := ""
		if m.treeCursor < len(m.flat) {
			pkg = m.flat[m.treeCursor].PkgName
		}
		query := m.filterInput
		m.filterInput = ""
		m.rebuildFlat()
		if pkg == "" {
			return m, nil
		}
		m.jumpToMatch(pkg, m.matchedOptionIn(pkg, query))
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.treeCursor > 0 {
			m.treeCursor--
			m.selectCurrentNode()
			m.ensureTreeCursorVisible()
		}
	case "down", "j":
		if m.treeCursor < len(m.flat)-1 {
			m.treeCursor++
			m.selectCurrentNode()
			m.ensureTreeCursorVisible()
		}
	case "left", "h":
		if m.treeCursor < len(m.flat) && m.flat[m.treeCursor].Expanded {
			m.flat[m.treeCursor].Expanded = false
			m.rebuildFlat()
			m.ensureTreeCursorVisible()
		}
	case "right", "l":
		if m.treeCursor < len(m.flat) && len(m.flat[m.treeCursor].Children) > 0 {
			m.flat[m.treeCursor].Expanded = true
			m.rebuildFlat()
			m.ensureTreeCursorVisible()
		}
	case "enter":
		if m.treeCursor < len(m.flat) && len(m.flat[m.treeCursor].Children) > 0 {
			m.flat[m.treeCursor].Expanded = !m.flat[m.treeCursor].Expanded
			m.rebuildFlat()
			m.ensureTreeCursorVisible()
		}
	case "z":
		m.collapseAll()
	case "Z":
		m.expandAll()
	case "H":
		m.hideEmptyPkgs = !m.hideEmptyPkgs
		m.rebuildFlat()
		if m.treeCursor >= len(m.flat) {
			m.treeCursor = max(0, len(m.flat)-1)
		}
		m.selectCurrentNode()
		m.ensureTreeCursorVisible()
	}
	return m, nil
}

func (m *Model) selectCurrentNode() {
	if m.treeCursor < len(m.flat) && m.flat[m.treeCursor].PkgName != "" {
		m.selectedPkg = m.flat[m.treeCursor].PkgName
		m.buildOptionItems()
		m.optCursor = 0
		m.optOff = 0
	}
}

func (m *Model) handleOptionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleOptions()
	presetIdx := len(visible)

	menuconfigIdx := -1
	if m.hasKConfig() {
		menuconfigIdx = presetIdx
		if m.hasPresets() {
			menuconfigIdx = presetIdx + 1
		}
	}

	if menuconfigIdx >= 0 && m.optCursor == menuconfigIdx && msg.String() == "enter" {
		entries := m.kconfigs[m.selectedPkg]
		m.runningMenuconfig = true
		m.saved = false
		return m, ensureConfigCmd(m.selectedPkg, entries, m.workDir)
	}

	if msg.String() == "enter" && m.optCursor < len(visible) {
		item := visible[m.optCursor]
		if item.Opt.Type() == api.OptionChoice && len(item.Opt.Values()) > 1 {
			m.openChoiceOverlay(item.Name, item.Opt.Values())
			return m, nil
		}
	}

	switch msg.String() {
	case "up", "k":
		if m.optCursor > 0 {
			m.optCursor--
		}
	case "down", "j":
		maxCursor := m.totalOptRows() - 1
		if m.optCursor < maxCursor {
			m.optCursor++
		}
	case "r":
		if m.optCursor < len(visible) {
			m.resetOption(visible[m.optCursor].Name)
		}
	case "R":
		if m.optCursor < len(visible) {
			m.resetOptionToDefault(visible[m.optCursor].Name)
		}
	case "?", "d":
		if m.optCursor < len(visible) {
			m.openDetailOverlay(visible[m.optCursor])
		}
	case " ", "enter", "right", "l":
		m.handleOptionAction(visible, presetIdx, false, msg.String())
	case "left", "h":
		m.handleOptionAction(visible, presetIdx, true, msg.String())
	}
	return m, nil
}

func (m *Model) handleOptionAction(visible []OptionItem, presetIdx int, reverse bool, key string) {
	if m.optCursor >= presetIdx && m.hasPresets() && m.optCursor == presetIdx {
		presets := m.presetOptions()
		if len(presets) > 1 {
			current := m.currentPreset()
			idx := 0
			for i, p := range presets {
				if p == current {
					idx = i
					break
				}
			}
			if reverse {
				idx--
				if idx < 0 {
					idx = len(presets) - 1
				}
			} else {
				idx = (idx + 1) % len(presets)
			}
			m.selectPreset(presets[idx])
		}
		return
	}

	if m.optCursor >= len(visible) {
		return
	}

	item := visible[m.optCursor]
	switch item.Opt.Type() {
	case api.OptionBool:
		if key == "enter" || key == " " || key == "right" || key == "l" || key == "left" || key == "h" {
			current := m.getValue(item.Name)
			if b, ok := current.(bool); ok {
				m.setValue(item.Name, !b)
			} else {
				m.setValue(item.Name, true)
			}
		}
	case api.OptionChoice:
		choices := item.Opt.Values()
		if len(choices) < 2 {
			return
		}
		current := fmt.Sprintf("%v", m.getValue(item.Name))
		idx := 0
		for i, v := range choices {
			if v == current {
				idx = i
				break
			}
		}
		if reverse {
			idx--
			if idx < 0 {
				idx = len(choices) - 1
			}
		} else {
			idx = (idx + 1) % len(choices)
		}
		m.setValue(item.Name, choices[idx])
	case api.OptionString, api.OptionInt:
		if !reverse {
			m.editing = true
			m.editInput = fmt.Sprintf("%v", m.getValue(item.Name))
			m.editCursor = len(m.editInput)
		}
	}
}

func (m *Model) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visibleOptions()
	if m.optCursor >= len(visible) {
		m.editing = false
		return m, nil
	}

	item := visible[m.optCursor]
	s := msg.String()

	switch s {
	case "esc":
		m.editing = false
		return m, nil
	case "enter":
		m.editing = false
		switch item.Opt.Type() {
		case api.OptionString:
			m.setValue(item.Name, m.editInput)
		case api.OptionInt:
			val, ok := parseIntInput(m.editInput)
			if !ok {
				return m, nil
			}
			m.setValue(item.Name, val)
		}
		return m, nil
	case "left":
		if m.editCursor > 0 {
			_, sz := utf8.DecodeLastRuneInString(m.editInput[:m.editCursor])
			m.editCursor -= sz
		}
		return m, nil
	case "right":
		if m.editCursor < len(m.editInput) {
			_, sz := utf8.DecodeRuneInString(m.editInput[m.editCursor:])
			m.editCursor += sz
		}
		return m, nil
	case "home", "ctrl+a":
		m.editCursor = 0
		return m, nil
	case "end", "ctrl+e":
		m.editCursor = len(m.editInput)
		return m, nil
	case "backspace":
		if m.editCursor > 0 {
			_, sz := utf8.DecodeLastRuneInString(m.editInput[:m.editCursor])
			m.editInput = m.editInput[:m.editCursor-sz] + m.editInput[m.editCursor:]
			m.editCursor -= sz
		}
		return m, nil
	case "delete":
		if m.editCursor < len(m.editInput) {
			_, sz := utf8.DecodeRuneInString(m.editInput[m.editCursor:])
			m.editInput = m.editInput[:m.editCursor] + m.editInput[m.editCursor+sz:]
		}
		return m, nil
	case "ctrl+u":
		m.editInput = m.editInput[m.editCursor:]
		m.editCursor = 0
		return m, nil
	case "ctrl+k":
		m.editInput = m.editInput[:m.editCursor]
		return m, nil
	}

	if msg.Type != tea.KeyRunes {
		return m, nil
	}
	ch := string(msg.Runes)
	if item.Opt.Type() == api.OptionInt {
		if len(ch) != 1 {
			return m, nil
		}
		c := ch[0]
		if !((c >= '0' && c <= '9') || (c == '-' && m.editCursor == 0)) {
			return m, nil
		}
	}
	m.editInput = m.editInput[:m.editCursor] + ch + m.editInput[m.editCursor:]
	m.editCursor += len(ch)
	return m, nil
}

func parseIntInput(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	var val int
	if _, err := fmt.Sscanf(s, "%d", &val); err != nil {
		return 0, false
	}
	return val, true
}

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	if m.width < 50 || m.height < 8 {
		return lipgloss.NewStyle().Width(m.width).Height(m.height).Render(
			"Terminal too small. Please resize to at least 50x8.",
		)
	}

	if m.confirmQuit {
		dialog := m.renderConfirmDialog()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
	}

	if m.overlay != overlayNone {
		dialog := m.renderOverlay()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
	}

	header := m.renderHeader()

	treePanel := treePanelStyle(m.focusArea == 0, m.treeWidth).Render(m.renderTree())
	optWidth := m.width - m.treeWidth - 5
	if optWidth < 1 {
		optWidth = 1
	}
	optPanel := optionsPanelStyle(m.focusArea == 1, optWidth).Render(m.renderOptions())

	main := lipgloss.JoinHorizontal(
		lipgloss.Top,
		treePanel,
		optPanel,
	)

	footer := m.renderFooter()

	m.renderedHeaderH = lipgloss.Height(header)
	m.renderedFooterH = lipgloss.Height(footer)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		main,
		footer,
	)
}

func (m *Model) renderHeader() string {
	title := titleStyle.Render("◆ VMake Configuration")

	var rightParts []string
	if m.workDir != "" {
		rightParts = append(rightParts, titlePathStyle.Render(m.workDir))
	}
	if m.hasChanges {
		n := m.modifiedCount()
		rightParts = append(rightParts, modifiedBadgeStyle.Render(fmt.Sprintf("● %d modified", n)))
	}
	rightPart := strings.Join(rightParts, "  ")

	headerWidth := m.width - 6
	headerContent := lipgloss.JoinHorizontal(
		lipgloss.Center,
		title,
		lipgloss.PlaceHorizontal(headerWidth-lipgloss.Width(title), lipgloss.Right, rightPart),
	)

	return headerBorderStyle(true).Width(m.width - 4).Render(headerContent)
}

func (m *Model) renderTree() string {
	var topBar string
	if m.filterActive {
		topBar = filterBoxActiveStyle.Render("/"+m.filterInput+"▎") + "\n"
	} else if m.filterInput != "" {
		n := m.filteredMatchCount()
		topBar = filterBoxAppliedStyle.Render(fmt.Sprintf("filter: %s (%d)", m.filterInput, n)) + "\n"
	}

	panelH := m.treePanelHeight()

	if len(m.flat) == 0 {
		return topBar + filterBoxAppliedStyle.Render("  No matches")
	}

	total := len(m.flat)
	start := m.treeOff
	drawH := m.treeItemRows()
	end := min(start+drawH, total)

	var b strings.Builder
	b.WriteString(topBar)

	for i := start; i < end; i++ {
		node := m.flat[i]
		prefix := strings.Repeat("  ", node.Depth)

		icon := " "
		if node.PkgName == GlobalPkgName {
			icon = "◈"
		} else if len(node.Children) > 0 {
			if node.Expanded {
				icon = "▾"
			} else {
				icon = "▸"
			}
		} else if !node.IsExternal {
			icon = "●"
		} else {
			icon = "○"
		}

		name := node.Name
		count := 0
		if node.PkgName != "" {
			count = m.optCountFor(node.PkgName)
		}
		fixed := node.Depth*2 + utf8.RuneCountInString(node.Prefix) + 2
		badgeLen := 0
		if count > 0 {
			badgeLen = utf8.RuneCountInString(fmt.Sprintf(" (%d)", count))
		}
		avail := m.treeWidth - fixed - badgeLen
		if avail < 1 {
			avail = 1
		}
		if utf8.RuneCountInString(name) > avail {
			name = truncateRunes(name, avail)
		}
		if node.IsExternal {
			name = externalPkgStyle.Render(name)
		}

		if count > 0 {
			name += countBadgeStyle.Render(fmt.Sprintf(" (%d)", count))
		}

		line := prefix + node.Prefix + icon + " " + name

		isSelected := i == m.treeCursor
		isFocused := m.focusArea == 0

		if isSelected && isFocused {
			line = selectedRowStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}

	if total > panelH {
		pct := float64(m.treeOff+drawH) / float64(total) * 100
		indicator := scrollIndicatorStyle.Render(fmt.Sprintf("  %d/%d  %.0f%%", min(m.treeCursor+1, total), total, pct))
		b.WriteString(indicator)
	}

	return b.String()
}

func (m *Model) renderOptions() string {
	if m.selectedPkg == "" {
		return lipgloss.PlaceHorizontal(m.width-m.treeWidth-6, lipgloss.Center, "Select a package")
	}

	if m.runningMenuconfig {
		return "\n  Running menuconfig for " + m.selectedPkg + "...\n"
	}

	visible := m.visibleOptions()
	hasKConfig := m.hasKConfig()

	if len(visible) == 0 && !hasKConfig {
		return "No options"
	}

	nameWidth := 0
	valWidth := 0
	for _, item := range visible {
		if w := utf8.RuneCountInString(item.Name); w > nameWidth {
			nameWidth = w
		}
		valStr := fmt.Sprintf("%v", m.getValue(item.Name))
		if w := utf8.RuneCountInString(valStr); w > valWidth {
			valWidth = w
		}
	}
	nameWidth = min(nameWidth, 20)
	valWidth = min(valWidth, 16)

	var allRows []optionRow
	navigableIdx := 0
	currentGroup := ""
	for _, item := range visible {
		if item.Group != currentGroup {
			currentGroup = item.Group
			allRows = append(allRows, optionRow{kind: rowGroup, navIdx: -1, text: currentGroup})
		}
		allRows = append(allRows, optionRow{kind: rowOption, navIdx: navigableIdx, idx: len(allRows), item: item})
		navigableIdx++
	}

	presetIdx := navigableIdx
	menuconfigIdx := -1
	if hasKConfig {
		if m.hasPresets() {
			allRows = append(allRows, optionRow{kind: rowPreset, navIdx: navigableIdx, idx: len(allRows)})
			navigableIdx++
			menuconfigIdx = navigableIdx
			allRows = append(allRows, optionRow{kind: rowMenuconfig, navIdx: navigableIdx, idx: len(allRows)})
			navigableIdx++
		} else {
			menuconfigIdx = navigableIdx
			allRows = append(allRows, optionRow{kind: rowMenuconfig, navIdx: navigableIdx, idx: len(allRows)})
			navigableIdx++
		}
	}

	cursorRowIdx := -1
	for ri, r := range allRows {
		if r.navIdx == m.optCursor {
			cursorRowIdx = ri
			break
		}
	}

	panelH := m.contentHeight()
	total := len(allRows)
	drawH := panelH
	if total > panelH {
		drawH = panelH - 1
	}
	if cursorRowIdx >= 0 {
		if cursorRowIdx < m.optOff {
			m.optOff = cursorRowIdx
		}
		if cursorRowIdx >= m.optOff+drawH {
			m.optOff = cursorRowIdx - drawH + 1
		}
	}
	maxOff := total - drawH
	if maxOff < 0 {
		maxOff = 0
	}
	if m.optOff > maxOff {
		m.optOff = maxOff
	}
	if m.optOff < 0 {
		m.optOff = 0
	}

	start := m.optOff
	end := min(start+drawH, total)

	var b strings.Builder

	for i := start; i < end; i++ {
		row := allRows[i]
		switch row.kind {
		case rowGroup:
			sepLen := nameWidth + valWidth + 6
			sep := strings.Repeat("─", max(sepLen-utf8.RuneCountInString(row.text)-4, 3))
			b.WriteString(groupStyle.Render("── "+row.text+" "+sep) + "\n")
		case rowOption:
			selected := row.navIdx == m.optCursor && m.focusArea == 1
			b.WriteString(m.renderOptionAligned(row.item, selected, nameWidth, valWidth) + "\n")
		case rowPreset:
			selected := presetIdx == m.optCursor && m.focusArea == 1
			b.WriteString(m.renderPresetRow(selected, nameWidth, valWidth) + "\n")
		case rowMenuconfig:
			selected := menuconfigIdx == m.optCursor && m.focusArea == 1
			b.WriteString(m.renderMenuconfigRow(selected, nameWidth) + "\n")
		}
	}

	totalRows := len(allRows)
	if totalRows > panelH {
		pct := float64(m.optOff+drawH) / float64(totalRows) * 100
		indicator := scrollIndicatorStyle.Render(fmt.Sprintf("  %d/%d  %.0f%%", min(m.optCursor+1, m.totalOptRows()), m.totalOptRows(), pct))
		b.WriteString(indicator)
	}

	return b.String()
}

type rowKind int

const (
	rowGroup rowKind = iota
	rowOption
	rowPreset
	rowMenuconfig
)

type optionRow struct {
	kind   rowKind
	text   string
	idx    int
	navIdx int
	item   OptionItem
}

func (m *Model) renderOptionAligned(item OptionItem, selected bool, nameW, valW int) string {
	name := item.Name
	desc := item.Opt.Description()

	marker := " "
	if m.isOptionModified(item.Name) {
		marker = modifiedMarkStyle.Render("*")
	}

	var val string
	if m.editing && selected {
		switch item.Opt.Type() {
		case api.OptionString, api.OptionInt:
			val = inputStyle.Render(renderEditField(m.editInput, m.editCursor))
		}
	} else {
		switch item.Opt.Type() {
		case api.OptionBool:
			v := m.getValue(item.Name)
			if b, ok := v.(bool); ok && b {
				val = checkboxStyle.Render("●")
			} else {
				val = checkboxEmptyStyle.Render("○")
			}
		case api.OptionString, api.OptionInt:
			val = inputStyle.Render(fmt.Sprintf("%v", m.getValue(item.Name)))
		case api.OptionChoice:
			val = dropdownStyle.Render(fmt.Sprintf("◀ %v ▶", m.getValue(item.Name)))
		}
	}

	namePad := fmt.Sprintf("%-*s", nameW, name)
	valRaw := fmt.Sprintf("%v", m.getValue(item.Name))
	valPad := valW - utf8.RuneCountInString(valRaw)
	if valPad < 0 {
		valPad = 0
	}

	shownDesc := desc
	if m.editing && selected {
		def := item.Opt.Default()
		if def != nil {
			shownDesc = fmt.Sprintf("(default: %v)", def)
		}
	}

	var line string
	if selected {
		nameRendered := selectedOptStyle.Render(namePad)
		valRendered := val + strings.Repeat(" ", valPad)
		descRendered := optionDescStyle.Render(shownDesc)
		line = fmt.Sprintf("%s %s  %s  %s", marker, nameRendered, valRendered, descRendered)
		line = selectedRowStyle.Render(line)
	} else {
		nameRendered := optionNameStyle.Render(namePad)
		descRendered := optionDescStyle.Render(desc)
		line = fmt.Sprintf("%s %s  %s  %s", marker, nameRendered, val, descRendered)
	}

	return line
}

func renderEditField(input string, cursor int) string {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(input) {
		cursor = len(input)
	}
	return input[:cursor] + "▎" + input[cursor:]
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	out := []rune{}
	for _, r := range s {
		if len(out) == max-1 {
			break
		}
		out = append(out, r)
	}
	out = append(out, '…')
	return string(out)
}

func (m *Model) renderPresetRow(selected bool, nameW, valW int) string {
	presetName := m.currentPreset()
	name := "preset"
	namePad := fmt.Sprintf("%-*s", nameW, name)
	val := dropdownStyle.Render(fmt.Sprintf("◀ %s ▶", presetName))

	var line string
	if selected {
		nameRendered := selectedOptStyle.Render(namePad)
		line = fmt.Sprintf("  %s  %s", nameRendered, val)
		line = selectedRowStyle.Render(line)
	} else {
		nameRendered := optionNameStyle.Render(namePad)
		line = fmt.Sprintf("  %s  %s", nameRendered, val)
	}
	return line
}

func (m *Model) renderMenuconfigRow(selected bool, nameW int) string {
	name := "menuconfig"
	namePad := fmt.Sprintf("%-*s", nameW, name)
	desc := optionDescStyle.Render("▸ run menuconfig")

	var line string
	if selected {
		nameRendered := selectedOptStyle.Render(namePad)
		line = fmt.Sprintf("  %s  %s", nameRendered, desc)
		line = selectedRowStyle.Render(line)
	} else {
		nameRendered := optionNameStyle.Render(namePad)
		line = fmt.Sprintf("  %s  %s", nameRendered, desc)
	}
	return line
}

func (m *Model) renderConfirmDialog() string {
	title := confirmTitleStyle.Render("Unsaved Changes")
	msg := confirmMsgStyle.Render("Save before exiting?")

	saveBtn := " Save "
	discardBtn := " Discard "
	if m.confirmBtn == 0 {
		saveBtn = btnActiveStyle.Render("[ Save ]")
		discardBtn = btnInactiveStyle.Render("  Discard  ")
	} else {
		saveBtn = btnInactiveStyle.Render("  Save  ")
		discardBtn = btnActiveStyle.Render("[ Discard ]")
	}

	buttons := lipgloss.JoinHorizontal(lipgloss.Center, saveBtn, "  ", discardBtn)

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		title,
		"",
		msg,
		"",
		buttons,
		"",
		confirmMsgStyle.Render("Tab: switch │ Enter: confirm │ Esc: cancel"),
	)

	return confirmStyle.Render(content)
}

func (m *Model) renderOverlay() string {
	switch m.overlay {
	case overlayChoice:
		return m.renderChoiceOverlay()
	case overlayDetail:
		return m.renderDetailOverlay()
	}
	return ""
}

func (m *Model) renderChoiceOverlay() string {
	opt := m.detailOption()
	desc := ""
	if opt != nil {
		desc = opt.Description()
	}
	title := confirmTitleStyle.Render(m.choiceOpt)

	var rows []string
	for i, v := range m.choiceValues {
		marker := "  "
		cur := fmt.Sprintf("%v", m.getValue(m.choiceOpt))
		if v == cur {
			marker = checkboxStyle.Render("● ")
		}
		line := fmt.Sprintf("%s %s", marker, v)
		if i == m.choiceCursor {
			line = selectedRowStyle.Render(" " + line)
		} else {
			line = " " + line
		}
		rows = append(rows, line)
	}
	list := strings.Join(rows, "\n")

	hint := confirmMsgStyle.Render("↑↓ navigate │ Enter select │ Esc cancel")

	parts := []string{title}
	if desc != "" {
		parts = append(parts, optionDescStyle.Render(desc))
	}
	parts = append(parts, "", list, "", hint)
	content := lipgloss.JoinVertical(lipgloss.Center, parts...)
	return overlayStyle.Render(content)
}

func (m *Model) renderDetailOverlay() string {
	opt := m.detailOption()
	if opt == nil {
		return overlayStyle.Render(confirmMsgStyle.Render("No details available"))
	}
	cur := m.getValue(m.choiceOpt)
	def := opt.Default()

	type kv struct{ k, v string }
	rows := []kv{
		{"name", m.choiceOpt},
		{"type", opt.Type().String()},
		{"default", fmt.Sprintf("%v", def)},
		{"current", fmt.Sprintf("%v", cur)},
	}
	modified := m.isOptionModified(m.choiceOpt)
	status := "no"
	if modified {
		status = modifiedMarkStyle.Render("yes")
	}
	rows = append(rows, kv{"modified", status})

	keyW := 0
	for _, r := range rows {
		if len(r.k) > keyW {
			keyW = len(r.k)
		}
	}

	var b strings.Builder
	b.WriteString(confirmTitleStyle.Render(m.choiceOpt) + "\n\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("  %-*s : %s\n", keyW, r.k, r.v))
	}
	if desc := opt.Description(); desc != "" {
		b.WriteString("\n  " + optionDescStyle.Render(desc) + "\n")
	}
	if opt.Type() == api.OptionChoice && len(opt.Values()) > 0 {
		b.WriteString("\n  " + confirmMsgStyle.Render("choices: "+strings.Join(opt.Values(), ", ")) + "\n")
	}
	b.WriteString("\n" + confirmMsgStyle.Render("Esc/? to close"))
	return overlayStyle.Render(b.String())
}

func (m *Model) renderFooter() string {
	var helpText string
	if m.filterActive {
		helpText = renderHelpEntries([]helpEntry{
			{"Enter", "confirm/1 match"}, {"Backspace", "delete"}, {"Esc", "cancel"},
		})
	} else if m.filterInput != "" {
		helpText = renderHelpEntries([]helpEntry{
			{"↑↓", "pick match"}, {"Enter", "jump"}, {"/", "refine"}, {"Esc", "clear"},
		})
	} else if m.editing {
		helpText = renderHelpEntries([]helpEntry{
			{"←→", "move cursor"}, {"Home/End", "jump"}, {"Backspace", "delete"},
			{"Enter", "confirm"}, {"Esc", "cancel"},
		})
	} else if m.focusArea == 0 {
		helpText = renderHelpEntries([]helpEntry{
			{"↑↓", "navigate"}, {"←→", "collapse/expand"}, {"/", "search"},
			{"z/Z", "collapse/expand all"}, {"H", "hide empty"},
			{"Tab", "options"}, {"Ctrl+S", "save"}, {"Esc", "cancel"},
		})
	} else {
		helpText = renderHelpEntries([]helpEntry{
			{"↑↓", "navigate"}, {"←→", "cycle value"}, {"Space/Enter", "edit"},
			{"r", "reset"}, {"R", "default"}, {"?", "detail"},
			{"Tab", "tree"}, {"Ctrl+S", "save"}, {"Esc", "back"},
		})
	}

	if m.hasChanges {
		n := m.modifiedCount()
		badge := fmt.Sprintf("● %d modified", n)
		helpText += "  " + modifiedBadgeStyle.Render(badge)
	}

	return footerBorderStyle().Width(m.width - 4).Render(helpText)
}

type helpEntry struct {
	key string
	act string
}

func renderHelpEntries(entries []helpEntry) string {
	var parts []string
	for _, e := range entries {
		parts = append(parts, helpKeyStyle.Render(e.key)+":"+e.act)
	}
	return strings.Join(parts, helpSepStyle.Render(" │ "))
}
