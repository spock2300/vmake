package api

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/spock2300/vmake/internal/buildruntime"
)

func BindBuildRuntime(p *Package, budget *buildruntime.Budget, ctx context.Context) error {
	if p == nil || budget == nil || budget.Jobs < 1 {
		return fmt.Errorf("a package and positive build jobs budget are required")
	}
	snapshot := *budget
	p.buildRuntime = &snapshot
	p.buildContext = ctx
	return nil
}

func (p *Package) executionContext() context.Context {
	if p.buildContext != nil {
		return p.buildContext
	}
	return context.Background()
}

func (p *Package) executionBudget() buildruntime.Budget {
	if p.buildRuntime != nil {
		return *p.buildRuntime
	}
	return buildruntime.Budget{Jobs: runtime.NumCPU()}
}

func parallelEnvironment() map[string]string {
	env := make(map[string]string)
	for _, key := range []string{"MAKEFLAGS", "MFLAGS", "GNUMAKEFLAGS", "CMAKE_BUILD_PARALLEL_LEVEL", "CMAKE_INSTALL_PARALLEL_LEVEL"} {
		if value, exists := os.LookupEnv(key); exists {
			env[key] = value
		}
	}
	return env
}
