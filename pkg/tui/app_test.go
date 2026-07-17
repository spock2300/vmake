package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spock2300/vmake/pkg/api"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripAnsi(s string) string { return ansiRe.ReplaceAllString(s, "") }

// Tree lines must never exceed the panel's rune budget. The (N) count badge
// previously wasn't counted in calcTreeWidth, causing overflow that corrupted
// the options panel layout and could hide the first option row.
func TestTreeDoesNotOverflowPanelWidth(t *testing.T) {
	opts := map[string]map[string]*api.Option{}
	var names []string
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("a_very_long_package_name_%02d", i)
		names = append(names, name)
		o := map[string]*api.Option{}
		for j := 0; j < 12; j++ {
			on := fmt.Sprintf("opt_%02d", j)
			o[on] = mkOpt(on, api.OptionBool, false).SetDescription("desc")
		}
		opts[name] = o
	}
	for _, w := range []int{60, 80, 100, 120} {
		t.Run(fmt.Sprintf("width=%d", w), func(t *testing.T) {
			m := NewModel(mkSources(names...), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
			m.width = w
			m.height = 24
			maxRune := 0
			for off := 0; off < len(m.flat); off++ {
				m.treeOff = off
				for _, line := range strings.Split(stripAnsi(m.renderTree()), "\n") {
					if rc := utf8.RuneCountInString(line); rc > maxRune {
						maxRune = rc
					}
				}
			}
			if maxRune > m.treeWidth {
				t.Errorf("width=%d: rendered tree line %d runes exceeds budget %d", w, maxRune, m.treeWidth)
			}
		})
	}
}

// With many packages forcing a wide tree, the selected package's first option
// must still render in the full View.
func TestFirstOptionVisibleWithManyPackages(t *testing.T) {
	opts := map[string]map[string]*api.Option{}
	var names []string
	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("longpkg_%02d", i)
		names = append(names, name)
		o := map[string]*api.Option{}
		for j := 0; j < 10; j++ {
			on := fmt.Sprintf("opt_%02d", j)
			o[on] = mkOpt(on, api.OptionBool, false).SetDescription("description text")
		}
		opts[name] = o
	}
	for _, w := range []int{60, 80, 100} {
		t.Run(fmt.Sprintf("width=%d", w), func(t *testing.T) {
			m := NewModel(mkSources(names...), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
			m.width = w
			m.height = 24
			m.selectedPkg = names[0]
			m.buildOptionItems()
			visible := m.visibleOptions()
			view := stripAnsi(m.View())
			if !strings.Contains(view, visible[0].Name) {
				t.Errorf("width=%d: first option %q not visible in View", w, visible[0].Name)
			}
		})
	}
}

