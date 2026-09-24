package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/spock2300/vmake/pkg/api"
)

func mouseTestModel(opts map[string]*api.Option) Model {
	m := NewModel(mkSources("app"), nil, map[string]map[string]*api.Option{"app": opts}, map[string]map[string]any{}, "/w", "host", nil, nil, nil)
	m.language = languageEnglish
	m.width, m.height = 100, 28
	m.jumpToMatch("app", "")
	return m
}

func mouseTestPosition(t *testing.T, view, text string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(stripAnsi(view), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return lipgloss.Width(line[:i]), y
		}
	}
	t.Fatalf("rendered view does not contain %q:\n%s", text, stripAnsi(view))
	return 0, 0
}

func mouseTestClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
}

func mouseTestWheel(x, y int, down bool) tea.MouseMsg {
	if down {
		return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
	}
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress}
}

func mouseTestClickText(t *testing.T, m *Model, text string) tea.Cmd {
	t.Helper()
	x, y := mouseTestPosition(t, m.View(), text)
	_, cmd := m.Update(mouseTestClick(x, y))
	return cmd
}

func TestMouseOptionActions(t *testing.T) {
	for _, typ := range []api.OptionType{api.OptionBool, api.OptionChoice, api.OptionString, api.OptionInt} {
		t.Run(typ.String(), func(t *testing.T) {
			var def any
			switch typ {
			case api.OptionBool:
				def = false
			case api.OptionChoice:
				def = "first-value"
			case api.OptionString:
				def = "original-value"
			case api.OptionInt:
				def = 12
			}
			opt := mkOpt("target_option", typ, def)
			if typ == api.OptionChoice {
				opt.SetValues("first-value", "second-value")
			}
			m := mouseTestModel(map[string]*api.Option{
				"first_option":  mkOpt("first_option", api.OptionBool, false),
				"target_option": opt,
			})
			mouseTestClickText(t, &m, "target_option")
			if m.focusArea != 1 || m.optCursor != 1 {
				t.Fatalf("click did not select target: focus=%d cursor=%d", m.focusArea, m.optCursor)
			}
			if m.getValue("first_option") != false {
				t.Fatal("click changed a different option")
			}
			switch typ {
			case api.OptionBool:
				if m.getValue("target_option") != true || !m.hasChanges {
					t.Fatal("bool click did not toggle and mark changes")
				}
			case api.OptionChoice:
				if m.overlay != overlayChoice || m.choiceOpt != "target_option" || m.getValue("target_option") != def {
					t.Fatal("choice click should open its selector without changing the value")
				}
				mouseTestClickText(t, &m, "second-value")
				if m.overlay != overlayNone || m.getValue("target_option") != "second-value" {
					t.Fatal("clicking a choice did not commit and close")
				}
			case api.OptionString, api.OptionInt:
				if !m.editing || m.editInput != fmt.Sprint(def) || m.getValue("target_option") != def {
					t.Fatalf("click did not begin editing current value: editing=%t input=%q", m.editing, m.editInput)
				}
				m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlA})
				m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlK})
				m.handleKey(keyRunes("7"))
				m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
				want := any("7")
				if typ == api.OptionInt {
					want = 7
				}
				if m.editing || m.getValue("target_option") != want {
					t.Fatalf("keyboard commit after mouse edit = %v, want %v", m.getValue("target_option"), want)
				}
			}
		})
	}
}

func TestMouseOptionWholeRowAndGroupMapping(t *testing.T) {
	m := mouseTestModel(map[string]*api.Option{
		"first_bool":  mkOpt("first_bool", api.OptionBool, false).SetGroup("A group"),
		"second_bool": mkOpt("second_bool", api.OptionBool, false).SetGroup("B group").SetDescription("click description"),
	})
	mouseTestClickText(t, &m, "B group")
	if m.focusArea != 1 || m.hasChanges || m.optCursor != 0 {
		t.Fatalf("group click must only focus options: focus=%d changed=%t cursor=%d", m.focusArea, m.hasChanges, m.optCursor)
	}
	mouseTestClickText(t, &m, "click description")
	if m.optCursor != 1 || m.getValue("second_bool") != true || m.getValue("first_bool") != false {
		t.Fatal("description click did not operate on its option across group headers")
	}
	x, y := mouseTestPosition(t, m.View(), "second_bool")
	m.Update(mouseTestClick(x-1, y))
	if m.getValue("second_bool") != false {
		t.Fatal("click on row padding did not toggle the option")
	}
	_, y = mouseTestPosition(t, m.View(), "second_bool")
	m.Update(mouseTestClick(x, y+1))
	if m.getValue("second_bool") != false || m.optCursor != 1 {
		t.Fatal("blank row changed the selected option")
	}
}

