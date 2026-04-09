package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/buildscript"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	p := tea.NewProgram(&m, tea.WithAltScreen())
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

	if m.editing {
		return m.handleEditKey(msg)
	}

	if m.runningMenuconfig {
		return m, nil
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
	case "esc":
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

func (m *Model) handleTreeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			m.flat = flattenTree(m.tree)
			m.treeWidth = calcTreeWidth(m.flat)
			m.ensureTreeCursorVisible()
		}
	case "right", "l":
		if m.treeCursor < len(m.flat) && len(m.flat[m.treeCursor].Children) > 0 {
			m.flat[m.treeCursor].Expanded = true
			m.flat = flattenTree(m.tree)
			m.treeWidth = calcTreeWidth(m.flat)
			m.ensureTreeCursorVisible()
		}
	case "enter":
		if m.treeCursor < len(m.flat) && len(m.flat[m.treeCursor].Children) > 0 {
			m.flat[m.treeCursor].Expanded = !m.flat[m.treeCursor].Expanded
			m.flat = flattenTree(m.tree)
			m.treeWidth = calcTreeWidth(m.flat)
			m.ensureTreeCursorVisible()
		}
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

	switch msg.String() {
	case "esc":
		m.editing = false
		return m, nil
	case "enter":
		m.editing = false
		switch item.Opt.Type() {
		case api.OptionString:
			m.setValue(item.Name, m.editInput)
		case api.OptionInt:
			var val int
			if _, err := fmt.Sscanf(m.editInput, "%d", &val); err != nil {
				return m, nil
			}
			m.setValue(item.Name, val)
		}
		return m, nil
	case "backspace":
		if item.Opt.Type() == api.OptionString || item.Opt.Type() == api.OptionInt {
			if len(m.editInput) > 0 {
				_, sz := utf8.DecodeLastRuneInString(m.editInput)
				m.editInput = m.editInput[:len(m.editInput)-sz]
			}
		}
	default:
		ch := msg.String()
		if item.Opt.Type() == api.OptionInt {
			if len(ch) == 1 && (ch >= "0" && ch <= "9" || ch == "-" && len(m.editInput) == 0) {
				m.editInput += ch
			}
		} else if item.Opt.Type() == api.OptionString {
			m.editInput += ch
		}
	}
	return m, nil
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

	header := m.renderHeader()

	treePanel := treePanelStyle(m.focusArea == 0, m.treeWidth).Render(m.renderTree())
	optWidth := m.width - m.treeWidth - 4
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
		rightParts = append(rightParts, modifiedBadgeStyle.Render("● Modified"))
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
	if len(m.flat) == 0 {
		return ""
	}

	panelH := m.contentHeight()

	total := len(m.flat)
	start := m.treeOff
	drawH := panelH
	if total > panelH {
		drawH = panelH - 1
	}
	end := min(start+drawH, total)

	var b strings.Builder

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
		if node.IsExternal {
			name = externalPkgStyle.Render(name)
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
		pct := float64(m.treeOff+panelH) / float64(total) * 100
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
	if cursorRowIdx >= 0 {
		if cursorRowIdx < m.optOff {
			m.optOff = cursorRowIdx
		}
		if cursorRowIdx >= m.optOff+panelH {
			m.optOff = cursorRowIdx - panelH + 1
		}
	}
	total := len(allRows)
	if m.optOff+panelH > total && total > panelH {
		m.optOff = total - panelH
	}
	if m.optOff < 0 {
		m.optOff = 0
	}

	drawH := panelH
	if total > panelH {
		drawH = panelH - 1
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
		pct := float64(m.optOff+panelH) / float64(totalRows) * 100
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

	var val string
	if m.editing && selected {
		switch item.Opt.Type() {
		case api.OptionString, api.OptionInt:
			val = inputStyle.Render(m.editInput + "▎")
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

	var line string
	if selected {
		nameRendered := selectedOptStyle.Render(namePad)
		valRendered := val + strings.Repeat(" ", valPad)
		descRendered := optionDescStyle.Render(desc)
		line = fmt.Sprintf("  %s  %s  %s", nameRendered, valRendered, descRendered)
		line = selectedRowStyle.Render(line)
	} else {
		nameRendered := optionNameStyle.Render(namePad)
		descRendered := optionDescStyle.Render(desc)
		line = fmt.Sprintf("  %s  %s  %s", nameRendered, val, descRendered)
	}

	return line
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

func (m *Model) renderFooter() string {
	var helpText string
	if m.editing {
		visible := m.visibleOptions()
		if m.optCursor < len(visible) {
			item := visible[m.optCursor]
			if item.Opt.Type() == api.OptionChoice {
				helpText = renderHelpEntries([]helpEntry{
					{"↑↓", "select"}, {"Enter", "confirm"}, {"Esc", "cancel"},
				})
			} else {
				helpText = renderHelpEntries([]helpEntry{
					{"Enter", "confirm"}, {"Esc", "cancel"},
				})
			}
		} else {
			helpText = renderHelpEntries([]helpEntry{
				{"Enter", "confirm"}, {"Esc", "cancel"},
			})
		}
	} else if m.focusArea == 0 {
		helpText = renderHelpEntries([]helpEntry{
			{"↑↓", "navigate"}, {"←→", "collapse/expand"}, {"Tab", "options"},
			{"Enter", "expand"}, {"Ctrl+S", "save"}, {"Esc", "cancel"},
		})
	} else {
		helpText = renderHelpEntries([]helpEntry{
			{"↑↓", "navigate"}, {"←→", "cycle value"}, {"Space/Enter", "edit"},
			{"Tab", "tree"}, {"Ctrl+S", "save"}, {"Esc", "back"},
		})
	}

	if m.hasChanges {
		helpText += "  " + modifiedBadgeStyle.Render("● Modified")
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
