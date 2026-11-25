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

	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

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

	languageSelectorStyle = lipgloss.NewStyle().
				Foreground(colorDropdown).
				Bold(true)

	inactivePkgStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	confirmStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDanger).
			Padding(1, 3)

	confirmTitleStyle = lipgloss.NewStyle().
				Foreground(colorDanger).
				Bold(true)

	overlayStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(1, 2)

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

	countBadgeStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	modifiedMarkStyle = lipgloss.NewStyle().
				Foreground(colorWarning).
				Bold(true)

	derivedMarkStyle = lipgloss.NewStyle().
				Foreground(colorDropdown).
				Bold(true)

	derivedFromStyle = lipgloss.NewStyle().
				Foreground(colorDropdown)

	explicitMarkStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	egoSubheaderStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	filterBoxActiveStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	filterBoxAppliedStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	trackFocalStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	trackUpstreamActiveStyle = lipgloss.NewStyle().
					Foreground(colorDropdown).
					Bold(true)

	trackDownstreamStyle = lipgloss.NewStyle().
				Foreground(colorSuccess)

	trackUpstreamInactiveStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#3D7A8A"))

	trackDownstreamInactiveStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#3B7A62"))

	treeBranchStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4E5166"))

	treeActiveLocalIconStyle = lipgloss.NewStyle().
					Foreground(colorSuccess)

	treeActiveRemoteIconStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#00A8CC"))

	treeActiveRemoteNameStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#BAC2DE"))

	treeGroupIconStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#BAC2DE"))

	treeActiveNoOptIconStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#5E9E8A"))

	treeActiveNoOptNameStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#A6ADC8"))

	treeInactiveIconStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#585B70"))

	canvasFocalHexStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#9D65FF")).
				Bold(true)
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

func egoGraphPanelStyle(focused bool, width int) lipgloss.Style {
	borderColor := colorDimBorder
	if focused {
		borderColor = colorAccent
	}
	return lipgloss.NewStyle().
		Width(width).
		Padding(0, 1).
		Border(lipgloss.NormalBorder(), false, false, true, true).
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
