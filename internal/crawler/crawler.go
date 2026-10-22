// Package crawler is a port of jaybizzle/crawler-detect v1.4.2, as Shlink uses it to flag visits
// as potential bots (isCrawler in module/Core/functions/functions.php).
package crawler

import (
	"regexp/syntax"
	"strings"
	"sync"

	"github.com/NamTranQuoc/shlink-go/internal/pcre"
)

// IsCrawler reports whether userAgent belongs to a crawler, like
// (new CrawlerDetect())->isCrawler($userAgent).
//
// CrawlerDetect removes the exclusion patterns from the user agent, trims it, and matches what is
// left against the crawler patterns, all case-insensitively.
func IsCrawler(userAgent string) bool {
	// "" and "0" are falsy in PHP, so isCrawler() falls back to the user agent CrawlerDetect read from
	// $_SERVER when it was built. Shlink's long-running process has no HTTP_* entries there, so that
	// fallback is empty and the result is false.
	if userAgent == "" || userAgent == "0" {
		return false
	}
	d := getDetector()
	agent, ok := d.exclusions.RemoveAll(userAgent)
	if !ok { // preg_replace() returned null
		return false
	}
	agent = trimPHP(agent)
	if agent == "" {
		return false
	}
	return d.matches(agent)
}

var getDetector = sync.OnceValue(func() *detector {
	return newDetector(crawlerPatterns, exclusionPatterns)
})

// yandexPattern is the only crawler pattern regexp/syntax cannot parse (negative lookahead). It is
// matched by hasYandexNotFollowedBySearch instead.
const yandexPattern = `Yandex(?!Search)`

type detector struct {
	exclusions *pcre.Regexp
	literals   *literalMatcher
	residual   *pcre.Regexp // nil when every crawler pattern is a literal
	yandex     bool
}

// newDetector compiles the crawler-detect pattern lists. PHP matches the crawler list as one
// alternation, but only asks whether it matches, so each alternative can be checked on its own:
// literal alternatives (optionally anchored with ^ or $) go into an Aho-Corasick automaton, the
// others into a pcre.Regexp, and the lookahead pattern into hand-written code. "$" in a literal
// alternative is checked as the end of the text: PCRE also accepts a final "\n" there, but the agent
// is trimmed before matching.
func newDetector(crawlers, exclusions []string) *detector {
	excl := &syntax.Regexp{Op: syntax.OpAlternate}
	for _, p := range exclusions {
		re, err := pcre.Parse(p, pcre.Caseless)
		if err != nil {
			panic("crawler: " + err.Error())
		}
		excl.Sub = append(excl.Sub, re)
	}
	d := &detector{exclusions: mustCompileSyntax(excl)}
	lb := newLiteralBuilder()
	var residual []*syntax.Regexp
	for _, p := range crawlers {
		if p == yandexPattern {
			d.yandex = true
			continue
		}
		re, err := pcre.Parse(p, pcre.Caseless)
		if err != nil {
			panic("crawler: " + err.Error())
		}
		residual = append(residual, classify(re, lb)...)
	}
	d.literals = lb.build()
	if len(residual) > 0 {
		d.residual = mustCompileSyntax(&syntax.Regexp{Op: syntax.OpAlternate, Sub: residual})
	}
	return d
}

func (d *detector) matches(agent string) bool {
	if d.literals.matches(agent) {
		return true
	}
	if d.yandex && hasYandexNotFollowedBySearch(agent) {
		return true
	}
	return d.residual != nil && d.residual.MatchString(agent)
}

func mustCompileSyntax(re *syntax.Regexp) *pcre.Regexp {
	compiled, err := pcre.CompileSyntax(re)
	if err != nil {
		panic("crawler: " + err.Error())
	}
	return compiled
}

// hasYandexNotFollowedBySearch is /Yandex(?!Search)/i: some "yandex" not followed by "search".
func hasYandexNotFollowedBySearch(s string) bool {
	for i := 0; i+6 <= len(s); i++ {
		if hasPrefixFoldASCII(s[i:], "yandex") && !hasPrefixFoldASCII(s[i+6:], "search") {
			return true
		}
	}
	return false
}

