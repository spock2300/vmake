package quiet

import (
	"os"
	"sync"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/scriptfs"
	vlog "github.com/spock2300/vmake/pkg/log"
)

type Intent int

const (
	Probe Intent = iota
	ProbeSilent
	Script
	Quiet
)

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

func Streams() func() {
	return combine(SilenceStdout(), SilenceStderr())
}

func SilenceStdout() func() { return silenceStream(&os.Stdout) }

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
