package rest

import (
	"net/http"
	"strings"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestImplicitOptions_optionsRequestsReturnEmptyResponse(t *testing.T) {
	h.Seed(t)

	resp := h.CallAPI(t, http.MethodOptions, "/short-urls")

	h.AssertEquals(t, http.StatusNoContent, resp.StatusCode)
	h.AssertEquals(t, "", resp.Text())
}

func TestImplicitOptions_optionsRequestsReturnAllowedMethodsForEndpoint(t *testing.T) {
	h.Seed(t)

	resp := h.CallAPI(t, http.MethodOptions, "/short-urls")
	allowedMethods := headerLine(resp, "Allow")

	h.AssertEquals(t, []string{http.MethodGet, http.MethodPost}, strings.Split(allowedMethods, ","))
}
