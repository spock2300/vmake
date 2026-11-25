package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/internal/gitstore"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
)

type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }

// exitError is the panic value a command uses to terminate the invocation. It
// unwinds through every deferred cleanup before run turns it back into an exit
// code, so a fatal path no longer skips the restores, closes and releases the
// command had queued.
type exitError struct {
	code  int
	cause error
}

func (e *exitError) Error() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return fmt.Sprintf("exit status %d", e.code)
}

// VmakeExitCode marks the signal for internal/scriptcall, which cannot import
// this package. The method name must not match a standard error's.
func (e *exitError) VmakeExitCode() int { return e.code }

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

func fatalResolveRoot(err error) {
	if errors.Is(err, errNotInProject) {
		cwd, _ := os.Getwd()
		fatalUsage("not inside a vmake project (no .vmake/, build.go or */build.go found in %s or any parent directory)", cwd)
	}
	var usage *usageError
	if errors.As(err, &usage) {
		fatalUsage("%v", usage.err)
	}
	fatalErr(err)
}

func fatalUsage(format string, args ...any) {
	err := &usageError{err: fmt.Errorf(format, args...)}
	vlog.Error("%v", err)
	exitWithError(err)
}

func exitWithError(err error) {
	panic(&exitError{code: exitCodeFor(err), cause: err})
}

func exitWithCode(code int) {
	panic(&exitError{code: code})
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
