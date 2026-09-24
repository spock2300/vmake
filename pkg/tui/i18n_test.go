package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spock2300/vmake/pkg/api"
)

func newLanguageTestModel() Model {
	opts := map[string]map[string]*api.Option{
		"app": {
			"enabled": mkOpt("enabled", api.OptionBool, false).SetDescription("Enable app"),
			"level":   mkOpt("level", api.OptionChoice, "debug").SetValues("debug", "release"),
			"name":    mkOpt("name", api.OptionString, "app").SetDescription("App name"),
		},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/work", "", nil, nil, nil)
	m.width = 100
	m.height = 30
	m.selectedPkg = "app"
	m.buildOptionItems()
	return m
}

func toggleLanguageForTest(t *testing.T, m *Model) {
	t.Helper()
	m.language = languageEnglish
	m.openLanguageSelector()
	m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyDown})
	model, _ := m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	got, ok := model.(*Model)
	if !ok {
		t.Fatalf("language selector returned %T, want *Model", model)
	}
	if got.language != languageChinese {
		t.Fatalf("language selector language = %d, want Chinese", got.language)
	}
	got.openLanguageSelector()
	got.handleOverlayKey(tea.KeyMsg{Type: tea.KeyUp})
	model, _ = got.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	got = model.(*Model)
	if got.language != languageEnglish {
		t.Fatalf("second selector language = %d, want English", got.language)
	}
}

func TestLanguageTogglePreservesInteractionState(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Model)
		check func(*testing.T, *Model)
	}{
		{
			name: "tree",
			setup: func(m *Model) {
				m.focusArea = 0
				m.treeCursor = 0
			},
			check: func(t *testing.T, m *Model) {
				if m.focusArea != 0 || m.treeCursor != 0 {
					t.Fatalf("tree state changed: focus=%d cursor=%d", m.focusArea, m.treeCursor)
				}
			},
		},
		{
			name: "options",
			setup: func(m *Model) {
				m.focusArea = 1
				m.optCursor = 1
			},
			check: func(t *testing.T, m *Model) {
				if m.focusArea != 1 || m.optCursor != 1 {
					t.Fatalf("options state changed: focus=%d cursor=%d", m.focusArea, m.optCursor)
				}
			},
		},
		{
			name: "edit",
			setup: func(m *Model) {
				m.focusArea = 1
				m.optCursor = 2
				m.editing = true
				m.editInput = "partial"
				m.editCursor = len(m.editInput)
			},
			check: func(t *testing.T, m *Model) {
				if !m.editing || m.editInput != "partial" || m.editCursor != len(m.editInput) {
					t.Fatalf("edit state changed: editing=%v input=%q cursor=%d", m.editing, m.editInput, m.editCursor)
				}
			},
		},
		{
			name: "filter",
			setup: func(m *Model) {
				m.filterActive = true
				m.filterInput = "app"
			},
			check: func(t *testing.T, m *Model) {
				if !m.filterActive || m.filterInput != "app" {
					t.Fatalf("filter state changed: active=%v input=%q", m.filterActive, m.filterInput)
				}
			},
		},
		{
			name: "confirm",
			setup: func(m *Model) {
				m.confirmQuit = true
				m.confirmBtn = 1
			},
			check: func(t *testing.T, m *Model) {
				if !m.confirmQuit || m.confirmBtn != 1 {
					t.Fatalf("confirm state changed: quit=%v button=%d", m.confirmQuit, m.confirmBtn)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newLanguageTestModel()
			tt.setup(&m)
			toggleLanguageForTest(t, &m)
			if m.language != languageEnglish {
				t.Fatal("toggle helper should leave model in English")
			}
			m.openLanguageSelector()
			m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyDown})
			m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
			model := tea.Model(&m)
			m = *model.(*Model)
			if m.language != languageChinese {
				t.Fatal("language selector should switch model back to Chinese")
			}
			tt.check(t, &m)
		})
	}
}

func TestChineseRenderingAndEnglishToggle(t *testing.T) {
	m := newLanguageTestModel()
	m.language = languageChinese

	view := stripAnsi(m.View())
	for _, want := range []string{"VMake 配置", "软件包", "选项", "导航", "语言: 中文"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Chinese view missing %q:\n%s", want, view)
		}
	}

	m.confirmQuit = true
	confirm := stripAnsi(m.View())
	for _, want := range []string{"有未保存的修改", "保存", "放弃"} {
		if !strings.Contains(confirm, want) {
			t.Fatalf("Chinese confirmation missing %q:\n%s", want, confirm)
		}
	}

	m.confirmQuit = false
	m.language = languageEnglish
	view = stripAnsi(m.View())
	for _, want := range []string{"VMake Configuration", "Packages", "Options", "navigate", "Language: English"} {
		if !strings.Contains(view, want) {
			t.Fatalf("English view missing %q:\n%s", want, view)
		}
	}
}

func TestChineseOverlayHint(t *testing.T) {
	m := newLanguageTestModel()
	m.language = languageChinese
	m.openChoiceOverlay("level", []string{"debug", "release"})
	view := stripAnsi(m.View())
	for _, want := range []string{"导航", "选择", "取消"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Chinese choice overlay missing %q:\n%s", want, view)
		}
	}
}

func TestLanguageSelectorOpensFromHeader(t *testing.T) {
	m := newLanguageTestModel()
	m.language = languageEnglish
	x, y, _ := renderedLanguageSelector(t, m.View(), "[Language: English ▼]")
	model, _ := m.handleMouse(tea.MouseMsg{X: x, Y: y, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = *model.(*Model)
	if m.overlay != overlayLanguage {
		t.Fatalf("header language selector opened overlay %d, want language overlay", m.overlay)
	}
	m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.language != languageChinese {
		t.Fatalf("language selector selected %d, want Chinese", m.language)
	}
}
