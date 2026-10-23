package lock

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestLockFileName(t *testing.T) {
	// Name produced by Symfony's FlockStore for the key used by GeolocationDbUpdater
	s := &flockStore{dir: "data/locks"}
	if got := filepath.Base(s.lockFile("geolocation-db-update")); got != "sf.geolocation-db-update.UVCVIku.lock" {
		t.Errorf("got %s", got)
	}
}

func TestFlockLocks(t *testing.T) {
	ctx := context.Background()
	f := NewFlockFactory(t.TempDir())
	first := f.CreateLock("visit:locate", time.Minute)
	if ok, err := first.Acquire(ctx, false); !ok || err != nil {
		t.Fatalf("first acquire: %v %v", ok, err)
	}
	second := f.CreateLock("visit:locate", time.Minute)
	if ok, err := second.Acquire(ctx, false); ok || err != nil {
		t.Fatalf("second acquire while held: %v %v", ok, err)
	}

	acquired := make(chan struct{})
	go func() {
		release, err := f.AcquireBlocking(ctx, "visit:locate", time.Minute)
		if err != nil {
			t.Error(err)
			return
		}
		close(acquired)
		release()
	}()
	select {
	case <-acquired:
		t.Fatal("blocking acquire did not wait")
	case <-time.After(100 * time.Millisecond):
	}
	if err := first.Release(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("blocking acquire did not get the released lock")
	}

	cancelled, cancel := context.WithCancel(ctx)
	hold, _ := f.AcquireBlocking(ctx, "tag_foo", 5*time.Second)
	cancel()
	if _, err := f.AcquireBlocking(cancelled, "tag_foo", 5*time.Second); err == nil {
		t.Fatal("blocking acquire ignored the cancelled context")
	}
	hold()
	if n := len(f.store.(*flockStore).process.entries); n != 0 {
		t.Fatalf("%d process lock entries left", n)
	}
}

func TestNamespacedResource(t *testing.T) {
	ns := "Shlink"
	s := &redisStore{namespace: &ns}
	for in, want := range map[string]string{"tag_foo": "Shlink:tag_foo", "Shlink:db:create": "Shlink:db:create"} {
		if got := s.resource(&key{resource: in}); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
	if got := (&redisStore{}).resource(&key{resource: "tag_foo"}); got != "tag_foo" {
		t.Errorf("no namespace: got %s", got)
	}
}
