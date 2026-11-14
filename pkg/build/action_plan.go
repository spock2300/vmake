package build

import (
	"context"
	"fmt"

	vlog "github.com/spock2300/vmake/pkg/log"
)

// plannedAction is the single description of one incremental action: the
// identity material that is hashed, the commands that are executed and the
// inputs/outputs that are recorded. Compile, link and incremental void
// producers all build one of these instead of repeating the
// check/invalidate/run/save protocol.
type plannedAction struct {
	Kind            string
	Subject         string
	Target          string
	WorkDir         string
	TargetOS        string
	Tools           string
	Environment     string
	Commands        []commandSpec
	Inputs          []string
	RecordInputs    []string
	SignatureInputs []string
	Outputs         []string
	RecordPath      string

	// Force bypasses the up-to-date check for reasons that cannot be encoded
	// in the signature, such as a missing dependency file.
	Force       bool
	ForceReason string
}

func (a *plannedAction) signature() (string, error) {
	return digestValue(struct {
		Version     int
		Kind        string
		Target      string
		WorkDir     string
		TargetOS    string
		Tools       string
		Environment string
		Commands    []commandSpec
		Inputs      []string
		Outputs     []string
	}{actionRecordVersion, a.Kind, a.Target, a.WorkDir, a.TargetOS, a.Tools, a.Environment, a.Commands, a.SignatureInputs, a.Outputs})
}

// runPlannedAction checks, runs and records one action. run executes the
// action and returns inputs discovered while running (dependency files); those
// are recorded but are not part of the pre-run signature, matching the
// historical compile behaviour. It returns the post-run dependency inputs and
// whether the action was already up to date.
func (s *Scheduler) runPlannedAction(ctx context.Context, action *plannedAction, run func(context.Context) ([]string, error)) (upToDate bool, postInputs []string, err error) {
	if action == nil {
		return false, nil, fmt.Errorf("nil planned action")
	}
	signature, err := action.signature()
	if err != nil {
		return false, nil, err
	}
	decision := checkAction(s.hashes, action.RecordPath, signature, action.WorkDir, action.Inputs, action.Outputs)
	if action.Force && decision.UpToDate {
		decision = actionDecision{Reason: action.ForceReason}
	}
	if decision.UpToDate {
		if s.stats != nil {
			s.stats.RecordUpToDate()
		}
		return true, action.Inputs, nil
	}
	if s.explain {
		subject := action.Subject
		if subject == "" {
			subject = action.Target
		}
		vlog.Info("  EXPLAIN %s: %s", subject, decision.Reason)
	}
	if err := invalidateActionRecord(action.RecordPath); err != nil {
		return false, nil, err
	}
	if err := ctx.Err(); err != nil {
		return false, nil, err
	}
	discovered, err := run(ctx)
	if err != nil {
		return false, nil, err
	}
	recordInputs := action.RecordInputs
	if recordInputs == nil {
		recordInputs = action.Inputs
	}
	inputs := append(append([]string{}, recordInputs...), discovered...)
	if err := saveActionRecord(s.hashes, action.RecordPath, signature, action.WorkDir, inputs, action.Outputs); err != nil {
		return false, nil, err
	}
	return false, discovered, nil
}
