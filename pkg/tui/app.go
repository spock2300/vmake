package tui

import (
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
)

type ConfigResult struct {
	Saved          bool
	Description    string
	Values         map[string]map[string]any
	Toolchain      string
	GlobalValues   map[string]any
	DerivedGlobals map[string]bool
	MenuconfigRan  map[string]bool
	PresetValues   map[string]string
	Editor         *config.Editor
}

func (r *ConfigResult) Commit(base *config.ConfigFile, kconfigs map[string][]*api.KConfigEntry, readKConfig func(pkg string, entry *api.KConfigEntry) (string, error)) (*config.ConfigFile, error) {
	if r == nil {
		return base.Clone(), nil
	}
	return config.CommitSnapshot(base, config.CommitSnapshotInput{
		Description:    r.Description,
		Values:         r.Values,
		Toolchain:      r.Toolchain,
		GlobalValues:   r.GlobalValues,
		DerivedGlobals: r.DerivedGlobals,
		MenuconfigRan:  r.MenuconfigRan,
		PresetValues:   r.PresetValues,
		KConfigs:       kconfigs,
	}, readKConfig)
}

func Run(in Input) (*ConfigResult, error) {
	m := NewModel(in)
	p := tea.NewProgram(&m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return nil, err
	}
	return m.result(), nil
}

