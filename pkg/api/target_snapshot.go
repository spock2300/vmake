package api

import "slices"

func SnapshotTarget(target *Target) *Target {
	copy := *target
	copy.files = slices.Clone(target.files)
	copy.excludeFiles = slices.Clone(target.excludeFiles)
	copy.includes = slices.Clone(target.includes)
	copy.publicIncludes = slices.Clone(target.publicIncludes)
	copy.defines = slices.Clone(target.defines)
	copy.publicDefines = slices.Clone(target.publicDefines)
	copy.links = slices.Clone(target.links)
	copy.publicLinks = slices.Clone(target.publicLinks)
	copy.providedLibs = slices.Clone(target.providedLibs)
	copy.deps = slices.Clone(target.deps)
	copy.cflags = slices.Clone(target.cflags)
	copy.publicCFlags = slices.Clone(target.publicCFlags)
	copy.cxxflags = slices.Clone(target.cxxflags)
	copy.publicCxxFlags = slices.Clone(target.publicCxxFlags)
	copy.ldflags = slices.Clone(target.ldflags)
	copy.publicLdFlags = slices.Clone(target.publicLdFlags)
	copy.excludeLibs = slices.Clone(target.excludeLibs)
	copy.postLinkDeps = slices.Clone(target.postLinkDeps)
	copy.postLinkOutputs = slices.Clone(target.postLinkOutputs)
	copy.inputs = slices.Clone(target.inputs)
	copy.outputs = slices.Clone(target.outputs)
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
