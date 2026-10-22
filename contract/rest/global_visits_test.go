package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestGlobalVisits_returnsExpectedVisitsStats(t *testing.T) {
	cases := []struct {
		name, apiKey                         string
		expectedVisits, expectedOrphanVisits int
	}{
		{"admin API key", "valid_api_key", 7, 3},
		{"domain API key", "domain_api_key", 0, 3},
		{"author API key", "author_api_key", 5, 3},
		{"no orphans API key", "no_orphans_api_key", 7, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodGet, "/visits", c.apiKey)
			payload := resp.JSON(t)

			h.AssertHasKey(t, payload, "visits")
			h.AssertEquals(t, c.expectedVisits, h.Dig(payload, "visits", "nonOrphanVisits", "total"))
			h.AssertEquals(t, c.expectedOrphanVisits, h.Dig(payload, "visits", "orphanVisits", "total"))
		})
	}
}
