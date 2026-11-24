package pipeline

import (
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func optionProjectionFixture() (OptionProjection, map[string]*api.Option) {
	acc := api.NewConfigAccessor(nil, nil)
	defs := map[string]*api.Option{
		"board": acc.Option("board").SetType(api.OptionString).SetGroup(api.GroupGlobal),
		"count": acc.Option("count").SetType(api.OptionInt),
		"flag":  acc.Option("flag").SetType(api.OptionBool).SetDefault(true),
		"label": acc.Option("label").SetType(api.OptionString).SetDefault("d"),
	}
	projection := OptionProjection{
		Values:       map[string]any{"board": "evb", "flag": false},
		Defs:         defs,
		GlobalDefs:   map[string]*api.Option{"board": defs["board"]},
		GlobalValues: map[string]any{"board": "evb"},
	}
	return projection, defs
}

func optionValuesByName(values []OptionValue) map[string]OptionValue {
	out := make(map[string]OptionValue, len(values))
	for _, value := range values {
		out[value.Name] = value
	}
	return out
}

func TestOptionProjectionInspectProfile(t *testing.T) {
	projection, _ := optionProjectionFixture()
	values := optionValuesByName(projection.Project(OptionProfileInspect))

	if got := len(values); got != 4 {
		t.Fatalf("len = %d, want 4 (declared globals included, absent values kept)", got)
	}
	if got := values["board"]; !got.Resolved || got.Value != "evb" {
		t.Errorf("board = %#v, want resolved evb", got)
	}
	if got := values["flag"]; !got.Resolved || got.Value != false {
		t.Errorf("flag = %#v, want resolved false", got)
	}
	if got := values["count"]; got.Resolved {
		t.Errorf("count = %#v, want unresolved (no value supplied)", got)
	}
	if got := values["label"]; got.Resolved {
		t.Errorf("label = %#v, want unresolved (inspect does not fill defaults)", got)
	}
}

func TestOptionProjectionQueryProfile(t *testing.T) {
	projection, _ := optionProjectionFixture()
	values := optionValuesByName(projection.Project(OptionProfileQuery))

	if _, ok := values["board"]; ok {
		t.Error("board present, want global options skipped by the query profile")
	}
	if got := values["count"]; !got.Resolved || got.Value != 0 {
		t.Errorf("count = %#v, want default-filled 0", got)
	}
	if got := values["flag"]; !got.Resolved || got.Value != false {
		t.Errorf("flag = %#v, want resolved false", got)
	}
	if got := values["label"]; !got.Resolved || got.Value != "d" {
		t.Errorf("label = %#v, want default-filled d", got)
	}
}

func TestOptionProjectionQueryDoesNotMutateInputs(t *testing.T) {
	projection, defs := optionProjectionFixture()
	projection.Project(OptionProfileQuery)

	if len(defs) != 4 {
		t.Errorf("defs mutated: len = %d, want 4", len(defs))
	}
	if _, ok := defs["extra"]; ok {
		t.Error("defs mutated: unexpected key extra")
	}
	if got := projection.Values["board"]; got != "evb" {
		t.Errorf("values mutated: board = %v, want evb", got)
	}
	if len(projection.Values) != 2 {
		t.Errorf("values mutated: len = %d, want 2", len(projection.Values))
	}
}

func TestOptionProjectionUnknownProfilePanics(t *testing.T) {
	projection, _ := optionProjectionFixture()
	defer func() {
		if recover() == nil {
			t.Fatal("Project accepted an unknown profile")
		}
	}()
	projection.Project(OptionProfile(99))
}
