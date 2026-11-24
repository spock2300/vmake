package tui

import (
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

	evalPreview := func(cfg *config.ConfigFile) (PreviewState, bool) {
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
		return PreviewState{
			Roots:          []string{"firmware", "hl-demo-clean"},
			ActivePackages: active,
			ActiveDeps:     activeDeps,
			DerivedOptions: derived,
			DerivedOwners:  owners,
		}, true
	}

	initPrev, _ := evalPreview(nil)
	m := NewModel(Input{
		Packages:         mkSources("hl-demo-clean", "firmware", "target-app", "board", "board-py32f539-evb", "lvgl-demos", "common"),
		Deps:             base.deps,
		Options:          base.ed().Options,
		Values:           map[string]map[string]any{"lvgl-demos": {"opt": false}},
		FocusActive:      true,
		InitialPreview:   initPrev,
		Preview:          evalPreview,
		WorkDir:          "/home/spock/git/hl-demo-clean",
		CurrentToolchain: "arm-none-eabi",
		GlobalOptions:    base.ed().GlobalOptions,
		GlobalValues:     map[string]any{},
	})
	m.language = languageChinese
	m.width = 116
	m.height = 30

	treeTxt := stripAnsi(m.layout().treeContent)
	for _, want := range []string{"全局 (8)", "firmware (12)", "board-py32f539-evb (16)", "lvgl-demos (2)", "远程 (1)", "py32f539 (3)", "未启用 / 无选项"} {
		if !strings.Contains(treeTxt, want) {
			t.Fatalf("expected %q in active workbench tree:\n%s", want, treeTxt)
		}
	}
	for _, hidden := range []string{"hl-demo-clean", "target-app", "common", "cherryusb"} {
		if strings.Contains(treeTxt, hidden) {
			t.Fatalf("inactive/0-option package %q should be folded initially:\n%s", hidden, treeTxt)
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
	for _, want := range []string{"依赖关系 · firmware", "上游 (被谁依赖)", "当前选中包", "下游 (直接依赖)", "选项 · firmware", "usb", "{}"} {
		if !strings.Contains(fullView, want) {
			t.Fatalf("expected %q simultaneously visible in workbench View():\n%s", want, fullView)
		}
	}

	m.handleKey(keyRunes("]"))
	if m.selectedPkg != "board-py32f539-evb" {
		t.Fatalf("pressing ] from firmware should jump to board-py32f539-evb, got %q", m.selectedPkg)
	}
	boardView := stripAnsi(m.View())
	for _, want := range []string{"依赖关系 · board-py32f539-evb", "[← firmware]", "← 派生 1 项"} {
		if !strings.Contains(boardView, want) {
			t.Fatalf("expected %q when viewing board-py32f539-evb:\n%s", want, boardView)
		}
	}

	m.handleKey(keyRunes("["))
	if m.selectedPkg != "firmware" {
		t.Fatalf("pressing [ should return to firmware, got %q", m.selectedPkg)
	}
	m.setValue("usb", true)
	afterToggleTree := stripAnsi(m.layout().treeContent)
	if !strings.Contains(afterToggleTree, "远程 (2)") {
		t.Fatalf("enabling usb should promote cherryusb into active 远程 (2):\n%s", afterToggleTree)
	}

	m.handleKey(keyRunes("H"))
	hiddenEmptyTree := stripAnsi(m.layout().treeContent)
	if strings.Contains(hiddenEmptyTree, "未启用 / 无选项") || !strings.Contains(hiddenEmptyTree, "未启用") {
		t.Fatalf("pressing H should rename group to 未启用 when 0-option packages are hidden:\n%s", hiddenEmptyTree)
	}
	if m.jumpToMatch("does-not-exist", "") {
		t.Fatal("jumpToMatch on missing package must return false")
	}
	if !m.jumpToGraphNeighbor("board", true) || m.selectedPkg != "board" || m.hideEmptyPkgs {
		t.Fatalf("jumping to 0-option neighbor board with H active should unhide and select board, got selected=%q hideEmpty=%v", m.selectedPkg, m.hideEmptyPkgs)
	}
	m.handleKey(keyRunes("{"))
	if !m.upEdgeSelected {
		t.Fatal("pressing { should enable explicit upstream caller selection")
	}

	m.handleKey(keyRunes("z"))
	for i, n := range m.flat {
		if n.PkgName == RemoteGroupPkgName {
			m.treeCursor = i
			break
		}
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.treeCursor >= len(m.flat) || !m.flat[m.treeCursor].IsGroup {
		t.Fatalf("down arrow after expanding Remote must land on collapsed intermediate subgroup, got cursor=%d", m.treeCursor)
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
