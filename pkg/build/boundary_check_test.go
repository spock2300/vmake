package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func hostTestTools(t *testing.T) (*toolchain.Toolchain, *ResolvedTools) {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		cc, err = exec.LookPath("gcc")
	}
	if err != nil {
		t.Skip("no C compiler available")
	}
	arTool, err := exec.LookPath("ar")
	if err != nil {
		t.Skip("no ar available")
	}
	tc := &toolchain.Toolchain{
		Name: "host",
		Tools: toolchain.Tools{
			CC:  cc,
			CXX: cc,
			AR:  arTool,
			LD:  cc,
		},
	}
	tools := &ResolvedTools{
		CC:  cc,
		CXX: cc,
		AR:  arTool,
	}
	return tc, tools
}

func writeTestFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func makeTestSchedulerForRoot(
	t *testing.T,
	root string,
	targets map[string]map[string]*api.Target,
	pkgMeta map[string]PkgBuildMeta,
	pkgDirs map[string]string,
) *Scheduler {
	t.Helper()
	tc, tools := hostTestTools(t)
	graph, err := NewBuildGraph(targets, pkgMeta, nil)
	if err != nil {
		t.Fatalf("NewBuildGraph: %v", err)
	}
	layouts := make(map[string]*PackageLayout, len(pkgDirs))
	for pkgName, dir := range pkgDirs {
		layouts[pkgName] = &PackageLayout{
			BuildKey: "k1",
			Ready:    true,
			Dirs: api.PkgDirs{
				SourceDir: dir,
				BuildDir:  filepath.Join(dir, "build"),
			},
		}
	}
	return newScheduler(schedulerConfig{
		Graph:      graph,
		Toolchain:  tc,
		Platform:   api.Platform{OS: runtime.GOOS},
		Layouts:    layouts,
		Mode:       api.ModeDebug,
		Tools:      tools,
		RootDir:    root,
		NumWorkers: 2,
	})
}

func TestHeaderBoundaryUndeclaredAndPrivateHeaders(t *testing.T) {
	root := t.TempDir()
	writeTestFiles(t, root, map[string]string{
		"shared_inc/shared.h": "#define SHARED_VAL 10\n",
		"lib/include/pub.h":   "#define PUB_VAL 20\n",
		"lib/src/priv.h":      "#define PRIV_VAL 30\n",
		"lib/src/lib.c":       "#include \"../include/pub.h\"\n#include \"priv.h\"\nint lib_fn(void) { return PUB_VAL + PRIV_VAL; }\n",
		"app/src/undecl.c":    "#include \"../../lib/include/pub.h\"\nint main(void) { return PUB_VAL; }\n",
		"app/src/priv_inc.c":  "#include \"../../lib/src/priv.h\"\nint main(void) { return PRIV_VAL; }\n",
		"app/src/valid.c":     "#include \"pub.h\"\n#include \"shared.h\"\nint lib_fn(void);\nint main(void) { return lib_fn() + PUB_VAL + SHARED_VAL - 60; }\n",
	})

	pkgDirs := map[string]string{
		"root": root,
		"lib":  filepath.Join(root, "lib"),
		"app":  filepath.Join(root, "app"),
	}
	pkgMeta := map[string]PkgBuildMeta{
		"root": {VisPath: "@local"},
		"lib":  {VisPath: "@local/lib"},
		"app":  {VisPath: "@local/app"},
	}

	makeLibTarget := func() *api.Target {
		return api.NewTargetRegistry().Target("lib").
			SetKind(api.TargetStatic).
			AddFiles("src/lib.c").
			AddPublicIncludes("include", "../shared_inc")
	}

	undeclApp := api.NewTargetRegistry().Target("app").
		SetKind(api.TargetBinary).
		AddFiles("src/undecl.c")
	s1 := makeTestSchedulerForRoot(t, root, map[string]map[string]*api.Target{
		"lib": {"lib": makeLibTarget()},
		"app": {"app": undeclApp},
	}, pkgMeta, pkgDirs)
	err := s1.BuildAll()
	if err == nil || !strings.Contains(err.Error(), "undeclared header dependency") {
		t.Fatalf("expected undeclared header dependency error, got %v", err)
	}
	if err2 := s1.BuildAll(); err2 == nil || !strings.Contains(err2.Error(), "undeclared header dependency") {
		t.Fatalf("expected second run to fail without caching invalid compile, got %v", err2)
	}

	privApp := api.NewTargetRegistry().Target("app").
		SetKind(api.TargetBinary).
		AddFiles("src/priv_inc.c").
		AddDeps("lib:lib")
	s2 := makeTestSchedulerForRoot(t, root, map[string]map[string]*api.Target{
		"lib": {"lib": makeLibTarget()},
		"app": {"app": privApp},
	}, pkgMeta, pkgDirs)
	err = s2.BuildAll()
	if err == nil || !strings.Contains(err.Error(), "private header inclusion") {
		t.Fatalf("expected private header inclusion error, got %v", err)
	}

	validApp := api.NewTargetRegistry().Target("app").
		SetKind(api.TargetBinary).
		AddFiles("src/valid.c").
		AddDeps("lib:lib")
	s3 := makeTestSchedulerForRoot(t, root, map[string]map[string]*api.Target{
		"lib": {"lib": makeLibTarget()},
		"app": {"app": validApp},
	}, pkgMeta, pkgDirs)
	if err := s3.BuildAll(); err != nil {
		t.Fatalf("expected valid public + shared header build to succeed, got %v", err)
	}
}

