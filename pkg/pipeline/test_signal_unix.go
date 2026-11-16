//go:build unix

package pipeline

import (
	"os/exec"
	"syscall"
)

func testProcessSignal(exitErr *exec.ExitError) (string, bool) {
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return "", false
	}
	return status.Signal().String(), true
}