func TestMouseVisibleOptionsIgnoreHiddenRows(t *testing.T) {
	m := mouseTestModel(map[string]*api.Option{
		"a_hidden": mkOpt("a_hidden", api.OptionBool, false).SetGroup("A group").SetShowIf(func(*api.ConfigContext) bool { return false }),
		"b_target": mkOpt("b_target", api.OptionBool, false).SetGroup("B group"),
	})
	mouseTestClickText(t, &m, "b_target")
	if m.optCursor != 0 || m.getValue("b_target") != true || m.getValue("a_hidden") != false {
		t.Fatal("hidden option or its group displaced the visible option hitbox")
	}
}

func TestMouseOptionRowsWithLongText(t *testing.T) {
	m := mouseTestModel(map[string]*api.Option{
		"a_long": mkOpt("a_long", api.OptionString, "a very long initial value that exceeds the panel width").SetDescription(strings.Repeat("较长的中文描述", 8)),
		"target": mkOpt("target", api.OptionBool, false),
	})
	m.width = 60
	mouseTestClickText(t, &m, "target")
	if m.getValue("target") != true || m.editing {
		t.Fatal("preceding long option text displaced the target option hitbox")
	}
}

func TestMousePresetAndMenuconfigActions(t *testing.T) {
	for _, withOptions := range []bool{false, true} {
		t.Run(fmt.Sprintf("options=%t", withOptions), func(t *testing.T) {
			opts := map[string]*api.Option{}
			if withOptions {
				opts["enabled"] = mkOpt("enabled", api.OptionBool, false).SetGroup("Build")
			}
			m := mouseTestModel(opts)
			entry := (&api.KConfigEntry{}).AddPreset("first_defconfig").AddPreset("second_defconfig").SelectPreset("first_defconfig")
			m.kconfigs = map[string][]*api.KConfigEntry{"app": {entry}}
			mouseTestClickText(t, &m, "first_defconfig")
			if m.currentPreset() != "second_defconfig" || m.presetValues["app"] != "second_defconfig" || m.focusArea != 1 || !m.hasChanges {
				t.Fatalf("preset click did not cycle and mark changes: preset=%q focus=%d changes=%t", m.currentPreset(), m.focusArea, m.hasChanges)
			}
			cmd := mouseTestClickText(t, &m, "menuconfig")
			if cmd == nil || !m.runningMenuconfig {
				t.Fatal("menuconfig click did not start its command flow")
			}
		})
	}
	t.Run("menuconfig_without_presets", func(t *testing.T) {
		m := mouseTestModel(nil)
		m.kconfigs = map[string][]*api.KConfigEntry{"app": {&api.KConfigEntry{}}}
		if cmd := mouseTestClickText(t, &m, "menuconfig"); cmd == nil || !m.runningMenuconfig {
			t.Fatal("menuconfig-only row is not clickable")
		}
	})
}

func TestMousePresetChangesSurviveOptionResetAndClearOnRevert(t *testing.T) {
	m := mouseTestModel(map[string]*api.Option{"enabled": mkOpt("enabled", api.OptionBool, false)})
	entry := (&api.KConfigEntry{}).AddPreset("first_defconfig").AddPreset("second_defconfig").SelectPreset("first_defconfig")
	m.kconfigs = map[string][]*api.KConfigEntry{"app": {entry}}
	mouseTestClickText(t, &m, "first_defconfig")
	if !m.hasChanges {
		t.Fatal("changing the preset did not mark unsaved changes")
	}
	mouseTestClickText(t, &m, "enabled")
	m.handleKey(keyRunes("r"))
	if m.getValue("enabled") != false || m.currentPreset() != "second_defconfig" || !m.hasChanges {
		t.Fatal("resetting an option cleared the pending preset change")
	}
	mouseTestClickText(t, &m, "second_defconfig")
	if m.currentPreset() != "first_defconfig" || m.hasChanges {
		t.Fatal("cycling back to the original preset did not clear unsaved changes")
	}
}

