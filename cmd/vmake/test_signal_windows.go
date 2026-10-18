//go:build windows

package main

import "os/exec"

const testSignalsSupported = false

func testProcessSignal(*exec.ExitError) (string, bool) { return "", false }
