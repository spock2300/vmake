package build

import (
	"context"
	"errors"
	"runtime"
	"sync"

	vlog "github.com/spock2300/vmake/pkg/log"
)

func (s *Scheduler) compileAll(resolved *ResolvedTarget, pkgInfo *PkgInfo) ([]string, error) {
	numFiles := len(resolved.SourceFiles)

	numWorkers := s.numWorkers
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}
	if numWorkers > numFiles {
		numWorkers = numFiles
	}

	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	jobs := make(chan int, numFiles)
	results := make([]compileResult, numFiles)

	var wg sync.WaitGroup
	var firstError sync.Once
	var firstErr error

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case idx, ok := <-jobs:
					if !ok {
						return
					}
					if ctx.Err() != nil {
						return
					}
					src := resolved.SourceFiles[idx]
					objPath, deps, err := s.compileSourceContext(ctx, resolved, src)
					results[idx] = compileResult{src: src, objPath: objPath, deps: deps, err: err}
					if err != nil {
						firstError.Do(func() {
							firstErr = err
							cancel()
						})
					}
				}
			}
		}()
	}

feed:
	for i := range resolved.SourceFiles {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	objs := make([]string, 0, numFiles)
	for _, r := range results {
		if r.objPath != "" {
			objs = append(objs, r.objPath)
		}
	}
	return objs, nil
}

type depResolveResult struct {
	includes    []string
	defines     []string
	cflags      []string
	cxxflags    []string
	ldflags     []string
	links       []string
	voidLdFlags []string
	artifacts   []string
}

func (s *Scheduler) compileSource(resolved *ResolvedTarget, src string) (string, []string, error) {
	return s.compileSourceContext(context.Background(), resolved, src)
}

func (s *Scheduler) compileSourceContext(ctx context.Context, resolved *ResolvedTarget, src string) (string, []string, error) {
	pkgInfo := s.pkgs[resolved.Node.PkgName]
	workDir := pkgInfo.SourceDir
	object, err := objectPath(resolved.Node.FullName, src, workDir)
	if err != nil {
		return "", nil, err
	}
	objRel := pkgInfo.OutputPath(object)
	opts := &CompileOptions{
		Includes: resolved.AllIncludes,
		Defines:  resolved.AllDefines,
		CFlags:   resolved.AllCFlags,
		CxxFlags: resolved.AllCxxFlags,
		Language: sourceLanguage(src),
		clang:    s.compiler.clangCC,
	}
	planner := *s.compiler
	planner.targetOS = s.targetOS(resolved.Node.PkgName)
	plan, err := planner.planCompile(src, objRel, opts, workDir)
	if err != nil {
		return "", nil, err
	}
	s.ccWriter.AddSpec(workDir, src, plan.Display)

	env, err := environmentSignature(s.compiler.env)
	if err != nil {
		return "", nil, err
	}
	deps, depErr := ParseDepFile(resolveWorkPath(workDir, plan.DepPath))
	action := &plannedAction{
		Kind:         "compile",
		Subject:      src,
		Target:       resolved.Node.FullName,
		WorkDir:      workDir,
		TargetOS:     s.targetOS(resolved.Node.PkgName),
		Tools:        s.toolIdentity(),
		Environment:  env,
		Commands:     plan.Commands,
		Inputs:       append([]string{src}, deps...),
		RecordInputs: []string{src},
		Outputs:      plan.Outputs,
		RecordPath:   resolveWorkPath(workDir, objRel+".vmake.json"),
		Force:        depErr != nil,
		ForceReason:  "depfile missing or unreadable",
	}
	upToDate, postInputs, err := s.runPlannedAction(ctx, action, func(ctx context.Context) ([]string, error) {
		if s.stats != nil {
			s.stats.RecordCompile(resolved.Node.FullName, src)
		}
		vlog.Info("  CC %s", src)
		compiler := *s.compiler
		compiler.targetOS = s.targetOS(resolved.Node.PkgName)
		deps, err := compiler.ExecutePlanContext(ctx, plan, src, objRel, workDir)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, removeCompileFiles(workDir, objRel, objRel+".d"))
		}
		return deps, nil
	})
	if err != nil {
		return "", nil, err
	}
	checkDeps := postInputs
	if upToDate {
		checkDeps = deps
	}
	if err := s.auditorFor().checkHeaderBoundaries(resolved, src, checkDeps); err != nil {
		return "", nil, err
	}
	return objRel, checkDeps, nil
}
