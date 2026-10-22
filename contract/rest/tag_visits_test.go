package rest

import (
	"fmt"
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestTagVisits_expectedVisitsAreReturned(t *testing.T) {
	cases := []struct {
		name, apiKey, tag    string
		query                h.Query
		expectedVisitsAmount int
	}{
		{"foo with admin API key", "valid_api_key", "foo", h.Q(), 5},
		{"foo with admin API key and no bots", "valid_api_key", "foo", h.Q("excludeBots", true), 4},
		{"bar with admin API key", "valid_api_key", "bar", h.Q(), 2},
		{"bar with admin API key and no bots", "valid_api_key", "bar", h.Q("excludeBots", true), 1},
		{"baz with admin API key", "valid_api_key", "baz", h.Q(), 0},
		{"foo with author API key", "author_api_key", "foo", h.Q(), 5},
		{"foo with author API key and no bots", "author_api_key", "foo", h.Q("excludeBots", true), 4},
		{"bar with author API key", "author_api_key", "bar", h.Q(), 2},
		{"bar with author API key and no bots", "author_api_key", "bar", h.Q("excludeBots", true), 1},
		{"foo with domain API key", "domain_api_key", "foo", h.Q(), 0},
		{"foo with specific domain", "valid_api_key", "foo", h.Q("domain", "example.com"), 0},
		{"foo with default domain", "valid_api_key", "foo", h.Q("domain", "DEFAULT"), 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodGet, fmt.Sprintf("/tags/%s/visits", c.tag), c.apiKey,
				h.Options{Query: c.query})
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusOK, resp.StatusCode)
			h.AssertHasKey(t, payload, "visits")
			visits, _ := payload["visits"].(map[string]any)
			h.AssertHasKey(t, visits, "data")
			h.AssertCount(t, c.expectedVisitsAmount, visits["data"])
		})
	}
}

func TestTagVisits_notFoundErrorIsReturnedForInvalidTags(t *testing.T) {
	cases := []struct{ name, apiKey, tag string }{
		{"admin API key with invalid tag", "valid_api_key", "invalid_tag"},
		{"domain API key with valid tag not used", "domain_api_key", "bar"},
		{"author API key with valid tag not used", "author_api_key", "baz"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodGet, fmt.Sprintf("/tags/%s/visits", c.tag), c.apiKey)
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusNotFound, resp.StatusCode)
			h.AssertEquals(t, http.StatusNotFound, payload["status"])
			h.AssertEquals(t, "https://shlink.io/api/error/tag-not-found", payload["type"])
			h.AssertEquals(t, fmt.Sprintf(`Tag with name "%s" could not be found`, c.tag), payload["detail"])
			h.AssertEquals(t, "Tag not found", payload["title"])
		})
	}
}