func TestMouseEditingClickAndWheelBehavior(t *testing.T) {
	for _, outside := range []string{"other_option", "header", "footer"} {
		t.Run(outside, func(t *testing.T) {
			m := mouseTestModel(map[string]*api.Option{
				"edit_option":  mkOpt("edit_option", api.OptionString, "initial"),
				"other_option": mkOpt("other_option", api.OptionBool, false),
			})
			mouseTestClickText(t, &m, "edit_option")
			m.handleKey(keyRunes(" pending"))
			input, cursor := m.editInput, m.editCursor
			mouseTestClickText(t, &m, "edit_option")
			x, y := mouseTestPosition(t, m.View(), "edit_option")
			m.Update(mouseTestWheel(x, y, true))
			if !m.editing || m.editInput != input || m.editCursor != cursor {
				t.Fatal("same-row click or wheel changed editing state or input cursor")
			}
			switch outside {
			case "other_option":
				mouseTestClickText(t, &m, "other_option")
			case "header":
				m.Update(mouseTestClick(0, 0))
			case "footer":
				m.Update(mouseTestClick(0, m.height-1))
			}
			if m.editing || m.getValue("edit_option") != "initial" || m.getValue("other_option") != false || m.hasChanges {
				t.Fatal("outside click must cancel input without applying another action")
			}
		})
	}
}

func TestMouseEditingCursorVisibleInSmallTerminal(t *testing.T) {
	for _, size := range [][2]int{{50, 9}, {60, 8}} {
		for _, lang := range []language{languageEnglish, languageChinese} {
			for valueIndex, initial := range []string{
				strings.Repeat("long initial value ", 8),
				strings.Repeat("很长的中文初始值", 8),
			} {
				t.Run(fmt.Sprintf("%dx%d/language=%d/value=%d", size[0], size[1], lang, valueIndex), func(t *testing.T) {
					m := mouseTestModel(map[string]*api.Option{"long_option_string": mkOpt("long_option_string", api.OptionString, initial)})
					m.language = lang
					m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					mouseTestClickText(t, &m, "long_option_string")
					checkCursor := func() {
						t.Helper()
						view := m.View()
						x, y := mouseTestPosition(t, view, "▎")
						if x < 0 || x >= m.width || y < 0 || y >= m.height {
							t.Fatalf("input cursor (%d,%d) is outside %dx%d screen:\n%s", x, y, m.width, m.height, stripAnsi(view))
						}
						input, cursor := m.editInput, m.editCursor
						m.Update(mouseTestClick(x, y))
						if !m.editing || m.editInput != input || m.editCursor != cursor {
							t.Fatal("clicking the visible edit continuation cancelled or changed the input")
						}
						if m.getValue("long_option_string") != initial || m.hasChanges {
							t.Fatal("cursor movement or continuation click committed an edit")
						}
					}
					checkCursor()
					for _, key := range []tea.KeyType{tea.KeyLeft, tea.KeyHome, tea.KeyRight, tea.KeyEnd} {
						m.handleKey(tea.KeyMsg{Type: key})
						checkCursor()
					}
				})
			}
		}
	}
}

