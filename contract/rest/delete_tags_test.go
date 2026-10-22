package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestDeleteTags_anErrorIsReturnedWithNonAdminApiKeys(t *testing.T) {
	cases := []struct{ name, apiKey, version, expectedType string }{
		{"author", "author_api_key", "2", "https://shlink.io/api/error/forbidden-tag-operation"},
		{"domain", "domain_api_key", "2", "https://shlink.io/api/error/forbidden-tag-operation"},
		{"version 3", "domain_api_key", "3", "https://shlink.io/api/error/forbidden-tag-operation"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodDelete, "/rest/v"+c.version+"/tags", c.apiKey, h.Options{
				Query: h.Q("tags", []string{"foo"}),
			})
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusForbidden, resp.StatusCode)
			h.AssertEquals(t, http.StatusForbidden, payload["status"])
			h.AssertEquals(t, c.expectedType, payload["type"])
			h.AssertEquals(t, "You are not allowed to delete tags", payload["detail"])
			h.AssertEquals(t, "Forbidden tag operation", payload["title"])
		})
	}
}
