package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

var (
	orphanInvalidShortURL = h.M{
		"referer":       "https://s.test/foo",
		"date":          "2020-03-01T00:00:00+00:00",
		"userAgent":     "cf-facebook",
		"visitLocation": nil,
		"potentialBot":  true,
		"visitedUrl":    "https://example.com/short",
		"type":          "invalid_short_url",
		"redirectUrl":   nil,
	}
	orphanRegularNotFound = h.M{
		"referer":       "https://s.test/foo/bar",
		"date":          "2020-02-01T00:00:00+00:00",
		"userAgent":     "shlink-tests-agent",
		"visitLocation": nil,
		"potentialBot":  false,
		"visitedUrl":    "https://s.test/bar",
		"type":          "regular_404",
		"redirectUrl":   nil,
	}
	orphanBaseURL = h.M{
		"referer":       "https://s.test",
		"date":          "2020-01-01T00:00:00+00:00",
		"userAgent":     "shlink-tests-agent",
		"visitLocation": nil,
		"potentialBot":  false,
		"visitedUrl":    "https://s.test/foo",
		"type":          "base_url",
		"redirectUrl":   nil,
	}
)

func TestOrphanVisits_properVisitsAreReturnedBasedInQuery(t *testing.T) {
	cases := []struct {
		name                       string
		query                      h.Query
		totalItems, expectedAmount int
		expectedVisits             h.L
	}{
		{"all data", h.Q(), 3, 3, h.L{orphanInvalidShortURL, orphanRegularNotFound, orphanBaseURL}},
		{"limit items", h.Q("itemsPerPage", 2), 3, 2, h.L{orphanInvalidShortURL, orphanRegularNotFound}},
		{"limit items and page", h.Q("itemsPerPage", 2, "page", 2), 3, 1, h.L{orphanBaseURL}},
		{"exclude bots", h.Q("excludeBots", true), 2, 2, h.L{orphanRegularNotFound, orphanBaseURL}},
		{"exclude bots and limit items", h.Q("excludeBots", true, "itemsPerPage", 1), 2, 1, h.L{orphanRegularNotFound}},
		{"base_url only", h.Q("type", "base_url"), 1, 1, h.L{orphanBaseURL}},
		{"regular_404 only", h.Q("type", "regular_404"), 1, 1, h.L{orphanRegularNotFound}},
		{"invalid_short_url only", h.Q("type", "invalid_short_url"), 1, 1, h.L{orphanInvalidShortURL}},
		{"example domain only", h.Q("domain", "example.com"), 1, 1, h.L{orphanInvalidShortURL}},
		{"default domain only", h.Q("domain", "s.test"), 2, 2, h.L{orphanRegularNotFound, orphanBaseURL}},
		{"default domain only with DEFAULT keyword", h.Q("domain", "DEFAULT"), 2, 2,
			h.L{orphanRegularNotFound, orphanBaseURL}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKey(t, http.MethodGet, "/visits/orphan", h.Options{Query: c.query})
			payload := resp.JSON(t)
			visits := h.DigOr(payload, h.L{}, "visits", "data")

			h.AssertEquals(t, c.totalItems, h.DigOr(payload, paginatorAllItems, "visits", "pagination", "totalItems"))
			h.AssertCount(t, c.expectedAmount, visits)
			h.AssertEquals(t, c.expectedVisits, visits)
		})
	}
}

func TestOrphanVisits_errorIsReturnedForInvalidType(t *testing.T) {
	h.Seed(t)

	resp := h.CallAPIWithKey(t, http.MethodGet, "/visits/orphan", h.Options{Query: h.Q("type", "invalid")})
	h.AssertEquals(t, 400, resp.StatusCode)
}

func TestOrphanVisits_noVisitsAreReturnedForRestrictedApiKey(t *testing.T) {
	h.Seed(t)

	resp := h.CallAPIWithKeyNamed(t, http.MethodGet, "/visits/orphan", "no_orphans_api_key")
	payload := resp.JSON(t)
	visits := h.Dig(payload, "visits", "data")

	h.AssertCount(t, 0, visits)
	h.AssertEquals(t, 0, h.DigOr(payload, paginatorAllItems, "visits", "pagination", "totalItems"))
}
