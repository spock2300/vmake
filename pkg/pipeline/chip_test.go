package pipeline

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/resolver"
)

func chipTestContext(t *testing.T, nodes ...*resolver.PackageNode) *RuntimeContext {
	t.Helper()
	r := resolver.NewResolver(nil, t.TempDir())
	graph := r.Graph()
	for _, node := range nodes {
		graph.Order = append(graph.Order, node.ID)
		graph.Packages[node.ID] = node
	}
	return &RuntimeContext{Config: emptyConfig(), Resolver: r, DepGraph: graph}
}

func chipOption(values ...string) *api.Option {
	return (&api.Option{}).
		SetType(api.OptionChoice).
		SetDefault(values[0]).
		SetValues(values...)
}

func TestCollectChipProfiles(t *testing.T) {
	first := localNode("chip-a")
	first.Pkg.SetChipProfile(api.ChipProfile{
		Name: "chip-a", PreferredToolchain: "tc-a", TargetOS: "none", TargetTriple: "triple-a",
	})
	second := localNode("chip-b")
	second.Pkg.SetChipProfile(api.ChipProfile{
		Name: "chip-b", PreferredToolchain: "tc-b", TargetOS: "none", TargetTriple: "triple-b",
	})
	ctx := chipTestContext(t, first, second)
	if err := collectChipProfiles(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ctx.ChipProfiles) != 2 {
		t.Fatalf("profiles = %+v", ctx.ChipProfiles)
	}
	if got := ctx.ChipProfiles["chip-a"].PreferredToolchain; got != "tc-a" {
		t.Errorf("chip-a toolchain = %q", got)
	}
}

