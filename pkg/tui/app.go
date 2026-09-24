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

	iexec "github.com/spock2300/vmake/internal/exec"
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

type makeResolver func() (string, error)

func ensureConfigCmd(pkgName string, entries []*api.KConfigEntry, workDir string, resolveMake makeResolver) tea.Cmd {
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
		if info, err := os.Stat(configPath); err == nil && info.Size() > 0 {
			return menuconfigDone{pkgName: pkgName, ensured: true}
		}
		presetName := e.SelectedPreset()
		if presetName == "" {
			presetName = e.DefaultPreset()
		}
		if presetName == "" {
			return menuconfigDone{pkgName: pkgName, ensured: true}
		}
		makeTool, err := resolveMake()
		if err != nil {
			return menuconfigDone{pkgName: pkgName, ensured: true, err: fmt.Errorf("generate preset %s: %w", presetName, err)}
		}
		args := []string{"-C", filepath.ToSlash(srcDir), presetName}
		_, err = iexec.RunWithOptions(makeTool, args, iexec.RunOptions{Quiet: true})
		if err == nil {
			err = api.ApplyKConfigPatches(configPath, e.Patches())
		}
		return menuconfigDone{pkgName: pkgName, ensured: true, err: err}
	}
}

func runMenuconfigCmd(pkgName string, entries []*api.KConfigEntry, workDir string, resolveMake makeResolver) tea.Cmd {
	if len(entries) == 0 {
		return func() tea.Msg { return menuconfigDone{pkgName: pkgName} }
	}
	cmd, err := menuconfigProcess(entries[0], workDir, resolveMake)
	if err != nil {
		return func() tea.Msg { return menuconfigDone{pkgName: pkgName, err: err} }
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			err = fmt.Errorf("%s: %w", iexec.FormatCommandLine(cmd.Path, cmd.Args[1:]), err)
		}
		return menuconfigDone{pkgName: pkgName, err: err}
	})
}

