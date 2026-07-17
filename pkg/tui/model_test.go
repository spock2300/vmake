package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

func mkOpt(name string, optType api.OptionType, def any) *api.Option {
	return api.NewConfigContext("test").Option(name).SetType(optType).SetDefault(def)
}

func mkSources(names ...string) []buildscript.Source {
	var out []buildscript.Source
	for _, n := range names {
		out = append(out, buildscript.Source{Name: n, Origin: api.SourceLocal})
	}
	return out
}

func TestGroupAndSortOptions_GroupingAndAlphaOrder(t *testing.T) {
	opts := map[string]*api.Option{
		"zeta":  mkOpt("zeta", api.OptionBool, false).SetGroup("ZGroup"),
		"alpha": mkOpt("alpha", api.OptionBool, false).SetGroup("ZGroup"),
		"beta":  mkOpt("beta", api.OptionBool, false).SetGroup("AGroup"),
	}
	items := groupAndSortOptions(opts)

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// AGroup before ZGroup
	if items[0].Group != "AGroup" || items[0].Name != "beta" {
		t.Errorf("first should be AGroup/beta, got %s/%s", items[0].Group, items[0].Name)
	}
	if items[1].Group != "ZGroup" || items[1].Name != "alpha" {
		t.Errorf("second should be ZGroup/alpha, got %s/%s", items[1].Group, items[1].Name)
	}
	if items[2].Name != "zeta" {
		t.Errorf("third should be zeta, got %s", items[2].Name)
	}
}

func TestGroupAndSortOptions_EmptyGroupBecomesGeneral(t *testing.T) {
	opts := map[string]*api.Option{
		"x": mkOpt("x", api.OptionBool, false),
	}
	items := groupAndSortOptions(opts)
	if items[0].Group != "General" {
		t.Errorf("empty group should become General, got %q", items[0].Group)
	}
}

func TestBuildDepTree_GlobalNodeFirst(t *testing.T) {
	sources := mkSources("app", "lib")
	deps := map[string][]string{
		"app": {"lib"},
	}
	tree := buildDepTree(sources, deps)
	if len(tree) == 0 || tree[0].PkgName != GlobalPkgName {
		t.Fatal("first node must be Global")
	}
}

func TestBuildDepTree_RootsFromNonDeps(t *testing.T) {
	sources := mkSources("app", "lib", "util")
	deps := map[string][]string{
		"app": {"lib", "util"},
		"lib": {},
	}
	tree := buildDepTree(sources, deps)
	if len(tree) != 2 {
		t.Fatalf("expected Global + 1 root (app), got %d top nodes", len(tree))
	}
	appNode := tree[1]
	if appNode.Name != "app" {
		t.Fatalf("expected root app, got %s", appNode.Name)
	}
	if len(appNode.Children) != 2 {
		t.Fatalf("expected app to have 2 children, got %d", len(appNode.Children))
	}
}

func TestFlattenTree_RespectsExpanded(t *testing.T) {
	tree := []*TreeNode{
		{Name: "a", Expanded: true, Children: []*TreeNode{
			{Name: "b", Expanded: false, Children: []*TreeNode{{Name: "c"}}},
		}},
	}
	flat := flattenTree(tree)
	if len(flat) != 2 {
		t.Fatalf("expected a,b (c collapsed), got %d nodes", len(flat))
	}
	if flat[0].Name != "a" || flat[1].Name != "b" {
		t.Errorf("unexpected flatten order: %s, %s", flat[0].Name, flat[1].Name)
	}
}

func TestFlattenTree_ExpandedShowsChildren(t *testing.T) {
	tree := []*TreeNode{
		{Name: "a", Expanded: true, Children: []*TreeNode{
			{Name: "b", Expanded: true, Children: []*TreeNode{{Name: "c"}}},
		}},
	}
	flat := flattenTree(tree)
	if len(flat) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(flat))
	}
}

