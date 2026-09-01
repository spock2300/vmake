package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/pipeline"
)

func newLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Manage .vmake/vmake.lock (pinned dependency versions)",
		Long: `Manage .vmake/vmake.lock — the committed lockfile that pins remote
dependency versions and commits for reproducible builds.

vmake consults the lockfile during dependency resolution; new upstream tags do
not change what you build until you run 'vmake lock update'.`,
	}
	cmd.AddCommand(newLockUpdateCmd())
	cmd.AddCommand(newLockShowCmd())
	return cmd
}

func newLockUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Re-resolve dependency versions and rewrite vmake.lock",
		Long: `Re-resolves remote dependency versions ignoring the current vmake.lock,
downloads the selected versions and rewrites the lock.

Version pins from .vmake/config.json take precedence over latest matching tags.`,
		Run: func(cmd *cobra.Command, args []string) {
			ctx := resolveToConfig(true)
			fatalErr(pipeline.UpdateLock(ctx))
		},
	}
}

func newLockShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print locked dependency versions",
		Run: func(cmd *cobra.Command, args []string) {
			l, err := lockfile.Load(getLockfilePath())
			if err != nil {
				fatalMsg("load %s: %v", getLockfilePath(), err)
			}
			if len(l.Packages) == 0 {
				fmt.Println("vmake.lock is empty (no remote dependencies locked yet)")
				return
			}
			for _, name := range l.SortedNames() {
				p := l.Packages[name]
				commit := p.Commit
				if len(commit) > 12 {
					commit = commit[:12]
				}
				fmt.Printf("%s %s (%s, %s)\n", name, p.Version, p.Source, commit)
			}
		},
	}
}
