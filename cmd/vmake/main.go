package main

import (
	"os"

	"github.com/spock2300/vmake/internal/gitusr"
)

var gitUserlandDirs []string

func main() {
	gitUserlandDirs = gitusr.SetupProcessEnv()
	loadPluginsForCommand(os.Args[1:])
	Execute()
}

func loadPluginsForCommand(args []string) {
	cmd, _, err := RootCmd.Find(args)
	if err == nil {
		for current := cmd; current != nil; current = current.Parent() {
			if current == extCmd {
				return
			}
		}
	}
	loadPlugins()
}
