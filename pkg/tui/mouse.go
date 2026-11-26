package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/spock2300/vmake/pkg/api"
)

type panelRowTarget struct {
	y      int
	height int
	index  int
}

type optionLine struct {
	text       string
	row        optionRow
	editCursor bool
}

func (m *Model) treePanelWidth() int {
	if m.width <= 0 {
		return m.treeWidth
	}
	return min(m.treeWidth, max(10, m.width/2-1))
}

func (m *Model) optionsPanelWidth() int {
	return max(1, m.width-m.treePanelWidth()-5)
}

func (m *Model) optionsContentWidth() int {
	style := optionsPanelStyle(m.focusArea == 1, m.optionsPanelWidth())
	return max(1, style.GetWidth()-style.GetPaddingLeft()-style.GetPaddingRight())
}

func mouseLeftPress(msg tea.MouseMsg) bool {
	return msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress
}

func mouseWheel(msg tea.MouseMsg) int {
	if msg.Action != tea.MouseActionPress {
		return 0
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return -1
	case tea.MouseButtonWheelDown:
		return 1
	default:
		return 0
	}
}

func (m *Model) handleEditingMouse(msg tea.MouseMsg, s screen) (tea.Model, tea.Cmd) {
	if !mouseLeftPress(msg) {
		return m, nil
	}
	index, ok := s.optionRowAt(msg.X, msg.Y)
	if !ok || index != m.optCursor {
		return m.handleEditKey(tea.KeyMsg{Type: tea.KeyEsc})
	}
	return m, nil
}

func (m *Model) handleDescriptionMouse(msg tea.MouseMsg, s screen) (tea.Model, tea.Cmd) {
	if !mouseLeftPress(msg) {
		return m, nil
	}
	if !s.descRow.contains(msg.X, msg.Y) {
		m.descEditing = false
	}
	return m, nil
}

func (m *Model) optionRows() ([]optionRow, int, int) {
	layout := m.optionRowIndices()
	visible := layout.visible
	nameWidth, valWidth := 0, 0
	for _, item := range visible {
		if w := lipgloss.Width(item.Name); w > nameWidth {
			nameWidth = w
		}
		vw := lipgloss.Width(formatValue(m.getValue(item.Name)))
		if item.Opt.Type() == api.OptionChoice {
			vw += 4
		}
		if vw > valWidth {
			valWidth = vw
		}
	}
	nameWidth = min(nameWidth, 20)
	maxValW := 16
	if m.optionsContentWidth() >= 64 {
		maxValW = 24
	}
	valWidth = min(valWidth, maxValW)

	rows := make([]optionRow, 0, layout.total+2)
	group := ""
	for i, item := range visible {
		if item.Group != group {
			group = item.Group
			rows = append(rows, optionRow{kind: rowGroup, navIdx: -1, text: group})
		}
		rows = append(rows, optionRow{kind: rowOption, navIdx: i, item: item})
	}
	if layout.advToggleIdx >= 0 {
		rows = append(rows, optionRow{kind: rowAdvancedToggle, navIdx: layout.advToggleIdx})
	}
	if layout.presetIdx >= 0 {
		rows = append(rows, optionRow{kind: rowPreset, navIdx: layout.presetIdx})
	}
	if layout.menuconfigIdx >= 0 {
		rows = append(rows, optionRow{kind: rowMenuconfig, navIdx: layout.menuconfigIdx})
	}
	return rows, nameWidth, valWidth
}

func (m *Model) handleMouseOptionClick(msg tea.MouseMsg, s screen) (tea.Model, tea.Cmd) {
	index, ok := s.optionRowAt(msg.X, msg.Y)
	if !ok {
		return m, nil
	}
	m.optCursor = index
	return m.handleOptionsKey(tea.KeyMsg{Type: tea.KeyEnter})
}

func (m *Model) optionLines() []optionLine {
	rows, nameWidth, valWidth := m.optionRows()
	var lines []optionLine
	for _, row := range rows {
		var text string
		selected := row.navIdx == m.optCursor && m.focusArea == 1
		switch row.kind {
		case rowGroup:
			group := row.text
			if group == "General" {
				group = m.text(textGeneral)
			} else if group == "Global" {
				group = m.text(textGlobal)
			} else if group == "Advanced" {
				group = m.text(textAdvancedGroup)
			}
			text = groupStyle.Render("── " + group + " " + strings.Repeat("─", max(nameWidth+valWidth+2-lipgloss.Width(group), 3)))
		case rowOption:
			text = m.renderOptionAligned(row.item, selected, nameWidth, valWidth)
		case rowAdvancedToggle:
			text = m.renderAdvancedToggleRow(selected)
		case rowPreset:
			text = m.renderPresetRow(selected, nameWidth, valWidth)
		case rowMenuconfig:
			text = m.renderMenuconfigRow(selected, nameWidth)
		}
		text = lipgloss.NewStyle().Width(m.optionsContentWidth()).Render(text)
		cursorMarker := -1
		if row.kind == rowOption && selected && m.editing {
			cursor := clamp(m.editCursor, 0, len(m.editInput))
			cursorMarker = strings.Count(row.item.Name+m.editInput[:cursor], "▎")
		}
		for _, line := range strings.Split(text, "\n") {
			markers := strings.Count(line, "▎")
			lines = append(lines, optionLine{text: line, row: row, editCursor: cursorMarker >= 0 && markers > cursorMarker})
			cursorMarker -= markers
		}
	}
	return lines
}
