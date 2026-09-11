package gitcmd

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/fs"
)

func TestArgsPrependsReproducibleConfig(t *testing.T) {
	got := Args("clone", "url", "dir")
	joined := strings.Join(got, " ")

	for _, want := range []string{
		"-c core.autocrlf=false",
		"-c core.eol=lf",
		"-c core.longpaths=true",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("Args() = %v, missing %q", got, want)
		}
	}

	tail := got[len(got)-3:]
	if tail[0] != "clone" || tail[1] != "url" || tail[2] != "dir" {
		t.Errorf("Args() = %v, original arguments must be preserved in order at the end", got)
	}
}

func TestArgsSymlinksFollowsCapability(t *testing.T) {
	joined := strings.Join(Args("status"), " ")
	hasSymlinks := strings.Contains(joined, "core.symlinks=true")
	if hasSymlinks != fs.SymlinksSupported() {
		t.Errorf("core.symlinks=%v but SymlinksSupported()=%v", hasSymlinks, fs.SymlinksSupported())
	}
}
