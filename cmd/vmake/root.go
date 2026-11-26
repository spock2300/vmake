package main

import (
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
	verbose     bool
	veryVerbose bool
	quiet       bool
	yesFlag     bool
	vmakeDir    string
)

var defaultVmakeDir string

func init() {
	homeDir, _ := os.UserHomeDir()
	defaultVmakeDir = filepath.Join(homeDir, ".vmake")
	vmakeDir = defaultVmakeDir
	installCommands()
}

type RuntimeContext = pipeline.RuntimeContext
type BuildOptions = pipeline.BuildOptions
type BuildResult = pipeline.BuildResult

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
		} else if isSilentResolveCmd(cmd) {
			// inspect and deps graph/why/check silence stdout/stderr while resolving,
			// so report a shadowed root before they start.
			rejectShadowedProjectRootFromCWD()
		}
		activeCommandSession = newCommandSession()
		activeCommandSession.exclusive = requiresExclusiveStorage(cmd)
		switch {
		case veryVerbose:
			vlog.SetLevel(vlog.VeryVerbose)
		case verbose:
			vlog.SetLevel(vlog.VerboseLevel)
		case quiet:
			vlog.SetLevel(vlog.Quiet)
		default:
			vlog.SetLevel(vlog.Normal)
		}
		exec.SetQuiet(vlog.IsQuiet())
		if len(gitUserlandDirs) > 0 {
			vlog.Debug("git userland on PATH: %v", gitUserlandDirs)
		}
	},
}

func isSilentResolveCmd(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if cmd.Name() == "inspect" {
		return true
	}
	if p := cmd.Parent(); p != nil && p.Name() == "deps" {
		switch cmd.Name() {
		case "graph", "why", "check":
			return true
		}
	}
	return false
}

// requiresExclusiveStorage reports whether the command may delete or rewrite
// shared storage. Ordinary builds reuse the cache lifecycle lock in shared
// mode so unrelated projects can build concurrently; the project lock already
// serializes commands within one project.
func requiresExclusiveStorage(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		switch current.Name() {
		case "clean", "distclean", "rebuild", "update", "sync", "prune", "pin", "unpin", "fetch":
			return true
		}
	}
	return false
}

func Execute() {
	os.Exit(run(os.Args[1:], nil, nil, runEnv{VmakeDir: vmakeDir, LoadPlugins: true}))
}

var wrappedArgs = make(map[*cobra.Command]cobra.PositionalArgs)

func wrapCommandArgs(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		wrapCommandArgs(child)
	}
	if cmd.Args == nil {
		return
	}
	if _, ok := wrappedArgs[cmd]; ok {
		return
	}
	validate := cmd.Args
	wrappedArgs[cmd] = validate
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return &usageError{err: err}
		}
		return nil
	}
}

func unwrapCommandArgs() {
	for cmd, original := range wrappedArgs {
		cmd.Args = original
	}
	clear(wrappedArgs)
}

func pipelinePathsAt(workDir string) *pipeline.Paths {
	return &pipeline.Paths{
		ProjectDir: workDir,
		DepsDir:    getDepsDirAt(workDir),
		CacheDir:   getCacheDir(),
		ReposDir:   getReposDir(),
		LockPath:   getLockfilePathAt(workDir),
	}
}

func loadResolveParamsAt(workDir string, ignoreLock bool, softGitignore bool, skipGitignore bool, configOverride string) (pipeline.ResolveParams, error) {
	cfg, configPath, digest, err := config.LoadProjectSnapshotWithOverride(workDir, configOverride)
	if err != nil {
		return pipeline.ResolveParams{}, err
	}
	pins, err := config.LoadProjectPins(workDir)
	if err != nil {
		return pipeline.ResolveParams{}, err
	}
	if !skipGitignore {
		if err := ensureGitignore(workDir); err != nil {
			if !softGitignore {
				return pipeline.ResolveParams{}, err
			}
			vlog.Error("gitignore: %v", err)
		}
	}
	cleanupLegacyStorage()
	paths := pipelinePathsAt(workDir)
	lock, err := lockfile.LoadOrCreate(paths.LockPath)
	if err != nil {
		return pipeline.ResolveParams{}, err
	}
	local, err := config.LoadLocal(workDir)
	if err != nil {
		return pipeline.ResolveParams{}, fmt.Errorf("load %s: %w", config.LocalPath(workDir), err)
	}
	return pipeline.ResolveParams{
		WorkDir:           workDir,
		ConfigPath:        configPath,
		ConfigDigest:      digest,
		Config:            cfg,
		Lock:              lock,
		LockPath:          paths.LockPath,
		IgnoreLock:        ignoreLock,
		Pins:              pins,
		SourceLinks:       local.Links,
		TrustChecker:      remoteScriptTrustChecker,
		Paths:             paths,
		ModeOverride:      modeFlag,
		ToolchainOverride: toolchainFlag,
	}, nil
}

func newRuntimeContext(params pipeline.ResolveParams, locks *storage.Session) *RuntimeContext {
	ctx := pipeline.NewContext(params)
	ctx.Locks = locks
	return ctx
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
