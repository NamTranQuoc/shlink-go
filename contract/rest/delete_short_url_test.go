package rest

import (
	"fmt"
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestDeleteShortUrl_notFoundErrorIsReturnWhenDeletingInvalidUrl(t *testing.T) {
	for _, c := range h.InvalidURLs {
		t.Run(c.Name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodDelete, h.BuildShortURLPath(c.ShortCode, c.Domain, ""), c.APIKey)

			h.AssertShortURLNotFound(t, resp, c)
		})
	}
}

func TestDeleteShortUrl_expectedTypeIsReturnedBasedOnApiVersion(t *testing.T) {
	cases := []struct{ version, expectedType string }{
		{"1", "https://shlink.io/api/error/short-url-not-found"},
		{"2", "https://shlink.io/api/error/short-url-not-found"},
		{"3", "https://shlink.io/api/error/short-url-not-found"},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKey(t, http.MethodDelete, "/rest/v"+c.version+"/short-urls/invalid-short-code")
			payload := resp.JSON(t)

			h.AssertEquals(t, c.expectedType, payload["type"])
		})
	}
}

func TestDeleteShortUrl_properShortUrlIsDeletedWhenDomainIsProvided(t *testing.T) {
	h.Seed(t)

	fetchWithDomainBefore := h.CallAPIWithKey(t, http.MethodGet, "/short-urls/ghi789?domain=example.com")
	fetchWithoutDomainBefore := h.CallAPIWithKey(t, http.MethodGet, "/short-urls/ghi789")
	deleteResp := h.CallAPIWithKey(t, http.MethodDelete, "/short-urls/ghi789?domain=example.com")
	fetchWithDomainAfter := h.CallAPIWithKey(t, http.MethodGet, "/short-urls/ghi789?domain=example.com")
	fetchWithoutDomainAfter := h.CallAPIWithKey(t, http.MethodGet, "/short-urls/ghi789")

	h.AssertEquals(t, http.StatusOK, fetchWithDomainBefore.StatusCode)
	h.AssertEquals(t, http.StatusOK, fetchWithoutDomainBefore.StatusCode)
	h.AssertEquals(t, http.StatusNoContent, deleteResp.StatusCode)
	h.AssertEquals(t, http.StatusNotFound, fetchWithDomainAfter.StatusCode)
	h.AssertEquals(t, http.StatusOK, fetchWithoutDomainAfter.StatusCode)
}
