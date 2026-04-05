# CLI Reference

Generated from vmake built-in commands. For plugin commands,
run `vmake <plugin> --help` or check the plugin documentation.

`vmake [--quiet -q --verbose -v --very-verbose -V]` - VMake - A Go-based C/C++ build system
  `vmake build [--force -f --install -i --install-type --mode --prefix -p --toolchain --manifest]` - Build the project
  `vmake clean [--all]` - Clean build artifacts
  `vmake distclean` - Deep clean all build artifacts (build scripts, caches, install dir)
  `vmake completion [shell]` - Generate shell completion script
    `vmake install [--shell]` - Install shell completion to your profile
  `vmake config` - Configure project options
  `vmake ext` - Manage extension repositories
    `vmake add <name> <git-url>` - Add an extension repository
    `vmake list` - List extension repositories and plugins
    `vmake remove <name>` - Remove an extension repository
    `vmake update [name]` - Update extension repositories
  `vmake git` - Git version management commands
    `vmake tag [version] [--major --message -m --minor --no-push --yes -y]` - Create version tag, update latest, and push
  `vmake pkg` - Manage packages
    `vmake clean <repo/name> [--all -a]` - Clean package cache
    `vmake list` - List installed packages
    `vmake search [pattern]` - Search for packages
    `vmake update <repo/name>` - Update package source
  `vmake manifest` - Install manifest management
    `vmake show <path>` - Show manifest contents
    `vmake checkout <path> [name]` - Checkout packages from manifest
  `vmake query` - Show dependency tree
  `vmake rebuild [--install -i --install-type --prefix -p]` - Rebuild the project
  `vmake repo` - Manage package repositories
    `vmake add <name> <git-url-or-template> [--native -n]` - Add a package repository
    `vmake list` - List all package repositories
    `vmake remove <name>` - Remove a package repository
    `vmake update <name>` - Update a package repository
  `vmake skill` - Manage AI coding assistant skills
    `vmake install [--project -p]` - Install VMake skill for AI assistants
    `vmake path` - Show skill installation paths
    `vmake uninstall` - Uninstall VMake skill
  `vmake toolchain` - Show toolchain information
    `vmake list` - List available toolchains
    `vmake show [name]` - Show toolchain details
  `vmake update [version]` - Update vmake to latest or specified version
  `vmake version` - Print version information
