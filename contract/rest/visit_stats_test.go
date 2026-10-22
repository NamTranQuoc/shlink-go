package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestVisitStats_expectedStatsAreReturned(t *testing.T) {
	orphanVisits := h.M{"total": 3, "nonBots": 2, "bots": 1}
	cases := []struct {
		name, apiKey    string
		expectedPayload h.M
	}{
		{"valid API key", "valid_api_key", h.M{
			"nonOrphanVisits": h.M{"total": 7, "nonBots": 6, "bots": 1},
			"orphanVisits":    orphanVisits,
		}},
		{"domain-only API key", "domain_api_key", h.M{
			"nonOrphanVisits": h.M{"total": 0, "nonBots": 0, "bots": 0},
			"orphanVisits":    orphanVisits,
		}},
		{"author API key", "author_api_key", h.M{
			"nonOrphanVisits": h.M{"total": 5, "nonBots": 4, "bots": 1},
			"orphanVisits":    orphanVisits,
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodGet, "/visits", c.apiKey)
			payload := resp.JSON(t)

			h.AssertEquals(t, h.M{"visits": c.expectedPayload}, payload)
		})
	}
}