func TestCalcTreeWidth_ClampsToRange(t *testing.T) {
	noCounts := map[string]int{}
	flat := []*TreeNode{{Name: "x", Depth: 0, Prefix: ""}}
	w := calcTreeWidth(flat, noCounts)
	if w < 20 || w > 40 {
		t.Errorf("width %d out of clamp range [20,40]", w)
	}

	wide := []*TreeNode{{Name: "very_very_very_long_package_name_exceeding_max", Depth: 0, Prefix: ""}}
	w2 := calcTreeWidth(wide, noCounts)
	if w2 > 40 {
		t.Errorf("wide width %d should clamp to 40", w2)
	}
}

func TestCalcTreeWidth_IncludesBadge(t *testing.T) {
	counts := map[string]int{"mediumlen_pkgname": 12}
	flat := []*TreeNode{{PkgName: "mediumlen_pkgname", Name: "mediumlen_pkgname", Depth: 0, Prefix: ""}}
	withBadge := calcTreeWidth(flat, counts)
	withoutBadge := calcTreeWidth(flat, map[string]int{})
	if withBadge <= withoutBadge {
		t.Errorf("width with badge (%d) should exceed without (%d)", withBadge, withoutBadge)
	}
}

func TestValuesEqual_IntVsFloatAfterJSONRoundTrip(t *testing.T) {
	// BUG: valuesEqual used != on `any`; int(4) != float64(4) is true (types differ),
	// causing false "modified" after config.json round-trip decodes all numbers as float64.
	a := map[string]map[string]any{
		"app": {"thread_count": 4},
	}
	b := map[string]map[string]any{
		"app": {"thread_count": float64(4)},
	}
	if !valuesEqual(a, b) {
		t.Error("valuesEqual should treat int(4) and float64(4) as equal (JSON round-trip)")
	}
}

func TestGlobalValuesEqual_IntVsFloat(t *testing.T) {
	a := map[string]any{"count": 4}
	b := map[string]any{"count": float64(4)}
	if !globalValuesEqual(a, b) {
		t.Error("globalValuesEqual should treat int(4) and float64(4) as equal")
	}
}

func TestValuesEqual_BoolAndString(t *testing.T) {
	if !valuesEqual(
		map[string]map[string]any{"p": {"b": true, "s": "hi"}},
		map[string]map[string]any{"p": {"b": true, "s": "hi"}},
	) {
		t.Error("equal bool/string maps should be equal")
	}
	if valuesEqual(
		map[string]map[string]any{"p": {"b": true}},
		map[string]map[string]any{"p": {"b": false}},
	) {
		t.Error("differing bools should not be equal")
	}
}

func TestValuesEqual_DifferentKeys(t *testing.T) {
	a := map[string]map[string]any{"p": {"x": 1}}
	b := map[string]map[string]any{"p": {"y": 1}}
	if valuesEqual(a, b) {
		t.Error("different option keys should not be equal")
	}
}

func TestDeepCopyValues_IsolatesOriginal(t *testing.T) {
	src := map[string]map[string]any{"p": {"x": 1}}
	dst := deepCopyValues(src)
	dst["p"]["x"] = 99
	if src["p"]["x"] == 99 {
		t.Error("deep copy should isolate original from mutations")
	}
}

func TestDeepCopyGlobal_IsolatesOriginal(t *testing.T) {
	src := map[string]any{"x": 1}
	dst := deepCopyGlobal(src)
	dst["x"] = 99
	if src["x"] == 99 {
		t.Error("deep copy should isolate original from mutations")
	}
}

func TestNewModel_AppliesDefaultsForMissingValues(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"debug": mkOpt("debug", api.OptionBool, true)},
	}
	globalOpts := map[string]*api.Option{
		"mode": mkOpt("mode", api.OptionChoice, "release").SetValues("debug", "release"),
	}
	sources := mkSources("app")
	m := NewModel(sources, map[string][]string{}, opts, map[string]map[string]any{}, "/work", "gcc", globalOpts, map[string]any{}, nil)

	if m.values["app"]["debug"] != true {
		t.Errorf("missing package value should default to true, got %v", m.values["app"]["debug"])
	}
	if m.globalValues["mode"] != "release" {
		t.Errorf("missing global mode should default to release, got %v", m.globalValues["mode"])
	}
	if m.globalValues["toolchain"] != "gcc" {
		t.Errorf("toolchain should be set from currentToolchain, got %v", m.globalValues["toolchain"])
	}
}

