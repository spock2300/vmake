package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorPrimary   = lipgloss.Color("#7D56F4")
	colorAccent    = lipgloss.Color("#00D4AA")
	colorSuccess   = lipgloss.Color("#04B575")
	colorWarning   = lipgloss.Color("#FF8C00")
	colorDanger    = lipgloss.Color("#FF6B6B")
	colorMuted     = lipgloss.Color("#626262")
	colorDimBorder = lipgloss.Color("#3C3C3C")
	colorHighlight = lipgloss.Color("#2A2A3C")
	colorOverlay   = lipgloss.Color("#1E1E2E")
	colorInput     = lipgloss.Color("#F25D94")
	colorDropdown  = lipgloss.Color("#00D9FF")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			Padding(0, 1)

	titlePathStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	groupStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorMuted)

	optionNameStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	optionDescStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	selectedOptStyle = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)

	selectedRowStyle = lipgloss.NewStyle().
				Background(colorHighlight)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	helpSepStyle = lipgloss.NewStyle().
			Foreground(colorDimBorder)

	checkboxStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	checkboxEmptyStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	inputStyle = lipgloss.NewStyle().
			Foreground(colorInput)

	dropdownStyle = lipgloss.NewStyle().
			Foreground(colorDropdown)

	externalPkgStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	confirmStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDanger).
			Padding(1, 3)

	confirmTitleStyle = lipgloss.NewStyle().
				Foreground(colorDanger).
				Bold(true)

	confirmMsgStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	btnActiveStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	btnInactiveStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	modifiedBadgeStyle = lipgloss.NewStyle().
				Foreground(colorWarning).
				Bold(true)

	scrollIndicatorStyle = lipgloss.NewStyle().
				Foreground(colorMuted)
)

func treePanelStyle(focused bool, width int) lipgloss.Style {
	borderColor := colorDimBorder
	if focused {
		borderColor = colorAccent
	}
	return lipgloss.NewStyle().
		Width(width).
		Padding(0, 1).
		BorderForeground(borderColor)
}

func optionsPanelStyle(focused bool, width int) lipgloss.Style {
	borderColor := colorDimBorder
	if focused {
		borderColor = colorAccent
	}
	return lipgloss.NewStyle().
		Width(width).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder(), false, false, false, true).
		BorderForeground(borderColor)
}

func headerBorderStyle(focused bool) lipgloss.Style {
	borderColor := colorDimBorder
	if focused {
		borderColor = colorAccent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(borderColor).
		Padding(0, 1)
}

func footerBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(colorDimBorder).
		Padding(0, 1)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}