func TestHeaderBoundaryUpToDateCacheCatchesRemovedDep(t *testing.T) {
	root := t.TempDir()
	libInc := filepath.Join(root, "lib", "include")
	writeTestFiles(t, root, map[string]string{
		"lib/include/pub.h": "#define PUB_VAL 7\n",
		"lib/src/lib.c":     "int dummy_lib(void) { return 0; }\n",
		"app/src/main.c":    "#include \"pub.h\"\nint main(void) { return PUB_VAL - 7; }\n",
	})

	pkgDirs := map[string]string{
		"lib": filepath.Join(root, "lib"),
		"app": filepath.Join(root, "app"),
	}
	pkgMeta := map[string]PkgBuildMeta{
		"lib": {VisPath: "@local/lib"},
		"app": {VisPath: "@local/app"},
	}

	makeTargetsWithDep := func(withDep bool) map[string]map[string]*api.Target {
		libT := api.NewTargetRegistry().Target("lib").
			SetKind(api.TargetStatic).
			AddFiles("src/lib.c").
			AddPublicIncludes(libInc)
		appT := api.NewTargetRegistry().Target("app").
			SetKind(api.TargetBinary).
			AddFiles("src/main.c").
			AddIncludes(libInc)
		if withDep {
			appT.AddDeps("lib:lib")
		}
		return map[string]map[string]*api.Target{
			"lib": {"lib": libT},
			"app": {"app": appT},
		}
	}

	sWithDep := makeTestSchedulerForRoot(t, root, makeTargetsWithDep(true), pkgMeta, pkgDirs)
	if err := sWithDep.BuildAll(); err != nil {
		t.Fatalf("initial build with dep failed: %v", err)
	}

	sRemovedDep := makeTestSchedulerForRoot(t, root, makeTargetsWithDep(false), pkgMeta, pkgDirs)
	err := sRemovedDep.BuildAll()
	if err == nil || !strings.Contains(err.Error(), "undeclared header dependency") {
		t.Fatalf("expected cached UpToDate check to reject removed AddDeps, got %v", err)
	}
}