func TestNewModel_KeepsProvidedValuesOverDefault(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"debug": mkOpt("debug", api.OptionBool, true)},
	}
	values := map[string]map[string]any{"app": {"debug": false}}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, values, "/work", "", nil, nil, nil)
	if m.values["app"]["debug"] != false {
		t.Errorf("provided value false should win over default, got %v", m.values["app"]["debug"])
	}
}

func TestModelCheckChanges_DetectsModification(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"debug": mkOpt("debug", api.OptionBool, false)},
	}
	values := map[string]map[string]any{"app": {"debug": false}}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, values, "/work", "", nil, nil, nil)
	m.selectedPkg = "app"
	m.buildOptionItems()
	if m.hasChanges {
		t.Error("fresh model should have no changes")
	}
	m.setValue("debug", true)
	if !m.hasChanges {
		t.Error("flipping a bool should set hasChanges")
	}
	m.setValue("debug", false)
	if m.hasChanges {
		t.Error("reverting back should clear hasChanges")
	}
}

func TestVisibleOptions_FiltersByShowIf(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {
			"ssl": mkOpt("ssl", api.OptionBool, false),
			"ssl_version": mkOpt("ssl_version", api.OptionString, "1.1").SetShowIf(func(c *api.ConfigContext) bool {
				return c.Bool("ssl")
			}),
		},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/work", "", nil, nil, nil)
	m.selectedPkg = "app"
	m.buildOptionItems()

	vis := m.visibleOptions()
	if len(vis) != 1 || vis[0].Name != "ssl" {
		t.Fatalf("ssl_version hidden when ssl=false; expected only ssl, got %v", names(vis))
	}

	m.setValue("ssl", true)
	vis = m.visibleOptions()
	if len(vis) != 2 {
		t.Fatalf("both options visible when ssl=true; got %v", names(vis))
	}
}

func names(items []OptionItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

func TestComputeOptCounts_ExcludesGlobalOptions(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {
			"debug":  mkOpt("debug", api.OptionBool, false),
			"thread": mkOpt("thread", api.OptionInt, 4),
		},
		"lib": {},
	}
	globalOpts := map[string]*api.Option{
		"mode": mkOpt("mode", api.OptionChoice, "release").SetGroup("Global"),
	}
	counts := computeOptCounts(opts, globalOpts)
	if counts["app"] != 2 {
		t.Errorf("app count = %d, want 2", counts["app"])
	}
	if counts["lib"] != 0 {
		t.Errorf("lib count = %d, want 0", counts["lib"])
	}
	if counts[GlobalPkgName] != 1 {
		t.Errorf("global count = %d, want 1", counts[GlobalPkgName])
	}
}

func TestIsDisplayable_KeepsGlobalAndOptionedAndStructural(t *testing.T) {
	optCounts := map[string]int{"app": 2, "lib": 0}
	kconfigs := map[string][]*api.KConfigEntry{}
	if !isDisplayable(&TreeNode{PkgName: GlobalPkgName}, optCounts, kconfigs) {
		t.Error("global node should be displayable")
	}
	if !isDisplayable(&TreeNode{PkgName: "app"}, optCounts, kconfigs) {
		t.Error("package with options should be displayable")
	}
	if isDisplayable(&TreeNode{PkgName: "lib"}, optCounts, kconfigs) {
		t.Error("empty leaf package should NOT be displayable")
	}
	parent := &TreeNode{PkgName: "parent", Children: []*TreeNode{{PkgName: "app"}}}
	if !isDisplayable(parent, optCounts, kconfigs) {
		t.Error("package with displayable descendant should be displayable")
	}
}

func TestFlattenTreeFiltered_HidesEmptyLeaves(t *testing.T) {
	optCounts := map[string]int{"app": 1, "lib": 0}
	tree := []*TreeNode{
		{PkgName: GlobalPkgName, Name: "Global", Expanded: true},
		{PkgName: "app", Name: "app", Expanded: true, Children: []*TreeNode{
			{PkgName: "lib", Name: "lib"},
		}},
	}
	flat := flattenTreeFiltered(tree, optCounts, map[string][]*api.KConfigEntry{})
	names := flatNodeNames(flat)
	if contains(names, "lib") {
		t.Error("empty lib leaf should be hidden")
	}
	if !contains(names, "app") {
		t.Error("app should be visible")
	}
}

