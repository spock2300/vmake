package main

import (
	"context"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/scriptfs"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

type inspectionRequest struct {
	execution     context.Context
	root          string
	overrides     []inspectOverride
	policy        *resolver.LockPolicy
	skipGitignore bool
	quiet         bool
	execQuiet     bool
}

type inspectionSession struct {
	ctx        *RuntimeContext
	inspection *pipeline.Inspection
	blocked    []string
	restore    func()
}

type inspectionStageError struct {
	err error
}

func (e *inspectionStageError) Error() string { return e.err.Error() }
func (e *inspectionStageError) Unwrap() error { return e.err }

func openInspection(req inspectionRequest) (*inspectionSession, error) {
	restore := inspectionScope(req.quiet, req.execQuiet)
	ctx, err := openProject(projectOptions{
		root:          req.root,
		execution:     req.execution,
		overrides:     req.overrides,
		policy:        req.policy,
		skipGitignore: req.skipGitignore,
	})
	if err != nil {
		restore()
		return nil, err
	}
	inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{FilterDeps: true})
	if err != nil {
		scriptfs.TakeBlockedWrites()
		closeProject(ctx)
		restore()
		return nil, &inspectionStageError{err: err}
	}
	return &inspectionSession{ctx: ctx, inspection: inspection, blocked: scriptfs.TakeBlockedWrites(), restore: restore}, nil
}

func (s *inspectionSession) Close() {
	if s != nil {
		closeProject(s.ctx)
		if s.restore != nil {
			s.restore()
		}
	}
}

func inspectionScope(quiet, execQuiet bool) func() {
	scriptfs.SetReadOnly(true)
	previousLevel := vlog.GetLevel()
	previousQuiet := iexec.IsQuiet()
	if quiet {
		vlog.SetLevel(vlog.Quiet)
	}
	if execQuiet {
		iexec.SetQuiet(true)
	}
	return func() {
		scriptfs.SetReadOnly(false)
		vlog.SetLevel(previousLevel)
		iexec.SetQuiet(previousQuiet)
	}
}
