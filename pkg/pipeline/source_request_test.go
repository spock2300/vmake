package pipeline

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/build"
	"github.com/spock2300/vmake/pkg/repo"
)

func TestSourceRequestProjectionLocalSetGitUsesLocalURLRefresh(t *testing.T) {
	s := sessionFixture(t)
	node := s.ctx.DepGraph.Packages["//app"]
	upstream := initGitRepo(t, map[string]string{"build.go": "package main\n"})
	node.Pkg.SetGit(upstream)

	req, err := s.sourceStage.sourceRequestFor("//app", node, s.ctx.packageSource("//app", node))
	if err != nil {
		t.Fatal(err)
	}
	if want := repo.PackageTreeKey("", build.MemberDir("app")); req.Key != want {
		t.Errorf("key = %q, want %q", req.Key, want)
	}
	if req.Root != "" {
		t.Errorf("root = %q, want empty for a local package", req.Root)
	}
	if !req.Refresh {
		t.Error("a versionless local Git request must refresh like EnsureSource does")
	}
	if req.Ref != "" || req.Commit != "" {
		t.Errorf("ref/commit = %q/%q, want empty", req.Ref, req.Commit)
	}
}

func TestSourceRequestProjectionKeepsTargetCommitRef(t *testing.T) {
	s := sessionFixture(t)
	node := s.ctx.DepGraph.Packages["//app"]
	upstream := initGitRepo(t, map[string]string{"build.go": "package main\n"})
	node.Pkg.SetGit(upstream)
	commit := strings.Repeat("a", 40)
	s.ctx.LockPolicy.Targets = map[string]string{"//app": commit}

	req, err := s.sourceStage.sourceRequestFor("//app", node, s.ctx.packageSource("//app", node))
	if err != nil {
		t.Fatal(err)
	}
	if req.Ref != commit || req.Commit != commit {
		t.Errorf("ref/commit = %q/%q, want %q", req.Ref, req.Commit, commit)
	}
	if !req.Refresh {
		t.Error("a target pin must refresh the tree, matching EnsureSource")
	}
}

func TestSourceRequestProjectionPinnedVersion(t *testing.T) {
	s := sessionFixture(t)
	node := s.ctx.DepGraph.Packages["//app"]
	upstream := initGitRepo(t, map[string]string{"build.go": "package main\n"})
	node.Pkg.SetGit(upstream).AddVersion("1.0.0", "v1.0.0")
	node.Constraints = []string{"=1.0.0"}

	req, err := s.sourceStage.sourceRequestFor("//app", node, s.ctx.packageSource("//app", node))
	if err != nil {
		t.Fatal(err)
	}
	if req.Version != "1.0.0" || req.Ref != "v1.0.0" || req.Commit != "" {
		t.Errorf("version/ref/commit = %q/%q/%q", req.Version, req.Ref, req.Commit)
	}
	if req.Refresh {
		t.Error("a pinned version request must not refresh")
	}
}
