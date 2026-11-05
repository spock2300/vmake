package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/spock2300/vmake/pkg/config"
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

Version pins from the active project configuration take precedence over latest matching tags.
All project configurations share .vmake/vmake.lock. Packages with an active
'vmake pkg link' keep their existing lock entry; unlink them before updating
their version.`,
		Run: func(cmd *cobra.Command, args []string) {
			ctx := resolveToConfig(true)
			fatalErr(pipeline.UpdateLock(ctx))
			if local, err := config.LoadLocal(findProjectDir()); err == nil && len(local.Links) > 0 {
				fmt.Println("Linked packages keep their existing lock entries; run 'vmake pkg unlink <pkg>' before updating their version.")
			}
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
			var links map[string]string
			if local, err := config.LoadLocal(findProjectDir()); err == nil {
				links = local.Links
			}
			for _, name := range l.SortedNames() {
				p := l.Packages[name]
				commit := p.Commit
				if len(commit) > 12 {
					commit = commit[:12]
				}
				suffix := ""
				if _, linked := links[name]; linked {
					suffix = " [linked; ignored]"
				}
				fmt.Printf("%s %s (%s, %s)%s\n", name, p.Version, p.Source, commit, suffix)
			}
		},
	}
}
