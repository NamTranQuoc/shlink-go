package rest

import (
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestBodyParser_returnsErrorWhenRequestBodyIsInvalidJson(t *testing.T) {
	h.Seed(t)

	resp := h.CallAPIWithKey(t, http.MethodPost, "/short-urls", h.Options{
		Headers: http.Header{"Content-Type": {"application/json"}},
		Body:    []byte(`{"foo`),
	})
	payload := resp.JSON(t)

	h.AssertEquals(t, 400, resp.StatusCode)
	h.AssertEquals(t, 400, payload["status"])
	h.AssertEquals(t, "Provided request does not contain a valid JSON body.", payload["detail"])
	h.AssertEquals(t, "Malformed request body", payload["title"])
	h.AssertEquals(t, "https://shlink.io/api/error/malformed-request-body", payload["type"])
}
