package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/config"
)

func buildHLDemoCleanTestModel() Model {
	sources := mkSources(
		"hl-demo-clean",
		"firmware",
		"target-app",
		"board",
		"board-py32f539-evb",
		"lvgl-demos",
		"common",
	)
	deps := map[string][]string{
		"firmware": {
			"hl/hl-mcu-sdk",
			"hl/hl-mcu-sdk/system",
			"board",
			"board-py32f539-evb",
			"lvgl-demos",
			"hl/hl-mcu-sdk/system/freertos/cherryusb",
		},
		"target-app": {
			"hl/hl-mcu-sdk",
			"hl/hl-mcu-sdk/system",
			"board",
			"board-py32f539-evb",
		},
		"board-py32f539-evb": {
			"hl/hl-mcu-sdk",
			"hl/hl-mcu-sdk/system/uniface",
			"board",
			"hl/hl-mcu-sdk/system/chip/puya/py32f539",
		},
		"lvgl-demos": {
			"hl/hl-mcu-sdk",
			"board",
			"common",
		},
		"common": {
			"hl/hl-mcu-sdk",
			"board",
		},
		"board": {
			"hl/hl-mcu-sdk",
			"hl/hl-mcu-sdk/system/uniface",
		},
	}

	makeN := func(n int) map[string]*api.Option {
		m := make(map[string]*api.Option, n)
		for i := 0; i < n; i++ {
			name := "opt"
			if i > 0 {
				name = string(rune('a' + i))
			}
			m[name] = mkOpt(name, api.OptionBool, true).SetDescription("Option description")
		}
		return m
	}

	fwOpts := makeN(11)
	fwOpts["usb"] = mkOpt("usb", api.OptionBool, false).SetDescription("Enable CherryUSB stack")

	boardOpts := makeN(15)
	boardOpts["graphics"] = mkOpt("graphics", api.OptionBool, false).SetDescription("Display panel support")

	opts := map[string]map[string]*api.Option{
		"hl-demo-clean":      {},
		"firmware":           fwOpts,
		"target-app":         makeN(7),
		"board-py32f539-evb": boardOpts,
		"lvgl-demos":         makeN(2),
		"common":             {},
		"board":              {},
		"hl/hl-mcu-sdk/system/chip/puya/py32f539": makeN(3),
		"hl/hl-mcu-sdk/system/freertos/cherryusb": makeN(16),
	}
	globalOpts := makeN(8)

	m := testModel(sources, deps, opts, map[string]map[string]any{}, "/home/spock/git/hl-demo-clean", "arm-none-eabi", globalOpts, map[string]any{}, nil)
	m.language = languageChinese
	m.width = 116
	m.height = 30
	return m
}

func TestGraph_UniqueNodesAndTopologicalOrder(t *testing.T) {
	m := buildHLDemoCleanTestModel()

	seen := make(map[string]int)
	var order []string
	for _, n := range m.flat {
		seen[n.PkgName]++
		order = append(order, n.PkgName)
	}

	for _, pkg := range []string{"firmware", "target-app", "board-py32f539-evb", "lvgl-demos", "common", "board", "hl-demo-clean"} {
		if seen[pkg] != 1 {
			t.Fatalf("package %q appeared %d times in graph, want 1 (order=%v)", pkg, seen[pkg], order)
		}
	}

	indexOf := func(pkg string) int {
		for i, p := range order {
			if p == pkg {
				return i
			}
		}
		return -1
	}

	edges := [][2]string{
		{"firmware", "board-py32f539-evb"},
		{"firmware", "lvgl-demos"},
		{"firmware", "board"},
		{"target-app", "board-py32f539-evb"},
		{"target-app", "board"},
		{"board-py32f539-evb", "board"},
		{"lvgl-demos", "common"},
		{"common", "board"},
	}
	for _, e := range edges {
		if indexOf(e[0]) >= indexOf(e[1]) {
			t.Errorf("topological violation: caller %q (row %d) must precede dependency %q (row %d)", e[0], indexOf(e[0]), e[1], indexOf(e[1]))
		}
	}
}