func TestHeaderBoundaryTargetlessUmbrellaAncestorVsTargetAncestor(t *testing.T) {
	root := t.TempDir()
	writeTestFiles(t, root, map[string]string{
		"include/shared.h": "#define SHARED_V 5\n",
		"root_priv.h":      "#define ROOT_PRIV 9\n",
		"main.c":           "int main(void){return 0;}\n",
		"lib/ok.c":         "#include \"../include/shared.h\"\nint ok_fn(void) { return SHARED_V; }\n",
		"lib/bad.c":        "#include \"../root_priv.h\"\nint bad_fn(void) { return ROOT_PRIV; }\n",
	})

	pkgDirs := map[string]string{
		"ws":  root,
		"lib": filepath.Join(root, "lib"),
	}
	pkgMeta := map[string]PkgBuildMeta{
		"ws":  {VisPath: "@local"},
		"lib": {VisPath: "@local/lib"},
	}

	sTargetless := makeTestSchedulerForRoot(t, root, map[string]map[string]*api.Target{
		"lib": {
			"utils": api.NewTargetRegistry().Target("utils").
				SetKind(api.TargetStatic).
				AddFiles("ok.c").
				AddPublicIncludes("../include"),
		},
	}, pkgMeta, pkgDirs)
	if err := sTargetless.BuildAll(); err != nil {
		t.Fatalf("expected targetless workspace ancestor include/ to be allowed, got %v", err)
	}

	sWithRootTarget := makeTestSchedulerForRoot(t, root, map[string]map[string]*api.Target{
		"ws": {
			"app": api.NewTargetRegistry().Target("app").
				SetKind(api.TargetBinary).
				AddFiles("main.c").
				AddDeps("lib:utils"),
		},
		"lib": {
			"utils": api.NewTargetRegistry().Target("utils").
				SetKind(api.TargetStatic).
				AddFiles("bad.c"),
		},
	}, pkgMeta, pkgDirs)
	if err := sWithRootTarget.BuildAll(); err == nil || !strings.Contains(err.Error(), "undeclared header dependency") {
		t.Fatalf("expected target-bearing ancestor private header to be rejected, got %v", err)
	}
}

