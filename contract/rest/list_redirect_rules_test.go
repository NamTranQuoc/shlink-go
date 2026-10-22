package rest

import (
	"fmt"
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

var languageEnCondition = h.M{"type": "language", "matchKey": nil, "matchValue": "en"}

func condition(typ string, matchKey, matchValue any) h.M {
	return h.M{"type": typ, "matchKey": matchKey, "matchValue": matchValue}
}

func rule(longURL string, priority int, conditions ...h.M) h.M {
	conds := h.L{}
	for _, c := range conditions {
		conds = append(conds, c)
	}
	return h.M{"longUrl": longURL, "priority": priority, "conditions": conds}
}

func TestListRedirectRules_errorIsReturnedWhenInvalidUrlIsFetched(t *testing.T) {
	h.Seed(t)

	response := h.CallAPIWithKey(t, http.MethodGet, "/short-urls/invalid/redirect-rules")
	payload := response.JSON(t)

	h.AssertEquals(t, 404, response.StatusCode)
	h.AssertEquals(t, 404, payload["status"])
	h.AssertEquals(t, "invalid", payload["shortCode"])
	h.AssertEquals(t, `No URL found with short code "invalid"`, payload["detail"])
	h.AssertEquals(t, "Short URL not found", payload["title"])
	h.AssertEquals(t, "https://shlink.io/api/error/short-url-not-found", payload["type"])
}

func TestListRedirectRules_returnsListOfRulesForShortUrl(t *testing.T) {
	cases := []struct {
		shortCode     string
		expectedRules h.L
	}{
		{"abc123", h.L{}},
		{"def456", h.L{
			rule("https://example.com/english-and-foo-query", 1,
				languageEnCondition, condition("any-value-query-param", "foo", nil)),
			rule("https://example.com/multiple-query-params", 2,
				condition("query-param", "hello", "world"), condition("query-param", "foo", "bar")),
			rule("https://example.com/only-english", 3, languageEnCondition),
			rule("android://foo/bar", 4, condition("device", nil, "android")),
			rule("fb://profile/33138223345", 5, condition("device", nil, "ios")),
			rule("https://example.com/static-ip-address", 6, condition("ip-address", nil, "1.2.3.4")),
			rule("https://example.com/linux", 7, condition("device", nil, "linux")),
		}},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)

			response := h.CallAPIWithKey(t, http.MethodGet, fmt.Sprintf("/short-urls/%s/redirect-rules", c.shortCode))
			payload := response.JSON(t)

			h.AssertEquals(t, 200, response.StatusCode)
			h.AssertEquals(t, c.expectedRules, payload["redirectRules"])
		})
	}
}
