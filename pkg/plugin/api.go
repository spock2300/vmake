package plugin

import (
	"gitee.com/spock2300/vmake/pkg/toolchain"
	"github.com/spf13/cobra"
)

type Info struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Entry       string `json:"entry"`
	Enabled     bool   `json:"enabled"`
}

type Context struct {
	VMakeDir    string
	PluginDir   string
	RepoDir     string
	CommandName string

	AddSubCommand              func(cmd *cobra.Command)
	RegisterToolchain          func(name string, tc *toolchain.Toolchain)
	GetToolchains              func() map[string]*toolchain.Toolchain
	SetOnMissing               func(toolchainName string, onMissing func(name string) (*toolchain.Toolchain, error))
	AddGlobalFlags             func(cflags, cxxflags []string)
	AddGlobalLdFlags           func(flags ...string)
	RegisterToolchainsFromRepo func()
	LoadToolchainDef           func() (*toolchain.ToolchainDef, error)
	DownloadFile               func(url, dest string) error
	ExtractToDir               func(archive, dest, format string) error
	RunGitLFS                  func(repoDir string, args ...string) error
}

type MainFunc func(ctx *Context)
