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
	Long:  `Show information about the built-in fallback system toolchain.`,
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
If no name is provided, shows the built-in fallback toolchain (host).`,
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
			mark = " (built-in fallback)"
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

func formatToolchainTool(tc *toolchain.Toolchain, name, configured string) string {
	label := name + ":"
	if configured == "" {
		return fmt.Sprintf("%-8s (not configured)", label)
	}
	installPath := tc.InstallPath
	if name == "MAKE" && tc.Tools.MAKE == "" {
		installPath = ""
	}
	resolved, err := toolchain.ResolveToolPath(configured, installPath)
	if err != nil {
		return fmt.Sprintf("%-8s %s (not found: %v)", label, configured, err)
	}
	source := "PATH"
	if filepath.IsAbs(configured) {
		source = "absolute"
	} else if installPath != "" {
		source = "install"
	}
	return fmt.Sprintf("%-8s %s -> %s [%s]", label, configured, filepath.ToSlash(resolved), source)
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
	if tc.TargetOS != "" || tc.TargetTriple != "" {
		vlog.Info("Target Defaults:")
		if tc.TargetOS != "" {
			vlog.Info("  target_os:     %s", tc.TargetOS)
		}
		if tc.TargetTriple != "" {
			vlog.Info("  target_triple: %s", tc.TargetTriple)
		}
	}
	vlog.Info("")
	if tc.Prefix != "" {
		vlog.Info("Prefix: %s", tc.Prefix)
	}
	vlog.Info("Tools:")
	for _, item := range []struct {
		name  string
		value string
	}{
		{"CC", tc.Tools.CC},
		{"CXX", tc.Tools.CXX},
		{"AR", tc.Tools.AR},
		{"LD", tc.Tools.LD},
		{"STRIP", tc.Tools.STRIP},
		{"RANLIB", tc.Tools.RANLIB},
		{"OBJCOPY", tc.Tools.OBJCOPY},
		{"SIZE", tc.Tools.SIZE},
		{"OBJDUMP", tc.Tools.OBJDUMP},
		{"NM", tc.Tools.NM},
		{"MAKE", tc.MakeTool()},
	} {
		vlog.Info("  %s", formatToolchainTool(tc, item.name, item.value))
	}
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
