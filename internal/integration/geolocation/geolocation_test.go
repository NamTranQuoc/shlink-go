package geolocation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/NamTranQuoc/shlink-go/internal/core"
)

// TestResolverMatchesUpstream compares with GeoLite2LocationResolver run in the shlinkio/shlink:5.1.8 image over
// MaxMind's GeoIP2-City-Test.mmdb (github.com/maxmind/MaxMind-DB test-data). The file is not committed; point
// SHLINK_GEOIP_TEST_DB at a local copy to run it.
func TestResolverMatchesUpstream(t *testing.T) {
	file := os.Getenv("SHLINK_GEOIP_TEST_DB")
	if file == "" {
		t.Skip("SHLINK_GEOIP_TEST_DB not set")
	}
	r := &Resolver{Updater: &DbUpdater{Options: NewOptions(nil, file, t.TempDir())}}
	cases := map[string]core.Location{
		"81.2.69.142":   {CountryCode: "GB", CountryName: "United Kingdom", RegionName: "England", City: "London", Latitude: 51.5142, Longitude: -0.0931, TimeZone: "Europe/London"},
		"89.160.20.112": {CountryCode: "SE", CountryName: "Sweden", RegionName: "Östergötland County", City: "Linköping", Latitude: 58.4167, Longitude: 15.6167, TimeZone: "Europe/Stockholm"},
		"2001:218::":    {CountryCode: "JP", CountryName: "Japan", RegionName: "", City: "", Latitude: 35.68536, Longitude: 139.75309, TimeZone: "Asia/Tokyo"},
		"175.16.199.0":  {CountryCode: "CN", CountryName: "China", RegionName: "Jilin Sheng", City: "Changchun", Latitude: 43.88, Longitude: 125.3228, TimeZone: "Asia/Harbin"},
	}
	for ip, want := range cases {
		got, err := r.ResolveIPLocation(ip)
		if err != nil || *got != want {
			t.Errorf("%s: got %+v (%v), want %+v", ip, got, err, want)
		}
	}
	var wrong *WrongIPError
	if _, err := r.ResolveIPLocation("1.1.1.1"); !errors.As(err, &wrong) || err.Error() != `Provided IP "1.1.1.1" is invalid` {
		t.Errorf("unexpected error for unknown IP: %v", err)
	}
	if _, err := r.ResolveIPLocation("nope"); err == nil || err.Error() != `The value "nope" is not a valid IP address.` {
		t.Errorf("unexpected error for invalid IP: %v", err)
	}
}

func TestExtractFile(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range map[string]string{
		"GeoLite2-City_20240101/LICENSE.txt":        "license",
		"GeoLite2-City_20240101/GeoLite2-City.mmdb": "fake-db",
	} {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))})
		_, _ = tw.Write([]byte(content))
	}
	_ = tw.Close()
	_ = gz.Close()
	compressed := filepath.Join(dir, "GeoLite2-City.tar.gz")
	if err := os.WriteFile(compressed, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := ExtractFile(compressed, "GeoLite2-City.mmdb", dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != "fake-db" {
		t.Errorf("unexpected content %q", b)
	}
	if _, err := ExtractFile(compressed, "missing.mmdb", dir); err == nil ||
		err.Error() != `File "missing.mmdb" not found inside compressed file "`+compressed+`"` {
		t.Errorf("unexpected error %v", err)
	}
}

func TestLockFileName(t *testing.T) {
	// Name produced by Symfony's FlockStore for the key used by GeolocationDbUpdater
	l := &Locks{Dir: "data/locks"}
	if got := filepath.Base(l.lockFile("geolocation-db-update")); got != lockNameFromSymfony {
		t.Errorf("got %s, want %s", got, lockNameFromSymfony)
	}
}

const lockNameFromSymfony = "sf.geolocation-db-update.UVCVIku.lock"
