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
	CommandName string

	AddSubCommand     func(cmd *cobra.Command)
	RegisterToolchain func(name string, tc *toolchain.Toolchain)
	GetToolchains     func() map[string]*toolchain.Toolchain
	DownloadFile      func(url, dest string) error
	ExtractArchive    func(archive, dest string) error
	RunGitLFS         func(repoDir string, args ...string) error
}

type MainFunc func(ctx *Context)
