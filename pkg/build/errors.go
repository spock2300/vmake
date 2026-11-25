package build

import (
	"context"
	"errors"
	"fmt"
)

type TargetError struct {
	Target string
	Stage  string
	Err    error
}

func (e *TargetError) Error() string {
	return fmt.Sprintf("%s: %v", e.Target, e.Err)
}

func (e *TargetError) Unwrap() error { return e.Err }

func wrapTargetError(target, stage string, err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	var targetErr *TargetError
	if errors.As(err, &targetErr) {
		return err
	}
	return &TargetError{Target: target, Stage: stage, Err: err}
}

const (
	IssueKindMissingDependency  = "missing-dependency"
	IssueKindPackageCycle       = "package-cycle"
	IssueKindTargetCycle        = "target-cycle"
	IssueKindInternalVisibility = "internal-visibility"
	IssueKindResolve            = "resolve"
)

type ResolveError struct {
	Kind string
	Err  error
}

func (e *ResolveError) Error() string { return e.Err.Error() }

func (e *ResolveError) Unwrap() error { return e.Err }
