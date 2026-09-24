package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func renderedLanguageSelector(t *testing.T, view, selector string) (int, int, int) {
	t.Helper()
	for y, line := range strings.Split(stripAnsi(view), "\n") {
		if i := strings.Index(line, selector); i >= 0 {
			return lipgloss.Width(line[:i]), y, lipgloss.Width(selector)
		}
	}
	t.Fatalf("view missing intact language selector %q:\n%s", selector, stripAnsi(view))
	return 0, 0, 0
}

func TestLanguageSelectorHitboxMatchesRenderedControl(t *testing.T) {
	for _, lang := range []struct {
		language language
		selector string
	}{
		{languageEnglish, "[Language: English ▼]"},
		{languageChinese, "[语言: 中文 ▼]"},
	} {
		for _, dir := range []string{
			"",
			"/work",
			"/home/user/projects/very/deeply/nested/firmware/build/working/directory",
			"/home/用户/项目/嵌入式固件/工作目录",
		} {
			for _, modified := range []bool{false, true} {
				t.Run(fmt.Sprintf("language=%d/dir=%s/modified=%t", lang.language, dir, modified), func(t *testing.T) {
					m := newLanguageTestModel()
					m.language = lang.language
					m.workDir = dir
					if modified {
						m.setValue("enabled", true)
					}
					for _, width := range []int{80, 50, 120, 60} {
						m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
						view := m.View()
						x, y, w := renderedLanguageSelector(t, view, lang.selector)
						if y != 0 {
							t.Fatalf("width=%d: selector moved from top row to %d", width, y)
						}
						lines := strings.Split(stripAnsi(view), "\n")
						for row := 0; row < m.headerHeight(); row++ {
							if got := lipgloss.Width(lines[row]); got > width {
								t.Fatalf("header row width=%d exceeds terminal width=%d", got, width)
							}
							for _, col := range []int{x - 1, x, x + w - 1, x + w} {
								m.handleMouse(tea.MouseMsg{X: col, Y: row, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
								wantOpen := row == y && col >= x && col < x+w
								if gotOpen := m.overlay == overlayLanguage; gotOpen != wantOpen {
									t.Fatalf("width=%d: click (%d,%d) opened=%t, want %t; rendered=(%d,%d,%d), model=(%d,%d,%d)", width, col, row, gotOpen, wantOpen, x, y, w, m.languageSelectorX, m.languageSelectorY, m.languageSelectorW)
								}
								m.closeOverlay()
							}
						}
					}
				})
			}
		}
	}
}
