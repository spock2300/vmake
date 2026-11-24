package main

import (
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/pkg/buildreport"
	"github.com/spock2300/vmake/pkg/pipeline"
	"github.com/spock2300/vmake/pkg/resolver"
)

// inspectionRequest is the project the command inspects plus how quiet the
// preamble should be. Project fields live in projectOptions so the request can
// express everything openProject accepts.
type inspectionRequest struct {
	project   projectOptions
	quiet     bool
	execQuiet bool
	facts     bool
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
	ctx, err := openProject(req.project)
	if err != nil {
		restore()
		return nil, err
	}
	root := ""
	if ctx.Paths != nil {
		root = ctx.Paths.ProjectDir
	}
	inspection, err := pipeline.InspectWithOptions(ctx, pipeline.InspectOptions{
		FilterDeps:  true,
		Facts:       req.facts,
		BuildReport: buildreport.ReportPath(root),
		TestReport:  buildreport.TestReportPath(root),
		Overrides:   inspectOverrideValues(req.project.overrides),
	})
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

// lockPolicyFor is the single construction of a command's lock policy: a
// locked build is also a strict-wrapper build.
func lockPolicyFor(dryRun, locked, offline bool) resolver.LockPolicy {
	return resolver.LockPolicy{DryRun: dryRun, Locked: locked, StrictWrapper: locked, Offline: offline}
}

func inspectionScope(quiet, execQuiet bool) func() {
	return pipeline.EnterQuietReadOnlyScope(quiet, execQuiet)
}
