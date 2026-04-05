package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
			return m, nil
		}
		m.saved = false
		return m, tea.Quit
	case "ctrl+s", "s":
		m.saved = true
		return m, tea.Quit
	case "esc":
		if m.focusArea == 0 {
			if m.hasChanges {
				m.confirmQuit = true
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
		}
	case "down", "j":
		if m.treeCursor < len(m.flat)-1 {
			m.treeCursor++
			m.selectCurrentNode()
		}
	case "left", "h":
		if m.treeCursor < len(m.flat) && m.flat[m.treeCursor].Expanded {
			m.flat[m.treeCursor].Expanded = false
			m.flat = flattenTree(m.tree)
		}
	case "right", "l":
		if m.treeCursor < len(m.flat) && len(m.flat[m.treeCursor].Children) > 0 {
			m.flat[m.treeCursor].Expanded = true
			m.flat = flattenTree(m.tree)
		}
	case "enter":
		if m.treeCursor < len(m.flat) && len(m.flat[m.treeCursor].Children) > 0 {
			m.flat[m.treeCursor].Expanded = !m.flat[m.treeCursor].Expanded
			m.flat = flattenTree(m.tree)
		}
	}
	return m, nil
}

func (m *Model) selectCurrentNode() {
	if m.treeCursor < len(m.flat) && m.flat[m.treeCursor].PkgName != "" {
		m.selectedPkg = m.flat[m.treeCursor].PkgName
		m.buildOptionItems()
		m.optCursor = 0
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
		maxCursor := presetIdx
		if m.hasPresets() {
			maxCursor = presetIdx + 1
		}
		if m.hasKConfig() {
			maxCursor++
		}
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
			fmt.Sscanf(m.editInput, "%d", &val)
			m.setValue(item.Name, val)
		case api.OptionChoice:
			if m.editIdx < len(m.editChoices) {
				m.setValue(item.Name, m.editChoices[m.editIdx])
			}
		}
		return m, nil
	case "backspace":
		if item.Opt.Type() == api.OptionString || item.Opt.Type() == api.OptionInt {
			if len(m.editInput) > 0 {
				m.editInput = m.editInput[:len(m.editInput)-1]
			}
		}
	case "up", "k":
		if item.Opt.Type() == api.OptionChoice {
			if m.editIdx > 0 {
				m.editIdx--
			} else {
				m.editIdx = len(m.editChoices) - 1
			}
		}
	case "down", "j":
		if item.Opt.Type() == api.OptionChoice {
			if m.editIdx < len(m.editChoices)-1 {
				m.editIdx++
			} else {
				m.editIdx = 0
			}
		}
	default:
		if item.Opt.Type() == api.OptionString || item.Opt.Type() == api.OptionInt {
			m.editInput += msg.String()
		}
	}
	return m, nil
}

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}

	title := titleStyle.Render("VMake Configuration")
	tree := m.renderTree()
	options := m.renderOptions()
	help := m.renderHelp()

	main := lipgloss.JoinHorizontal(
		lipgloss.Top,
		treeStyle.Render(tree),
		lipgloss.NewStyle().Padding(1, 2).Render(options),
	)

	if m.confirmQuit {
		confirmBar := confirmStyle.Render("You have unsaved changes. Save before exit? (Y/N/Esc)")
		return lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			main,
			confirmBar,
		)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		main,
		help,
	)
}

func (m *Model) renderTree() string {
	var b strings.Builder

	for i, node := range m.flat {
		prefix := strings.Repeat("  ", node.Depth)

		icon := " "
		if node.PkgName == GlobalPkgName {
			icon = "⚙️"
		} else if len(node.Children) > 0 {
			if node.Expanded {
				icon = "▼"
			} else {
				icon = "▶"
			}
		}

		line := prefix + node.Prefix + icon + " " + node.Name
		if i == m.treeCursor && m.focusArea == 0 {
			b.WriteString(treeSelectedStyle.Render(line) + "\n")
		} else {
			b.WriteString(treeItemStyle.Render(line) + "\n")
		}
	}

	return b.String()
}

