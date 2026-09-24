package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type dialogRect struct {
	x, y, width, height int
}

func (r dialogRect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.width && y >= r.y && y < r.y+r.height
}

type dialogTarget struct {
	bounds dialogRect
	index  int
}

type dialogLayout struct {
	view    string
	bounds  dialogRect
	targets []dialogTarget
}

func (m *Model) frameDialog(style lipgloss.Style, content string, targets []dialogTarget) dialogLayout {
	view := style.Render(content)
	width, height := lipgloss.Width(view), lipgloss.Height(view)
	x, y := max(0, (m.width-width)/2), max(0, (m.height-height)/2)
	for i := range targets {
		targets[i].bounds.x += x + style.GetBorderLeftSize() + style.GetPaddingLeft()
		targets[i].bounds.y += y + style.GetBorderTopSize() + style.GetPaddingTop()
	}
	return dialogLayout{view, dialogRect{x, y, width, height}, targets}
}

func (m *Model) dialogContentWidth(style lipgloss.Style, parts ...string) int {
	width := 1
	for _, part := range parts {
		width = max(width, lipgloss.Width(part))
	}
	if m.width > 0 {
		width = min(width, max(1, m.width-style.GetHorizontalFrameSize()-2))
	}
	return width
}

func dialogLines(text string, width int) []string {
	return strings.Split(ansi.Hardwrap(ansi.Wordwrap(text, width, ""), width, true), "\n")
}

func centerDialogLines(lines []string, width int) string {
	centered := make([]string, len(lines))
	for i, line := range lines {
		centered[i] = lipgloss.PlaceHorizontal(width, lipgloss.Center, line)
	}
	return strings.Join(centered, "\n")
}

func (m *Model) choiceDialogLayout() dialogLayout {
	titleText := m.choiceOpt
	current := fmt.Sprintf("%v", m.getValue(m.choiceOpt))
	description := ""
	if m.overlay == overlayLanguage {
		titleText = m.text(textLanguage)
		current = m.languageLabel()
	} else if opt := m.detailOption(); opt != nil {
		description = opt.Description()
	}
	style := overlayStyle
	if m.height > 0 && m.height < 12 {
		style = style.Padding(0, 2)
	}
	widthParts := []string{titleText, description, m.text(textChoiceHint)}
	for _, value := range m.choiceValues {
		widthParts = append(widthParts, "    "+value)
	}
	width := m.dialogContentWidth(style, widthParts...)
	title := confirmTitleStyle.Render(ansi.Truncate(titleText, width, "..."))
	hint := dialogLines(confirmMsgStyle.Render(m.text(textChoiceHint)), width)
	available := m.height - style.GetVerticalFrameSize()
	if m.height == 0 {
		available = len(m.choiceValues) + len(hint) + 5
	}
	minimumRows := min(3, max(1, len(m.choiceValues)))
	spaced := available >= 1+len(hint)+minimumRows+2
	gaps := 0
	if spaced {
		gaps = 2
	}
	prefix := []string{title}
	if description != "" {
		lines := dialogLines(optionDescStyle.Render(description), width)
		limit := max(0, available-1-len(hint)-minimumRows-gaps)
		if len(lines) > limit && limit > 0 {
			lines[limit-1] = ansi.Truncate(lines[limit-1], max(1, width-3), "") + "..."
		}
		prefix = append(prefix, lines[:min(len(lines), limit)]...)
	}
	if spaced {
		prefix = append(prefix, "")
	}
	suffix := hint
	if spaced {
		suffix = append([]string{""}, suffix...)
	}
	visible := max(1, available-len(prefix)-len(suffix))
	visible = min(visible, max(1, len(m.choiceValues)))
	m.choiceOff = clamp(m.choiceOff, 0, max(0, len(m.choiceValues)-visible))
	if m.choiceCursor < m.choiceOff {
		m.choiceOff = m.choiceCursor
	} else if m.choiceCursor >= m.choiceOff+visible {
		m.choiceOff = m.choiceCursor - visible + 1
	}
	listWidth := 1
	for _, value := range m.choiceValues {
		listWidth = max(listWidth, min(width, lipgloss.Width("    "+value)))
	}
	listX := (width - listWidth) / 2
	lines := append([]string(nil), prefix...)
	targets := make([]dialogTarget, 0, visible)
	for i := m.choiceOff; i < min(len(m.choiceValues), m.choiceOff+visible); i++ {
		marker := "  "
		if m.choiceValues[i] == current {
			marker = checkboxStyle.Render("● ")
		}
		line := ansi.Truncate(" "+marker+" "+m.choiceValues[i], listWidth, "...")
		line = lipgloss.PlaceHorizontal(listWidth, lipgloss.Left, line)
		if i == m.choiceCursor {
			line = selectedRowStyle.Render(line)
		}
		targets = append(targets, dialogTarget{dialogRect{listX, len(lines), listWidth, 1}, i})
		lines = append(lines, line)
	}
	lines = append(lines, suffix...)
	return m.frameDialog(style, centerDialogLines(lines, width), targets)
}

