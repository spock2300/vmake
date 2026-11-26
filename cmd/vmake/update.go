package main

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/version"
)

const (
	moduleRoot = "github.com/spock2300/vmake"
	modulePath = moduleRoot + "/cmd/vmake"
)

var updateCmd = &cobra.Command{
	Use:   "update [version]",
	Short: "Update vmake to latest or specified version",
	Long: `Update vmake using go install.

Examples:
  vmake update          Update to latest stable version
  vmake update v1.2.3   Update to specific version`,
	Run: runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) {
	fmt.Printf("Current version: %s\n", version.Version)

	goCmd, err := iexec.LookPath("go")
	fatalErr(err)

	targetVer := ""
	if len(args) > 0 {
		targetVer = args[0]
		fatalErr(validateRequestedVersion(targetVer))
	} else {
		versions, err := listAvailableVersions(goCmd)
		fatalErr(err)
		targetVer = pickLatestStable(versions)
		fmt.Printf("Available versions: %s\n", strings.Join(versions, " "))
		fmt.Printf("Selected latest: %s\n", targetVer)
	}

	fmt.Printf("Installing vmake@%s...\n", targetVer)
	fatalErr(iexec.RunToStdout("", goCmd, "install", fmt.Sprintf("%s@%s", modulePath, targetVer)))
	fmt.Printf("Update to %s completed!\n", targetVer)
}

func listAvailableVersions(goCmd string) ([]string, error) {
	output, err := iexec.RunWithOptions(goCmd, []string{"list", "-m", "-versions", moduleRoot}, iexec.RunOptions{Quiet: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list module versions: %w", err)
	}
	return parseModuleVersions(iexec.TrimOutput(output))
}

func parseModuleVersions(output string) ([]string, error) {
	fields := strings.Fields(output)
	if len(fields) < 2 || fields[0] != moduleRoot {
		return nil, fmt.Errorf("no published versions found")
	}
	return fields[1:], nil
}

func pickLatestStable(versions []string) string {
	var stable []string
	for _, v := range versions {
		pv, ok := api.ParseVersion(v)
		if ok && pv.Pre == "" && versionMajorCompatible(pv.Major) {
			stable = append(stable, v)
		}
	}
	result, ok := api.MatchVersion(stable, ">=0.0.0")
	if !ok {
		fatalMsg("no stable version available")
	}
	return result
}

func validateRequestedVersion(v string) error {
	pv, ok := api.ParseVersion(v)
	if !ok {
		return nil
	}
	if !versionMajorCompatible(pv.Major) {
		return fmt.Errorf("version %s is not installable from %s: module path requires major version %d", v, modulePath, moduleMajor())
	}
	return nil
}

func versionMajorCompatible(major int) bool {
	want := moduleMajor()
	return major == want || (want == 1 && major == 0)
}

func moduleMajor() int {
	base := path.Base(moduleRoot)
	if len(base) > 1 && base[0] == 'v' {
		if major, err := strconv.Atoi(base[1:]); err == nil && major >= 2 {
			return major
		}
	}
	return 1
}
