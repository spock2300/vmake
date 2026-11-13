package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func hasHostCC() bool {
	for _, cc := range []string{"cc", "gcc", "clang"} {
		if _, err := exec.LookPath(cc); err == nil {
			return true
		}
	}
	return false
}

func writePureAddDepsMultiModuleProject(t *testing.T, root string) string {
	t.Helper()
	proj := filepath.Join(root, "pure_adddeps")

	writeExtensionFile(t, filepath.Join(proj, "app", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("main.c").
			AddDeps("mid:core")
	})
}
`)
	writeExtensionFile(t, filepath.Join(proj, "app", "main.c"), `int mid_val(void);
int main(void) { return mid_val(); }
`)

	writeExtensionFile(t, filepath.Join(proj, "mid", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("core").
			SetKind(api.TargetStatic).
			AddFiles("mid.c").
			AddPublicIncludes("include").
			AddDeps("leaf:util")
	})
}
`)
	writeExtensionFile(t, filepath.Join(proj, "mid", "include", "mid.h"), "int mid_val(void);\n")
	writeExtensionFile(t, filepath.Join(proj, "mid", "mid.c"), `#include "include/mid.h"
#include "leaf.h"
int mid_val(void) { return leaf_val(); }
`)

	writeExtensionFile(t, filepath.Join(proj, "leaf", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("util").
			SetKind(api.TargetStatic).
			AddFiles("leaf.c").
			AddPublicIncludes("include")
	})
}
`)
	writeExtensionFile(t, filepath.Join(proj, "leaf", "include", "leaf.h"), "int leaf_val(void);\n")
	writeExtensionFile(t, filepath.Join(proj, "leaf", "leaf.c"), `#include "include/leaf.h"
int leaf_val(void) { return 0; }
`)

	return proj
}

func writeRootedGraphProject(t *testing.T, root string) string {
	t.Helper()
	proj := filepath.Join(root, "ws")

	writeExtensionFile(t, filepath.Join(proj, "app", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("use_fast").SetType(api.OptionBool).SetDefault(false)
	})
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("mid")
		if ctx.When("use_fast", true) {
			ctx.AddRequires("fast")
		}
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		t := ctx.Target("app").
			SetKind(api.TargetBinary).
			AddFiles("main.c").
			AddDeps("mid")
		if ctx.When("use_fast", true) {
			t.AddDeps("fast")
		}
	})
}
`)
	writeExtensionFile(t, filepath.Join(proj, "app", "main.c"), `int mid_val(void);
int main(void) { return mid_val(); }
`)

	writeExtensionFile(t, filepath.Join(proj, "mid", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("leaf")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("core").
			SetKind(api.TargetStatic).
			AddFiles("mid.c").
			AddPublicIncludes("include").
			AddDeps("leaf:util")
	})
}
`)
	writeExtensionFile(t, filepath.Join(proj, "mid", "include", "mid.h"), "int mid_val(void);\n")
	writeExtensionFile(t, filepath.Join(proj, "mid", "mid.c"), `#include "include/mid.h"
#include "leaf.h"
int mid_val(void) { return leaf_val(); }
`)

	writeExtensionFile(t, filepath.Join(proj, "fast", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("leaf")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("accel").
			SetKind(api.TargetStatic).
			AddFiles("fast.c").
			AddDeps("leaf:util")
	})
}
`)
	writeExtensionFile(t, filepath.Join(proj, "fast", "fast.c"), `#include "leaf.h"
int fast_val(void) { return leaf_val() + 1; }
`)

	writeExtensionFile(t, filepath.Join(proj, "leaf", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("util").
			SetKind(api.TargetStatic).
			AddFiles("leaf.c").
			AddPublicIncludes("include")
	})
}
`)
	writeExtensionFile(t, filepath.Join(proj, "leaf", "include", "leaf.h"), "int leaf_val(void);\n")
	writeExtensionFile(t, filepath.Join(proj, "leaf", "leaf.c"), `#include "include/leaf.h"
int leaf_val(void) { return 0; }
`)

	writeExtensionFile(t, filepath.Join(proj, "unused", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("unused").SetKind(api.TargetStatic).AddFiles("u.c")
	})
}
`)
	writeExtensionFile(t, filepath.Join(proj, "unused", "u.c"), "int u(void) { return 1; }\n")

	return proj
}