func TestMouseIgnoresReleaseMotionAndOtherButtons(t *testing.T) {
	for _, event := range []tea.MouseMsg{
		{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease},
		{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
		{Type: tea.MouseRelease, Action: tea.MouseActionRelease},
		{Type: tea.MouseRight, Button: tea.MouseButtonRight, Action: tea.MouseActionPress},
		{Type: tea.MouseMiddle, Button: tea.MouseButtonMiddle, Action: tea.MouseActionPress},
	} {
		t.Run(event.String(), func(t *testing.T) {
			m := mouseTestModel(map[string]*api.Option{"enabled": mkOpt("enabled", api.OptionBool, false)})
			event.X, event.Y = mouseTestPosition(t, m.View(), "enabled")
			m.Update(event)
			if m.getValue("enabled") != false || m.hasChanges || m.focusArea != 0 {
				t.Fatal("non-click mouse event activated the option row")
			}
		})
	}
}

func TestMouseOptionsAfterResizeAndLongHeader(t *testing.T) {
	for _, lang := range []language{languageEnglish, languageChinese} {
		t.Run(fmt.Sprintf("language=%d", lang), func(t *testing.T) {
			m := mouseTestModel(map[string]*api.Option{
				"first":  mkOpt("first", api.OptionBool, false),
				"second": mkOpt("second", api.OptionBool, false),
			})
			m.language = lang
			for _, size := range [][2]int{{60, 8}, {100, 28}, {50, 9}} {
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.optCursor = 1
				before := m.getValue("second")
				mouseTestClickText(t, &m, "second")
				if m.getValue("second") == before || m.getValue("first") != false {
					t.Fatalf("%dx%d: click missed visible selected option", size[0], size[1])
				}
			}
			m.workDir = "/home/用户/项目/嵌入式固件/very/deeply/nested/build/working/directory"
			m.Update(tea.WindowSizeMsg{Width: 60, Height: 28})
			before := m.getValue("second")
			mouseTestClickText(t, &m, "second")
			if m.getValue("second") == before {
				t.Fatal("wrapped header displaced option click")
			}
		})
	}
}

func TestMouseOptionsWheelScrollsRenderedRows(t *testing.T) {
	opts := make(map[string]*api.Option)
	for i := 0; i < 18; i++ {
		name := fmt.Sprintf("option_%02d", i)
		opts[name] = mkOpt(name, api.OptionBool, false).SetGroup(fmt.Sprintf("Group %02d", i/2))
	}
	m := mouseTestModel(opts)
	m.height = 14
	x, y := mouseTestPosition(t, m.View(), "option_00")
	m.Update(mouseTestWheel(x, y, true))
	m.View()
	if m.optOff <= 0 || m.focusArea != 1 || m.treeOff != 0 || m.hasChanges {
		t.Fatalf("option wheel did not scroll its panel: off=%d focus=%d treeOff=%d", m.optOff, m.focusArea, m.treeOff)
	}
	for range 100 {
		m.Update(mouseTestWheel(x, y, true))
		m.View()
	}
	lastOff := m.optOff
	m.Update(mouseTestWheel(x, y, true))
	if m.optOff != lastOff {
		t.Fatal("option wheel exceeded its lower boundary")
	}
	view := stripAnsi(m.View())
	for row, line := range strings.Split(view, "\n") {
		if i := strings.Index(line, "/18 "); i >= 0 {
			cursor := m.optCursor
			m.Update(mouseTestClick(lipgloss.Width(line[:i]), row))
			if m.hasChanges || m.optCursor != cursor {
				t.Fatal("scroll indicator click activated an option")
			}
			break
		}
	}
	mouseTestClickText(t, &m, "option_17")
	if m.getValue("option_17") != true {
		t.Fatal("scrolled option click did not map past group rows")
	}
	for range 100 {
		m.Update(mouseTestWheel(x, y, false))
		m.View()
	}
	if m.optOff != 0 {
		t.Fatalf("wheel up did not reach first option rows: off=%d", m.optOff)
	}
	mouseTestPosition(t, m.View(), "option_00")
}

func TestMouseTreeWheelAndFilteredRows(t *testing.T) {
	var names []string
	opts := map[string]map[string]*api.Option{}
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("pkg_%02d", i)
		names = append(names, name)
		opts[name] = map[string]*api.Option{"enabled": mkOpt("enabled", api.OptionBool, false)}
	}
	m := NewModel(mkSources(names...), nil, opts, map[string]map[string]any{}, "/w", "host", nil, nil, nil)
	m.language, m.width, m.height, m.focusArea = languageEnglish, 100, 14, 1
	x, y := mouseTestPosition(t, m.View(), "pkg_00")
	m.Update(mouseTestWheel(x, y, true))
	m.View()
	if m.treeOff <= 0 || m.focusArea != 0 || m.optOff != 0 {
		t.Fatalf("tree wheel did not scroll its panel: treeOff=%d focus=%d optOff=%d", m.treeOff, m.focusArea, m.optOff)
	}
	for range 100 {
		m.Update(mouseTestWheel(x, y, true))
		m.View()
	}
	lastOff := m.treeOff
	m.Update(mouseTestWheel(x, y, true))
	if m.treeOff != lastOff {
		t.Fatal("tree wheel exceeded its lower boundary")
	}
	mouseTestClickText(t, &m, "pkg_19")
	if m.selectedPkg != "pkg_19" {
		t.Fatalf("scrolled leaf click selected %q", m.selectedPkg)
	}
	for range 100 {
		m.Update(mouseTestWheel(x, y, false))
		m.View()
	}
	if m.treeOff != 0 {
		t.Fatalf("tree wheel up did not reach the start: off=%d", m.treeOff)
	}
	m.filterInput = "pkg_0"
	m.rebuildFlat()
	m.treeOff = 0
	mouseTestClickText(t, &m, "pkg_02")
	if m.selectedPkg != "pkg_02" {
		t.Fatalf("filter bar displaced tree click: selected %q", m.selectedPkg)
	}
}

