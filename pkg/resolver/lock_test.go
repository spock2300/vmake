package resolver

import (
	"testing"
	"time"

	"github.com/spock2300/vmake/pkg/repo"
)

func TestCloseReleasesRepositoryLocks(t *testing.T) {
	mgr := repo.NewRepoManager(t.TempDir())
	r := NewResolver(mgr, t.TempDir())

	shared, err := mgr.Lock("demo", false)
	if err != nil {
		t.Fatal(err)
	}
	r.repoLocks["demo"] = shared

	r.Close()
	r.Close()

	acquired := make(chan error, 1)
	go func() {
		exclusive, err := mgr.Lock("demo", true)
		if err == nil {
			err = exclusive.Release()
		}
		acquired <- err
	}()
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exclusive lock still blocked after Resolver.Close")
	}
}
