package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	treeStyle = lipgloss.NewStyle().
			Width(36).
			Padding(1, 1).
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(lipgloss.Color("#3C3C3C"))

	treeItemStyle = lipgloss.NewStyle().
			PaddingLeft(1)

	treeSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7D56F4")).
				Bold(true).
				PaddingLeft(1)

	groupStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#626262")).
			MarginTop(1).
			MarginBottom(0)

	optionNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575"))

	optionDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))

	selectedOptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7D56F4")).
				Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			Padding(1, 1)

	checkboxStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575"))

	checkboxEmptyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#626262"))

	inputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F25D94"))

	dropdownStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00D9FF"))

	confirmStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B")).
			Background(lipgloss.Color("#2D2D2D")).
			Bold(true).
			Padding(1, 2)

	modifiedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B")).
			Bold(true)
)