func TestCollectChipProfilesConflict(t *testing.T) {
	first := localNode("chip-a")
	first.Pkg.SetChipProfile(api.ChipProfile{Name: "chip-a", PreferredToolchain: "tc-a"})
	second := localNode("chip-b")
	second.Pkg.SetChipProfile(api.ChipProfile{Name: "chip-a", PreferredToolchain: "tc-b"})
	ctx := chipTestContext(t, first, second)
	if err := collectChipProfiles(ctx); err == nil || !strings.Contains(err.Error(), "chip-a") {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestExtendChipChoice(t *testing.T) {
	ctx := chipTestContext(t)
	ctx.ChipProfiles = map[string]api.ChipProfile{
		"chip-a": {Name: "chip-a"},
		"chip-b": {Name: "chip-b"},
	}
	ctx.GlobalOptions = map[string]*api.Option{api.ChipOptionName: chipOption("chip-a")}
	extendChipChoice(ctx)
	got := ctx.GlobalOptions[api.ChipOptionName].Values()
	if len(got) != 2 || got[0] != "chip-a" || got[1] != "chip-b" {
		t.Fatalf("chip values = %v", got)
	}
}

func TestExtendChipChoiceDoesNotCreateOption(t *testing.T) {
	ctx := chipTestContext(t)
	ctx.ChipProfiles = map[string]api.ChipProfile{
		"chip-a": {Name: "chip-a"},
		"chip-b": {Name: "chip-b"},
	}
	ctx.GlobalOptions = map[string]*api.Option{}
	extendChipChoice(ctx)
	if opt := ctx.GlobalOptions[api.ChipOptionName]; opt != nil {
		t.Fatalf("chip option should not be created: %+v", opt)
	}
}

func TestResolveActiveChipUsesNeededProfile(t *testing.T) {
	tc := registerTargetDefaultsToolchain(t, "chip-default", "none", "arm-none-eabi")
	active := localNode("chip-a")
	active.Pkg.SetChipProfile(api.ChipProfile{
		Name: "chip-a", PreferredToolchain: tc, TargetOS: "none", TargetTriple: "arm-none-eabi",
	})
	inactive := localNode("chip-b")
	inactive.Pkg.SetChipProfile(api.ChipProfile{
		Name: "chip-b", PreferredToolchain: "other-tc", TargetOS: "none", TargetTriple: "other",
	})
	ctx := chipTestContext(t, active, inactive)
	if err := collectChipProfiles(ctx); err != nil {
		t.Fatal(err)
	}
	ctx.GlobalOptions = map[string]*api.Option{api.ChipOptionName: chipOption("chip-a", "chip-b")}
	needed := map[string]bool{"chip-a": true, "chip-b": false}
	if err := resolveActiveChip(ctx, needed); err != nil {
		t.Fatal(err)
	}
	if ctx.PreferredToolchain != tc {
		t.Errorf("preferred toolchain = %q, want %q", ctx.PreferredToolchain, tc)
	}
	if ctx.ChipProfile.TargetOS != "none" || ctx.ChipProfile.TargetTriple != "arm-none-eabi" {
		t.Errorf("chip profile = %+v", ctx.ChipProfile)
	}
}

func TestResolveActiveChipIgnoresUnconfiguredDefault(t *testing.T) {
	first := localNode("chip-a")
	first.Pkg.SetChipProfile(api.ChipProfile{Name: "chip-a"})
	second := localNode("chip-b")
	second.Pkg.SetChipProfile(api.ChipProfile{Name: "chip-b"})
	ctx := chipTestContext(t, first, second)
	if err := collectChipProfiles(ctx); err != nil {
		t.Fatal(err)
	}
	ctx.GlobalOptions = map[string]*api.Option{api.ChipOptionName: chipOption("chip-a", "chip-b")}
	if err := resolveActiveChip(ctx, map[string]bool{"chip-b": true}); err != nil {
		t.Fatalf("needed chip must win over the option default: %v", err)
	}
	if ctx.ChipProfile.Name != "chip-b" {
		t.Fatalf("active profile = %q, want chip-b", ctx.ChipProfile.Name)
	}
}

func TestResolveActiveChipIgnoresOptionWithoutProfiles(t *testing.T) {
	app := localNode("app")
	ctx := chipTestContext(t, app)
	ctx.GlobalOptions = map[string]*api.Option{api.ChipOptionName: chipOption("board-x")}
	if err := resolveActiveChip(ctx, map[string]bool{"app": true}); err != nil {
		t.Fatalf("existing chip option without profiles must not fail: %v", err)
	}
}

func TestResolveActiveChipConflict(t *testing.T) {
	first := localNode("chip-a")
	first.Pkg.SetChipProfile(api.ChipProfile{Name: "chip-a", PreferredToolchain: "tc-a"})
	second := localNode("chip-b")
	second.Pkg.SetChipProfile(api.ChipProfile{Name: "chip-b", PreferredToolchain: "tc-b"})
	ctx := chipTestContext(t, first, second)
	if err := collectChipProfiles(ctx); err != nil {
		t.Fatal(err)
	}
	needed := map[string]bool{"chip-a": true, "chip-b": true}
	if err := resolveActiveChip(ctx, needed); err == nil || !strings.Contains(err.Error(), "chip-a") {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestResolveActiveChipExplicitToolchainBypassesConflict(t *testing.T) {
	first := localNode("chip-a")
	first.Pkg.SetChipProfile(api.ChipProfile{Name: "chip-a", PreferredToolchain: "tc-a"})
	second := localNode("chip-b")
	second.Pkg.SetChipProfile(api.ChipProfile{Name: "chip-b", PreferredToolchain: "tc-b"})
	ctx := chipTestContext(t, first, second)
	if err := collectChipProfiles(ctx); err != nil {
		t.Fatal(err)
	}
	ctx.ToolchainOverride = "host"
	needed := map[string]bool{"chip-a": true, "chip-b": true}
	if err := resolveActiveChip(ctx, needed); err != nil {
		t.Fatal(err)
	}
	if ctx.PreferredToolchain != "" {
		t.Errorf("preferred toolchain = %q, want empty", ctx.PreferredToolchain)
	}
}

func TestResolveActiveChipMissingToolchain(t *testing.T) {
	active := localNode("chip-a")
	active.Pkg.SetChipProfile(api.ChipProfile{Name: "chip-a", PreferredToolchain: "missing-chip-toolchain"})
	ctx := chipTestContext(t, active)
	if err := collectChipProfiles(ctx); err != nil {
		t.Fatal(err)
	}
	ctx.GlobalOptions = map[string]*api.Option{api.ChipOptionName: chipOption("chip-a")}
	if err := resolveActiveChip(ctx, map[string]bool{"chip-a": true}); err == nil ||
		!strings.Contains(err.Error(), "missing-chip-toolchain") {
		t.Fatalf("missing toolchain error = %v", err)
	}
}

func TestResolveActiveChipRejectsConfiguredMismatch(t *testing.T) {
	tc := registerTargetDefaultsToolchain(t, "chip-mismatch", "none", "arm-none-eabi")
	active := localNode("chip-a")
	active.Pkg.SetChipProfile(api.ChipProfile{Name: "chip-a", PreferredToolchain: tc})
	inactive := localNode("chip-b")
	inactive.Pkg.SetChipProfile(api.ChipProfile{Name: "chip-b"})
	ctx := chipTestContext(t, active, inactive)
	if err := collectChipProfiles(ctx); err != nil {
		t.Fatal(err)
	}
	ctx.Config.Global.Options[api.ChipOptionName] = "chip-b"
	if err := resolveActiveChip(ctx, map[string]bool{"chip-a": true}); err == nil ||
		!strings.Contains(err.Error(), "chip-b") {
		t.Fatalf("chip mismatch error = %v", err)
	}
}

func TestResolveActiveChipExplicitToolchainKeepsNeededProfile(t *testing.T) {
	tc := registerTargetDefaultsToolchain(t, "chip-explicit", "none", "arm-none-eabi")
	active := localNode("chip-a")
	active.Pkg.SetChipProfile(api.ChipProfile{
		Name: "chip-a", PreferredToolchain: tc, TargetOS: "none", TargetTriple: "arm-none-eabi",
	})
	inactive := localNode("chip-b")
	inactive.Pkg.SetChipProfile(api.ChipProfile{Name: "chip-b"})
	ctx := chipTestContext(t, active, inactive)
	if err := collectChipProfiles(ctx); err != nil {
		t.Fatal(err)
	}
	ctx.Config.Global.Options[api.ChipOptionName] = "chip-b"
	ctx.ToolchainOverride = tc
	if err := resolveActiveChip(ctx, map[string]bool{"chip-a": true}); err != nil {
		t.Fatal(err)
	}
	if ctx.ChipProfile.Name != "chip-a" || ctx.PreferredToolchain != tc {
		t.Fatalf("profile = %+v / %q", ctx.ChipProfile, ctx.PreferredToolchain)
	}
}

func TestResolveActiveChipWithoutProfilesKeepsChipOption(t *testing.T) {
	app := localNode("app")
	ctx := chipTestContext(t, app)
	ctx.Config.Global.Options[api.ChipOptionName] = "board-x"
	if err := resolveActiveChip(ctx, map[string]bool{"app": true}); err != nil {
		t.Fatalf("pre-existing chip option must not fail: %v", err)
	}
}

func TestResolveActiveChipForDisplayUsesSelectedProfile(t *testing.T) {
	tc := registerTargetDefaultsToolchain(t, "chip-display", "none", "arm-none-eabi")
	active := localNode("chip-a")
	active.Pkg.SetChipProfile(api.ChipProfile{
		Name: "chip-a", PreferredToolchain: tc, TargetOS: "none", TargetTriple: "arm-none-eabi",
	})
	ctx := chipTestContext(t, active)
	ctx.GlobalOptions = map[string]*api.Option{api.ChipOptionName: chipOption("chip-a")}
	ResolveActiveChipForDisplay(ctx)
	if ctx.PreferredToolchain != tc || ctx.ChipProfile.Name != "chip-a" {
		t.Fatalf("display profile = %+v / %q", ctx.ChipProfile, ctx.PreferredToolchain)
	}
}

func TestProjectPlatformUsesChipProfileDefaults(t *testing.T) {
	ctx := chipTestContext(t)
	ctx.ChipProfile = api.ChipProfile{Name: "chip-a", TargetOS: "none", TargetTriple: "arm-none-eabi"}
	platform, err := ProjectPlatform(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if platform.OS != "none" || platform.Triple != "arm-none-eabi" {
		t.Fatalf("platform = %+v", platform)
	}
}

func TestChipTargetsApply(t *testing.T) {
	alternate := registerTargetDefaultsToolchain(t, "chip-alt", "none", "arm-none-eabi")
	other := registerTargetDefaultsToolchain(t, "chip-other", "none", "riscv32-unknown-elf")
	profile := api.ChipProfile{
		Name:               "chip-a",
		PreferredToolchain: "chip-preferred",
		TargetOS:           "none",
		TargetTriple:       "arm-none-eabi",
	}
	if !chipTargetsApply(profile, "chip-preferred") {
		t.Error("preferred toolchain should apply")
	}
	if !chipTargetsApply(profile, alternate) {
		t.Error("alternate toolchain with the same triple should apply")
	}
	if chipTargetsApply(profile, other) {
		t.Error("toolchain with another triple should not apply")
	}
	if chipTargetsApply(profile, "host") {
		t.Error("host should not apply chip targets")
	}
	if !chipTargetsApply(api.ChipProfile{Name: "chip-a"}, "host") {
		t.Error("profile without a preferred toolchain should apply")
	}
	if chipTargetsApply(api.ChipProfile{}, "host") {
		t.Error("empty profile should not apply")
	}
}