func TestHeaderBoundaryEmptyPublicIncludeAndUnneededWorkspacePkg(t *testing.T) {
	root := t.TempDir()
	writeTestFiles(t, root, map[string]string{
		"dep/dep.c":                   "int dep_fn(void) { return 1; }\n",
		"dep/priv.h":                  "#define DEP_PRIV 1\n",
		"sdk/internal/secret/priv.h":  "#define SECRET_PRIV 2\n",
		"app_empty/main.c":            "#include \"../dep/priv.h\"\nint main(void) { return DEP_PRIV; }\n",
		"app_ws/main.c":               "#include \"../sdk/internal/secret/priv.h\"\nint main(void) { return SECRET_PRIV; }\n",
	})

	tc, tools := hostTestTools(t)

	depT := api.NewTargetRegistry().Target("dep").SetKind(api.TargetStatic).AddFiles("dep.c")
	depT.AddPublicIncludes("")
	appEmptyT := api.NewTargetRegistry().Target("app").SetKind(api.TargetBinary).AddFiles("main.c").AddDeps("dep:dep")
	g1, err := NewBuildGraph(map[string]map[string]*api.Target{
		"dep":       {"dep": depT},
		"app_empty": {"app": appEmptyT},
	}, map[string]PkgBuildMeta{
		"dep":       {VisPath: "@local/dep"},
		"app_empty": {VisPath: "@local/app_empty"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s1 := newScheduler(schedulerConfig{
		Graph:     g1,
		Toolchain: tc,
		Platform:  api.Platform{OS: runtime.GOOS},
		Layouts: map[string]*PackageLayout{
			"dep":       {BuildKey: "k1", Ready: true, Dirs: api.PkgDirs{SourceDir: filepath.Join(root, "dep"), BuildDir: filepath.Join(root, "dep", "build")}},
			"app_empty": {BuildKey: "k1", Ready: true, Dirs: api.PkgDirs{SourceDir: filepath.Join(root, "app_empty"), BuildDir: filepath.Join(root, "app_empty", "build")}},
		},
		Tools:      tools,
		RootDir:    root,
		NumWorkers: 1,
	})
	if err := s1.BuildAll(); err == nil || !strings.Contains(err.Error(), "private header inclusion") {
		t.Fatalf("empty PublicIncludes must not disable header checks, got: %v", err)
	}

	appWsT := api.NewTargetRegistry().Target("app").SetKind(api.TargetBinary).AddFiles("main.c")
	g2, err := NewBuildGraph(map[string]map[string]*api.Target{
		"app_ws": {"app": appWsT},
	}, map[string]PkgBuildMeta{
		"app_ws": {VisPath: "@local/app_ws"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s2 := newScheduler(schedulerConfig{
		Graph:     g2,
		Toolchain: tc,
		Platform:  api.Platform{OS: runtime.GOOS},
		Layouts: map[string]*PackageLayout{
			"app_ws": {BuildKey: "k1", Ready: true, Dirs: api.PkgDirs{SourceDir: filepath.Join(root, "app_ws"), BuildDir: filepath.Join(root, "app_ws", "build")}},
		},
		WorkspaceDirs: map[string]string{
			"app_ws": filepath.Join(root, "app_ws"),
			"secret": filepath.Join(root, "sdk", "internal", "secret"),
		},
		Tools:      tools,
		RootDir:    root,
		NumWorkers: 1,
	})
	if err := s2.BuildAll(); err == nil || !strings.Contains(err.Error(), `owned by package "secret"`) {
		t.Fatalf("expected unneeded workspace package header to be caught, got: %v", err)
	}
}

func TestTargetDeclaredPathsAndColdBoundaryCheck(t *testing.T) {
	root := t.TempDir()
	writeTestFiles(t, root, map[string]string{
		"parent/include/pub.h":        "#define PUB 1\n",
		"parent/vendor/priv.h":        "#define PRIV 2\n",
		"parent/lib/libblob.a":        "stub",
		"parent/parent.c":             "int parent_fn(void) { return 1; }\n",
		"parent/child/child.c":        "#include \"pub.h\"\nint child_fn(void) { return PUB; }\n",
		"sibling/conf.h":              "#define CONF 1\n",
		"consumer/build.go":           "package main\n",
	})

	pkgDirs := map[string]string{
		"parent":       filepath.Join(root, "parent"),
		"parent/child": filepath.Join(root, "parent", "child"),
		"sibling":      filepath.Join(root, "sibling"),
		"consumer":     filepath.Join(root, "consumer"),
	}
	pkgMeta := map[string]PkgBuildMeta{
		"parent":       {VisPath: "@local/parent"},
		"parent/child": {VisPath: "@local/parent/child"},
		"sibling":      {VisPath: "@local/sibling"},
		"consumer":     {VisPath: "@local/consumer"},
	}

	parentT := func() *api.Target {
		return api.NewTargetRegistry().Target("parent").
			SetKind(api.TargetStatic).
			AddFiles("parent.c").
			AddPublicIncludes("include")
	}

	cases := []struct {
		name    string
		pkg     string
		target  *api.Target
		wantSub string
	}{
		{
			name: "cross-package-prebuilt",
			pkg:  "parent/child",
			target: api.NewTargetRegistry().Target("child").
				SetKind(api.TargetStatic).
				SetPrebuilt("../lib/libblob.a").
				AddDeps("parent:parent"),
			wantSub: "cross-package prebuilt path",
		},
		{
			name: "private-dir-include",
			pkg:  "parent/child",
			target: api.NewTargetRegistry().Target("child").
				SetKind(api.TargetStatic).
				AddFiles("child.c").
				AddIncludes("../vendor").
				AddDeps("parent:parent"),
			wantSub: "private header inclusion",
		},
		{
			name: "relative-parent-public-include",
			pkg:  "parent/child",
			target: api.NewTargetRegistry().Target("child").
				SetKind(api.TargetStatic).
				AddFiles("child.c").
				AddIncludes("../include").
				AddDeps("parent:parent"),
			wantSub: "cross-package relative include",
		},
		{
			name: "cross-package-relative-input",
			pkg:  "consumer",
			target: api.NewTargetRegistry().Target("gen").
				SetKind(api.TargetVoid).
				AddInputs("../sibling/conf.h").
				AddOutputs("$builddir/out.bin"),
			wantSub: "cross-package relative input",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tMap := map[string]map[string]*api.Target{
				"parent":  {"parent": parentT()},
				"sibling": {"sibling": api.NewTargetRegistry().Target("sibling").SetKind(api.TargetVoid)},
				tc.pkg:    {tc.target.Name(): tc.target},
			}
			graph, err := NewBuildGraph(tMap, pkgMeta, nil)
			if err != nil {
				t.Fatal(err)
			}
			violations := CheckExistingBuildBoundaries(graph, nil, nil, pkgDirs, root, api.ModeDebug)
			matched := false
			for _, v := range violations {
				if strings.Contains(v.Message, tc.wantSub) {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("cold CheckExistingBuildBoundaries violations = %+v, want %q", violations, tc.wantSub)
			}
		})
	}
}

func TestManagedDepsDirDoesNotFallBackToRootOwner(t *testing.T) {
	root := t.TempDir()
	unmanagedHdr := filepath.Join(root, ".vmake_deps", "aux", "ext.h")
	writeTestFiles(t, root, map[string]string{
		".vmake_deps/aux/ext.h": "#define EXT_V 42\n",
		"root_main.c":           "int main(void) { return 0; }\n",
		"sub/sub.c":             "#include <ext.h>\nint main(void) { return EXT_V - 42; }\n",
	})

	pkgDirs := map[string]string{
		"ws":  root,
		"sub": filepath.Join(root, "sub"),
	}
	pkgMeta := map[string]PkgBuildMeta{
		"ws":  {VisPath: "@local"},
		"sub": {VisPath: "@local/sub"},
	}

	s := makeTestSchedulerForRoot(t, root, map[string]map[string]*api.Target{
		"ws": {
			"root_app": api.NewTargetRegistry().Target("root_app").
				SetKind(api.TargetBinary).
				AddFiles("root_main.c"),
		},
		"sub": {
			"sub_app": api.NewTargetRegistry().Target("sub_app").
				SetKind(api.TargetBinary).
				AddFiles("sub.c").
				AddCFlags("-I" + filepath.Dir(unmanagedHdr)),
		},
	}, pkgMeta, pkgDirs)
	if err := s.BuildAll(); err != nil {
		t.Fatalf("managed .vmake_deps path falsely attributed to root package: %v", err)
	}
}

func TestDirectIncludeTransitivePiggybackAndUmbrella(t *testing.T) {
	root := t.TempDir()
	writeTestFiles(t, root, map[string]string{
		"leaf/include/leaf.h":       "#define LEAF_V 10\n",
		"leaf/leaf.c":               "int leaf_fn(void) { return 10; }\n",
		"mid/include/mid.h":         "#include \"leaf.h\"\n#define MID_V (LEAF_V + 5)\n",
		"mid/mid.c":                 "#include \"include/mid.h\"\nint mid_fn(void) { return MID_V; }\n",
		"libc_pkg/include/string.h": "typedef unsigned long size_t;\nvoid *memcpy(void *, const void *, size_t);\n",
		"libc_pkg/libc.c":           "int libc_dummy(void) { return 0; }\n",
		"app_ok/main.c":             "#include <string.h>\n#include \"mid.h\"\nint mid_fn(void);\nint main(void) { return mid_fn() - 15; }\n",
		"app_bad/main.c":            "#include \"mid.h\"\n#include \"leaf.h\"\nint mid_fn(void);\nint main(void) { return mid_fn() + LEAF_V - 25; }\n",
		"app_umb/main.c":            "#include \"leaf.h\"\nint leaf_fn(void);\nint main(void) { return leaf_fn() - 10; }\n",
	})

	pkgDirs := map[string]string{
		"leaf":     filepath.Join(root, "leaf"),
		"mid":      filepath.Join(root, "mid"),
		"libc_pkg": filepath.Join(root, "libc_pkg"),
		"umbrella": filepath.Join(root, "umbrella"),
		"app_ok":   filepath.Join(root, "app_ok"),
		"app_bad":  filepath.Join(root, "app_bad"),
		"app_umb":  filepath.Join(root, "app_umb"),
	}
	pkgMeta := map[string]PkgBuildMeta{
		"leaf":     {VisPath: "@local/leaf"},
		"mid":      {VisPath: "@local/mid", Deps: []string{"leaf", "libc_pkg"}},
		"libc_pkg": {VisPath: "@local/libc_pkg"},
		"umbrella": {VisPath: "@local/umbrella", Deps: []string{"leaf"}},
		"app_ok":   {VisPath: "@local/app_ok", Deps: []string{"mid"}},
		"app_bad":  {VisPath: "@local/app_bad", Deps: []string{"mid"}},
		"app_umb":  {VisPath: "@local/app_umb", Deps: []string{"umbrella"}},
	}

	makeBase := func() map[string]map[string]*api.Target {
		return map[string]map[string]*api.Target{
			"leaf": {
				"leaf": api.NewTargetRegistry().Target("leaf").
					SetKind(api.TargetStatic).
					AddFiles("leaf.c").
					AddPublicIncludes("include"),
			},
			"libc_pkg": {
				"c": api.NewTargetRegistry().Target("c").
					SetKind(api.TargetStatic).
					AddFiles("libc.c").
					AddPublicIncludes("include"),
			},
			"mid": {
				"mid": api.NewTargetRegistry().Target("mid").
					SetKind(api.TargetStatic).
					AddFiles("mid.c").
					AddPublicIncludes("include").
					AddDeps("leaf:leaf", "libc_pkg:c"),
			},
			"umbrella": {
				"umbrella": api.NewTargetRegistry().Target("umbrella").
					SetKind(api.TargetVoid).
					UseRequires(),
			},
		}
	}

	tOk := makeBase()
	tOk["app_ok"] = map[string]*api.Target{
		"app": api.NewTargetRegistry().Target("app").
			SetKind(api.TargetBinary).
			AddFiles("main.c").
			AddDeps("mid:mid"),
	}
	if err := makeTestSchedulerForRoot(t, root, tOk, pkgMeta, pkgDirs).BuildAll(); err != nil {
		t.Fatalf("expected nested header via direct dep + angle-bracket std header to pass, got %v", err)
	}

	tBad := makeBase()
	tBad["app_bad"] = map[string]*api.Target{
		"app": api.NewTargetRegistry().Target("app").
			SetKind(api.TargetBinary).
			AddFiles("main.c").
			AddDeps("mid:mid"),
	}
	coldGraph, err := NewBuildGraph(tBad, pkgMeta, nil)
	if err != nil {
		t.Fatal(err)
	}
	coldViolations := CheckExistingBuildBoundaries(coldGraph, nil, nil, pkgDirs, root, api.ModeDebug)
	foundCold := false
	for _, v := range coldViolations {
		if strings.Contains(v.Message, "transitive header inclusion") && strings.Contains(v.Message, `"leaf"`) {
			foundCold = true
		}
	}
	if !foundCold {
		t.Fatalf("expected cold deps check to catch transitive header inclusion, got %+v", coldViolations)
	}
	err = makeTestSchedulerForRoot(t, root, tBad, pkgMeta, pkgDirs).BuildAll()
	if err == nil || !strings.Contains(err.Error(), "transitive header inclusion") {
		t.Fatalf("expected build to reject direct include of transitive dep, got %v", err)
	}

	tUmb := makeBase()
	tUmb["app_umb"] = map[string]*api.Target{
		"app": api.NewTargetRegistry().Target("app").
			SetKind(api.TargetBinary).
			AddFiles("main.c").
			AddDeps("umbrella:*"),
	}
	if err := makeTestSchedulerForRoot(t, root, tUmb, pkgMeta, pkgDirs).BuildAll(); err != nil {
		t.Fatalf("expected grouping TargetVoid umbrella to expose member direct includes, got %v", err)
	}
}

func TestLinkedRemoteHeaderBoundariesAndContractLinkage(t *testing.T) {
	root := t.TempDir()
	writeTestFiles(t, root, map[string]string{
		"uniface/include/unihal.h": "void board_init_early(void);\n",
		"core/include/core.h":      "int core_v(void);\n",
		"core/core.c":              "#include \"include/core.h\"\nint core_v(void) { return 3; }\n",
		"mid/include/mid.h":        "#include \"core.h\"\nint mid_v(void);\n",
		"mid/mid.c":                "#include \"include/mid.h\"\nint mid_v(void) { return core_v(); }\n",
		"boot/boot.c":              "#include \"unihal.h\"\nint boot_entry(void) { board_init_early(); return 0; }\n",
		"boot_trans/boot.c":        "#include \"core.h\"\nint boot_entry(void) { return core_v() - 3; }\n",
		"app/main.c":               "int boot_entry(void);\nvoid board_init_early(void) {}\nint main(void) { return boot_entry(); }\n",
	})

	pkgDirs := map[string]string{
		"sdk/uniface":    filepath.Join(root, "uniface"),
		"sdk/core":       filepath.Join(root, "core"),
		"sdk/mid":        filepath.Join(root, "mid"),
		"sdk/boot":       filepath.Join(root, "boot"),
		"sdk/boot_trans": filepath.Join(root, "boot_trans"),
		"app":            filepath.Join(root, "app"),
	}
	pkgMeta := map[string]PkgBuildMeta{
		"sdk/uniface":    {Origin: api.SourceRemote, Linked: true, VisPath: "sdk/uniface"},
		"sdk/core":       {Origin: api.SourceRemote, Linked: true, VisPath: "sdk/core"},
		"sdk/mid":        {Origin: api.SourceRemote, Linked: true, Deps: []string{"sdk/core"}, VisPath: "sdk/mid"},
		"sdk/boot":       {Origin: api.SourceRemote, Linked: true, Deps: []string{"sdk/uniface"}, VisPath: "sdk/boot"},
		"sdk/boot_trans": {Origin: api.SourceRemote, Linked: true, Deps: []string{"sdk/mid"}, VisPath: "sdk/boot_trans"},
		"app":            {Origin: api.SourceLocal, VisPath: "@local/app"},
	}

	sContract := makeTestSchedulerForRoot(t, root, map[string]map[string]*api.Target{
		"sdk/uniface": {"uniface": api.NewTargetRegistry().Target("uniface").SetKind(api.TargetVoid).AddPublicIncludes("include")},
		"sdk/boot":    {"boot": api.NewTargetRegistry().Target("boot").SetKind(api.TargetStatic).AddFiles("boot.c").AddDeps("sdk/uniface:uniface")},
		"app":         {"app": api.NewTargetRegistry().Target("app").SetKind(api.TargetBinary).AddFiles("main.c").AddDeps("sdk/boot:boot")},
	}, pkgMeta, pkgDirs)
	if err := sContract.BuildAll(); err != nil {
		t.Fatalf("expected C contract header + link-time provider to succeed naturally, got %v", err)
	}

	sLinkedTrans := makeTestSchedulerForRoot(t, root, map[string]map[string]*api.Target{
		"sdk/core":       {"core": api.NewTargetRegistry().Target("core").SetKind(api.TargetStatic).AddFiles("core.c").AddPublicIncludes("include")},
		"sdk/mid":        {"mid": api.NewTargetRegistry().Target("mid").SetKind(api.TargetStatic).AddFiles("mid.c").AddPublicIncludes("include").AddDeps("sdk/core:core")},
		"sdk/boot_trans": {"boot": api.NewTargetRegistry().Target("boot").SetKind(api.TargetStatic).AddFiles("boot.c").AddDeps("sdk/mid:mid")},
		"app":            {"app": api.NewTargetRegistry().Target("app").SetKind(api.TargetBinary).AddFiles("main.c").AddDeps("sdk/boot_trans:boot")},
	}, pkgMeta, pkgDirs)
	if err := sLinkedTrans.BuildAll(); err == nil || !strings.Contains(err.Error(), "transitive header inclusion") {
		t.Fatalf("expected linked remote package to enforce direct header boundaries, got %v", err)
	}
}

func TestInactiveIfdefIncludeDoesNotTriggerFalseTransitiveError(t *testing.T) {
	root := t.TempDir()
	writeTestFiles(t, root, map[string]string{
		"leaf/include/types.h": "typedef int leaf_int_t;\n",
		"leaf/leaf.c":          "#include \"include/types.h\"\nleaf_int_t leaf_v(void) { return 7; }\n",
		"mid/include/mid.h":    "#include \"types.h\"\nleaf_int_t mid_v(void);\n",
		"mid/mid.c":            "#include \"include/mid.h\"\nleaf_int_t mid_v(void) { return 7; }\n",
		"app/main.c":           "#if 0\n#include \"types.h\"\n#endif\n#include \"mid.h\"\nint main(void) { return mid_v() - 7; }\n",
	})
	pkgDirs := map[string]string{
		"leaf": filepath.Join(root, "leaf"),
		"mid":  filepath.Join(root, "mid"),
		"app":  filepath.Join(root, "app"),
	}
	pkgMeta := map[string]PkgBuildMeta{
		"leaf": {VisPath: "@local/leaf"},
		"mid":  {Deps: []string{"leaf"}, VisPath: "@local/mid"},
		"app":  {Deps: []string{"mid"}, VisPath: "@local/app"},
	}
	s := makeTestSchedulerForRoot(t, root, map[string]map[string]*api.Target{
		"leaf": {"leaf": api.NewTargetRegistry().Target("leaf").SetKind(api.TargetStatic).AddFiles("leaf.c").AddPublicIncludes("include")},
		"mid":  {"mid": api.NewTargetRegistry().Target("mid").SetKind(api.TargetStatic).AddFiles("mid.c").AddPublicIncludes("include").AddDeps("leaf:leaf")},
		"app":  {"app": api.NewTargetRegistry().Target("app").SetKind(api.TargetBinary).AddFiles("main.c").AddDeps("mid:mid")},
	}, pkgMeta, pkgDirs)
	if err := s.BuildAll(); err != nil {
		t.Fatalf("inactive #if 0 include triggered false transitive error: %v", err)
	}
}
