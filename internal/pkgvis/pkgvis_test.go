package pkgvis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckInternalLocalPackages(t *testing.T) {
	root := filepath.Join("/workspace", "proj")
	vis := func(id, rel string) (string, string) {
		dir := root
		if rel != "." {
			dir = filepath.Join(root, filepath.FromSlash(rel))
		}
		return id, VisibilityPath(true, id, root, dir)
	}

	rootID, rootVis := vis("proj", ".")
	appID, appVis := vis("app", "app")
	commonID, commonVis := vis("common", "internal/common")
	netID, netVis := vis("net", "net")
	httpID, httpVis := vis("http", "net/http")
	wireID, wireVis := vis("wire", "net/internal/wire")
	netInternalID, netInternalVis := vis("internal", "net/internal")

	if err := CheckInternal(appID, appVis, commonID, commonVis); err != nil {
		t.Fatalf("root-level internal package should be visible to app: %v", err)
	}
	if err := CheckInternal(rootID, rootVis, commonID, commonVis); err != nil {
		t.Fatalf("root-level internal package should be visible to root: %v", err)
	}
	if err := CheckInternal(netID, netVis, wireID, wireVis); err != nil {
		t.Fatalf("net/internal/wire should be visible to net: %v", err)
	}
	if err := CheckInternal(httpID, httpVis, wireID, wireVis); err != nil {
		t.Fatalf("net/internal/wire should be visible to net/http: %v", err)
	}
	if err := CheckInternal(netID, netVis, netInternalID, netInternalVis); err != nil {
		t.Fatalf("net/internal should be visible to net: %v", err)
	}

	if err := CheckInternal(appID, appVis, wireID, wireVis); err == nil || !strings.Contains(err.Error(), "use of internal package") {
		t.Fatalf("app -> net/internal/wire = %v, want internal error", err)
	}
	if err := CheckInternal(rootID, rootVis, wireID, wireVis); err == nil || !strings.Contains(err.Error(), "use of internal package") {
		t.Fatalf("root -> net/internal/wire = %v, want internal error", err)
	}
}

func TestCheckInternalRemoteSubPackages(t *testing.T) {
	sdkVis := VisibilityPath(false, "myorg/sdk", "", "")
	drvVis := VisibilityPath(false, "myorg/sdk/drv/spi", "", "")
	halVis := VisibilityPath(false, "myorg/sdk/internal/hal", "", "")
	regVis := VisibilityPath(false, "myorg/sdk/drv/internal/reg", "", "")
	otherRemoteVis := VisibilityPath(false, "myorg/other", "", "")
	localAppVis := VisibilityPath(true, "app", "/proj", "/proj/app")

	if err := CheckInternal("myorg/sdk", sdkVis, "myorg/sdk/internal/hal", halVis); err != nil {
		t.Fatalf("sdk -> sdk/internal/hal: %v", err)
	}
	if err := CheckInternal("myorg/sdk/drv/spi", drvVis, "myorg/sdk/internal/hal", halVis); err != nil {
		t.Fatalf("sdk/drv/spi -> sdk/internal/hal: %v", err)
	}
	if err := CheckInternal("myorg/sdk/drv/spi", drvVis, "myorg/sdk/drv/internal/reg", regVis); err != nil {
		t.Fatalf("sdk/drv/spi -> sdk/drv/internal/reg: %v", err)
	}

	if err := CheckInternal("myorg/sdk", sdkVis, "myorg/sdk/drv/internal/reg", regVis); err == nil {
		t.Fatal("sdk root must not access sdk/drv/internal/reg")
	}
	if err := CheckInternal("myorg/other", otherRemoteVis, "myorg/sdk/internal/hal", halVis); err == nil {
		t.Fatal("other remote package must not access sdk/internal/hal")
	}
	if err := CheckInternal("app", localAppVis, "myorg/sdk/internal/hal", halVis); err == nil {
		t.Fatal("local app must not access remote sdk/internal/hal")
	}

	topInternalVis := VisibilityPath(false, "myorg/internal", "", "")
	topInternalSubVis := VisibilityPath(false, "myorg/internal/pub", "", "")
	topInternalPrivVis := VisibilityPath(false, "myorg/internal/internal/priv", "", "")
	if err := CheckInternal("app", localAppVis, "myorg/internal", topInternalVis); err != nil {
		t.Fatalf("top-level remote package named internal should be public: %v", err)
	}
	if err := CheckInternal("app", localAppVis, "myorg/internal/pub", topInternalSubVis); err != nil {
		t.Fatalf("public subpackage of myorg/internal should be visible: %v", err)
	}
	if err := CheckInternal("app", localAppVis, "myorg/internal/internal/priv", topInternalPrivVis); err == nil {
		t.Fatal("internal subpackage under myorg/internal must not be visible to app")
	}
	if got := VisibilityPath(true, "", "/proj/sub", "/proj"); got != "@local" {
		t.Fatalf("empty pkgID outside projectRoot = %q, want @local", got)
	}
}

func TestVisibilityPathPreservesInternalAcrossSymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real_proj")
	wireDir := filepath.Join(realRoot, "net", "internal", "wire")
	if err := os.MkdirAll(wireDir, 0755); err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(base, "link_proj")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	got := VisibilityPath(true, "wire", linkRoot, wireDir)
	if got != "@local/net/internal/wire" {
		t.Fatalf("VisibilityPath across symlinked root = %q, want @local/net/internal/wire", got)
	}
}
