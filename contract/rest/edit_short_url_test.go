package rest

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func findShortURLMetaByShortCode(t *testing.T, shortCode string) any {
	t.Helper()
	matchingShortURL := h.CallAPIWithKey(t, http.MethodGet, "/short-urls/"+shortCode).JSON(t)
	return h.DigOr(matchingShortURL, h.M{}, "meta")
}

func TestEditShortUrl_metadataCanBeReset(t *testing.T) {
	now := time.Now()
	cases := []h.M{
		{"validSince": h.AtomString(now.AddDate(0, 1, 0))},
		{"validUntil": h.AtomString(now.AddDate(0, -1, 0))},
		{"maxVisits": 20},
		{"validUntil": h.AtomString(now.AddDate(1, 0, 0)), "maxVisits": 100},
		{
			"validSince": h.AtomString(now.AddDate(-1, 0, 0)),
			"validUntil": h.AtomString(now.AddDate(1, 0, 0)),
			"maxVisits":  100,
		},
	}
	for i, meta := range cases {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)
			shortCode := "abc123"
			url := "/short-urls/" + shortCode
			resetMeta := h.M{"validSince": nil, "validUntil": nil, "maxVisits": nil}

			editWithProvidedMeta := h.CallAPIWithKey(t, http.MethodPatch, url, h.Options{JSON: meta})
			metaAfterEditing := findShortURLMetaByShortCode(t, shortCode)

			editWithResetMeta := h.CallAPIWithKey(t, http.MethodPatch, url, h.Options{JSON: resetMeta})
			metaAfterResetting := findShortURLMetaByShortCode(t, shortCode)

			h.AssertEquals(t, http.StatusOK, editWithProvidedMeta.StatusCode)
			h.AssertEquals(t, http.StatusOK, editWithResetMeta.StatusCode)
			h.AssertEquals(t, resetMeta, metaAfterResetting)
			for key, expectedValue := range meta {
				h.AssertEquals(t, expectedValue, h.Dig(metaAfterEditing, key), key)
			}
		})
	}
}

func TestEditShortUrl_tryingToEditInvalidUrlReturnsNotFoundError(t *testing.T) {
	for _, c := range h.InvalidURLs {
		t.Run(c.Name, func(t *testing.T) {
			h.Seed(t)

			url := h.BuildShortURLPath(c.ShortCode, c.Domain, "")
			// PHP's json_encode([]) is "[]", which is what Guzzle sends for an empty payload.
			resp := h.CallAPIWithKeyNamed(t, http.MethodPatch, url, c.APIKey, h.Options{JSON: h.L{}})

			h.AssertShortURLNotFound(t, resp, c)
		})
	}
}

func TestEditShortUrl_providingInvalidDataReturnsBadRequest(t *testing.T) {
	h.Seed(t)
	expectedDetail := "Provided data is not valid"

	resp := h.CallAPIWithKey(t, http.MethodPatch, "/short-urls/invalid", h.Options{
		JSON: h.M{"maxVisits": "not_a_number"},
	})
	payload := resp.JSON(t)

	h.AssertEquals(t, http.StatusBadRequest, resp.StatusCode)
	h.AssertEquals(t, http.StatusBadRequest, payload["status"])
	h.AssertEquals(t, "https://shlink.io/api/error/invalid-data", payload["type"])
	h.AssertEquals(t, expectedDetail, payload["detail"])
	h.AssertEquals(t, "Invalid data", payload["title"])
}

func TestEditShortUrl_metadataIsEditedOnProperShortUrlBasedOnDomain(t *testing.T) {
	cases := []struct {
		name        string
		domain      *string
		expectedURL string
	}{
		{"domain", h.Ptr("example.com"),
			"https://blog.alejandrocelaya.com/2019/04/27/considerations-to-properly-use-open-source-software-projects/"},
		{"no domain", nil, "https://shlink.io/documentation/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			url := h.BuildShortURLPath("ghi789", c.domain, "")

			editResp := h.CallAPIWithKey(t, http.MethodPatch, url, h.Options{JSON: h.M{"maxVisits": 100}})
			editedShortURL := editResp.JSON(t)

			h.AssertEquals(t, http.StatusOK, editResp.StatusCode)
			h.AssertEquals(t, optional(c.domain), editedShortURL["domain"])
			h.AssertEquals(t, c.expectedURL, editedShortURL["longUrl"])
			h.AssertEquals(t, 100, h.Dig(editedShortURL, "meta", "maxVisits"))
		})
	}
}
