package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/gitstore"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/plugin"
	"github.com/spock2300/vmake/pkg/repo"
)

type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }

func fatalErr(err error) {
	if err != nil {
		vlog.Error("Error: %v", err)
		exitWithError(err)
	}
}

func fatalMsg(format string, args ...any) {
	vlog.Error(format, args...)
	exitWithError(fmt.Errorf(format, args...))
}

func fatalUsage(format string, args ...any) {
	err := &usageError{err: fmt.Errorf(format, args...)}
	vlog.Error("%v", err)
	exitWithError(err)
}

func exitWithError(err error) {
	var usage *usageError
	if errors.As(err, &usage) {
		os.Exit(2)
	}
	os.Exit(1)
}

func getRepoManager() *repo.RepoManager {
	return repo.NewRepoManager(getReposDir())
}

func getPluginManager() *plugin.Manager {
	return plugin.NewManager(vmakeDir)
}

func newPkgRef(repoName, pkgName string) *api.Package {
	return api.NewPackage().SetRepo(repoName).SetName(pkgName)
}

func mustSplitPkgRef(ref string) (string, string) {
	repoName, pkgName, ok := api.SplitPackageRef(ref)
	if !ok {
		fatalMsg("Error: invalid package reference: %s", ref)
	}
	if !validPkgRefComponent(repoName) || !validPkgRefComponent(pkgName) {
		fatalMsg("Error: invalid package reference: %s", ref)
	}
	return repoName, pkgName
}

func validPkgRefComponent(s string) bool {
	return gitstore.ValidateName(s) == nil
}

func newActionCmd(use, short, verb, entityType string, action func(name string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fatalErr(action(args[0]))
			fmt.Printf("%s %s '%s'\n", verb, entityType, args[0])
		},
	}
}
