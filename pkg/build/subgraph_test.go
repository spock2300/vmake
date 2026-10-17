package build

import (
	"sort"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func collectNames(got map[string]bool) []string {
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func assertCollected(t *testing.T, got map[string]bool, want ...string) {
	t.Helper()
	sort.Strings(want)
	names := collectNames(got)
	if len(names) != len(want) {
		t.Fatalf("collected packages = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("collected packages = %v, want %v", names, want)
		}
	}
}

func TestCollectBuildPackagesResolvesShortTargetDependency(t *testing.T) {
	app := makeTargetWithDeps("app", "lib:build")
	utils := makeTargetWithDeps("build")
	targets := map[string]map[string]*api.Target{
		"parent/tool": {"app": app},
		"parent/lib":  {"build": utils},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"parent/tool": {},
		"parent/lib":  {},
	}
	needed := map[string]bool{"parent/tool": true, "parent/lib": true}
	subParents := map[string]string{"parent/tool": "parent", "parent/lib": "parent"}

	assertCollected(t,
		CollectSubGraphPackages("parent/tool", pkgMeta, targets, needed, subParents),
		"parent/lib", "parent/tool")
}

func TestCollectBuildPackagesResolvesShortWildcardDependency(t *testing.T) {
	app := makeTargetWithDeps("app", "lib:*")
	lib := makeTargetWithDeps("lib")
	extra := makeTargetWithDeps("extra")
	targets := map[string]map[string]*api.Target{
		"parent/tool": {"app": app},
		"parent/lib":  {"lib": lib, "extra": extra},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"parent/tool": {},
		"parent/lib":  {},
	}
	needed := map[string]bool{"parent/tool": true, "parent/lib": true}
	subParents := map[string]string{"parent/tool": "parent", "parent/lib": "parent"}

	assertCollected(t,
		CollectSubGraphPackages("parent/tool", pkgMeta, targets, needed, subParents),
		"parent/lib", "parent/tool")
}

func TestCollectBuildPackagesIgnoresBareTargetDependency(t *testing.T) {
	app := makeTargetWithDeps("app", "utils")
	own := makeTargetWithDeps("utils")
	other := makeTargetWithDeps("utils")
	targets := map[string]map[string]*api.Target{
		"parent/tool": {"app": app, "utils": own},
		"utils":       {"utils": other},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"parent/tool": {},
		"utils":       {},
	}
	needed := map[string]bool{"parent/tool": true, "utils": true}
	subParents := map[string]string{"parent/tool": "parent"}

	assertCollected(t,
		CollectSubGraphPackages("parent/tool", pkgMeta, targets, needed, subParents),
		"parent/tool")
}

func TestCollectBuildPackagesExpandsAbsolutePackagePath(t *testing.T) {
	app := makeTargetWithDeps("app", "parent/lib")
	lib := makeTargetWithDeps("lib")
	targets := map[string]map[string]*api.Target{
		"parent/tool": {"app": app},
		"parent/lib":  {"lib": lib},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"parent/tool": {},
		"parent/lib":  {},
	}
	needed := map[string]bool{"parent/tool": true, "parent/lib": true}
	subParents := map[string]string{"parent/tool": "parent", "parent/lib": "parent"}

	assertCollected(t,
		CollectSubGraphPackages("parent/tool", pkgMeta, targets, needed, subParents),
		"parent/lib", "parent/tool")
}

func TestCollectBuildPackagesPrefersNearestShortDependency(t *testing.T) {
	app := makeTargetWithDeps("app", "lib:build")
	near := makeTargetWithDeps("build")
	far := makeTargetWithDeps("build")
	targets := map[string]map[string]*api.Target{
		"parent/tool": {"app": app},
		"parent/lib":  {"build": near},
		"lib":         {"build": far},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"parent/tool": {},
		"parent/lib":  {},
		"lib":         {},
	}
	needed := map[string]bool{"parent/tool": true, "parent/lib": true, "lib": true}
	subParents := map[string]string{"parent/tool": "parent", "parent/lib": "parent"}

	assertCollected(t,
		CollectSubGraphPackages("parent/tool", pkgMeta, targets, needed, subParents),
		"parent/lib", "parent/tool")
}