// Names too long to fit are truncated (with …) so the badge stays visible inline
// and the line never overflows the panel.
func TestTreeTruncatesOverlongNames(t *testing.T) {
	longName := "this_is_an_extremely_long_package_name_that_will_not_fit_inline_xxx"
	opts := map[string]map[string]*api.Option{
		"short":  {"a": mkOpt("a", api.OptionBool, false)},
		longName: {"a": mkOpt("a", api.OptionBool, false), "b": mkOpt("b", api.OptionBool, false)},
	}
	m := NewModel(mkSources("short", longName), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 24
	treeOut := stripAnsi(m.renderTree())
	if !strings.Contains(treeOut, "…") {
		t.Errorf("expected overlong name to be truncated with …\n%s", treeOut)
	}
	for _, line := range strings.Split(treeOut, "\n") {
		if rc := utf8.RuneCountInString(line); rc > m.treeWidth {
			t.Errorf("truncated line still overflows: %d > %d: %q", rc, m.treeWidth, line)
		}
	}
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// Single match: typing + Enter jumps immediately and restores the full tree.
func TestFilterSingleMatchJumpsImmediately(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app":   {"debug": mkOpt("debug", api.OptionBool, false)},
		"other": {"x": mkOpt("x", api.OptionBool, false)},
	}
	m := NewModel(mkSources("app", "other"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 24

	m.handleKey(keyRunes("/"))
	if !m.filterActive {
		t.Fatal("'/'' should activate filter input")
	}
	m.handleKey(keyRunes("ap"))
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.filterActive {
		t.Error("Enter should exit filter input mode")
	}
	if m.filterInput != "" {
		t.Errorf("single-match Enter should clear filter, got %q", m.filterInput)
	}
	if m.selectedPkg != "app" {
		t.Errorf("single-match should jump to app, selectedPkg=%q", m.selectedPkg)
	}
}

// Multiple matches: typing + Enter enters browse mode (filter kept, not jumped yet).
func TestFilterMultipleMatchEntersBrowseMode(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"lib1": {"x": mkOpt("x", api.OptionBool, false)},
		"lib2": {"y": mkOpt("y", api.OptionBool, false)},
		"app":  {"z": mkOpt("z", api.OptionBool, false)},
	}
	m := NewModel(mkSources("lib1", "lib2", "app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 24

	m.handleKey(keyRunes("/"))
	m.handleKey(keyRunes("lib"))
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.filterActive {
		t.Error("Enter should exit filter input mode (browse)")
	}
	if m.filterInput != "lib" {
		t.Errorf("multi-match Enter should keep filter applied for browse, got %q", m.filterInput)
	}
	// browse mode shows only the 2 lib matches
	if c := m.filteredMatchCount(); c != 2 {
		t.Errorf("browse should show 2 matches, got %d", c)
	}
	if m.focusArea != 0 {
		t.Error("browse mode should focus the tree")
	}
}

// In browse mode, Enter jumps to the selected match and restores full tree.
func TestBrowseEnterJumpsToSelectedMatch(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"lib1": {"x": mkOpt("x", api.OptionBool, false)},
		"lib2": {"y": mkOpt("y", api.OptionBool, false)},
		"app":  {"z": mkOpt("z", api.OptionBool, false)},
	}
	m := NewModel(mkSources("lib1", "lib2", "app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 24

	m.handleKey(keyRunes("/"))
	m.handleKey(keyRunes("lib"))
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	// now in browse mode on lib1; move down to lib2
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedPkg != "lib2" {
		t.Fatalf("down should select lib2, got %q", m.selectedPkg)
	}
	// Enter jumps to lib2, restoring full tree
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.filterInput != "" {
		t.Errorf("browse Enter should clear filter, got %q", m.filterInput)
	}
	if m.selectedPkg != "lib2" {
		t.Errorf("browse Enter should jump to selected lib2, got %q", m.selectedPkg)
	}
}

// In browse mode, Esc clears the filter and restores full tree.
func TestBrowseEscClearsFilter(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"lib1": {"x": mkOpt("x", api.OptionBool, false)},
		"lib2": {"y": mkOpt("y", api.OptionBool, false)},
	}
	m := NewModel(mkSources("lib1", "lib2"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 24
	m.handleKey(keyRunes("/"))
	m.handleKey(keyRunes("lib"))
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.filterInput != "" {
		t.Errorf("Esc should clear filter, got %q", m.filterInput)
	}
}

// After any search jump, focus must be on the tree so the user can keep
// navigating packages (they never get trapped on the options panel).
func TestJumpKeepsFocusOnTree(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {
			"debug":   mkOpt("debug", api.OptionBool, false),
			"version": mkOpt("version", api.OptionString, "1.0"),
		},
		"lib": {"x": mkOpt("x", api.OptionBool, false)},
	}
	m := NewModel(mkSources("app", "lib"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 24

	// search matches the "version" option in app → single match → jump
	m.handleKey(keyRunes("/"))
	m.handleKey(keyRunes("version"))
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.focusArea != 0 {
		t.Errorf("after jump focus should be on tree (0), got %d", m.focusArea)
	}
	// user can immediately navigate the package tree with arrows
	startPkg := m.selectedPkg
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedPkg == startPkg {
		t.Errorf("tree navigation after jump should change selection: still %q", m.selectedPkg)
	}
}

// Navigating the tree with j/k all the way down must render the last package.
func TestTreeJKNavReachesLast(t *testing.T) {
	opts := map[string]map[string]*api.Option{}
	var names []string
	for i := 0; i < 40; i++ {
		n := fmt.Sprintf("pkg_%02d", i)
		names = append(names, n)
		opts[n] = map[string]*api.Option{"a": mkOpt("a", api.OptionBool, false)}
	}
	m := NewModel(mkSources(names...), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 8
	last := names[len(names)-1]
	for i := 0; i < len(m.flat); i++ {
		m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	out := stripAnsi(m.View())
	if !strings.Contains(out, last) {
		t.Errorf("last pkg %q not visible after navigating to bottom with ↓\n%s", last, out)
	}
}

// Editing an OptionString must accept multi-byte (non-ASCII) runes, e.g. CJK
// or accented chars. Previously len(ch)!=1 (byte length) silently dropped them.
func TestEditAcceptsMultibyteRunes(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"name": mkOpt("name", api.OptionString, "")},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 24
	m.selectedPkg = "app"
	m.buildOptionItems()
	m.focusArea = 1
	// enter edit mode (Space/Enter on a string option)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if !m.editing {
		t.Fatal("expected edit mode")
	}
	// type a CJK char (3 bytes) + an accented char (2 bytes)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("中")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("é")})
	if m.editInput != "中é" {
		t.Errorf("editInput = %q, want 中é", m.editInput)
	}
	// commit
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.getValue("name") != "中é" {
		t.Errorf("saved value = %v, want 中é", m.getValue("name"))
	}
}

// OptionInt edit must still reject non-digit runes.
func TestEditIntRejectsNonDigit(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"n": mkOpt("n", api.OptionInt, 0)},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 24
	m.selectedPkg = "app"
	m.buildOptionItems()
	m.focusArea = 1
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if !m.editing {
		t.Fatal("expected edit mode")
	}
	// editInput is seeded with current value "0"
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if m.editInput != "0" {
		t.Errorf("int edit should reject 'a', got input %q", m.editInput)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	if m.editInput != "04" {
		t.Errorf("int edit should accept '4', got input %q", m.editInput)
	}
}

// Browse-mode Esc must resync selectedPkg to whatever node the cursor lands on
// in the restored (full) tree, not leave it pointing at the old package.
func TestBrowseEscResyncsSelectedPkg(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"lib1": {"x": mkOpt("x", api.OptionBool, false)},
		"lib2": {"y": mkOpt("y", api.OptionBool, false)},
		"app":  {"z": mkOpt("z", api.OptionBool, false)},
	}
	m := NewModel(mkSources("lib1", "lib2", "app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 24
	// search "lib" -> 2 matches -> browse mode
	m.handleKey(keyRunes("/"))
	m.handleKey(keyRunes("lib"))
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	// in browse, flat = [lib1, lib2] (filtered); cursor on lib1 (filtered idx 0).
	// Esc restores full tree; cursor idx 0 now = Global, but the first real pkg
	// the cursor should map to must match the rendered node at that index.
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.filterInput != "" {
		t.Fatalf("Esc should clear filter, got %q", m.filterInput)
	}
	// selectedPkg must equal the package at the cursor in the FULL flat tree.
	cursorNode := ""
	if m.treeCursor < len(m.flat) {
		cursorNode = m.flat[m.treeCursor].PkgName
	}
	if m.selectedPkg != cursorNode {
		t.Errorf("selectedPkg %q != cursor node %q after browse Esc", m.selectedPkg, cursorNode)
	}
}

// Mouse click on a tree node with children toggles its expansion (like Enter),
// so users can explore the dependency tree with the mouse.
func TestMouseClickTogglesTreeExpansion(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"a": mkOpt("a", api.OptionBool, false)},
		"lib": {"b": mkOpt("b", api.OptionBool, false)},
	}
	deps := map[string][]string{"app": {"lib"}}
	m := NewModel(mkSources("app", "lib"), deps, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 24

	// app starts collapsed: flat = [Global(0), app(1)]
	appIdx := -1
	for i, n := range m.flat {
		if n.PkgName == "app" {
			appIdx = i
		}
	}
	if appIdx == -1 {
		t.Fatal("app not in flat tree")
	}
	if m.flat[appIdx].Expanded {
		t.Fatal("app should start collapsed")
	}

	// click on app row: Y = headerH(2) + appIdx
	click := func(y int) tea.MouseMsg {
		return tea.MouseMsg{X: 2, Y: y, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	}
	m.handleMouse(click(2 + appIdx))

	appNode := (*TreeNode)(nil)
	for _, n := range m.flat {
		if n.PkgName == "app" {
			appNode = n
		}
	}
	if appNode == nil || !appNode.Expanded {
		t.Error("click should expand app")
	}
	// after expand, lib should now be visible
	visible := map[string]bool{}
	for _, n := range m.flat {
		visible[n.PkgName] = true
	}
	if !visible["lib"] {
		t.Error("lib should be visible after expanding app")
	}

	// click app again → collapse
	m.handleMouse(click(2 + appIdx))
	appNode = nil
	for _, n := range m.flat {
		if n.PkgName == "app" {
			appNode = n
		}
	}
	if appNode != nil && appNode.Expanded {
		t.Error("second click should collapse app")
	}
}

// Mouse click on a leaf package just selects it (no toggle, no children).
func TestMouseClickLeafSelectsOnly(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"a": mkOpt("a", api.OptionBool, false)},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.width = 80
	m.height = 24
	click := tea.MouseMsg{X: 2, Y: 3, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	m.handleMouse(click)
	if m.selectedPkg != "app" {
		t.Errorf("click should select app, got %q", m.selectedPkg)
	}
}

// Mouse Y mapping must use the ACTUAL rendered header height, not a hardcoded
// value. With a long workDir the header wraps to 4 rows; the old hardcoded 2
// made clicks land above their target ("click the 2nd row to hit the 1st pkg").
func TestMouseMappingUsesRenderedHeaderHeight(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"a": mkOpt("a", api.OptionBool, false)},
		"lib": {"b": mkOpt("b", api.OptionBool, false)},
	}
	longDir := "/home/user/projects/very/deeply/nested/firmware/build/working/directory"
	m := NewModel(mkSources("app", "lib"), map[string][]string{"app": {"lib"}}, opts, map[string]map[string]any{}, longDir, "gcc", nil, nil, nil)
	m.width = 60
	m.height = 24
	m.View()

	hh := m.headerHeight()
	if hh <= 2 {
		t.Fatalf("expected wrapped header >2 rows for long workDir, got %d", hh)
	}

	// Global sits at Y=hh; the first real package (app) at Y=hh+1.
	click := func(y int) tea.MouseMsg {
		return tea.MouseMsg{X: 2, Y: y, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	}
	mm := m
	mm.handleMouse(click(hh))
	if mm.selectedPkg != GlobalPkgName && mm.flat[mm.treeCursor].Name != "Global" {
		t.Errorf("click at headerH should hit Global, got %q", mm.selectedPkg)
	}
	mm = m
	mm.handleMouse(click(hh + 1))
	if mm.selectedPkg != "app" {
		t.Errorf("click at headerH+1 should hit first package app, got %q", mm.selectedPkg)
	}
}

// Footer height is also dynamic (it wraps when help text is long).
func TestContentHeightAccountsForWrappedFooter(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"a": mkOpt("a", api.OptionBool, false)},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "gcc", nil, nil, nil)
	m.width = 50
	m.height = 24
	m.View()
	if m.footerHeight() <= 2 {
		t.Fatalf("expected wrapped footer >2 rows at width 50, got %d", m.footerHeight())
	}
	// contentHeight must subtract the real footer, not a hardcoded 2.
	realContent := m.height - m.headerHeight() - m.footerHeight()
	if m.contentHeight() != realContent {
		t.Errorf("contentHeight=%d, want %d (height - header - footer)", m.contentHeight(), realContent)
	}
}
