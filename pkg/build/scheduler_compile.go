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
	s.ccWriter.AddCommand(workDir, src, objRel, opts)
	compilerPath, flags := selectCompilerAndFlags(s.compiler.ccPath, s.compiler.cxxPath, opts.CFlags, opts.CxxFlags, opts)
	env, err := environmentSignature(s.compiler.env)
	if err != nil {
		return "", nil, err
	}
	signature, err := digestValue(struct {
		Version     int
		Target      string
		WorkDir     string
		TargetOS    string
		Tools       string
		Environment string
		Command     commandSpec
	}{actionRecordVersion, resolved.Node.FullName, workDir, s.targetOS(resolved.Node.PkgName), s.toolIdentity(), env, commandSpec{Program: compilerPath, Args: compileArgs(opts, objRel, src, flags, objRel+".d", workDir)}})
	if err != nil {
		return "", nil, err
	}
	recordPath := resolveWorkPath(workDir, objRel+".vmake.json")
	deps, depErr := ParseDepFile(resolveWorkPath(workDir, objRel+".d"))
	inputs := append([]string{src}, deps...)
	outputs := []string{objRel, objRel + ".d"}
	decision := checkAction(s.hashes, recordPath, signature, workDir, inputs, outputs)
	if depErr != nil && decision.UpToDate {
		decision = actionDecision{Reason: "depfile missing or unreadable"}
	}
	if decision.UpToDate {
		if s.stats != nil {
			s.stats.RecordUpToDate()
		}
		return objRel, deps, nil
	}
	if s.check {
		if s.stats != nil {
			s.stats.RecordPlannedCompile(resolved.Node.FullName, src)
		}
		return objRel, deps, nil
	}
	if s.explain {
		vlog.Info("  EXPLAIN %s: %s", src, decision.Reason)
	}
	if err := invalidateActionRecord(recordPath); err != nil {
		return "", nil, err
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if s.stats != nil {
		s.stats.RecordCompile(resolved.Node.FullName, src)
	}
	vlog.Info("  CC %s", src)
	compiler := *s.compiler
	compiler.targetOS = s.targetOS(resolved.Node.PkgName)
	deps, err = compiler.CompileContext(ctx, src, objRel, opts, workDir)
	if err != nil {
		return "", nil, err
	}
	if err := ctx.Err(); err != nil {
		return "", nil, errors.Join(err, removeCompileFiles(workDir, objRel, objRel+".d"))
	}
	if err := saveActionRecord(s.hashes, recordPath, signature, workDir, append([]string{src}, deps...), outputs); err != nil {
		return "", nil, err
	}
	return objRel, deps, nil
}
