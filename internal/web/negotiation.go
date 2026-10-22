package web

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// problemPriorities ports ProblemDetailsResponseFactory::NEGOTIATION_PRIORITIES.
var problemPriorities = []string{"application/json", "application/*+json", "application/xml", "application/*+xml"}

type accept struct {
	value   string
	base    string
	sub     string
	quality float64
	params  map[string]string
}

// parseAccept ports Negotiation\Accept; ok is false for invalid media types.
func parseAccept(value string) (accept, bool) {
	parts := strings.Split(value, ";")
	a := accept{value: value, quality: 1, params: map[string]string{}}
	for _, part := range parts[1:] {
		kv := strings.Split(part, "=")
		if len(kv) != 2 {
			continue
		}
		a.params[strings.ToLower(strings.TrimSpace(kv[0]))] = strings.Trim(kv[1], ` "`)
	}
	if q, ok := a.params["q"]; ok {
		a.quality = phpFloatCast(q)
		delete(a.params, "q")
	}
	typ := strings.ToLower(strings.TrimSpace(parts[0]))
	if typ == "*" {
		typ = "*/*"
	}
	bs := strings.Split(typ, "/")
	if len(bs) != 2 || bs[0] == "" || bs[0] == "0" || bs[1] == "" || bs[1] == "0" {
		return a, false
	}
	a.base, a.sub = bs[0], bs[1]
	return a, true
}

var leadingFloat = regexp.MustCompile(`^\s*[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?`)

// phpFloatCast ports PHP's (float) cast of a string: the longest numeric prefix, or 0.
func phpFloatCast(s string) float64 {
	m := leadingFloat.FindString(s)
	f, _ := strconv.ParseFloat(strings.TrimSpace(m), 64)
	return f
}

var acceptHeaderParts = regexp.MustCompile(`(?:[^,"]*(?:"[^"]*")?)+[^,"]*`)

func intersectCount(a, b map[string]string) int {
	n := 0
	for k, v := range a {
		if bv, ok := b[k]; ok && bv == v {
			n++
		}
	}
	return n
}

type acceptMatch struct {
	quality float64
	score   int
	index   int
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func splitPlus(sub string) (string, string) {
	before, after, found := strings.Cut(sub, "+")
	if !found {
		return sub, ""
	}
	return before, after
}

func matchAccept(a, p accept, index int) *acceptMatch {
	inter := intersectCount(a.params, p.params)
	baseEqual := strings.EqualFold(a.base, p.base)
	subEqual := strings.EqualFold(a.sub, p.sub)
	if (a.base == "*" || baseEqual) && (a.sub == "*" || subEqual) && inter == len(a.params) {
		return &acceptMatch{a.quality * p.quality, 100*b2i(baseEqual) + 10*b2i(subEqual) + inter, index}
	}
	if !strings.Contains(a.sub, "+") || !strings.Contains(p.sub, "+") {
		return nil
	}
	aSub, aPlus := splitPlus(a.sub)
	pSub, pPlus := splitPlus(p.sub)
	if !(a.base == "*" || baseEqual) || !(aSub == "*" || pSub == "*" || aPlus == "*" || pPlus == "*") {
		return nil
	}
	subEqual = strings.EqualFold(aSub, pSub)
	plusEqual := strings.EqualFold(aPlus, pPlus)
	if (aSub == "*" || pSub == "*" || subEqual) && (aPlus == "*" || pPlus == "*" || plusEqual) && inter == len(a.params) {
		return &acceptMatch{a.quality * p.quality, 100*b2i(baseEqual) + 10*b2i(subEqual) + b2i(plusEqual) + inter, index}
	}
	return nil
}

// negotiate ports Negotiation\Negotiator::getBest() (non-strict); it returns the best priority or "".
func negotiate(header string, priorities []string) string {
	var accepted []accept
	for _, part := range acceptHeaderParts.FindAllString(header, -1) {
		part = strings.TrimSpace(part)
		if part == "" || part == "0" {
			continue
		}
		if a, ok := parseAccept(part); ok {
			accepted = append(accepted, a)
		}
	}
	prios := make([]accept, len(priorities))
	for i, p := range priorities {
		prios[i], _ = parseAccept(p)
	}
	best := map[int]*acceptMatch{}
	for i, p := range prios {
		for _, a := range accepted {
			if m := matchAccept(a, p, i); m != nil {
				if cur, ok := best[i]; !ok || cur.score < m.score {
					best[i] = m
				}
			}
		}
	}
	if len(best) == 0 {
		return ""
	}
	matches := make([]*acceptMatch, 0, len(best))
	for _, m := range best {
		matches = append(matches, m)
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].quality != matches[j].quality {
			return matches[i].quality > matches[j].quality
		}
		return matches[i].index < matches[j].index
	})
	return priorities[matches[0].index]
}

// problemMediaType returns the problem details media type for an Accept header, or "" when none can be provided.
func problemMediaType(acceptHeader string) string {
	if acceptHeader == "" {
		acceptHeader = "*/*"
	}
	return negotiate(acceptHeader, problemPriorities)
}
