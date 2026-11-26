package main

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/spf13/cobra"

	exec "github.com/spock2300/vmake/internal/exec"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/version"
)

type runEnv struct {
	VmakeDir    string
	LoadPlugins bool
}

func run(argv []string, stdout, stderr io.Writer, env runEnv) int {
	resetProcessState(env)
	restoreOut := pipeStdout(stdout)
	defer restoreOut()
	restoreErr := pipeStderr(stderr)
	defer restoreErr()
	prepareRoot(argv, context.Background(), env.LoadPlugins)
	code, err := executeRoot()
	releaseExtensionRepoLocks()
	if session := activeCommandSession; session != nil {
		activeCommandSession = nil
		if closeErr := session.close(); err == nil {
			err = closeErr
		}
	}
	if code != 0 {
		return code
	}
	return exitCodeFor(err)
}

func prepareRoot(argv []string, ctx context.Context, loadPlugins bool) {
	unwrapCommandArgs()
	resetCommandTree(RootCmd)
	installCommands()
	RootCmd.SetArgs(argv)
	RootCmd.SetContext(ctx)
	if loadPlugins {
		loadPluginsForCommand(argv)
		for _, pluginCmd := range pluginCommands {
			RootCmd.AddCommand(pluginCmd)
		}
	}
	wrapCommandArgs(RootCmd)
}

func resetCommandTree(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		resetCommandTree(child)
	}
	cmd.ResetFlags()
	cmd.ResetCommands()
}

func executeRoot() (code int, err error) {
	defer func() {
		if r := recover(); r != nil {
			if exit, ok := r.(*exitError); ok {
				code, err = exit.code, nil
				return
			}
			panic(r)
		}
	}()
	return 0, RootCmd.Execute()
}

func exitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	var interrupted *buildInterrupted
	if errors.As(err, &interrupted) {
		return interrupted.code
	}
	var usage *usageError
	if errors.As(err, &usage) {
		return 2
	}
	return 1
}

func resetProcessState(env runEnv) {
	cachedProjectDir = ""
	activeCommandSession = nil
	extensionRepoLocks = nil
	repoAddNative = false
	repoTrustURLFlag = ""
	if env.VmakeDir != "" {
		vmakeDir = env.VmakeDir
	} else {
		vmakeDir = defaultVmakeDir
	}
	vlog.SetLevel(vlog.Normal)
	exec.SetQuiet(false)
}

func pipeStdout(w io.Writer) func() { return pipeStream(w, &os.Stdout) }

func pipeStderr(w io.Writer) func() { return pipeStream(w, &os.Stderr) }

func pipeStream(w io.Writer, target **os.File) func() {
	if w == nil {
		return func() {}
	}
	previous := *target
	reader, writer, err := os.Pipe()
	if err != nil {
		return func() {}
	}
	*target = writer
	drained := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(drained)
		_, _ = io.Copy(w, reader)
		_ = reader.Close()
	}()
	return func() {
		once.Do(func() {
			_ = writer.Close()
			<-drained
			*target = previous
		})
	}
}

