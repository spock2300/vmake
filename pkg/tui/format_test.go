package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/spock2300/vmake/pkg/api"
)

func TestFormatValueUsesPlainDecimal(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{name: "int", in: 7, want: "7"},
		{name: "large int", in: int64(4294967296), want: "4294967296"},
		{name: "whole float", in: float64(4), want: "4"},
		{name: "million float", in: float64(1000000), want: "1000000"},
		{name: "large float", in: float64(4294967296), want: "4294967296"},
		{name: "fractional float", in: 1.5, want: "1.5"},
		{name: "string", in: "x", want: "x"},
		{name: "bool", in: true, want: "true"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatValue(c.in); got != c.want {
				t.Fatalf("formatValue(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestLargeIntegerRendersWithoutExponent(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"count": mkOpt("count", api.OptionInt, int64(4294967296))},
	}
	values := map[string]map[string]any{
		"app": {"count": float64(4294967296)},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, values, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 24
	m.selectedPkg = "app"
	m.buildOptionItems()
	m.focusArea = 1

	out := stripAnsi(m.renderOptions())
	if !strings.Contains(out, "4294967296") {
		t.Fatalf("options view lacks plain value:\n%s", out)
	}
	if strings.Contains(out, "e+") {
		t.Fatalf("options view uses scientific notation:\n%s", out)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.editing {
		t.Fatal("expected edit mode")
	}
	if m.editInput != "4294967296" {
		t.Fatalf("editInput = %q, want plain decimal", m.editInput)
	}

	m.openDetailOverlay(OptionItem{Name: "count"})
	detail := stripAnsi(m.renderDetailOverlay())
	if !strings.Contains(detail, "4294967296") || strings.Contains(detail, "e+") {
		t.Fatalf("detail view uses scientific notation:\n%s", detail)
	}
}
