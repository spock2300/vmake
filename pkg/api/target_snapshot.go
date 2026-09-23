package api

import "slices"

func SnapshotTarget(target *Target) *Target {
	copy := *target
	copy.files = slices.Clone(target.files)
	copy.excludeFiles = slices.Clone(target.excludeFiles)
	copy.includes = slices.Clone(target.includes)
	copy.publicIncludes = slices.Clone(target.publicIncludes)
	copy.defines = slices.Clone(target.defines)
	copy.languages = slices.Clone(target.languages)
	copy.links = slices.Clone(target.links)
	copy.providedLibs = slices.Clone(target.providedLibs)
	copy.deps = slices.Clone(target.deps)
	copy.cflags = slices.Clone(target.cflags)
	copy.cxxflags = slices.Clone(target.cxxflags)
	copy.ldflags = slices.Clone(target.ldflags)
	copy.excludeLibs = slices.Clone(target.excludeLibs)
	copy.postLinkDeps = slices.Clone(target.postLinkDeps)
	copy.postLinkOutputs = slices.Clone(target.postLinkOutputs)
	copy.genRules = slices.Clone(target.genRules)
	copy.postLinks = slices.Clone(target.postLinks)
	for i := range copy.postLinks {
		copy.postLinks[i].Args = slices.Clone(copy.postLinks[i].Args)
	}
	if target.includeRules != nil {
		copy.includeRules = make(map[string][]string, len(target.includeRules))
		for name, rules := range target.includeRules {
			copy.includeRules[name] = slices.Clone(rules)
		}
	}
	return &copy
}
