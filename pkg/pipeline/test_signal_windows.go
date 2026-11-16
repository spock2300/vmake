//go:build windows

package pipeline

import "os/exec"

func testProcessSignal(*exec.ExitError) (string, bool) { return "", false }