func TestWorkbench_ThreeZonesTraversalAndLivePreview(t *testing.T) {
	base := buildHLDemoCleanTestModel()

	evalPreview := func(cfg *config.ConfigFile) (config.Preview, bool) {
		usbOn := false
		if cfg != nil && cfg.Entries != nil {
			if fw, ok := cfg.Entries["firmware"]; ok {
				if b, ok := fw.Options["usb"].(bool); ok && b {
					usbOn = true
				}
			}
		}
		active := map[string]bool{
			"hl-demo-clean":                true,
			"firmware":                     true,
			"board-py32f539-evb":           true,
			"lvgl-demos":                   true,
			"board":                        true,
			"hl/hl-mcu-sdk":                true,
			"hl/hl-mcu-sdk/system":         true,
			"hl/hl-mcu-sdk/system/uniface": true,
			"hl/hl-mcu-sdk/system/chip/puya/py32f539": true,
		}
		fwDeps := []string{
			"hl/hl-mcu-sdk",
			"hl/hl-mcu-sdk/system",
			"board",
			"board-py32f539-evb",
			"lvgl-demos",
		}
		if usbOn {
			active["hl/hl-mcu-sdk/system/freertos/cherryusb"] = true
			fwDeps = append(fwDeps, "hl/hl-mcu-sdk/system/freertos/cherryusb")
		}
		activeDeps := map[string][]string{
			"firmware":           fwDeps,
			"board-py32f539-evb": {"hl/hl-mcu-sdk", "hl/hl-mcu-sdk/system/uniface", "board", "hl/hl-mcu-sdk/system/chip/puya/py32f539"},
			"lvgl-demos":         {"hl/hl-mcu-sdk", "board"},
			"board":              {"hl/hl-mcu-sdk", "hl/hl-mcu-sdk/system/uniface"},
		}
		derived := map[string]map[string]any{
			"board-py32f539-evb": {"graphics": true},
		}
		owners := map[string]map[string]string{
			"board-py32f539-evb": {"graphics": "firmware"},
		}
		return config.Preview{
			Roots:          []string{"firmware", "hl-demo-clean"},
			ActivePackages: active,
			ActiveDeps:     activeDeps,
			DerivedOptions: derived,
			DerivedOwners:  owners,
		}, true
	}

	initPrev, _ := evalPreview(nil)
	m := NewModel(Input{
		WorkDir: "/home/spock/git/hl-demo-clean",
		Workbench: &config.Workbench{
			Packages:         mkSources("hl-demo-clean", "firmware", "target-app", "board", "board-py32f539-evb", "lvgl-demos", "common"),
			Deps:             base.deps,
			Options:          base.ed().Options,
			Values:           map[string]map[string]any{"lvgl-demos": {"opt": false}},
			FocusActive:      true,
			InitialPreview:   initPrev,
			Preview:          evalPreview,
			CurrentToolchain: "arm-none-eabi",
			GlobalOptions:    base.ed().GlobalOptions,
			GlobalValues:     map[string]any{},
		},
	})
	m.language = languageChinese
	m.width = 116
	m.height = 30

	treeTxt := stripAnsi(m.layout().treeContent)
	for _, want := range []string{"全局 (8)", "本地 (5)", "firmware (12)", "board-py32f539-evb (16)", "lvgl-demos (2)", "hl/hl-mcu-sdk (4)", "py32f539 (3)", "未启用 (3)"} {
		if !strings.Contains(treeTxt, want) {
			t.Fatalf("expected %q in active workbench tree:\n%s", want, treeTxt)
		}
	}
	for _, hidden := range []string{"target-app", "common", "cherryusb"} {
		if strings.Contains(treeTxt, hidden) {
			t.Fatalf("inactive package %q should be folded initially:\n%s", hidden, treeTxt)
		}
	}

	m.jumpToMatch("lvgl-demos", "")
	explicitView := stripAnsi(m.View())
	for _, want := range []string{"• 显式 1 项", "• opt"} {
		if !strings.Contains(explicitView, want) {
			t.Fatalf("expected persisted explicit marker %q in lvgl-demos view:\n%s", want, explicitView)
		}
	}

	m.jumpToMatch("firmware", "")
	fullView := stripAnsi(m.View())
	for _, want := range []string{"依赖关系 · firmware", "上游 (本包依赖)", "当前选中包", "下游 (依赖本包)", "选项 · firmware", "usb", "{}"} {
		if !strings.Contains(fullView, want) {
			t.Fatalf("expected %q simultaneously visible in workbench View():\n%s", want, fullView)
		}
	}

	m.handleKey(keyRunes("["))
	if m.selectedPkg != "board-py32f539-evb" {
		t.Fatalf("pressing [ from firmware should jump to its dependency board-py32f539-evb, got %q", m.selectedPkg)
	}
	boardView := stripAnsi(m.View())
	for _, want := range []string{"依赖关系 · board-py32f539-evb", "[← firmware]", "← 派生 1 项"} {
		if !strings.Contains(boardView, want) {
			t.Fatalf("expected %q when viewing board-py32f539-evb:\n%s", want, boardView)
		}
	}

	m.handleKey(keyRunes("]"))
	if m.selectedPkg != "firmware" {
		t.Fatalf("pressing ] should return to the dependent firmware, got %q", m.selectedPkg)
	}
	m.setValue("usb", true)
	afterToggleTree := stripAnsi(m.layout().treeContent)
	if !strings.Contains(afterToggleTree, "hl/hl-mcu-sdk (5)") {
		t.Fatalf("enabling usb should promote cherryusb into active hl/hl-mcu-sdk (5):\n%s", afterToggleTree)
	}

	m.handleKey(keyRunes("H"))
	hiddenEmptyTree := stripAnsi(m.layout().treeContent)
	if strings.Contains(hiddenEmptyTree, "board\n") || !strings.Contains(hiddenEmptyTree, "hl/hl-mcu-sdk (2)") {
		t.Fatalf("pressing H should hide active 0-option packages inside containers:\n%s", hiddenEmptyTree)
	}
	if m.jumpToMatch("does-not-exist", "") {
		t.Fatal("jumpToMatch on missing package must return false")
	}
	if !m.jumpToGraphNeighbor("board", true) || m.selectedPkg != "board" || m.hideEmptyPkgs {
		t.Fatalf("jumping to 0-option neighbor board with H active should unhide and select board, got selected=%q hideEmpty=%v", m.selectedPkg, m.hideEmptyPkgs)
	}
	m.handleKey(keyRunes("}"))
	if !m.downEdgeSelected {
		t.Fatal("pressing } should enable explicit downstream caller selection")
	}

	m.handleKey(keyRunes("z"))
	sdkGroup := containerGroupPkgName("hl/hl-mcu-sdk")
	for i, n := range m.flat {
		if n.PkgName == sdkGroup {
			m.treeCursor = i
			break
		}
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	// Advance past root leaf to the collapsed intermediate directory "system".
	foundSubgroup := false
	for i := m.treeCursor + 1; i < len(m.flat); i++ {
		if m.flat[i].IsGroup && m.flat[i].ContainerID == "" && !m.flat[i].Expanded {
			m.treeCursor = i
			foundSubgroup = true
			break
		}
	}
	if !foundSubgroup {
		t.Fatalf("expected collapsed intermediate subgroup under %s after z+Enter", sdkGroup)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.flat[m.treeCursor].Expanded {
		t.Fatal("Enter on intermediate subgroup must expand it")
	}

	m.jumpToMatch("firmware", "")
	m.View()
	clicked := false
	for _, box := range m.layout().egoBoxes {
		if box.pkgName == "lvgl-demos" {
			style := egoGraphPanelStyle(m.focusArea == 0, m.optionsPanelWidth())
			mx := m.layout().treeW + style.GetBorderLeftSize() + style.GetPaddingLeft() + box.x + 1
			my := m.layout().headerH + box.y
			m.handleMouse(tea.MouseMsg{X: mx, Y: my, Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			clicked = true
			break
		}
	}
	if !clicked {
		t.Fatal("expected lvgl-demos hitbox in firmware Ego-Graph")
	}
	if m.selectedPkg != "lvgl-demos" {
		t.Fatalf("clicking lvgl-demos card in Ego-Graph should select lvgl-demos, got %q", m.selectedPkg)
	}
}

func TestEgoGraph_UpstreamIsDependenciesDownstreamIsDependents(t *testing.T) {
	m := buildHLDemoCleanTestModel()
	m.width = 150
	m.height = 30

	m.jumpToMatch("hl/hl-mcu-sdk", "")
	ego := m.egoGraphData()
	if len(ego.Upstream) != 0 {
		t.Fatalf("hl/hl-mcu-sdk declares no dependencies, got %v", ego.Upstream)
	}
	if len(ego.Downstream) == 0 {
		t.Fatal("hl/hl-mcu-sdk must list the packages that depend on it")
	}
	badge := stripAnsi(m.renderEgoSummaryBadge(ego))
	if want := fmt.Sprintf("↑0 ↓%d", len(ego.Downstream)); badge != want {
		t.Fatalf("summary badge = %q, want %q", badge, want)
	}
	boxes := m.layout().egoBoxes
	callers := make(map[string]bool, len(ego.Downstream))
	for _, c := range ego.Downstream {
		callers[c.PkgName] = true
	}
	cards := 0
	for _, box := range boxes {
		if box.isMore {
			continue
		}
		if box.isUpstream {
			t.Fatalf("dependent %q rendered in the upstream column", box.pkgName)
		}
		if !callers[box.pkgName] {
			t.Fatalf("rendered dependent card %q is not a dependent of hl/hl-mcu-sdk", box.pkgName)
		}
		cards++
	}
	if cards == 0 {
		t.Fatal("hl/hl-mcu-sdk must render at least one dependent card")
	}
	view := stripAnsi(m.View())
	if !strings.Contains(view, "（无上游依赖）") {
		t.Fatalf("expected an empty upstream column for a package without dependencies:\n%s", view)
	}
	if !strings.Contains(view, "╟─") || !strings.Contains(view, "►[") || strings.Contains(view, "◄") || strings.Contains(view, "╢") {
		t.Fatalf("expected outward ╟ connector and rightward ► wires into dependents of hl/hl-mcu-sdk:\n%s", view)
	}

	m.jumpToMatch("firmware", "")
	ego = m.egoGraphData()
	if len(ego.Upstream) == 0 {
		t.Fatal("firmware declares dependencies")
	}
	if len(ego.Downstream) != 0 {
		t.Fatalf("nothing depends on firmware, got %v", ego.Downstream)
	}
	badge = stripAnsi(m.renderEgoSummaryBadge(ego))
	if want := fmt.Sprintf("↑%d ↓0", len(ego.Upstream)); badge != want {
		t.Fatalf("summary badge = %q, want %q", badge, want)
	}
	boxes = m.layout().egoBoxes
	deps := make(map[string]bool, len(ego.Upstream))
	for _, d := range ego.Upstream {
		deps[d.PkgName] = true
	}
	cards = 0
	for _, box := range boxes {
		if box.isMore {
			continue
		}
		if !box.isUpstream {
			t.Fatalf("dependency %q rendered in the downstream column", box.pkgName)
		}
		if !deps[box.pkgName] {
			t.Fatalf("rendered dependency card %q is not a dependency of firmware", box.pkgName)
		}
		cards++
	}
	if cards == 0 {
		t.Fatal("firmware must render at least one dependency card")
	}
	view = stripAnsi(m.View())
	if !strings.Contains(view, "（无下游依赖）") {
		t.Fatalf("expected an empty downstream column for a package nothing depends on:\n%s", view)
	}
	if !strings.Contains(view, "►╢") || strings.Contains(view, "◄") || strings.Contains(view, "╟") {
		t.Fatalf("expected rightward ► wire into outward ╢ connector on firmware:\n%s", view)
	}

	m.jumpToMatch("board-py32f539-evb", "")
	view = stripAnsi(m.View())
	if !strings.Contains(view, "►╢") || !strings.Contains(view, "╟─") || (!strings.Contains(view, "►[") && !strings.Contains(view, "►┤")) || strings.Contains(view, "◄") {
		t.Fatalf("expected middle node board-py32f539-evb to connect ►╢ on left and ╟─ ... ► on right:\n%s", view)
	}
}

func gutterRoleFor(m *Model, pkg string) gutterRole {
	roles := m.gutterRoles()
	for i, node := range m.flat {
		if node.PkgName == pkg {
			return roles[i]
		}
	}
	return gutterNone
}

func TestEgoGraph_SideMarkersAndControlsMatchColumns(t *testing.T) {
	if got, _ := sideNeighborGlyph(true, true, true); got != "▶" {
		t.Fatalf("active upstream glyph = %q, want ▶", got)
	}
	if got, _ := sideNeighborGlyph(true, true, false); got != "▶" {
		t.Fatalf("active downstream glyph = %q, want ▶", got)
	}
	if got, _ := sideNeighborGlyph(false, true, false); got != "▷" {
		t.Fatalf("inactive highlighted glyph = %q, want ▷", got)
	}
	if got, _ := sideNeighborGlyph(false, false, false); got != "◌" {
		t.Fatalf("inactive unhighlighted glyph = %q, want ◌", got)
	}

	m := buildHLDemoCleanTestModel()
	m.width = 150
	m.height = 30

	m.jumpToMatch("hl/hl-mcu-sdk", "")
	if got := gutterRoleFor(&m, "firmware"); got != gutterDownstream {
		t.Fatalf("firmware row role in dependents view = %v, want gutterDownstream", got)
	}
	m.handleKey(keyRunes("}"))
	if !m.downEdgeSelected || m.downEdgeIdx != 1 {
		t.Fatalf("pressing } must cycle downstream edges: selected=%v idx=%d", m.downEdgeSelected, m.downEdgeIdx)
	}

	m.jumpToMatch("firmware", "")
	if got := gutterRoleFor(&m, "board"); got != gutterUpstream {
		t.Fatalf("board row role in dependency view = %v, want gutterUpstream", got)
	}
	m.handleKey(keyRunes("{"))
	if m.upEdgeIdx != 1 {
		t.Fatalf("pressing { must cycle upstream edges, got idx=%d", m.upEdgeIdx)
	}

	s := m.layout()
	leftX := s.treeW + 1
	if leftX >= s.treeW+s.optionsW/2 {
		t.Fatalf("test viewport too narrow for two ego halves: treeW=%d optionsW=%d", s.treeW, s.optionsW)
	}
	m.handleMouse(mouseTestWheel(leftX, s.headerH+1, true))
	if m.upEdgeIdx != 2 {
		t.Fatalf("wheel on the upstream half changed upEdgeIdx to %d, want 2", m.upEdgeIdx)
	}
	m.downEdgeSelected = false
	m.downEdgeIdx = 0
	m.handleMouse(mouseTestWheel(s.treeW+s.optionsW/2+1, s.headerH+1, true))
	if !m.downEdgeSelected || m.downEdgeIdx != 1 {
		t.Fatalf("wheel on the downstream half: selected=%v idx=%d, want true/1", m.downEdgeSelected, m.downEdgeIdx)
	}
}

func TestEgoGraph_MoreRowClicksCycleCorrectSide(t *testing.T) {
	m := buildHLDemoCleanTestModel()
	m.width = 150
	m.height = 22

	m.jumpToMatch("hl/hl-mcu-sdk", "")
	s := m.layout()
	var more *egoNodeHitbox
	for i := range s.egoBoxes {
		if s.egoBoxes[i].isMore && !s.egoBoxes[i].isUpstream {
			more = &s.egoBoxes[i]
			break
		}
	}
	if more == nil {
		t.Fatalf("expected a downstream more row in the ego graph:\n%s", stripAnsi(m.View()))
	}
	m.handleMouse(mouseTestClick(s.graphLeft+more.x+1, s.headerH+more.y))
	if !m.downEdgeSelected || m.downEdgeIdx != 1 {
		t.Fatalf("clicking the downstream more row: selected=%v idx=%d, want true/1", m.downEdgeSelected, m.downEdgeIdx)
	}

	m.jumpToMatch("firmware", "")
	s = m.layout()
	more = nil
	for i := range s.egoBoxes {
		if s.egoBoxes[i].isMore && s.egoBoxes[i].isUpstream {
			more = &s.egoBoxes[i]
			break
		}
	}
	if more == nil {
		t.Fatalf("expected an upstream more row in the ego graph:\n%s", stripAnsi(m.View()))
	}
	m.handleMouse(mouseTestClick(s.graphLeft+more.x+1, s.headerH+more.y))
	if m.upEdgeIdx != 1 {
		t.Fatalf("clicking the upstream more row changed upEdgeIdx to %d, want 1", m.upEdgeIdx)
	}
}

func TestEgoGraph_EnglishColumnLabels(t *testing.T) {
	m := buildHLDemoCleanTestModel()
	m.width = 150
	m.height = 30
	m.language = languageEnglish

	m.jumpToMatch("hl/hl-mcu-sdk", "")
	view := stripAnsi(m.View())
	for _, want := range []string{"Upstream (Depends On)", "Downstream (Used By)", "(no dependencies)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in English ego graph:\n%s", want, view)
		}
	}

	m.jumpToMatch("firmware", "")
	view = stripAnsi(m.View())
	for _, want := range []string{"Upstream (Depends On)", "Downstream (Used By)", "(no callers)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in English ego graph:\n%s", want, view)
		}
	}
}

