package resolver

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveIndependentConstraintIntersections(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	for _, count := range []int{1, 8} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			original := writeNativeRepoWithVersions(t)
			base := filepath.Dir(original)
			children := []string{}
			wide := map[string]string{}
			narrow := map[string]string{}
			for i := 0; i < count; i++ {
				leaf := fmt.Sprintf("leaf%d", i)
				gitCmd(t, base, "clone", "-q", "--bare", original, filepath.Join(base, leaf+".git"))
				key := "testnative/" + leaf
				children = append(children, key)
				wide[key] = ">=1.2"
				narrow[key] = "<1.5"
			}
			for _, s := range []struct {
				name   string
				bounds map[string]string
			}{{"mod_a", wide}, {"mod_b", narrow}} {
				manifest, err := json.Marshal(map[string]any{"modules": s.bounds})
				if err != nil {
					t.Fatal(err)
				}
				writeTaggedNativeModule(t, filepath.Join(base, s.name+".git"), map[string]string{
					"build.go": requirePackageMain(children...), "vmake.json": string(manifest),
				}, "v1.0.0")
			}
			r := newNativeTestResolver(t, original)
			t.Cleanup(r.Close)
			project := writeLocalProject(t, map[string]string{"testnative/mod_a": ">=1.0", "testnative/mod_b": ">=1.0"}, nil, "testnative/mod_a", "testnative/mod_b")
			err := r.ResolveProject(project, nil)
			if err != nil {
				t.Fatalf("valid graph with %d satisfiable intersections: %v", count, err)
			}
			for _, key := range children {
				n := r.Graph().Containers[key]
				if n == nil || n.Version != "1.2.0" {
					t.Fatalf("selected wrong version of %s", key)
				}
			}
		})
	}
}