// hasPrefixFoldASCII reports whether s starts with the lowercase ASCII prefix, folding A-Z only.
func hasPrefixFoldASCII(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if lowerASCII(s[i]) != prefix[i] {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// trimPHP is PHP's trim() with its default character list " \t\n\r\0\x0B".
func trimPHP(s string) string {
	return strings.Trim(s, " \t\n\r\x00\x0B")
}

// classify adds the literal alternatives of re to lb and returns the others.
func classify(re *syntax.Regexp, lb *literalBuilder) []*syntax.Regexp {
	if re.Op == syntax.OpAlternate {
		var rest []*syntax.Regexp
		for _, sub := range re.Sub {
			rest = append(rest, classify(sub, lb)...)
		}
		return rest
	}
	parts := []*syntax.Regexp{re}
	if re.Op == syntax.OpConcat {
		parts = re.Sub
	}
	start, end := false, false
	if len(parts) > 0 && parts[0].Op == syntax.OpBeginText {
		start, parts = true, parts[1:]
	}
	if len(parts) > 0 && parts[len(parts)-1].Op == syntax.OpEndText {
		end, parts = true, parts[:len(parts)-1]
	}
	// An unanchored search for X*Y matches exactly when one for Y does (X* can match nothing), and
	// likewise for YX*.
	if !start && len(parts) > 1 && parts[0].Op == syntax.OpStar {
		parts = parts[1:]
	}
	if !end && len(parts) > 1 && parts[len(parts)-1].Op == syntax.OpStar {
		parts = parts[:len(parts)-1]
	}
	set, ok := literalSetOfConcat(parts)
	if ok {
		for _, lit := range set {
			if lit == "" {
				ok = false
			}
		}
	}
	if !ok {
		return []*syntax.Regexp{re}
	}
	for _, lit := range set {
		lb.add(lit, start, end)
	}
	return nil
}

const maxLiteralSet = 64

// literalSetOfConcat returns the lowercased strings the concatenation matches, when that is a small
// finite set of ASCII strings matched case-insensitively.
func literalSetOfConcat(parts []*syntax.Regexp) ([]string, bool) {
	set := []string{""}
	for _, p := range parts {
		sub, ok := literalSet(p)
		if !ok || len(set)*len(sub) > maxLiteralSet {
			return nil, false
		}
		next := make([]string, 0, len(set)*len(sub))
		for _, a := range set {
			for _, b := range sub {
				next = append(next, a+b)
			}
		}
		set = next
	}
	return set, true
}

func literalSet(re *syntax.Regexp) ([]string, bool) {
	switch re.Op {
	case syntax.OpEmptyMatch:
		return []string{""}, true
	case syntax.OpLiteral:
		b := make([]byte, 0, len(re.Rune))
		for _, r := range re.Rune {
			if r >= 0x80 {
				return nil, false
			}
			c := byte(r)
			if lowerASCII(c) != c || ('a' <= c && c <= 'z') {
				if re.Flags&syntax.FoldCase == 0 {
					return nil, false
				}
			}
			b = append(b, lowerASCII(c))
		}
		return []string{string(b)}, true
	case syntax.OpCharClass:
		// Folded classes also list U+017F and U+212A, which no single byte can be. A class reaching
		// into 0x80-0xFF (a negated one) also matches those bytes, which the automaton does not model.
		seen := map[byte]bool{}
		var set []string
		for i := 0; i < len(re.Rune); i += 2 {
			if re.Rune[i] < 0x100 && re.Rune[i+1] >= 0x80 {
				return nil, false
			}
			for r := re.Rune[i]; r <= re.Rune[i+1] && r < 0x80; r++ {
				c := lowerASCII(byte(r))
				if !seen[c] {
					seen[c] = true
					set = append(set, string(c))
				}
				if len(set) > maxLiteralSet {
					return nil, false
				}
			}
		}
		// A class with letters must hold both cases of each, or folding would widen it.
		for c := range seen {
			if 'a' <= c && c <= 'z' && (!classHas(re, rune(c)) || !classHas(re, rune(c-'a'+'A'))) {
				return nil, false
			}
		}
		return set, true
	case syntax.OpCapture:
		return literalSet(re.Sub[0])
	case syntax.OpConcat:
		return literalSetOfConcat(re.Sub)
	case syntax.OpQuest:
		sub, ok := literalSet(re.Sub[0])
		if !ok {
			return nil, false
		}
		return append([]string{""}, sub...), true
	case syntax.OpAlternate:
		var set []string
		for _, sub := range re.Sub {
			s, ok := literalSet(sub)
			if !ok || len(set)+len(s) > maxLiteralSet {
				return nil, false
			}
			set = append(set, s...)
		}
		return set, true
	}
	return nil, false
}

func classHas(re *syntax.Regexp, r rune) bool {
	for i := 0; i < len(re.Rune); i += 2 {
		if re.Rune[i] <= r && r <= re.Rune[i+1] {
			return true
		}
	}
	return false
}
