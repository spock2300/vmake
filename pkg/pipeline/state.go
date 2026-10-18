package pipeline

import (
	"github.com/spock2300/vmake/internal/buildruntime"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/build"
)

type runContext struct {
	ctx             *RuntimeContext
	opts            BuildOptions
	cfg             *buildConfig
	session         *build.Session
	budget          *buildruntime.Budget
	globalFlags     [4][]string
	globalFlagsHash string
}

type configStage struct {
	needed        map[string]bool
	allPkgOptions map[string]map[string]any
}

type sourceStage struct {
	rc             *runContext
	conf           *configStage
	pkgDirs        map[string]*api.PkgDirs
	layouts        map[string]*build.PackageLayout
	remote         *remoteVersionState
	patchHashes    map[string]string
	sourceSeeds    map[string]string
	sourceCommits  map[string]string
	sourceVersions map[string]string
}

type declareStage struct {
	rc               *runContext
	conf             *configStage
	source           *sourceStage
	bindings         map[string]*packageBinding
	declarations     map[string]uint8
	declarationPath  []string
	scopeValues      map[string]any
	subGraphErrors   map[string]error
	subGraphRequests map[string]string
	subGraphBuilt    map[string]bool
	allTargets       map[string]map[string]*api.Target
	pkgMetaMap       map[string]build.PkgBuildMeta
	buildCtxs        map[string]*api.BuildContext
}

type planStage struct {
	graph *build.BuildGraph
	job   *build.BuildJob
}
