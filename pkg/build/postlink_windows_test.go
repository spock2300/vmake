package build

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func TestPostLinkPrefixedWindowsOutput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project space")
	buildDir := filepath.Join(dir, "build")
	output := filepath.Join(buildDir, "firmware")
	derived := output + ".bin"
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{output, derived} {
		if err := os.WriteFile(path, []byte("firmware"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	target := api.NewTargetRegistry().Target("firmware").SetKind(api.TargetBinary).SetDefault(true).
		AddPostLink("objcopy", "-O", "binary", "{output}", "./{output}.bin").
		AddPostLinkOutputs("./{output}.bin", "./{output}")
	args := expandPostLinkArgs(target.PostLinkSteps()[0].Args, dir, output)
	wantArgs := []string{"-O", "binary", "build/firmware", "./build/firmware.bin"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("post-link arguments=%v, want %v", args, wantArgs)
	}
	if paths := postLinkOutputPaths(target, dir, output); !reflect.DeepEqual(paths, []string{derived}) {
		t.Fatalf("declared outputs=%v, want [%s]", paths, derived)
	}
	graph, err := NewBuildGraph(map[string]map[string]*api.Target{"p": {"firmware": target}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	node := graph.Nodes["p:firmware"]
	resolved := &ResolvedTarget{Node: node, OutputPath: output}
	dirs := &api.PkgDirs{SourceDir: dir, BuildDir: buildDir}
	scheduler := &Scheduler{
		pkgs:      map[string]*PkgInfo{"p": {PkgDirs: *dirs}},
		toolchain: &toolchain.Toolchain{},
		platform:  api.Platform{OS: "none"},
	}
	if scheduler.needRelink(resolved, nil) {
		t.Fatal("existing prefixed post-link output triggers relinking")
	}
	prefix := filepath.Join(dir, "install")
	installer := NewArtifactInstaller(graph, map[string]*api.PkgDirs{"p": dirs}, prefix)
	installer.SetPackageInfo("p", &PkgInstallInfo{BuildDir: buildDir, TargetOS: "none"})
	if err := installer.InstallAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(prefix, "bin", "firmware.bin")); err != nil {
		t.Fatalf("prefixed post-link output not installed: %v", err)
	}
	if err := os.Remove(derived); err != nil {
		t.Fatal(err)
	}
	if !scheduler.needRelink(resolved, nil) {
		t.Fatal("missing prefixed post-link output does not trigger relinking")
	}
}
