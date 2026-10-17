package api

import (
	"reflect"
	"testing"
)

func TestResolveSubPackageNameAbsolute(t *testing.T) {
	exists := func(name string) bool { return false }
	got := ResolveSubPackageName("anything", "foo/bar", nil, exists)
	if got != "foo/bar" {
		t.Errorf("absolute name should pass through, got %q", got)
	}
}

func TestResolveSubPackageNameNoParent(t *testing.T) {
	exists := func(name string) bool { return false }
	got := ResolveSubPackageName("pkg", "dep", map[string]string{}, exists)
	if got != "dep" {
		t.Errorf("no parent → bare name, got %q", got)
	}
}

func TestResolveSubPackageNameWalksUpChain(t *testing.T) {
	subParents := map[string]string{
		"root/child/grandchild": "root",
		"root/child":            "root",
	}
	existing := map[string]bool{"root/dep": true}
	exists := func(name string) bool { return existing[name] }

	got := ResolveSubPackageName("root/child/grandchild", "dep", subParents, exists)
	if got != "root/dep" {
		t.Errorf("walked-up resolve = %q, want root/dep", got)
	}
}

func TestResolveSubPackageNameDirectMatch(t *testing.T) {
	subParents := map[string]string{"parent/sub": "parent"}
	existing := map[string]bool{"parent/sub/dep": true}
	exists := func(name string) bool { return existing[name] }

	got := ResolveSubPackageName("parent/sub", "dep", subParents, exists)
	if got != "parent/sub/dep" {
		t.Errorf("direct match = %q, want parent/sub/dep", got)
	}
}

func TestResolveSubPackageNameFallbackToBare(t *testing.T) {
	subParents := map[string]string{"parent/sub": "parent"}
	exists := func(name string) bool { return false }

	got := ResolveSubPackageName("parent/sub", "dep", subParents, exists)
	if got != "dep" {
		t.Errorf("no match should fall back to bare, got %q", got)
	}
}

func TestSubPackageCandidates(t *testing.T) {
	subParents := map[string]string{
		"root/child/grandchild": "root",
		"root/child":            "root",
	}
	got := SubPackageCandidates("root/child/grandchild", "dep", subParents)
	want := []string{"root/child/grandchild/dep", "root/child/dep", "root/dep"}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidates[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSubPackageCandidatesNoParentOrAbsolute(t *testing.T) {
	if c := SubPackageCandidates("pkg", "dep", map[string]string{}); c != nil {
		t.Errorf("no parent → nil candidates, got %v", c)
	}
	if c := SubPackageCandidates("root/child", "a/dep", map[string]string{"root/child": "root"}); c != nil {
		t.Errorf("absolute dep → nil candidates, got %v", c)
	}
}

func TestSubPackageCandidatesIncludeAllAncestors(t *testing.T) {
	subParents := map[string]string{
		"root/a/b/sub": "root",
		"root/a":       "root",
	}
	got := SubPackageCandidates("root/a/b/sub", "dep", subParents)
	want := []string{
		"root/a/b/sub/dep",
		"root/a/b/dep",
		"root/a/dep",
		"root/dep",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
}

func TestResolveSubPackageNamePrefersNearestCandidate(t *testing.T) {
	subParents := map[string]string{"root/a/sub": "root"}
	existing := map[string]bool{
		"root/dep":     true,
		"root/a/dep":   true,
		"root/a/sub/x": true,
	}
	got := ResolveSubPackageName("root/a/sub", "dep", subParents, func(name string) bool {
		return existing[name]
	})
	if got != "root/a/dep" {
		t.Fatalf("nearest candidate = %q, want root/a/dep", got)
	}
}

func TestResolveSubPackageNameNestedChild(t *testing.T) {
	subParents := map[string]string{"root/a": "root"}
	got := ResolveSubPackageName("root/a", "child", subParents, func(name string) bool {
		return name == "root/a/child"
	})
	if got != "root/a/child" {
		t.Fatalf("nested child = %q, want root/a/child", got)
	}
}

func TestResolveSubPackageNameSkipsUnknownIntermediate(t *testing.T) {
	subParents := map[string]string{"root/a/b/sub": "root"}
	got := ResolveSubPackageName("root/a/b/sub", "dep", subParents, func(name string) bool {
		return name == "root/a/dep"
	})
	if got != "root/a/dep" {
		t.Fatalf("walk-up = %q, want root/a/dep", got)
	}
}

func TestResolveSubPackageNameDoesNotConsultExistsForAbsolute(t *testing.T) {
	calls := 0
	got := ResolveSubPackageName("root/a", "other/dep", map[string]string{"root/a": "root"}, func(string) bool {
		calls++
		return true
	})
	if got != "other/dep" || calls != 0 {
		t.Fatalf("absolute resolve = %q with %d callbacks, want passthrough without callbacks", got, calls)
	}
}

func TestResolveSubPackageNameDoesNotConsultExistsWithoutParent(t *testing.T) {
	calls := 0
	got := ResolveSubPackageName("pkg", "dep", map[string]string{}, func(string) bool {
		calls++
		return true
	})
	if got != "dep" || calls != 0 {
		t.Fatalf("top-level resolve = %q with %d callbacks, want bare name without callbacks", got, calls)
	}
}

func TestSubPackageCandidatesTerminatesOnNonAncestorParent(t *testing.T) {
	subParents := map[string]string{"root/a": "other/root"}
	got := SubPackageCandidates("root/a", "dep", subParents)
	want := []string{"root/a/dep", "root/dep"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
}

func TestSubPackageCandidatesDoNotMutateParents(t *testing.T) {
	subParents := map[string]string{"root/a": "root"}
	before := map[string]string{"root/a": "root"}
	SubPackageCandidates("root/a", "dep", subParents)
	if !reflect.DeepEqual(subParents, before) {
		t.Fatalf("subParents mutated: %v, want %v", subParents, before)
	}
}
