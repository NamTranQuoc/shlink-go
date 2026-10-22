// Package geolocation ports Shlink's IP geolocation: shlink-ip-geolocation's GeoLite2 options, db updater, file
// extractor and location resolver, plus Shlink's GeolocationDbUpdater, VisitLocator and the related event listeners.
package geolocation

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/core"
)

const (
	dbCompressedFile    = "GeoLite2-City.tar.gz"
	dbDecompressedFile  = "GeoLite2-City.mmdb"
	downloadFromPattern = "https://download.maxmind.com/app/geoip_download" +
		"?edition_id=GeoLite2-City&license_key={license_key}&suffix=tar.gz"
)

// Options ports GeoLite2Options, configured like config/autoload/geolite2.global.php.
type Options struct {
	LicenseKey        *string
	DBLocation        string
	TempDir           string
	ConnectionTimeout time.Duration
	DownloadFrom      string
}

// OptionsFromEnv ports config/autoload/geolite2.global.php, with paths relative to the working dir (/etc/shlink).
func OptionsFromEnv() Options {
	return NewOptions(config.GeoliteLicenseKey.Load().StringOrNil(), "data/GeoLite2-City.mmdb", "data/temp-geolite")
}

// NewOptions builds options, deriving the download URL from the license key.
func NewOptions(licenseKey *string, dbLocation, tempDir string) Options {
	key := ""
	if licenseKey != nil {
		key = *licenseKey
	}
	return Options{
		LicenseKey: licenseKey, DBLocation: dbLocation, TempDir: tempDir, ConnectionTimeout: 15 * time.Second,
		DownloadFrom: strings.ReplaceAll(downloadFromPattern, "{license_key}", key),
	}
}

// HasLicenseKey ports GeoLite2Options::hasLicenseKey().
func (o Options) HasLicenseKey() bool {
	return o.LicenseKey != nil && *o.LicenseKey != "" && *o.LicenseKey != "0"
}

// ErrMissingLicense ports MissingLicenseException.
var ErrMissingLicense = errors.New("Impossible to download GeoLite2 db file. A license key was not provided.")

// DbUpdateError ports DbUpdateException.
type DbUpdateError struct {
	Message string
	Prev    error
}

func (e *DbUpdateError) Error() string { return e.Message }
func (e *DbUpdateError) Unwrap() error { return e.Prev }

// DbUpdater ports shlink-ip-geolocation's GeoLite2\DbUpdater.
type DbUpdater struct {
	Options Options
	Client  *http.Client
}

// NewDbUpdater creates an updater whose HTTP client honors the connection timeout.
func NewDbUpdater(o Options) *DbUpdater {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&netDialer{timeout: o.ConnectionTimeout}).DialContext
	return &DbUpdater{Options: o, Client: &http.Client{Transport: transport}}
}

// DatabaseFileExists ports DbUpdater::databaseFileExists().
func (u *DbUpdater) DatabaseFileExists() bool {
	_, err := os.Stat(u.Options.DBLocation)
	return err == nil
}

// DownloadFreshCopy ports DbUpdater::downloadFreshCopy(). progress receives the total and downloaded bytes.
func (u *DbUpdater) DownloadFreshCopy(ctx context.Context, progress func(total, downloaded int64)) error {
	if !u.Options.HasLicenseKey() {
		return ErrMissingLicense
	}
	compressed := filepath.Join(u.Options.TempDir, dbCompressedFile)
	if err := u.download(ctx, compressed, progress); err != nil {
		return &DbUpdateError{"An error occurred while trying to download a fresh copy of the GeoLite2 database", err}
	}
	extracted, err := ExtractFile(compressed, dbDecompressedFile, u.Options.TempDir)
	if err != nil {
		return &DbUpdateError{fmt.Sprintf("An error occurred while trying to extract the GeoLite2 database from %s",
			compressed), err}
	}
	if err := copyFile(extracted, u.Options.DBLocation); err != nil {
		return &DbUpdateError{fmt.Sprintf("An error occurred while trying to copy GeoLite2 db file to %s folder",
			u.Options.DBLocation), err}
	}
	_ = os.Remove(compressed)
	_ = os.Remove(extracted)
	return nil
}