func TestModelCollapseExpandAll(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"debug": mkOpt("debug", api.OptionBool, false)},
		"lib": {"x": mkOpt("x", api.OptionBool, false)},
	}
	sources := mkSources("app", "lib")
	deps := map[string][]string{"app": {"lib"}}
	m := NewModel(sources, deps, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)

	m.expandAll()
	visibleBeforeCollapse := len(m.flat)

	m.collapseAll()
	if len(m.flat) >= visibleBeforeCollapse {
		t.Errorf("after collapseAll flat (%d) should shrink vs expanded (%d)", len(m.flat), visibleBeforeCollapse)
	}

	m.expandAll()
	if len(m.flat) < visibleBeforeCollapse {
		t.Errorf("after expandAll flat (%d) should restore >= %d", len(m.flat), visibleBeforeCollapse)
	}
}

func TestModelHideEmptyPkgs(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app":   {"debug": mkOpt("debug", api.OptionBool, false)},
		"empty": {},
	}
	sources := mkSources("app", "empty")
	m := NewModel(sources, map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)

	beforeNames := flatNodeNames(m.flat)
	if !contains(beforeNames, "empty") {
		t.Fatal("empty pkg should be visible by default")
	}

	m.hideEmptyPkgs = true
	m.rebuildFlat()
	afterNames := flatNodeNames(m.flat)
	if contains(afterNames, "empty") {
		t.Error("empty pkg should be hidden when hideEmptyPkgs=true")
	}
	if !contains(afterNames, "app") {
		t.Error("app pkg should remain visible")
	}
}

func TestModelOptCountFor(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"debug": mkOpt("debug", api.OptionBool, false)},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	if m.optCountFor("app") != 1 {
		t.Errorf("optCountFor(app) = %d, want 1", m.optCountFor("app"))
	}
	if m.optCountFor("missing") != 0 {
		t.Errorf("optCountFor(missing) = %d, want 0", m.optCountFor("missing"))
	}
}

func flatNodeNames(nodes []*TreeNode) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

func contains(slice []string, s string) bool {
	for _, x := range slice {
		if x == s {
			return true
		}
	}
	return false
}

func TestIsOptionModified_DetectsChangeFromOpen(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"debug": mkOpt("debug", api.OptionBool, false)},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.selectedPkg = "app"
	m.buildOptionItems()
	if m.isOptionModified("debug") {
		t.Error("fresh option at default should not be modified")
	}
	m.setValue("debug", true)
	if !m.isOptionModified("debug") {
		t.Error("changed option should be modified")
	}
}

func TestResetOption_RestoresOpenValue(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"debug": mkOpt("debug", api.OptionBool, false)},
	}
	values := map[string]map[string]any{"app": {"debug": true}}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, values, "/w", "", nil, nil, nil)
	m.selectedPkg = "app"
	m.buildOptionItems()
	m.setValue("debug", false)
	if !m.isOptionModified("debug") {
		t.Fatal("should be modified after change")
	}
	m.resetOption("debug")
	if m.isOptionModified("debug") {
		t.Error("reset should restore open value (true)")
	}
	if m.getValue("debug") != true {
		t.Errorf("after reset value = %v, want true", m.getValue("debug"))
	}
}

func TestResetOptionToDefault_RestoresDefault(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"threads": mkOpt("threads", api.OptionInt, 4)},
	}
	values := map[string]map[string]any{"app": {"threads": 8}}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, values, "/w", "", nil, nil, nil)
	m.selectedPkg = "app"
	m.buildOptionItems()
	m.resetOptionToDefault("threads")
	if m.getValue("threads") != 4 {
		t.Errorf("after reset-to-default = %v, want 4 (default)", m.getValue("threads"))
	}
}

