package pipeline

func buildPkgKeyExtra(rc *runContext, cfg *configStage, src *sourceStage) (map[string]string, error) {
	extra := make(map[string]string)
	for name := range cfg.needed {
		node := rc.ctx.DepGraph.Packages[name]
		material, err := src.keyMaterialFor(name)
		if err != nil {
			return nil, err
		}
		if node == nil || node.IsLocal() {
			extra[name] = material.localExtra()
		} else {
			extra[name] = material.remoteExtra()
		}
	}
	return extra, nil
}
