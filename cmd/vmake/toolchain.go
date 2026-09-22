package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

var toolchainCmd = &cobra.Command{
	Use:   "toolchain",
	Short: "Show toolchain information",
	Long:  `Show information about the default system toolchain.`,
}

var toolchainListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available toolchains",
	Long:  `Show all available toolchains (built-in and registered by plugins).`,
	Run:   runToolchainList,
}

var toolchainShowCmd = &cobra.Command{
	Use:   "show [name]",
	Short: "Show toolchain details",
	Long: `Display detailed information about a specific toolchain.
If no name is provided, shows the default toolchain (host).`,
	Run: runToolchainShow,
}

func init() {
	RootCmd.AddCommand(toolchainCmd)
	toolchainCmd.AddCommand(toolchainListCmd)
	toolchainCmd.AddCommand(toolchainShowCmd)

	toolchainShowCmd.ValidArgsFunction = completeToolchain
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
		status := "installed"
		if len(toolchain.ValidateToolchain(tc)) > 0 {
			status = "unavailable"
		}
		vlog.Info("  %s%s [%s]", name, mark, status)
		vlog.Info("    Display: %s", tc.DisplayName)
		vlog.Info("    CC:      %s", tc.Tools.CC)
		vlog.Info("    CXX:     %s", tc.Tools.CXX)
	}
	if errs := mgr.ToolchainErrors(); len(errs) > 0 {
		vlog.Info("Unavailable toolchains:")
		for _, err := range errs {
			vlog.Info("  %s", formatToolchainDefinitionError(err))
		}
	}
}

func formatToolchainDefinitionError(err *toolchain.DefinitionError) string {
	path := err.Path()
	if rel, relErr := filepath.Rel(getExtensionsDir(), path); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		path = filepath.ToSlash(rel)
	}
	if err.Name() == "" {
		return fmt.Sprintf("%s [unidentified]: %v", path, err.Unwrap())
	}
	return fmt.Sprintf("%s [%s]: %v", err.Name(), path, err.Unwrap())
}

func runToolchainShow(cmd *cobra.Command, args []string) {
	mgr := toolchain.GetManager()

	name := mgr.GetDefaultToolchain()
	if len(args) > 0 {
		name = args[0]
	}

	tc, err := mgr.GetToolchain(name)
	fatalErr(err)

	vlog.Info("Toolchain: %s", tc.Name)
	vlog.Info("Display Name: %s", tc.DisplayName)
	vlog.Info("")
	vlog.Info("Tools:")
	vlog.Info("  CC:     %s", tc.Tools.CC)
	vlog.Info("  CXX:    %s", tc.Tools.CXX)
	vlog.Info("  AR:     %s", tc.Tools.AR)
	vlog.Info("  LD:     %s", tc.Tools.LD)
	vlog.Info("  STRIP:  %s", tc.Tools.STRIP)
	vlog.Info("  RANLIB: %s", tc.Tools.RANLIB)
	vlog.Info("")

	if tc.InstallPath != "" {
		vlog.Info("")
		vlog.Info("Install Path: %s", tc.InstallPath)
	}

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
