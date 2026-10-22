package harness

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Normalize converts a Go value into the shape encoding/json produces when decoding into `any` (map[string]any,
// []any, float64, string, bool, nil), so expected values written with ints and typed maps compare to decoded JSON.
func Normalize(t testing.TB, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("normalize %#v: %v", v, err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("normalize %#v: %v", v, err)
	}
	return out
}

// AssertEquals is the counterpart of PHPUnit's assertEquals on json_decode($body, true) payloads: maps compare by key
// regardless of order, lists by position, numbers as float64. It stops the test on mismatch, like PHPUnit does.
func AssertEquals(t testing.TB, expected, actual any, msg ...any) {
	t.Helper()
	exp, act := Normalize(t, expected), Normalize(t, actual)
	if reflect.DeepEqual(exp, act) {
		return
	}
	var diffs []string
	diff("$", exp, act, &diffs)
	prefix := ""
	if len(msg) > 0 {
		prefix = fmt.Sprintf(fmt.Sprint(msg[0]), msg[1:]...) + ": "
	}
	t.Fatalf("%svalues differ\n  %s\nexpected: %s\nactual:   %s", prefix, strings.Join(diffs, "\n  "), compact(exp), compact(act))
}

// AssertStatus checks the response status code and prints the body on mismatch.
func AssertStatus(t testing.TB, resp *Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		t.Fatalf("expected status %d, got %d\nbody: %s", expected, resp.StatusCode, resp.Body)
	}
}

// AssertHasKey mirrors assertArrayHasKey.
func AssertHasKey(t testing.TB, m map[string]any, key string) {
	t.Helper()
	if _, ok := m[key]; !ok {
		t.Fatalf("expected key %q in %s", key, compact(m))
	}
}

// AssertNotHasKey mirrors assertArrayNotHasKey.
func AssertNotHasKey(t testing.TB, m map[string]any, key string) {
	t.Helper()
	if _, ok := m[key]; ok {
		t.Fatalf("expected no key %q in %s", key, compact(m))
	}
}

// AssertCount mirrors assertCount on a decoded JSON list.
func AssertCount(t testing.TB, expected int, list any) {
	t.Helper()
	items, ok := list.([]any)
	if !ok {
		t.Fatalf("expected a list of %d items, got %s", expected, compact(list))
	}
	if len(items) != expected {
		t.Fatalf("expected %d items, got %d: %s", expected, len(items), compact(list))
	}
}

// Dig walks nested JSON objects by key, like PHP's $payload['a']['b'] ?? null: it returns nil when a key is missing
// or an intermediate value is not an object.
func Dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

// DigOr is Dig with a fallback for missing values, like PHP's `?? $fallback`.
func DigOr(v any, fallback any, keys ...string) any {
	if r := Dig(v, keys...); r != nil {
		return r
	}
	return fallback
}

func diff(path string, exp, act any, out *[]string) {
	switch e := exp.(type) {
	case map[string]any:
		a, ok := act.(map[string]any)
		if !ok {
			break
		}
		keys := map[string]bool{}
		for k := range e {
			keys[k] = true
		}
		for k := range a {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			ev, eok := e[k]
			av, aok := a[k]
			p := path + "." + k
			switch {
			case !aok:
				*out = append(*out, fmt.Sprintf("%s: missing, expected %s", p, compact(ev)))
			case !eok:
				*out = append(*out, fmt.Sprintf("%s: unexpected %s", p, compact(av)))
			default:
				diff(p, ev, av, out)
			}
		}
		return
	case []any:
		a, ok := act.([]any)
		if !ok {
			break
		}
		if len(e) != len(a) {
			*out = append(*out, fmt.Sprintf("%s: expected %d items, got %d", path, len(e), len(a)))
		}
		for i := 0; i < len(e) && i < len(a); i++ {
			diff(fmt.Sprintf("%s[%d]", path, i), e[i], a[i], out)
		}
		return
	}
	if !reflect.DeepEqual(exp, act) {
		*out = append(*out, fmt.Sprintf("%s: expected %s, got %s", path, compact(exp), compact(act)))
	}
}

func compact(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%#v", v)
	}
	if len(raw) > 2000 {
		return string(raw[:2000]) + "..."
	}
	return string(raw)
}
