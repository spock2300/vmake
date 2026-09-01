package build

import (
	"sync"

	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

// buildAllParallel schedules whole packages (Kahn levels over the package
// dependency DAG) across a worker pool. Targets within one package build
// sequentially — they share mutable per-package state (api.Package dep maps,
// void stamps) — while independent packages build concurrently.
func (s *Scheduler) buildAllParallel() []error {
	var order []string
	if err := s.graph.ForEachDefault(s.includeTests, func(node *BuildNode) error {
		order = append(order, node.FullName)
		return nil
	}); err != nil {
		return []error{err}
	}

	pkgTargets := make(map[string][]string)
	var pkgOrder []string
	for _, fn := range order {
		node, err := s.graph.GetNode(fn)
		if err != nil {
			return []error{err}
		}
		if _, seen := pkgTargets[node.PkgName]; !seen {
			pkgOrder = append(pkgOrder, node.PkgName)
		}
		pkgTargets[node.PkgName] = append(pkgTargets[node.PkgName], fn)
		if node.Target.Kind() == api.TargetVoid && s.packages[node.PkgName] == nil {
			s.ensurePackageForVoid(node)
		}
	}

	deps := make(map[string]map[string]bool, len(pkgOrder))
	for _, p := range pkgOrder {
		set := make(map[string]bool)
		for _, fn := range pkgTargets[p] {
			node, _ := s.graph.GetNode(fn)
			for _, dep := range node.Deps {
				if depNode, err := s.graph.GetNode(dep); err == nil && depNode.PkgName != p {
					if _, isBuilt := pkgTargets[depNode.PkgName]; isBuilt {
						set[depNode.PkgName] = true
					}
				}
			}
		}
		deps[p] = set
	}

	indeg := make(map[string]int, len(pkgOrder))
	dependents := make(map[string][]string)
	for _, p := range pkgOrder {
		indeg[p] = len(deps[p])
		for d := range deps[p] {
			dependents[d] = append(dependents[d], p)
		}
	}

	var (
		mu     sync.Mutex
		cond   = sync.NewCond(&mu)
		ready  []string
		done   int
		failed = make(map[string]bool)
		errs   []error
		stop   bool
	)

	anyDepFailed := func(p string) bool {
		for d := range deps[p] {
			if failed[d] {
				return true
			}
		}
		return false
	}

	var cascadeSkip func(p string)
	cascadeSkip = func(p string) {
		done++
		failed[p] = true
		vlog.Info("[skip] package %s (dependency failed)", p)
		for _, d := range dependents[p] {
			indeg[d]--
			if indeg[d] == 0 {
				cascadeSkip(d)
			}
		}
	}

	complete := func(p string, err error) {
		if err != nil {
			errs = append(errs, err)
			failed[p] = true
			if !s.keepGoing {
				stop = true
			}
		}
		for _, d := range dependents[p] {
			indeg[d]--
			if indeg[d] == 0 {
				if failed[p] || anyDepFailed(d) {
					cascadeSkip(d)
				} else {
					ready = append(ready, d)
				}
			}
		}
		done++
		cond.Broadcast()
	}

	for _, p := range pkgOrder {
		if indeg[p] == 0 {
			ready = append(ready, p)
		}
	}

	worker := func() {
		for {
			mu.Lock()
			for len(ready) == 0 && done < len(pkgOrder) && !stop {
				cond.Wait()
			}
			if stop || (len(ready) == 0 && done >= len(pkgOrder)) {
				cond.Broadcast()
				mu.Unlock()
				return
			}
			p := ready[0]
			ready = ready[1:]
			mu.Unlock()

			build := s.buildTargetsFn
			if build == nil {
				build = s.buildPkgTargets
			}
			err := build(pkgTargets[p])

			mu.Lock()
			complete(p, err)
			mu.Unlock()
		}
	}

	n := s.parallelPkgs
	if n > len(pkgOrder) {
		n = len(pkgOrder)
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker()
		}()
	}
	wg.Wait()
	return errs
}

func (s *Scheduler) buildPkgTargets(fullNames []string) error {
	failedTargets := make(map[string]bool)
	var firstErr error
	for _, fn := range fullNames {
		node, err := s.graph.GetNode(fn)
		if err != nil {
			return err
		}
		if s.depsFailed(node, failedTargets) {
			vlog.Info("[skip] %s (dependency failed)", fn)
			failedTargets[fn] = true
			continue
		}
		if err := s.Build(fn); err != nil {
			failedTargets[fn] = true
			if firstErr == nil {
				firstErr = err
			}
			if !s.keepGoing {
				return firstErr
			}
		}
	}
	return firstErr
}
