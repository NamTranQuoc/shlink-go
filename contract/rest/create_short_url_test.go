package rest

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// createShortURL ports CreateShortUrlTest::createShortUrl. The body is copied, so callers can reuse it.
func createShortURL(t *testing.T, body h.M, apiKey, version string) (int, map[string]any) {
	t.Helper()
	b := h.M{}
	for k, v := range body {
		b[k] = v
	}
	if b["longUrl"] == nil {
		b["longUrl"] = "https://app.shlink.io"
	}
	if apiKey == "" {
		apiKey = "valid_api_key"
	}
	if version == "" {
		version = "2"
	}
	resp := h.CallAPIWithKeyNamed(t, http.MethodPost, "/rest/v"+version+"/short-urls", apiKey, h.Options{JSON: b})
	return resp.StatusCode, resp.JSON(t)
}

func TestCreateShortUrl_createsNewShortUrlWhenOnlyLongUrlIsProvided(t *testing.T) {
	h.Seed(t)
	expectedKeys := []string{"shortCode", "shortUrl", "longUrl", "dateCreated", "tags"}

	statusCode, payload := createShortURL(t, nil, "", "")

	h.AssertEquals(t, http.StatusOK, statusCode)
	for _, key := range expectedKeys {
		h.AssertHasKey(t, payload, key)
	}
}

func TestCreateShortUrl_lonUrlSupportsDifferentTypesOfSchemas(t *testing.T) {
	cases := []struct{ name, longURL string }{
		{"mailto", "mailto:foo@example.com"},
		{"file", "file:///foo/bar"},
		{"https", "https://example.com"},
		{"deeplink", "shlink://some/path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			statusCode, payload := createShortURL(t, h.M{"longUrl": c.longURL}, "", "")

			h.AssertEquals(t, http.StatusOK, statusCode)
			h.AssertEquals(t, c.longURL, payload["longUrl"])
		})
	}
}

func TestCreateShortUrl_createsNewShortUrlWithCustomSlug(t *testing.T) {
	h.Seed(t)

	statusCode, payload := createShortURL(t, h.M{"customSlug": "my cool slug"}, "", "")

	h.AssertEquals(t, http.StatusOK, statusCode)
	h.AssertEquals(t, "my-cool-slug", payload["shortCode"])
}

var conflictingSlugs = []struct {
	name, slug string
	domain     *string
}{
	{"without domain", "custom", nil},
	{"with domain", "custom-with-domain", h.Ptr("some-domain.com")},
}

func optional(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func TestCreateShortUrl_failsToCreateShortUrlWithDuplicatedSlug(t *testing.T) {
	for _, c := range conflictingSlugs {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			suffix := ""
			if c.domain != nil {
				suffix = fmt.Sprintf(` for domain "%s"`, *c.domain)
			}
			detail := fmt.Sprintf(`Provided slug "%s" is already in use%s.`, c.slug, suffix)

			statusCode, payload := createShortURL(t, h.M{"customSlug": c.slug, "domain": optional(c.domain)}, "", "")

			h.AssertEquals(t, http.StatusBadRequest, statusCode)
			h.AssertEquals(t, http.StatusBadRequest, payload["status"])
			h.AssertEquals(t, detail, payload["detail"])
			h.AssertEquals(t, "https://shlink.io/api/error/non-unique-slug", payload["type"])
			h.AssertEquals(t, "Invalid custom slug", payload["title"])
			h.AssertEquals(t, c.slug, payload["customSlug"])

			if c.domain != nil {
				h.AssertEquals(t, *c.domain, payload["domain"])
			} else {
				h.AssertNotHasKey(t, payload, "domain")
			}
		})
	}
}

func TestCreateShortUrl_expectedTypeIsReturnedForConflictingSlugBasedOnApiVersion(t *testing.T) {
	cases := []struct{ version, expectedType string }{
		{"1", "https://shlink.io/api/error/non-unique-slug"},
		{"2", "https://shlink.io/api/error/non-unique-slug"},
		{"3", "https://shlink.io/api/error/non-unique-slug"},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)

			_, payload := createShortURL(t, h.M{"customSlug": "custom"}, "", c.version)
			h.AssertEquals(t, c.expectedType, payload["type"])
		})
	}
}

func TestCreateShortUrl_createsNewShortUrlWithTags(t *testing.T) {
	cases := []struct {
		name                       string
		providedTags, expectedTags []string
	}{
		{"simple tags", []string{"foo", "bar", "baz"}, []string{"foo", "bar", "baz"}},
		{"tags with spaces", []string{"fo o", "  bar", "b az"}, []string{"fo-o", "bar", "b-az"}},
		{"tags with special chars", []string{"UUU", "Aäa"}, []string{"uuu", "aäa"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			statusCode, payload := createShortURL(t, h.M{"tags": c.providedTags}, "", "")

			h.AssertEquals(t, http.StatusOK, statusCode)
			h.AssertEquals(t, c.expectedTags, payload["tags"])
		})
	}
}

