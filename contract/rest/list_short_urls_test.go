package rest

import (
	"fmt"
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

var (
	shortURLShlinkWithTitle = h.M{
		"shortCode":        "abc123",
		"shortUrl":         "http://s.test/abc123",
		"longUrl":          "https://shlink.io",
		"dateCreated":      "2018-05-01T00:00:00+00:00",
		"visitsSummary":    h.M{"total": 3, "nonBots": 3, "bots": 0},
		"tags":             h.L{"foo"},
		"meta":             h.M{"validSince": nil, "validUntil": nil, "maxVisits": 2},
		"domain":           nil,
		"title":            "My cool title",
		"crawlable":        true,
		"forwardQuery":     true,
		"hasRedirectRules": false,
	}
	shortURLDocs = h.M{
		"shortCode":        "ghi789",
		"shortUrl":         "http://s.test/ghi789",
		"longUrl":          "https://shlink.io/documentation/",
		"dateCreated":      "2018-05-01T00:00:00+00:00",
		"visitsSummary":    h.M{"total": 2, "nonBots": 2, "bots": 0},
		"tags":             h.L{},
		"meta":             h.M{"validSince": nil, "validUntil": "2020-05-01T00:00:00+00:00", "maxVisits": nil},
		"domain":           nil,
		"title":            nil,
		"crawlable":        false,
		"forwardQuery":     true,
		"hasRedirectRules": false,
	}
	shortURLCustomSlugAndDomain = h.M{
		"shortCode":        "custom-with-domain",
		"shortUrl":         "http://some-domain.com/custom-with-domain",
		"longUrl":          "https://google.com",
		"dateCreated":      "2018-10-20T00:00:00+00:00",
		"visitsSummary":    h.M{"total": 0, "nonBots": 0, "bots": 0},
		"tags":             h.L{},
		"meta":             h.M{"validSince": nil, "validUntil": nil, "maxVisits": nil},
		"domain":           "some-domain.com",
		"title":            nil,
		"crawlable":        false,
		"forwardQuery":     true,
		"hasRedirectRules": false,
	}
	shortURLMeta = h.M{
		"shortCode":        "def456",
		"shortUrl":         "http://s.test/def456",
		"longUrl":          "https://blog.alejandrocelaya.com/2017/12/09/acmailer-7-0-the-most-important-release-in-a-long-time/",
		"dateCreated":      "2019-01-01T00:00:10+00:00",
		"visitsSummary":    h.M{"total": 2, "nonBots": 1, "bots": 1},
		"tags":             h.L{"bar", "foo"},
		"meta":             h.M{"validSince": "2020-05-01T00:00:00+00:00", "validUntil": nil, "maxVisits": nil},
		"domain":           nil,
		"title":            nil,
		"crawlable":        false,
		"forwardQuery":     true,
		"hasRedirectRules": true,
	}
	shortURLCustomSlug = h.M{
		"shortCode":        "custom",
		"shortUrl":         "http://s.test/custom",
		"longUrl":          "https://shlink.io",
		"dateCreated":      "2019-01-01T00:00:20+00:00",
		"visitsSummary":    h.M{"total": 0, "nonBots": 0, "bots": 0},
		"tags":             h.L{},
		"meta":             h.M{"validSince": nil, "validUntil": nil, "maxVisits": 2},
		"domain":           nil,
		"title":            nil,
		"crawlable":        true,
		"forwardQuery":     false,
		"hasRedirectRules": false,
	}
	shortURLCustomDomain = h.M{
		"shortCode":        "ghi789",
		"shortUrl":         "http://example.com/ghi789",
		"longUrl":          "https://blog.alejandrocelaya.com/2019/04/27/considerations-to-properly-use-open-source-software-projects/",
		"dateCreated":      "2019-01-01T00:00:30+00:00",
		"visitsSummary":    h.M{"total": 0, "nonBots": 0, "bots": 0},
		"tags":             h.L{"foo"},
		"meta":             h.M{"validSince": nil, "validUntil": nil, "maxVisits": nil},
		"domain":           "example.com",
		"title":            nil,
		"crawlable":        false,
		"forwardQuery":     true,
		"hasRedirectRules": false,
	}
)

func TestListShortUrls_shortUrlsAreProperlyListed(t *testing.T) {
	// Chronos::parse('2018-12-01')->toAtomString(), with the UTC default timezone of the test env.
	const dec2018 = "2018-12-01T00:00:00+00:00"
	cases := []struct {
		query             h.Query
		expectedShortUrls h.L
		apiKey            string
	}{
		{h.Q(), h.L{shortURLCustomDomain, shortURLCustomSlug, shortURLMeta, shortURLCustomSlugAndDomain,
			shortURLShlinkWithTitle, shortURLDocs}, ""},
		{h.Q("excludePastValidUntil", "true"), h.L{shortURLCustomDomain, shortURLCustomSlug, shortURLMeta,
			shortURLCustomSlugAndDomain, shortURLShlinkWithTitle}, ""},
		{h.Q("excludeMaxVisitsReached", "true"), h.L{shortURLCustomDomain, shortURLCustomSlug, shortURLMeta,
			shortURLCustomSlugAndDomain, shortURLDocs}, ""},
		{h.Q("orderBy", "shortCode"), h.L{shortURLShlinkWithTitle, shortURLCustomSlug, shortURLCustomSlugAndDomain,
			shortURLMeta, shortURLDocs, shortURLCustomDomain}, ""},
		{h.Q("orderBy", "shortCode-DESC"), h.L{shortURLDocs, shortURLCustomDomain, shortURLMeta,
			shortURLCustomSlugAndDomain, shortURLCustomSlug, shortURLShlinkWithTitle}, ""},
		{h.Q("orderBy", "title-DESC"), h.L{shortURLShlinkWithTitle, shortURLMeta, shortURLCustomSlug, shortURLDocs,
			shortURLCustomDomain, shortURLCustomSlugAndDomain}, ""},
		{h.Q("startDate", dec2018), h.L{shortURLCustomDomain, shortURLCustomSlug, shortURLMeta}, ""},
		{h.Q("endDate", dec2018), h.L{shortURLCustomSlugAndDomain, shortURLShlinkWithTitle, shortURLDocs}, ""},
		{h.Q("tags", []string{"foo"}), h.L{shortURLCustomDomain, shortURLMeta, shortURLShlinkWithTitle}, ""},
		{h.Q("tags", []string{"bar"}), h.L{shortURLMeta}, ""},
		{h.Q("tags", []string{"foo", "bar"}), h.L{shortURLCustomDomain, shortURLMeta, shortURLShlinkWithTitle}, ""},
		{h.Q("tags", []string{"foo", "bar"}, "tagsMode", "any"),
			h.L{shortURLCustomDomain, shortURLMeta, shortURLShlinkWithTitle}, ""},
		{h.Q("tags", []string{"foo", "bar"}, "tagsMode", "all"), h.L{shortURLMeta}, ""},
		{h.Q("tags", []string{"foo", "bar", "baz"}), h.L{shortURLCustomDomain, shortURLMeta, shortURLShlinkWithTitle}, ""},
		{h.Q("tags", []string{"foo", "bar", "baz"}, "tagsMode", "all"), h.L{}, ""},
		{h.Q("tags", []string{"foo"}, "endDate", dec2018), h.L{shortURLShlinkWithTitle}, ""},
		{h.Q("searchTerm", "alejandro"), h.L{shortURLCustomDomain, shortURLMeta}, ""},
		{h.Q("searchTerm", "cool"), h.L{shortURLShlinkWithTitle}, ""},
		{h.Q("searchTerm", "example.com"), h.L{shortURLCustomDomain}, ""},
		{h.Q("domain", "example.com"), h.L{shortURLCustomDomain}, ""},
		{h.Q("domain", "DEFAULT"), h.L{shortURLCustomSlug, shortURLMeta, shortURLShlinkWithTitle, shortURLDocs}, ""},

		// Exclude tags
		{h.Q("excludeTags", []string{"foo"}), h.L{shortURLCustomSlug, shortURLCustomSlugAndDomain, shortURLDocs}, ""},
		{h.Q("excludeTags", []string{"foo", "bar"}), h.L{shortURLCustomSlug, shortURLCustomSlugAndDomain, shortURLDocs}, ""},
		{h.Q("excludeTags", []string{"bar", "foo"}, "excludeTagsMode", "all"), h.L{shortURLCustomDomain,
			shortURLCustomSlug, shortURLCustomSlugAndDomain, shortURLShlinkWithTitle, shortURLDocs}, ""},

		// Filter by API key name
		{h.Q("apiKeyName", "author_api_key"), h.L{shortURLCustomSlug, shortURLMeta, shortURLShlinkWithTitle}, ""},
		{h.Q("apiKeyName", "invalid"), h.L{}, ""},
		// If the author_api_key is used, the `apiKeyName` param is ignored
		{h.Q("apiKeyName", "valid_api_key"), h.L{shortURLCustomSlug, shortURLMeta, shortURLShlinkWithTitle},
			"author_api_key"},
		// If the domain_api_key is used, the `apiKeyName` param is ignored
		{h.Q("apiKeyName", "valid_api_key"), h.L{shortURLCustomDomain}, "domain_api_key"},

		// Different API keys
		{h.Q(), h.L{shortURLCustomSlug, shortURLMeta, shortURLShlinkWithTitle}, "author_api_key"},
		{h.Q(), h.L{shortURLCustomDomain}, "domain_api_key"},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)
			apiKey := c.apiKey
			if apiKey == "" {
				apiKey = "valid_api_key"
			}

			resp := h.CallAPIWithKeyNamed(t, http.MethodGet, "/short-urls", apiKey, h.Options{Query: c.query})
			respPayload := resp.JSON(t)

			h.AssertEquals(t, http.StatusOK, resp.StatusCode)
			h.AssertEquals(t, h.M{
				"shortUrls": h.M{
					"data":       c.expectedShortUrls,
					"pagination": buildPagination(len(c.expectedShortUrls)),
				},
			}, respPayload)
		})
	}
}

func buildPagination(itemsCount int) h.M {
	return h.M{
		"currentPage":        1,
		"pagesCount":         1,
		"itemsPerPage":       10,
		"itemsInCurrentPage": itemsCount,
		"totalItems":         itemsCount,
	}
}

func TestListShortUrls_errorIsReturnedWhenProvidingInvalidValues(t *testing.T) {
	cases := []struct {
		query                   h.Query
		expectedInvalidElements []string
	}{
		{h.Q("tagsMode", "invalid"), []string{"tagsMode"}},
		{h.Q("orderBy", "invalid"), []string{"orderBy"}},
		{h.Q("orderBy", "invalid", "tagsMode", "invalid"), []string{"orderBy", "tagsMode"}},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKey(t, http.MethodGet, "/short-urls", h.Options{Query: c.query})
			respPayload := resp.JSON(t)

			h.AssertEquals(t, 400, resp.StatusCode)
			h.AssertEquals(t, h.M{
				"invalidElements": c.expectedInvalidElements,
				"title":           "Invalid data",
				"type":            "https://shlink.io/api/error/invalid-data",
				"status":          400,
				"detail":          "Provided data is not valid",
			}, respPayload)
		})
	}
}
