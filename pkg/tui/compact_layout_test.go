package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/spock2300/vmake/pkg/api"
)

func compactLayoutModel() Model {
	var names []string
	opts := map[string]map[string]*api.Option{}
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("package_%02d", i)
		names = append(names, name)
		opts[name] = map[string]*api.Option{
			"enabled": mkOpt("enabled", api.OptionBool, false),
			"second":  mkOpt("second", api.OptionBool, false),
		}
	}
	m := NewModel(mkSources(names...), nil, opts, map[string]map[string]any{}, "/w", "host", nil, nil, nil)
	m.jumpToMatch(names[0], "")
	m.focusArea = 1
	return m
}

func TestCompactLayoutFitsTerminal(t *testing.T) {
	for _, lang := range []language{languageEnglish, languageChinese} {
		for _, size := range [][2]int{{50, 9}, {60, 8}} {
			for _, path := range []string{strings.Repeat("/workspace/project", 10), strings.Repeat("/工作目录/项目配置", 10)} {
				for _, modified := range []bool{false, true} {
					for _, filter := range []string{"none", "active", "applied", "empty"} {
						name := fmt.Sprintf("language=%d/%dx%d/chinese_path=%t/modified=%t/filter=%s", lang, size[0], size[1], strings.Contains(path, "工作"), modified, filter)
						t.Run(name, func(t *testing.T) {
							m := compactLayoutModel()
							m.language = lang
							m.workDir = path
							m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
							if modified {
								m.setValue("enabled", true)
							}
							if filter != "none" {
								m.filterInput = "package"
								m.filterActive = filter == "active"
								if filter == "empty" {
									m.filterInput = "no-match"
								}
								m.rebuildFlat()
							}
							for frame := 0; frame < 2; frame++ {
								view := m.View()
								if lipgloss.Height(view) > m.height || lipgloss.Width(view) > m.width {
									t.Fatalf("frame %d is %dx%d in %dx%d terminal:\n%s", frame, lipgloss.Width(view), lipgloss.Height(view), m.width, m.height, stripAnsi(view))
								}
								selector := "[" + m.text(textLanguage) + ": " + m.languageLabel() + " ▼]"
								x, y := mouseTestPosition(t, view, selector)
								if x != m.languageSelectorX || y != m.languageSelectorY {
									t.Fatalf("selector rendered at (%d,%d), hitbox starts at (%d,%d)", x, y, m.languageSelectorX, m.languageSelectorY)
								}
								x, y = mouseTestPosition(t, view, "enabled")
								if idx, ok := m.optionRowAt(x, y); !ok || idx != 0 {
									t.Fatalf("visible option does not match its mouse target: index=%d ok=%t", idx, ok)
								}
								if !strings.Contains(stripAnsi(view), m.text(textPackages)) || !strings.Contains(stripAnsi(view), m.text(textOptions)) {
									t.Fatalf("panel title is missing:\n%s", stripAnsi(view))
								}
								if frame == 0 {
									m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
									m.View()
									m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestNormalLayoutPreservesHeaderAndFooter(t *testing.T) {
	m := compactLayoutModel()
	m.language = languageEnglish
	m.width, m.height = 120, 40
	m.setValue("enabled", true)
	header, footer := m.renderHeader(), m.renderFooter()
	view := m.View()
	if !strings.HasPrefix(view, header+"\n") || !strings.HasSuffix(view, "\n"+footer) {
		t.Fatalf("normal terminal unexpectedly compressed its header or footer:\n%s", stripAnsi(view))
	}
}
