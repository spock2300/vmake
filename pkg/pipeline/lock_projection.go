package pipeline

import (
	"fmt"
	"strings"

	"github.com/spock2300/vmake/pkg/lockfile"
)

// lockDecision is one package's desired lockfile outcome. Keep copies the
// existing entry (linked packages and unchanged retained entries), Skip omits
// the package (subpackages, floated packages), and the remaining fields write
// a fresh entry.
type lockDecision struct {
	Name          string
	Keep          bool
	Skip          bool
	Version       string
	Commit        string
	Source        string
	WrapperCommit string
}

func projectLock(decisions []lockDecision, existing *lockfile.Lock) *lockfile.Lock {
	updated := lockfile.New()
	for _, decision := range decisions {
		if decision.Skip {
			continue
		}
		if decision.Keep {
			if existing != nil {
				if entry, ok := existing.Get(decision.Name); ok {
					updated.Set(decision.Name, entry)
				}
			}
			continue
		}
		if decision.Version == "" && decision.Commit == "" {
			continue
		}
		updated.Set(decision.Name, &lockfile.LockedPkg{
			Version:       decision.Version,
			Commit:        decision.Commit,
			Source:        decision.Source,
			WrapperCommit: decision.WrapperCommit,
		})
	}
	return updated
}

func validateLockedProjection(decisions []lockDecision, existing *lockfile.Lock) error {
	updated := projectLock(decisions, existing)
	if len(updated.Packages) == 0 && (existing == nil || len(existing.Packages) == 0) {
		return nil
	}
	if existing == nil || !existing.Equal(updated) {
		changes := lockfile.Diff(existing, updated)
		if len(changes) > 0 {
			var names []string
			for _, ch := range changes {
				names = append(names, fmt.Sprintf("%s (%s)", ch.Name, ch.Kind))
			}
			return fmt.Errorf("--locked: vmake.lock is out of date (%s); run 'vmake deps sync' or 'vmake deps update'", strings.Join(names, ", "))
		}
		return fmt.Errorf("--locked: vmake.lock is out of date or missing; run 'vmake deps sync' or 'vmake deps update'")
	}
	return nil
}

func keepLockDecision(name string) lockDecision {
	return lockDecision{Name: name, Keep: true}
}

func subPackageDecision(name string) lockDecision {
	return lockDecision{Name: name, Skip: true}
}

// lockDecisions projects the current resolution/SourceStage state into the
// per-package decisions that the lockfile writer and --locked validator share.
func (s *sourceStage) lockDecisions() ([]lockDecision, error) {
	var decisions []lockDecision
	if s.rc.ctx.Lock != nil {
		for name := range s.rc.ctx.SourceLinks {
			if _, ok := s.rc.ctx.Lock.Get(name); ok {
				decisions = append(decisions, keepLockDecision(name))
			}
		}
	}
	subParents := s.rc.ctx.Resolver.SubParents()
	for _, name := range s.rc.ctx.Resolver.GetOrder() {
		node := s.rc.ctx.DepGraph.Packages[name]
		if node.Source != nil && node.Source.Link != nil {
			continue
		}
		if _, isSub := subParents[name]; isSub {
			decisions = append(decisions, subPackageDecision(name))
			continue
		}
		if node.IsLocal() {
			if version := s.sourceVersions[name]; version != "" || s.sourceCommits[name] != "" {
				decisions = append(decisions, lockDecision{Name: name, Version: version, Commit: s.sourceCommits[name], Source: "git"})
			} else if node.Pkg != nil && len(node.Pkg.GitURLs()) > 0 && s.rc.ctx.Lock != nil && !s.rc.ctx.isLockIgnoredFor(name) {
				if old, ok := s.rc.ctx.Lock.Get(name); ok && old.Source == "git" {
					decisions = append(decisions, keepLockDecision(name))
				}
			}
			continue
		}
		version := s.sourceVersions[name]
		commit := ""
		if s.remote != nil {
			if version == "" {
				version = s.remote.versions[name]
			}
			commit = s.remote.commits[name]
		}
		if version == "" && commit == "" {
			if s.rc.ctx.Lock != nil && !s.rc.ctx.isLockIgnoredFor(name) {
				if _, ok := s.rc.ctx.Lock.Get(name); ok {
					decisions = append(decisions, keepLockDecision(name))
					continue
				}
			}
			if node.Native != nil && (node.Native.Selected != "" || node.Native.Commit != "") {
				version = node.Native.Selected
				commit = node.Native.Commit
			}
		}
		if version == "" && commit == "" {
			continue
		}
		source := "registry"
		if node.Native != nil {
			source = "native"
		}
		wrapperCommit := ""
		if source == "registry" {
			var err error
			wrapperCommit, err = s.registryWrapperCommit(name)
			if err != nil {
				return nil, fmt.Errorf("pin wrapper commit for %s: %w", name, err)
			}
		}
		decisions = append(decisions, lockDecision{
			Name:          name,
			Version:       version,
			Commit:        commit,
			Source:        source,
			WrapperCommit: wrapperCommit,
		})
	}
	return decisions, nil
}
