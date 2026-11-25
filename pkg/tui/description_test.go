package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
)

func descriptionModel() Model {
	opts := map[string]map[string]*api.Option{
		"app": {"debug": mkOpt("debug", api.OptionBool, false)},
	}
	m := testModel(mkSources("app"), nil, opts, map[string]map[string]any{}, "/w", "host", nil, nil, nil)
	m.language = languageEnglish
	m.width = 100
	m.height = 20
	m.focusArea = 1
	m.selectedPkg = "app"
	m.buildOptionItems()
	m.ed().Desc = "old"
	m.ed().OrigDesc = "old"
	return m
}

func TestDescriptionDisplayAndEdit(t *testing.T) {
	m := descriptionModel()
	view := stripAnsi(m.View())
	if !strings.Contains(view, "Config Description: old") || m.layout().descRow.height != 1 {
		t.Fatalf("description row missing (rect=%+v):\n%s", m.layout().descRow, view)
	}
	if optView := stripAnsi(m.layout().optionContent); strings.Contains(optView, "Description:") {
		t.Fatalf("config description should live in header, not inside package options:\n%s", optView)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	if !m.descEditing {
		t.Fatal("D did not start description editing")
	}
	if view := stripAnsi(m.View()); !strings.Contains(view, "old▎") {
		t.Fatalf("edit field missing:\n%s", view)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" new")})
	if m.descInput != "old new" {
		t.Fatalf("input = %q, want %q", m.descInput, "old new")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.descEditing || m.ed().Desc != "old new" || !m.ed().HasChanges() || m.modifiedCount() != 1 {
		t.Fatalf("after edit: editing=%t description=%q changes=%t count=%d", m.descEditing, m.ed().Desc, m.ed().HasChanges(), m.modifiedCount())
	}
	if view := stripAnsi(m.View()); !strings.Contains(view, "Config Description: old new") {
		t.Fatalf("edited description not rendered:\n%s", view)
	}
}

func TestDescriptionShowsConfigFileAndEditsFromTreeFocus(t *testing.T) {
	m := descriptionModel()
	m.focusArea = 0
	m.configFile = "configs/py32f539/evb/wifi-ble.json"
	m.language = languageChinese
	m.ed().Desc = "无线诊断固件"
	m.ed().OrigDesc = m.ed().Desc

	view := stripAnsi(m.View())
	want := "配置说明 [configs/py32f539/evb/wifi-ble.json]: 无线诊断固件"
	if !strings.Contains(view, want) {
		t.Fatalf("expected %q in header:\n%s", want, view)
	}
	if optView := stripAnsi(m.layout().optionContent); strings.Contains(optView, "配置说明") || strings.Contains(optView, "无线诊断固件") {
		t.Fatalf("package options panel must not display config description:\n%s", optView)
	}
	if !strings.Contains(view, "D 编辑说明") {
		t.Fatalf("tree-focus footer should advertise D description shortcut:\n%s", view)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	if !m.descEditing {
		t.Fatal("D should start description editing even when package tree is focused")
	}

	if got := m.descriptionLabel(100); got != "配置说明 [configs/py32f539/evb/wifi-ble.json]:" {
		t.Fatalf("wide descriptionLabel = %q", got)
	}
	if got := m.descriptionLabel(50); got != "配置说明 [wifi-ble.json]:" {
		t.Fatalf("medium descriptionLabel = %q", got)
	}
	if got := m.descriptionLabel(30); got != "配置说明:" {
		t.Fatalf("narrow descriptionLabel = %q", got)
	}
}

func TestDescriptionEditCancelAndClear(t *testing.T) {
	m := descriptionModel()
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.descEditing || m.ed().Desc != "old" || m.ed().HasChanges() {
		t.Fatalf("esc: editing=%t description=%q changes=%t", m.descEditing, m.ed().Desc, m.ed().HasChanges())
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.descEditing || m.ed().Desc != "" || !m.ed().HasChanges() || m.modifiedCount() != 1 {
		t.Fatalf("clear: editing=%t description=%q changes=%t count=%d", m.descEditing, m.ed().Desc, m.ed().HasChanges(), m.modifiedCount())
	}
}

func TestDescriptionInputFiltersControlRunes(t *testing.T) {
	m := descriptionModel()
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\nb\tc")})
	if m.descInput != "oldabc" {
		t.Fatalf("control runes kept: %q", m.descInput)
	}
	long := []rune(strings.Repeat("x", int(config.MaxDescriptionLength)))
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: long})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if got := len([]rune(m.descInput)); got != int(config.MaxDescriptionLength) {
		t.Fatalf("input length = %d, want %d", got, config.MaxDescriptionLength)
	}
}

