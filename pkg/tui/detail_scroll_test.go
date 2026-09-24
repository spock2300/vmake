package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/spock2300/vmake/pkg/api"
)

func detailScrollModel(lang language, width, height int) Model {
	m := newLanguageTestModel()
	m.language, m.width, m.height = lang, width, height
	def := strings.Repeat("默认路径/default/", 12) + "\nDEFAULT_END"
	current := strings.Repeat("当前路径/current/", 12) + "\nCURRENT_END"
	desc := strings.Repeat("中文说明 wrapped description ", 16) + "\nDESCRIPTION_END"
	m.options["app"]["level"] = mkOpt("level", api.OptionChoice, def).
		SetValues(def, current, strings.Repeat("候选值/choice/", 12)+"\nCHOICES_END").
		SetDescription(desc)
	m.setValue("level", current)
	m.openDetailOverlay(OptionItem{Name: "level"})
	return m
}

func detailScrollView(t *testing.T, m *Model) string {
	t.Helper()
	view := m.View()
	if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
		t.Fatalf("detail exceeds terminal %dx%d: %dx%d\n%s", m.width, m.height, lipgloss.Width(view), lipgloss.Height(view), stripAnsi(view))
	}
	if m.detailRows < 1 || m.detailOff < 0 || m.detailOff > max(0, m.detailTotal-m.detailRows) {
		t.Fatalf("invalid detail viewport: offset=%d rows=%d total=%d", m.detailOff, m.detailRows, m.detailTotal)
	}
	dialogTextPosition(t, view, m.text(textCloseDetails))
	return stripAnsi(view)
}

func TestDetailScrollReachesCompleteContent(t *testing.T) {
	for _, lang := range []language{languageEnglish, languageChinese} {
		for _, size := range [][2]int{{60, 8}, {50, 9}} {
			t.Run(fmt.Sprintf("language_%d_%dx%d", lang, size[0], size[1]), func(t *testing.T) {
				m := detailScrollModel(lang, size[0], size[1])
				current := m.getValue("level")
				if !strings.Contains(detailScrollView(t, &m), m.text(textDetailScrollHint)) {
					t.Fatal("overflowing details have no scroll hint")
				}
				var seen strings.Builder
				closeY := -1
				for steps := 0; ; steps++ {
					if steps > 1024 {
						t.Fatal("detail never reached its end")
					}
					view := detailScrollView(t, &m)
					_, y := dialogTextPosition(t, view, m.text(textCloseDetails))
					if closeY >= 0 && y != closeY {
						t.Fatalf("close hint moved from row %d to %d while scrolling", closeY, y)
					}
					closeY = y
					seen.WriteString(view + "\n")
					offset := m.detailOff
					m.Update(tea.KeyMsg{Type: tea.KeyDown})
					m.View()
					if m.detailOff == offset {
						break
					}
				}
				for _, want := range []string{"DEFAULT_END", "CURRENT_END", "DESCRIPTION_END", "CHOICES_END", m.text(textCurrent), m.text(textModifiedLabel)} {
					if !strings.Contains(seen.String(), want) {
						t.Fatalf("scrolling never revealed %q", want)
					}
				}
				status := m.text(textModifiedLabel) + " : " + m.text(textYes)
				if !strings.Contains(strings.Join(strings.Fields(seen.String()), " "), status) {
					t.Fatalf("scrolling never revealed modification status %q", status)
				}
				if m.overlay != overlayDetail || m.getValue("level") != current || !m.hasChanges {
					t.Fatal("scrolling changed the option or closed its details")
				}
			})
		}
	}
}

