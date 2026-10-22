package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// paginatorAllItems is Shlinkio\Shlink\Common\Paginator\Paginator::ALL_ITEMS.
const paginatorAllItems = -1

func TestShortUrlVisits_tryingToGetVisitsForInvalidUrlReturnsNotFoundError(t *testing.T) {
	for _, c := range h.InvalidURLs {
		t.Run(c.Name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodGet, h.BuildShortURLPath(c.ShortCode, c.Domain, "/visits"), c.APIKey)

			h.AssertShortURLNotFound(t, resp, c)
		})
	}
}

func TestShortUrlVisits_properVisitsAreReturnedWhenDomainIsProvided(t *testing.T) {
	cases := []struct {
		name                   string
		domain                 *string
		expectedAmountOfVisits int
	}{
		{"domain", h.Ptr("example.com"), 0},
		{"no domain", nil, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKey(t, http.MethodGet, h.BuildShortURLPath("ghi789", c.domain, "/visits"))
			payload := resp.JSON(t)

			h.AssertEquals(t, c.expectedAmountOfVisits, h.DigOr(payload, paginatorAllItems, "visits", "pagination", "totalItems"))
			h.AssertCount(t, c.expectedAmountOfVisits, h.DigOr(payload, h.L{}, "visits", "data"))
		})
	}
}

func TestShortUrlVisits_properVisitsAreReturnedWhenExcludingBots(t *testing.T) {
	cases := []struct {
		name                   string
		excludeBots            bool
		expectedAmountOfVisits int
	}{
		{"bots excluded", true, 1},
		{"bots not excluded", false, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			url := "/short-urls/def456/visits"
			if c.excludeBots {
				url = h.WithQuery(url, h.Q("excludeBots", true))
			}

			resp := h.CallAPIWithKey(t, http.MethodGet, url)
			payload := resp.JSON(t)

			h.AssertEquals(t, c.expectedAmountOfVisits, h.DigOr(payload, paginatorAllItems, "visits", "pagination", "totalItems"))
			h.AssertCount(t, c.expectedAmountOfVisits, h.DigOr(payload, h.L{}, "visits", "data"))
		})
	}
}
