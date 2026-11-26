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

func TestExportedContainerWithSetGitEndToEndBuild(t *testing.T) {
	skipWithoutHostTools(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := canonicalTempDir(t)

	calcWork := filepath.Join(root, "calc-work")
	for rel, content := range map[string]string{
		"calc.h": "int calc_val(void);\n",
		"calc.c": "#include \"calc.h\"\nint calc_val(void) { return CALC_FACTOR + 7; }\n",
	} {
		dst := filepath.Join(calcWork, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	testGit(t, calcWork, "init", "-q", "-b", "master")
	testGit(t, calcWork, "config", "user.email", "test@example.com")
	testGit(t, calcWork, "config", "user.name", "test")
	testGit(t, calcWork, "add", "-A")
	testGit(t, calcWork, "commit", "-q", "-m", "v1")
	testGit(t, calcWork, "tag", "v1.0.0")

	bareDir := filepath.Join(root, "remotes")
	if err := os.MkdirAll(bareDir, 0755); err != nil {
		t.Fatal(err)
	}
	calcBare := filepath.Join(bareDir, "calc.git")
	testGit(t, calcWork, "clone", "-q", "--bare", ".", calcBare)

	sdkWork := filepath.Join(root, "sdk-work")
	sdkFiles := map[string]string{
		"vmake.json": `{
			"exports": {
				"math": "//sdk/math"
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
		"internal/calc/build.go": fmt.Sprintf(`package main
import (
	"fmt"
	"path/filepath"
	"github.com/spock2300/vmake/pkg/api"
)
func Main(p *api.Package) {
	p.OnPackage(func(p *api.Package) {
		p.SetGit(%q)
		p.AddVersion("1.0.0", "v1.0.0")
	})
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("factor").SetDefault(1).SetInternal(true)
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("calc").SetKind(api.TargetStatic).
			AddFiles(filepath.Join(p.SrcDir(), "calc.c")).
			AddPublicIncludes(p.SrcDir()).
			AddDefines(fmt.Sprintf("CALC_FACTOR=%%d", ctx.Int("factor")))
	})
}
`, calcBare),
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
	sdkBare := filepath.Join(bareDir, "sdk.git")
	testGit(t, sdkWork, "clone", "-q", "--bare", ".", sdkBare)

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
		t.Fatalf("RunBuild(exported container): %v", err)
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
	if _, ok := lk.GetSource("selforg/sdk/internal/calc"); !ok {
		t.Fatalf("missing SetGit source lock selforg/sdk/internal/calc: %+v", lk.Sources)
	}

	insp, err := InspectWithOptions(ctx, InspectOptions{Facts: true, FilterDeps: true})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	calcFacts, ok := insp.Facts("selforg/sdk/internal/calc")
	if !ok {
		t.Fatal("missing facts for selforg/sdk/internal/calc")
	}
	if !strings.HasSuffix(calcFacts.SrcDir, "src") || calcFacts.PackageDir == "" {
		t.Fatalf("calc facts = %+v, want PackageDir and materialized SrcDir", calcFacts)
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
	if _, _, err := ResolveSetPackageOption(ctxLocked, "selforg/sdk/sdk/math", "scale"); err == nil {
		t.Fatal("raw internal path of exported package must be rejected")
	} else if strings.Contains(err.Error(), "selforg/sdk/internal/calc") {
		t.Fatalf("available package list must not leak unexported internal packages: %v", err)
	}
}
