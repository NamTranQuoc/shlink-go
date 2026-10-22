package rest

import (
	"net/http"
	"strings"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func assertNoHeader(t *testing.T, resp *h.Response, name string) {
	t.Helper()
	if _, ok := resp.Header[http.CanonicalHeaderKey(name)]; ok {
		t.Fatalf("expected no %s header, got %q", name, resp.Header.Get(name))
	}
}

func assertHasHeader(t *testing.T, resp *h.Response, name string) {
	t.Helper()
	if _, ok := resp.Header[http.CanonicalHeaderKey(name)]; !ok {
		t.Fatalf("expected %s header, got headers %v", name, resp.Header)
	}
}

func TestCors_responseDoesNotIncludeCorsHeadersWhenOriginIsNotSent(t *testing.T) {
	h.Seed(t)

	resp := h.CallAPIWithKey(t, http.MethodGet, "/short-urls")

	h.AssertEquals(t, 200, resp.StatusCode)
	assertNoHeader(t, resp, "Access-Control-Allow-Origin")
	assertNoHeader(t, resp, "Access-Control-Allow-Methods")
	assertNoHeader(t, resp, "Access-Control-Max-Age")
	assertNoHeader(t, resp, "Access-Control-Allow-Headers")
	assertNoHeader(t, resp, "Access-Control-Allow-Credentials")
}

func TestCors_responseIncludesCorsHeadersIfOriginIsSent(t *testing.T) {
	cases := []struct {
		name, origin, endpoint string
		expectedStatusCode     int
	}{
		{"foo.com", "foo.com", "/short-urls", 200},
		{"bar.io", "bar.io", "/foo/bar", 404},
		{"baz.dev", "baz.dev", "/short-urls", 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKey(t, http.MethodGet, c.endpoint, h.Options{
				Headers: http.Header{"Origin": {c.origin}},
			})

			h.AssertEquals(t, c.expectedStatusCode, resp.StatusCode)
			h.AssertEquals(t, "*", headerLine(resp, "Access-Control-Allow-Origin"))
			assertNoHeader(t, resp, "Access-Control-Allow-Methods")
			assertNoHeader(t, resp, "Access-Control-Max-Age")
			assertNoHeader(t, resp, "Access-Control-Allow-Headers")
			assertNoHeader(t, resp, "Access-Control-Allow-Credentials")
		})
	}
}

func TestCors_preflightRequestsIncludeExtraCorsHeaders(t *testing.T) {
	cases := []struct{ name, endpoint, expectedAllowedMethods string }{
		{"invalid route", "/foo/bar", "GET,POST,PUT,PATCH,DELETE"},
		{"short URLs route", "/short-urls", "GET,POST"},
		{"tags route", "/tags", "GET,DELETE,PUT"},
		{"health route", "/health", "GET"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			allowedHeaders := "Authorization"

			resp := h.CallAPIWithKey(t, http.MethodOptions, c.endpoint, h.Options{
				Headers: http.Header{
					"Origin":                         {"foo.com"},
					"Access-Control-Request-Headers": {allowedHeaders},
				},
			})

			h.AssertEquals(t, 204, resp.StatusCode)
			assertHasHeader(t, resp, "Access-Control-Allow-Origin")
			assertHasHeader(t, resp, "Access-Control-Max-Age")
			h.AssertEquals(t, c.expectedAllowedMethods, headerLine(resp, "Access-Control-Allow-Methods"))
			h.AssertEquals(t, allowedHeaders, headerLine(resp, "Access-Control-Allow-Headers"))
		})
	}
}

// headerLine mirrors PSR-7's getHeaderLine: all values of a header joined with ", ".
func headerLine(resp *h.Response, name string) string {
	return strings.Join(resp.Header.Values(name), ", ")
}
