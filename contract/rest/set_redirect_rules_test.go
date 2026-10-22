package rest

import (
	"fmt"
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestSetRedirectRules_errorIsReturnedWhenInvalidUrlIsProvided(t *testing.T) {
	h.Seed(t)

	response := h.CallAPIWithKey(t, http.MethodPost, "/short-urls/invalid/redirect-rules")
	payload := response.JSON(t)

	h.AssertEquals(t, 404, response.StatusCode)
	h.AssertEquals(t, 404, payload["status"])
	h.AssertEquals(t, "invalid", payload["shortCode"])
	h.AssertEquals(t, `No URL found with short code "invalid"`, payload["detail"])
	h.AssertEquals(t, "Short URL not found", payload["title"])
	h.AssertEquals(t, "https://shlink.io/api/error/short-url-not-found", payload["type"])
}

func TestSetRedirectRules_errorIsReturnedWhenInvalidDataIsProvided(t *testing.T) {
	withCondition := func(cond h.M) h.M {
		return h.M{"redirectRules": h.L{h.M{"longUrl": "https://example.com", "conditions": h.L{cond}}}}
	}
	cases := []struct {
		name        string
		bodyPayload h.M
	}{
		{"invalid data", h.M{"redirectRules": h.L{"foo"}}},
		{"invalid long URL", h.M{"redirectRules": h.L{h.M{"longUrl": "invalid"}}}},
		{"non-array conditions", h.M{"redirectRules": h.L{h.M{"longUrl": "https://example.com", "conditions": "foo"}}}},
		{"invalid condition type", withCondition(condition("invalid", nil, "foo"))},
		{"invalid device type", withCondition(condition("device", nil, "invalid-device"))},
		{"invalid IP address", withCondition(condition("ip-address", nil, "not an IP address"))},
		{"invalid country code", withCondition(condition("geolocation-country-code", nil, "not a country code"))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			response := h.CallAPIWithKey(t, http.MethodPost, "/short-urls/abc123/redirect-rules", h.Options{
				JSON: c.bodyPayload,
			})
			payload := response.JSON(t)

			h.AssertEquals(t, 400, response.StatusCode)
			h.AssertEquals(t, 400, payload["status"])
			h.AssertEquals(t, "Provided data is not valid", payload["detail"])
			h.AssertEquals(t, "Invalid data", payload["title"])
			h.AssertEquals(t, "https://shlink.io/api/error/invalid-data", payload["type"])
		})
	}
}

func TestSetRedirectRules_setsListOfRulesForShortUrl(t *testing.T) {
	ipRule := func(matchValue string) h.L {
		return h.L{rule("https://example.com", 1, condition("ip-address", nil, matchValue))}
	}
	cases := []struct {
		name, shortCode string
		expectedRules   h.L
	}{
		{"#0", "def456", h.L{}},
		{"#1", "abc123", h.L{
			rule("https://example.com/english-and-foo-query", 1,
				languageEnCondition, condition("any-value-query-param", "foo", nil)),
			rule("https://example.com/multiple-query-params", 2,
				condition("query-param", "hello", "world"), condition("query-param", "foo", "bar")),
		}},
		{"static IP", "abc123", ipRule("1.2.3.4")},
		{"CIDR block", "abc123", ipRule("1.2.3.0/24")},
		{"IP wildcard pattern", "abc123", ipRule("1.2.3.*")},
		{"in-between IP wildcard pattern", "abc123", ipRule("1.2.*.4")},
		{"country code", "abc123", h.L{
			rule("https://example.com", 1, condition("geolocation-country-code", nil, "US")),
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			response := h.CallAPIWithKey(t, http.MethodPost, fmt.Sprintf("/short-urls/%s/redirect-rules", c.shortCode),
				h.Options{JSON: h.M{"redirectRules": c.expectedRules}})
			payload := response.JSON(t)

			h.AssertEquals(t, 200, response.StatusCode)
			h.AssertEquals(t, c.expectedRules, payload["redirectRules"])
		})
	}
}
