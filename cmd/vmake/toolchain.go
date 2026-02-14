package main

import (
	"strings"

	vlog "gitee.com/spock2300/vmake/pkg/log"
	"gitee.com/spock2300/vmake/pkg/toolchain"

	"github.com/spf13/cobra"
)

var toolchainCmd = &cobra.Command{
	Use:   "toolchain",
	Short: "Manage toolchains",
	Long:  `Initialize, list, or show toolchain configurations.`,
}

var toolchainInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize toolchain config",
	Long:  `Create a default toolchain configuration file if it doesn't exist.`,
	Run:   runToolchainInit,
}

var toolchainListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available toolchains",
	Long:  `Show all configured toolchains with their basic information.`,
	Run:   runToolchainList,
}

var toolchainShowCmd = &cobra.Command{
	Use:   "show [name]",
	Short: "Show toolchain details",
	Long: `Display detailed information about a specific toolchain.
If no name is provided, shows the default toolchain.`,
	Run: runToolchainShow,
}

func init() {
	RootCmd.AddCommand(toolchainCmd)
	toolchainCmd.AddCommand(toolchainInitCmd)
	toolchainCmd.AddCommand(toolchainListCmd)
	toolchainCmd.AddCommand(toolchainShowCmd)
}

func runToolchainInit(cmd *cobra.Command, args []string) {
	path := toolchain.GetGlobalConfigPath()

	if toolchain.ExistsGlobalConfig() {
		vlog.Info("Config already exists: %s", path)
		vlog.Info("Delete it first if you want to regenerate")
		return
	}

	tmpl := toolchain.GetDefaultTemplate()
	if err := toolchain.SaveGlobal(tmpl); err != nil {
		vlog.Error("Failed to create config: %v", err)
		return
	}

	vlog.Info("Created %s", path)
	vlog.Info("Please edit the file to configure your toolchains")
}

func runToolchainList(cmd *cobra.Command, args []string) {
	mgr := toolchain.GetManager()
	toolchains, err := mgr.ListToolchains()
	if err != nil {
		vlog.Error("Failed to load toolchains: %v", err)
		return
	}

	defaultTC := mgr.GetDefaultToolchain()
	vlog.Info("Available toolchains:")
	for name, tc := range toolchains {
		mark := ""
		if name == defaultTC {
			mark = " (default)"
		}
		vlog.Info("  %s%s", name, mark)
		vlog.Info("    Display: %s", tc.DisplayName)
		vlog.Info("    Host:    %s", tc.Host)
		vlog.Info("    CC:      %s", tc.Tools.CC)
		vlog.Info("    CXX:     %s", tc.Tools.CXX)
	}
}

func runToolchainShow(cmd *cobra.Command, args []string) {
	mgr := toolchain.GetManager()

	name := ""
	if len(args) > 0 {
		name = args[0]
	}

	if name == "" {
		name = mgr.GetDefaultToolchain()
	}

	tc, err := mgr.GetToolchain(name)
	if err != nil {
		vlog.Error("Error: %v", err)
		return
	}

	vlog.Info("Toolchain: %s", tc.Name)
	vlog.Info("Display Name: %s", tc.DisplayName)
	vlog.Info("Host: %s", tc.Host)
	vlog.Info("")
	vlog.Info("Tools:")
	vlog.Info("  CC:     %s", tc.Tools.CC)
	vlog.Info("  CXX:    %s", tc.Tools.CXX)
	vlog.Info("  AR:     %s", tc.Tools.AR)
	vlog.Info("  LD:     %s", tc.Tools.LD)
	vlog.Info("  STRIP:  %s", tc.Tools.STRIP)
	vlog.Info("  RANLIB: %s", tc.Tools.RANLIB)
	vlog.Info("")
	vlog.Info("Default Flags:")
	vlog.Info("  CFlags:   [%s]", strings.Join(tc.DefaultFlags.CFlags, ", "))
	vlog.Info("  CxxFlags: [%s]", strings.Join(tc.DefaultFlags.CxxFlags, ", "))
	vlog.Info("  LdFlags:  [%s]", strings.Join(tc.DefaultFlags.LdFlags, ", "))
	vlog.Info("")
	vlog.Info("Download URL: %s", tc.DownloadURL)
	vlog.Info("Install Path: %s", tc.InstallPath)

	vlog.Info("")
	vlog.Info("Validation:")
	errs := toolchain.ValidateToolchain(tc)
	if len(errs) == 0 {
		vlog.Info("  All tools found")
	} else {
		for _, err := range errs {
			vlog.Error("  ERROR: %s", err)
		}
	}
}
