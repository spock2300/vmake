package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func (m *Model) handleEditingMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !mouseLeftPress(msg) {
		return m, nil
	}
	index, ok := m.optionRowAt(msg.X, msg.Y)
	if !ok || index != m.optCursor {
		return m.handleEditKey(tea.KeyMsg{Type: tea.KeyEsc})
	}
	return m, nil
}

func (m *Model) optionRows() ([]optionRow, int, int) {
	visible := m.visibleOptions()
	nameWidth, valWidth := 0, 0
	for _, item := range visible {
		if w := lipgloss.Width(item.Name); w > nameWidth {
			nameWidth = w
		}
		if w := lipgloss.Width(fmt.Sprintf("%v", m.getValue(item.Name))); w > valWidth {
			valWidth = w
		}
	}
	nameWidth = min(nameWidth, 20)
	valWidth = min(valWidth, 16)

	rows := make([]optionRow, 0, len(visible)+2)
	navIdx := 0
	group := ""
	for _, item := range visible {
		if item.Group != group {
			group = item.Group
			rows = append(rows, optionRow{kind: rowGroup, navIdx: -1, text: group})
		}
		rows = append(rows, optionRow{kind: rowOption, navIdx: navIdx, item: item})
		navIdx++
	}

	if m.hasKConfig() {
		if m.hasPresets() {
			rows = append(rows, optionRow{kind: rowPreset, navIdx: navIdx})
			navIdx++
		}
		rows = append(rows, optionRow{kind: rowMenuconfig, navIdx: navIdx})
	}
	return rows, nameWidth, valWidth
}

func (m *Model) optionRowAt(x, y int) (int, bool) {
	style := optionsPanelStyle(m.focusArea == 1, m.optionsPanelWidth())
	left := m.renderedTreeW + style.GetBorderLeftSize()
	if x < left || x >= left+style.GetWidth() {
		return 0, false
	}
	for _, target := range m.optionMouseRows {
		if y-m.headerHeight() >= target.y && y-m.headerHeight() < target.y+target.height {
			return target.index, target.index >= 0
		}
	}
	return 0, false
}

func (m *Model) handleMouseOptionClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	index, ok := m.optionRowAt(msg.X, msg.Y)
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
			}
			text = groupStyle.Render("── " + group + " " + strings.Repeat("─", max(nameWidth+valWidth+2-lipgloss.Width(group), 3)))
		case rowOption:
			text = m.renderOptionAligned(row.item, selected, nameWidth, valWidth)
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
