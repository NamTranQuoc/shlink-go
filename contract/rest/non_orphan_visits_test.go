package rest

import (
	"net/http"
	"testing"
	"time"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestNonOrphanVisits_properVisitsAreReturnedBasedInQuery(t *testing.T) {
	cases := []struct {
		name                      string
		query                     h.Query
		totalItems, returnedItems int
	}{
		{"all data", h.Q(), 7, 7},
		{"middle page", h.Q("page", 2, "itemsPerPage", 3), 7, 3},
		{"last page", h.Q("page", 3, "itemsPerPage", 3), 7, 1},
		{"bots excluded", h.Q("excludeBots", "true"), 6, 6},
		{"bots excluded and pagination", h.Q("excludeBots", "true", "page", 1, "itemsPerPage", 4), 6, 4},
		{"date filter", h.Q("startDate", h.AtomString(time.Now().AddDate(0, 0, 1))), 0, 0},
		{"domain filter", h.Q("domain", "example.com"), 0, 0},
		{"default domain filter", h.Q("domain", "DEFAULT"), 7, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKey(t, http.MethodGet, "/visits/non-orphan", h.Options{Query: c.query})
			payload := resp.JSON(t)

			h.AssertEquals(t, c.totalItems, h.DigOr(payload, paginatorAllItems, "visits", "pagination", "totalItems"))
			h.AssertCount(t, c.returnedItems, h.DigOr(payload, h.L{}, "visits", "data"))
		})
	}
}
