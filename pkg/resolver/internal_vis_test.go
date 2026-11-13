package resolver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/buildscript"
)

func writeLocalScript(t *testing.T, root, relDir, body string) buildscript.Source {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(relDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "build.go")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return buildscript.Source{
		Name: filepath.Base(dir),
		Dir:  dir,
		Path: file,
	}
}

func TestResolverInternalVisibilityLocalSubtree(t *testing.T) {
	root := t.TempDir()
	wireSrc := writeLocalScript(t, root, "net/internal/wire", `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {}
`)
	httpSrc := writeLocalScript(t, root, "net/http", `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) { p.AddRequires("wire") }
`)
	appSrc := writeLocalScript(t, root, "app", `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) { p.AddRequires("wire") }
`)

	rOK := NewResolver(nil, filepath.Join(root, ".vmake_deps"))
	rOK.SetProjectRoot(root)
	if err := rOK.ResolveAll([]buildscript.Source{wireSrc, httpSrc}); err != nil {
		t.Fatalf("expected net/http -> net/internal/wire allowed, got %v", err)
	}

	rBad := NewResolver(nil, filepath.Join(root, ".vmake_deps"))
	rBad.SetProjectRoot(root)
	err := rBad.ResolveAll([]buildscript.Source{wireSrc, appSrc})
	if err == nil || !strings.Contains(err.Error(), `use of internal package "wire" not allowed from package "app"`) {
		t.Fatalf("expected internal visibility error, got %v", err)
	}

	var nilResolver *Resolver
	if got := nilResolver.VisibilityPath("/myorg/sdk/"); got != "myorg/sdk" {
		t.Fatalf("nilResolver.VisibilityPath = %q, want myorg/sdk", got)
	}
}
