package matomo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NamTranQuoc/shlink-go/internal/core"
)

// The expected request was produced by MatomoTrackerBuilder + MatomoTracker::getUrlTrackPageView() in the
// shlinkio/shlink:5.1.8 image, with the same tracked values; random parts (r, _idts, _id) are pinned here.
func TestPageViewRequestMatchesUpstream(t *testing.T) {
	tr := &tracker{
		siteID: 3, apiURL: "https://matomo.example.com/", tokenAuth: "fake-token", createTs: 1792670229,
		visitorID: "f78347dc69ef0ae1", pageURL: "https://s.test/abc", urlReferrer: "https://ref.example/a b?x=1&y=ñ",
		userAgent: "Mozilla/5.0 (X11)", ip: "1.2.3.4", forcedDatetime: "2024-03-04 05:06:07", country: "br",
		city: "Rio de Janeiro", lat: -22.9068, long: -43.1729,
		customParams: [][2]string{{"type", "valid_short_url"}, {"orphan", "true"}},
	}
	endpoint, body := tr.pageViewRequest("Título & más", "", "633269")
	want := "idsite=3&rec=1&apiv=1&r=633269&cip=1.2.3.4&cdt=2024-03-04+05%3A06%3A07&_idts=1792670229&_id=f78347dc69ef0ae1" +
		"&url=https%3A%2F%2Fs.test%2Fabc&urlref=https%3A%2F%2Fref.example%2Fa+b%3Fx%3D1%26y%3D%C3%B1&country=br" +
		"&city=Rio+de+Janeiro&lat=-22.9068&long=-43.1729&type=valid_short_url&orphan=true" +
		"&action_name=T%C3%ADtulo+%26+m%C3%A1s&token_auth=fake-token"
	if endpoint != "https://matomo.example.com/matomo.php" {
		t.Errorf("endpoint: %s", endpoint)
	}
	if body != want {
		t.Errorf("body:\n got %s\nwant %s", body, want)
	}
}

// TestRedirectsArePostedAgain pins curl's CURL_REDIR_POST_ALL with its default limit of 30 redirects.
func TestRedirectsArePostedAgain(t *testing.T) {
	var final *http.Request
	var finalBody string
	limit := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var n int
		_, _ = fmt.Sscanf(r.URL.Path, "/%d", &n)
		if n < limit {
			http.Redirect(w, r, fmt.Sprintf("/%d", n+1), []int{301, 302, 303, 307, 308}[n%5])
			return
		}
		b, _ := io.ReadAll(r.Body)
		final, finalBody = r, string(b)
	}))
	defer srv.Close()
	s := NewVisitSender(Options{}, nil, core.ShortURLStringifier{})

	limit = 30
	if err := s.post(context.Background(), srv.URL+"/0", "idsite=3", "agent"); err != nil {
		t.Fatal(err)
	}
	if final.Method != http.MethodPost || finalBody != "idsite=3" || final.URL.Path != "/30" ||
		final.Header.Get("User-Agent") != "agent" {
		t.Fatalf("final request: %s %s %q %q", final.Method, final.URL.Path, finalBody, final.Header.Get("User-Agent"))
	}
	limit = 31
	if err := s.post(context.Background(), srv.URL+"/0", "idsite=3", "agent"); err == nil ||
		err.Error() != "Maximum (30) redirects followed" {
		t.Fatalf("31 redirects: %v", err)
	}
}
