package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/build"
)

func TestLocalPkgKeyMatchesJoinKeyExtra(t *testing.T) {
	for _, commit := range []string{"", "abc123"} {
		key := newLocalPkgKey("cc", "debug", map[string]any{"x": 1}, "flags", "script", commit)
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
	key := newRemotePkgKey("cc", "release", map[string]any{"x": 1}, "1.0.0", "commit", "flags", "patch", "script")
	want := build.JoinKeyExtra("1.0.0", "commit", "flags", "patch", "script")
	if key.extra != want {
		t.Errorf("extra = %q, want %q", key.extra, want)
	}
	if key.buildKey() != build.BuildKey("cc", "release", map[string]any{"x": 1}, want) {
		t.Error("build key mismatch")
	}
}

func TestPkgKeyGolden(t *testing.T) {
	local := newLocalPkgKey("cc", "debug", map[string]any{"x": 1}, "flags", "script", "abc123")
	if got, want := local.buildKey(), "d0c95aade2effcf2df39b7935054e0e646a7ce8ba700cf3e31bb005338479af6"; got != want {
		t.Errorf("local build key = %q, want %q", got, want)
	}
	remote := newRemotePkgKey("cc", "release", map[string]any{"x": 1}, "1.0.0", "commit", "flags", "patch", "script")
	if got, want := remote.buildKey(), "a24cdbc0ed379ba5848fb66b7cc2339f8d1f0135f839b0865bff904f34ff5e2b"; got != want {
		t.Errorf("remote build key = %q, want %q", got, want)
	}
}

func TestLocalPkgDirsExactKey(t *testing.T) {
	opts := map[string]any{"x": 1}
	for _, commit := range []string{"", "abc123"} {
		dirs := makeLocalPkgDirs("/script", newLocalPkgKey("cc", "debug", opts, "flags", "script", commit))
		wantKey := build.BuildKey("cc", "debug", opts, build.JoinKeyExtra("", commit, "flags", "", "script"))
		if dirs.SourceDir != "/script" {
			t.Errorf("commit %q: SourceDir = %q", commit, dirs.SourceDir)
		}
		if dirs.InstallDir != "" {
			t.Errorf("commit %q: local InstallDir = %q, want empty", commit, dirs.InstallDir)
		}
		if want := filepath.Join("/script", "build", wantKey); dirs.BuildDir != want {
			t.Errorf("commit %q: BuildDir = %q, want %q", commit, dirs.BuildDir, want)
		}
	}
}

func TestRemotePkgDirsExactKey(t *testing.T) {
	opts := map[string]any{"x": 1}
	key := newRemotePkgKey("cc", "release", opts, "1.0.0", "commit", "flags", "patch", "script")
	wantKey := build.BuildKey("cc", "release", opts, build.JoinKeyExtra("1.0.0", "commit", "flags", "patch", "script"))
	for _, member := range []string{"", "nested/member"} {
		dirs := makeRemotePkgDirs("/vd", member, key)
		output := filepath.Join("/vd", "out", storage.OwnerKey(filepath.ToSlash(member)), wantKey)
		if want := filepath.Join("/vd", "src", filepath.FromSlash(member)); dirs.SourceDir != want {
			t.Errorf("member %q: SourceDir = %q, want %q", member, dirs.SourceDir, want)
		}
		if want := filepath.Join(output, "build"); dirs.BuildDir != want {
			t.Errorf("member %q: BuildDir = %q, want %q", member, dirs.BuildDir, want)
		}
		if want := filepath.Join(output, "install"); dirs.InstallDir != want {
			t.Errorf("member %q: InstallDir = %q, want %q", member, dirs.InstallDir, want)
		}
	}
}

func TestRemotePkgDirsMemberNotInBuildKey(t *testing.T) {
	key := newRemotePkgKey("cc", "release", nil, "1.0.0", "commit", "flags", "patch", "script")
	root := makeRemotePkgDirs("/vd", "", key)
	member := makeRemotePkgDirs("/vd", "nested/member", key)
	rootKey := filepath.Base(filepath.Dir(root.BuildDir))
	memberKey := filepath.Base(filepath.Dir(member.BuildDir))
	if rootKey != key.buildKey() || memberKey != key.buildKey() {
		t.Errorf("member changed build key: root=%q member=%q want=%q", rootKey, memberKey, key.buildKey())
	}
}