func TestDepsGraphFormatsAndUnifiedInspectQuery(t *testing.T) {
	pureProj := writePureAddDepsMultiModuleProject(t, t.TempDir())

	stdout, stderr, code := runInspectCommand(t, pureProj, "deps", "graph")
	if code != 0 || stderr != "" {
		t.Fatalf("deps graph exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	wantText := "app mid\nmid leaf\n"
	if stdout != wantText {
		t.Fatalf("deps graph =\n%s\nwant:\n%s", stdout, wantText)
	}

	stdout, stderr, code = runInspectCommand(t, pureProj, "deps", "graph", "--targets")
	if code != 0 || stderr != "" {
		t.Fatalf("deps graph --targets exit=%d stderr=%q", code, stderr)
	}
	wantTargetText := "app:app mid:core\nmid:core leaf:util\n"
	if stdout != wantTargetText {
		t.Fatalf("deps graph --targets =\n%s\nwant:\n%s", stdout, wantTargetText)
	}

	stdout, stderr, code = runInspectCommand(t, pureProj, "deps", "graph", "--format", "dot")
	if code != 0 || stderr != "" {
		t.Fatalf("deps graph --format dot exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "digraph vmake_deps {") || !strings.Contains(stdout, `"app" -> "mid";`) || !strings.Contains(stdout, `"mid" -> "leaf";`) {
		t.Fatalf("unexpected dot output:\n%s", stdout)
	}

	stdout, stderr, code = runInspectCommand(t, pureProj, "deps", "graph", "--format", "mermaid")
	if code != 0 || stderr != "" {
		t.Fatalf("deps graph --format mermaid exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "flowchart TD") || !strings.Contains(stdout, `["app"]`) || !strings.Contains(stdout, "-->") {
		t.Fatalf("unexpected mermaid output:\n%s", stdout)
	}

	stdout, stderr, code = runInspectCommand(t, pureProj, "inspect")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect exit=%d stderr=%q", code, stderr)
	}
	insp := decodeInspectEnvelope(t, stdout)
	if !slices.Equal(insp.Data.Roots, []string{"app"}) {
		t.Fatalf("inspect roots = %v, want [app]", insp.Data.Roots)
	}
	wantInspectGraph := []inspectEdge{{From: "app", To: "mid"}, {From: "mid", To: "leaf"}}
	if !slices.Equal(insp.Data.Graph, wantInspectGraph) {
		t.Fatalf("inspect graph = %v, want %v", insp.Data.Graph, wantInspectGraph)
	}
	appPkg := inspectPackageNamed(t, insp.Data, "app")
	if !slices.Equal(appPkg.Deps, []string{"mid"}) {
		t.Fatalf("inspect app.Deps = %v, want [mid]", appPkg.Deps)
	}

	stdout, stderr, code = runInspectCommand(t, pureProj, "inspect", "--no-targets")
	if code != 0 || stderr != "" {
		t.Fatalf("inspect --no-targets exit=%d stderr=%q", code, stderr)
	}
	inspNoTargets := decodeInspectEnvelope(t, stdout)
	if len(inspNoTargets.Data.Graph) != 0 {
		t.Fatalf("inspect --no-targets graph = %v, want empty", inspNoTargets.Data.Graph)
	}

	stdout, stderr, code = runInspectCommand(t, pureProj, "query")
	if code != 0 || stderr != "" {
		t.Fatalf("query exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "app (binary)") || !strings.Contains(stdout, "└── mid:core (static)") || !strings.Contains(stdout, "    └── leaf:util (static)") {
		t.Fatalf("query did not render nested AddDeps tree:\n%s", stdout)
	}

	rootedProj := writeRootedGraphProject(t, t.TempDir())
	stdout, stderr, code = runInspectCommand(t, rootedProj, "deps", "graph", "--all", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("deps graph --all --json exit=%d stderr=%q", code, stderr)
	}
	var env depsGraphEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("decode deps graph json: %v\n%s", err, stdout)
	}
	if !slices.Contains(env.Packages, "unused") || !slices.Contains(env.Packages, "fast") {
		t.Fatalf("expected --all packages to include unused and fast, got %v", env.Packages)
	}
	if !slices.Equal(env.Roots, []string{"app"}) {
		t.Fatalf("expected roots=[app], got %v", env.Roots)
	}
}

func TestDepsWhyPackageAndTarget(t *testing.T) {
	proj := writeRootedGraphProject(t, t.TempDir())

	stdout, stderr, code := runInspectCommand(t, proj, "deps", "why", "leaf")
	if code != 0 || stderr != "" {
		t.Fatalf("deps why leaf exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "# leaf\n") || !strings.Contains(stdout, "app\n") || !strings.Contains(stdout, "mid (OnRequire, app:app -> mid:core)") || !strings.Contains(stdout, "leaf (OnRequire, mid:core -> leaf:util)") {
		t.Fatalf("unexpected deps why leaf output:\n%s", stdout)
	}

	stdout, stderr, code = runInspectCommand(t, proj, "deps", "why", "leaf", "--all", "--json", "-s", "app/use_fast=true")
	if code != 0 || stderr != "" {
		t.Fatalf("deps why --all --json exit=%d stderr=%q", code, stderr)
	}
	var whyEnv depsWhyEnvelope
	if err := json.Unmarshal([]byte(stdout), &whyEnv); err != nil {
		t.Fatalf("decode deps why json: %v\n%s", err, stdout)
	}
	if !whyEnv.Exists || !whyEnv.Needed || len(whyEnv.Paths) != 2 {
		t.Fatalf("expected 2 paths to leaf when use_fast=true, got %+v", whyEnv)
	}

	stdout, stderr, code = runInspectCommand(t, proj, "deps", "why", "leaf:util")
	if code != 0 || stderr != "" {
		t.Fatalf("deps why leaf:util exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "# leaf:util\n") || !strings.Contains(stdout, "app:app\n") || !strings.Contains(stdout, "mid:core") || !strings.Contains(stdout, "leaf:util") {
		t.Fatalf("unexpected deps why leaf:util output:\n%s", stdout)
	}

	stdout, stderr, code = runInspectCommand(t, proj, "deps", "why", "app")
	if code != 0 || !strings.Contains(stdout, "app (root)") {
		t.Fatalf("deps why app exit=%d stdout=%q", code, stdout)
	}

	stdout, stderr, code = runInspectCommand(t, proj, "deps", "why", "unused")
	if code != 0 || !strings.Contains(stdout, "not reachable") {
		t.Fatalf("deps why unused exit=%d stdout=%q", code, stdout)
	}

	stdout, _, code = runInspectCommand(t, proj, "deps", "why", "missing_pkg", "--json")
	if code == 0 {
		t.Fatalf("expected non-zero exit for missing_pkg, stdout=%q", stdout)
	}
	var missingEnv depsWhyEnvelope
	if err := json.Unmarshal([]byte(stdout), &missingEnv); err != nil || missingEnv.Exists {
		t.Fatalf("unexpected missing json: err=%v env=%+v", err, missingEnv)
	}
}

func TestDepsCheckAndBuildEnforceGoRules(t *testing.T) {
	t.Run("healthy_project", func(t *testing.T) {
		proj := writeRootedGraphProject(t, t.TempDir())
		stdout, stderr, code := runInspectCommand(t, proj, "deps", "check")
		if code != 0 || stderr != "" || !strings.Contains(stdout, "Dependency graph OK") {
			t.Fatalf("deps check exit=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
		if hasHostCC() {
			_, buildStderr, buildCode := runInspectCommand(t, proj, "build")
			if buildCode != 0 {
				t.Fatalf("vmake build failed on healthy project: code=%d stderr=%s", buildCode, buildStderr)
			}
			stdout, stderr, code = runInspectCommand(t, proj, "deps", "check", "--json")
			if code != 0 || stderr != "" {
				t.Fatalf("post-build deps check exit=%d stderr=%q stdout=%q", code, stderr, stdout)
			}
			var checkEnv depsCheckEnvelope
			if err := json.Unmarshal([]byte(stdout), &checkEnv); err != nil || checkEnv.Status != "ok" || len(checkEnv.Issues) != 0 {
				t.Fatalf("unexpected post-build check json: err=%v env=%+v", err, checkEnv)
			}
		}
	})

	t.Run("onrequire_cycle", func(t *testing.T) {
		proj := filepath.Join(t.TempDir(), "cycle")
		writeExtensionFile(t, filepath.Join(proj, "root", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("a") })
}
`)
		writeExtensionFile(t, filepath.Join(proj, "a", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("b") })
}
`)
		writeExtensionFile(t, filepath.Join(proj, "b", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) { ctx.AddRequires("a") })
}
`)
		stdout, _, code := runInspectCommand(t, proj, "deps", "check", "--json")
		if code == 0 {
			t.Fatalf("expected non-zero exit for cycle, stdout=%s", stdout)
		}
		var env depsCheckEnvelope
		if err := json.Unmarshal([]byte(stdout), &env); err != nil {
			t.Fatalf("decode check json: %v\n%s", err, stdout)
		}
		if env.Status != "error" || len(env.Issues) != 1 || env.Issues[0].Kind != "package-cycle" || !strings.Contains(env.Issues[0].Message, "a -> b -> a") || strings.Contains(env.Issues[0].Message, "root -> a") {
			t.Fatalf("expected minimal cycle a -> b -> a, got %+v", env)
		}
	})

	t.Run("cross_package_reverse_target_cycle", func(t *testing.T) {
		proj := filepath.Join(t.TempDir(), "revcycle")
		writeExtensionFile(t, filepath.Join(proj, "a", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("a1").SetKind(api.TargetStatic).AddFiles("a1.c").AddDeps("b:b1")
		ctx.Target("a2").SetKind(api.TargetStatic).AddFiles("a2.c")
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "a", "a1.c"), "int a1(void){return 1;}\n")
		writeExtensionFile(t, filepath.Join(proj, "a", "a2.c"), "int a2(void){return 2;}\n")
		writeExtensionFile(t, filepath.Join(proj, "b", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("b1").SetKind(api.TargetStatic).AddFiles("b1.c")
		ctx.Target("b2").SetKind(api.TargetStatic).AddFiles("b2.c").AddDeps("a:a2")
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "b", "b1.c"), "int b1(void){return 1;}\n")
		writeExtensionFile(t, filepath.Join(proj, "b", "b2.c"), "int b2(void){return 2;}\n")

		stdout, _, code := runInspectCommand(t, proj, "deps", "check")
		if code == 0 || !strings.Contains(stdout, "[package-cycle]") || !strings.Contains(stdout, "a -> b -> a") {
			t.Fatalf("expected cross-package reverse target cycle error, code=%d stdout=%q", code, stdout)
		}
	})

	t.Run("internal_visibility_violation", func(t *testing.T) {
		proj := filepath.Join(t.TempDir(), "intvis")
		writeExtensionFile(t, filepath.Join(proj, "app", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("main.c").AddDeps("secret")
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "app", "main.c"), "int main(void){return 0;}\n")
		writeExtensionFile(t, filepath.Join(proj, "sdk", "internal", "secret", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("secret").SetKind(api.TargetStatic).AddFiles("s.c")
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "sdk", "internal", "secret", "s.c"), "int s(void){return 0;}\n")

		stdout, _, code := runInspectCommand(t, proj, "deps", "check")
		if code == 0 || !strings.Contains(stdout, "[internal-visibility]") {
			t.Fatalf("expected internal visibility error, code=%d stdout=%q", code, stdout)
		}
	})

	t.Run("build_rejects_private_header_and_reverse_symbol", func(t *testing.T) {
		if !hasHostCC() {
			t.Skip("no host C compiler")
		}

		hdrProj := filepath.Join(t.TempDir(), "hdr")
		writeExtensionFile(t, filepath.Join(hdrProj, "app", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("main.c").AddDeps("lib")
	})
}
`)
		writeExtensionFile(t, filepath.Join(hdrProj, "app", "main.c"), `#include "../lib/priv.h"
int main(void) { return PRIV_CONST; }
`)
		writeExtensionFile(t, filepath.Join(hdrProj, "lib", "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("lib").SetKind(api.TargetStatic).AddFiles("lib.c").AddPublicIncludes("include")
	})
}
`)
		writeExtensionFile(t, filepath.Join(hdrProj, "lib", "include", "pub.h"), "int lib_fn(void);\n")
		writeExtensionFile(t, filepath.Join(hdrProj, "lib", "priv.h"), "#define PRIV_CONST 2\n")
		writeExtensionFile(t, filepath.Join(hdrProj, "lib", "lib.c"), "#include \"include/pub.h\"\nint lib_fn(void){return 0;}\n")

		_, stderr, code := runInspectCommand(t, hdrProj, "build")
		if code == 0 || !strings.Contains(stderr, "includes private header") {
			t.Fatalf("expected private header build failure, code=%d stderr=%q", code, stderr)
		}
	})
}

func TestDepsSubagentReviewRegressions(t *testing.T) {
	t.Run("noisy_stdout_and_no_gitignore_mutation", func(t *testing.T) {
		proj := filepath.Join(t.TempDir(), "noisy")
		writeExtensionFile(t, filepath.Join(proj, "app", "build.go"), `package main

import (
	"fmt"
	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	fmt.Println("NOISY_MAIN")
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) {
		fmt.Println("NOISY_REQUIRE")
		ctx.AddRequires("mid")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		fmt.Println("NOISY_BUILD")
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("main.c").AddDeps("mid")
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "app", "main.c"), "int main(void){return 0;}\n")
		writeExtensionFile(t, filepath.Join(proj, "mid", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("mid").SetKind(api.TargetStatic).AddFiles("mid.c")
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "mid", "mid.c"), "int mid(void){return 0;}\n")

		stdout, stderr, code := runInspectCommand(t, proj, "deps", "graph", "--json")
		if code != 0 || stderr != "" {
			t.Fatalf("deps graph --json exit=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
		var gEnv depsGraphEnvelope
		if err := json.Unmarshal([]byte(stdout), &gEnv); err != nil {
			t.Fatalf("noisy build.go broke deps graph --json: %v\n%s", err, stdout)
		}

		stdout, stderr, code = runInspectCommand(t, proj, "deps", "why", "mid", "--json")
		if code != 0 || stderr != "" {
			t.Fatalf("deps why --json exit=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
		var wEnv depsWhyEnvelope
		if err := json.Unmarshal([]byte(stdout), &wEnv); err != nil {
			t.Fatalf("noisy build.go broke deps why --json: %v\n%s", err, stdout)
		}

		stdout, stderr, code = runInspectCommand(t, proj, "deps", "check", "--json")
		if code != 0 || stderr != "" {
			t.Fatalf("deps check --json exit=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
		var cEnv depsCheckEnvelope
		if err := json.Unmarshal([]byte(stdout), &cEnv); err != nil || cEnv.Status != "ok" {
			t.Fatalf("noisy build.go broke deps check --json: %v\n%s", err, stdout)
		}

		if _, err := os.Stat(filepath.Join(proj, ".gitignore")); !os.IsNotExist(err) {
			t.Fatalf("read-only deps commands must not create .gitignore, stat err=%v", err)
		}
	})

	t.Run("zero_target_public_umbrella_wrapping_internal", func(t *testing.T) {
		proj := filepath.Join(t.TempDir(), "umbrella")
		writeExtensionFile(t, filepath.Join(proj, "app", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("sdk")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("main.c").AddDeps("sdk")
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "app", "main.c"), "int secret_fn(void);\nint main(void){return secret_fn();}\n")
		writeExtensionFile(t, filepath.Join(proj, "sdk", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("secret")
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "sdk", "internal", "secret", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("secret").SetKind(api.TargetStatic).AddFiles("s.c")
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "sdk", "internal", "secret", "s.c"), "int secret_fn(void);\nint secret_fn(void){return 0;}\n")

		stdout, stderr, code := runInspectCommand(t, proj, "deps", "check")
		if code != 0 || stderr != "" {
			t.Fatalf("deps check rejected valid 0-target umbrella: code=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
		if hasHostCC() {
			_, bStderr, bCode := runInspectCommand(t, proj, "build")
			if bCode != 0 {
				t.Fatalf("build rejected valid 0-target umbrella: code=%d stderr=%q", bCode, bStderr)
			}
		}
	})

	t.Run("unmaterialized_explicit_target_and_missing_addrequires", func(t *testing.T) {
		proj := filepath.Join(t.TempDir(), "unmat")
		writeExtensionFile(t, filepath.Join(proj, "app", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("ext")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("main.c").AddDeps("ext:extlib")
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "app", "main.c"), "int main(void){return 0;}\n")
		writeExtensionFile(t, filepath.Join(proj, "ext", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetGit("https://example.invalid/ext.git")
	p.AddVersion("1.0.0", "v1.0.0")
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("extlib").SetKind(api.TargetStatic).AddFiles("ext.c")
	})
}
`)

		stdout, stderr, code := runInspectCommand(t, proj, "deps", "check")
		if code != 0 || stderr != "" {
			t.Fatalf("deps check failed on unmaterialized SetGit explicit target: code=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
		stdout, _, code = runInspectCommand(t, proj, "deps", "why", "ext:extlib", "--json")
		if code == 0 || !strings.Contains(stdout, "not materialized") {
			t.Fatalf("expected unmaterialized reason for ext:extlib, code=%d stdout=%q", code, stdout)
		}

		missingReq := filepath.Join(t.TempDir(), "missing_req")
		writeExtensionFile(t, filepath.Join(missingReq, "app", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("main.c").AddDeps("secret")
	})
}
`)
		writeExtensionFile(t, filepath.Join(missingReq, "app", "main.c"), "int main(void){return 0;}\n")
		writeExtensionFile(t, filepath.Join(missingReq, "sdk", "internal", "secret", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("secret").SetKind(api.TargetStatic).AddFiles("s.c")
	})
}
`)
		writeExtensionFile(t, filepath.Join(missingReq, "sdk", "internal", "secret", "s.c"), "int s(void){return 0;}\n")

		stdout, _, code = runInspectCommand(t, missingReq, "deps", "check")
		if code == 0 || !strings.Contains(stdout, "[internal-visibility]") || !strings.Contains(stdout, "[missing-dependency]") {
			t.Fatalf("expected both internal-visibility and missing-dependency, code=%d stdout=%q", code, stdout)
		}
	})

	t.Run("flag_conflicts_and_exit_codes", func(t *testing.T) {
		proj := writeRootedGraphProject(t, t.TempDir())

		for _, args := range [][]string{
			{"deps", "graph", "--json", "--targets"},
			{"deps", "graph", "--json", "--format", "dot"},
			{"deps", "graph", "-s", "unknown_opt=1"},
			{"deps", "why", "app", "-s", "unknown_opt=1"},
			{"deps", "check", "-s", "unknown_opt=1"},
		} {
			_, _, code := runInspectCommand(t, proj, args...)
			if code != 2 {
				t.Fatalf("args %v exit=%d, want 2", args, code)
			}
		}

		stdout, _, code := runInspectCommand(t, proj, "deps", "why", "app:", "--json")
		if code == 0 || !strings.Contains(stdout, "invalid target reference") {
			t.Fatalf("expected invalid target reference for 'app:', code=%d stdout=%q", code, stdout)
		}
	})

	t.Run("unwired_intermediate_userequires_diamond", func(t *testing.T) {
		proj := filepath.Join(t.TempDir(), "diamond")
		writeExtensionFile(t, filepath.Join(proj, "app", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("mid")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("main.c").UseRequires()
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "app", "main.c"), "int main(void){return 0;}\n")
		writeExtensionFile(t, filepath.Join(proj, "mid", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnRequire(func(ctx *api.RequireContext) {
		ctx.AddRequires("leaf")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("mid").SetKind(api.TargetStatic).AddFiles("mid.c")
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "mid", "mid.c"), "int mid(void){return 0;}\n")
		writeExtensionFile(t, filepath.Join(proj, "leaf", "build.go"), `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("leaf").SetKind(api.TargetStatic).AddFiles("leaf.c")
	})
}
`)
		writeExtensionFile(t, filepath.Join(proj, "leaf", "leaf.c"), "int leaf(void){return 0;}\n")

		stdout, stderr, code := runInspectCommand(t, proj, "deps", "why", "leaf:leaf", "--json")
		if code != 0 || stderr != "" {
			t.Fatalf("deps why leaf:leaf exit=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
		var env depsWhyEnvelope
		if err := json.Unmarshal([]byte(stdout), &env); err != nil || len(env.Steps) == 0 {
			t.Fatalf("expected reachable path to leaf:leaf via UseRequires, got err=%v env=%+v", err, env)
		}
	})
}
