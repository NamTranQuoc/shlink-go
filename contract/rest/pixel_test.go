package rest

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// Not covered by upstream's API tests: PixelAction tracks a visit and always answers with a 1x1 GIF.

var pixelGIF, _ = base64.StdEncoding.DecodeString("R0lGODlhAQABAJAAAP8AAAAAACH5BAUQAAAALAAAAAABAAEAAAICBAEAOw==")

func visitsTotal(t *testing.T, uri string) float64 {
	t.Helper()
	resp := h.CallAPIWithKey(t, http.MethodGet, uri)
	h.AssertStatus(t, resp, http.StatusOK)
	return resp.JSON(t)["visits"].(map[string]any)["pagination"].(map[string]any)["totalItems"].(float64)
}

func assertPixel(t *testing.T, resp *h.Response) {
	t.Helper()
	h.AssertStatus(t, resp, http.StatusOK)
	h.AssertEquals(t, "image/gif", resp.Header.Get("Content-Type"))
	if !bytes.Equal(pixelGIF, resp.Body) {
		t.Fatalf("unexpected pixel body: %x", resp.Body)
	}
}

func TestPixel_tracksVisitForValidShortUrl(t *testing.T) {
	h.Seed(t)
	before := visitsTotal(t, "/short-urls/def456/visits")

	assertPixel(t, h.Request(t, http.MethodGet, "/def456/track"))

	h.AssertEquals(t, before+1, visitsTotal(t, "/short-urls/def456/visits"))
}

func TestPixel_returnsPixelWithoutTrackingForInvalidShortUrl(t *testing.T) {
	h.Seed(t)
	before := visitsTotal(t, "/visits/orphan")

	assertPixel(t, h.Request(t, http.MethodGet, "/invalid/track"))

	h.AssertEquals(t, before, visitsTotal(t, "/visits/orphan"))
}
