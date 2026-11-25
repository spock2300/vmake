package resolver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProjectFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolverInternalVisibilityLocalSubtree(t *testing.T) {
	root := canonicalTempDir(t)
	writeProjectFiles(t, root, map[string]string{
		"vmake.json": `{"roots": ["//net/http"]}`,
		"net/internal/wire/build.go": `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {}
`,
		"net/http/build.go": `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) { p.AddRequires("//net/internal/wire") }
`,
		"app/build.go": `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) { p.AddRequires("//net/internal/wire") }
`,
	})

	rOK := NewResolver(nil, filepath.Join(root, ".vmake_deps"))
	if err := rOK.ResolveProject(root, nil); err != nil {
		t.Fatalf("expected //net/http -> //net/internal/wire allowed, got %v", err)
	}

	rBad := NewResolver(nil, filepath.Join(root, ".vmake_deps"))
	err := rBad.ResolveProject(root, []string{"//app"})
	if err == nil || !strings.Contains(err.Error(), `use of internal package "//net/internal/wire" not allowed from package "//app"`) {
		t.Fatalf("expected internal visibility error, got %v", err)
	}

	var nilResolver *Resolver
	if got := nilResolver.VisibilityPath("/myorg/sdk/"); got != "myorg/sdk" {
		t.Fatalf("nilResolver.VisibilityPath = %q, want myorg/sdk", got)
	}
}
