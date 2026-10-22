package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestRenameTag_anErrorIsReturnedWithNonAdminApiKeys(t *testing.T) {
	cases := []struct{ name, apiKey string }{
		{"author", "author_api_key"},
		{"domain", "domain_api_key"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodPut, "/tags", c.apiKey, h.Options{
				JSON: h.M{"oldName": "foo", "newName": "foo_renamed"},
			})
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusForbidden, resp.StatusCode)
			h.AssertEquals(t, http.StatusForbidden, payload["status"])
			h.AssertEquals(t, "https://shlink.io/api/error/forbidden-tag-operation", payload["type"])
			h.AssertEquals(t, "You are not allowed to rename tags", payload["detail"])
			h.AssertEquals(t, "Forbidden tag operation", payload["title"])
		})
	}
}
