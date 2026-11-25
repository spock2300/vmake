package pipeline

import (
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/buildscript"
	"github.com/spock2300/vmake/pkg/lockfile"
	"github.com/spock2300/vmake/pkg/resolver"
)

const pinnedCommit = "0123456789abcdef0123456789abcdef01234567"

func resolutionNode(name string, local bool, versions map[string]string) *resolver.PackageNode {
	origin := api.SourceRemote
	container := "repo/pkg"
	rel := ""
	kind := resolver.SourceKindNative
	if local {
		origin = api.SourceLocal
		container = api.LocalContainerID
		rel = strings.TrimPrefix(name, "//")
		kind = resolver.SourceKindLocal
	}
	src := buildscript.NewSource(name, "/src/"+rel+"/build.go", "/src/"+rel, origin)
	src.Container = container
	src.RelPath = rel
	pkg := api.NewPackage().SetName(name).SetVersions(versions)
	if local {
		pkg.SetGit("https://example.invalid/" + rel + ".git")
	}
	return resolver.NewPackageNode(name, container, rel, kind, src, pkg)
}

func resolutionContext(t *testing.T, nodes ...*resolver.PackageNode) *RuntimeContext {
	t.Helper()
	r := resolver.NewResolver(nil, t.TempDir())
	for _, node := range nodes {
		r.Graph().Packages[node.ID] = node
		r.Graph().Order = append(r.Graph().Order, node.ID)
		if node.Container != "" && r.Graph().Containers[node.Container] == nil {
			r.Graph().Containers[node.Container] = &resolver.ContainerNode{
				ID:   node.Container,
				Kind: node.Kind,
			}
		}
	}
	return &RuntimeContext{
		Resolver: r,
		DepGraph: r.Graph(),
		Paths:    &Paths{ProjectDir: t.TempDir(), DepsDir: t.TempDir(), ReposDir: t.TempDir()},
	}
}

func applyResolutionState(ctx *RuntimeContext, lock *lockfile.Lock, pins map[string]string, policy resolver.LockPolicy) {
	ctx.Lock = lock
	ctx.Pins = pins
	ctx.LockPolicy = policy
	ctx.Resolver.SetLockfile(lock, policy.IgnoreAll)
	ctx.Resolver.SetLockPolicy(policy)
	ctx.Resolver.SetConfigPins(pins)
}

func TestContainerVersionKeyMatrix(t *testing.T) {
	versions := map[string]string{"1.0.0": "ref100", "2.0.0": "ref200"}

	tests := []struct {
		name      string
		container *resolver.ContainerNode
		lock      *lockfile.Lock
		pins      map[string]string
		policy    resolver.LockPolicy
		wantVer   string
		wantCom   string
		wantOK    bool
	}{
		{
			name: "native selected fallback",
			container: &resolver.ContainerNode{
				ID: "repo/native", Kind: resolver.SourceKindNative,
				URL: "https://example.invalid/native.git", Versions: versions, Version: "2.0.0", Commit: "native-commit",
			},
			wantVer: "2.0.0",
			wantCom: "native-commit",
			wantOK:  true,
		},
		{
			name: "lock entry wins over native fallback",
			container: &resolver.ContainerNode{
				ID: "repo/native", Kind: resolver.SourceKindNative,
				URL: "https://example.invalid/native.git", Versions: versions, Version: "2.0.0", Commit: "native-commit",
			},
			lock:    lockWithContainer("repo/native", "1.0.0", "lock-commit", "native"),
			wantVer: "1.0.0",
			wantCom: "lock-commit",
			wantOK:  true,
		},
		{
			name: "config pin version reuses matching lock commit",
			container: &resolver.ContainerNode{
				ID: "repo/native", Kind: resolver.SourceKindNative,
				Versions: versions,
			},
			lock:    lockWithContainer("repo/native", "1.0.0", "lock-commit", "native"),
			pins:    map[string]string{"repo/native": "1.0.0"},
			wantVer: "1.0.0",
			wantCom: "lock-commit",
			wantOK:  true,
		},
		{
			name: "config pin version falls back to native selected commit",
			container: &resolver.ContainerNode{
				ID: "repo/native", Kind: resolver.SourceKindNative,
				URL: "https://example.invalid/native.git", Versions: versions, Version: "2.0.0", Commit: "native-commit",
			},
			pins:    map[string]string{"repo/native": "2.0.0"},
			wantVer: "2.0.0",
			wantCom: "native-commit",
			wantOK:  true,
		},
		{
			name: "target commit pin",
			container: &resolver.ContainerNode{
				ID: "repo/native", Kind: resolver.SourceKindNative, Versions: versions,
			},
			lock:    lockWithContainer("repo/native", "1.0.0", "lock-commit", "native"),
			policy:  resolver.LockPolicy{Targets: map[string]string{"repo/native": pinnedCommit}},
			wantVer: "",
			wantCom: pinnedCommit,
			wantOK:  true,
		},
		{
			name: "float suppresses lock and config pin",
			container: &resolver.ContainerNode{
				ID: "repo/native", Kind: resolver.SourceKindNative, Versions: versions,
			},
			lock:   lockWithContainer("repo/native", "1.0.0", "lock-commit", "native"),
			pins:   map[string]string{"repo/native": "1.0.0"},
			policy: resolver.LockPolicy{Float: map[string]bool{"repo/native": true}},
			wantOK: false,
		},
		{
			name: "ignore all suppresses lock",
			container: &resolver.ContainerNode{
				ID: "repo/native", Kind: resolver.SourceKindNative, Versions: versions,
			},
			lock:   lockWithContainer("repo/native", "1.0.0", "lock-commit", "native"),
			policy: resolver.LockPolicy{IgnoreAll: true},
			wantOK: false,
		},
		{
			name: "linked source wins over pin and lock",
			container: &resolver.ContainerNode{
				ID: "repo/native", Kind: resolver.SourceKindNative, Versions: versions,
				Link: &buildscript.SourceLink{Path: "/link/native", Commit: "link-commit", Native: true, LocalSource: true},
			},
			lock:    lockWithContainer("repo/native", "1.0.0", "lock-commit", "native"),
			pins:    map[string]string{"repo/native": "2.0.0"},
			wantVer: resolver.LocalLinkVersion,
			wantCom: "link-commit",
			wantOK:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := resolutionContext(t)
			ctx.DepGraph.Containers[tc.container.ID] = tc.container
			applyResolutionState(ctx, tc.lock, tc.pins, tc.policy)
			gotVer, gotCom, gotOK := containerVersionKey(ctx, tc.container.ID, tc.container)
			if gotVer != tc.wantVer || gotCom != tc.wantCom || gotOK != tc.wantOK {
				t.Fatalf("containerVersionKey = (%q, %q, %v), want (%q, %q, %v)", gotVer, gotCom, gotOK, tc.wantVer, tc.wantCom, tc.wantOK)
			}
		})
	}
}

