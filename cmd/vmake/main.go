package main

import (
	"github.com/spock2300/vmake/internal/gitusr"
)

var gitUserlandDirs []string

func main() {
	gitUserlandDirs = gitusr.SetupProcessEnv()
	loadPlugins()
	Execute()
}
