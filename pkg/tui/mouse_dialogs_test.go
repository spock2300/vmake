package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/spock2300/vmake/pkg/api"
)

func dialogTextPosition(t *testing.T, view, text string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(stripAnsi(view), "\n") {
		if index := strings.Index(line, text); index >= 0 {
			return lipgloss.Width(line[:index]), y
		}
	}
	t.Fatalf("missing %q in dialog:\n%s", text, stripAnsi(view))
	return 0, 0
}

func dialogMousePress(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
}

func TestDialogMouseChoiceViewport(t *testing.T) {
	for _, lang := range []language{languageEnglish, languageChinese} {
		for _, size := range [][2]int{{100, 30}, {60, 8}, {50, 9}, {51, 12}} {
			t.Run(fmt.Sprintf("language_%d_%dx%d", lang, size[0], size[1]), func(t *testing.T) {
				m := newLanguageTestModel()
				m.language, m.width, m.height = lang, size[0], size[1]
				m.options["app"]["level"].SetDescription(strings.Repeat("很长的中文说明 and wrapped description ", 6))
				values := make([]string, 20)
				for i := range values {
					values[i] = fmt.Sprintf("candidate-%02d", i)
				}
				m.openChoiceOverlay("level", values)
				m.choiceCursor = 9
				view := m.View()
				if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
					t.Fatalf("dialog exceeds terminal %dx%d: %dx%d\n%s", m.width, m.height, lipgloss.Width(view), lipgloss.Height(view), stripAnsi(view))
				}
				x, y := dialogTextPosition(t, view, values[9])
				m.handleMouse(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
				if m.choiceCursor != 9 {
					t.Fatal("wheel outside dialog moved choice")
				}
				m.handleMouse(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
				if m.choiceCursor != 10 {
					t.Fatalf("wheel cursor=%d, want 10", m.choiceCursor)
				}
				dialogTextPosition(t, m.View(), values[10])
				m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnd})
				x, y = dialogTextPosition(t, m.View(), values[len(values)-1])
				m.handleMouse(dialogMousePress(x, y))
				if m.overlay != overlayNone || m.getValue("level") != values[len(values)-1] {
					t.Fatalf("click selected overlay=%d value=%v", m.overlay, m.getValue("level"))
				}
			})
		}
	}
}

func TestDialogMouseLanguageCandidates(t *testing.T) {
	for _, lang := range []language{languageEnglish, languageChinese} {
		for _, size := range [][2]int{{100, 30}, {60, 8}, {50, 9}} {
			t.Run(fmt.Sprintf("language_%d_%dx%d", lang, size[0], size[1]), func(t *testing.T) {
				m := newLanguageTestModel()
				m.language, m.width, m.height = lang, size[0], size[1]
				m.openLanguageSelector()
				target, want := "中文", languageChinese
				if lang == languageChinese {
					target, want = "English", languageEnglish
				}
				x, y := dialogTextPosition(t, m.View(), target)
				m.handleMouse(dialogMousePress(x, y))
				if m.overlay != overlayNone || m.language != want {
					t.Fatalf("click selected overlay=%d language=%d, want %d", m.overlay, m.language, want)
				}
			})
		}
	}
}

func TestDialogMouseConfirmButtons(t *testing.T) {
	for _, lang := range []language{languageEnglish, languageChinese} {
		for _, size := range [][2]int{{100, 30}, {60, 8}, {50, 9}, {51, 12}} {
			for selected := 0; selected < 2; selected++ {
				for target := 0; target < 2; target++ {
					t.Run(fmt.Sprintf("language_%d_%dx%d_selected_%d_target_%d", lang, size[0], size[1], selected, target), func(t *testing.T) {
						m := newLanguageTestModel()
						m.language, m.width, m.height = lang, size[0], size[1]
						m.confirmQuit, m.confirmBtn = true, selected
						view := m.View()
						if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
							t.Fatalf("confirmation exceeds terminal:\n%s", stripAnsi(view))
						}
						label := m.text(textSave)
						if target == 1 {
							label = m.text(textDiscard)
						}
						if target == selected {
							label = "[ " + label + " ]"
						} else {
							label = "  " + label + "  "
						}
						x, y := dialogTextPosition(t, view, label)
						_, cmd := m.handleMouse(dialogMousePress(x+2, y))
						if cmd == nil {
							t.Fatal("confirmation click did not quit")
						}
						if _, ok := cmd().(tea.QuitMsg); !ok || m.saved != (target == 0) {
							t.Fatalf("confirmation saved=%v, want %v", m.saved, target == 0)
						}
					})
				}
			}
		}
	}
}

