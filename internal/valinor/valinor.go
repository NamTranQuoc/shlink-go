// Package valinor reproduces the subset of CuyZ\Valinor's mapping rules Shlink relies on (scalar value casting enabled,
// superfluous keys allowed), so request payloads are accepted or rejected exactly like upstream.
package valinor

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Input is a decoded payload: a JSON body decoded with UseNumber, or query params (strings and nested maps/lists).
type Input map[string]any

// Errors collects mapping errors in parameter order. Names are strings, except list indexes which upstream reports
// as integers.
type Errors struct {
	names    []any
	messages []string
}

// Add records an error for a node.
func (e *Errors) Add(name any, message string) {
	e.names = append(e.names, name)
	e.messages = append(e.messages, message)
}

// Empty tells if no error was recorded.
func (e *Errors) Empty() bool { return len(e.names) == 0 }

// Names returns the invalid element names. Like the PHP array upstream builds (name => message), names are unique
// and keep the position of their first occurrence; numeric names are integers.
func (e *Errors) Names() []any {
	out := make([]any, 0, len(e.names))
	seen := map[string]bool{}
	for _, n := range e.names {
		key := fmt.Sprint(n)
		if seen[key] {
			continue
		}
		seen[key] = true
		if i, err := strconv.Atoi(key); err == nil && strconv.Itoa(i) == key {
			out = append(out, i)
		} else {
			out = append(out, n)
		}
	}
	return out
}

// Messages returns the error messages, aligned with Names.
func (e *Errors) Messages() []string { return e.messages }

// Paths returns the node path of every recorded error, aligned with Messages.
func (e *Errors) Paths() []string {
	out := make([]string, len(e.names))
	for i, n := range e.names {
		out[i] = fmt.Sprint(n)
	}
	return out
}

// Has tells if the key was provided (even with a null value).
func (in Input) Has(key string) bool {
	_, ok := in[key]
	return ok
}

// IsNull tells if a value is JSON null.
func IsNull(v any) bool { return v == nil }

// ToInt applies Valinor's int rules: ints, integral floats and integer strings.
func ToInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		if t == math.Trunc(t) && !math.IsInf(t, 0) {
			return int(t), true
		}
	case json.Number:
		if i, err := strconv.ParseInt(string(t), 10, 64); err == nil {
			return int(i), true
		}
		if f, err := t.Float64(); err == nil && f == math.Trunc(f) && !strings.ContainsAny(string(t), "eE") {
			return int(f), true
		}
	case string:
		if intString.MatchString(t) {
			if i, err := strconv.ParseInt(t, 10, 64); err == nil {
				return int(i), true
			}
		}
	}
	return 0, false
}

var intString = regexp.MustCompile(`^[+-]?\d+$`)

// ToBool applies Valinor's bool rules: true/false, 1/0 and "1"/"0"/"true"/"false".
func ToBool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case int:
		return t == 1, t == 0 || t == 1
	case json.Number:
		switch string(t) {
		case "1":
			return true, true
		case "0":
			return false, true
		}
	case float64:
		return false, false
	case string:
		switch t {
		case "1", "true":
			return true, true
		case "0", "false":
			return false, true
		}
	}
	return false, false
}

// ToString applies Valinor's string rules: strings, and numbers cast to their PHP string representation.
func ToString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return phpNumberString(t), true
	case int:
		return strconv.Itoa(t), true
	case float64:
		return strconv.FormatFloat(t, 'G', 14, 64), true
	}
	return "", false
}

// phpNumberString renders a JSON number like PHP casts an int or float to string.
func phpNumberString(n json.Number) string {
	s := string(n)
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return strconv.FormatInt(i, 10)
	}
	f, err := n.Float64()
	if err != nil {
		return s
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strings.Replace(strconv.FormatFloat(f, 'G', 14, 64), "E+", "E+", 1)
}

// ToStringList applies Valinor's array<string> rules. Keys are preserved (objects keep their keys, lists their
// indexes). Invalid elements are reported by key.
func ToStringList(v any, errs *Errors) ([]string, bool) {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		ok := true
		for i, item := range t {
			s, valid := ToString(item)
			if !valid {
				errs.Add(i, describe(item)+" is not a valid string.")
				ok = false
				continue
			}
			out = append(out, s)
		}
		return out, ok
	case map[string]any:
		out := make([]string, 0, len(t))
		ok := true
		for _, k := range sortedKeys(t) {
			s, valid := ToString(t[k])
			if !valid {
				errs.Add(k, describe(t[k])+" is not a valid string.")
				ok = false
				continue
			}
			out = append(out, s)
		}
		return out, ok
	}
	return nil, false
}

// ToStringListKeyed is ToStringList that also returns the PHP array key of every value: list indexes, or object keys
// (sorted, as decoded JSON objects do not keep their order).
func ToStringListKeyed(v any, errs *Errors) ([]string, []string, bool) {
	values, ok := ToStringList(v, errs)
	if !ok {
		return values, nil, ok
	}
	var keys []string
	switch t := v.(type) {
	case []any:
		keys = make([]string, len(t))
		for i := range t {
			keys[i] = strconv.Itoa(i)
		}
	case map[string]any:
		keys = sortedKeys(t)
	}
	return values, keys, true
}

func sortedKeys(m map[string]any) []string {
	// JSON objects decoded into maps lose their order; keep a stable order (upstream keeps insertion order, which is
	// irrelevant for how tags are used).
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func describe(v any) string {
	switch t := v.(type) {
	case nil:
		return "Value null"
	case bool:
		if t {
			return "Value true"
		}
		return "Value false"
	case string:
		return "Value '" + t + "'"
	case json.Number:
		return "Value " + string(t)
	}
	return "Value array"
}

// StripTags approximates PHP's strip_tags(): it removes anything that looks like an HTML/PHP tag or comment.
func StripTags(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	var b strings.Builder
	depth := 0
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if depth == 0 {
			if c == '<' && i+1 < len(s) && !isSpace(s[i+1]) {
				depth = 1
				continue
			}
			b.WriteByte(c)
			continue
		}
		// inside a tag
		if inQuote != 0 {
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			inQuote = c
		case '<':
			depth++
		case '>':
			depth--
		}
	}
	return b.String()
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
}

// TrimStrip is the global string converter shlink-common registers: trim(strip_tags($value)).
func TrimStrip(s string) string {
	return PHPTrim(StripTags(s))
}

// PHPTrim ports PHP's trim() default character list (" \t\n\r\0\x0B").
func PHPTrim(s string) string {
	return strings.Trim(s, " \t\n\r\x00\x0B")
}