func (m *Model) result() *ConfigResult {
	ed := m.ed()
	return &ConfigResult{
		Saved:          m.saved,
		Description:    ed.Desc,
		Values:         m.sparseValues(),
		Toolchain:      getToolchainValue(ed.GlobalValues),
		GlobalValues:   m.sparseGlobalValues(),
		DerivedGlobals: maps.Clone(ed.DerivedGlobals),
		MenuconfigRan:  ed.MenuconfigRan,
		PresetValues:   ed.PresetValues,
		Editor:         ed,
	}
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
		_, err := api.BindEnsureKConfigPreset(e, srcDir, false, func(dir string, extraEnv map[string]string, args ...string) error {
			makeTool, err := resolveMake()
			if err != nil {
				return fmt.Errorf("generate preset %s: %w", strings.Join(args, " "), err)
			}
			makeArgs := append([]string{"-C", filepath.ToSlash(dir)}, args...)
			_, err = iexec.RunWithOptions(makeTool, makeArgs, iexec.RunOptions{Quiet: true, Env: extraEnv})
			return err
		})
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
	if configName := entry.ConfigPath(); configName != "" && configName != ".config" {
		cmd.Env = append(cmd.Environ(), "KCONFIG_CONFIG="+filepath.ToSlash(filepath.Join(srcDir, configName)))
	}
	return cmd, nil
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case menuconfigDone:
		ed := m.ed()
		m.menuconfigErr = msg.err
		if msg.ensured {
			if msg.err != nil {
				m.runningMenuconfig = false
				ed.SetHasChanges(true)
				return m, nil
			}
			entries := ed.KConfigs[msg.pkgName]
			return m, runMenuconfigCmd(msg.pkgName, entries, m.workDir, m.resolveMake())
		}
		m.runningMenuconfig = false
		if msg.err != nil {
			ed.SetHasChanges(true)
		} else {
			ed.MarkMenuconfigRan(msg.pkgName)
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

	if m.descEditing {
		return m.handleDescEditKey(msg)
	}

	if m.runningMenuconfig {
		return m, nil
	}

	if m.filterActive {
		return m.handleFilterKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		if m.ed().HasChanges() {
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
			if m.ed().HasChanges() {
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
				ed := m.ed()
				m.treeCursor = 0
				for i, n := range m.flat {
					if nodeDirectMatch(n, query, ed.Options, ed.GlobalOptions) {
						m.treeCursor = i
						break
					}
				}
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
	if m.descEditing {
		return m.handleDescriptionMouse(msg)
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
	inGraph := !inTree && m.renderedGraphH > 0 && my < headerH+m.renderedGraphH

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
		} else if inGraph {
			if hit, ok := m.egoHitAt(mx, my); ok {
				if hit.isMore {
					if hit.isDep {
						m.downEdgeIdx++
					} else {
						m.upEdgeSelected = true
						m.upEdgeIdx++
					}
				} else if hit.pkgName != "" {
					m.jumpToGraphNeighbor(hit.pkgName, true)
				}
			}
		} else {
			m.focusArea = 1
			return m.handleMouseOptionClick(msg)
		}
	case mouseWheel(msg) != 0:
		if inTree {
			m.focusArea = 0
			m.moveTreeCursor(mouseWheel(msg))
		} else if inGraph {
			delta := mouseWheel(msg)
			midX := m.renderedTreeW + m.optionsPanelWidth()/2
			if mx < midX {
				m.upEdgeSelected = true
				m.upEdgeIdx += delta
			} else {
				m.downEdgeIdx += delta
			}
		} else {
			m.focusArea = 1
			m.moveOptionCursor(mouseWheel(msg))
		}
	}
	return m, nil
}

func (m *Model) moveTreeCursor(delta int) {
	if len(m.flat) == 0 {
		return
	}
	next := clamp(m.treeCursor+delta, 0, len(m.flat)-1)
	if next == m.treeCursor {
		return
	}
	m.navHistory = nil
	m.upEdgeIdx = 0
	m.downEdgeIdx = 0
	m.upEdgeSelected = false
	m.treeCursor = next
	m.selectCurrentNode()
	m.ensureTreeCursorVisible()
}

func (m *Model) moveOptionCursor(delta int) {
	m.optCursor = clamp(m.optCursor+delta, 0, max(0, m.totalOptRows()-1))
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
		m.moveTreeCursor(-1)
	case "down", "j":
		m.moveTreeCursor(1)
	case "left", "h":
		if m.treeCursor < len(m.flat) && len(m.flat[m.treeCursor].Children) > 0 && m.flat[m.treeCursor].Expanded {
			m.flat[m.treeCursor].Expanded = false
			m.rebuildFlat()
			m.ensureTreeCursorVisible()
		} else {
			m.stepUpstream()
		}
	case "right", "l":
		if m.treeCursor < len(m.flat) && len(m.flat[m.treeCursor].Children) > 0 && !m.flat[m.treeCursor].Expanded {
			m.flat[m.treeCursor].Expanded = true
			m.rebuildFlat()
			m.ensureTreeCursorVisible()
		} else if len(m.visibleOptions()) > 0 || m.hasKConfig() {
			m.focusArea = 1
		}
	case "[":
		m.stepUpstream()
	case "]":
		m.stepDownstream()
	case "{":
		m.upEdgeSelected = true
		m.upEdgeIdx++
	case "}":
		m.downEdgeIdx++
	case "enter", " ":
		if m.treeCursor < len(m.flat) && len(m.flat[m.treeCursor].Children) > 0 {
			m.flat[m.treeCursor].Expanded = !m.flat[m.treeCursor].Expanded
			m.rebuildFlat()
			m.ensureTreeCursorVisible()
		} else {
			m.focusArea = 1
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
	if m.treeCursor < len(m.flat) && m.flat[m.treeCursor].PkgName != "" && !m.flat[m.treeCursor].IsGroup {
		m.selectedPkg = m.flat[m.treeCursor].PkgName
		m.buildOptionItems()
		m.optCursor = 0
		m.optOff = 0
	}
	m.updateGraphTracks()
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
		entries := m.ed().KConfigs[m.selectedPkg]
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
		m.moveOptionCursor(-1)
	case "down", "j":
		m.moveOptionCursor(1)
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
	case "D":
		if m.descRowOffset == 1 {
			m.startDescriptionEdit()
		}
	case " ", "enter", "right", "l":
		m.handleOptionAction(visible, presetIdx, false, msg.String())
	case "left", "h":
		m.handleOptionAction(visible, presetIdx, true, msg.String())
	case "[":
		m.stepUpstream()
	case "]":
		m.stepDownstream()
	case "{":
		m.upEdgeSelected = true
		m.upEdgeIdx++
	case "}":
		m.downEdgeIdx++
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
		current := formatValue(m.getValue(item.Name))
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
			m.editInput = formatValue(m.getValue(item.Name))
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
		if m.editInput == formatValue(m.getValue(item.Name)) {
			return m, nil
		}
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

func (m *Model) startDescriptionEdit() {
	m.descEditing = true
	m.descInput = sanitizeDescription(m.ed().Desc)
	m.descCursor = len(m.descInput)
}

func (m *Model) handleDescEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "esc":
		m.descEditing = false
		return m, nil
	case "enter":
		m.descEditing = false
		// 编辑框是单行、显示时折行；没改内容就不要用折行版本覆盖原文（原文可以含换行）。
		// 非法原文（例如旧版本写下的超长文本）仍要借这次编辑归一化。
		if _, err := config.ValidateDescription(m.ed().Desc); err != nil || m.descInput != sanitizeDescription(m.ed().Desc) {
			m.setDescription(m.descInput)
		}
		return m, nil
	case "left":
		if m.descCursor > 0 {
			_, sz := utf8.DecodeLastRuneInString(m.descInput[:m.descCursor])
			m.descCursor -= sz
		}
		return m, nil
	case "right":
		if m.descCursor < len(m.descInput) {
			_, sz := utf8.DecodeRuneInString(m.descInput[m.descCursor:])
			m.descCursor += sz
		}
		return m, nil
	case "home", "ctrl+a":
		m.descCursor = 0
		return m, nil
	case "end", "ctrl+e":
		m.descCursor = len(m.descInput)
		return m, nil
	case "backspace":
		if m.descCursor > 0 {
			_, sz := utf8.DecodeLastRuneInString(m.descInput[:m.descCursor])
			m.descInput = m.descInput[:m.descCursor-sz] + m.descInput[m.descCursor:]
			m.descCursor -= sz
		}
		return m, nil
	case "delete":
		if m.descCursor < len(m.descInput) {
			_, sz := utf8.DecodeRuneInString(m.descInput[m.descCursor:])
			m.descInput = m.descInput[:m.descCursor] + m.descInput[m.descCursor+sz:]
		}
		return m, nil
	case "ctrl+u":
		m.descInput = m.descInput[m.descCursor:]
		m.descCursor = 0
		return m, nil
	case "ctrl+k":
		m.descInput = m.descInput[:m.descCursor]
		return m, nil
	}

	if msg.Type != tea.KeyRunes {
		return m, nil
	}
	ch := string(descRunes(msg.Runes))
	if ch == "" {
		return m, nil
	}
	if utf8.RuneCountInString(m.descInput)+utf8.RuneCountInString(ch) > config.MaxDescriptionLength {
		return m, nil
	}
	m.descInput = m.descInput[:m.descCursor] + ch + m.descInput[m.descCursor:]
	m.descCursor += len(ch)
	return m, nil
}

func descRunes(runes []rune) []rune {
	var out []rune
	for _, r := range runes {
		if r == '\t' || r < 32 || r == 127 {
			continue
		}
		out = append(out, r)
	}
	return out
}

func sanitizeDescription(text string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\t' || r < 32 || r == 127 {
			return ' '
		}
		return r
	}, text)
	runes := []rune(cleaned)
	if len(runes) > config.MaxDescriptionLength {
		runes = runes[:config.MaxDescriptionLength]
	}
	return string(runes)
}