func TestMouseTreeExpansionAtRenderedPositions(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"parent_pkg": {"enabled": mkOpt("enabled", api.OptionBool, false)},
		"child_pkg":  {"enabled": mkOpt("enabled", api.OptionBool, false)},
	}
	m := NewModel(mkSources("parent_pkg", "child_pkg"), map[string][]string{"parent_pkg": {"child_pkg"}}, opts, map[string]map[string]any{}, "/home/用户/项目/very/deeply/nested/working/directory", "host", nil, nil, nil)
	m.language, m.width, m.height = languageChinese, 60, 28
	mouseTestClickText(t, &m, "parent_pkg")
	if m.selectedPkg != "parent_pkg" || !m.flat[m.treeCursor].Expanded {
		t.Fatal("parent click did not select and expand")
	}
	mouseTestClickText(t, &m, "child_pkg")
	if m.selectedPkg != "child_pkg" || m.flat[m.treeCursor].Expanded {
		t.Fatal("leaf click should select without expansion")
	}
	x, y := mouseTestPosition(t, m.View(), "parent_pkg")
	m.Update(mouseTestClick(x, y))
	if m.selectedPkg != "parent_pkg" || m.flat[m.treeCursor].Expanded {
		t.Fatal("second parent click did not collapse")
	}
}

func TestMouseChoiceAndLanguageOverlays(t *testing.T) {
	for _, lang := range []language{languageEnglish, languageChinese} {
		t.Run(fmt.Sprintf("language=%d", lang), func(t *testing.T) {
			m := mouseTestModel(map[string]*api.Option{
				"selection": mkOpt("selection", api.OptionChoice, "alpha-value").SetValues("alpha-value", "中文候选项", "gamma-value").SetDescription("第一行描述\n第二行描述"),
			})
			m.language = lang
			mouseTestClickText(t, &m, "selection")
			x, y := mouseTestPosition(t, m.View(), "中文候选项")
			m.Update(mouseTestWheel(x, y, true))
			if m.overlay != overlayChoice || m.choiceCursor != 1 || m.getValue("selection") != "alpha-value" {
				t.Fatal("choice wheel must move the candidate without committing")
			}
			m.Update(mouseTestWheel(x, y, false))
			if m.choiceCursor != 0 {
				t.Fatal("choice wheel up did not move back")
			}
			mouseTestClickText(t, &m, "中文候选项")
			if m.overlay != overlayNone || m.getValue("selection") != "中文候选项" {
				t.Fatal("multibyte candidate click missed its displayed row")
			}
			m.openLanguageSelector()
			x, y = mouseTestPosition(t, m.View(), "English")
			m.Update(mouseTestWheel(x, y, true))
			if m.choiceCursor != 1 || m.language != lang {
				t.Fatal("language wheel did not move to Chinese without committing")
			}
			m.Update(mouseTestWheel(x, y, false))
			if m.choiceCursor != 0 || m.language != lang {
				t.Fatal("language wheel did not move back without committing")
			}
			wantLabel, wantLanguage := "中文", languageChinese
			if lang == languageChinese {
				wantLabel, wantLanguage = "English", languageEnglish
			}
			mouseTestClickText(t, &m, wantLabel)
			if m.overlay != overlayNone || m.language != wantLanguage {
				t.Fatal("language candidate click did not apply and close")
			}
		})
	}
}

