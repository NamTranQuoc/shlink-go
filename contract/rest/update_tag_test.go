package rest

import (
	"fmt"
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestUpdateTag_notProvidingTagsReturnsBadRequest(t *testing.T) {
	// PHP's json_encode([]) is "[]", which is what Guzzle sends for an empty payload.
	cases := []any{h.L{}, h.M{"oldName": "foo"}, h.M{"newName": "foo"}}
	for i, body := range cases {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)
			expectedDetail := "Provided data is not valid"

			resp := h.CallAPIWithKey(t, http.MethodPut, "/tags", h.Options{JSON: body})
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusBadRequest, resp.StatusCode)
			h.AssertEquals(t, http.StatusBadRequest, payload["status"])
			h.AssertEquals(t, "https://shlink.io/api/error/invalid-data", payload["type"])
			h.AssertEquals(t, expectedDetail, payload["detail"])
			h.AssertEquals(t, "Invalid data", payload["title"])
		})
	}
}

func TestUpdateTag_tryingToRenameInvalidTagReturnsNotFound(t *testing.T) {
	cases := []struct{ name, version, expectedType string }{
		{"version 1", "1", "https://shlink.io/api/error/tag-not-found"},
		{"version 2", "2", "https://shlink.io/api/error/tag-not-found"},
		{"version 3", "3", "https://shlink.io/api/error/tag-not-found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			expectedDetail := `Tag with name "invalid_tag" could not be found`

			resp := h.CallAPIWithKey(t, http.MethodPut, "/rest/v"+c.version+"/tags", h.Options{
				JSON: h.M{"oldName": "invalid_tag", "newName": "foo"},
			})
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusNotFound, resp.StatusCode)
			h.AssertEquals(t, http.StatusNotFound, payload["status"])
			h.AssertEquals(t, c.expectedType, payload["type"])
			h.AssertEquals(t, expectedDetail, payload["detail"])
			h.AssertEquals(t, "Tag not found", payload["title"])
		})
	}
}

func TestUpdateTag_errorIsThrownWhenTryingToRenameTagToAnotherTagName(t *testing.T) {
	cases := []struct{ name, version, expectedType string }{
		{"version 1", "1", "https://shlink.io/api/error/tag-conflict"},
		{"version 2", "2", "https://shlink.io/api/error/tag-conflict"},
		{"version 3", "3", "https://shlink.io/api/error/tag-conflict"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			expectedDetail := "You cannot rename tag foo to bar, because it already exists"

			resp := h.CallAPIWithKey(t, http.MethodPut, "/rest/v"+c.version+"/tags", h.Options{
				JSON: h.M{"oldName": "foo", "newName": "bar"},
			})
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusConflict, resp.StatusCode)
			h.AssertEquals(t, http.StatusConflict, payload["status"])
			h.AssertEquals(t, c.expectedType, payload["type"])
			h.AssertEquals(t, expectedDetail, payload["detail"])
			h.AssertEquals(t, "Tag conflict", payload["title"])
		})
	}
}

func TestUpdateTag_tagIsProperlyRenamedWhenRenamingToItself(t *testing.T) {
	h.Seed(t)

	resp := h.CallAPIWithKey(t, http.MethodPut, "/tags", h.Options{
		JSON: h.M{"oldName": "foo", "newName": "foo"},
	})

	h.AssertEquals(t, http.StatusNoContent, resp.StatusCode)
}