func menuconfigProcess(entry *api.KConfigEntry, workDir string, resolveMake makeResolver) (*exec.Cmd, error) {
	srcDir := entry.SrcDir()
	if srcDir == "" {
		srcDir = workDir
	}
	program := entry.MenuconfigCmd()
	args := entry.MenuconfigArgs()
	if program == "" {
		var err error
		program, err = resolveMake()
		if err != nil {
			return nil, fmt.Errorf("menuconfig: %w", err)
		}
		args = []string{"menuconfig"}
	}
	cmd := exec.Command(program, args...)
	cmd.Dir = srcDir
	return cmd, nil
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case menuconfigDone:
		m.menuconfigErr = msg.err
		if msg.ensured {
			if msg.err != nil {
				m.runningMenuconfig = false
				m.hasChanges = true
				return m, nil
			}
			entries := m.kconfigs[msg.pkgName]
			return m, runMenuconfigCmd(msg.pkgName, entries, m.workDir, m.resolveMake())
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
	if m.width < 50 || m.height < 8 || msg.X < 0 || msg.Y < 0 || msg.X >= m.width || msg.Y >= m.height {
		return m, nil
	}
	if !mouseLeftPress(msg) && mouseWheel(msg) == 0 {
		return m, nil
	}
	m.View()
	if m.confirmQuit {
		return m.handleConfirmMouse(msg)
	}
	if m.overlay != overlayNone {
		return m.handleOverlayMouse(msg)
	}
	if m.editing {
		return m.handleEditingMouse(msg)
	}
	if m.filterActive || m.runningMenuconfig {
		return m, nil
	}

	mx, my := msg.X, msg.Y
	headerH := m.headerHeight()
	if mouseLeftPress(msg) && my == m.languageSelectorY && mx >= m.languageSelectorX && mx < m.languageSelectorX+m.languageSelectorW {
		m.openLanguageSelector()
		return m, nil
	}
	if my < headerH || my >= headerH+m.renderedMainH {
		return m, nil
	}
	inTree := mx < m.renderedTreeW

	switch {
	case mouseLeftPress(msg):
		if inTree {
			m.focusArea = 0
			for _, target := range m.treeMouseRows {
				if my-headerH < target.y || my-headerH >= target.y+target.height {
					continue
				}
				idx := target.index
				m.treeCursor = idx
				m.selectCurrentNode()
				if len(m.flat[idx].Children) > 0 {
					m.flat[idx].Expanded = !m.flat[idx].Expanded
					m.rebuildFlat()
				}
				m.ensureTreeCursorVisible()
				break
			}
		} else {
			m.focusArea = 1
			return m.handleMouseOptionClick(msg)
		}
	case mouseWheel(msg) != 0:
		if inTree {
			m.focusArea = 0
			m.treeOff = clamp(m.treeOff+mouseWheel(msg), 0, max(0, len(m.flat)-m.treeItemRows()))
		} else {
			m.focusArea = 1
			m.optScrolled = true
			m.optOff = clamp(m.optOff+mouseWheel(msg), 0, max(0, len(m.optionLines())-m.optItemRows()))
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
	if m.overlay == overlayDetail {
		switch msg.String() {
		case "up", "k", "down", "j", "pgup", "pgdown", "home", "g", "end", "G":
			m.scrollDetail(msg.String())
			return m, nil
		}
	}
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
		if (m.overlay == overlayChoice || m.overlay == overlayLanguage) && m.choiceCursor > 0 {
			m.choiceCursor--
		}
	case "down", "j":
		if (m.overlay == overlayChoice || m.overlay == overlayLanguage) && m.choiceCursor < len(m.choiceValues)-1 {
			m.choiceCursor++
		}
	case "home", "g":
		if m.overlay == overlayChoice || m.overlay == overlayLanguage {
			m.choiceCursor = 0
		}
	case "end", "G":
		if (m.overlay == overlayChoice || m.overlay == overlayLanguage) && len(m.choiceValues) > 0 {
			m.choiceCursor = len(m.choiceValues) - 1
		}
	case "enter":
		if m.overlay == overlayChoice && m.choiceCursor < len(m.choiceValues) {
			m.setValue(m.choiceOpt, m.choiceValues[m.choiceCursor])
		} else if m.overlay == overlayLanguage && m.choiceCursor < len(m.choiceValues) {
			if m.choiceCursor == 0 {
				m.language = languageEnglish
			} else {
				m.language = languageChinese
			}
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
		m.optScrolled = false
	}
}

func (m *Model) handleOptionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.optScrolled = false
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
		m.menuconfigErr = nil
		m.runningMenuconfig = true
		m.saved = false
		return m, ensureConfigCmd(m.selectedPkg, entries, m.workDir, m.resolveMake())
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
			m.text(textTerminalTooSmall),
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
	footer := m.renderFooter()
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
	m.renderedHeaderH = lipgloss.Height(header)
	m.renderedFooterH = lipgloss.Height(footer)

	treePanel := treeStyle.MaxHeight(m.contentHeight()).Render(m.renderTree())
	m.renderedTreeW = lipgloss.Width(treePanel)
	optPanel := optionStyle.MaxHeight(m.contentHeight()).Render(m.renderOptions())

	main := lipgloss.JoinHorizontal(
		lipgloss.Top,
		treePanel,
		optPanel,
	)
	m.renderedMainH = lipgloss.Height(main)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		main,
		footer,
	)
}

func (m *Model) renderHeader() string {
	return m.renderHeaderWithin(0)
}

func (m *Model) renderHeaderWithin(maxHeight int) string {
	title := titleStyle.Padding(0).Render("◆ " + m.text(textVMakeConfiguration))

	var rightParts []string
	if m.workDir != "" {
		rightParts = append(rightParts, titlePathStyle.Render(m.workDir))
	}
	if m.hasChanges {
		n := m.modifiedCount()
		rightParts = append(rightParts, modifiedBadgeStyle.Render(m.textf(textModified, n)))
	}
	languageSelector := languageSelectorStyle.Render("[" + m.text(textLanguage) + ": " + m.languageLabel() + " ▼]")
	headerStyle := headerBorderStyle(true).Width(m.width - 4)
	headerWidth := headerStyle.GetWidth() - headerStyle.GetHorizontalFrameSize()
	m.languageSelectorW = lipgloss.Width(languageSelector)
	leftWidth := max(1, headerWidth-m.languageSelectorW-2)
	m.languageSelectorX = headerStyle.GetBorderLeftSize() + headerStyle.GetPaddingLeft() + leftWidth + 2
	m.languageSelectorY = headerStyle.GetBorderTopSize() + headerStyle.GetPaddingTop()
	rightPart := strings.Join(rightParts, "  ")

	leftContent := title
	if rightPart != "" {
		if lipgloss.Width(title)+2+lipgloss.Width(rightPart) <= leftWidth {
			leftContent += lipgloss.PlaceHorizontal(leftWidth-lipgloss.Width(title), lipgloss.Right, rightPart)
		} else {
			leftContent += "\n" + rightPart
		}
	}
	leftStyle := lipgloss.NewStyle().Width(leftWidth)
	if maxHeight > 0 {
		leftStyle = leftStyle.MaxHeight(max(1, maxHeight-headerStyle.GetVerticalFrameSize()))
	}
	left := leftStyle.Render(leftContent)
	headerContent := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", languageSelector)

	return headerStyle.Render(headerContent)
}

func (m *Model) renderTree() string {
	m.treeMouseRows = nil
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

	panelH := m.treePanelHeight()

	if len(m.flat) == 0 {
		b.WriteString(topBar)
		b.WriteString(filterBoxAppliedStyle.Render("  " + m.text(textNoMatches)))
		return b.String()
	}

	total := len(m.flat)
	start := m.treeOff
	drawH := m.treeItemRows()
	end := min(start+drawH, total)

	if topBar != "" {
		topBar = lipgloss.NewStyle().MaxWidth(contentWidth).Render(strings.TrimSuffix(topBar, "\n")) + "\n"
	}
	b.WriteString(topBar)
	rowY := 1 + strings.Count(topBar, "\n")

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
		if node.PkgName == GlobalPkgName {
			name = m.packageLabel(node.PkgName)
		}
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
		line = lipgloss.NewStyle().MaxWidth(contentWidth).Render(line)
		m.treeMouseRows = append(m.treeMouseRows, panelRowTarget{y: rowY, height: 1, index: i})
		b.WriteString(line + "\n")
		rowY++
	}

	if total > panelH && panelH > 1 {
		pct := float64(m.treeOff+drawH) / float64(total) * 100
		indicator := scrollIndicatorStyle.Render(fmt.Sprintf("  %d/%d  %.0f%%", min(m.treeCursor+1, total), total, pct))
		b.WriteString(indicator)
	}

	return strings.TrimSuffix(b.String(), "\n")
}