func TestTreeLayout_DistinguishesActiveNoOptAndInactiveEdges(t *testing.T) {
	base := buildHLDemoCleanTestModel()
	initPrev := config.Preview{
		Roots: []string{"firmware"},
		ActivePackages: map[string]bool{
			"firmware":           true,
			"board-py32f539-evb": true,
			"board":              true,
			"hl/hl-mcu-sdk":      true,
		},
		ActiveDeps: map[string][]string{
			"firmware":           {"hl/hl-mcu-sdk", "board", "board-py32f539-evb"},
			"board-py32f539-evb": {"hl/hl-mcu-sdk", "board"},
			"board":              {"hl/hl-mcu-sdk"},
		},
	}
	m := NewModel(Input{
		WorkDir: "/home/spock/git/hl-demo-clean",
		Workbench: &config.Workbench{
			Packages:         mkSources("hl-demo-clean", "firmware", "target-app", "board", "board-py32f539-evb", "lvgl-demos", "common"),
			Deps:             base.deps,
			Options:          base.ed().Options,
			FocusActive:      true,
			InitialPreview:   initPrev,
			CurrentToolchain: "arm-none-eabi",
			GlobalOptions:    base.ed().GlobalOptions,
			GlobalValues:     map[string]any{},
		},
	})
	m.language = languageChinese
	m.width = 140
	m.height = 40
	m.expandAll()

	t.Run("firmware_upstream_and_unrelated", func(t *testing.T) {
		m.jumpToMatch("firmware", "")
		m.focusArea = 1
		tree := stripAnsi(m.layout().treeContent)
		if !strings.Contains(tree, "◌ py32f539") {
			t.Fatalf("inactive unrelated package py32f539 should use ◌:\n%s", tree)
		}
		if !strings.Contains(tree, "◁ lvgl-demos") || !strings.Contains(tree, "◁ cherryusb") {
			t.Fatalf("inactive upstream dependencies lvgl-demos and cherryusb should use ◁:\n%s", tree)
		}
	})

	t.Run("sdk_active_and_inactive_downstream", func(t *testing.T) {
		m.jumpToMatch("hl/hl-mcu-sdk", "")
		m.focusArea = 1
		tree := stripAnsi(m.layout().treeContent)
		if !strings.Contains(tree, "◆ hl-mcu-sdk") {
			t.Fatalf("selected hl/hl-mcu-sdk leaf should show ◆:\n%s", tree)
		}
		if !strings.Contains(tree, "▶ board") {
			t.Fatalf("active 0-option downstream board should show ▶:\n%s", tree)
		}
		if !strings.Contains(tree, "▷ target-app") {
			t.Fatalf("inactive downstream target-app should show ▷:\n%s", tree)
		}
		if !strings.Contains(tree, "◌ cherryusb") {
			t.Fatalf("inactive unrelated cherryusb should show ◌:\n%s", tree)
		}
	})

	t.Run("global_active_no_opt_vs_inactive", func(t *testing.T) {
		m.jumpToMatch(GlobalPkgName, "")
		m.focusArea = 1
		tree := stripAnsi(m.layout().treeContent)
		if !strings.Contains(tree, "○ hl-mcu-sdk") {
			t.Fatalf("active 0-option package hl/hl-mcu-sdk should use ○ (not ◌):\n%s", tree)
		}
		if !strings.Contains(tree, "○ board") {
			t.Fatalf("active 0-option package board should use ○ (not ◌):\n%s", tree)
		}
		if !strings.Contains(tree, "◌ target-app") {
			t.Fatalf("inactive package target-app should use ◌:\n%s", tree)
		}
	})
}

