# CLI Reference

Generated from the live command tree at install time, so installed
extension plugin commands appear here too. For plugin details, run
`vmake <plugin> --help`.

`vmake [--quiet -q --verbose -v --very-verbose -V --yes -y]` - VMake - A Go-based C/C++ build system
  `vmake build [--install -i --install-type --jobs -j --keep-going -k --manifest --mode --prefix -p --tests --toolchain]` - Build the project
  `vmake check-symbols [--strict]` - Audit exported symbols of shared libraries and binaries via nm (Linux only; refuses to run on Windows)
  `vmake clean [--all]` - Clean build artifacts
  `vmake completion [shell]` - Generate shell completion script
    `vmake completion install [--shell]` - Install shell completion to your profile
  `vmake config [--set -s]` - Open a TUI to configure build options for all packages.
  `vmake distclean [--purge-cache]` - Deep clean all build artifacts
  `vmake doctor` - Diagnose platform prerequisites and build.go patterns
  `vmake ext` - Manage extension repositories
    `vmake ext add <name> <git-url>` - Add an extension repository
    `vmake ext list` - List extension repositories and plugins
    `vmake ext remove <name>` - Remove an extension repository
    `vmake ext update [name]` - Update extension repositories
  `vmake git` - Git version management commands
    `vmake git tag [version] [--major --message -m --minor --no-push]` - Create version tag, update latest, and push
  `vmake init-editor [--local --remove]` - Generate editor support files for build.go (go.mod for gopls)
  `vmake lock` - Manage .vmake/vmake.lock (pinned dependency versions)
    `vmake lock show` - Print locked dependency versions
    `vmake lock update` - Re-resolve dependency versions and rewrite vmake.lock
  `vmake manifest` - Inspect and restore install manifests
    `vmake manifest checkout <path> [name]` - Checkout packages to recorded versions
    `vmake manifest show <path>` - Show manifest contents
  `vmake pkg` - Manage packages
    `vmake pkg clean <repo/name> [--all -a]` - Clean package cache
    `vmake pkg list` - List installed packages
    `vmake pkg search [pattern]` - Search for packages
    `vmake pkg update <repo/name>[@version] [--dry-run]` - Update package source (floats to origin/HEAD, or pins @version)
  `vmake query` - Show dependency tree
    `vmake query config <pkg>` - Show effective option values and generated defines
    `vmake query targets` - List build targets without building
  `vmake rebuild [--install -i --install-type --jobs -j --keep-going -k --manifest --mode --prefix -p --tests --toolchain]` - Rebuild the project
  `vmake repo` - Manage package repositories
    `vmake repo add <name> <git-url-or-template> [--native -n]` - Add a package repository
    `vmake repo list` - List all package repositories
    `vmake repo remove <name>` - Remove a package repository
    `vmake repo trust <name>` - Trust a repository's build.go scripts
    `vmake repo untrust <name>` - Revoke trust for a repository
    `vmake repo update <name>` - Update a package repository
  `vmake skill` - Manage AI coding assistant skills
    `vmake skill install [--project -p]` - Install VMake skill for AI assistants
    `vmake skill path` - Show skill installation paths
    `vmake skill uninstall` - Uninstall VMake skill
  `vmake test [--jobs -j --keep-going -k --manifest --mode --tests --toolchain]` - Build and run test targets
  `vmake toolchain` - Show toolchain information
    `vmake toolchain list` - List available toolchains
    `vmake toolchain show [name]` - Show toolchain details
  `vmake update [version]` - Update vmake to latest or specified version
  `vmake version` - Print version information
