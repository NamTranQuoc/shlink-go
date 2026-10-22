package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// Not covered by upstream's API tests, which run without a Mercure hub: the endpoint reports the missing integration.
func TestMercureInfo_errorIsReturnedWhenMercureIsNotConfigured(t *testing.T) {
	h.Seed(t)
	resp := h.CallAPIWithKey(t, http.MethodGet, "/rest/v3/mercure-info")

	h.AssertStatus(t, resp, http.StatusNotImplemented)
	h.AssertEquals(t, "application/problem+json", resp.Header.Get("Content-Type"))
	h.AssertEquals(t, map[string]any{
		"title":  "Mercure integration not configured",
		"type":   "https://shlink.io/api/error/mercure-not-configured",
		"status": float64(501),
		"detail": "This Shlink instance is not integrated with a mercure hub.",
	}, resp.JSON(t))
}
