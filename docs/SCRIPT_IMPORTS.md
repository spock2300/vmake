# Local Go imports in build scripts

Build scripts can import ordinary Go packages from the same project. The
package source is interpreted by yaegi; vmake does not run `go build` and does
not create `.so` files.

## Module declaration

vmake reads the module path from, in order:

1. `vmake.json` at the project/checkout root;
2. `.vmake/script.json` (same schema);
3. the `module` line of `go.mod`.

`vmake.json`:

```json
{
  "script": {
    "module": "hl-mcu-sdk",
    "root": "."
  }
}
```

`root` is relative to the file and defaults to `.`. It must stay inside the
current project or remote checkout. Without one of these files local imports
are disabled and vmake behaves as before.

## Usage

A build script can import a package under the declared module path:

```go
package main

import (
    "github.com/spock2300/vmake/pkg/api"
    "hl-mcu-sdk/buildlib/layout"
)

func Main(p *api.Package) {
    p.OnBuild(func(ctx *api.BuildContext) {
        p.Generate(func() error {
            return layout.Generate(partitionFile, policyFile, templateFile, p.BuildDir())
        })
    })
}
```

The package is a normal Go package (not `package main`) and should be pure Go:
cgo and `go:embed` are not supported by the interpreted source importer.

## How paths are resolved

vmake gives the interpreter a virtual GOPATH:

```text
<virtual GOPATH>/src/hl-mcu-sdk/...  ->  <module root>/...
```

The mapping is a read-only `fs.FS` implemented by vmake. No symlinks, temporary
GOPATH directories, or source copies are created, so the same path works on
Linux, macOS and Windows.

Local package imports are bound to one `buildscript.Source`:

- a local project uses the project root;
- a remote package uses its own checkout root;
- a remote package can never resolve the consuming project's `vmake.json` or
  source packages.

The search for `vmake.json`/`go.mod` starts at the build script directory and
stops at the owning project/checkout root. This also supports build scripts in
subdirectories and monorepos.

## Script IO semantics

Imported packages run in the build script's interpreter. vmake's script-relative
`os`/`filepath` wrappers therefore apply to them too: relative file paths resolve
against the directory of the build script that started the interpreter. Pass
absolute paths, or paths relative to that build script, when a helper needs
project files.

## Remote packages

A remote package must include its module declaration (`vmake.json` or `go.mod`)
and any imported helper packages in its checkout. The module path is internal to
that package; consumers do not need to add it to their own configuration.

If a remote package imports a path outside its checkout, the import fails. A
remote package cannot import a helper from the consuming project, and two remote
packages may use the same module path without interfering with each other.
