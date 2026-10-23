package importer

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientDoesNotFollowRedirects(t *testing.T) {
	followed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			followed = true
			_, _ = w.Write([]byte(`{"links":[]}`))
			return
		}
		http.Redirect(w, r, "/target", http.StatusFound)
	}))
	defer srv.Close()

	api := &apiConsumer{client: NewHTTPClient()}
	if _, err := api.callAPI(srv.URL + "/start"); err == nil {
		t.Fatal("expected the empty 302 body to fail JSON decoding")
	}
	if followed {
		t.Fatal("the redirect was followed")
	}
}
