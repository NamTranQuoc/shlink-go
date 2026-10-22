package harness

import (
	"net/http"
	"testing"
)

// M is shorthand for JSON objects in expected values.
type M = map[string]any

// L is shorthand for JSON lists in expected values.
type L = []any

// InvalidURLCase is one case of upstream's ApiTestDataProviders::invalidUrlsProvider.
type InvalidURLCase struct {
	Name           string
	ShortCode      string
	Domain         *string
	ExpectedDetail string
	APIKey         string
}

// Ptr returns a pointer to v, for optional values in data providers.
func Ptr[T any](v T) *T { return &v }

// InvalidURLs ports module/Rest/test-api/Utils/ApiTestDataProviders::invalidUrlsProvider.
var InvalidURLs = []InvalidURLCase{
	{"invalid shortcode", "invalid", nil, `No URL found with short code "invalid"`, "valid_api_key"},
	{"invalid shortcode without domain", "abc123", Ptr("example.com"),
		`No URL found with short code "abc123" for domain "example.com"`, "valid_api_key"},
	{"invalid shortcode and custom domain", "custom-with-domain", Ptr("example.com"),
		`No URL found with short code "custom-with-domain" for domain "example.com"`, "valid_api_key"},
	{"valid shortcode with invalid API key", "ghi789", nil, `No URL found with short code "ghi789"`, "author_api_key"},
	{"valid shortcode and custom domain with invalid API key", "custom-with-domain", Ptr("some-domain.com"),
		`No URL found with short code "custom-with-domain" for domain "some-domain.com"`, "domain_api_key"},
}

// DomainOrNil converts an optional domain into the value PHP would compare against (string or null).
func (c InvalidURLCase) DomainOrNil() any {
	if c.Domain == nil {
		return nil
	}
	return *c.Domain
}

// BuildShortURLPath ports module/Rest/test-api/Utils/UrlBuilder::buildShortUrlPath.
func BuildShortURLPath(shortCode string, domain *string, suffix string) string {
	path := "/short-urls/" + shortCode + suffix
	if domain == nil {
		return path
	}
	return WithQuery(path, Q("domain", *domain))
}

// AssertShortURLNotFound holds the assertions upstream repeats for every invalidUrlsProvider case.
func AssertShortURLNotFound(t testing.TB, resp *Response, c InvalidURLCase) {
	t.Helper()
	payload := resp.JSON(t)
	AssertEquals(t, http.StatusNotFound, resp.StatusCode, "status code")
	AssertEquals(t, http.StatusNotFound, payload["status"], "status")
	AssertEquals(t, "https://shlink.io/api/error/short-url-not-found", payload["type"], "type")
	AssertEquals(t, c.ExpectedDetail, payload["detail"], "detail")
	AssertEquals(t, "Short URL not found", payload["title"], "title")
	AssertEquals(t, c.ShortCode, payload["shortCode"], "shortCode")
	AssertEquals(t, c.DomainOrNil(), payload["domain"], "domain")
}
