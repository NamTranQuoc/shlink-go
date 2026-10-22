package harness

import (
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// LongURLWithTimeoutPath mirrors the `long_url_with_timeout` route upstream adds in config/test/test_config.global.php.
// The target cannot serve that route here, so the test process serves it from its helper server.
const LongURLWithTimeoutPath = "/api-tests/long-url-with-timeout"

// MatomoTrackingPath is where the integrations profile points MATOMO_BASE_URL's tracker endpoint.
const MatomoTrackingPath = "/matomo.php"

const helperListenAddr = ":19999"

var (
	helperOnce sync.Once
	helperErr  error

	matomoMu       sync.Mutex
	matomoRequests []MatomoRequest
)

// MatomoRequest is a tracking request the target sent to the helper server.
type MatomoRequest struct {
	Method      string
	Query       string
	UserAgent   string
	ContentType string
	Body        string
}

// TakeMatomoRequests returns the tracking requests received since the previous call and forgets them.
func TakeMatomoRequests() []MatomoRequest {
	matomoMu.Lock()
	defer matomoMu.Unlock()
	out := matomoRequests
	matomoRequests = nil
	return out
}

// HelperURL starts the helper server once per test binary and returns the base URL under which the target reaches it
// (CONTRACT_HELPER_URL).
func HelperURL(t testing.TB) string {
	t.Helper()
	helperOnce.Do(func() {
		mux := http.NewServeMux()
		mux.HandleFunc("GET "+LongURLWithTimeoutPath, func(w http.ResponseWriter, r *http.Request) {
			// Title resolution times out at 3 seconds.
			select {
			case <-time.After(5 * time.Second):
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<title>The title</title>"))
		})
		mux.HandleFunc(MatomoTrackingPath, func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			matomoMu.Lock()
			matomoRequests = append(matomoRequests, MatomoRequest{
				Method:      r.Method,
				Query:       r.URL.RawQuery,
				UserAgent:   r.Header.Get("User-Agent"),
				ContentType: r.Header.Get("Content-Type"),
				Body:        string(body),
			})
			matomoMu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		})
		registerImportSources(mux)
		ln, err := net.Listen("tcp", helperListenAddr)
		if err != nil {
			helperErr = err
			return
		}
		go func() {
			if err := http.Serve(ln, mux); err != nil && !errors.Is(err, net.ErrClosed) {
				panic(err)
			}
		}()
	})
	if helperErr != nil {
		t.Fatalf("start helper server on %s: %v", helperListenAddr, helperErr)
	}
	return LoadConfig().HelperURL
}

// LongURLWithTimeout returns a URL whose response takes longer than Shlink's title resolution timeout.
func LongURLWithTimeout(t testing.TB) string {
	t.Helper()
	return HelperURL(t) + LongURLWithTimeoutPath
}