func TestResolvePackageSourceVersionMatrix(t *testing.T) {
	versions := map[string]string{"1.0.0": "ref100", "2.0.0": "ref200"}

	tests := []struct {
		name    string
		node    *resolver.PackageNode
		lock    *lockfile.Lock
		pins    map[string]string
		policy  resolver.LockPolicy
		wantVer string
		wantRef string
		wantCom string
		wantErr string
	}{
		{
			name:    "locked local SetGit version and commit",
			node:    resolutionNode("//pkg", true, versions),
			lock:    lockWithSource("//pkg", "1.0.0", "lock-commit", ""),
			wantVer: "1.0.0",
			wantRef: "ref100",
			wantCom: "lock-commit",
		},
		{
			name:    "config pin version beats lock version",
			node:    resolutionNode("//pkg", true, versions),
			lock:    lockWithSource("//pkg", "1.0.0", "lock-commit", ""),
			pins:    map[string]string{"//pkg": "2.0.0"},
			wantVer: "2.0.0",
			wantRef: "ref200",
			wantCom: "",
		},
		{
			name:    "config pin matching lock version reuses commit",
			node:    resolutionNode("//pkg", true, versions),
			lock:    lockWithSource("//pkg", "1.0.0", "lock-commit", ""),
			pins:    map[string]string{"//pkg": "1.0.0"},
			wantVer: "1.0.0",
			wantRef: "ref100",
			wantCom: "lock-commit",
		},
		{
			name:    "commit pin",
			node:    resolutionNode("//pkg", true, versions),
			pins:    map[string]string{"//pkg": pinnedCommit},
			wantVer: "",
			wantRef: pinnedCommit,
			wantCom: pinnedCommit,
		},
		{
			name:    "no versions and pin version is rejected",
			node:    resolutionNode("//pkg", true, nil),
			pins:    map[string]string{"//pkg": "1.0.0"},
			wantErr: "declares no versions",
		},
		{
			name:    "no versions and locked commit is used",
			node:    resolutionNode("//pkg", true, nil),
			lock:    lockWithSource("//pkg", "", "lock-commit", ""),
			wantVer: "",
			wantRef: "lock-commit",
			wantCom: "lock-commit",
		},
		{
			name:    "float suppresses lock and config pin",
			node:    resolutionNode("//pkg", true, versions),
			lock:    lockWithSource("//pkg", "1.0.0", "lock-commit", ""),
			pins:    map[string]string{"//pkg": "1.0.0"},
			policy:  resolver.LockPolicy{Float: map[string]bool{"//pkg": true}},
			wantVer: "2.0.0",
			wantRef: "ref200",
			wantCom: "",
		},
		{
			name:    "plain local SetGit package without versions selects nothing",
			node:    resolutionNode("//plain", true, nil),
			wantVer: "",
			wantRef: "",
			wantCom: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := resolutionContext(t, tc.node)
			applyResolutionState(ctx, tc.lock, tc.pins, tc.policy)
			src := newSourceStage(&runContext{ctx: ctx, opts: BuildOptions{}}, &configStage{needed: map[string]bool{tc.node.ID: true}})
			gotVer, gotRef, gotCom, err := src.resolvePackageSourceVersion(tc.node.ID, tc.node)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if gotVer != tc.wantVer || gotRef != tc.wantRef || gotCom != tc.wantCom {
				t.Fatalf("resolvePackageSourceVersion = (%q, %q, %q), want (%q, %q, %q)", gotVer, gotRef, gotCom, tc.wantVer, tc.wantRef, tc.wantCom)
			}
		})
	}
}