func TestDescriptionRowHiddenOnTinyLayout(t *testing.T) {
	m := descriptionModel()
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 8})
	if m.layout().tooSmall {
		t.Fatal("60x8 should remain a live compact layout, not tooSmall")
	}
	if m.layout().descRow.height != 0 {
		t.Fatalf("description row height = %d, want 0", m.layout().descRow.height)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	if m.descEditing {
		t.Fatal("D must not start description editing when description row is hidden")
	}
}

func TestDescriptionEditCancelsWhenResizeHidesRow(t *testing.T) {
	m := descriptionModel()
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	if !m.descEditing {
		t.Fatal("D did not start description editing")
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 8})
	if m.descEditing {
		t.Fatal("shrinking terminal to hide description row must exit descEditing")
	}
}

func TestDescriptionRowClickStartsEditing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		width   int
		workDir string
	}{
		{name: "single_line_header", width: 100, workDir: "/w"},
		{name: "wrapped_header", width: 70, workDir: "/home/user/projects/very/deeply/nested/firmware/working/directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := descriptionModel()
			m.width = tc.width
			m.workDir = tc.workDir
			view := m.View()
			x, y := mouseTestPosition(t, view, "Config Description: old")
			m.Update(tea.MouseMsg{X: x + 2, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if !m.descEditing {
				t.Fatalf("click on rendered description (%d,%d) did not start editing (rect=%+v):\n%s", x+2, y, m.layout().descRow, stripAnsi(view))
			}
		})
	}
}

func TestDescriptionLoadedMultilineIsSanitized(t *testing.T) {
	m := descriptionModel()
	m.ed().Desc = "short\nsecond"
	m.ed().OrigDesc = m.ed().Desc
	header := stripAnsi(m.renderHeader())
	if !strings.Contains(header, "Config Description: short second") {
		t.Fatalf("multiline description not collapsed for display:\n%s", header)
	}
	if lines := strings.Split(strings.TrimRight(header, "\n"), "\n"); len(lines) != 3 {
		t.Fatalf("header occupied %d lines, want 3 (title + description + border):\n%s", len(lines), header)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	if m.descInput != "short second" {
		t.Fatalf("edit seed = %q, want %q", m.descInput, "short second")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" x")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.ed().Desc != "short second x" || !m.ed().HasChanges() {
		t.Fatalf("edit lost: description=%q changes=%t", m.ed().Desc, m.ed().HasChanges())
	}
}

func TestDescriptionNoopEditKeepsLoadedLineBreaks(t *testing.T) {
	m := descriptionModel()
	m.ed().Desc = "first\nsecond"
	m.ed().OrigDesc = m.ed().Desc
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	if m.descInput != "first second" {
		t.Fatalf("edit seed = %q, want %q", m.descInput, "first second")
	}
	// 打开编辑后直接回车（没改内容）：不能把多行原文覆盖成单行。
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.ed().Desc != "first\nsecond" || m.ed().HasChanges() {
		t.Fatalf("no-op edit flattened: description=%q changes=%t", m.ed().Desc, m.ed().HasChanges())
	}
}

func TestDescriptionLoadedOverlongIsTruncated(t *testing.T) {
	m := descriptionModel()
	m.ed().Desc = strings.Repeat("x", int(config.MaxDescriptionLength)+50)
	m.ed().OrigDesc = m.ed().Desc
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	if got := len([]rune(m.descInput)); got != int(config.MaxDescriptionLength) {
		t.Fatalf("edit seed length = %d, want %d", got, config.MaxDescriptionLength)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := len([]rune(m.ed().Desc)); got != int(config.MaxDescriptionLength) {
		t.Fatalf("saved length = %d, want %d", got, config.MaxDescriptionLength)
	}
}
