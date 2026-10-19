package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	exec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/storage"
	"github.com/spock2300/vmake/pkg/config"
	"github.com/spock2300/vmake/pkg/lockfile"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/pipeline"
)

var (
	verbose                 bool
	veryVerbose             bool
	quiet                   bool
	yesFlag                 bool
	vmakeDir                string
	commandStorage          *storage.Session
	commandStorageExclusive bool
)

func init() {
	homeDir, _ := os.UserHomeDir()
	vmakeDir = filepath.Join(homeDir, ".vmake")
	RootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	RootCmd.PersistentFlags().BoolVarP(&veryVerbose, "very-verbose", "V", false, "very verbose output")
	RootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "quiet mode")
	RootCmd.PersistentFlags().BoolVarP(&yesFlag, "yes", "y", false, "assume yes for interactive prompts (e.g. trusting remote repositories)")
	RootCmd.PersistentFlags().StringVarP(&projectOverride, "project", "C", "", "project root directory (default: search upward from the current directory)")
	RootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &usageError{err: err}
	})
	RootCmd.AddCommand(newQueryCmd())
	RootCmd.AddCommand(newInspectCmd())
	RootCmd.AddCommand(newCheckSymbolsCmd())
	RootCmd.AddCommand(newInitEditorCmd())
	RootCmd.AddCommand(newLockCmd())
}

type RuntimeContext = pipeline.RuntimeContext
type BuildOptions = pipeline.BuildOptions
type BuildResult = pipeline.BuildResult

func runBuildPhase(ctx *RuntimeContext, opts BuildOptions) (*BuildResult, error) {
	return pipeline.RunBuild(ctx, opts)
}

var RootCmd = &cobra.Command{
	Use:   "vmake",
	Short: "VMake - A Go-based C/C++ build system",
	Long: `VMake is a minimal build system for C/C++ projects.
It uses Go buildscripts for configuration and provides a TUI for option management.`,
	RunE:         runBuild,
	SilenceUsage: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if projectOverride != "" {
			findProjectDir()
		} else if cmd.Name() == "inspect" {
			// inspect silences stdout/stderr while resolving, so report a
			// shadowed root before it starts.
			rejectShadowedProjectRootFromCWD()
		}
		commandStorageExclusive = requiresExclusiveStorage(cmd)
		if cmd.Flags().Changed("output") {
			jsonOutput = true
		}
		switch {
		case veryVerbose:
			vlog.SetLevel(vlog.VeryVerbose)
		case verbose:
			vlog.SetLevel(vlog.VerboseLevel)
		case quiet:
			vlog.SetLevel(vlog.Quiet)
		}
		exec.SetQuiet(vlog.IsQuiet())
		if len(gitUserlandDirs) > 0 {
			vlog.Debug("git userland on PATH: %v", gitUserlandDirs)
		}
	},
}

// requiresExclusiveStorage reports whether the command may delete or rewrite
// shared storage. Ordinary builds reuse the cache lifecycle lock in shared
// mode so unrelated projects can build concurrently; the project lock already
// serializes commands within one project.
func requiresExclusiveStorage(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		switch current.Name() {
		case "clean", "distclean", "rebuild", "update":
			return true
		}
	}
	return false
}

func Execute() {
	preReportJSON = detectPreReportJSON(os.Args[1:])
	wrapCommandArgs(RootCmd)
	err := RootCmd.Execute()
	releaseExtensionRepoLocks()
	if commandStorage != nil {
		closeErr := commandStorage.Close()
		commandStorage = nil
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		var interrupted *buildInterrupted
		if errors.As(err, &interrupted) {
			os.Exit(interrupted.code)
		}
		exitWithError(err)
	}
}

func wrapCommandArgs(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		wrapCommandArgs(child)
	}
	if cmd.Args == nil {
		return
	}
	validate := cmd.Args
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return &usageError{err: err}
		}
		return nil
	}
}

func commandStorageLocks() *storage.Session {
	if commandStorage != nil {
		return commandStorage
	}
	var err error
	commandStorage, err = storage.Acquire(findProjectDir(), getCacheDir(), commandStorageExclusive)
	fatalErr(err)
	return commandStorage
}

func pipelinePaths() *pipeline.Paths {
	return &pipeline.Paths{
		ProjectDir: findProjectDir(),
		DepsDir:    getDepsDir(),
		CacheDir:   getCacheDir(),
		ReposDir:   getReposDir(),
		LockPath:   getLockfilePath(),
	}
}

type resolveInputs struct {
	ignoreLock   bool
	workDir      string
	configPath   string
	configDigest string
	lock         *lockfile.Lock
	cfg          *config.ConfigFile
	paths        *pipeline.Paths
}

