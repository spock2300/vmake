package resolver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
)

func TestHasCachedScriptMissingFile(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	if r.hasCachedScript("/nonexistent/build.so", "/path/build.go") {
		t.Error("missing script should return false")
	}
}

func TestHasCachedScriptEmptyFile(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "build.so")
	if err := os.WriteFile(empty, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(nil, t.TempDir())
	if r.hasCachedScript(empty, "/path/build.go") {
		t.Error("empty script file should return false")
	}
}

func TestHasCachedScriptFreshScriptNoBuildGo(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "build.so")
	if err := os.WriteFile(script, []byte("plugin content"), 0644); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(nil, t.TempDir())
	if !r.hasCachedScript(script, "") {
		t.Error("fresh non-empty script with no buildGoPath should return true")
	}
}

func TestHasCachedScriptStaleScriptVsBuildGo(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "build.so")
	buildGo := filepath.Join(dir, "build.go")

	oldTime := time.Now().Add(-1 * time.Hour)
	if err := os.WriteFile(script, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(script, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(buildGo, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(nil, t.TempDir())
	if r.hasCachedScript(script, buildGo) {
		t.Error("script older than build.go should return false")
	}
}

func TestHasCachedScriptFreshScriptAndBuildGo(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "build.so")
	buildGo := filepath.Join(dir, "build.go")

	oldTime := time.Now().Add(-1 * time.Hour)
	if err := os.WriteFile(buildGo, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(buildGo, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(nil, t.TempDir())
	if !r.hasCachedScript(script, buildGo) {
		t.Error("script newer than build.go should return true")
	}
}

func TestScriptPathAndOutputDir(t *testing.T) {
	r := NewResolver(nil, "/tmp/deps")
	if got := r.buildscriptOutputDir("foo"); got != "/tmp/deps/foo" {
		t.Errorf("buildscriptOutputDir = %q", got)
	}
	src := buildscript.NewSource("foo", "/p/build.go", "/p", "/tmp/deps/foo", api.SourceLocal, false)
	if got := r.scriptPath(src); got != "/tmp/deps/foo/build.so" {
		t.Errorf("scriptPath = %q", got)
	}
}

func TestNewResolverInitializes(t *testing.T) {
	r := NewResolver(nil, "/tmp/deps")
	if r.Graph() == nil {
		t.Error("Graph() should not be nil")
	}
	if r.Graph().Packages == nil {
		t.Error("Packages map should not be nil")
	}
	if r.GetOrder() != nil {
		t.Error("Order should be nil/empty initially")
	}
	if r.SubParents() == nil {
		t.Error("SubParents should not be nil")
	}
}

func TestSetForceAndGlobalSourcesDir(t *testing.T) {
	r := NewResolver(nil, "/tmp/deps")
	r.SetForce(true)
	r.SetGlobalSourcesDir("/global/sources")
}

func TestResolveDeferredEmpty(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	if err := r.ResolveDeferred(); err != nil {
		t.Errorf("ResolveDeferred with empty graph should succeed: %v", err)
	}
}

func TestResolveDeferredOnlyNonDeferred(t *testing.T) {
	r := NewResolver(nil, t.TempDir())
	r.graph.Packages["a"] = &PackageNode{ID: "a", Deferred: false, Deps: []string{}}
	r.graph.Packages["b"] = &PackageNode{ID: "b", Deferred: false, Deps: []string{"a"}}

	if err := r.ResolveDeferred(); err != nil {
		t.Errorf("ResolveDeferred with no deferred nodes should succeed: %v", err)
	}
}