func TestMouseOverlayOutsideAndInteriorClicks(t *testing.T) {
	for _, kind := range []overlayKind{overlayChoice, overlayLanguage, overlayDetail} {
		for _, lang := range []language{languageEnglish, languageChinese} {
			t.Run(fmt.Sprintf("overlay=%d/language=%d", kind, lang), func(t *testing.T) {
				m := mouseTestModel(map[string]*api.Option{"selection": mkOpt("selection", api.OptionChoice, "first-value").SetValues("first-value", "second-value")})
				m.language = lang
				switch kind {
				case overlayChoice:
					m.openChoiceOverlay("selection", []string{"first-value", "second-value"})
				case overlayLanguage:
					m.openLanguageSelector()
				case overlayDetail:
					m.openDetailOverlay(m.visibleOptions()[0])
				}
				title := "selection"
				if kind == overlayLanguage {
					title = m.text(textLanguage)
				}
				mouseTestClickText(t, &m, title)
				if m.overlay != kind {
					t.Fatal("clicking dialog title closed or activated the dialog")
				}
				m.Update(mouseTestClick(0, 0))
				if m.overlay != overlayNone || m.getValue("selection") != "first-value" || m.language != lang || m.hasChanges {
					t.Fatal("outside click did not cancel without changing values")
				}
			})
		}
	}
}

func TestMouseConfirmButtonsAndOutside(t *testing.T) {
	for _, lang := range []language{languageEnglish, languageChinese} {
		for _, save := range []bool{false, true} {
			for _, selected := range []int{0, 1} {
				t.Run(fmt.Sprintf("language=%d/save=%t/selected=%d", lang, save, selected), func(t *testing.T) {
					m := mouseTestModel(map[string]*api.Option{"enabled": mkOpt("enabled", api.OptionBool, false)})
					m.language = lang
					m.setValue("enabled", true)
					m.confirmQuit, m.confirmBtn = true, selected
					label := m.text(textDiscard)
					button := 1
					if save {
						label, button = m.text(textSave), 0
					}
					text := "  " + label + "  "
					if selected == button {
						text = "[ " + label + " ]"
					}
					cmd := mouseTestClickText(t, &m, text)
					if cmd == nil || m.saved != save {
						t.Fatalf("button click did not quit with save=%t: saved=%t cmd=%v", save, m.saved, cmd != nil)
					}
					if _, ok := cmd().(tea.QuitMsg); !ok {
						t.Fatal("confirmation button did not return Quit")
					}
				})
			}
		}
		t.Run(fmt.Sprintf("outside/language=%d", lang), func(t *testing.T) {
			m := mouseTestModel(map[string]*api.Option{"enabled": mkOpt("enabled", api.OptionBool, false)})
			m.language = lang
			m.setValue("enabled", true)
			m.confirmQuit = true
			if cmd := mouseTestClickText(t, &m, m.text(textSaveBeforeExit)); cmd != nil || !m.confirmQuit {
				t.Fatal("dialog interior should not choose a button")
			}
			_, cmd := m.Update(mouseTestClick(0, 0))
			if cmd != nil || m.confirmQuit || !m.hasChanges || m.getValue("enabled") != true {
				t.Fatal("outside confirmation click must cancel the dialog and preserve edits")
			}
		})
	}
}
