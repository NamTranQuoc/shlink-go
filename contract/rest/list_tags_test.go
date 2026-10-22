package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestListTags_expectedListOfTagsIsReturned(t *testing.T) {
	cases := []struct {
		name, apiKey string
		query        h.Query
		expectedTags h.M
	}{
		{"admin API key", "valid_api_key", h.Q(), h.M{
			"data": h.L{"bar", "baz", "foo"},
			"pagination": h.M{
				"currentPage": 1, "pagesCount": 1, "itemsPerPage": 3, "itemsInCurrentPage": 3, "totalItems": 3,
			},
		}},
		{"admin api key with pagination", "valid_api_key", h.Q("page", 2, "itemsPerPage", 2), h.M{
			"data": h.L{"foo"},
			"pagination": h.M{
				"currentPage": 2, "pagesCount": 2, "itemsPerPage": 2, "itemsInCurrentPage": 1, "totalItems": 3,
			},
		}},
		{"author API key", "author_api_key", h.Q(), h.M{
			"data": h.L{"bar", "foo"},
			"pagination": h.M{
				"currentPage": 1, "pagesCount": 1, "itemsPerPage": 2, "itemsInCurrentPage": 2, "totalItems": 2,
			},
		}},
		{"domain API key", "domain_api_key", h.Q(), h.M{
			"data": h.L{"foo"},
			"pagination": h.M{
				"currentPage": 1, "pagesCount": 1, "itemsPerPage": 1, "itemsInCurrentPage": 1, "totalItems": 1,
			},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodGet, "/tags", c.apiKey, h.Options{Query: c.query})
			payload := resp.JSON(t)

			h.AssertEquals(t, h.M{"tags": c.expectedTags}, payload)
		})
	}
}
