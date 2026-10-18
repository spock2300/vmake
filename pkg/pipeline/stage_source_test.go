package pipeline

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/resolver"
)

const sourceDownloadHeading = "Downloading package sources..."

func capturePipelineStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		defer r.Close()
		var sb strings.Builder
		_, _ = io.Copy(&sb, r)
		done <- sb.String()
	}()
	defer func() {
		os.Stdout = old
		_ = w.Close()
	}()
	fn()
	_ = w.Close()
	return <-done
}

func prepareAllPackagesWithOutput(t *testing.T, s *sourceStage) string {
	t.Helper()
	return capturePipelineStdout(t, func() {
		if err := s.prepareAllPackages(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPrepareAllPackagesOmitsDownloadHeadingWithoutSourceWork(t *testing.T) {
	s := sessionFixture(t)
	s.ctx.DepGraph.Packages["app"].Deps = []string{"dep"}
	vlog.SetLevel(vlog.Normal)
	defer vlog.SetLevel(vlog.Normal)
	if out := prepareAllPackagesWithOutput(t, s.sourceStage); strings.Contains(out, sourceDownloadHeading) {
		t.Fatalf("no-source build printed the download heading:\n%s", out)
	} else if out != "" {
		t.Fatalf("no-source build wrote stdout: %q", out)
	}

	// A materialized source tree is reused without source work.
	upstream := initGitRepo(t, map[string]string{"build.go": "package main\n"})
	testGit(t, upstream, "tag", "v1.0.0")
	node := s.ctx.DepGraph.Packages["app"]
	node.Pkg.SetGit(upstream).AddVersion("1.0.0", "v1.0.0")
	node.Constraints = []string{"=1.0.0"}
	if out := prepareAllPackagesWithOutput(t, s.sourceStage); !strings.HasPrefix(out, "\n"+sourceDownloadHeading+"\n") {
		t.Fatalf("missing local source build omitted the download heading:\n%s", out)
	}
	fresh := newSourceStage(s.runContext, s.configStage)
	if out := prepareAllPackagesWithOutput(t, fresh); strings.Contains(out, sourceDownloadHeading) {
		t.Fatalf("materialized local source build printed the download heading:\n%s", out)
	} else if out != "" {
		t.Fatalf("materialized local source build wrote stdout: %q", out)
	}
}

func TestPrepareAllPackagesPrintsDownloadHeadingForRemoteSource(t *testing.T) {
	upstream := initGitRepo(t, map[string]string{
		"build.go": "package main\n",
		"input.c":  "int value = 1;\n",
	})
	testGit(t, upstream, "tag", "v1.0.0")
	s := sessionFixture(t)
	delete(s.ctx.DepGraph.Packages, "app")
	delete(s.ctx.DepGraph.Packages, "dep")
	s.ctx.DepGraph.Order = []string{"native/sample"}
	s.needed = map[string]bool{"native/sample": true}
	node := resolver.NewPackageNode("native/sample",
		buildscript.NewSource("native/sample", filepath.Join(upstream, "build.go"), upstream, api.SourceRemote),
		api.NewPackage().SetRepo("native").SetName("sample"))
	node.Pkg.SetGit(upstream).AddVersion("1.0.0", "v1.0.0")
	s.ctx.DepGraph.Packages["native/sample"] = node
	config.SetEntry(s.ctx.Config, "native/sample", &config.EntryConfig{Version: "1.0.0"})

	vlog.SetLevel(vlog.Normal)
	defer vlog.SetLevel(vlog.Normal)
	if out := prepareAllPackagesWithOutput(t, s.sourceStage); !strings.HasPrefix(out, "\n"+sourceDownloadHeading+"\n") {
		t.Fatalf("missing remote source build omitted the download heading:\n%s", out)
	}
	commit := s.remote.commits["native/sample"]
	if commit == "" {
		t.Fatal("first remote build did not record a commit")
	}
	lock := lockfile.New()
	lock.Set("native/sample", &lockfile.LockedPkg{Version: "1.0.0", Commit: commit, Source: "native"})
	s.ctx.Lock = lock
	fresh := newSourceStage(s.runContext, s.configStage)
	if out := prepareAllPackagesWithOutput(t, fresh); strings.Contains(out, sourceDownloadHeading) {
		t.Fatalf("locked remote source build printed the download heading:\n%s", out)
	} else if out != "" {
		t.Fatalf("locked remote source build wrote stdout: %q", out)
	}
}

func TestPrepareAllPackagesQuietKeepsDownloadHeadingHidden(t *testing.T) {
	upstream := initGitRepo(t, map[string]string{"build.go": "package main\n"})
	testGit(t, upstream, "tag", "v1.0.0")
	s := sessionFixture(t)
	node := s.ctx.DepGraph.Packages["app"]
	node.Pkg.SetGit(upstream).AddVersion("1.0.0", "v1.0.0")
	node.Constraints = []string{"=1.0.0"}

	vlog.SetLevel(vlog.Quiet)
	defer vlog.SetLevel(vlog.Normal)
	out := capturePipelineStdout(t, func() {
		if err := s.prepareAllPackages(); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, sourceDownloadHeading) {
		t.Fatalf("quiet build printed the download heading:\n%s", out)
	}
	if out != "" {
		t.Fatalf("quiet source preparation wrote stdout: %q", out)
	}
}
