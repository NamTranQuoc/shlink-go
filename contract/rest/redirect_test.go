package rest

import (
	"fmt"
	"net/http"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

const def456LongURL = "https://blog.alejandrocelaya.com/2017/12/09/acmailer-7-0-the-most-important-release-in-a-long-time/"

func TestRedirect_properRedirectHappensBasedOnRedirectRules(t *testing.T) {
	type tc struct {
		name             string
		options          h.Options
		expectedRedirect string
	}
	ua := func(userAgent string) h.Options {
		return h.Options{Headers: http.Header{"User-Agent": {userAgent}}}
	}
	lang := func(acceptLanguage string) h.Options {
		return h.Options{Headers: http.Header{"Accept-Language": {acceptLanguage}}}
	}
	cases := []tc{
		{"android", ua(h.AndroidUserAgent), "android://foo/bar"},
		{"ios", ua(h.IOSUserAgent), "fb://profile/33138223345"},
		{"linux", ua(h.LinuxUserAgent), "https://example.com/linux"},
		{"desktop", ua(h.WindowsUserAgent), def456LongURL},
		{"unknown", h.Options{}, def456LongURL},
		{"rule: english and foo", h.Options{
			Headers: http.Header{"Accept-Language": {"en-UK"}},
			Query:   h.Q("foo", "bar"),
		}, "https://example.com/english-and-foo-query?foo=bar"},
		{"rule: multiple query params", h.Options{
			Query: h.Q("foo", "bar", "hello", "world"),
		}, "https://example.com/multiple-query-params?foo=bar&hello=world"},
		{"rule: british english", lang("en-UK"), "https://example.com/only-english"},
		{"rule: english", lang("en"), "https://example.com/only-english"},
		{"rule: complex matching accept language", lang("fr-FR, es;q=0.9, en;q=0.9, *;q=0.2"),
			"https://example.com/only-english"},
		{"rule: too low quality accept language", lang("fr-FR, es;q=0.8, en;q=0.5, *;q=0.2"), def456LongURL},
	}
	for _, header := range h.IPAddressHeaders {
		value := "1.2.3.4"
		if header == "Forwarded" {
			value = "for=1.2.3.4"
		}
		cases = append(cases, tc{
			fmt.Sprintf(`rule: IP address in "%s" header`, header),
			h.Options{Headers: http.Header{header: {value}}},
			"https://example.com/static-ip-address",
		})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)

			resp := h.CallShortURL(t, "def456", c.options)

			h.AssertEquals(t, 302, resp.StatusCode)
			h.AssertEquals(t, c.expectedRedirect, headerLine(resp, "Location"))
		})
	}
}

func TestRedirect_properRedirectHappensForNonHttpLongUrls(t *testing.T) {
	for i, longURL := range []string{"android://foo/bar", "fb://profile/33138223345", "viber://pa?chatURI=1234"} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			h.Seed(t)
			slug := "non-http-schema"
			h.CallAPIWithKey(t, http.MethodPost, "/short-urls", h.Options{
				JSON: h.M{"longUrl": longURL, "customSlug": slug},
			})

			resp := h.CallShortURL(t, slug)

			h.AssertEquals(t, 302, resp.StatusCode)
			h.AssertEquals(t, longURL, headerLine(resp, "Location"))
		})
	}
}

func TestRedirect_queryParametersAreProperlyForwarded(t *testing.T) {
	h.Seed(t)
	slug := "forward-query-params"
	h.CallAPIWithKey(t, http.MethodPost, "/short-urls", h.Options{
		JSON: h.M{"longUrl": "https://example.com", "customSlug": slug, "forwardQuery": true},
	})

	resp := h.CallShortURL(t, slug, h.Options{Query: h.Q("foo bar", "123")})

	h.AssertEquals(t, 302, resp.StatusCode)
	h.AssertEquals(t, "https://example.com?foo%20bar=123", headerLine(resp, "Location"))
}
