package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	iexec "github.com/spock2300/vmake/internal/exec"
	"github.com/spock2300/vmake/internal/fs"
	"github.com/spock2300/vmake/internal/gitcmd"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/repo"
)

// ValidateSourceLink canonicalizes and validates one local source link.
func ValidateSourceLink(ctx context.Context, projectDir, depsDir, reposDir, name, path string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	repoName, pkgName, ok := api.SplitPackageRef(name)
	if !ok || repoName == "" || pkgName == "" {
		return "", fmt.Errorf("invalid package reference %q", name)
	}
	if strings.Contains(pkgName, "/") {
		return "", fmt.Errorf("source links apply to parent packages only: %s", name)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(projectDir, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("linked source %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("linked source %s is not a directory", abs)
	}
	for _, root := range []string{depsDir, projectDir, reposDir} {
		if root == "" {
			continue
		}
		if pathWithin(root, abs) {
			return "", fmt.Errorf("linked source %s must be outside %s", abs, root)
		}
	}
	mgr := repo.NewRepoManager(reposDir)
	if !mgr.Exists(repoName) {
		return "", fmt.Errorf("repository %q not found; run 'vmake repo list'", repoName)
	}
	if _, err := repo.GetCurrentCommitContext(ctx, abs); err != nil {
		return "", fmt.Errorf("linked source %s must be a git work tree: %w", abs, err)
	}
	workTree, err := iexec.RunWithOptions("git", gitcmd.Args("rev-parse", "--is-inside-work-tree"),
		iexec.RunOptions{Dir: abs, Context: ctx, Quiet: true, CaptureStderr: true})
	if err != nil || strings.TrimSpace(string(workTree)) != "true" {
		return "", fmt.Errorf("linked source %s must be a git work tree", abs)
	}
	if mgr.IsNative(repoName) {
		if !fs.FileExists(filepath.Join(abs, "build.go")) {
			return "", fmt.Errorf("linked native package %s has no build.go in %s", name, abs)
		}
	} else if _, err := mgr.FindPackageGo(repoName, pkgName); err != nil {
		return "", fmt.Errorf("registry package %s: %w", name, err)
	}
	return abs, nil
}

// ValidateSourceLinks validates every machine-local link before resolution.
func ValidateSourceLinks(ctx *RuntimeContext) (map[string]string, error) {
	if len(ctx.SourceLinks) == 0 {
		return nil, nil
	}
	links := make(map[string]string, len(ctx.SourceLinks))
	for name, path := range ctx.SourceLinks {
		abs, err := ValidateSourceLink(ctx.Context, ctx.Paths.ProjectDir, ctx.Paths.DepsDir, ctx.Paths.ReposDir, name, path)
		if err != nil {
			return nil, err
		}
		links[name] = abs
	}
	return links, nil
}