func (m *Model) renderOptions() string {
	m.optionMouseRows = nil
	title := m.text(textOptions)
	if m.selectedPkg != "" {
		title += " · " + m.packageLabel(m.selectedPkg)
	}
	var b strings.Builder
	b.WriteString(panelTitleStyle.MaxWidth(m.optionsContentWidth()).Render(title) + "\n")

	if m.selectedPkg == "" {
		b.WriteString(lipgloss.PlaceHorizontal(m.width-m.treeWidth-6, lipgloss.Center, m.text(textSelectPackage)))
		return b.String()
	}

	if m.runningMenuconfig {
		b.WriteString("\n  " + m.textf(textRunningMenuconfig, m.packageLabel(m.selectedPkg)) + "\n")
		return b.String()
	}

	visible := m.visibleOptions()
	hasKConfig := m.hasKConfig()

	if len(visible) == 0 && !hasKConfig {
		b.WriteString(m.text(textNoOptions))
		return b.String()
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

	panelH := m.contentHeight() - 1
	if panelH < 1 {
		panelH = 1
	}
	total := len(allRows)
	drawH := panelH
	if total > panelH {
		drawH = max(1, panelH-1)
	}
	if cursorRowIdx >= 0 && !m.optScrolled {
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

	for i := start; i < end; i++ {
		line := allRows[i]
		m.optionMouseRows = append(m.optionMouseRows, panelRowTarget{y: i - start + 1, height: 1, index: line.row.navIdx})
		b.WriteString(line.text + "\n")
	}

	totalRows := len(allRows)
	if totalRows > panelH && panelH > 1 {
		pct := float64(m.optOff+drawH) / float64(totalRows) * 100
		indicator := scrollIndicatorStyle.Render(fmt.Sprintf("  %d/%d  %.0f%%", min(m.optCursor+1, m.totalOptRows()), m.totalOptRows(), pct))
		b.WriteString(indicator)
	}

	return strings.TrimSuffix(b.String(), "\n")
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
	name := m.text(textPreset)
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
	desc := optionDescStyle.Render("▸ " + m.text(textRunMenuconfig))

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
	return m.confirmDialogLayout().view
}

func (m *Model) renderOverlay() string {
	switch m.overlay {
	case overlayChoice, overlayLanguage:
		return m.renderChoiceOverlay()
	case overlayDetail:
		return m.renderDetailOverlay()
	}
	return ""
}

func (m *Model) renderChoiceOverlay() string {
	return m.choiceDialogLayout().view
}

func (m *Model) renderDetailOverlay() string {
	opt := m.detailOption()
	if opt == nil {
		return m.renderDetailDialog(confirmMsgStyle.Render(m.text(textNoDetails)))
	}
	cur := m.getValue(m.choiceOpt)
	def := opt.Default()

	type kv struct{ k, v string }
	rows := []kv{
		{m.text(textName), m.choiceOpt},
		{m.text(textType), opt.Type().String()},
		{m.text(textDefault), fmt.Sprintf("%v", def)},
		{m.text(textCurrent), fmt.Sprintf("%v", cur)},
	}
	modified := m.isOptionModified(m.choiceOpt)
	status := m.text(textNo)
	if modified {
		status = modifiedMarkStyle.Render(m.text(textYes))
	}
	rows = append(rows, kv{m.text(textModifiedLabel), status})

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
		b.WriteString("\n  " + confirmMsgStyle.Render(m.text(textChoices)+strings.Join(opt.Values(), ", ")) + "\n")
	}
	return m.renderDetailDialog(b.String())
}

func (m *Model) renderFooter() string {
	return m.renderFooterWithin(0)
}

func (m *Model) renderFooterWithin(maxHeight int) string {
	var helpText string
	if m.filterActive {
		helpText = renderHelpEntries([]helpEntry{
			{"Enter", m.text(textConfirmMatch)}, {"Backspace", m.text(textDelete)}, {"Esc", m.text(textCancel)},
		})
	} else if m.filterInput != "" {
		helpText = renderHelpEntries([]helpEntry{
			{"↑↓", m.text(textPickMatch)}, {"Enter", m.text(textJump)}, {"/", m.text(textRefine)}, {"Esc", m.text(textClear)},
		})
	} else if m.editing {
		helpText = renderHelpEntries([]helpEntry{
			{"←→", m.text(textMoveCursor)}, {"Home/End", m.text(textJump)}, {"Backspace", m.text(textDelete)},
			{"Enter", m.text(textConfirm)}, {"Esc", m.text(textCancel)},
		})
	} else if m.focusArea == 0 {
		helpText = renderHelpEntries([]helpEntry{
			{"↑↓", m.text(textNavigate)}, {"←→", m.text(textCollapseExpand)}, {"/", m.text(textSearch)},
			{"z/Z", m.text(textCollapseExpandAll)}, {"H", m.text(textHideEmpty)},
			{"Tab", m.text(textOptions)}, {"Ctrl+S", m.text(textSave)}, {"Esc", m.text(textCancel)},
		})
	} else {
		helpText = renderHelpEntries([]helpEntry{
			{"↑↓", m.text(textNavigate)}, {"←→", m.text(textCycleValue)}, {"Space/Enter", m.text(textEdit)},
			{"r", m.text(textReset)}, {"R", m.text(textDefault)}, {"?", m.text(textDetail)},
			{"Tab", m.text(textPackages)}, {"Ctrl+S", m.text(textSave)}, {"Esc", m.text(textBack)},
		})
	}

	if m.hasChanges {
		n := m.modifiedCount()
		badge := m.textf(textModified, n)
		helpText += "  " + modifiedBadgeStyle.Render(badge)
	}
	if m.menuconfigErr != nil {
		helpText += "\n" + m.menuconfigErr.Error()
	}
	if diagnostics := m.toolchainDiagnostics(); diagnostics != "" {
		helpText += "\n" + diagnostics
	}

	footerStyle := footerBorderStyle().Width(m.width - 4)
	if maxHeight > 0 {
		contentWidth := max(1, footerStyle.GetWidth()-footerStyle.GetHorizontalFrameSize())
		contentHeight := max(1, maxHeight-footerStyle.GetVerticalFrameSize())
		helpText = lipgloss.NewStyle().Width(contentWidth).MaxHeight(contentHeight).Render(helpText)
	}
	return footerStyle.Render(helpText)
}

type helpEntry struct {
	key string
	act string
}

func renderHelpEntries(entries []helpEntry) string {
	var parts []string
	for _, e := range entries {
		parts = append(parts, helpKeyStyle.Render(e.key)+" "+e.act)
	}
	return strings.Join(parts, helpSepStyle.Render(" │ "))
}
