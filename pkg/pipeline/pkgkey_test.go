package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/build"
)

func TestLocalPkgKeyMatchesJoinKeyExtra(t *testing.T) {
	for _, commit := range []string{"", "abc123"} {
		key := localPkgKeyFromParts("cc", "debug", map[string]any{"x": 1}, "flags", "script", commit)
		want := build.JoinKeyExtra("", commit, "flags", "", "script")
		if key.extra != want {
			t.Errorf("commit %q: extra = %q, want %q", commit, key.extra, want)
		}
		if key.buildKey() != build.BuildKey("cc", "debug", map[string]any{"x": 1}, want) {
			t.Errorf("commit %q: build key mismatch", commit)
		}
	}
}

func TestRemotePkgKeyMatchesJoinKeyExtra(t *testing.T) {
	key := remotePkgKeyFromParts("cc", "release", map[string]any{"x": 1}, "1.0.0", "commit", "flags", "patch", "script")
	want := build.JoinKeyExtra("1.0.0", "commit", "flags", "patch", "script")
	if key.extra != want {
		t.Errorf("extra = %q, want %q", key.extra, want)
	}
	if key.buildKey() != build.BuildKey("cc", "release", map[string]any{"x": 1}, want) {
		t.Error("build key mismatch")
	}
}

func TestPkgKeyGolden(t *testing.T) {
	local := localPkgKeyFromParts("cc", "debug", map[string]any{"x": 1}, "flags", "script", "abc123")
	if got, want := local.buildKey(), "d0c95aade2effcf2df39b7935054e0e646a7ce8ba700cf3e31bb005338479af6"; got != want {
		t.Errorf("local build key = %q, want %q", got, want)
	}
	remote := remotePkgKeyFromParts("cc", "release", map[string]any{"x": 1}, "1.0.0", "commit", "flags", "patch", "script")
	if got, want := remote.buildKey(), "a24cdbc0ed379ba5848fb66b7cc2339f8d1f0135f839b0865bff904f34ff5e2b"; got != want {
		t.Errorf("remote build key = %q, want %q", got, want)
	}
}

func TestLocalPkgDirsExactKey(t *testing.T) {
	opts := map[string]any{"x": 1}
	for _, commit := range []string{"", "abc123"} {
		layout, err := layoutFor(layoutInput{
			Local:     true,
			SourceDir: "/script",
			CCKey:     "cc",
			Mode:      "debug",
			Options:   opts,
			Material:  keyMaterial{commit: commit, flagsHash: "flags", scriptHash: "script"},
			Ready:     true,
		})
		if err != nil {
			t.Fatal(err)
		}
		wantKey := build.BuildKey("cc", "debug", opts, build.JoinKeyExtra("", commit, "flags", "", "script"))
		if layout.Dirs.SourceDir != "/script" {
			t.Errorf("commit %q: SourceDir = %q", commit, layout.Dirs.SourceDir)
		}
		if layout.Dirs.InstallDir != "" {
			t.Errorf("commit %q: local InstallDir = %q, want empty", commit, layout.Dirs.InstallDir)
		}
		if want := filepath.Join("/script", "build", wantKey); layout.Dirs.BuildDir != want {
			t.Errorf("commit %q: BuildDir = %q, want %q", commit, layout.Dirs.BuildDir, want)
		}
	}
}

func TestRemotePkgDirsExactKey(t *testing.T) {
	opts := map[string]any{"x": 1}
	material := keyMaterial{version: "1.0.0", commit: "commit", flagsHash: "flags", patchHash: "patch", scriptHash: "script"}
	wantKey := build.BuildKey("cc", "release", opts, build.JoinKeyExtra("1.0.0", "commit", "flags", "patch", "script"))
	for _, member := range []string{"", "nested/member"} {
		layout, err := layoutFor(layoutInput{
			PkgRoot:  "/vd",
			Member:   member,
			CCKey:    "cc",
			Mode:     "release",
			Options:  opts,
			Material: material,
			Ready:    true,
		})
		if err != nil {
			t.Fatal(err)
		}
		output := filepath.Join("/vd", "out", storage.OwnerKey(filepath.ToSlash(member)), wantKey)
		if want := filepath.Join("/vd", "src", filepath.FromSlash(member)); layout.Dirs.SourceDir != want {
			t.Errorf("member %q: SourceDir = %q, want %q", member, layout.Dirs.SourceDir, want)
		}
		if want := filepath.Join(output, "build"); layout.Dirs.BuildDir != want {
			t.Errorf("member %q: BuildDir = %q, want %q", member, layout.Dirs.BuildDir, want)
		}
		if want := filepath.Join(output, "install"); layout.Dirs.InstallDir != want {
			t.Errorf("member %q: InstallDir = %q, want %q", member, layout.Dirs.InstallDir, want)
		}
	}
}

func TestRemotePkgDirsMemberNotInBuildKey(t *testing.T) {
	material := keyMaterial{version: "1.0.0", commit: "commit", flagsHash: "flags", patchHash: "patch", scriptHash: "script"}
	root, err := layoutFor(layoutInput{PkgRoot: "/vd", CCKey: "cc", Mode: "release", Material: material, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	member, err := layoutFor(layoutInput{PkgRoot: "/vd", Member: "nested/member", CCKey: "cc", Mode: "release", Material: material, Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	rootKey := filepath.Base(filepath.Dir(root.Dirs.BuildDir))
	memberKey := filepath.Base(filepath.Dir(member.Dirs.BuildDir))
	if rootKey != root.BuildKey || memberKey != member.BuildKey || root.BuildKey != member.BuildKey {
		t.Errorf("member changed build key: root=%q member=%q want=%q", rootKey, memberKey, root.BuildKey)
	}
}
