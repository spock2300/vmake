# CLI Reference

Generated from vmake built-in commands. For plugin commands,
run `vmake <plugin> --help` or check the plugin documentation.

`vmake [--quiet -q --verbose -v --very-verbose -V]` - VMake - A Go-based C/C++ build system
  `vmake build [--force -f --install -i --install-type --manifest --mode --prefix -p --tests --toolchain]` - Build the project.
    `--install-type`: `runtime` (binaries+shared, default) or `sdk` (everything including static libs)
    `--mode`: `debug` or `release`
    `--manifest <path>`: pin versions from a manifest file
    `--toolchain`: override toolchain
  `vmake clean [--all]` - Clean build artifacts
  `vmake completion [bash|zsh|fish|powershell|install]` - Generate shell completion script
    `vmake completion install [--shell <name>]` - Install shell completion to your profile
  `vmake config [--set -s <name>=<value>]` - Open a TUI to configure build options for all packages.
    `--set` supports `option=value` (global), `pkg/option=value` (package-specific), and bool coercion (`true`/`false`/`on`/`off`/`1`/`0`). Validates choices against allowed values.
  `vmake distclean` - Deep clean all build artifacts
  `vmake ext` - Manage extension repositories
    `vmake ext add <name> <git-url>` - Add an extension repository
    `vmake ext list` - List extension repositories and plugins
    `vmake ext remove <name>` - Remove an extension repository
    `vmake ext update [name]` - Update extension repositories
  `vmake git` - Git version management commands
    `vmake git tag [version] [--major --message -m --minor --no-push --yes -y]` - Create version tag, update latest, and push
  `vmake manifest` - Inspect and restore install manifests
    `vmake manifest checkout <path> [name]` - Checkout packages to recorded versions
    `vmake manifest show <path>` - Show manifest contents
  `vmake pkg` - Manage packages
    `vmake pkg clean <repo/name> [--all -a]` - Clean package cache
    `vmake pkg list` - List installed packages
    `vmake pkg search [pattern]` - Search for packages
    `vmake pkg update <repo/name>` - Update package source
  `vmake query` - Show dependency tree
  `vmake rebuild [--install -i --install-type --prefix -p]` - Rebuild the project
  `vmake repo` - Manage package repositories
    `vmake repo add <name> <git-url-or-template> [--native -n]` - Add a package repository
    `vmake repo list` - List all package repositories
    `vmake repo remove <name>` - Remove a package repository
    `vmake repo update <name>` - Update a package repository
  `vmake skill` - Manage AI coding assistant skills
    `vmake skill install [--project -p]` - Install VMake skill for AI assistants
    `vmake skill path` - Show skill installation paths
    `vmake skill uninstall` - Uninstall VMake skill
  `vmake test` - Build and run test targets
  `vmake toolchain` - Show toolchain information
    `vmake toolchain list` - List available toolchains
    `vmake toolchain show [name]` - Show toolchain details
  `vmake update [version]` - Update vmake to latest or specified version
  `vmake version` - Print version information