func TestCreateShortUrl_createsNewShortUrlWithVisitsLimit(t *testing.T) {
	for i, maxVisits := range []int{10, 11, 12, 13, 14, 15} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)

			statusCode, payload := createShortURL(t, h.M{"maxVisits": maxVisits}, "", "")
			shortCode, _ := payload["shortCode"].(string)

			h.AssertEquals(t, http.StatusOK, statusCode)

			// Last request to the short URL will return a 404, and the rest, a 302
			for range maxVisits {
				h.AssertEquals(t, http.StatusFound, h.CallShortURL(t, shortCode).StatusCode)
			}
			lastResp := h.CallShortURL(t, shortCode)
			h.AssertEquals(t, http.StatusNotFound, lastResp.StatusCode)
		})
	}
}

func TestCreateShortUrl_createsShortUrlWithValidSince(t *testing.T) {
	h.Seed(t)

	statusCode, payload := createShortURL(t, h.M{"validSince": h.AtomString(time.Now().AddDate(0, 0, 1))}, "", "")
	shortCode, _ := payload["shortCode"].(string)

	h.AssertEquals(t, http.StatusOK, statusCode)

	// Request to the short URL will return a 404 since it's not valid yet
	lastResp := h.CallShortURL(t, shortCode)
	h.AssertEquals(t, http.StatusNotFound, lastResp.StatusCode)
}

func TestCreateShortUrl_createsShortUrlWithValidUntil(t *testing.T) {
	h.Seed(t)

	statusCode, payload := createShortURL(t, h.M{"validUntil": h.AtomString(time.Now().AddDate(0, 0, -1))}, "", "")
	shortCode, _ := payload["shortCode"].(string)

	h.AssertEquals(t, http.StatusOK, statusCode)

	// Request to the short URL will return a 404 since it's no longer valid
	lastResp := h.CallShortURL(t, shortCode)
	h.AssertEquals(t, http.StatusNotFound, lastResp.StatusCode)
}

func TestCreateShortUrl_returnsAnExistingShortUrlWhenRequested(t *testing.T) {
	longURL := "https://www.alejandrocelaya.com"
	cases := []struct {
		name string
		body h.M
	}{
		{"only long URL", h.M{"longUrl": longURL}},
		{"long URL and tags", h.M{"longUrl": longURL, "tags": []string{"boo", "far"}}},
		{"long URL and custom slug", h.M{"longUrl": longURL, "customSlug": "my cool slug"}},
		{"several params", h.M{
			"longUrl":    longURL,
			"tags":       []string{"boo", "far"},
			"validSince": h.AtomString(time.Now()),
			"maxVisits":  7,
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			firstStatusCode, first := createShortURL(t, c.body, "", "")

			body := h.M{"findIfExists": true}
			for k, v := range c.body {
				body[k] = v
			}
			secondStatusCode, second := createShortURL(t, body, "", "")

			h.AssertEquals(t, http.StatusOK, firstStatusCode)
			h.AssertEquals(t, http.StatusOK, secondStatusCode)
			h.AssertEquals(t, first["shortCode"], second["shortCode"])
		})
	}
}

func TestCreateShortUrl_returnsErrorWhenRequestingReturnExistingButCustomSlugIsInUse(t *testing.T) {
	for _, c := range conflictingSlugs {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			longURL := "https://www.alejandrocelaya.com"

			firstStatusCode, _ := createShortURL(t, h.M{"longUrl": longURL}, "", "")
			secondStatusCode, _ := createShortURL(t, h.M{
				"longUrl":      longURL,
				"customSlug":   c.slug,
				"findIfExists": true,
				"domain":       optional(c.domain),
			}, "", "")

			h.AssertEquals(t, http.StatusOK, firstStatusCode)
			h.AssertEquals(t, http.StatusBadRequest, secondStatusCode)
		})
	}
}

func TestCreateShortUrl_createsNewShortUrlIfRequestedToFindButThereIsNoMatch(t *testing.T) {
	h.Seed(t)

	firstStatusCode, first := createShortURL(t, h.M{"longUrl": "https://www.alejandrocelaya.com"}, "", "")
	secondStatusCode, second := createShortURL(t, h.M{
		"longUrl":      "https://www.alejandrocelaya.com/projects",
		"findIfExists": true,
	}, "", "")

	h.AssertEquals(t, http.StatusOK, firstStatusCode)
	h.AssertEquals(t, http.StatusOK, secondStatusCode)
	if first["shortCode"] == second["shortCode"] {
		t.Fatalf("expected different short codes, got %v twice", first["shortCode"])
	}
}

