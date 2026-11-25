package resolver

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	vlog "github.com/spock2300/vmake/pkg/log"
)

func captureResolverStdout(t *testing.T, fn func()) string {
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

func TestResolveNativeLogsVersionListing(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	vlog.SetLevel(vlog.VerboseLevel)
	defer vlog.SetLevel(vlog.Normal)
	url := writeNativeRepoWithVersions(t)
	r := newNativeTestResolver(t, url)
	project := writeLocalProject(t, map[string]string{"testnative/conflictpkg": ">=1.2"}, nil, "testnative/conflictpkg")

	out := captureResolverStdout(t, func() {
		if err := r.ResolveProject(project, nil); err != nil {
			t.Fatal(err)
		}
	})

	want := "testnative/conflictpkg: listing versions from "
	if !strings.Contains(out, want) {
		t.Fatalf("stdout missing %q:\n%s", want, out)
	}
}
