package tui

import (
	"fmt"
	"strings"

	"gitee.com/spock2300/vmake/pkg/api"
	"gitee.com/spock2300/vmake/pkg/buildscript"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ConfigResult struct {
	Saved        bool
	Values       map[string]map[string]any
	Toolchain    string
	GlobalValues map[string]any
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
) (*ConfigResult, error) {
	m := NewModel(packages, deps, options, values, workDir, currentToolchain, globalOptions, globalValues)
	p := tea.NewProgram(&m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return nil, err
	}
	return &ConfigResult{
		Saved:        m.saved,
		Values:       m.values,
		Toolchain:    getToolchainValue(m.globalValues),
		GlobalValues: m.globalValues,
	}, nil
}

func getToolchainValue(globalValues map[string]any) string {
	if tc, ok := globalValues["toolchain"].(string); ok {
		return tc
	}
	return ""
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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

	switch msg.String() {
	case "ctrl+c":
		if m.hasChanges {
			m.confirmQuit = true
			return m, nil
		}
		m.saved = false
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
		m.focusArea = (m.focusArea + 1) % 2
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
		m.saved = true
		return m, tea.Quit
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

	switch msg.String() {
	case "up", "k":
		if m.optCursor > 0 {
			m.optCursor--
		}
	case "down", "j":
		if m.optCursor < len(visible)-1 {
			m.optCursor++
		}
	case " ":
		if m.optCursor < len(visible) {
			item := visible[m.optCursor]
			if item.Opt.Type() == api.OptionBool {
				current := m.getValue(item.Name)
				if b, ok := current.(bool); ok {
					m.setValue(item.Name, !b)
				} else {
					m.setValue(item.Name, true)
				}
			}
		}
	case "enter":
		if m.optCursor < len(visible) {
			item := visible[m.optCursor]
			switch item.Opt.Type() {
			case api.OptionString, api.OptionInt:
				m.editing = true
				m.editInput = fmt.Sprintf("%v", m.getValue(item.Name))
			case api.OptionChoice:
				m.editing = true
				m.editChoices = item.Opt.Values()
				current := fmt.Sprintf("%v", m.getValue(item.Name))
				m.editIdx = 0
				for i, v := range m.editChoices {
					if v == current {
						m.editIdx = i
						break
					}
				}
			}
		}
	}
	return m, nil
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
		if item.Opt.Type() == api.OptionChoice && m.editIdx > 0 {
			m.editIdx--
		}
	case "down", "j":
		if item.Opt.Type() == api.OptionChoice && m.editIdx < len(m.editChoices)-1 {
			m.editIdx++
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

	visible := m.visibleOptions()
	if len(visible) == 0 {
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

func (m *Model) renderHelp() string {
	if m.editing {
		visible := m.visibleOptions()
		if m.optCursor < len(visible) {
			item := visible[m.optCursor]
			if item.Opt.Type() == api.OptionChoice {
				base := "↑↓: select | Enter: confirm | Esc: cancel"
				if m.hasChanges {
					return helpStyle.Render(base + " " + modifiedStyle.Render("[Modified]"))
				}
				return helpStyle.Render(base)
			}
		}
		base := "Enter: confirm | Esc: cancel"
		if m.hasChanges {
			return helpStyle.Render(base + " " + modifiedStyle.Render("[Modified]"))
		}
		return helpStyle.Render(base)
	}
	if m.focusArea == 0 {
		base := "↑↓: navigate | ←→: collapse/expand | Tab: options | Enter: Save | Esc: Cancel"
		if m.hasChanges {
			return helpStyle.Render(base + " " + modifiedStyle.Render("[Modified]"))
		}
		return helpStyle.Render(base)
	}
	base := "↑↓: navigate | Space: toggle | Enter: edit | Tab: tree | Esc: back"
	if m.hasChanges {
		return helpStyle.Render(base + " " + modifiedStyle.Render("[Modified]"))
	}
	return helpStyle.Render(base)
}