func TestDialogMouseBlankAndOutsideClicks(t *testing.T) {
	for _, mode := range []string{"choice", "language", "detail", "confirm"} {
		t.Run(mode, func(t *testing.T) {
			m := newLanguageTestModel()
			m.language = languageEnglish
			m.setValue("enabled", true)
			title := "level"
			switch mode {
			case "choice":
				m.openChoiceOverlay("level", []string{"debug", "release"})
			case "language":
				m.openLanguageSelector()
				title = m.text(textLanguage)
			case "detail":
				m.openDetailOverlay(OptionItem{Name: "enabled", Opt: mkOpt("enabled", api.OptionBool, false)})
				title = "enabled"
			case "confirm":
				m.confirmQuit = true
				title = m.text(textUnsavedChanges)
			}
			x, y := dialogTextPosition(t, m.View(), title)
			m.handleMouse(dialogMousePress(x, y))
			if mode == "confirm" && !m.confirmQuit || mode != "confirm" && m.overlay == overlayNone {
				t.Fatal("clicking dialog title performed an action")
			}
			m.handleMouse(dialogMousePress(0, 0))
			if m.confirmQuit || m.overlay != overlayNone || !m.hasChanges || m.getValue("enabled") != true {
				t.Fatalf("outside click did not cancel while preserving changes: confirm=%v overlay=%d modified=%v", m.confirmQuit, m.overlay, m.hasChanges)
			}
		})
	}
}

func TestDialogMouseDetailFitsViewport(t *testing.T) {
	for _, lang := range []language{languageEnglish, languageChinese} {
		for _, size := range [][2]int{{100, 30}, {60, 8}, {50, 9}, {51, 12}} {
			t.Run(fmt.Sprintf("language_%d_%dx%d", lang, size[0], size[1]), func(t *testing.T) {
				m := newLanguageTestModel()
				m.language, m.width, m.height = lang, size[0], size[1]
				m.options["app"]["name"] = mkOpt("name", api.OptionString, strings.Repeat("默认中文值", 30)).
					SetDescription(strings.Repeat("很长的中文说明和 long description ", 40))
				value := strings.Repeat("修改后的超长中文值", 30)
				m.setValue("name", value)
				m.openDetailOverlay(OptionItem{Name: "name"})
				view := m.View()
				if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
					t.Fatalf("detail exceeds terminal %dx%d: %dx%d\n%s", m.width, m.height, lipgloss.Width(view), lipgloss.Height(view), stripAnsi(view))
				}
				if !strings.Contains(stripAnsi(view), m.text(textDetailScrollHint)) {
					t.Fatalf("scrollable detail is missing navigation hint:\n%s", stripAnsi(view))
				}
				dialogTextPosition(t, view, m.text(textCloseDetails))
				x, y := dialogTextPosition(t, view, "name")
				m.handleMouse(dialogMousePress(x, y))
				if m.overlay != overlayDetail || m.getValue("name") != value {
					t.Fatal("click inside detail changed state")
				}
				m.handleMouse(dialogMousePress(0, m.height/2))
				if m.overlay != overlayNone || !m.hasChanges || m.getValue("name") != value {
					t.Fatal("click outside detail did not close while preserving value")
				}
			})
		}
	}
}
