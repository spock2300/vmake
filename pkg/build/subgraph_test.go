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

func TestCollectBuildPackagesResolvesIntraContainerTargetDependency(t *testing.T) {
	app := makeTargetWithDeps("app", "//lib:build")
	utils := makeTargetWithDeps("build")
	targets := map[string]map[string]*api.Target{
		"repo/parent/tool": {"app": app},
		"repo/parent/lib":  {"build": utils},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"repo/parent/tool": {Container: "repo/parent"},
		"repo/parent/lib":  {Container: "repo/parent"},
	}
	needed := map[string]bool{"repo/parent/tool": true, "repo/parent/lib": true}

	assertCollected(t,
		CollectSubGraphPackages("repo/parent/tool", pkgMeta, targets, needed),
		"repo/parent/lib", "repo/parent/tool")
}

func TestCollectBuildPackagesResolvesIntraContainerWildcardDependency(t *testing.T) {
	app := makeTargetWithDeps("app", "//lib:*")
	lib := makeTargetWithDeps("lib")
	extra := makeTargetWithDeps("extra")
	targets := map[string]map[string]*api.Target{
		"repo/parent/tool": {"app": app},
		"repo/parent/lib":  {"lib": lib, "extra": extra},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"repo/parent/tool": {Container: "repo/parent"},
		"repo/parent/lib":  {Container: "repo/parent"},
	}
	needed := map[string]bool{"repo/parent/tool": true, "repo/parent/lib": true}

	assertCollected(t,
		CollectSubGraphPackages("repo/parent/tool", pkgMeta, targets, needed),
		"repo/parent/lib", "repo/parent/tool")
}

func TestCollectBuildPackagesIgnoresBareTargetDependency(t *testing.T) {
	app := makeTargetWithDeps("app", "utils")
	own := makeTargetWithDeps("utils")
	other := makeTargetWithDeps("utils")
	targets := map[string]map[string]*api.Target{
		"repo/parent/tool": {"app": app, "utils": own},
		"//utils":          {"utils": other},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"repo/parent/tool": {Container: "repo/parent"},
		"//utils":          {Container: "//"},
	}
	needed := map[string]bool{"repo/parent/tool": true, "//utils": true}

	assertCollected(t,
		CollectSubGraphPackages("repo/parent/tool", pkgMeta, targets, needed),
		"repo/parent/tool")
}

func TestCollectBuildPackagesExpandsCanonicalPackagePath(t *testing.T) {
	app := makeTargetWithDeps("app", "repo/parent/lib")
	lib := makeTargetWithDeps("lib")
	targets := map[string]map[string]*api.Target{
		"repo/parent/tool": {"app": app},
		"repo/parent/lib":  {"lib": lib},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"repo/parent/tool": {Container: "repo/parent"},
		"repo/parent/lib":  {Container: "repo/parent"},
	}
	needed := map[string]bool{"repo/parent/tool": true, "repo/parent/lib": true}

	assertCollected(t,
		CollectSubGraphPackages("repo/parent/tool", pkgMeta, targets, needed),
		"repo/parent/lib", "repo/parent/tool")
}

func TestCollectBuildPackagesResolvesIntraContainerRequireDependency(t *testing.T) {
	app := makeTargetWithDeps("app")
	lib := makeTargetWithDeps("lib")
	targets := map[string]map[string]*api.Target{
		"repo/parent/tool": {"app": app},
		"repo/parent/lib":  {"lib": lib},
	}
	pkgMeta := map[string]PkgBuildMeta{
		"repo/parent/tool": {Container: "repo/parent", Deps: []string{"//lib"}},
		"repo/parent/lib":  {Container: "repo/parent"},
	}
	needed := map[string]bool{"repo/parent/tool": true, "repo/parent/lib": true}

	assertCollected(t,
		CollectSubGraphPackages("repo/parent/tool", pkgMeta, targets, needed),
		"repo/parent/lib", "repo/parent/tool")
}
