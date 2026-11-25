// Package quiet owns every way a command silences output: the process streams,
// the log level, subprocess output and the interpreted-script read-only guard.
// Callers get one restore function and never sequence the levers themselves.
package quiet

import (
	"os"
	"sync"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/scriptfs"
	vlog "github.com/spock2300/vmake/pkg/log"
)

// Intent names the silencing a command needs.
type Intent int

const (
	// Probe quiets logging, subprocess output and script writes while a
	// command resolves a project, but leaves the streams alone so the command
	// can still print its result.
	Probe Intent = iota
	// ProbeSilent is Probe plus both streams silenced.
	ProbeSilent
	// Script silences both streams around interpreted script output.
	Script
	// Quiet silences stdout while keeping the log, for a quiet build summary.
	Quiet
)

// Enter applies intent and returns an idempotent restore.
func Enter(intent Intent) func() {
	switch intent {
	case Probe:
		return ProbeScope(true, true)
	case ProbeSilent:
		return combine(ProbeScope(true, true), Streams())
	case Script:
		return Streams()
	case Quiet:
		return SilenceStdout()
	}
	return func() {}
}

// ProbeScope applies the read-only, log and subprocess levers independently.
// The log and subprocess levers differ at one call site, so they stay explicit.
func ProbeScope(quietLog, quietExec bool) func() {
	scriptfs.SetReadOnly(true)
	previousLevel := vlog.GetLevel()
	previousExecQuiet := iexec.IsQuiet()
	if quietLog {
		vlog.SetLevel(vlog.Quiet)
	}
	if quietExec {
		iexec.SetQuiet(true)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			scriptfs.SetReadOnly(false)
			vlog.SetLevel(previousLevel)
			iexec.SetQuiet(previousExecQuiet)
		})
	}
}

// Streams silences both process streams.
func Streams() func() {
	return combine(SilenceStdout(), SilenceStderr())
}

// SilenceStdout silences stdout.
func SilenceStdout() func() { return silenceStream(&os.Stdout) }

// SilenceStderr silences stderr.
func SilenceStderr() func() { return silenceStream(&os.Stderr) }

func silenceStream(stream **os.File) func() {
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return func() {}
	}
	original := *stream
	*stream = devNull
	var once sync.Once
	return func() {
		once.Do(func() {
			*stream = original
			devNull.Close()
		})
	}
}

func combine(restores ...func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			for i := len(restores) - 1; i >= 0; i-- {
				restores[i]()
			}
		})
	}
}
