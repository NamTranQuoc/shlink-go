package rest

import (
	"fmt"
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestDomainVisits_expectedVisitsAreReturned(t *testing.T) {
	cases := []struct {
		name, apiKey, domain string
		excludeBots          bool
		expectedVisitsAmount int
	}{
		{"example.com with admin API key", "valid_api_key", "example.com", false, 0},
		{"DEFAULT with admin API key", "valid_api_key", "DEFAULT", false, 7},
		{"DEFAULT with admin API key and no bots", "valid_api_key", "DEFAULT", true, 6},
		{"DEFAULT with domain API key", "domain_api_key", "DEFAULT", false, 0},
		{"DEFAULT with author API key", "author_api_key", "DEFAULT", false, 5},
		{"DEFAULT with author API key and no bots", "author_api_key", "DEFAULT", true, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			query := h.Q()
			if c.excludeBots {
				query = h.Q("excludeBots", true)
			}

			resp := h.CallAPIWithKeyNamed(t, http.MethodGet, fmt.Sprintf("/domains/%s/visits", c.domain), c.apiKey,
				h.Options{Query: query})
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusOK, resp.StatusCode)
			h.AssertHasKey(t, payload, "visits")
			visits, _ := payload["visits"].(map[string]any)
			h.AssertHasKey(t, visits, "data")
			h.AssertCount(t, c.expectedVisitsAmount, visits["data"])
		})
	}
}

func TestDomainVisits_notFoundErrorIsReturnedForInvalidTags(t *testing.T) {
	cases := []struct{ name, apiKey, domain string }{
		{"admin API key with invalid domain", "valid_api_key", "invalid_domain.com"},
		{"domain API key with not-owned valid domain", "domain_api_key", "this_domain_is_detached.com"},
		{"author API key with valid domain not used in URLs", "author_api_key", "this_domain_is_detached.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodGet, fmt.Sprintf("/domains/%s/visits", c.domain), c.apiKey)
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusNotFound, resp.StatusCode)
			h.AssertEquals(t, http.StatusNotFound, payload["status"])
			h.AssertEquals(t, "https://shlink.io/api/error/domain-not-found", payload["type"])
			h.AssertEquals(t, fmt.Sprintf(`Domain with authority "%s" could not be found`, c.domain), payload["detail"])
			h.AssertEquals(t, "Domain not found", payload["title"])
			h.AssertEquals(t, c.domain, payload["authority"])
		})
	}
}

func TestDomainVisits_expectedNotFoundTypeIsReturnedForApiVersion(t *testing.T) {
	cases := []struct{ version, expectedType string }{
		{"1", "https://shlink.io/api/error/domain-not-found"},
		{"2", "https://shlink.io/api/error/domain-not-found"},
		{"3", "https://shlink.io/api/error/domain-not-found"},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKey(t, http.MethodGet, "/rest/v"+c.version+"/domains/invalid.com/visits")
			payload := resp.JSON(t)

			h.AssertEquals(t, c.expectedType, payload["type"])
		})
	}
}
