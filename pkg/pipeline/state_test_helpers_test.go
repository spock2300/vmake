package pipeline

type buildPhaseState struct {
	*runContext
	*configStage
	*sourceStage
	*declareStage
}

func newBuildPhaseState(ctx *RuntimeContext, opts BuildOptions) *buildPhaseState {
	rc := newRunContext(ctx, opts, nil)
	conf := &configStage{
		needed:        make(map[string]bool),
		allPkgOptions: make(map[string]map[string]any),
	}
	src := newSourceStage(rc, conf)
	decl := &declareStage{
		rc:             rc,
		conf:           conf,
		source:         src,
		bindings:       make(map[string]*packageBinding),
		declarations:   make(map[string]uint8),
		subGraphErrors: make(map[string]error),
		subGraphBuilt:  make(map[string]bool),
	}
	return &buildPhaseState{
		runContext:   rc,
		configStage:  conf,
		sourceStage:  src,
		declareStage: decl,
	}
}

func (s *buildPhaseState) computeDirsAndOptions() {
	s.allPkgOptions = collectAllPkgOptions(s.ctx, s.needed)
	s.pkgDirs = ResolveAllPackageDirs(s.ctx.DepGraph)
}

func (s *buildPhaseState) buildPkgKeyExtra() (map[string]string, error) {
	return buildPkgKeyExtra(s.runContext, s.configStage, s.sourceStage)
}

func (s *buildPhaseState) buildAndRunPipeline() (*BuildResult, error) {
	plan, err := planBuild(s.runContext, s.configStage, s.sourceStage, s.declareStage)
	if err != nil {
		return nil, err
	}
	return executePlan(s.runContext, s.configStage, s.sourceStage, s.declareStage, plan)
}