func (in resolveInputs) params(mode, toolchain string) pipeline.ResolveParams {
	return pipeline.ResolveParams{
		WorkDir:           in.workDir,
		ConfigPath:        in.configPath,
		ConfigDigest:      in.configDigest,
		Config:            in.cfg,
		Lock:              in.lock,
		LockPath:          in.paths.LockPath,
		IgnoreLock:        in.ignoreLock,
		TrustChecker:      remoteScriptTrustChecker,
		Paths:             in.paths,
		ModeOverride:      mode,
		ToolchainOverride: toolchain,
	}
}

func loadResolveInputs(ignoreLock bool, softGitignore bool) (resolveInputs, error) {
	workDir := findProjectDir()
	cfg, configPath, digest, err := config.LoadProjectSnapshot(findProjectDir())
	if err != nil {
		return resolveInputs{}, err
	}
	if err := ensureGitignore(findProjectDir()); err != nil {
		if !softGitignore {
			return resolveInputs{}, err
		}
		vlog.Error("gitignore: %v", err)
	}
	cleanupLegacyStorage()
	paths := pipelinePaths()
	lock, err := lockfile.LoadOrCreate(paths.LockPath)
	if err != nil {
		return resolveInputs{}, err
	}
	return resolveInputs{
		ignoreLock:   ignoreLock,
		workDir:      workDir,
		configPath:   configPath,
		configDigest: digest,
		lock:         lock,
		cfg:          cfg,
		paths:        paths,
	}, nil
}

func resolveToConfig(ignoreLock bool) *RuntimeContext {
	ctx, err := resolveToConfigContext(context.Background(), ignoreLock)
	fatalErr(err)
	return ctx
}

func newRuntimeContext(inputs resolveInputs, locks *storage.Session) *RuntimeContext {
	ctx := pipeline.NewContext(inputs.params(modeFlag, toolchainFlag))
	ctx.Locks = locks
	return ctx
}

func resolveToConfigContext(execution context.Context, ignoreLock bool) (*RuntimeContext, error) {
	overrides, err := parseInspectOverrides(ephemeralSetFlags)
	if err != nil {
		return nil, err
	}
	locks := commandStorageLocks()
	inputs, err := loadResolveInputs(ignoreLock, false)
	if err != nil {
		return nil, err
	}
	ctx := newRuntimeContext(inputs, locks)
	ctx.Context = execution
	if len(overrides) > 0 {
		ctx.OnOptionsCollected = func(c *RuntimeContext) error {
			return applyEphemeralOverrides(c, overrides)
		}
	}
	if err := pipeline.Require(ctx); err != nil {
		if ctx.Resolver != nil {
			ctx.Resolver.Close()
		}
		return nil, err
	}
	if err := pipeline.Configure(ctx); err != nil {
		if ctx.Resolver != nil {
			ctx.Resolver.Close()
		}
		return nil, err
	}
	return ctx, nil
}

func applyEphemeralOverrides(ctx *RuntimeContext, overrides []inspectOverride) error {
	for _, override := range overrides {
		opt, err := inspectOverrideOption(ctx, override.pkgName, override.optName)
		if err != nil {
			return err
		}
		value, err := coerceValue(override.raw, opt.Type())
		if err != nil {
			return fmt.Errorf("%s: %w", override.key(), err)
		}
		override.value = value
		if err := validateInspectOverride(ctx, override); err != nil {
			return err
		}
		applySetValue(ctx, override.pkgName, override.optName, value)
	}
	return nil
}

func resolveToConfigBestEffort(ignoreLock bool) (*RuntimeContext, bool) {
	locks := commandStorageLocks()
	inputs, err := loadResolveInputs(ignoreLock, true)
	fatalErr(err)
	ctx := newRuntimeContext(inputs, locks)
	if err := pipeline.Require(ctx); err != nil {
		return ctx, false
	}
	fatalErr(pipeline.Configure(ctx))
	return ctx, true
}

func ensureGitignore(workDir string) error {
	gitignorePath := filepath.Join(workDir, ".gitignore")
	content := ""
	if data, err := os.ReadFile(gitignorePath); err == nil {
		content = string(data)
	}
	if gitignoreIgnoresVmakeDeps(content) {
		return nil
	}
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open %s: %w", gitignorePath, err)
	}
	defer f.Close()
	var buf []byte
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		buf = append(buf, '\n')
	}
	buf = append(buf, []byte(".vmake_deps/\n")...)
	if _, err := f.Write(buf); err != nil {
		return fmt.Errorf("write %s: %w", gitignorePath, err)
	}
	return nil
}

func gitignoreIgnoresVmakeDeps(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "!") {
			continue
		}
		if line == ".vmake_deps" || line == ".vmake_deps/" {
			return true
		}
	}
	return false
}
