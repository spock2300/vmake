//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

const testSignalsSupported = true

func testProcessSignal(exitErr *exec.ExitError) (string, bool) {
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return "", false
	}
	return status.Signal().String(), true
}