func TestCreateShortUrl_failsToCreateShortUrlWithoutLongUrl(t *testing.T) {
	cases := []struct {
		name    string
		payload any
		version string
	}{
		// PHP's json_encode([]) is "[]", which is what Guzzle sends for an empty payload.
		{"missing long url v2", h.L{}, "2"},
		{"missing long url v3", h.L{}, "3"},
		{"empty long url v2", h.M{"longUrl": nil}, "2"},
		{"empty long url v3", h.M{"longUrl": "  "}, "3"},
		{"missing url schema v2", h.M{"longUrl": "foo.com"}, "2"},
		{"missing url schema v3", h.M{"longUrl": "foo.com"}, "3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallAPIWithKey(t, http.MethodPost, "/rest/v"+c.version+"/short-urls", h.Options{JSON: c.payload})
			payload := resp.JSON(t)

			h.AssertEquals(t, http.StatusBadRequest, resp.StatusCode)
			h.AssertEquals(t, http.StatusBadRequest, payload["status"])
			h.AssertEquals(t, "https://shlink.io/api/error/invalid-data", payload["type"])
			h.AssertEquals(t, "Provided data is not valid", payload["detail"])
			h.AssertEquals(t, "Invalid data", payload["title"])
		})
	}
}

func TestCreateShortUrl_defaultDomainIsDroppedIfProvided(t *testing.T) {
	h.Seed(t)

	createStatusCode, created := createShortURL(t, h.M{
		"longUrl": "https://www.alejandrocelaya.com",
		"domain":  "s.test",
	}, "", "")
	shortCode, _ := created["shortCode"].(string)
	getResp := h.CallAPIWithKey(t, http.MethodGet, "/short-urls/"+shortCode)
	payload := getResp.JSON(t)

	h.AssertEquals(t, http.StatusOK, createStatusCode)
	h.AssertEquals(t, http.StatusOK, getResp.StatusCode)
	h.AssertHasKey(t, payload, "domain")
	h.AssertEquals(t, nil, payload["domain"])
}

func TestCreateShortUrl_apiKeyDomainIsEnforced(t *testing.T) {
	cases := []struct {
		name           string
		providedDomain *string
	}{
		{"no domain", nil},
		{"invalid domain", h.Ptr("this-will-be-overwritten.com")},
		{"example domain", h.Ptr("example.com")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			statusCode, payload := createShortURL(t, h.M{"domain": optional(c.providedDomain)}, "domain_api_key", "")

			h.AssertEquals(t, http.StatusOK, statusCode)
			h.AssertEquals(t, "example.com", payload["domain"])
		})
	}
}

func TestCreateShortUrl_canCreateShortUrlsWithEmojis(t *testing.T) {
	h.Seed(t)

	statusCode, payload := createShortURL(t, h.M{
		"longUrl":    "https://emojipedia.org/fire/",
		"title":      "🔥🔥🔥",
		"customSlug": "🦣🦣🦣",
	}, "", "")
	h.AssertEquals(t, http.StatusOK, statusCode)
	h.AssertEquals(t, "🔥🔥🔥", payload["title"])
	h.AssertEquals(t, "🦣🦣🦣", payload["shortCode"])
	h.AssertEquals(t, "http://s.test/🦣🦣🦣", payload["shortUrl"])
}

func TestCreateShortUrl_titleIsCroppedIfTooLong(t *testing.T) {
	// str_pad('bar', 600, ' ', STR_PAD_BOTH) puts the odd padding character on the right.
	paddedBar := strings.Repeat(" ", 298) + "bar" + strings.Repeat(" ", 299)
	cases := []struct{ title, expectedTitle string }{
		{"foo", "foo"},
		{paddedBar, "bar"},
		{strings.Repeat("a", 511), strings.Repeat("a", 511)},
		{strings.Repeat("b", 512), strings.Repeat("b", 512)},
		{strings.Repeat("c", 513), strings.Repeat("c", 512)},
		{strings.Repeat("d", 600), strings.Repeat("d", 512)},
		{strings.Repeat("e", 800), strings.Repeat("e", 512)},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)

			statusCode, payload := createShortURL(t, h.M{"title": c.title}, "", "")

			h.AssertEquals(t, http.StatusOK, statusCode)
			h.AssertEquals(t, c.expectedTitle, payload["title"])
		})
	}
}

func TestCreateShortUrl_prefixCanBeSet(t *testing.T) {
	for i, customSlug := range []*string{nil, h.Ptr("my-custom-slug")} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)

			statusCode, payload := createShortURL(t, h.M{
				"longUrl":    "https://example.com",
				"pathPrefix": "foo/b  ar-baz",
				"customSlug": optional(customSlug),
			}, "", "")

			h.AssertEquals(t, http.StatusOK, statusCode)
			shortCode, _ := payload["shortCode"].(string)
			if !strings.HasPrefix(shortCode, "foo-b--ar-baz") {
				t.Fatalf("expected short code starting with %q, got %q", "foo-b--ar-baz", shortCode)
			}
		})
	}
}

func TestCreateShortUrl_failsToCreateShortUrlWithInvalidDomain(t *testing.T) {
	for i, domain := range []string{"localhost:80000", "127.0.0.1", "???/&%$&"} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)

			statusCode, payload := createShortURL(t, h.M{"domain": domain}, "", "")

			h.AssertEquals(t, http.StatusBadRequest, statusCode)
			h.AssertEquals(t, http.StatusBadRequest, payload["status"])
			h.AssertEquals(t, "https://shlink.io/api/error/invalid-data", payload["type"])
			h.AssertEquals(t, "Provided data is not valid", payload["detail"])
			h.AssertEquals(t, "Invalid data", payload["title"])
		})
	}
}
