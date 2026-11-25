package quiet

import (
	"os"
	"testing"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/scriptfs"
	vlog "github.com/spock2300/vmake/pkg/log"
)

func reset(t *testing.T) {
	t.Helper()
	vlog.SetLevel(vlog.Normal)
	iexec.SetQuiet(false)
	scriptfs.SetReadOnly(false)
}

func TestEnterProbeLeavesStreamsAndRestoresEveryLever(t *testing.T) {
	reset(t)
	stdout := os.Stdout
	restore := Enter(Probe)
	if !scriptfs.IsReadOnly() {
		t.Fatal("Probe did not enter the read-only scope")
	}
	if vlog.GetLevel() != vlog.Quiet || !iexec.IsQuiet() {
		t.Fatalf("Probe left the log levers alone: level %v, exec quiet %v", vlog.GetLevel(), iexec.IsQuiet())
	}
	if os.Stdout != stdout {
		t.Fatal("Probe swapped stdout; a command must still print its result")
	}
	restore()
	if scriptfs.IsReadOnly() || vlog.GetLevel() != vlog.Normal || iexec.IsQuiet() {
		t.Fatal("Probe did not restore every lever")
	}
}

func TestEnterProbeSilentAlsoSilencesBothStreams(t *testing.T) {
	reset(t)
	stdout, stderr := os.Stdout, os.Stderr
	restore := Enter(ProbeSilent)
	if os.Stdout == stdout || os.Stderr == stderr {
		t.Fatal("ProbeSilent did not swap both streams")
	}
	restore()
	if os.Stdout != stdout || os.Stderr != stderr {
		t.Fatal("ProbeSilent did not restore both streams")
	}
}

func TestEnterScriptLeavesLogLeversAlone(t *testing.T) {
	reset(t)
	stdout, stderr := os.Stdout, os.Stderr
	restore := Enter(Script)
	if os.Stdout == stdout || os.Stderr == stderr {
		t.Fatal("Script did not swap both streams")
	}
	if vlog.GetLevel() != vlog.Normal || iexec.IsQuiet() || scriptfs.IsReadOnly() {
		t.Fatal("Script changed a log lever it does not own")
	}
	restore()
}

func TestEnterQuietSilencesStdoutOnly(t *testing.T) {
	reset(t)
	stdout, stderr := os.Stdout, os.Stderr
	restore := Enter(Quiet)
	if os.Stdout == stdout {
		t.Fatal("Quiet did not swap stdout")
	}
	if os.Stderr != stderr {
		t.Fatal("Quiet swapped stderr; the log must still reach it")
	}
	restore()
	if os.Stdout != stdout {
		t.Fatal("Quiet did not restore stdout")
	}
}

func TestRestoreIsIdempotent(t *testing.T) {
	reset(t)
	stdout, stderr := os.Stdout, os.Stderr
	restore := Enter(ProbeSilent)
	restore()
	restore()
	restore()
	if os.Stdout != stdout || os.Stderr != stderr || scriptfs.IsReadOnly() || iexec.IsQuiet() {
		t.Fatal("a repeated restore changed state")
	}
}
