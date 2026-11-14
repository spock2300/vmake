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
	if local {
		origin = api.SourceLocal
	}
	src := buildscript.NewSource(name, "/src/"+name+"/build.go", "/src/"+name, origin)
	pkg := api.NewPackage().SetName(name).SetVersions(versions)
	if local {
		pkg.SetGit("https://example.invalid/" + name + ".git")
	}
	return resolver.NewPackageNode(name, src, pkg)
}

func resolutionContext(t *testing.T, nodes ...*resolver.PackageNode) *RuntimeContext {
	t.Helper()
	r := resolver.NewResolver(nil, t.TempDir())
	for _, node := range nodes {
		r.Graph().Packages[node.ID] = node
		r.Graph().Order = append(r.Graph().Order, node.ID)
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

func TestRemoteVersionKeyMatrix(t *testing.T) {
	versions := map[string]string{"1.0.0": "ref100", "2.0.0": "ref200"}

	tests := []struct {
		name    string
		node    *resolver.PackageNode
		lock    *lockfile.Lock
		pins    map[string]string
		policy  resolver.LockPolicy
		wantVer string
		wantCom string
		wantOK  bool
		prepare func(*resolver.PackageNode)
	}{
		{
			name:    "native selected fallback",
			node:    resolutionNode("repo/native", false, versions),
			wantVer: "2.0.0",
			wantCom: "native-commit",
			wantOK:  true,
			prepare: func(n *resolver.PackageNode) {
				n.Native = &resolver.NativePackageInfo{GitURL: "https://example.invalid/native.git", Versions: versions, Selected: "2.0.0", Commit: "native-commit"}
			},
		},
		{
			name:    "lock entry wins over native fallback",
			node:    resolutionNode("repo/native", false, versions),
			lock:    lockWith("repo/native", "1.0.0", "lock-commit", "native"),
			wantVer: "1.0.0",
			wantCom: "lock-commit",
			wantOK:  true,
			prepare: func(n *resolver.PackageNode) {
				n.Native = &resolver.NativePackageInfo{GitURL: "https://example.invalid/native.git", Versions: versions, Selected: "2.0.0", Commit: "native-commit"}
			},
		},
		{
			name:    "config pin version reuses matching lock commit",
			node:    resolutionNode("repo/native", false, versions),
			lock:    lockWith("repo/native", "1.0.0", "lock-commit", "native"),
			pins:    map[string]string{"repo/native": "1.0.0"},
			wantVer: "1.0.0",
			wantCom: "lock-commit",
			wantOK:  true,
		},
		{
			name:    "config pin version falls back to native selected commit",
			node:    resolutionNode("repo/native", false, versions),
			pins:    map[string]string{"repo/native": "2.0.0"},
			wantVer: "2.0.0",
			wantCom: "native-commit",
			wantOK:  true,
			prepare: func(n *resolver.PackageNode) {
				n.Native = &resolver.NativePackageInfo{GitURL: "https://example.invalid/native.git", Versions: versions, Selected: "2.0.0", Commit: "native-commit"}
			},
		},
		{
			name:    "target commit pin",
			node:    resolutionNode("repo/native", false, versions),
			lock:    lockWith("repo/native", "1.0.0", "lock-commit", "native"),
			policy:  resolver.LockPolicy{Targets: map[string]string{"repo/native": pinnedCommit}},
			wantVer: "",
			wantCom: pinnedCommit,
			wantOK:  true,
		},
		{
			name:   "float suppresses lock and config pin",
			node:   resolutionNode("repo/native", false, versions),
			lock:   lockWith("repo/native", "1.0.0", "lock-commit", "native"),
			pins:   map[string]string{"repo/native": "1.0.0"},
			policy: resolver.LockPolicy{Float: map[string]bool{"repo/native": true}},
			wantOK: false,
		},
		{
			name:   "ignore all suppresses lock",
			node:   resolutionNode("repo/native", false, versions),
			lock:   lockWith("repo/native", "1.0.0", "lock-commit", "native"),
			policy: resolver.LockPolicy{IgnoreAll: true},
			wantOK: false,
		},
		{
			name:    "linked source wins over pin and lock",
			node:    resolutionNode("repo/native", false, versions),
			lock:    lockWith("repo/native", "1.0.0", "lock-commit", "native"),
			pins:    map[string]string{"repo/native": "2.0.0"},
			wantVer: resolver.LocalLinkVersion,
			wantCom: "link-commit",
			wantOK:  true,
			prepare: func(n *resolver.PackageNode) {
				n.Source.Link = &buildscript.SourceLink{Path: "/link/native", Commit: "link-commit", Native: true, LocalSource: true}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.prepare != nil {
				tc.prepare(tc.node)
			}
			ctx := resolutionContext(t, tc.node)
			applyResolutionState(ctx, tc.lock, tc.pins, tc.policy)
			gotVer, gotCom, gotOK := remoteVersionKey(ctx, tc.node.ID, tc.node)
			if gotVer != tc.wantVer || gotCom != tc.wantCom || gotOK != tc.wantOK {
				t.Fatalf("remoteVersionKey = (%q, %q, %v), want (%q, %q, %v)", gotVer, gotCom, gotOK, tc.wantVer, tc.wantCom, tc.wantOK)
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
			node:    resolutionNode("local/pkg", true, versions),
			lock:    lockWith("local/pkg", "1.0.0", "lock-commit", "git"),
			wantVer: "1.0.0",
			wantRef: "ref100",
			wantCom: "lock-commit",
		},
		{
			name:    "config pin version beats lock version",
			node:    resolutionNode("local/pkg", true, versions),
			lock:    lockWith("local/pkg", "1.0.0", "lock-commit", "git"),
			pins:    map[string]string{"local/pkg": "2.0.0"},
			wantVer: "2.0.0",
			wantRef: "ref200",
			wantCom: "",
		},
		{
			name:    "config pin matching lock version reuses commit",
			node:    resolutionNode("local/pkg", true, versions),
			lock:    lockWith("local/pkg", "1.0.0", "lock-commit", "git"),
			pins:    map[string]string{"local/pkg": "1.0.0"},
			wantVer: "1.0.0",
			wantRef: "ref100",
			wantCom: "lock-commit",
		},
		{
			name:    "commit pin",
			node:    resolutionNode("local/pkg", true, versions),
			pins:    map[string]string{"local/pkg": pinnedCommit},
			wantVer: "",
			wantRef: pinnedCommit,
			wantCom: pinnedCommit,
		},
		{
			name:    "no versions and pin version is rejected",
			node:    resolutionNode("local/pkg", true, nil),
			pins:    map[string]string{"local/pkg": "1.0.0"},
			wantErr: "declares no versions",
		},
		{
			name:    "no versions and locked commit is used",
			node:    resolutionNode("local/pkg", true, nil),
			lock:    lockWith("local/pkg", "", "lock-commit", "git"),
			wantVer: "",
			wantRef: "lock-commit",
			wantCom: "lock-commit",
		},
		{
			name:    "float suppresses lock and config pin",
			node:    resolutionNode("local/pkg", true, versions),
			lock:    lockWith("local/pkg", "1.0.0", "lock-commit", "git"),
			pins:    map[string]string{"local/pkg": "1.0.0"},
			policy:  resolver.LockPolicy{Float: map[string]bool{"local/pkg": true}},
			wantVer: "2.0.0",
			wantRef: "ref200",
			wantCom: "",
		},
		{
			name:    "plain local package without versions selects nothing",
			node:    resolutionNode("local/plain", true, nil),
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
			policy:      resolver.LockPolicy{Targets: map[string]string{"pkg": "1.2.3"}},
			pins:        map[string]string{"pkg": "9.9.9"},
			wantPin:     "1.2.3",
			wantFromTar: true,
			wantHasPin:  true,
			wantIgnored: true,
		},
		{
			name:        "float suppresses config pin and ignores lock",
			policy:      resolver.LockPolicy{Float: map[string]bool{"pkg": true}},
			pins:        map[string]string{"pkg": "9.9.9"},
			wantIgnored: true,
		},
		{
			name:       "config pin",
			pins:       map[string]string{"pkg": "9.9.9"},
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
			gotPin, gotFromTarget, gotHasPin := ctx.packagePin("pkg")
			if gotPin != tc.wantPin || gotFromTarget != tc.wantFromTar || gotHasPin != tc.wantHasPin {
				t.Fatalf("packagePin = (%q, %v, %v), want (%q, %v, %v)", gotPin, gotFromTarget, gotHasPin, tc.wantPin, tc.wantFromTar, tc.wantHasPin)
			}
			if got := ctx.isLockIgnoredFor("pkg"); got != tc.wantIgnored {
				t.Fatalf("isLockIgnoredFor = %v, want %v", got, tc.wantIgnored)
			}
		})
	}
}

func lockWith(name, version, commit, source string) *lockfile.Lock {
	l := lockfile.New()
	l.Set(name, &lockfile.LockedPkg{Version: version, Commit: commit, Source: source})
	return l
}
