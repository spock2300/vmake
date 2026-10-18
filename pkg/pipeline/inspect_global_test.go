package pipeline

import (
	"reflect"
	"testing"

	"github.com/spock2300/vmake/pkg/resolver"
)

func TestNeededGlobalFlags(t *testing.T) {
	r := resolver.NewResolver(nil, t.TempDir())
	r.Graph().Order = []string{"dep", "extra", "root"}
	ctx := &RuntimeContext{
		Resolver: r,
		BufferedGlobalFlags: map[string]*packageGlobalFlags{
			"dep": {
				cFlags:  []string{"-DDEP"},
				ldFlags: []string{"-ldep"},
				links:   []string{"-ldep"},
			},
			"extra": {
				cFlags:   []string{"-DEXTRA"},
				cxxFlags: []string{"-DEXTRA_CXX"},
				ldFlags:  []string{"-DEXTRA_LD"},
				links:    []string{"-lextra"},
			},
			"root": {
				cFlags:   []string{"-DROOT", "-DROOT"},
				cxxFlags: []string{"-std=c++20"},
				ldFlags:  []string{"-Wl,--gc-sections"},
				links:    []string{"-lm"},
			},
		},
	}

	cflags, cxxflags, ldflags, links := neededGlobalFlags(ctx, map[string]bool{"dep": true, "root": true})

	if want := []string{"-DDEP", "-DROOT", "-DROOT"}; !reflect.DeepEqual(cflags, want) {
		t.Errorf("cflags = %v, want %v", cflags, want)
	}
	if want := []string{"-std=c++20"}; !reflect.DeepEqual(cxxflags, want) {
		t.Errorf("cxxflags = %v, want %v", cxxflags, want)
	}
	if want := []string{"-ldep", "-Wl,--gc-sections"}; !reflect.DeepEqual(ldflags, want) {
		t.Errorf("ldflags = %v, want %v", ldflags, want)
	}
	if want := []string{"-ldep", "-lm"}; !reflect.DeepEqual(links, want) {
		t.Errorf("links = %v, want %v", links, want)
	}
	if cflags, cxxflags, ldflags, links := neededGlobalFlags(ctx, nil); cflags != nil || cxxflags != nil || ldflags != nil || links != nil {
		t.Errorf("no needed packages produced %v/%v/%v/%v, want nil", cflags, cxxflags, ldflags, links)
	}
}
