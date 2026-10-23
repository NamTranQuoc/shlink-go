package geolocation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/db"
	"github.com/NamTranQuoc/shlink-go/internal/integration/lock"
)

func newTestStore(t *testing.T) *core.Store {
	t.Helper()
	d, err := db.Open(db.Params{Driver: db.SQLite, SQLitePath: filepath.Join(t.TempDir(), "db.sqlite")}, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CreateSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return core.NewStore(d)
}

func fakeArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "GeoLite2-City_1/GeoLite2-City.mmdb", Mode: 0o644, Size: 4})
	_, _ = tw.Write([]byte("fake"))
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// TestCheckDbUpdate walks GeolocationDbUpdater's decision table: first download, up to date, failed attempts, and
// an old successful download.
func TestCheckDbUpdate(t *testing.T) {
	ctx := context.Background()
	failing := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if failing {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(fakeArchive(t))
	}))
	defer server.Close()

	dir := t.TempDir()
	key := "fake-license"
	o := NewOptions(&key, filepath.Join(dir, "GeoLite2-City.mmdb"), filepath.Join(dir, "tmp"))
	o.DownloadFrom = server.URL
	store := newTestStore(t)
	now := time.Now()
	u := &GeolocationDbUpdater{
		Updater: NewDbUpdater(o), Store: store, Tracking: config.TrackingOptions{}, Locks: lock.NewFlockFactory(filepath.Join(dir, "locks")),
		Now: func() time.Time { return now },
	}

	if r, err := u.CheckDbUpdate(ctx, nil); err != nil || r != DBCreated {
		t.Fatalf("first check: %v %v", r, err)
	}
	if b, _ := os.ReadFile(o.DBLocation); string(b) != "fake" {
		t.Fatalf("db not copied: %q", b)
	}
	if r, err := u.CheckDbUpdate(ctx, nil); err != nil || r != DBIsUpToDate {
		t.Fatalf("second check: %v %v", r, err)
	}

	now = now.AddDate(0, 0, 31)
	failing = true
	if _, err := u.CheckDbUpdate(ctx, nil); err == nil {
		t.Fatal("expected a failure")
	} else if f, ok := err.(*DbUpdateFailedError); !ok || !f.OlderDBExists {
		t.Fatalf("unexpected error %#v", err)
	}
	fsID, _ := currentFilesystemID()
	updates, _ := store.RecentGeolocationDbUpdates(ctx, fsID, 15)
	if len(updates) != 2 || updates[0].Status != core.GeolocationUpdateError || updates[0].Reason != "Last successful attempt is old enough" ||
		*updates[0].Error != "An error occurred while updating geolocation database, but an older DB is already present. "+
			"Prev: An error occurred while trying to download a fresh copy of the GeoLite2 database" {
		t.Fatalf("unexpected tracked updates %+v", updates[0])
	}

	failing = false
	now = now.Add(time.Minute)
	if r, err := u.CheckDbUpdate(ctx, nil); err != nil || r != DBUpdated {
		t.Fatalf("retry after error: %v %v", r, err)
	}

	u.Tracking.DisableTracking = true
	if r, _ := u.CheckDbUpdate(ctx, nil); r != CheckSkipped {
		t.Fatalf("expected skip, got %v", r)
	}
}

// TestDownloadFollowsGuzzleRedirectLimit pins Guzzle's default of at most 5 redirects.
func TestDownloadFollowsGuzzleRedirectLimit(t *testing.T) {
	var hops int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var n int
		_, _ = fmt.Sscanf(r.URL.Path, "/%d", &n)
		if n < hops {
			http.Redirect(w, r, fmt.Sprintf("/%d", n+1), http.StatusFound)
			return
		}
		_, _ = w.Write(fakeArchive(t))
	}))
	defer server.Close()

	key := "fake-license"
	for _, tc := range []struct {
		hops int
		ok   bool
	}{{5, true}, {6, false}} {
		hops = tc.hops
		dir := t.TempDir()
		o := NewOptions(&key, filepath.Join(dir, "GeoLite2-City.mmdb"), filepath.Join(dir, "tmp"))
		o.DownloadFrom = server.URL + "/0"
		err := NewDbUpdater(o).DownloadFreshCopy(context.Background(), nil)
		if (err == nil) != tc.ok {
			t.Fatalf("%d redirects: err = %v", tc.hops, err)
		}
	}
}
