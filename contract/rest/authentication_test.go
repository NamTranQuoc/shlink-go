package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestAuthentication_authorizationErrorIsReturnedIfNoApiKeyIsSent(t *testing.T) {
	cases := []struct{ name, version, expectedType string }{
		{"version 1", "1", "https://shlink.io/api/error/missing-authentication"},
		{"version 2", "2", "https://shlink.io/api/error/missing-authentication"},
		{"version 3", "3", "https://shlink.io/api/error/missing-authentication"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			expectedDetail := `Expected one of the following authentication headers, ["X-Api-Key"], but none were provided`

			resp := h.CallAPI(t, http.MethodGet, "/rest/v"+c.version+"/short-urls")
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusUnauthorized, resp.StatusCode)
			h.AssertEquals(t, http.StatusUnauthorized, payload["status"])
			h.AssertEquals(t, c.expectedType, payload["type"])
			h.AssertEquals(t, expectedDetail, payload["detail"])
			h.AssertEquals(t, "Invalid authorization", payload["title"])
		})
	}
}

func TestAuthentication_apiKeyErrorIsReturnedWhenProvidedApiKeyIsInvalid(t *testing.T) {
	cases := []struct{ name, apiKey, version, expectedType string }{
		{"key which does not exist", "invalid", "2", "https://shlink.io/api/error/invalid-api-key"},
		{"key which is expired", "expired_api_key", "2", "https://shlink.io/api/error/invalid-api-key"},
		{"key which is disabled", "disabled_api_key", "2", "https://shlink.io/api/error/invalid-api-key"},
		{"version 3", "disabled_api_key", "3", "https://shlink.io/api/error/invalid-api-key"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			expectedDetail := "Provided API key does not exist or is invalid."

			resp := h.CallAPI(t, http.MethodGet, "/rest/v"+c.version+"/short-urls", h.Options{
				Headers: http.Header{"X-Api-Key": {c.apiKey}},
			})
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusUnauthorized, resp.StatusCode)
			h.AssertEquals(t, http.StatusUnauthorized, payload["status"])
			h.AssertEquals(t, c.expectedType, payload["type"])
			h.AssertEquals(t, expectedDetail, payload["detail"])
			h.AssertEquals(t, "Invalid API key", payload["title"])
		})
	}
}
