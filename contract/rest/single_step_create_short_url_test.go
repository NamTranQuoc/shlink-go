package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func singleStepCreateShortURL(t *testing.T, format, apiKey any) *h.Response {
	t.Helper()
	return h.CallAPI(t, http.MethodGet, "/short-urls/shorten", h.Options{
		Query: h.Q("longUrl", "https://app.shlink.io", "apiKey", apiKey, "format", format),
	})
}

func TestSingleStepCreateShortUrl_createsNewShortUrlWithExpectedResponse(t *testing.T) {
	cases := []struct {
		name                string
		format              any
		expectedContentType string
	}{
		{"txt format", "txt", "text/plain"},
		{"json format", "json", "application/json"},
		{"<empty> format", nil, "application/json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := singleStepCreateShortURL(t, c.format, "valid_api_key")

			h.AssertEquals(t, http.StatusOK, resp.StatusCode)
			h.AssertEquals(t, c.expectedContentType, headerLine(resp, "Content-Type"))
		})
	}
}

func TestSingleStepCreateShortUrl_authorizationErrorIsReturnedIfNoApiKeyIsSent(t *testing.T) {
	h.Seed(t)
	expectedDetail := `Expected authentication to be provided in "apiKey" query param`

	resp := singleStepCreateShortURL(t, "json", nil)
	payload := resp.JSON(t)

	h.AssertEquals(t, http.StatusUnauthorized, resp.StatusCode)
	h.AssertEquals(t, http.StatusUnauthorized, payload["status"])
	h.AssertEquals(t, "https://shlink.io/api/error/missing-authentication", payload["type"])
	h.AssertEquals(t, expectedDetail, payload["detail"])
	h.AssertEquals(t, "Invalid authorization", payload["title"])
}
