package pipeline

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/resolver"
)

// lockDecision is one container's or SetGit source's desired vmake.lock v2
// outcome. Keep copies the existing entry (linked entries and unchanged
// retained entries), Skip omits the entry, and the remaining fields write a
// fresh entry.
type lockDecision struct {
	Section resolver.LockSection
	Name    string
	Keep    bool
	Skip    bool
	Version string
	Commit  string
	Source  string // container only: "native" or "registry"
	URL     string // source only: primary git URL
}

func projectLock(decisions []lockDecision, existing *lockfile.Lock) *lockfile.Lock {
	updated := lockfile.New()
	for _, decision := range decisions {
		if decision.Skip {
			continue
		}
		if decision.Section == resolver.LockSectionContainer {
			if decision.Keep {
				if existing != nil {
					if entry, ok := existing.GetContainer(decision.Name); ok {
						copyEntry := *entry
						updated.SetContainer(decision.Name, &copyEntry)
					}
				}
				continue
			}
			if decision.Version == "" && decision.Commit == "" {
				continue
			}
			updated.SetContainer(decision.Name, &lockfile.LockedContainer{
				Version: decision.Version,
				Commit:  decision.Commit,
				Source:  decision.Source,
			})
			continue
		}
		if decision.Keep {
			if existing != nil {
				if entry, ok := existing.GetSource(decision.Name); ok {
					copyEntry := *entry
					updated.SetSource(decision.Name, &copyEntry)
				}
			}
			continue
		}
		if decision.Version == "" && decision.Commit == "" {
			continue
		}
		updated.SetSource(decision.Name, &lockfile.LockedSource{
			Version: decision.Version,
			Commit:  decision.Commit,
			URL:     decision.URL,
		})
	}
	return updated
}

