package rest

import (
	"net/http"
	"testing"
	"time"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestResolveShortUrl_shortUrlIsProperlyResolvedEvenWhenNotEnabled(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name         string
		disabledMeta h.M
	}{
		{"future validSince", h.M{"validSince": h.AtomString(now.AddDate(0, 1, 0))}},
		{"past validUntil", h.M{"validUntil": h.AtomString(now.AddDate(0, -1, 0))}},
		{"maxVisits reached", h.M{"maxVisits": 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			shortCode := "abc123"
			url := "/short-urls/" + shortCode
			h.CallShortURL(t, shortCode)

			editResp := h.CallAPIWithKey(t, http.MethodPatch, url, h.Options{JSON: c.disabledMeta})
			visitResp := h.CallShortURL(t, shortCode)
			fetchResp := h.CallAPIWithKey(t, http.MethodGet, url)

			h.AssertEquals(t, http.StatusOK, editResp.StatusCode)
			h.AssertEquals(t, http.StatusNotFound, visitResp.StatusCode)
			h.AssertEquals(t, http.StatusOK, fetchResp.StatusCode)
		})
	}
}

func TestResolveShortUrl_tryingToResolveInvalidUrlReturnsNotFoundError(t *testing.T) {
	for _, c := range h.InvalidURLs {
		t.Run(c.Name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKeyNamed(t, http.MethodGet, h.BuildShortURLPath(c.ShortCode, c.Domain, ""), c.APIKey)

			h.AssertShortURLNotFound(t, resp, c)
		})
	}
}
