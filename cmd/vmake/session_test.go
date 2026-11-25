package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommandSessionCapturesAmbientFlags(t *testing.T) {
	oldSet, oldLocked, oldOffline, oldConfig := ephemeralSetFlags, lockedFlag, offlineFlag, configFlag
	defer func() {
		ephemeralSetFlags, lockedFlag, offlineFlag, configFlag = oldSet, oldLocked, oldOffline, oldConfig
	}()
	ephemeralSetFlags = []string{"app/opt=1"}
	lockedFlag, offlineFlag, configFlag = true, true, "configs/test.json"

	session := newCommandSession()
	if len(session.setFlags) != 1 || session.setFlags[0] != "app/opt=1" {
		t.Fatalf("set flags = %v", session.setFlags)
	}
	if !session.locked || !session.offline {
		t.Fatalf("policy = locked=%v offline=%v", session.locked, session.offline)
	}
	if session.configSelector != "configs/test.json" {
		t.Fatalf("config selector = %q", session.configSelector)
	}
}

func TestCommandSessionCachesAndClosesStorage(t *testing.T) {
	arrangeResolveTestGlobals(t)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".vmake"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)

	session := &commandSession{}
	first := session.storageLocks()
	if first == nil {
		t.Fatal("storageLocks returned nil")
	}
	if got := session.storageLocks(); got != first {
		t.Fatal("storageLocks did not reuse the acquired session")
	}
	if err := session.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if session.storage != nil {
		t.Fatal("close did not clear the storage session")
	}
	if err := session.close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}
