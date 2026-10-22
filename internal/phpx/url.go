// Package phpx ports the small pieces of PHP and PHP-library behavior whose exact output is observable through
// Shlink's API: URL encoding, Guzzle's query and URI handling, and similar helpers.
package phpx

import (
	"strconv"
	"strings"
)

const upperHex = "0123456789ABCDEF"

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// RawURLEncode ports PHP's rawurlencode() (RFC 3986).
func RawURLEncode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isAlnum(c) || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperHex[c>>4])
		b.WriteByte(upperHex[c&15])
	}
	return b.String()
}

// URLEncode ports PHP's urlencode() (application/x-www-form-urlencoded).
func URLEncode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case isAlnum(c) || c == '-' || c == '_' || c == '.':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&15])
		}
	}
	return b.String()
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// RawURLDecode ports PHP's rawurldecode(): invalid escapes are left untouched.
func RawURLDecode(s string) string {
	return decode(s, false)
}

// URLDecode ports PHP's urldecode(): like rawurldecode() but also turns '+' into a space.
func URLDecode(s string) string {
	return decode(s, true)
}

func decode(s string, plusAsSpace bool) string {
	if !strings.ContainsAny(s, "%+") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '+' && plusAsSpace {
			b.WriteByte(' ')
			continue
		}
		if c == '%' && i+2 < len(s) {
			h, ok1 := unhex(s[i+1])
			l, ok2 := unhex(s[i+2])
			if ok1 && ok2 {
				b.WriteByte(h<<4 | l)
				i += 2
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// QueryParam is one key of a parsed query string. Repeated keys collect all their values in order. A nil value means
// the key had no `=` at all.
type QueryParam struct {
	Key    string
	Values []*string
	// IsList is true when the key was repeated (PHP turns the value into an array).
	IsList bool
}

// Query is an ordered query string, like the PHP array Guzzle's Query::parse() returns.
type Query []QueryParam

func (q Query) index(key string) int {
	key = normalizeArrayKey(key)
	for i, p := range q {
		if normalizeArrayKey(p.Key) == key {
			return i
		}
	}
	return -1
}

// normalizeArrayKey mirrors PHP array key casting: decimal integer strings become integers, so "01" and "1" differ but
// "1" always matches "1".
func normalizeArrayKey(key string) string {
	if n, err := strconv.ParseInt(key, 10, 64); err == nil && strconv.FormatInt(n, 10) == key {
		return "\x00int:" + key
	}
	return key
}

// Get returns the first value of a key and whether the key exists.
func (q Query) Get(key string) (*string, bool) {
	i := q.index(key)
	if i < 0 {
		return nil, false
	}
	p := q[i]
	if p.IsList {
		// PHP would return an array; callers comparing to strings never match it.
		return nil, true
	}
	return p.Values[0], true
}

// Has tells if the key exists.
func (q Query) Has(key string) bool { return q.index(key) >= 0 }

// Without returns a copy without the key.
func (q Query) Without(key string) Query {
	i := q.index(key)
	if i < 0 {
		return q
	}
	out := make(Query, 0, len(q)-1)
	out = append(out, q[:i]...)
	return append(out, q[i+1:]...)
}

// ParseQuery ports GuzzleHttp\Psr7\Query::parse($str) with the default url-encoding: '+' is a space.
func ParseQuery(s string) Query {
	var out Query
	if s == "" {
		return out
	}
	for _, kvp := range strings.Split(s, "&") {
		key, value, hasValue := strings.Cut(kvp, "=")
		key = RawURLDecode(strings.ReplaceAll(key, "+", " "))
		var v *string
		if hasValue {
			decoded := RawURLDecode(strings.ReplaceAll(value, "+", " "))
			v = &decoded
		}
		if i := out.index(key); i >= 0 {
			out[i].Values = append(out[i].Values, v)
			out[i].IsList = true
			continue
		}
		out = append(out, QueryParam{Key: key, Values: []*string{v}})
	}
	return out
}

// BuildQuery ports GuzzleHttp\Psr7\Query::build() with RFC 3986 encoding.
func BuildQuery(q Query) string {
	var b strings.Builder
	for _, p := range q {
		k := RawURLEncode(p.Key)
		for _, v := range p.Values {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			b.WriteString(k)
			if v != nil {
				b.WriteByte('=')
				b.WriteString(RawURLEncode(*v))
			}
		}
	}
	return b.String()
}

// MergeQueries ports Laminas ArrayUtils::merge($a, $b, preserveNumericKeys: true) for parsed queries: keys of b
// overwrite keys of a in place, new keys are appended, and when both values are lists they are merged by index.
func MergeQueries(a, b Query) Query {
	out := make(Query, len(a))
	copy(out, a)
	for _, p := range b {
		i := out.index(p.Key)
		if i < 0 {
			out = append(out, p)
			continue
		}
		existing := out[i]
		if existing.IsList && p.IsList {
			merged := append([]*string{}, existing.Values...)
			for j, v := range p.Values {
				if j < len(merged) {
					merged[j] = v
				} else {
					merged = append(merged, v)
				}
			}
			out[i] = QueryParam{Key: existing.Key, Values: merged, IsList: true}
			continue
		}
		out[i] = QueryParam{Key: existing.Key, Values: p.Values, IsList: p.IsList}
	}
	return out
}
