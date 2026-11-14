package build

import (
	"sync"

	"github.com/spock2300/vmake/pkg/api"
)

// auditEnv is everything the header/boundary audit needs. It is deliberately
// smaller than a Scheduler so CheckExistingBuildBoundaries does not have to
// construct a partially initialized scheduler.
type auditEnv struct {
	graph         *BuildGraph
	pkgs          map[string]*PkgInfo
	packages      map[string]*api.Package
	workspaceDirs map[string]string
	rootDir       string
	canonCache    *pathCanonCache
}

type headerAuditor struct {
	auditEnv
	headerOwnersOnce sync.Once
	headerOwners     *headerOwnerIndex
}

func newHeaderAuditor(env auditEnv) *headerAuditor {
	if env.canonCache == nil {
		env.canonCache = newPathCanonCache()
	}
	return &headerAuditor{auditEnv: env}
}
