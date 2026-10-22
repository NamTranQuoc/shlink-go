package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestHealthReturnsPass(t *testing.T) {
	for _, path := range []string{"/rest/health", "/rest/v1/health", "/rest/v2/health", "/rest/v3/health"} {
		resp := h.Request(t, http.MethodGet, path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, resp.StatusCode)
		}
		body := resp.JSON(t)
		if body["status"] != "pass" || body["version"] != "5.1.8" {
			t.Fatalf("%s: unexpected body %v", path, body)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/health+json" {
			t.Fatalf("%s: unexpected content type %q", path, ct)
		}
	}
}

func TestSeedFixtures(t *testing.T) {
	h.Seed(t)
	resp := h.CallAPIWithKey(t, http.MethodGet, "/short-urls")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
	}
}