func TestResetOption_FullCycle(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"threads": mkOpt("threads", api.OptionInt, 4)},
	}
	values := map[string]map[string]any{"app": {"threads": 4}}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, values, "/w", "", nil, nil, nil)
	m.selectedPkg = "app"
	m.buildOptionItems()
	m.setValue("threads", 9)
	if !m.isOptionModified("threads") {
		t.Fatal("should be modified after change to 9")
	}
	m.resetOption("threads")
	if m.isOptionModified("threads") {
		t.Error("reset to open value (4) should clear modified")
	}
	if m.getValue("threads") != 4 {
		t.Errorf("value = %v, want 4", m.getValue("threads"))
	}
}

func TestModifiedCount_AggregatesAcrossPackages(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"debug": mkOpt("debug", api.OptionBool, false)},
		"lib": {"x": mkOpt("x", api.OptionBool, false)},
	}
	globalOpts := map[string]*api.Option{
		"mode": mkOpt("mode", api.OptionChoice, "release").SetValues("debug", "release"),
	}
	m := NewModel(mkSources("app", "lib"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", globalOpts, nil, nil)
	m.selectedPkg = "app"
	m.buildOptionItems()
	m.setValue("debug", true)
	m.selectedPkg = "lib"
	m.buildOptionItems()
	m.setValue("x", true)
	m.selectedPkg = GlobalPkgName
	m.buildOptionItems()
	m.setValue("mode", "debug")
	if m.modifiedCount() != 3 {
		t.Errorf("modifiedCount = %d, want 3", m.modifiedCount())
	}
	m.setValue("mode", "release")
	if m.modifiedCount() != 2 {
		t.Errorf("after reverting mode, modifiedCount = %d, want 2", m.modifiedCount())
	}
}

func TestIsOptionModified_IntVsFloatRoundTrip(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"threads": mkOpt("threads", api.OptionInt, 4)},
	}
	values := map[string]map[string]any{"app": {"threads": float64(4)}}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, values, "/w", "", nil, nil, nil)
	m.selectedPkg = "app"
	m.buildOptionItems()
	if m.isOptionModified("threads") {
		t.Error("float64(4) should not count as modified vs int default 4")
	}
}

func TestOpenChoiceOverlay_PositionsCursorAtCurrent(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"level": mkOpt("level", api.OptionChoice, "b").SetValues("a", "b", "c", "d")},
	}
	values := map[string]map[string]any{"app": {"level": "c"}}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, values, "/w", "", nil, nil, nil)
	m.selectedPkg = "app"
	m.buildOptionItems()
	m.openChoiceOverlay("level", opts["app"]["level"].Values())
	if m.overlay != overlayChoice {
		t.Fatal("overlay should be overlayChoice")
	}
	if m.choiceCursor != 2 {
		t.Errorf("choiceCursor = %d, want 2 (c)", m.choiceCursor)
	}
}

func TestCloseOverlay_ResetsState(t *testing.T) {
	m := Model{overlay: overlayChoice, choiceOpt: "x", choiceValues: []string{"a"}, choiceCursor: 1}
	m.closeOverlay()
	if m.overlay != overlayNone {
		t.Error("overlay should be none after close")
	}
	if m.choiceOpt != "" || m.choiceValues != nil || m.choiceCursor != 0 {
		t.Error("overlay fields should reset on close")
	}
}

func TestRenderOverlay_ChoiceContainsAllValues(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"level": mkOpt("level", api.OptionChoice, "a").SetValues("a", "b", "c")},
	}
	values := map[string]map[string]any{"app": {"level": "a"}}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, values, "/w", "", nil, nil, nil)
	m.selectedPkg = "app"
	m.buildOptionItems()
	m.openChoiceOverlay("level", opts["app"]["level"].Values())
	out := m.renderOverlay()
	for _, v := range []string{"a", "b", "c"} {
		if !strings.Contains(out, v) {
			t.Errorf("choice overlay should contain %q", v)
		}
	}
}

func TestRenderOverlay_DetailShowsDefaultAndCurrent(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"threads": mkOpt("threads", api.OptionInt, 4)},
	}
	values := map[string]map[string]any{"app": {"threads": 9}}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, values, "/w", "", nil, nil, nil)
	m.selectedPkg = "app"
	m.buildOptionItems()
	m.openDetailOverlay(OptionItem{Name: "threads", Opt: opts["app"]["threads"]})
	out := m.renderOverlay()
	if !strings.Contains(out, "4") {
		t.Error("detail overlay should show default 4")
	}
	if !strings.Contains(out, "9") {
		t.Error("detail overlay should show current 9")
	}
}

