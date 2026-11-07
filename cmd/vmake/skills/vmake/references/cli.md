# CLI Reference

Generated from the live command tree at install time, so installed
extension plugin commands appear here too. For plugin details, run
`vmake <plugin> --help`.

`vmake [--project <value> -C --quiet -q --verbose -v --very-verbose -V --yes -y]` - VMake - A Go-based C/C++ build system
  `vmake build [--explain --install -i --install-type <value> --jobs <value> -j --keep-going -k --locked --manifest <value> --mode <value> --offline --prefix <value> -p --set <value> -s --tests --toolchain <value>]` - Build the project
  `vmake cache` - Inspect and remove shared git object caches
    `vmake cache list` - List cached upstream repositories
    `vmake cache remove <url>` - Remove one cached upstream repository
  `vmake check-symbols [--mode <value> --set <value> -s --strict --toolchain <value>]` - Audit exported symbols of shared libraries and binaries via nm
  `vmake clean [--all]` - Clean build artifacts
  `vmake completion [shell]` - Generate shell completion script
    `vmake completion install [--shell <value>]` - Install shell completion to your profile
  `vmake config [--set <value> -s]` - Edit the active build configuration or manage configuration files
    `vmake config copy <filename.json>` - Copy the active configuration to a new file without switching
    `vmake config describe [text]` - Print or set the description of the active configuration
    `vmake config list` - List project configuration files and the active selection
    `vmake config use <filename.json>` - Select an existing configuration in .vmake/project.json
  `vmake deps` - Manage project dependencies, pins, working trees and .vmake/vmake.lock
    `vmake deps fetch [pkg...] [--all --json --offline]` - Materialize locked dependency sources into .vmake_deps/ without building
    `vmake deps pin <pkg>@<version|commit> [--dry-run --json]` - Pin a dependency version or commit in .vmake/project.json and vmake.lock
    `vmake deps status [--json]` - Show unified dependency state across lock, pins, working trees and cache
    `vmake deps sync [--json]` - Synchronize working trees and vmake.lock without floating locked packages
    `vmake deps unpin <pkg> [--json]` - Remove a dependency pin from .vmake/project.json
    `vmake deps update [pkg[@version|commit]...] [--dry-run --json --refresh-repos]` - Re-resolve dependencies and rewrite vmake.lock
  `vmake distclean [--purge-sources]` - Deep clean all build artifacts
  `vmake doctor [--toolchain <value>]` - Diagnose platform prerequisites and build.go patterns
  `vmake ext` - Manage extension repositories
    `vmake ext add <name> <git-url>` - Add an extension repository
    `vmake ext list` - List extension repositories and plugins
    `vmake ext provision <name>` - Install the current host's toolchains declared by an extension repository
    `vmake ext remove <name>` - Remove an extension repository
    `vmake ext trust <name>` - Trust an extension repository and its future updates
    `vmake ext untrust <name>` - Revoke trust for an extension repository
    `vmake ext update [name]` - Update extension repositories
  `vmake git` - Git version management commands
    `vmake git tag [version] [--major --message <value> -m --minor --no-push]` - Create version tag, update latest, and push
  `vmake init-editor [--local <value> --remove]` - Generate editor support files for build.go (go.mod for gopls)
  `vmake inspect [--no-targets --set <value> -s]` - Print a machine-readable project model
  `vmake manifest` - Inspect and restore install manifests
    `vmake manifest checkout <path> [name]` - Checkout packages to recorded versions
    `vmake manifest show <path>` - Show manifest contents
  `vmake pkg` - Discover packages and manage project sources
    `vmake pkg clean <repo/name> [--all -a]` - Clean package build outputs
    `vmake pkg link <repo/name> <path>` - Build a package from a local working tree
    `vmake pkg list` - List packages materialized in this project
    `vmake pkg search [pattern] [--details --for-project --json --repo <value>]` - Search registered repositories for packages
    `vmake pkg show <repo/pkg>[@version] [--for-project --json]` - Show package metadata
    `vmake pkg unlink <repo/name>` - Remove a local source link
  `vmake query [--set <value> -s]` - Show dependency tree
    `vmake query config <pkg>` - Show effective option values and generated defines
    `vmake query targets` - List build targets without building
  `vmake rebuild [--explain --install -i --install-type <value> --jobs <value> -j --keep-going -k --locked --manifest <value> --mode <value> --offline --prefix <value> -p --set <value> -s --tests --toolchain <value>]` - Rebuild the project
  `vmake repo` - Manage package repositories
    `vmake repo add <name> <git-url-or-template> [--native -n]` - Add a package repository
    `vmake repo list` - List all package repositories
    `vmake repo remove <name>` - Remove a package repository
    `vmake repo trust <name>` - Trust a repository and its future updates
    `vmake repo untrust <name>` - Revoke trust for a repository
    `vmake repo update <name>` - Update a package repository
  `vmake skill` - Manage AI coding assistant skills
    `vmake skill install [--project <value> -p]` - Install VMake skill for AI assistants
    `vmake skill path` - Show skill installation paths
    `vmake skill uninstall` - Uninstall VMake skill
  `vmake test [--explain --jobs <value> -j --keep-going -k --locked --manifest <value> --mode <value> --offline --set <value> -s --toolchain <value>]` - Build and run test targets
  `vmake toolchain` - Show toolchain information
    `vmake toolchain list` - List available toolchains
    `vmake toolchain show [name]` - Show toolchain details
  `vmake update [version]` - Update vmake to latest or specified version
  `vmake version` - Print version information
