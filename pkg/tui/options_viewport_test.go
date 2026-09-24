package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/spock2300/vmake/pkg/api"
)

func viewportModel() Model {
	opts := map[string]map[string]*api.Option{
		"app": {
			"first":  mkOpt("first", api.OptionBool, false),
			"second": mkOpt("second", api.OptionBool, false),
		},
	}
	m := NewModel(mkSources("app"), nil, opts, map[string]map[string]any{}, "/w", "host", nil, nil, nil)
	m.language = languageEnglish
	m.selectedPkg = "app"
	m.focusArea = 1
	m.buildOptionItems()
	return m
}

func TestOptionsSingleRowViewportKeepsSelectedRowVisible(t *testing.T) {
	for _, lang := range []language{languageEnglish, languageChinese} {
		t.Run(fmt.Sprintf("language=%d", lang), func(t *testing.T) {
			m := viewportModel()
			m.language = lang
			m.width = 60
			m.height = m.headerHeight() + m.footerHeight() + 2
			m.kconfigs = map[string][]*api.KConfigEntry{
				"app": {(&api.KConfigEntry{}).AddPreset("default_config").SelectPreset("default_config")},
			}
			if rows := m.optItemRows(); rows != 1 {
				t.Fatalf("option row budget = %d, want 1", rows)
			}
			wantRows := []string{"first", "second", "default_config", "menuconfig"}
			check := func(cursor int) {
				t.Helper()
				view := stripAnsi(m.renderOptions())
				lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
				if len(lines) != 2 || !strings.Contains(lines[1], wantRows[cursor]) {
					t.Fatalf("cursor %d should show only its selected row below the title:\n%s", cursor, view)
				}
				if strings.Contains(view, "%") {
					t.Fatalf("single row viewport should omit its scroll indicator:\n%s", view)
				}
			}
			check(0)
			for cursor := 1; cursor < len(wantRows); cursor++ {
				m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
				if m.optCursor != cursor {
					t.Fatalf("down selected row %d, want %d", m.optCursor, cursor)
				}
				check(cursor)
			}
			for cursor := len(wantRows) - 2; cursor >= 0; cursor-- {
				m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
				if m.optCursor != cursor {
					t.Fatalf("up selected row %d, want %d", m.optCursor, cursor)
				}
				check(cursor)
			}
		})
	}
}

func TestSmallTerminalKeepsSelectedOptionVisible(t *testing.T) {
	for _, size := range [][2]int{{60, 8}, {50, 9}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := viewportModel()
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for frame := 0; frame < 2; frame++ {
				if view := stripAnsi(m.View()); !strings.Contains(view, "first") {
					t.Fatalf("first option is missing from frame %d of a small terminal:\n%s", frame, view)
				}
			}
			m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
			if view := stripAnsi(m.View()); m.optCursor != 1 || !strings.Contains(view, "second") {
				t.Fatalf("down should show second option, cursor=%d:\n%s", m.optCursor, view)
			}
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
			m.View()
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for frame := 0; frame < 2; frame++ {
				if view := stripAnsi(m.View()); !strings.Contains(view, "second") {
					t.Fatalf("selected option is missing after resize on frame %d:\n%s", frame, view)
				}
			}
			m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
			if view := stripAnsi(m.View()); m.optCursor != 0 || !strings.Contains(view, "first") {
				t.Fatalf("up should show first option, cursor=%d:\n%s", m.optCursor, view)
			}
		})
	}
}
