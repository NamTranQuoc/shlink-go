package useragent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/NamTranQuoc/shlink-go/internal/pcre"
)

type goldenCase struct {
	UA            string       `json:"ua"`
	B64           string       `json:"b64"`
	Platform      *string      `json:"platform"`
	PlatformB64   string       `json:"platformB64"`
	Browser       *string      `json:"browser"`
	BrowserB64    string       `json:"browserB64"`
	Version       *string      `json:"version"`
	VersionB64    string       `json:"versionB64"`
	Devices       []DeviceType `json:"devices"`
	ShlinkBrowser *Browser     `json:"shlinkBrowser"`
}

func decode(t testing.TB, s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (c goldenCase) userAgent(t testing.TB) string {
	if c.B64 != "" {
		return decode(t, c.B64)
	}
	return c.UA
}

func expected(t testing.TB, v *string, b64 string) nullable {
	if b64 != "" {
		return str(decode(t, b64))
	}
	if v == nil {
		return nullable{}
	}
	return str(*v)
}

// loadGolden reads testdata/golden.json, written by testdata/gen/gen.php from PHP's UserAgentParser
// and Shlink's models. UA_GOLDEN points the test at another file produced by the same script.
func loadGolden(t testing.TB) []goldenCase {
	path := os.Getenv("UA_GOLDEN")
	if path == "" {
		path = "testdata/golden.json"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestParseMatchesPHP(t *testing.T) {
	cases := loadGolden(t)
	failures := 0
	for _, c := range cases {
		ua := c.userAgent(t)
		want := result{
			platform: expected(t, c.Platform, c.PlatformB64),
			browser:  expected(t, c.Browser, c.BrowserB64),
			version:  expected(t, c.Version, c.VersionB64),
		}
		got := parse(ua)
		devices := DeviceTypesFromUserAgent(ua)
		browser, ok := BrowserFromUserAgent(ua)
		var wantBrowser Browser
		if c.ShlinkBrowser != nil {
			wantBrowser = *c.ShlinkBrowser
		}
		if got != want || !slices.Equal(devices, c.Devices) || ok != (c.ShlinkBrowser != nil) || browser != wantBrowser {
			failures++
			if failures <= 20 {
				t.Errorf("%q:\n got  %+v %v %q\n want %+v %v %q", truncate(ua), got, devices, browser, want, c.Devices, wantBrowser)
			}
		}
	}
	t.Logf("%d user agents, %d mismatches", len(cases), failures)
}

func TestParseFields(t *testing.T) {
	ua := Parse("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.3856.62")
	if ua != (UserAgent{Platform: PlatformWindows, Browser: BrowserNameEdge, BrowserVersion: "146.0.3856.62"}) {
		t.Errorf("got %+v", ua)
	}
	if ua := Parse(""); ua != (UserAgent{}) {
		t.Errorf("empty: got %+v", ua)
	}
}

func TestCapsHoldEveryGroup(t *testing.T) {
	for _, re := range []*pcre.Regexp{parenRe, platformRe, androidRe, browserRe, contactBotRe, leadingBotRe, commentBotRe, rvRe, playStationRe} {
		if re.NumSubexp() > maxGroups {
			t.Errorf("a pattern has %d groups, more than maxGroups", re.NumSubexp())
		}
	}
}

func truncate(s string) string {
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

var benchAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 16_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/109.0 Mobile/15E148 Safari/605.1.15",
	"Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/109.0.5414.86 Mobile Safari/537.36",
	"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
	"curl/8.5.0",
}

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		Parse(benchAgents[i%len(benchAgents)])
	}
}

func BenchmarkParseBrowser(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		Parse(benchAgents[0])
	}
}
