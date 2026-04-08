package main

import (
	"embed"
	"fmt"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"gitee.com/spock2300/vmake/internal/fs"
)

//go:embed skills/vmake/SKILL.md
//go:embed skills/vmake/references
//go:embed skills/vmake/examples
var skillFS embed.FS

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage AI coding assistant skills",
	Long:  "Install, uninstall, or show path for VMake AI skill.",
}

var skillInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install VMake skill for AI assistants",
	Long: `Installs the VMake skill to ~/.claude/skills/vmake/ and ~/.agents/skills/vmake/
so that AI coding assistants (Claude Code, OpenCode, Cursor, etc.) can assist
with VMake build configuration.

Use --project to also install to the current project's .claude/skills/ directory.`,
	Run: func(cmd *cobra.Command, args []string) {
		installSkill(cmd.Flag("project").Value.String())
	},
}

var skillUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall VMake skill",
	Run: func(cmd *cobra.Command, args []string) {
		uninstallSkill()
	},
}

var skillPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show skill installation paths",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Installation paths:")
		fmt.Printf("  Claude Code: %s\n", getSkillPath("claude"))
		fmt.Printf("  OpenCode:   %s\n", getSkillPath("agents"))
	},
}

func init() {
	skillInstallCmd.Flags().StringP("project", "p", "", "Also install to current project's .claude/skills/ directory")
	skillCmd.AddCommand(skillInstallCmd)
	skillCmd.AddCommand(skillUninstallCmd)
	skillCmd.AddCommand(skillPathCmd)
	RootCmd.AddCommand(skillCmd)
}

func getSkillPath(which string) string {
	home, _ := os.UserHomeDir()
	if which == "claude" {
		return filepath.Join(home, ".claude", "skills", "vmake")
	}
	return filepath.Join(home, ".agents", "skills", "vmake")
}

func installSkill(projectPath string) {
	targets := []string{getSkillPath("claude"), getSkillPath("agents")}
	if projectPath != "" {
		targets = append(targets, filepath.Join(projectPath, ".claude", "skills", "vmake"))
	}

	wasInstalled := isAlreadyInstalled(targets)

	err := copyEmbedToTargets(targets)
	if err != nil {
		fatalMsg("Failed to install skill: %v", err)
	}

	cliRef := generateCLIRef(RootCmd)
	for _, target := range targets {
		cliPath := filepath.Join(target, "references", "cli.md")
		if err := os.MkdirAll(filepath.Dir(cliPath), 0755); err != nil {
			fatalMsg("Failed to create references dir: %v", err)
		}
		if err := os.WriteFile(cliPath, []byte(cliRef), 0644); err != nil {
			fatalMsg("Failed to write cli.md: %v", err)
		}
	}

	action := "installed"
	if wasInstalled {
		action = "updated"
	}
	fmt.Printf("VMake skill %s successfully to:\n", action)
	for _, target := range targets {
		fmt.Printf("  %s\n", target)
	}
}

func uninstallSkill() {
	targets := []string{getSkillPath("claude"), getSkillPath("agents")}
	for _, target := range targets {
		if err := fs.RemoveAll(target); err != nil {
			fatalMsg("Failed to uninstall skill: %v", err)
		}
		fmt.Printf("Removed %s\n", target)
	}
	fmt.Println("VMake skill uninstalled.")
}

func isAlreadyInstalled(targets []string) bool {
	for _, target := range targets {
		if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err == nil {
			return true
		}
	}
	return false
}

func copyEmbedToTargets(targets []string) error {
	return stdfs.WalkDir(skillFS, "skills", func(path string, d stdfs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		content, err := skillFS.ReadFile(path)
		if err != nil {
			return err
		}

		for _, target := range targets {
			relPath := strings.TrimPrefix(path, "skills/vmake/")
			destPath := filepath.Join(target, relPath)
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(destPath, content, 0644); err != nil {
				return err
			}
		}
		return nil
	})
}

func generateCLIRef(root *cobra.Command) string {
	var b strings.Builder
	b.WriteString("# CLI Reference\n\n")
	b.WriteString("Generated from vmake built-in commands. For plugin commands,\n")
	b.WriteString("run `vmake <plugin> --help` or check the plugin documentation.\n\n")

	var walk func(cmd *cobra.Command, depth int)
	walk = func(cmd *cobra.Command, depth int) {
		if !cmd.IsAvailableCommand() || cmd.IsAdditionalHelpTopicCommand() {
			return
		}
		if cmd.Name() == "help" {
			return
		}

		indent := strings.Repeat("  ", depth)
		usage := cmd.Use
		if cmd.HasFlags() {
			var flags []string
			cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
				if f.Name == "help" {
					return
				}
				flag := "--" + f.Name
				if f.Shorthand != "" {
					flag += " -" + f.Shorthand
				}
				flags = append(flags, flag)
			})
			if len(flags) > 0 {
				usage += " [" + strings.Join(flags, " ") + "]"
			}
		}

		prefix := "vmake "
		if depth == 0 {
			prefix = ""
		}
		b.WriteString(fmt.Sprintf("%s`%s%s`", indent, prefix, usage))
		if cmd.Short != "" {
			b.WriteString(fmt.Sprintf(" - %s", cmd.Short))
		}
		b.WriteString("\n")

		for _, sub := range cmd.Commands() {
			walk(sub, depth+1)
		}
	}

	walk(root, 0)
	return b.String()
}
