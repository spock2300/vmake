package api

import "testing"

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