func (m *Model) renderOptions() string {
	var b strings.Builder

	if m.selectedPkg == "" {
		return "Select a package"
	}

	if m.runningMenuconfig {
		return "\n  Running menuconfig for " + m.selectedPkg + "...\n"
	}

	visible := m.visibleOptions()
	hasKConfig := m.hasKConfig()

	if len(visible) == 0 && !hasKConfig {
		return "No options"
	}

	currentGroup := ""
	for i, item := range visible {
		if item.Group != currentGroup {
			currentGroup = item.Group
			b.WriteString(groupStyle.Render("── "+currentGroup+" ──") + "\n")
		}

		line := m.renderOption(item, i == m.optCursor)
		b.WriteString(line + "\n")
	}

	if hasKConfig {
		presetIdx := len(visible)
		if m.hasPresets() {
			b.WriteString("\n")
			presetName := m.currentPreset()
			selected := m.optCursor == presetIdx
			name := optionNameStyle.Render("preset")
			if selected {
				name = selectedOptStyle.Render("preset")
			}
			val := dropdownStyle.Render(fmt.Sprintf("[%s]", presetName))
			desc := optionDescStyle.Render("← →: select preset")
			b.WriteString(fmt.Sprintf("  %s %s %s", name, val, desc) + "\n")
		}
		b.WriteString("\n")
		menuconfigIdx := presetIdx
		if m.hasPresets() {
			menuconfigIdx = presetIdx + 1
		}
		selected := m.optCursor == menuconfigIdx
		name := optionNameStyle.Render("menuconfig")
		if selected {
			name = selectedOptStyle.Render("menuconfig")
		}
		desc := optionDescStyle.Render("Enter: run menuconfig")
		b.WriteString(fmt.Sprintf("  %s %s", name, desc) + "\n")
	}

	return b.String()
}

func (m *Model) renderOption(item OptionItem, selected bool) string {
	name := optionNameStyle.Render(item.Name)
	desc := optionDescStyle.Render(item.Opt.Description())

	var val string
	if m.editing && selected {
		switch item.Opt.Type() {
		case api.OptionString, api.OptionInt:
			val = inputStyle.Render("[" + m.editInput + "_]")
		case api.OptionChoice:
			if m.editIdx < len(m.editChoices) {
				val = dropdownStyle.Render("[" + m.editChoices[m.editIdx] + "]")
			}
		}
	} else {
		switch item.Opt.Type() {
		case api.OptionBool:
			v := m.getValue(item.Name)
			if b, ok := v.(bool); ok && b {
				val = checkboxStyle.Render("[x]")
			} else {
				val = checkboxEmptyStyle.Render("[ ]")
			}
		case api.OptionString, api.OptionInt:
			val = inputStyle.Render(fmt.Sprintf("%v", m.getValue(item.Name)))
		case api.OptionChoice:
			val = dropdownStyle.Render(fmt.Sprintf("[%s]", m.getValue(item.Name)))
		}
	}

	if selected && !m.editing {
		name = selectedOptStyle.Render(item.Name)
	}

	return fmt.Sprintf("  %s %s %s", name, val, desc)
}

func renderHelp(base string, modified bool) string {
	if modified {
		base += " " + modifiedStyle.Render("[Modified]")
	}
	return helpStyle.Render(base)
}

func (m *Model) renderHelp() string {
	if m.editing {
		visible := m.visibleOptions()
		if m.optCursor < len(visible) {
			item := visible[m.optCursor]
			if item.Opt.Type() == api.OptionChoice {
				return renderHelp("↑↓: select | Enter: confirm | Esc: cancel", m.hasChanges)
			}
		}
		return renderHelp("Enter: confirm | Esc: cancel", m.hasChanges)
	}
	if m.focusArea == 0 {
		return renderHelp("↑↓: navigate | ←→: collapse/expand | Tab: options | Enter: expand | S: Save | Esc: Cancel", m.hasChanges)
	}
	helpText := "↑↓: navigate | ←→: cycle value | Space/Enter: edit | Tab: tree | Esc: back"
	return renderHelp(helpText, m.hasChanges)
}