func TestWorkbench_ContainerGroupingSummaryAndNoFakeRoot(t *testing.T) {
	// Container hl/hl-mcu-sdk has NO root build.go — only real member packages.
	sources := mkSources("//firmware", "//board")
	deps := map[string][]string{
		"//firmware": {"//board", "hl/hl-mcu-sdk/system/chip/puya/py32f539"},
		"//board":    {"hl/hl-mcu-sdk/system/uniface"},
	}
	opts := map[string]map[string]*api.Option{
		"//firmware": {"wifi": mkOpt("wifi", api.OptionBool, true)},
		"//board":    {},
		"hl/hl-mcu-sdk/system/chip/puya/py32f539": {"clock": mkOpt("clock", api.OptionInt, 48)},
		"hl/hl-mcu-sdk/system/uniface":            {},
	}
	wb := &config.Workbench{
		Packages: sources,
		Deps:     deps,
		Containers: map[string]config.WorkbenchContainer{
			"//":            {ID: "//", Kind: "local"},
			"hl/hl-mcu-sdk": {ID: "hl/hl-mcu-sdk", Kind: "native", Version: "1.2.0", Commit: "0123456789abcdef"},
		},
		PackageContainer: map[string]string{
			"//firmware":                              "//",
			"//board":                                 "//",
			"hl/hl-mcu-sdk/system/chip/puya/py32f539": "hl/hl-mcu-sdk",
			"hl/hl-mcu-sdk/system/uniface":            "hl/hl-mcu-sdk",
		},
		Options:     opts,
		FocusActive: true,
		InitialPreview: config.Preview{
			Roots: []string{"//firmware"},
			ActivePackages: map[string]bool{
				"//firmware":                              true,
				"//board":                                 true,
				"hl/hl-mcu-sdk/system/chip/puya/py32f539": true,
				"hl/hl-mcu-sdk/system/uniface":            true,
			},
			ActiveDeps: deps,
		},
		GlobalOptions: map[string]*api.Option{"mode": mkOpt("mode", api.OptionChoice, "debug").SetValues("debug", "release")},
	}
	m := NewModel(Input{Workbench: wb, WorkDir: "/project"})
	m.language = languageChinese
	m.width = 120
	m.height = 30

	treeTxt := stripAnsi(m.layout().treeContent)
	for _, want := range []string{"本地 (2)", "firmware (1)", "○ board", "hl/hl-mcu-sdk (2)", "py32f539 (1)", "○ uniface"} {
		if !strings.Contains(treeTxt, want) {
			t.Fatalf("expected %q in 2.0 workbench tree:\n%s", want, treeTxt)
		}
	}
	if strings.Contains(treeTxt, "hl/hl-mcu-sdk/system") {
		t.Fatalf("container header must not absorb child directory segment:\n%s", treeTxt)
	}
	if strings.Contains(treeTxt, "未启用") {
		t.Fatalf("active 0-option packages must not create an Inactive group:\n%s", treeTxt)
	}

	// First select a real package, then move cursor to the container header.
	m.jumpToMatch("//firmware", "")
	if m.selectedPkg != "//firmware" {
		t.Fatalf("selectedPkg = %q, want //firmware", m.selectedPkg)
	}
	sdkIdx := -1
	for i, n := range m.flat {
		if n.ContainerID == "hl/hl-mcu-sdk" {
			sdkIdx = i
			break
		}
	}
	if sdkIdx < 0 {
		t.Fatalf("container node hl/hl-mcu-sdk not found in flat tree")
	}
	m.moveTreeCursor(sdkIdx - m.treeCursor)
	if m.selectedPkg != "" {
		t.Fatalf("moving cursor to container header left stale selectedPkg = %q", m.selectedPkg)
	}
	summaryView := stripAnsi(m.View())
	for _, want := range []string{"模块容器 · hl/hl-mcu-sdk", "native", "1.2.0 (0123456789ab)", "2 个生效 / 共 2 个（1 个可配置）"} {
		if !strings.Contains(summaryView, want) {
			t.Fatalf("expected %q in container summary view:\n%s", want, summaryView)
		}
	}
	if ego := m.egoGraphData(); !ego.IsGroup || len(ego.Upstream) != 0 || len(ego.Downstream) != 0 {
		t.Fatalf("container header ego-graph should be empty group view, got %+v", ego)
	}

	// Pressing → / [ / ] on a container header must NOT focus stale options or step stale edges.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.focusArea != 0 {
		t.Fatalf("pressing Right on expanded container header focused options panel (focusArea=%d)", m.focusArea)
	}
	m.handleKey(keyRunes("["))
	m.handleKey(keyRunes("]"))
	if m.selectedPkg != "" || m.treeCursor != sdkIdx {
		t.Fatalf("pressing [ or ] on container header jumped to stale package: selectedPkg=%q cursor=%d", m.selectedPkg, m.treeCursor)
	}

	// Pressing H hides active 0-option packages (board, uniface) cleanly.
	m.handleKey(keyRunes("H"))
	hiddenTree := stripAnsi(m.layout().treeContent)
	if strings.Contains(hiddenTree, "board") || strings.Contains(hiddenTree, "uniface") {
		t.Fatalf("H should hide active 0-option packages:\n%s", hiddenTree)
	}
	if !strings.Contains(hiddenTree, "本地 (1)") || !strings.Contains(hiddenTree, "hl/hl-mcu-sdk (1)") {
		t.Fatalf("H should update container counts to configurable active packages:\n%s", hiddenTree)
	}
}
