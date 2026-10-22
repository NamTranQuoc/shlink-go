package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func tagStats(tag string, shortURLsCount, total, nonBots, bots int) h.M {
	return h.M{
		"tag":            tag,
		"shortUrlsCount": shortURLsCount,
		"visitsSummary":  h.M{"total": total, "nonBots": nonBots, "bots": bots},
	}
}

func pagination(currentPage, pagesCount, itemsPerPage, itemsInCurrentPage, totalItems int) h.M {
	return h.M{
		"currentPage":        currentPage,
		"pagesCount":         pagesCount,
		"itemsPerPage":       itemsPerPage,
		"itemsInCurrentPage": itemsInCurrentPage,
		"totalItems":         totalItems,
	}
}

func TestTagsStats_expectedListOfTagsIsReturned(t *testing.T) {
	cases := []struct {
		name, apiKey       string
		query              h.Query
		expectedStats      h.L
		expectedPagination h.M
	}{
		{"admin API key", "valid_api_key", h.Q(),
			h.L{tagStats("bar", 1, 2, 1, 1), tagStats("baz", 0, 0, 0, 0), tagStats("foo", 3, 5, 4, 1)},
			pagination(1, 1, 3, 3, 3)},
		{"admin API key with pagination", "valid_api_key", h.Q("page", 1, "itemsPerPage", 2),
			h.L{tagStats("bar", 1, 2, 1, 1), tagStats("baz", 0, 0, 0, 0)},
			pagination(1, 2, 2, 2, 3)},
		{"author API key", "author_api_key", h.Q(),
			h.L{tagStats("bar", 1, 2, 1, 1), tagStats("foo", 2, 5, 4, 1)},
			pagination(1, 1, 2, 2, 2)},
		{"author API key with pagination", "author_api_key", h.Q("page", 2, "itemsPerPage", 1),
			h.L{tagStats("foo", 2, 5, 4, 1)},
			pagination(2, 2, 1, 1, 2)},
		{"domain API key", "domain_api_key", h.Q(),
			h.L{tagStats("foo", 1, 0, 0, 0)},
			pagination(1, 1, 1, 1, 1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodGet, "/tags/stats", c.apiKey, h.Options{Query: c.query})
			tags := h.Dig(resp.JSON(t), "tags")

			h.AssertEquals(t, c.expectedStats, h.Dig(tags, "data"))
			h.AssertEquals(t, c.expectedPagination, h.Dig(tags, "pagination"))
		})
	}
}
