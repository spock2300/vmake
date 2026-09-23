package build

import (
	"context"
	"fmt"

	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

type targetResult struct {
	done bool
	err  error
}

type Session struct {
	ctx     context.Context
	results map[string]targetResult
	tools   map[string]*ResolvedTools
	active  string
}

func NewSession(ctx context.Context) *Session {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Session{ctx: ctx, results: make(map[string]targetResult)}
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
