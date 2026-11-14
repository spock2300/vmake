package build

import (
	"context"
	"fmt"
	"slices"

	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type targetResult struct {
	done bool
	err  error
}

type Session struct {
	ctx      context.Context
	results  map[string]targetResult
	tools    map[string]*ResolvedTools
	active   string
	outcomes map[string]targetOutcome
	stats    *BuildStats
}

type targetOutcome struct {
	FullName  string
	Result    TargetResult
	Placement ArtifactPlacement
}

const (
	TargetStatusBuilt    = "built"
	TargetStatusUpToDate = "up-to-date"
)

type TargetResult struct {
	Package    string      `json:"package"`
	Target     string      `json:"target"`
	Kind       string      `json:"kind"`
	Outputs    []string    `json:"outputs"`
	Status     string      `json:"status"`
	DurationMs int64       `json:"durationMs"`
	Size       *TargetSize `json:"size,omitempty"`
}

func (s *Session) TargetResults() []TargetResult {
	if s == nil {
		return []TargetResult{}
	}
	keys := make([]string, 0, len(s.outcomes))
	for key := range s.outcomes {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]TargetResult, 0, len(keys))
	for _, key := range keys {
		target := s.outcomes[key].Result
		target.Outputs = append([]string{}, target.Outputs...)
		if target.Size != nil {
			size := *target.Size
			target.Size = &size
		}
		result = append(result, target)
	}
	return result
}

func (s *Session) TargetPlacements() map[string]ArtifactPlacement {
	out := make(map[string]ArtifactPlacement)
	if s == nil {
		return out
	}
	for name, outcome := range s.outcomes {
		if outcome.Placement.Kind == api.TargetVoid {
			continue
		}
		placement := outcome.Placement
		placement.PostLinkOutputs = append([]string(nil), placement.PostLinkOutputs...)
		out[name] = placement
	}
	return out
}

func (s *Session) recordTarget(outcome targetOutcome) {
	if s == nil || outcome.FullName == "" {
		return
	}
	if _, exists := s.outcomes[outcome.FullName]; exists {
		return
	}
	if s.outcomes == nil {
		s.outcomes = make(map[string]targetOutcome)
	}
	s.outcomes[outcome.FullName] = outcome
}

func (s *Session) Stats() *BuildStats {
	if s.stats == nil {
		s.stats = NewBuildStats()
	}
	return s.stats
}

func NewSession(ctx context.Context) *Session {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Session{
		ctx:      ctx,
		results:  make(map[string]targetResult),
		outcomes: make(map[string]targetOutcome),
		stats:    NewBuildStats(),
	}
}

func (s *Session) ResolveTools(tc *toolchain.Toolchain, platform api.Platform) (*ResolvedTools, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	platform.OS = platform.OSOrHost()
	env, err := environmentSignature(tc.CommandEnv())
	if err != nil {
		return nil, err
	}
	key, err := digestValue(struct {
		Toolchain *toolchain.Toolchain
		Platform  api.Platform
		Env       string
	}{tc, platform, env})
	if err != nil {
		return nil, err
	}
	if tools := s.tools[key]; tools != nil {
		return tools, nil
	}
	tools, err := resolveTools(s.ctx, tc, platform)
	if err != nil {
		return nil, err
	}
	if s.tools == nil {
		s.tools = make(map[string]*ResolvedTools)
	}
	s.tools[key] = tools
	return tools, nil
}

func (s *Session) ActiveTarget() string {
	return s.active
}

func (s *Session) Execute(name string, run func() error) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if s.active != "" {
		return fmt.Errorf("cannot build target %s while target %s is running", name, s.active)
	}
	if result, ok := s.results[name]; ok {
		if result.done {
			return result.err
		}
		return fmt.Errorf("recursive target build: %s", name)
	}
	s.results[name] = targetResult{}
	s.active = name
	defer func() { s.active = "" }()
	var result error
	if err := scriptcall.Run(func() { result = run() }); err != nil {
		result = err
	}
	if s.ctx.Err() != nil {
		result = s.ctx.Err()
	}
	s.results[name] = targetResult{done: true, err: result}
	return result
}