func TestFlattenTreeSearch_MatchesByPkgName(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app":   {"debug": mkOpt("debug", api.OptionBool, false)},
		"other": {"x": mkOpt("x", api.OptionBool, false)},
	}
	tree := []*TreeNode{
		{PkgName: GlobalPkgName, Name: "Global"},
		{PkgName: "app", Name: "app"},
		{PkgName: "other", Name: "other"},
	}
	flat := flattenTreeSearch(tree, "app", opts, nil)
	names := flatNodeNames(flat)
	if !contains(names, "app") {
		t.Error("app should match search 'app'")
	}
	if contains(names, "other") {
		t.Error("other should NOT match search 'app'")
	}
}

func TestFlattenTreeSearch_MatchesByOptionName(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app":   {"thread_count": mkOpt("thread_count", api.OptionInt, 4)},
		"other": {"x": mkOpt("x", api.OptionBool, false)},
	}
	tree := []*TreeNode{
		{PkgName: GlobalPkgName, Name: "Global"},
		{PkgName: "app", Name: "app"},
		{PkgName: "other", Name: "other"},
	}
	flat := flattenTreeSearch(tree, "thread", opts, nil)
	names := flatNodeNames(flat)
	if !contains(names, "app") {
		t.Error("app should match search 'thread' via option thread_count")
	}
	if contains(names, "other") {
		t.Error("other should NOT match search 'thread'")
	}
}

func TestFlattenTreeSearch_MatchesByDescription(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"x": mkOpt("x", api.OptionString, "").SetDescription("Enable SSL support")},
	}
	tree := []*TreeNode{{PkgName: "app", Name: "app"}}
	flat := flattenTreeSearch(tree, "ssl", opts, nil)
	if len(flat) == 0 {
		t.Error("app should match search 'ssl' via description")
	}
}

func TestFlattenTreeSearch_CaseInsensitive(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"App": {"x": mkOpt("x", api.OptionBool, false)},
	}
	tree := []*TreeNode{{PkgName: "App", Name: "App"}}
	flat := flattenTreeSearch(tree, "APP", opts, nil)
	if len(flat) == 0 {
		t.Error("search should be case-insensitive")
	}
}

func TestRebuildFlat_SearchPrecedence(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app":   {"debug": mkOpt("debug", api.OptionBool, false)},
		"empty": {},
	}
	m := NewModel(mkSources("app", "empty"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)

	m.hideEmptyPkgs = true
	m.filterInput = "debug"
	m.rebuildFlat()
	names := flatNodeNames(m.flat)
	if !contains(names, "app") {
		t.Error("search should keep app")
	}
	if contains(names, "empty") {
		t.Error("empty should be filtered out")
	}
}

func TestFindFirstMatch_ByPkgName(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app":   {"debug": mkOpt("debug", api.OptionBool, false)},
		"other": {"x": mkOpt("x", api.OptionBool, false)},
	}
	m := NewModel(mkSources("app", "other"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	pkg, opt := m.findFirstMatch("oth")
	if pkg != "other" {
		t.Errorf("findFirstMatch pkg = %q, want other", pkg)
	}
	if opt != "" {
		t.Errorf("findFirstMatch opt = %q, want empty (matched pkg name only)", opt)
	}
}

func TestFindFirstMatch_ByOptionName(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"thread_count": mkOpt("thread_count", api.OptionInt, 4)},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	pkg, opt := m.findFirstMatch("thread")
	if pkg != "app" {
		t.Errorf("findFirstMatch pkg = %q, want app", pkg)
	}
	if opt != "thread_count" {
		t.Errorf("findFirstMatch opt = %q, want thread_count", opt)
	}
}

func TestFindFirstMatch_NoMatch(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"debug": mkOpt("debug", api.OptionBool, false)},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	pkg, opt := m.findFirstMatch("zzz")
	if pkg != "" || opt != "" {
		t.Errorf("findFirstMatch no-match = %q,%q; want empty", pkg, opt)
	}
}

