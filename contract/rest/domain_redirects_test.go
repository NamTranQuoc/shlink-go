package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestDomainRedirects_anErrorIsReturnedWhenTryingToEditAnInvalidDomain(t *testing.T) {
	cases := []struct {
		name    string
		request any
	}{
		// PHP's json_encode([]) is "[]", which is what Guzzle sends for an empty payload.
		{"no domain", h.L{}},
		{"empty domain", h.M{"domain": ""}},
		{"null domain", h.M{"domain": nil}},
		{"invalid domain", h.M{"domain": "not a domain"}},
		{"invalid IP", h.M{"domain": "540.38.386.0"}},
		{"invalid port", h.M{"domain": "example.com:888888"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKey(t, http.MethodPatch, "/domains/redirects", h.Options{JSON: c.request})
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusBadRequest, resp.StatusCode)
			h.AssertEquals(t, http.StatusBadRequest, payload["status"])
			h.AssertEquals(t, "https://shlink.io/api/error/invalid-data", payload["type"])
			h.AssertEquals(t, "Provided data is not valid", payload["detail"])
			h.AssertEquals(t, "Invalid data", payload["title"])
		})
	}
}

func TestDomainRedirects_allowsToEditDomainRedirects(t *testing.T) {
	cases := []struct {
		name                      string
		request, expectedResponse h.M
	}{
		{"new domain",
			h.M{"domain": "my-new-domain.com", "regular404Redirect": "foo.com"},
			h.M{"baseUrlRedirect": nil, "regular404Redirect": "foo.com", "invalidShortUrlRedirect": nil}},
		{"default domain",
			h.M{"domain": "s.test", "regular404Redirect": "foo-for-default.com"},
			h.M{"baseUrlRedirect": nil, "regular404Redirect": "foo-for-default.com", "invalidShortUrlRedirect": nil}},
		{"existing domain with redirects",
			h.M{"domain": "detached-with-redirects.com", "baseUrlRedirect": nil, "invalidShortUrlRedirect": "foo.com"},
			h.M{"baseUrlRedirect": nil, "regular404Redirect": "bar.com", "invalidShortUrlRedirect": "foo.com"}},
		{"existing domain with no redirects",
			h.M{"domain": "example.com", "baseUrlRedirect": nil, "invalidShortUrlRedirect": "foo.com"},
			h.M{"baseUrlRedirect": nil, "regular404Redirect": nil, "invalidShortUrlRedirect": "foo.com"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKey(t, http.MethodPatch, "/domains/redirects", h.Options{JSON: c.request})
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusOK, resp.StatusCode)
			h.AssertEquals(t, c.expectedResponse, payload)
		})
	}
}
