package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestListDomains_domainsAreProperlyListed(t *testing.T) {
	noRedirects := h.M{"baseUrlRedirect": nil, "regular404Redirect": nil, "invalidShortUrlRedirect": nil}
	defaultDomain := h.M{"domain": "s.test", "isDefault": true, "redirects": noRedirects}
	exampleDomain := h.M{"domain": "example.com", "isDefault": false, "redirects": noRedirects}
	cases := []struct {
		name, apiKey    string
		expectedDomains h.L
	}{
		{"admin API key", "valid_api_key", h.L{
			defaultDomain,
			h.M{"domain": "detached-with-redirects.com", "isDefault": false, "redirects": h.M{
				"baseUrlRedirect": "foo.com", "regular404Redirect": "bar.com", "invalidShortUrlRedirect": nil,
			}},
			exampleDomain,
			h.M{"domain": "some-domain.com", "isDefault": false, "redirects": noRedirects},
		}},
		{"author API key", "author_api_key", h.L{defaultDomain}},
		{"domain API key", "domain_api_key", h.L{exampleDomain}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodGet, "/domains", c.apiKey)
			respPayload := resp.JSON(t)

			h.AssertEquals(t, http.StatusOK, resp.StatusCode)
			h.AssertEquals(t, h.M{
				"domains": h.M{
					"data":             c.expectedDomains,
					"defaultRedirects": noRedirects,
				},
			}, respPayload)
		})
	}
}