func (m *Model) setDescription(text string) {
	text, err := config.ValidateDescription(text)
	if err != nil {
		return
	}
	m.ed().SetDescription(text)
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

	var rightCol string
	if egoInner := m.egoGraphInnerHeight(); egoInner > 0 {
		graphStyle := egoGraphPanelStyle(m.focusArea == 0, m.optionsPanelWidth())
		graphPanel := graphStyle.MaxHeight(egoInner + 1).Render(m.renderEgoGraph())
		m.renderedGraphH = lipgloss.Height(graphPanel)
		optMaxH := max(1, m.contentHeight()-m.renderedGraphH)
		optPanel := optionStyle.MaxHeight(optMaxH).Render(m.renderOptions())
		rightCol = lipgloss.JoinVertical(lipgloss.Left, graphPanel, optPanel)
	} else {
		m.renderedGraphH = 0
		m.egoNodeBoxes = nil
		rightCol = optionStyle.MaxHeight(m.contentHeight()).Render(m.renderOptions())
	}

	main := lipgloss.JoinHorizontal(
		lipgloss.Top,
		treePanel,
		rightCol,
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
	if m.ed().HasChanges() {
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

	m.updateGraphTracks()

	for i := start; i < end; i++ {
		node := m.flat[i]
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
		} else if node.GutterRole == gutterFocal || isSelected {
			icon = "◆"
		} else if node.GutterRole == gutterUpstream {
			icon = "◀"
		} else if node.GutterRole == gutterDownstream {
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
			switch node.GutterRole {
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

	m.descRowOffset = 0
	if m.optionsContentHeight() >= 3 {
		b.WriteString(m.renderDescriptionRow())
		m.descRowOffset = 1
	}

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

	panelH := m.optionsContentHeight() - 1 - m.descRowOffset
	if panelH < 1 {
		panelH = 1
	}
	total := len(allRows)
	drawH := panelH
	if total > panelH {
		drawH = max(1, panelH-1)
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

	for i := start; i < end; i++ {
		line := allRows[i]
		m.optionMouseRows = append(m.optionMouseRows, panelRowTarget{y: i - start + 1 + m.descRowOffset, height: 1, index: line.row.navIdx})
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

func (m *Model) renderDescriptionRow() string {
	contentWidth := m.optionsContentWidth()
	label := m.text(textDescription) + ":"
	valueWidth := contentWidth - lipgloss.Width(label) - 2
	if valueWidth < 1 {
		valueWidth = 1
	}
	var value string
	switch {
	case m.descEditing:
		value = inputStyle.Render(truncateDisplay(renderEditField(m.descInput, m.descCursor), valueWidth))
	case m.ed().Desc != "":
		value = optionDescStyle.Render(truncateDisplay(sanitizeDescription(m.ed().Desc), valueWidth))
	default:
		value = optionDescStyle.Render(m.text(textDescriptionNotSet))
	}
	line := groupStyle.Render(label) + " " + value
	return lipgloss.NewStyle().MaxWidth(contentWidth).Render(line) + "\n"
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
	bg := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(colorHighlight)
		}
		return s
	}
	space := func(n int) string {
		if n <= 0 {
			return ""
		}
		s := strings.Repeat(" ", n)
		if selected {
			return selectedRowStyle.Render(s)
		}
		return s
	}

	derivedOwner, isDerived := m.optionDerivedBy(m.selectedPkg, item.Name)
	marker := space(1)
	if m.isOptionModified(item.Name) {
		marker = bg(modifiedMarkStyle).Render("*")
	} else if m.ed().IsExplicit(m.selectedPkg, item.Name) {
		marker = bg(explicitMarkStyle).Render("•")
	} else if isDerived {
		marker = bg(derivedMarkStyle).Render("←")
	}

	var val string
	valPlainCols := 0
	if m.editing && selected {
		switch item.Opt.Type() {
		case api.OptionString, api.OptionInt:
			ef := renderEditField(m.editInput, m.editCursor)
			valPlainCols = lipgloss.Width(ef)
			val = bg(inputStyle).Render(ef)
		}
	} else {
		switch item.Opt.Type() {
		case api.OptionBool:
			valPlainCols = 1
			v := m.getValue(item.Name)
			if b, ok := v.(bool); ok && b {
				val = bg(checkboxStyle).Render("●")
			} else {
				val = bg(checkboxEmptyStyle).Render("○")
			}
		case api.OptionString, api.OptionInt:
			fv := formatValue(m.getValue(item.Name))
			valPlainCols = lipgloss.Width(fv)
			val = bg(inputStyle).Render(fv)
		case api.OptionChoice:
			fv := fmt.Sprintf("◀ %s ▶", formatValue(m.getValue(item.Name)))
			valPlainCols = lipgloss.Width(fv)
			val = bg(dropdownStyle).Render(fv)
		}
	}

	namePad := fmt.Sprintf("%-*s", nameW, name)
	valPadCols := max(0, valW-valPlainCols)
	valRendered := val + space(valPadCols)

	prefixCols := 2 + lipgloss.Width(namePad) + 2 + max(valW, valPlainCols) + 2
	availDesc := m.optionsContentWidth() - prefixCols

	shownDesc := desc
	var descRendered string
	descCols := 0
	if m.editing && selected {
		if def := m.defaultFor(item.Name); def != nil {
			shownDesc = fmt.Sprintf("(default: %s)", formatValue(def))
		}
		if availDesc >= 12 {
			shownDesc = truncateDisplay(shownDesc, availDesc)
		}
		descCols = lipgloss.Width(shownDesc)
		descRendered = bg(optionDescStyle).Render(shownDesc)
	} else if isDerived && derivedOwner != "" {
		tagPlain := "[← " + shortPkgDisplayName(derivedOwner, m.localPkgSet) + "]"
		ownerTag := bg(derivedFromStyle).Render(tagPlain)
		if shownDesc != "" {
			rem := availDesc - lipgloss.Width(tagPlain) - 1
			if availDesc >= 14 && rem >= 4 {
				shownDesc = truncateDisplay(shownDesc, rem)
			}
			descCols = lipgloss.Width(tagPlain) + 1 + lipgloss.Width(shownDesc)
			descRendered = ownerTag + space(1) + bg(optionDescStyle).Render(shownDesc)
		} else {
			descCols = lipgloss.Width(tagPlain)
			descRendered = ownerTag
		}
	} else {
		if availDesc >= 12 {
			shownDesc = truncateDisplay(shownDesc, availDesc)
		}
		descCols = lipgloss.Width(shownDesc)
		descRendered = bg(optionDescStyle).Render(shownDesc)
	}

	var nameRendered string
	if selected {
		nameRendered = bg(selectedOptStyle).Render(namePad)
	} else {
		nameRendered = optionNameStyle.Render(namePad)
	}

	line := marker + space(1) + nameRendered + space(2) + valRendered + space(2) + descRendered
	if selected {
		tail := m.optionsContentWidth() - (prefixCols + descCols)
		if tail > 0 {
			line += space(tail)
		}
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

func truncateDisplay(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	var out strings.Builder
	width := 0
	for _, r := range s {
		runeWidth := lipgloss.Width(string(r))
		if width+runeWidth > max-1 {
			break
		}
		out.WriteRune(r)
		width += runeWidth
	}
	return out.String() + "…"
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
	def := m.defaultFor(m.choiceOpt)

	type kv struct{ k, v string }
	rows := []kv{
		{m.text(textName), m.choiceOpt},
		{m.text(textType), opt.Type().String()},
		{m.text(textDefault), formatValue(def)},
		{m.text(textCurrent), formatValue(cur)},
	}
	modified := m.isOptionModified(m.choiceOpt)
	status := m.text(textNo)
	if modified {
		status = modifiedMarkStyle.Render(m.text(textYes))
	}
	rows = append(rows, kv{m.text(textModifiedLabel), status})
	if m.ed().IsExplicit(m.selectedPkg, m.choiceOpt) {
		rows = append(rows, kv{m.text(textSource), explicitMarkStyle.Render(m.text(textExplicitSource))})
	} else if owner, ok := m.optionDerivedBy(m.selectedPkg, m.choiceOpt); ok && owner != "" {
		rows = append(rows, kv{m.text(textDerivedFrom), derivedFromStyle.Render(owner)})
	}

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
	} else if m.editing || m.descEditing {
		helpText = renderHelpEntries([]helpEntry{
			{"←→", m.text(textMoveCursor)}, {"Home/End", m.text(textJump)}, {"Backspace", m.text(textDelete)},
			{"Enter", m.text(textConfirm)}, {"Esc", m.text(textCancel)},
		})
	} else if m.focusArea == 0 {
		helpText = renderHelpEntries([]helpEntry{
			{"↑↓", m.text(textNavigate)}, {"[]", m.text(textStepDeps)}, {"{}", m.text(textCycleEdges)}, {"←→", m.text(textCollapseExpand)},
			{"/", m.text(textSearch)}, {"z/Z", m.text(textCollapseExpandAll)}, {"H", m.text(textHideEmpty)},
			{"Tab", m.text(textOptions)}, {"Ctrl+S", m.text(textSave)}, {"Esc", m.text(textCancel)},
		})
	} else {
		helpText = renderHelpEntries([]helpEntry{
			{"↑↓", m.text(textNavigate)}, {"←→", m.text(textCycleValue)}, {"Space/Enter", m.text(textEdit)},
			{"[]", m.text(textStepDeps)}, {"{}", m.text(textCycleEdges)}, {"r", m.text(textReset)}, {"R", m.text(textDefault)},
			{"?", m.text(textDetail)}, {"D", m.text(textDescriptionEdit)},
			{"Tab", m.text(textPackages)}, {"Ctrl+S", m.text(textSave)}, {"Esc", m.text(textBack)},
		})
	}

	if m.ed().HasChanges() {
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