func TestPackagePinAndLockIgnoreMatrix(t *testing.T) {
	tests := []struct {
		name        string
		policy      resolver.LockPolicy
		pins        map[string]string
		wantPin     string
		wantFromTar bool
		wantHasPin  bool
		wantIgnored bool
	}{
		{
			name:        "target wins over config pin",
			policy:      resolver.LockPolicy{Targets: map[string]string{"//pkg": "1.2.3"}},
			pins:        map[string]string{"//pkg": "9.9.9"},
			wantPin:     "1.2.3",
			wantFromTar: true,
			wantHasPin:  true,
			wantIgnored: true,
		},
		{
			name:        "float suppresses config pin and ignores lock",
			policy:      resolver.LockPolicy{Float: map[string]bool{"//pkg": true}},
			pins:        map[string]string{"//pkg": "9.9.9"},
			wantIgnored: true,
		},
		{
			name:       "config pin",
			pins:       map[string]string{"//pkg": "9.9.9"},
			wantPin:    "9.9.9",
			wantHasPin: true,
		},
		{
			name:        "ignore all",
			policy:      resolver.LockPolicy{IgnoreAll: true},
			wantIgnored: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := resolutionContext(t)
			applyResolutionState(ctx, nil, tc.pins, tc.policy)
			gotPin, gotFromTarget, gotHasPin := ctx.lockQuery().Sources().PinFor("//pkg")
			if gotPin != tc.wantPin || gotFromTarget != tc.wantFromTar || gotHasPin != tc.wantHasPin {
				t.Fatalf("packagePin = (%q, %v, %v), want (%q, %v, %v)", gotPin, gotFromTarget, gotHasPin, tc.wantPin, tc.wantFromTar, tc.wantHasPin)
			}
			if got := ctx.isLockIgnoredFor("//pkg"); got != tc.wantIgnored {
				t.Fatalf("isLockIgnoredFor = %v, want %v", got, tc.wantIgnored)
			}
		})
	}
}

func TestVerifyLockBeforePreparePinMismatch(t *testing.T) {
	tests := []struct {
		name    string
		pins    map[string]string
		policy  resolver.LockPolicy
		lock    *lockfile.Lock
		wantErr string
	}{
		{
			name:    "config pin matches the lock",
			lock:    lockWithContainer("repo/pkg", "1.0.0", "c1", "native"),
			pins:    map[string]string{"repo/pkg": "1.0.0"},
			policy:  resolver.LockPolicy{Locked: true},
			wantErr: "",
		},
		{
			name:    "config pin differs from the lock",
			lock:    lockWithContainer("repo/pkg", "1.0.0", "c1", "native"),
			pins:    map[string]string{"repo/pkg": "2.0.0"},
			policy:  resolver.LockPolicy{Locked: true},
			wantErr: "pinned to 2.0.0 in .vmake/project.json, but vmake.lock has 1.0.0",
		},
		{
			name:    "target pin differs from the lock",
			lock:    lockWithContainer("repo/pkg", "1.0.0", "c1", "native"),
			policy:  resolver.LockPolicy{Locked: true, Targets: map[string]string{"repo/pkg": "3.0.0"}},
			wantErr: "pinned to 3.0.0 in .vmake/project.json, but vmake.lock has 1.0.0",
		},
		{
			name:    "float suppresses the pin comparison",
			lock:    lockWithContainer("repo/pkg", "1.0.0", "c1", "native"),
			pins:    map[string]string{"repo/pkg": "2.0.0"},
			policy:  resolver.LockPolicy{Locked: true, Float: map[string]bool{"repo/pkg": true}},
			wantErr: "",
		},
		{
			name:    "empty lock is a phase-level failure",
			lock:    lockfile.New(),
			pins:    map[string]string{"repo/pkg": "2.0.0"},
			policy:  resolver.LockPolicy{Locked: true},
			wantErr: "vmake.lock is out of date or missing",
		},
		{
			name:    "missing entry is reported before the pin",
			lock:    lockWithContainer("other/pkg", "1.0.0", "c1", "native"),
			pins:    map[string]string{"repo/pkg": "2.0.0"},
			policy:  resolver.LockPolicy{Locked: true},
			wantErr: "is not locked in vmake.lock",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := resolutionContext(t, resolutionNode("repo/pkg", false, nil))
			applyResolutionState(ctx, tc.lock, tc.pins, tc.policy)
			rc := newRunContext(ctx, BuildOptions{}, nil)
			src := newSourceStage(rc, &configStage{needed: NeededSet{"repo/pkg": true}})
			err := src.verifyLockBeforePrepare()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("verifyLockBeforePrepare: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
