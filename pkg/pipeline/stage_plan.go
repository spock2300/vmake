package pipeline

import "github.com/spock2300/vmake/pkg/build"

func buildPkgKeyExtra(rc *runContext, cfg *configStage, src *sourceStage) (map[string]string, error) {
	extra := make(map[string]string)
	for name := range cfg.needed {
		node := rc.ctx.DepGraph.Packages[name]
		flagsHash := packageFlagsHash(rc.globalFlagsHash, node)
		if node == nil || node.IsLocal() {
			scriptHash, err := src.scriptHashFor(name)
			if err != nil {
				return nil, err
			}
			extra[name] = localKeyExtra(flagsHash, scriptHash, src.sourceCommits[name])
			continue
		}
		version, commit := src.remotePkgKeyMaterial(name)
		if version == "" && node.Native != nil {
			version = node.Native.Selected
		}
		if commit == "" && node.Native != nil {
			commit = node.Native.Commit
		}
		scriptHash, err := src.scriptHashFor(name)
		if err != nil {
			return nil, err
		}
		extra[name] = build.JoinKeyExtra(version, src.packageCommitKey(name, commit), flagsHash, src.patchHashes[name], scriptHash)
	}
	return extra, nil
}
