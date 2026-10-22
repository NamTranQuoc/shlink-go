package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestRequestId_generatesRequestId(t *testing.T) {
	h.Seed(t)

	resp := h.CallAPI(t, http.MethodGet, "/health")
	assertHasHeader(t, resp, "X-Request-Id")
}

func TestRequestId_keepsProvidedRequestId(t *testing.T) {
	h.Seed(t)

	resp := h.CallAPI(t, http.MethodGet, "/health", h.Options{
		Headers: http.Header{"X-Request-Id": {"foobar"}},
	})
	// Upstream asserts assertEquals('foobar', $response->hasHeader('X-Request-Id')), which with PHP's loose comparison
	// only checks that the header is present.
	assertHasHeader(t, resp, "X-Request-Id")
}
