package main

import (
	exec "github.com/spock2300/vmake/internal/exec"
	vlog "github.com/spock2300/vmake/pkg/log"
)

type vmakeLogger struct{}

func (vmakeLogger) Debug(format string, args ...any) { vlog.Debug(format, args...) }
func (vmakeLogger) Error(format string, args ...any) { vlog.Error(format, args...) }
func (vmakeLogger) Fatal(format string, args ...any) { vlog.Fatal(format, args...) }

func init() {
	exec.SetLogger(vmakeLogger{})
}
