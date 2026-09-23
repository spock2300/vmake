package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"
)

type buildInterrupted struct {
	code int
}

func (e *buildInterrupted) Error() string { return "build interrupted" }
func (e *buildInterrupted) Unwrap() error { return context.Canceled }

func withBuildContext(ctx *RuntimeContext, run func() error) (err error) {
	base := ctx.Context
	if base == nil {
		base = context.Background()
	}
	executionContext, cancel := context.WithCancel(base)
	ctx.Context = executionContext
	defer cancel()
	signals := make(chan os.Signal, 2)
	accepted := []os.Signal{os.Interrupt}
	if runtime.GOOS != "windows" {
		accepted = append(accepted, syscall.SIGTERM)
	}
	signal.Notify(signals, accepted...)
	stop := make(chan struct{})
	done := make(chan struct{})
	var exitCode atomic.Int32
	go func() {
		defer close(done)
		select {
		case sig := <-signals:
			code := int32(130)
			if sig == syscall.SIGTERM {
				code = 143
			}
			exitCode.Store(code)
			cancel()
			signal.Stop(signals)
			signal.Reset(accepted...)
		case <-stop:
		}
	}()
	defer func() {
		signal.Stop(signals)
		close(stop)
		<-done
		if code := exitCode.Load(); code != 0 {
			err = &buildInterrupted{code: int(code)}
		} else if ctx.Context.Err() != nil {
			err = fmt.Errorf("build canceled: %w", ctx.Context.Err())
		}
	}()
	return run()
}