func installCommands() {
	RootCmd.CompletionOptions.DisableDefaultCmd = true
	RootCmd.Version = version.Version
	RootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &usageError{err: err}
	})

	RootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	RootCmd.PersistentFlags().BoolVarP(&veryVerbose, "very-verbose", "V", false, "very verbose output")
	RootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "quiet mode")
	RootCmd.PersistentFlags().BoolVarP(&yesFlag, "yes", "y", false, "assume yes for interactive prompts (e.g. trusting repositories; trust persists for future updates)")
	RootCmd.PersistentFlags().StringVarP(&projectOverride, "project", "C", "", "project root directory (default: search upward from the current directory)")
	addConfigFlag(RootCmd)

	RootCmd.AddCommand(buildCmd)
	addInstallFlags(buildCmd)
	addBuildFlags(buildCmd)

	RootCmd.AddCommand(rebuildCmd)
	addInstallFlags(rebuildCmd)
	addBuildFlags(rebuildCmd)

	RootCmd.AddCommand(testCmd)
	addTestFlags(testCmd)

	RootCmd.AddCommand(cleanCmd)
	addConfigFlag(cleanCmd)
	cleanCmd.Flags().BoolVar(&cleanAllFlag, "all", false, "clean build outputs for every configuration")

	RootCmd.AddCommand(distCleanCmd)
	distCleanCmd.Flags().BoolVar(&distCleanPurgeSources, "purge-sources", false, "also remove downloaded package sources under .vmake_deps")

	RootCmd.AddCommand(doctorCmd)
	addConfigFlag(doctorCmd)
	doctorCmd.Flags().StringVar(&toolchainFlag, "toolchain", "", "toolchain to diagnose (default: active config, chip PreferredToolchain, or host)")
	doctorCmd.RegisterFlagCompletionFunc("toolchain", completeToolchain)

	installConfigCommand()
	installCacheCommand()
	installCompletionCommand()
	installExtCommand()
	installGitCommand()
	installManifestCommand()
	installPkgCommand()
	installRepoCommand()
	installSkillCommand()
	installToolchainCommand()

	RootCmd.AddCommand(updateCmd)
	RootCmd.AddCommand(versionCmd)

	RootCmd.AddCommand(newQueryCmd())
	RootCmd.AddCommand(newInspectCmd())
	RootCmd.AddCommand(newCheckSymbolsCmd())
	RootCmd.AddCommand(newInitEditorCmd())
	RootCmd.AddCommand(newDepsCmd())
}

func installConfigCommand() {
	addConfigFlag(configCmd)
	configCmd.Flags().StringArrayVarP(&setFlags, "set", "s", nil,
		"Set option value non-interactively (format: [pkg:]option=value)")
	configCmd.Flags().StringArrayVarP(&unsetFlags, "unset", "u", nil,
		"Remove explicit option override and revert to default (format: [pkg:]option)")
	configCmd.AddCommand(newConfigListCmd(), newConfigUseCmd(), newConfigCopyCmd(), newConfigDescribeCmd(), newConfigCheckCmd())
	RootCmd.AddCommand(configCmd)
}

func installCacheCommand() {
	cacheCmd.AddCommand(cacheListCmd, cacheRemoveCmd)
	RootCmd.AddCommand(cacheCmd)
}

func installCompletionCommand() {
	shells = map[string]shellHandler{
		"bash":       {func(w io.Writer) { RootCmd.GenBashCompletionV2(w, true) }, installBashCompletion},
		"zsh":        {func(w io.Writer) { RootCmd.GenZshCompletion(w) }, installZshCompletion},
		"fish":       {func(w io.Writer) { RootCmd.GenFishCompletion(w, true) }, installFishCompletion},
		"powershell": {func(w io.Writer) { RootCmd.GenPowerShellCompletionWithDesc(w) }, nil},
	}
	completionInstallCmd.Flags().StringVar(&completionShell, "shell", "", "override shell detection (bash, zsh, fish)")
	completionCmd.AddCommand(completionInstallCmd)
	RootCmd.AddCommand(completionCmd)
}

func installExtCommand() {
	extCmd.AddCommand(extAddCmd, extRemoveCmd, extListCmd, extUpdateCmd, extProvisionCmd, extTrustCmd, extUntrustCmd)
	extRemoveCmd.ValidArgsFunction = completeExtRepoName
	extUpdateCmd.ValidArgsFunction = completeExtRepoName
	extProvisionCmd.ValidArgsFunction = completeExtRepoName
	extTrustCmd.ValidArgsFunction = completeExtRepoName
	extUntrustCmd.ValidArgsFunction = completeExtRepoName
	RootCmd.AddCommand(extCmd)
}