func TestDetailScrollKeyboardNavigation(t *testing.T) {
	m := detailScrollModel(languageEnglish, 60, 8)
	detailScrollView(t, &m)
	for _, key := range []tea.KeyMsg{{Type: tea.KeyUp}, keyRunes("k"), {Type: tea.KeyPgUp}, {Type: tea.KeyHome}, keyRunes("g")} {
		m.Update(key)
		if m.detailOff != 0 {
			t.Fatalf("%q scrolled before the beginning", key.String())
		}
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyDown}, keyRunes("j")} {
		before := m.detailOff
		m.Update(key)
		if m.detailOff != before+1 {
			t.Fatalf("%q moved from %d to %d, want one line", key.String(), before, m.detailOff)
		}
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyUp}, keyRunes("k")} {
		before := m.detailOff
		m.Update(key)
		if m.detailOff != before-1 {
			t.Fatalf("%q moved from %d to %d, want one line", key.String(), before, m.detailOff)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.detailOff != m.detailRows {
		t.Fatalf("page down offset=%d, want %d", m.detailOff, m.detailRows)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.detailOff != 0 {
		t.Fatalf("page up offset=%d, want 0", m.detailOff)
	}
	for _, end := range []tea.KeyMsg{{Type: tea.KeyEnd}, keyRunes("G")} {
		m.Update(end)
		view := detailScrollView(t, &m)
		if !strings.Contains(view, "CHOICES_END") || m.detailOff != m.detailTotal-m.detailRows {
			t.Fatalf("%q did not reveal the end:\n%s", end.String(), view)
		}
		for _, key := range []tea.KeyMsg{{Type: tea.KeyDown}, keyRunes("j"), {Type: tea.KeyPgDown}} {
			before := m.detailOff
			m.Update(key)
			if m.detailOff != before {
				t.Fatalf("%q scrolled beyond the end", key.String())
			}
		}
		m.Update(tea.KeyMsg{Type: tea.KeyHome})
		if m.detailOff != 0 {
			t.Fatal("home did not return to the beginning")
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m.Update(keyRunes("g"))
	if m.detailOff != 0 {
		t.Fatal("g did not return to the beginning")
	}
}

func TestDetailScrollMouseAndReset(t *testing.T) {
	for _, lang := range []language{languageEnglish, languageChinese} {
		t.Run(fmt.Sprintf("language_%d", lang), func(t *testing.T) {
			m := detailScrollModel(lang, 50, 9)
			current := m.getValue("level")
			view := detailScrollView(t, &m)
			x, y := dialogTextPosition(t, view, m.text(textCloseDetails))
			m.Update(mouseTestWheel(0, m.height/2, true))
			if m.detailOff != 0 {
				t.Fatal("wheel outside details scrolled their contents")
			}
			m.Update(mouseTestWheel(x, y, true))
			if m.detailOff != 1 {
				t.Fatalf("wheel down offset=%d, want 1", m.detailOff)
			}
			m.Update(mouseTestClick(x, y))
			if m.detailOff != 1 || m.overlay != overlayDetail {
				t.Fatal("click inside details changed navigation state")
			}
			m.Update(mouseTestWheel(x, y, false))
			m.Update(mouseTestWheel(x, y, false))
			if m.detailOff != 0 {
				t.Fatalf("wheel up crossed the beginning: offset=%d", m.detailOff)
			}
			m.Update(tea.KeyMsg{Type: tea.KeyEnd})
			m.Update(mouseTestClick(0, m.height/2))
			if m.overlay != overlayNone || m.detailOff != 0 || m.detailRows != 0 || m.detailTotal != 0 {
				t.Fatal("closing details did not reset their viewport")
			}
			m.openDetailOverlay(OptionItem{Name: "level"})
			if m.detailOff != 0 {
				t.Fatal("reopening details retained the old offset")
			}
			if !strings.Contains(detailScrollView(t, &m), "level") || m.getValue("level") != current || !m.hasChanges {
				t.Fatal("mouse navigation changed the value or failed to reopen at the beginning")
			}
		})
	}
}

func TestDetailScrollResizeAndUnboundedHeight(t *testing.T) {
	m := detailScrollModel(languageChinese, 50, 9)
	detailScrollView(t, &m)
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	for _, size := range [][2]int{{100, 30}, {60, 8}, {50, 9}, {200, 100}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		detailScrollView(t, &m)
		m.Update(tea.KeyMsg{Type: tea.KeyEnd})
		if !strings.Contains(detailScrollView(t, &m), "CHOICES_END") {
			t.Fatalf("content became unreachable after resize to %dx%d", size[0], size[1])
		}
	}
	m.height = 0
	view := stripAnsi(m.renderDetailOverlay())
	for _, want := range []string{"DEFAULT_END", "CURRENT_END", "DESCRIPTION_END", "CHOICES_END", m.text(textCloseDetails)} {
		if !strings.Contains(view, want) {
			t.Fatalf("unbounded details omitted %q", want)
		}
	}
	if m.detailOff != 0 {
		t.Fatalf("unbounded details retained offset %d", m.detailOff)
	}
}
