package main

import (
	"github.com/spock2300/vmake/internal/gitusr"
)

var (
	gitUserlandDirs []string
	pluginsLoaded   bool
)

func main() {
	gitUserlandDirs = gitusr.SetupProcessEnv()
	Execute()
}

func loadPluginsForCommand(args []string) {
	if pluginsLoaded {
		return
	}
	if !commandNeedsPlugins(args) {
		return
	}
	pluginsLoaded = true
	loadPlugins()
}

func commandNeedsPlugins(args []string) bool {
	cmd, _, err := RootCmd.Find(args)
	if err == nil {
		for current := cmd; current != nil; current = current.Parent() {
			if current == extCmd || current == cacheCmd {
				return false
			}
		}
	}
	return true
}
