package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestDeleteShortUrlVisits_deletesVisitsForShortUrlWithoutAffectingTheRest(t *testing.T) {
	h.Seed(t)
	h.AssertEquals(t, 7, totalVisits(t))
	h.AssertEquals(t, 3, orphanVisits(t))

	resp := h.CallAPIWithKey(t, http.MethodDelete, "/short-urls/abc123/visits")
	payload := resp.JSON(t)

	h.AssertEquals(t, 200, resp.StatusCode)
	h.AssertEquals(t, 3, payload["deletedVisits"])
	h.AssertEquals(t, 4, totalVisits(t))  // This verifies that other visits have not been affected
	h.AssertEquals(t, 3, orphanVisits(t)) // This verifies that orphan visits have not been affected
}

func TestDeleteShortUrlVisits_returnsErrorForInvalidShortUrls(t *testing.T) {
	cases := []struct {
		name, uri     string
		options       h.Options
		expectedError string
	}{
		{"not exists", "/short-urls/does-not-exist/visits", h.Options{},
			`No URL found with short code "does-not-exist"`},
		{"needs domain", "/short-urls/custom-with-domain/visits", h.Options{},
			`No URL found with short code "custom-with-domain"`},
		{"invalid domain", "/short-urls/abc123/visits", h.Options{Query: h.Q("domain", "ff.test")},
			`No URL found with short code "abc123" for domain "ff.test"`},
		{"wrong domain", "/short-urls/custom-with-domain/visits", h.Options{Query: h.Q("domain", "ff.test")},
			`No URL found with short code "custom-with-domain" for domain "ff.test"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKey(t, http.MethodDelete, "/rest/v3"+c.uri, c.options)
			payload := resp.JSON(t)

			h.AssertEquals(t, 404, resp.StatusCode)
			h.AssertEquals(t, c.expectedError, payload["detail"])
			h.AssertEquals(t, "https://shlink.io/api/error/short-url-not-found", payload["type"])
		})
	}
}

func TestDeleteShortUrlVisits_cannotDeleteVisitsForShortUrlWithWrongApiKeyPermissions(t *testing.T) {
	h.Seed(t)

	resp := h.CallAPIWithKeyNamed(t, http.MethodDelete, "/short-urls/abc123/visits", "domain_api_key")
	h.AssertEquals(t, 404, resp.StatusCode)
}
