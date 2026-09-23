package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

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
	RootCmd.AddCommand(newQueryCmd())
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
		commandStorageExclusive = false
		for current := cmd; current != nil; current = current.Parent() {
			switch current.Name() {
			case "clean", "distclean", "rebuild", "update":
				commandStorageExclusive = true
			}
		}
		switch {
		case veryVerbose:
			vlog.SetLevel(vlog.VeryVerbose)
		case verbose:
			vlog.SetLevel(vlog.Verbose)
		case quiet:
			vlog.SetLevel(vlog.Quiet)
		}
		if len(gitUserlandDirs) > 0 {
			vlog.Debug("git userland on PATH: %v", gitUserlandDirs)
		}
	},
}

func Execute() {
	err := RootCmd.Execute()
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
		os.Exit(1)
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
		LocksDir:   getLocksDir(),
		ReposDir:   getReposDir(),
		LockPath:   getLockfilePath(),
	}
}

func resolveParams(ignoreLock bool, workDir, configPath, lockPath string, lock *lockfile.Lock, cfg *config.ConfigFile) pipeline.ResolveParams {
	return pipeline.ResolveParams{
		WorkDir:           workDir,
		ConfigPath:        configPath,
		Config:            cfg,
		Lock:              lock,
		LockPath:          lockPath,
		IgnoreLock:        ignoreLock,
		TrustChecker:      remoteScriptTrustChecker,
		Paths:             pipelinePaths(),
		ModeOverride:      modeFlag,
		ToolchainOverride: toolchainFlag,
	}
}

func mustLoadConfig(path string) *config.ConfigFile {
	cfg, err := config.Load(path)
	fatalErr(err)
	return cfg
}

func resolveToConfig(ignoreLock bool) *RuntimeContext {
	ctx, err := resolveToConfigContext(context.Background(), ignoreLock)
	fatalErr(err)
	return ctx
}

func resolveToConfigContext(execution context.Context, ignoreLock bool) (*RuntimeContext, error) {
	locks := commandStorageLocks()
	workDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(workDir, ".vmake", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if err := ensureGitignore(findProjectDir()); err != nil {
		return nil, err
	}
	cleanupLegacyStorage()
	lockPath := getLockfilePath()
	lock, err := lockfile.LoadOrCreate(lockPath)
	if err != nil {
		return nil, err
	}
	ctx := pipeline.NewContext(resolveParams(ignoreLock, workDir, configPath, lockPath, lock, cfg))
	ctx.Context = execution
	ctx.Locks = locks
	if err := pipeline.Require(ctx); err != nil {
		return nil, err
	}
	if err := pipeline.Configure(ctx); err != nil {
		return nil, err
	}
	return ctx, nil
}

func mustLoadLockfile(path string) *lockfile.Lock {
	l, err := lockfile.LoadOrCreate(path)
	fatalErr(err)
	return l
}

func resolveToConfigBestEffort(ignoreLock bool) (*RuntimeContext, bool) {
	locks := commandStorageLocks()
	workDir, err := os.Getwd()
	if err != nil {
		vlog.Error("Error: %v", err)
		os.Exit(1)
	}
	configPath := filepath.Join(workDir, ".vmake", "config.json")
	cfg := mustLoadConfig(configPath)
	if err := ensureGitignore(findProjectDir()); err != nil {
		vlog.Error("gitignore: %v", err)
	}
	cleanupLegacyStorage()
	lockPath := getLockfilePath()
	lock := mustLoadLockfile(lockPath)
	ctx := pipeline.NewContext(resolveParams(ignoreLock, workDir, configPath, lockPath, lock, cfg))
	ctx.Locks = locks
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
	buf = append(buf, []byte("vmake_deps/\n")...)
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
		if line == "vmake_deps" || line == "vmake_deps/" ||
			strings.HasSuffix(line, "/vmake_deps") || strings.HasSuffix(line, "/vmake_deps/") {
			return true
		}
	}
	return false
}
