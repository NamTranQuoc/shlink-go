package crawler

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

type goldenCase struct {
	UA      string `json:"ua"`
	B64     string `json:"b64"`
	Crawler bool   `json:"crawler"`
}

func (c goldenCase) userAgent(t testing.TB) string {
	if c.B64 == "" {
		return c.UA
	}
	b, err := base64.StdEncoding.DecodeString(c.B64)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// loadGolden reads testdata/golden.json, written by testdata/gen/gen.php from PHP's CrawlerDetect.
// CRAWLER_GOLDEN points the test at another file produced by the same script.
func loadGolden(t testing.TB) []goldenCase {
	path := os.Getenv("CRAWLER_GOLDEN")
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

func TestIsCrawlerMatchesPHP(t *testing.T) {
	cases := loadGolden(t)
	failures := 0
	for _, c := range cases {
		ua := c.userAgent(t)
		if got := IsCrawler(ua); got != c.Crawler {
			failures++
			if failures <= 20 {
				t.Errorf("IsCrawler(%q) = %v, PHP says %v", truncate(ua), got, c.Crawler)
			}
		}
	}
	t.Logf("%d user agents, %d mismatches", len(cases), failures)
}

// The tests below check the byte engine, the automaton and the residual split against Go's regexp
// as an independent oracle. Go's regexp decodes UTF-8 and folds Unicode, so its input gets every
// byte >= 0x80 mapped to its own private-use rune first; on that input both read the patterns the
// same way PCRE does without /u.

func toByteRunes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x80 {
			b.WriteByte(c)
		} else {
			b.WriteRune(rune(0xE000 + int(c)))
		}
	}
	return b.String()
}

func fromByteRunes(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0xE080 && r <= 0xE0FF {
			b.WriteByte(byte(r - 0xE000))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func oracleInputs(t *testing.T) []string {
	var inputs []string
	for _, c := range loadGolden(t) {
		inputs = append(inputs, c.userAgent(t))
	}
	// Every pattern's own text, and variants around it, exercise each alternative and its anchors.
	unescape := strings.NewReplacer(`\/`, "/", `\.`, ".", `\(`, "(", `\)`, ")", `\+`, "+", `\?`, "?", `\\`, `\`, "^", "", "$", "")
	for _, p := range append(append([]string(nil), crawlerPatterns...), exclusionPatterns...) {
		lit := unescape.Replace(p)
		inputs = append(inputs, lit, strings.ToUpper(lit), "x"+lit, lit+"x", "x"+lit+"x", lit+"\n", lit[:len(lit)/2], lit[1:], lit+"9.9_9/1.")
	}
	return inputs
}

func TestExclusionsEqualOracle(t *testing.T) {
	oracle := regexp.MustCompile(`(?i)(?:` + strings.Join(exclusionPatterns, "|") + `)`)
	d := getDetector()
	for _, s := range oracleInputs(t) {
		want := fromByteRunes(oracle.ReplaceAllLiteralString(toByteRunes(s), ""))
		if got, _ := d.exclusions.RemoveAll(s); got != want {
			t.Errorf("removeAll(%q) = %q, oracle %q", truncate(s), truncate(got), truncate(want))
		}
	}
}

func TestMatcherEqualsOracle(t *testing.T) {
	var patterns []string
	for _, p := range crawlerPatterns {
		if p != yandexPattern {
			patterns = append(patterns, p)
		}
	}
	oracle := regexp.MustCompile(`(?i)(?:` + strings.Join(patterns, "|") + `)`)
	d := newDetector(patterns, exclusionPatterns)
	for _, ua := range oracleInputs(t) {
		stripped, _ := d.exclusions.RemoveAll(ua)
		for _, s := range []string{ua, trimPHP(stripped)} {
			if s == "" || strings.HasSuffix(s, "\n") {
				continue // the agent is never empty nor ends with "\n" when matched
			}
			if got, want := d.matches(s), oracle.MatchString(toByteRunes(s)); got != want {
				t.Errorf("matches(%q) = %v, oracle %v", truncate(s), got, want)
			}
		}
	}
}

func TestYandexLookahead(t *testing.T) {
	for s, want := range map[string]bool{
		"Yandex": true, "yandex": true, "YandexSearch": false, "YANDEXsearch/1": false, "YandexSearc": true,
		"YandexSearchYandex": true, "YandexYandexSearch": true, "xyandexsearch": false, "Yande": false,
	} {
		if got := hasYandexNotFollowedBySearch(s); got != want {
			t.Errorf("hasYandexNotFollowedBySearch(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestLookaheadPatternRegistered(t *testing.T) {
	if !getDetector().yandex {
		t.Fatal("the lookahead pattern was not registered")
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

func BenchmarkIsCrawler(b *testing.B) {
	getDetector()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		IsCrawler(benchAgents[i%len(benchAgents)])
	}
}

func BenchmarkIsCrawlerBrowser(b *testing.B) {
	getDetector()
	b.ReportAllocs()
	for b.Loop() {
		IsCrawler(benchAgents[0])
	}
}