func (m *Model) confirmDialogLayout() dialogLayout {
	title := confirmTitleStyle.Render(m.text(textUnsavedChanges))
	message := confirmMsgStyle.Render(m.text(textSaveBeforeExit))
	hint := confirmMsgStyle.Render(m.text(textConfirmHint))
	buttons := []string{
		btnInactiveStyle.Render("  " + m.text(textSave) + "  "),
		btnInactiveStyle.Render("  " + m.text(textDiscard) + "  "),
	}
	if m.confirmBtn == 0 {
		buttons[0] = btnActiveStyle.Render("[ " + m.text(textSave) + " ]")
	} else {
		buttons[1] = btnActiveStyle.Render("[ " + m.text(textDiscard) + " ]")
	}
	buttonRow := lipgloss.JoinHorizontal(lipgloss.Top, buttons[0], "  ", buttons[1])
	style := confirmStyle
	width := m.dialogContentWidth(style, title, message, hint, buttonRow)
	parts := [][]string{dialogLines(title, width), dialogLines(message, width), {buttonRow}, dialogLines(hint, width)}
	baseHeight := 0
	for _, part := range parts {
		baseHeight += len(part)
	}
	if m.height > 0 && baseHeight+3+style.GetVerticalFrameSize() > m.height {
		style = style.Padding(0, 3)
	}
	gapCount := 3
	if m.height > 0 {
		gapCount = clamp(m.height-style.GetVerticalFrameSize()-baseHeight, 0, 3)
	}
	lines := make([]string, 0, baseHeight+gapCount)
	var targets []dialogTarget
	for i, part := range parts {
		if i == 2 {
			x := (width - lipgloss.Width(buttonRow)) / 2
			for j, button := range buttons {
				buttonWidth := lipgloss.Width(button)
				targets = append(targets, dialogTarget{dialogRect{x, len(lines), buttonWidth, 1}, j})
				x += buttonWidth + 2
			}
		}
		lines = append(lines, part...)
		if i < gapCount {
			lines = append(lines, "")
		}
	}
	return m.frameDialog(style, centerDialogLines(lines, width), targets)
}

func (m *Model) renderDetailDialog(content string) string {
	style := overlayStyle
	if m.height > 0 && m.height < 12 {
		style = style.Padding(0, 2)
	}
	closeHint := confirmMsgStyle.Render(m.text(textCloseDetails))
	width := m.dialogContentWidth(style, content, closeHint)
	lines := dialogLines(strings.TrimRight(content, "\n"), width)
	hint := dialogLines(closeHint, width)
	suffix := append([]string{""}, hint...)
	available := len(lines) + len(suffix)
	if m.height > 0 {
		available = max(1, m.height-style.GetVerticalFrameSize())
	}
	if len(lines)+len(suffix) > available {
		compact := make([]string, 0, len(lines))
		for _, line := range lines {
			if strings.TrimSpace(ansi.Strip(line)) != "" {
				compact = append(compact, line)
			}
		}
		lines = compact
		suffix = hint
		if len(lines)+len(suffix) > available {
			suffix = append(dialogLines(confirmMsgStyle.Render(m.text(textDetailScrollHint)), width), hint...)
		}
	}
	m.detailTotal = len(lines)
	m.detailRows = min(len(lines), max(1, available-len(suffix)))
	m.detailOff = clamp(m.detailOff, 0, max(0, m.detailTotal-m.detailRows))
	visible := append([]string(nil), lines[m.detailOff:m.detailOff+m.detailRows]...)
	visible = append(visible, suffix...)
	for i, line := range visible {
		visible[i] = lipgloss.PlaceHorizontal(width, lipgloss.Left, line)
	}
	return style.Render(strings.Join(visible, "\n"))
}

func (m *Model) scrollDetail(key string) {
	m.renderDetailOverlay()
	switch key {
	case "up", "k":
		m.detailOff--
	case "down", "j":
		m.detailOff++
	case "pgup":
		m.detailOff -= m.detailRows
	case "pgdown":
		m.detailOff += m.detailRows
	case "home", "g":
		m.detailOff = 0
	case "end", "G":
		m.detailOff = m.detailTotal - m.detailRows
	}
	m.detailOff = clamp(m.detailOff, 0, max(0, m.detailTotal-m.detailRows))
}

func (m *Model) handleOverlayMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	delta := mouseWheel(msg)
	if !mouseLeftPress(msg) && delta == 0 {
		return m, nil
	}
	var layout dialogLayout
	if m.overlay == overlayChoice || m.overlay == overlayLanguage {
		layout = m.choiceDialogLayout()
	} else {
		view := m.renderDetailOverlay()
		width, height := lipgloss.Width(view), lipgloss.Height(view)
		layout.bounds = dialogRect{max(0, (m.width-width)/2), max(0, (m.height-height)/2), width, height}
	}
	if !layout.bounds.contains(msg.X, msg.Y) {
		if mouseLeftPress(msg) {
			return m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEsc})
		}
		return m, nil
	}
	if delta != 0 {
		if m.overlay == overlayChoice || m.overlay == overlayLanguage || m.overlay == overlayDetail {
			key := tea.KeyDown
			if delta < 0 {
				key = tea.KeyUp
			}
			return m.handleOverlayKey(tea.KeyMsg{Type: key})
		}
		return m, nil
	}
	for _, target := range layout.targets {
		if target.bounds.contains(msg.X, msg.Y) {
			m.choiceCursor = target.index
			return m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return m, nil
}

func (m *Model) handleConfirmMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !mouseLeftPress(msg) {
		return m, nil
	}
	layout := m.confirmDialogLayout()
	if !layout.bounds.contains(msg.X, msg.Y) {
		return m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEsc})
	}
	for _, target := range layout.targets {
		if target.bounds.contains(msg.X, msg.Y) {
			m.confirmBtn = target.index
			return m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return m, nil
}
