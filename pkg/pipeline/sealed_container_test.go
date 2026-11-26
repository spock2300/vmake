package pipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/resolver"
)

func TestSealedExportedContainerEndToEndBuild(t *testing.T) {
	skipWithoutHostTools(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := canonicalTempDir(t)

	sdkWork := filepath.Join(root, "sdk-work")
	sdkFiles := map[string]string{
		"vmake.json": `{
			"exports": {
				"math": "//sdk/math"
			},
			"sealed_sources": {
				"//internal/calc": "vendor/calc"
			}
		}`,
		"sdk/math/build.go": `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.AddRequires("//internal/calc")
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("scale").SetDefault(2)
	})
	p.OnConfigureDeps(func(ctx *api.DepConfigContext) {
		ctx.SetDefault("//internal/calc", "factor", ctx.Int("scale")*10)
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("math").SetKind(api.TargetVoid).UseRequires()
	})
}
`,
		"internal/calc/build.go": `package main
import (
	"fmt"
	"path/filepath"
	"github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://unreachable.invalid/calc.git")
		p.AddVersion("1.0.0", "deadbeef")
		p.AddPatches("already-applied.patch")
	})
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("factor").SetDefault(1).SetInternal(true)
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("calc").SetKind(api.TargetStatic).
			AddFiles(filepath.Join(p.SrcDir(), "calc.c")).
			AddPublicIncludes(p.SrcDir()).
			AddDefines(fmt.Sprintf("CALC_FACTOR=%d", ctx.Int("factor")))
	})
}
`,
		"vendor/calc/calc.h": "int calc_val(void);\n",
		"vendor/calc/calc.c": "#include \"calc.h\"\nint calc_val(void) { return CALC_FACTOR + 7; }\n",
	}
	for rel, content := range sdkFiles {
		dst := filepath.Join(sdkWork, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	testGit(t, sdkWork, "init", "-q", "-b", "master")
	testGit(t, sdkWork, "config", "user.email", "test@example.com")
	testGit(t, sdkWork, "config", "user.name", "test")
	testGit(t, sdkWork, "add", "-A")
	testGit(t, sdkWork, "commit", "-q", "-m", "v1")
	testGit(t, sdkWork, "tag", "v1.0.0")
	bareDir := filepath.Join(root, "remotes")
	bare := filepath.Join(bareDir, "sdk.git")
	if err := os.MkdirAll(bareDir, 0755); err != nil {
		t.Fatal(err)
	}
	testGit(t, sdkWork, "clone", "-q", "--bare", ".", bare)

	proj := filepath.Join(root, "consumer")
	template := filepath.Join(bareDir, "{name}.git")
	projFiles := map[string]string{
		"vmake.json": fmt.Sprintf(`{
			"roots": ["//app"],
			"repos": {"selforg": {"native": %q}},
			"modules": {"selforg/sdk": ">=1.0.0"}
		}`, template),
		"app/build.go": `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.AddRequires("selforg/sdk/math")
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).
			AddFiles("main.c").
			AddDeps("selforg/sdk/math")
	})
}
`,
		"app/main.c": `#include "calc.h"
int main(void) { return calc_val() == 27 ? 0 : 1; }
`,
	}
	for rel, content := range projFiles {
		dst := filepath.Join(proj, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	lockPath := filepath.Join(proj, ".vmake", "vmake.lock")
	openCtx := func(policy resolver.LockPolicy) *RuntimeContext {
		var lk *lockfile.Lock
		if loaded, err := lockfile.Load(lockPath); err == nil {
			lk = loaded
		}
		ctx := NewContext(ResolveParams{
			WorkDir:      proj,
			Config:       emptyConfig(),
			Lock:         lk,
			LockPath:     lockPath,
			LockPolicy:   policy,
			TrustChecker: func(buildscript.Source) error { return nil },
			Paths: &Paths{
				ProjectDir: proj,
				DepsDir:    filepath.Join(proj, ".vmake_deps"),
				CacheDir:   filepath.Join(root, "cache"),
				ReposDir:   filepath.Join(root, "empty-repos"),
				LockPath:   lockPath,
			},
		})
		if err := Require(ctx); err != nil {
			t.Fatalf("Require: %v", err)
		}
		if err := Configure(ctx); err != nil {
			t.Fatalf("Configure: %v", err)
		}
		return ctx
	}

	ctx := openCtx(resolver.LockPolicy{})
	defer ctx.Resolver.Close()
	res, err := RunBuild(ctx, BuildOptions{})
	if err != nil {
		t.Fatalf("RunBuild(sealed exported container): %v", err)
	}
	appOut := res.TargetPlacements["//app:app"].OutputPath
	if appOut == "" {
		t.Fatalf("missing target placement for //app:app: %+v", res.TargetPlacements)
	}
	if out, err := exec.Command(appOut).CombinedOutput(); err != nil {
		t.Fatalf("built binary failed (factor*10+7 != 27): %v\n%s", err, out)
	}

	lk, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	if _, ok := lk.GetContainer("selforg/sdk"); !ok {
		t.Fatalf("missing container lock selforg/sdk: %+v", lk.Containers)
	}
	if len(lk.Sources) != 0 {
		t.Fatalf("sealed source leaked into consumer vmake.lock sources: %+v", lk.Sources)
	}

	insp, err := InspectWithOptions(ctx, InspectOptions{Facts: true, FilterDeps: true})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	calcFacts, ok := insp.Facts("selforg/sdk/internal/calc")
	if !ok {
		t.Fatal("missing facts for selforg/sdk/internal/calc")
	}
	if !calcFacts.Sealed || !strings.HasSuffix(calcFacts.SrcDir, filepath.Join("vendor", "calc")) {
		t.Fatalf("calc facts = %+v, want Sealed=true and SrcDir ending with vendor/calc", calcFacts)
	}
	ctx.Resolver.Close()

	ctxLocked := openCtx(resolver.LockPolicy{Locked: true, Offline: true})
	defer ctxLocked.Resolver.Close()
	if _, err := RunBuild(ctxLocked, BuildOptions{}); err != nil {
		t.Fatalf("locked+offline rebuild failed: %v", err)
	}

	ctxLocked.Config.SetOption("selforg/sdk/internal/calc", "factor", 99)
	if err := ValidateConfigValues(ctxLocked, nil); err == nil {
		t.Fatal("external consumer must not configure non-exported package")
	}
}

func TestPkgLinkOverridesSealedSource(t *testing.T) {
	skipWithoutHostTools(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := canonicalTempDir(t)

	sdkWork := filepath.Join(root, "sdk-work")
	for rel, content := range map[string]string{
		"vmake.json": `{
			"exports": {"api": "//api"},
			"sealed_sources": {"//api": "vendor/api"}
		}`,
		"api/build.go": `package main
import (
	"path/filepath"
	"github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit("https://unreachable.invalid/api.git")
		p.AddVersion("1.0.0", "deadbeef")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("api").SetKind(api.TargetStatic).
			AddFiles(filepath.Join(p.SrcDir(), "api.c")).
			AddPublicIncludes(p.SrcDir())
	})
}
`,
		"vendor/api/api.h": "int api_code(void);\n",
		"vendor/api/api.c": "#include \"api.h\"\nint api_code(void) { return 0; }\n",
	} {
		dst := filepath.Join(sdkWork, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	testGit(t, sdkWork, "init", "-q", "-b", "master")
	testGit(t, sdkWork, "config", "user.email", "test@example.com")
	testGit(t, sdkWork, "config", "user.name", "test")
	testGit(t, sdkWork, "add", "-A")
	testGit(t, sdkWork, "commit", "-q", "-m", "v1")
	testGit(t, sdkWork, "tag", "v1.0.0")

	bareDir := filepath.Join(root, "remotes")
	if err := os.MkdirAll(bareDir, 0755); err != nil {
		t.Fatal(err)
	}
	testGit(t, sdkWork, "clone", "-q", "--bare", ".", filepath.Join(bareDir, "sdk.git"))

	overrideSrc := filepath.Join(root, "override-api")
	if err := os.MkdirAll(overrideSrc, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"api.h": "int api_code(void);\n",
		"api.c": "#include \"api.h\"\nint api_code(void) { return 1; }\n",
	} {
		if err := os.WriteFile(filepath.Join(overrideSrc, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	testGit(t, overrideSrc, "init", "-q", "-b", "master")
	testGit(t, overrideSrc, "config", "user.email", "test@example.com")
	testGit(t, overrideSrc, "config", "user.name", "test")
	testGit(t, overrideSrc, "add", "-A")
	testGit(t, overrideSrc, "commit", "-q", "-m", "override")

	proj := filepath.Join(root, "consumer")
	for rel, content := range map[string]string{
		"vmake.json": fmt.Sprintf(`{
			"roots": ["//"],
			"repos": {"selforg": {"native": %q}},
			"modules": {"selforg/sdk": ">=1.0.0"}
		}`, filepath.Join(bareDir, "{name}.git")),
		"build.go": `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) {
	p.AddRequires("selforg/sdk/api")
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).AddFiles("main.c").AddDeps("selforg/sdk/api")
	})
}
`,
		"main.c": `#include "api.h"
int main(void) { return api_code(); }
`,
	} {
		dst := filepath.Join(proj, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	ctx := NewContext(ResolveParams{
		WorkDir:      proj,
		Config:       emptyConfig(),
		SourceLinks:  map[string]string{"selforg/sdk/api": overrideSrc},
		TrustChecker: func(buildscript.Source) error { return nil },
		Paths: &Paths{
			ProjectDir: proj,
			DepsDir:    filepath.Join(proj, ".vmake_deps"),
			CacheDir:   filepath.Join(root, "cache"),
			ReposDir:   filepath.Join(root, "empty-repos"),
			LockPath:   filepath.Join(proj, ".vmake", "vmake.lock"),
		},
	})
	if err := Require(ctx); err != nil {
		t.Fatalf("Require: %v", err)
	}
	defer ctx.Resolver.Close()
	if err := Configure(ctx); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	res, err := RunBuild(ctx, BuildOptions{})
	if err != nil {
		t.Fatalf("RunBuild(linked override of sealed source): %v", err)
	}
	if err := exec.Command(res.TargetPlacements["//:app"].OutputPath).Run(); err == nil {
		t.Fatal("linked override binary returned 0; expected override source (return 1) to win over sealed source")
	}
	insp, err := InspectWithOptions(ctx, InspectOptions{Facts: true, FilterDeps: true})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	facts, ok := insp.Facts("selforg/sdk/api")
	if !ok {
		t.Fatal("missing facts for selforg/sdk/api")
	}
	if facts.Sealed || !facts.Linked {
		t.Fatalf("linked override facts = %+v, want Sealed=false Linked=true", facts)
	}
	if filepath.Clean(facts.SrcDir) != filepath.Clean(overrideSrc) {
		t.Fatalf("linked SrcDir = %q, want %q", facts.SrcDir, overrideSrc)
	}
	if gotKey, wantKey := insp.layouts["selforg/sdk/api"].BuildKey, res.Layouts["selforg/sdk/api"].BuildKey; gotKey != wantKey {
		t.Fatalf("linked sealed buildKey mismatch: inspect=%q build=%q", gotKey, wantKey)
	}
}
