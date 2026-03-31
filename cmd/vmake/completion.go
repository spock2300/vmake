package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"gitee.com/spock2300/vmake/pkg/toolchain"
)

var completionShell string

var completionCmd = &cobra.Command{
	Use:   "completion [shell]",
	Short: "Generate shell completion script",
	Long: `Generate shell autocompletion script.

  Print to stdout:
    vmake completion bash  > /etc/bash_completion.d/vmake
    vmake completion zsh   > ~/.zsh/completions/_vmake
    vmake completion fish  > ~/.config/fish/completions/vmake.fish

  Auto-install to your shell profile:
    vmake completion install`,
	Args:              cobra.ExactArgs(1),
	ValidArgs:         []string{"bash", "zsh", "fish", "powershell"},
	ValidArgsFunction: completeCompletionShell,
	Run: func(cmd *cobra.Command, args []string) {
		generateCompletion(args[0], os.Stdout)
	},
}

var completionInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install shell completion to your profile",
	Long: `Detect your shell and install completion automatically.

  bash:  ~/.local/share/bash-completion/completions/vmake
  zsh:   ~/.zsh/completions/_vmake  (adds fpath to ~/.zshrc if needed)
  fish:  ~/.config/fish/completions/vmake.fish`,
	Run: runCompletionInstall,
}

func init() {
	RootCmd.CompletionOptions.DisableDefaultCmd = true
	completionInstallCmd.Flags().StringVar(&completionShell, "shell", "", "override shell detection (bash, zsh, fish)")
	completionCmd.AddCommand(completionInstallCmd)
	RootCmd.AddCommand(completionCmd)
}

func generateCompletion(shell string, w io.Writer) {
	switch shell {
	case "bash":
		RootCmd.GenBashCompletionV2(w, true)
	case "zsh":
		RootCmd.GenZshCompletion(w)
	case "fish":
		RootCmd.GenFishCompletion(w, true)
	case "powershell":
		RootCmd.GenPowerShellCompletionWithDesc(w)
	default:
		fatalMsg("unsupported shell: %s (supported: bash, zsh, fish, powershell)", shell)
	}
}

func completeCompletionShell(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{"bash", "zsh", "fish", "powershell"}, cobra.ShellCompDirectiveNoFileComp
}

func runCompletionInstall(cmd *cobra.Command, args []string) {
	shell := detectShell()

	switch shell {
	case "bash":
		installBashCompletion()
	case "zsh":
		installZshCompletion()
	case "fish":
		installFishCompletion()
	default:
		fatalMsg("unsupported shell: %s (supported: bash, zsh, fish). Use --shell to override.", shell)
	}
}

func detectShell() string {
	if completionShell != "" {
		return completionShell
	}
	return filepath.Base(os.Getenv("SHELL"))
}

func installBashCompletion() {
	home := homeDir()
	dir := filepath.Join(home, ".local", "share", "bash-completion", "completions")
	path := filepath.Join(dir, "vmake")

	if err := os.MkdirAll(dir, 0755); err != nil {
		fatalErr(fmt.Errorf("create %s: %w", dir, err))
	}

	writeCompletionToFile(path, "bash")
	fmt.Printf("Installed bash completion to %s\n", path)
	fmt.Println("Restart your shell or run: exec bash")
}

func installZshCompletion() {
	home := homeDir()
	dir := filepath.Join(home, ".zsh", "completions")
	path := filepath.Join(dir, "_vmake")

	if err := os.MkdirAll(dir, 0755); err != nil {
		fatalErr(fmt.Errorf("create %s: %w", dir, err))
	}

	writeCompletionToFile(path, "zsh")

	zshrc := filepath.Join(home, ".zshrc")
	fpathLine := "fpath=(~/.zsh/completions $fpath)"

	if ensureLineInFile(zshrc, fpathLine) {
		fmt.Printf("Added fpath entry to %s\n", zshrc)
	}

	fmt.Printf("Installed zsh completion to %s\n", path)
	fmt.Println("Restart your shell or run: exec zsh")
}

func installFishCompletion() {
	home := homeDir()
	dir := filepath.Join(home, ".config", "fish", "completions")
	path := filepath.Join(dir, "vmake.fish")

	if err := os.MkdirAll(dir, 0755); err != nil {
		fatalErr(fmt.Errorf("create %s: %w", dir, err))
	}

	writeCompletionToFile(path, "fish")
	fmt.Printf("Installed fish completion to %s\n", path)
	fmt.Println("Restart your shell or run: exec fish")
}

func writeCompletionToFile(path, shell string) {
	var buf bytes.Buffer
	generateCompletion(shell, &buf)
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		fatalErr(fmt.Errorf("write %s: %w", path, err))
	}
}

func homeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		fatalMsg("cannot determine home directory: %v", err)
	}
	return dir
}

func ensureLineInFile(path, line string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if bytes.Contains(content, []byte(line)) {
		return false
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return false
	}
	defer f.Close()

	fmt.Fprintf(f, "\n%s\n", line)
	return true
}

func completeMapKeys[V any](m map[string]V) ([]string, cobra.ShellCompDirective) {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func completeSlice(s []string) ([]string, cobra.ShellCompDirective) {
	return s, cobra.ShellCompDirectiveNoFileComp
}

func completeToolchain(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	tcs, err := toolchain.GetManager().ListToolchains()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return completeMapKeys(tcs)
}

func completeMode(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeSlice([]string{"debug", "release"})
}

func completeInstallType(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"runtime\tInstall binaries and shared libraries only",
		"sdk\tInstall all artifacts including headers and static libraries",
	}, cobra.ShellCompDirectiveNoFileComp
}

func completeRepoName(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	infos := getRepoManager().ListInfo()
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func completeExtRepoName(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	repos := getPluginManager().ListRepos()
	names := make([]string, 0, len(repos))
	for _, r := range repos {
		names = append(names, r.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func completePkgRef(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	packagesDir := getPackagesDir()
	repoEntries, err := readDirEntries(packagesDir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var refs []string
	for _, repoEntry := range repoEntries {
		repoName := repoEntry.Name()
		pkgEntries, err := readDirEntries(filepath.Join(packagesDir, repoName))
		if err != nil {
			continue
		}
		for _, pkgEntry := range pkgEntries {
			refs = append(refs, repoName+"/"+pkgEntry.Name())
		}
	}
	return refs, cobra.ShellCompDirectiveNoFileComp
}