type progressWriter struct {
	total, done int64
	fn          func(total, downloaded int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.fn != nil {
		p.fn(p.total, p.done)
	}
	return len(b), nil
}

func (u *DbUpdater) download(ctx context.Context, dest string, progress func(total, downloaded int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.Options.DownloadFrom, nil)
	if err != nil {
		return err
	}
	resp, err := u.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Guzzle's http_errors option makes 4xx/5xx responses fail
	if resp.StatusCode >= 400 {
		return fmt.Errorf("server responded with status %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	pw := &progressWriter{total: resp.ContentLength, fn: progress}
	if pw.total < 0 {
		pw.total = 0
	}
	_, err = io.Copy(io.MultiWriter(f, pw), resp.Body)
	return err
}

// ExtractFile ports MemoryEfficientFileExtractor::extractFile(): the first tar entry whose name ends with
// fileToExtract is written to destinationDir.
func ExtractFile(compressedPath, fileToExtract, destinationDir string) (string, error) {
	in, err := os.Open(compressedPath)
	if err != nil {
		return "", fmt.Errorf(`It was not possible to open file "%s"`, compressedPath)
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return "", fmt.Errorf(`It was not possible to open file "%s"`, compressedPath)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if !strings.HasSuffix(h.Name, fileToExtract) {
			continue
		}
		output := filepath.Join(destinationDir, fileToExtract)
		out, err := os.Create(output)
		if err != nil {
			return "", fmt.Errorf(`It was not possible to open destination dir "%s"`, destinationDir)
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return "", err
		}
		return output, out.Close()
	}
	return "", fmt.Errorf(`File "%s" not found inside compressed file "%s"`, fileToExtract, compressedPath)
}

// copyFile ports Filesystem::copy() plus chmod 0666, writing through a temp file so readers never see a partial db.
func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := to + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o666); err != nil {
		return err
	}
	return os.Rename(tmp, to)
}

// WrongIPError ports WrongIpException.
type WrongIPError struct {
	Message string
	Prev    error
}

func (e *WrongIPError) Error() string { return e.Message }
func (e *WrongIPError) Unwrap() error { return e.Prev }

// cityRecord holds the GeoLite2 City fields GeoLite2LocationResolver maps, in English like geoip2's default locale.
type cityRecord struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Location struct {
		Latitude  *float64 `maxminddb:"latitude"`
		Longitude *float64 `maxminddb:"longitude"`
		TimeZone  string   `maxminddb:"time_zone"`
	} `maxminddb:"location"`
}

// Resolver ports GeoLite2LocationResolver over the configured db file. Upstream opens the file on every lookup; this
// keeps it open and reopens it when the file changes (e.g. after an update).
type Resolver struct {
	Updater *DbUpdater

	mu      sync.Mutex
	reader  *maxminddb.Reader
	modTime time.Time
	size    int64
}

// DatabaseFileExists implements web.GeoResolver.
func (r *Resolver) DatabaseFileExists() bool { return r.Updater.DatabaseFileExists() }

func (r *Resolver) currentReader() (*maxminddb.Reader, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	info, err := os.Stat(r.Updater.Options.DBLocation)
	if err != nil {
		return nil, err
	}
	if r.reader != nil && info.ModTime().Equal(r.modTime) && info.Size() == r.size {
		return r.reader, nil
	}
	reader, err := maxminddb.Open(r.Updater.Options.DBLocation)
	if err != nil {
		return nil, &WrongIPError{Message: "Provided GeoLite2 db file is invalid", Prev: err}
	}
	// The previous reader may still be used by in-flight lookups; let the GC unmap it.
	r.reader, r.modTime, r.size = reader, info.ModTime(), info.Size()
	return reader, nil
}

// ResolveIPLocation implements web.GeoResolver, porting GeoLite2LocationResolver::resolveIpLocation().
func (r *Resolver) ResolveIPLocation(ip string) (*core.Location, error) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil, fmt.Errorf(`The value "%s" is not a valid IP address.`, ip)
	}
	reader, err := r.currentReader()
	if err != nil {
		return nil, err
	}
	result := reader.Lookup(addr)
	if err := result.Err(); err != nil {
		return nil, &WrongIPError{Message: "Provided GeoLite2 db file is invalid", Prev: err}
	}
	if !result.Found() {
		return nil, &WrongIPError{Message: fmt.Sprintf(`Provided IP "%s" is invalid`, ip),
			Prev: fmt.Errorf(`The address %s is not in the database.`, ip)}
	}
	var rec cityRecord
	if err := result.Decode(&rec); err != nil {
		return nil, &WrongIPError{Message: "Provided GeoLite2 db file is invalid", Prev: err}
	}
	loc := &core.Location{
		CountryCode: rec.Country.ISOCode,
		CountryName: rec.Country.Names["en"],
		City:        rec.City.Names["en"],
		TimeZone:    rec.Location.TimeZone,
	}
	if len(rec.Subdivisions) > 0 {
		loc.RegionName = rec.Subdivisions[0].Names["en"]
	}
	if rec.Location.Latitude != nil {
		loc.Latitude = *rec.Location.Latitude
	}
	if rec.Location.Longitude != nil {
		loc.Longitude = *rec.Location.Longitude
	}
	return loc, nil
}
