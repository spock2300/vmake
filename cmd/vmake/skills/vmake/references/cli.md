# CLI Reference

Generated from the live command tree at install time, so installed
extension plugin commands appear here too. For plugin details, run
`vmake <plugin> --help`.

`vmake [--project <value> -C --quiet -q --verbose -v --very-verbose -V --yes -y]` - VMake - A Go-based C/C++ build system
  `vmake build [--explain --install -i --install-type <value> --jobs <value> -j --json --keep-going -k --manifest <value> --mode <value> --output <value> --prefix <value> -p --tests --toolchain <value>]` - Build the project
  `vmake cache` - Inspect and remove shared git object caches
    `vmake cache list` - List cached upstream repositories
    `vmake cache remove <url>` - Remove one cached upstream repository
  `vmake check-symbols [--mode <value> --strict --toolchain <value>]` - Audit exported symbols of shared libraries and binaries via nm
  `vmake clean [--all]` - Clean build artifacts
  `vmake completion [shell]` - Generate shell completion script
    `vmake completion install [--shell <value>]` - Install shell completion to your profile
  `vmake config [--set <value> -s]` - Edit the active build configuration or manage configuration files
    `vmake config copy <filename.json>` - Copy the active configuration to a new file without switching
    `vmake config describe [text]` - Print or set the description of the active configuration
    `vmake config list` - List project configuration files and the active selection
    `vmake config use <filename.json>` - Select an existing configuration in .vmake/project.json
  `vmake distclean [--purge-sources]` - Deep clean all build artifacts
  `vmake doctor [--json --toolchain <value>]` - Diagnose platform prerequisites and build.go patterns
  `vmake ext` - Manage extension repositories
    `vmake ext add <name> <git-url>` - Add an extension repository
    `vmake ext list` - List extension repositories and plugins
    `vmake ext remove <name>` - Remove an extension repository
    `vmake ext trust <name>` - Trust an extension repository at its current commit
    `vmake ext untrust <name>` - Revoke trust for an extension repository
    `vmake ext update [name]` - Update extension repositories
  `vmake git` - Git version management commands
    `vmake git tag [version] [--major --message <value> -m --minor --no-push]` - Create version tag, update latest, and push
  `vmake init-editor [--local <value> --remove]` - Generate editor support files for build.go (go.mod for gopls)
  `vmake inspect [--allow-dry-run-writes --set <value>]` - Print a machine-readable project model
  `vmake lock` - Manage .vmake/vmake.lock (pinned dependency versions)
    `vmake lock show` - Print locked dependency versions
    `vmake lock update` - Re-resolve dependency versions and rewrite vmake.lock
  `vmake manifest` - Inspect and restore install manifests
    `vmake manifest checkout <path> [name]` - Checkout packages to recorded versions
    `vmake manifest show <path>` - Show manifest contents
  `vmake pkg` - Manage packages
    `vmake pkg clean <repo/name> [--all -a]` - Clean package build outputs
    `vmake pkg list` - List package sources
    `vmake pkg search [pattern]` - Search for packages
    `vmake pkg update <repo/name>[@version] [--dry-run]` - Update package source (floats to origin/HEAD, or pins @version)
  `vmake query` - Show dependency tree
    `vmake query config <pkg>` - Show effective option values and generated defines
    `vmake query targets` - List build targets without building
  `vmake rebuild [--explain --install -i --install-type <value> --jobs <value> -j --json --keep-going -k --manifest <value> --mode <value> --output <value> --prefix <value> -p --tests --toolchain <value>]` - Rebuild the project
  `vmake repo` - Manage package repositories
    `vmake repo add <name> <git-url-or-template> [--native -n]` - Add a package repository
    `vmake repo list` - List all package repositories
    `vmake repo remove <name>` - Remove a package repository
    `vmake repo trust <name>` - Trust a repository's build.go scripts
    `vmake repo untrust <name>` - Revoke trust for a repository
    `vmake repo update <name>` - Update a package repository
  `vmake skill` - Manage AI coding assistant skills
    `vmake skill install [--project <value> -p]` - Install VMake skill for AI assistants
    `vmake skill path` - Show skill installation paths
    `vmake skill uninstall` - Uninstall VMake skill
  `vmake test [--explain --jobs <value> -j --json --keep-going -k --manifest <value> --mode <value> --output <value> --require-tests --tests --toolchain <value>]` - Build and run test targets
  `vmake toolchain` - Show toolchain information
    `vmake toolchain list` - List available toolchains
    `vmake toolchain show [name]` - Show toolchain details
  `vmake update [version]` - Update vmake to latest or specified version
  `vmake version` - Print version information
