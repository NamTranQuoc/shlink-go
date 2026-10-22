package harness

import (
	"fmt"
	"strconv"
	"strings"
)

// Param is one query parameter. Value may be nil (omitted), a string, a bool, an integer or a []string.
type Param struct {
	Key   string
	Value any
}

// Query is an ordered list of query parameters, encoded the way Guzzle's `query` request option does it:
// http_build_query with PHP_QUERY_RFC3986. Order is kept, nil values are skipped, booleans become 1/0, spaces become
// %20 and list values become key[0]=a&key[1]=b.
type Query []Param

// Encode returns the encoded query string, without the leading "?".
func (q Query) Encode() string {
	var parts []string
	for _, p := range q {
		key := rawURLEncode(p.Key)
		switch v := p.Value.(type) {
		case nil:
		case []string:
			for i, item := range v {
				parts = append(parts, rawURLEncode(fmt.Sprintf("%s[%d]", p.Key, i))+"="+rawURLEncode(item))
			}
		default:
			parts = append(parts, key+"="+rawURLEncode(scalarString(v)))
		}
	}
	return strings.Join(parts, "&")
}

func scalarString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case bool:
		if v {
			return "1"
		}
		return "0"
	case int:
		return strconv.Itoa(v)
	default:
		return fmt.Sprint(v)
	}
}

// rawURLEncode mirrors PHP's rawurlencode: everything but ALPHA / DIGIT / "-" / "." / "_" / "~" is percent-encoded.
func rawURLEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// WithQuery appends an encoded query to a path, like upstream's `(new Uri($path))->withQuery(Query::build($params))`.
func WithQuery(path string, q Query) string {
	if enc := q.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}

// Q builds a Query from alternating keys and values: Q("page", 2, "itemsPerPage", 3).
func Q(keyValues ...any) Query {
	if len(keyValues)%2 != 0 {
		panic("harness.Q needs an even number of arguments")
	}
	q := make(Query, 0, len(keyValues)/2)
	for i := 0; i < len(keyValues); i += 2 {
		q = append(q, Param{Key: keyValues[i].(string), Value: keyValues[i+1]})
	}
	return q
}