func installGitCommand() {
	gitTagCmd.Flags().BoolVar(&gitTagMinor, "minor", false, "bump minor version")
	gitTagCmd.Flags().BoolVar(&gitTagMajor, "major", false, "bump major version")
	gitTagCmd.Flags().BoolVar(&gitTagNoPush, "no-push", false, "create tags without pushing")
	gitTagCmd.Flags().StringVarP(&gitTagMsg, "message", "m", "", "custom tag message")
	gitCmd.AddCommand(gitTagCmd)
	RootCmd.AddCommand(gitCmd)
}

func installManifestCommand() {
	manifestCmd.AddCommand(manifestShowCmd)
	manifestCmd.AddCommand(manifestCheckoutCmd)
	RootCmd.AddCommand(manifestCmd)
}

func installPkgCommand() {
	pkgCmd.AddCommand(pkgListCmd)
	pkgCmd.AddCommand(pkgSearchCmd)
	pkgCmd.AddCommand(pkgShowCmd)
	pkgCmd.AddCommand(pkgCleanCmd)
	pkgCmd.AddCommand(pkgLinkCmd)
	pkgCmd.AddCommand(pkgUnlinkCmd)
	RootCmd.AddCommand(pkgCmd)

	pkgCleanCmd.Flags().BoolVarP(&pkgCleanAll, "all", "a", false, "also clean source code")
	pkgSearchCmd.Flags().StringVar(&pkgSearchRepo, "repo", "", "limit the search to one repository")
	pkgSearchCmd.Flags().BoolVar(&pkgSearchDetails, "details", false, "read build.go; also match description, license and version")
	pkgSearchCmd.Flags().BoolVar(&pkgSearchForProject, "for-project", false, "include cached, locked and installed status")
	pkgSearchCmd.Flags().BoolVar(&pkgSearchJSON, "json", false, "print machine-readable JSON")
	pkgShowCmd.Flags().BoolVar(&pkgShowForProject, "for-project", false, "include cached, locked and installed status")
	pkgShowCmd.Flags().BoolVar(&pkgShowJSON, "json", false, "print machine-readable JSON")

	pkgCleanCmd.ValidArgsFunction = completePkgRef
	pkgUnlinkCmd.ValidArgsFunction = completePkgRef
	pkgShowCmd.ValidArgsFunction = completeCatalogPkgRef
	pkgSearchCmd.RegisterFlagCompletionFunc("repo", completeRepoName)
}

func installRepoCommand() {
	repoCmd.AddCommand(repoAddCmd)
	repoCmd.AddCommand(repoRemoveCmd)
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoUpdateCmd)
	repoCmd.AddCommand(repoTrustCmd)
	repoCmd.AddCommand(repoUntrustCmd)
	RootCmd.AddCommand(repoCmd)

	repoAddCmd.Flags().BoolVarP(&repoAddNative, "native", "n", false, "add a native repository (URL template with {name} placeholder)")
	repoTrustCmd.Flags().StringVar(&repoTrustURLFlag, "url", "", "URL or native URL template for a repository declared in vmake.json")

	repoRemoveCmd.ValidArgsFunction = completeRepoName
	repoUpdateCmd.ValidArgsFunction = completeRepoName
	repoTrustCmd.ValidArgsFunction = completeRepoName
	repoUntrustCmd.ValidArgsFunction = completeRepoName
}

func installSkillCommand() {
	skillInstallCmd.Flags().StringP("project", "p", "", "Also install to <dir>/.agents/skills/ (path used verbatim)")
	skillCmd.AddCommand(skillInstallCmd)
	skillCmd.AddCommand(skillUninstallCmd)
	skillCmd.AddCommand(skillPathCmd)
	RootCmd.AddCommand(skillCmd)
}

func installToolchainCommand() {
	toolchainCmd.AddCommand(toolchainListCmd)
	toolchainCmd.AddCommand(toolchainShowCmd)
	toolchainShowCmd.ValidArgsFunction = completeToolchain
	RootCmd.AddCommand(toolchainCmd)
}