func validateLockedProjection(decisions []lockDecision, existing *lockfile.Lock) error {
	updated := projectLock(decisions, existing)
	if updated.IsEmpty() && (existing == nil || existing.IsEmpty()) {
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

func keepContainerLockDecision(name string) lockDecision {
	return lockDecision{Section: resolver.LockSectionContainer, Name: name, Keep: true}
}

func keepSourceLockDecision(name string) lockDecision {
	return lockDecision{Section: resolver.LockSectionSource, Name: name, Keep: true}
}

// lockDecisions projects the current resolution/sourceStage state into the
// container and source decisions that the lockfile writer and --locked
// validator share.
func (s *sourceStage) lockDecisions() ([]lockDecision, error) {
	var decisions []lockDecision
	decidedContainers := make(map[string]bool)
	decidedSources := make(map[string]bool)
	processedContainers := make(map[string]bool)
	processedSources := make(map[string]bool)

	if s.rc.ctx.Lock != nil {
		linkNames := make([]string, 0, len(s.rc.ctx.SourceLinks))
		for name := range s.rc.ctx.SourceLinks {
			linkNames = append(linkNames, name)
		}
		sort.Strings(linkNames)
		for _, name := range linkNames {
			if _, ok := s.rc.ctx.Lock.GetContainer(name); ok {
				decisions = append(decisions, keepContainerLockDecision(name))
				decidedContainers[name] = true
			}
			if _, ok := s.rc.ctx.Lock.GetSource(name); ok {
				decisions = append(decisions, keepSourceLockDecision(name))
				decidedSources[name] = true
			}
		}
	}

	var neededContainers map[string]bool
	filterNeeded := s.conf != nil && len(s.conf.needed) > 0
	if filterNeeded {
		neededContainers = NeededContainers(s.rc.ctx.DepGraph, s.conf.needed)
	}

	containerIDs := make([]string, 0, len(s.rc.ctx.DepGraph.Containers))
	for id := range s.rc.ctx.DepGraph.Containers {
		if filterNeeded && !neededContainers[id] {
			continue
		}
		containerIDs = append(containerIDs, id)
	}
	sort.Strings(containerIDs)
	for _, id := range containerIDs {
		c := s.rc.ctx.DepGraph.Containers[id]
		if c == nil || c.IsLocal() || c.IsLinked() || c.IsLocalPath() || decidedContainers[id] {
			continue
		}
		processedContainers[id] = true
		if c.IsRegistry() {
			commit := s.containerCommits[id]
			if commit == "" {
				commit = c.Commit
			}
			if commit == "" {
				var err error
				commit, err = s.registryCommit(c.Repo)
				if err != nil {
					return nil, fmt.Errorf("pin registry commit for %s: %w", id, err)
				}
			}
			if commit == "" {
				if s.rc.ctx.Lock != nil && !s.rc.ctx.isLockIgnoredFor(id) {
					if _, ok := s.rc.ctx.Lock.GetContainer(id); ok {
						decisions = append(decisions, keepContainerLockDecision(id))
						decidedContainers[id] = true
					}
				}
				continue
			}
			decisions = append(decisions, lockDecision{
				Section: resolver.LockSectionContainer,
				Name:    id,
				Commit:  commit,
				Source:  resolver.SourceKindRegistry.String(),
			})
			decidedContainers[id] = true
			continue
		}
		if c.IsNative() {
			version := s.containerVersions[id]
			commit := s.containerCommits[id]
			if version == "" && commit == "" {
				if s.rc.ctx.Lock != nil && !s.rc.ctx.isLockIgnoredFor(id) {
					if _, ok := s.rc.ctx.Lock.GetContainer(id); ok {
						decisions = append(decisions, keepContainerLockDecision(id))
						decidedContainers[id] = true
						continue
					}
				}
				version = c.Version
				commit = c.Commit
			}
			if version == "" && commit == "" {
				continue
			}
			decisions = append(decisions, lockDecision{
				Section: resolver.LockSectionContainer,
				Name:    id,
				Version: version,
				Commit:  commit,
				Source:  resolver.SourceKindNative.String(),
			})
			decidedContainers[id] = true
		}
	}

	for _, name := range s.rc.ctx.DepGraph.Order {
		if filterNeeded && !s.conf.needed[name] {
			continue
		}
		node := s.rc.ctx.DepGraph.Packages[name]
		if node == nil || !node.HasOwnGit() || decidedSources[name] {
			continue
		}
		if node.Source != nil && node.Source.Link != nil && node.Source.Link.LocalSource {
			continue
		}
		processedSources[name] = true
		version := s.sourceVersions[name]
		commit := s.sourceCommits[name]
		if version == "" && commit == "" {
			if s.rc.ctx.Lock != nil && !s.rc.ctx.isLockIgnoredFor(name) {
				if _, ok := s.rc.ctx.Lock.GetSource(name); ok {
					decisions = append(decisions, keepSourceLockDecision(name))
					decidedSources[name] = true
				}
			}
			continue
		}
		url := ""
		if urls := node.Pkg.GitURLs(); len(urls) > 0 {
			url = urls[0]
		}
		decisions = append(decisions, lockDecision{
			Section: resolver.LockSectionSource,
			Name:    name,
			Version: version,
			Commit:  commit,
			URL:     url,
		})
		decidedSources[name] = true
	}

	if !s.pruneLock && s.rc.ctx.Lock != nil {
		for _, id := range s.rc.ctx.Lock.SortedContainerNames() {
			if decidedContainers[id] || processedContainers[id] {
				continue
			}
			if c := s.rc.ctx.DepGraph.Containers[id]; c != nil && (c.IsLocal() || c.IsLocalPath()) {
				continue
			}
			decisions = append(decisions, keepContainerLockDecision(id))
			decidedContainers[id] = true
		}
		for _, name := range s.rc.ctx.Lock.SortedSourceNames() {
			if decidedSources[name] || processedSources[name] {
				continue
			}
			if node := s.rc.ctx.DepGraph.Packages[name]; node != nil && !node.HasOwnGit() {
				continue
			}
			decisions = append(decisions, keepSourceLockDecision(name))
			decidedSources[name] = true
		}
	}

	return decisions, nil
}
