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
	view := stripAnsi(m.layout().optionContent)
	if !strings.Contains(view, "Description: old") || m.layout().descRowOffset != 1 {
		t.Fatalf("description row missing (offset=%d):\n%s", m.layout().descRowOffset, view)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	if !m.descEditing {
		t.Fatal("D did not start description editing")
	}
	if view := stripAnsi(m.layout().optionContent); !strings.Contains(view, "old▎") {
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
	if view := stripAnsi(m.layout().optionContent); !strings.Contains(view, "Description: old new") {
		t.Fatalf("edited description not rendered:\n%s", view)
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
	m.height = m.layout().headerH + m.layout().footerH + 2
	if m.layout().descRowOffset != 0 {
		t.Fatalf("description row offset = %d, want 0", m.layout().descRowOffset)
	}
}

func TestDescriptionRowClickStartsEditing(t *testing.T) {
	m := descriptionModel()
	m.View()
	if m.layout().descRowOffset != 1 {
		t.Fatalf("description row offset = %d, want 1", m.layout().descRowOffset)
	}
	x := m.layout().treeW + 3
	y := m.layout().headerH + 1
	m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !m.descEditing {
		t.Fatal("click did not start description editing")
	}
}

func TestDescriptionLoadedMultilineIsSanitized(t *testing.T) {
	m := descriptionModel()
	m.ed().Desc = "short\nsecond"
	m.ed().OrigDesc = m.ed().Desc
	view := stripAnsi(m.layout().optionContent)
	if !strings.Contains(view, "Description: short second") {
		t.Fatalf("multiline description not collapsed for display:\n%s", view)
	}
	if lines := strings.Split(strings.TrimRight(view, "\n"), "\n"); len(lines) != 4 {
		t.Fatalf("description row occupied %d lines, want 4 total:\n%s", len(lines), view)
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
