package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/spock2300/vmake/pkg/api"
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
						for row := 0; row < m.layout().headerH; row++ {
							if got := lipgloss.Width(lines[row]); got > width {
								t.Fatalf("header row width=%d exceeds terminal width=%d", got, width)
							}
							for _, col := range []int{x - 1, x, x + w - 1, x + w} {
								m.handleMouse(tea.MouseMsg{X: col, Y: row, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
								wantOpen := row == y && col >= x && col < x+w
								if gotOpen := m.overlay == overlayLanguage; gotOpen != wantOpen {
									t.Fatalf("width=%d: click (%d,%d) opened=%t, want %t; rendered=(%d,%d,%d), model=(%d,%d,%d)", width, col, row, gotOpen, wantOpen, x, y, w, m.layout().languageSelector.x, m.layout().languageSelector.y, m.layout().languageSelector.width)
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

func TestAdvancedOptionsMouseControlsHitboxes(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {
			"basic": mkOpt("basic", api.OptionBool, false),
			"adv_1": mkOpt("adv_1", api.OptionInt, 16).SetAdvanced(true),
			"adv_2": mkOpt("adv_2", api.OptionBool, true).SetAdvanced(true),
		},
		"advonly": {
			"only_adv": mkOpt("only_adv", api.OptionBool, false).SetAdvanced(true),
		},
	}

	for _, tc := range []struct {
		name      string
		lang      language
		hdrOff    string
		hdrOn     string
		langSel   string
		optOff    string
		optOn     string
		rowShow   string
		rowHide   string
	}{
		{
			name:    "en",
			lang:    languageEnglish,
			hdrOff:  "[○ All Options]",
			hdrOn:   "[● All Options]",
			langSel: "[Language: English ▼]",
			optOff:  "[▸ +2 adv]",
			optOn:   "[▾ hide adv]",
			rowShow: "▸ Show all advanced options",
			rowHide: "▾ Hide advanced options",
		},
		{
			name:    "zh",
			lang:    languageChinese,
			hdrOff:  "[○ 全部选项]",
			hdrOn:   "[● 全部选项]",
			langSel: "[语言: 中文 ▼]",
			optOff:  "[▸ +2 高级]",
			optOn:   "[▾ 折叠高级]",
			rowShow: "▸ 展开高级选项",
			rowHide: "▾ 折叠高级选项",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := map[string][]string{"app": {"advonly"}}
			m := testModel(mkSources("app", "advonly"), deps, opts, map[string]map[string]any{}, "/work", "default", nil, nil, nil)
			m.language = tc.lang
			m.jumpToMatch("app", "")
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			if m.layout().graphH <= 0 {
				t.Fatalf("expected ego graph above options panel to exercise non-zero optionsTopY")
			}

			// 1. Header toggle button pixel-exact click + language selector right beside it.
			view := m.View()
			hx, hy, hw := renderedLanguageSelector(t, view, tc.hdrOff)
			lx, ly, lw := renderedLanguageSelector(t, view, tc.langSel)
			if got := m.layout().advancedToggle; got.x != hx || got.y != hy || got.width != hw {
				t.Fatalf("header advancedToggle hitbox=%+v, rendered=(%d,%d,%d)", got, hx, hy, hw)
			}
			if got := m.layout().languageSelector; got.x != lx || got.y != ly || got.width != lw {
				t.Fatalf("header languageSelector hitbox=%+v, rendered=(%d,%d,%d)", got, lx, ly, lw)
			}
			m.handleMouse(tea.MouseMsg{X: hx, Y: hy, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if !m.showAdvanced {
				t.Fatal("clicking header toggle must enable showAdvanced")
			}
			view = m.View()
			hx, hy, _ = renderedLanguageSelector(t, view, tc.hdrOn)
			m.handleMouse(tea.MouseMsg{X: hx, Y: hy, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if m.showAdvanced {
				t.Fatal("clicking header toggle again must disable showAdvanced")
			}

			// 2. Options panel title bar clickable badge.
			view = m.View()
			ox, oy, ow := renderedLanguageSelector(t, view, tc.optOff)
			if got := m.layout().optAdvancedToggle; got.x != ox || got.y != oy || got.width != ow {
				t.Fatalf("options title toggle hitbox=%+v, rendered=(%d,%d,%d)", got, ox, oy, ow)
			}
			m.handleMouse(tea.MouseMsg{X: ox, Y: oy, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if !m.showAdvanced {
				t.Fatal("clicking options title badge must enable showAdvanced")
			}
			view = m.View()
			ox, oy, _ = renderedLanguageSelector(t, view, tc.optOn)
			m.handleMouse(tea.MouseMsg{X: ox, Y: oy, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if m.showAdvanced {
				t.Fatal("clicking options title badge again must disable showAdvanced")
			}

			// 3. In-list collapsible row click (including package with 0 basic options).
			m.jumpToMatch("advonly", "")
			view = m.View()
			rx, ry, _ := renderedLanguageSelector(t, view, tc.rowShow)
			m.handleMouse(tea.MouseMsg{X: rx, Y: ry, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if !m.showAdvanced {
				t.Fatal("clicking in-list Show advanced row must enable showAdvanced")
			}
			view = m.View()
			if !strings.Contains(stripAnsi(view), "only_adv") {
				t.Fatalf("expanded advonly view missing only_adv:\n%s", stripAnsi(view))
			}
			rx, ry, _ = renderedLanguageSelector(t, view, tc.rowHide)
			m.handleMouse(tea.MouseMsg{X: rx, Y: ry, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if m.showAdvanced {
				t.Fatal("clicking in-list Hide advanced row must disable showAdvanced")
			}
		})
	}
}