func TestExpandToPkg_ExpandsAncestors(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"debug": mkOpt("debug", api.OptionBool, false)},
		"lib": {"x": mkOpt("x", api.OptionBool, false)},
	}
	deps := map[string][]string{"app": {"lib"}}
	m := NewModel(mkSources("app", "lib"), deps, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)

	m.collapseAll()
	if contains(flatNodeNames(m.flat), "lib") {
		t.Fatal("lib should be hidden after collapseAll")
	}
	if !m.expandToPkg("lib") {
		t.Fatal("expandToPkg(lib) should return true")
	}
	m.rebuildFlat()
	if !contains(flatNodeNames(m.flat), "lib") {
		t.Error("lib should be visible after expandToPkg")
	}
}

func TestJumpToMatch_PackageOnly(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app":   {"debug": mkOpt("debug", api.OptionBool, false)},
		"other": {"x": mkOpt("x", api.OptionBool, false)},
	}
	m := NewModel(mkSources("app", "other"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	ok := m.jumpToMatch("other", "")
	if !ok {
		t.Fatal("jumpToMatch should succeed")
	}
	if m.selectedPkg != "other" {
		t.Errorf("selectedPkg = %q, want other", m.selectedPkg)
	}
}

func TestJumpToMatch_OptionPositionsCursorStaysOnTree(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {
			"debug":        mkOpt("debug", api.OptionBool, false),
			"thread_count": mkOpt("thread_count", api.OptionInt, 4),
		},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	m.jumpToMatch("app", "thread_count")
	if m.selectedPkg != "app" {
		t.Fatalf("selectedPkg = %q, want app", m.selectedPkg)
	}
	visible := m.visibleOptions()
	if m.optCursor >= len(visible) || visible[m.optCursor].Name != "thread_count" {
		t.Errorf("optCursor should point at thread_count, got cursor=%d", m.optCursor)
	}
	if m.focusArea != 0 {
		t.Error("focusArea should stay on tree (0) after a search jump; user must Tab to edit options")
	}
}

func TestJumpToMatch_EmptyPkgReturnsFalse(t *testing.T) {
	m := Model{}
	if m.jumpToMatch("", "") {
		t.Error("jumpToMatch with empty pkg should return false")
	}
}

func TestFilteredMatchCount_OnlyPackages(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app":  {"debug": mkOpt("debug", api.OptionBool, false)},
		"lib":  {"x": mkOpt("x", api.OptionBool, false)},
		"util": {"y": mkOpt("y", api.OptionBool, false)},
	}
	m := NewModel(mkSources("app", "lib", "util"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	// no filter: all packages visible
	if c := m.filteredMatchCount(); c != 3 {
		t.Errorf("no-filter count = %d, want 3", c)
	}
	// apply a filter matching two packages
	m.filterInput = "ap"
	m.rebuildFlat()
	if c := m.filteredMatchCount(); c != 1 {
		t.Errorf("filter 'ap' count = %d, want 1 (app)", c)
	}
}

func TestFilteredMatchCount_ExcludesGlobalNode(t *testing.T) {
	globalOpts := map[string]*api.Option{"mode": mkOpt("mode", api.OptionChoice, "release")}
	m := NewModel(mkSources(), map[string][]string{}, map[string]map[string]*api.Option{}, map[string]map[string]any{}, "/w", "", globalOpts, nil, nil)
	if c := m.filteredMatchCount(); c != 0 {
		t.Errorf("only-global count = %d, want 0 (global excluded)", c)
	}
}

func TestMatchedOptionIn_FindsByOptionName(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {
			"debug":        mkOpt("debug", api.OptionBool, false),
			"thread_count": mkOpt("thread_count", api.OptionInt, 4),
		},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	if got := m.matchedOptionIn("app", "thread"); got != "thread_count" {
		t.Errorf("matchedOptionIn(app,thread) = %q, want thread_count", got)
	}
	if got := m.matchedOptionIn("app", "zzz"); got != "" {
		t.Errorf("matchedOptionIn(app,zzz) = %q, want empty", got)
	}
}

func TestMatchedOptionIn_FindsByDescription(t *testing.T) {
	opts := map[string]map[string]*api.Option{
		"app": {"x": mkOpt("x", api.OptionString, "").SetDescription("Enable SSL support")},
	}
	m := NewModel(mkSources("app"), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
	if got := m.matchedOptionIn("app", "ssl"); got != "x" {
		t.Errorf("matchedOptionIn by desc = %q, want x", got)
	}
}

// Scrolling the tree must reach the very last item. Previously the off-by-one
// between ensureTreeCursorVisible (panelH) and renderTree (drawH=panelH-1)
// left the last package permanently off-screen.
func TestTreeScrollReachesLastItem(t *testing.T) {
	opts := map[string]map[string]*api.Option{}
	var names []string
	for i := 0; i < 40; i++ {
		n := fmt.Sprintf("pkg_%02d", i)
		names = append(names, n)
		opts[n] = map[string]*api.Option{"a": mkOpt("a", api.OptionBool, false)}
	}
	last := names[len(names)-1]
	for _, h := range []int{8, 10, 15, 24} {
		t.Run(fmt.Sprintf("h=%d", h), func(t *testing.T) {
			m := NewModel(mkSources(names...), map[string][]string{}, opts, map[string]map[string]any{}, "/w", "", nil, nil, nil)
			m.width = 80
			m.height = h
			m.treeCursor = len(m.flat) - 1
			m.selectCurrentNode()
			m.ensureTreeCursorVisible()
			out := ansiRe.ReplaceAllString(m.renderTree(), "")
			if !strings.Contains(out, last) {
				t.Errorf("h=%d: last pkg %q not rendered after scroll to bottom\n%s", h, last, out)
			}
		})
	}
}

// Scrolling must also reach the first item (no regression at the top).
func TestTreeScrollReachesFirstItem(t *testing.T) {
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
	m.treeOff = 20
	m.treeCursor = 0
	m.ensureTreeCursorVisible()
	out := ansiRe.ReplaceAllString(m.renderTree(), "")
	if !strings.Contains(out, "Global") {
		t.Errorf("first node (Global) not rendered after scroll to top\n%s", out)
	}
}

func TestRenderEditField_CursorPlacement(t *testing.T) {
	out := renderEditField("hello", 0)
	if out != "▎hello" {
		t.Errorf("cursor at 0 = %q, want ▎hello", out)
	}
	out = renderEditField("hello", 5)
	if out != "hello▎" {
		t.Errorf("cursor at end = %q, want hello▎", out)
	}
	out = renderEditField("hello", 2)
	if out != "he▎llo" {
		t.Errorf("cursor mid = %q, want he▎llo", out)
	}
}

func TestRenderEditField_CursorClamped(t *testing.T) {
	if renderEditField("ab", -1) != "▎ab" {
		t.Error("negative cursor should clamp to 0")
	}
	if renderEditField("ab", 99) != "ab▎" {
		t.Error("oversize cursor should clamp to len")
	}
}

func TestParseIntInput_ValidAndInvalid(t *testing.T) {
	cases := []struct {
		in   string
		val  int
		want bool
	}{
		{"42", 42, true},
		{"  7 ", 7, true},
		{"-3", -3, true},
		{"", 0, false},
		{"abc", 0, false},
		{"12x", 12, true},
	}
	for _, c := range cases {
		v, ok := parseIntInput(c.in)
		if ok != c.want || (ok && v != c.val) {
			t.Errorf("parseIntInput(%q) = %d,%v want %d,%v", c.in, v, ok, c.val, c.want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if truncateRunes("hello", 10) != "hello" {
		t.Error("short string should not truncate")
	}
	if truncateRunes("hello", 5) != "hello" {
		t.Error("exactly fitting string should not truncate")
	}
	if truncateRunes("hello", 4) != "hel…" {
		t.Errorf("truncate(hello,4) = %q, want hel…", truncateRunes("hello", 4))
	}
	if truncateRunes("hello", 1) != "…" {
		t.Errorf("truncate(hello,1) = %q, want …", truncateRunes("hello", 1))
	}
	if truncateRunes("abc", 0) != "" {
		t.Error("truncate to 0 should be empty")
	}
}
