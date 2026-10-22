package web

import (
	"strconv"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// phpArray is an ordered PHP array with string or integer keys.
type phpArray struct {
	keys    []string
	values  map[string]any
	nextIdx int
}

func newPHPArray() *phpArray { return &phpArray{values: map[string]any{}} }

// phpKey normalizes a key like PHP's symtable: canonical decimal integers become integer keys.
func phpKey(k string) (string, int, bool) {
	if n, err := strconv.Atoi(k); err == nil && strconv.Itoa(n) == k {
		return k, n, true
	}
	return k, 0, false
}

func (a *phpArray) set(key string, v any) {
	k, n, isInt := phpKey(key)
	if isInt && n >= a.nextIdx {
		a.nextIdx = n + 1
	}
	if _, ok := a.values[k]; !ok {
		a.keys = append(a.keys, k)
	}
	a.values[k] = v
}

func (a *phpArray) appendValue(v any) string {
	k := strconv.Itoa(a.nextIdx)
	a.set(k, v)
	return k
}

func (a *phpArray) child(index *string) *phpArray {
	if index == nil {
		c := newPHPArray()
		a.appendValue(c)
		return c
	}
	k, _, _ := phpKey(*index)
	if c, ok := a.values[k].(*phpArray); ok {
		return c
	}
	c := newPHPArray()
	a.set(k, c)
	return c
}

// toValue converts nested arrays into []any (sequential 0..n-1 keys) or map[string]any.
func (a *phpArray) toValue() any {
	isList := true
	for i, k := range a.keys {
		if k != strconv.Itoa(i) {
			isList = false
			break
		}
	}
	if isList {
		out := make([]any, len(a.keys))
		for i, k := range a.keys {
			out[i] = convertPHPValue(a.values[k])
		}
		return out
	}
	out := make(map[string]any, len(a.keys))
	for _, k := range a.keys {
		out[k] = convertPHPValue(a.values[k])
	}
	return out
}

func convertPHPValue(v any) any {
	if arr, ok := v.(*phpArray); ok {
		return arr.toValue()
	}
	return v
}

// registerVariable ports php_register_variable_ex() for a single name/value pair.
func registerVariable(track *phpArray, name, value string) {
	name = strings.TrimLeft(name, " ")
	if i := strings.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	base := []byte(name)
	bracket := -1
	for i, c := range base {
		if c == ' ' || c == '.' {
			base[i] = '_'
		} else if c == '[' {
			bracket = i
			break
		}
	}
	if bracket < 0 {
		if len(base) == 0 {
			return
		}
		track.set(string(base), value)
		return
	}
	varName := string(base[:bracket])
	if varName == "" {
		return
	}
	rest := name[bracket:]
	// Collect indexes; an unterminated bracket makes the rest part of the name.
	var indexes []*string
	for len(rest) > 0 && rest[0] == '[' {
		closing := strings.IndexByte(rest[1:], ']')
		if closing < 0 {
			if len(indexes) == 0 {
				varName = varName + "_" + rest[1:]
				track.set(varName, value)
				return
			}
			// PHP stops at the last well-formed index and assigns the value there.
			break
		}
		idx := rest[1 : 1+closing]
		if idx == "" {
			indexes = append(indexes, nil)
		} else {
			indexes = append(indexes, &idx)
		}
		rest = rest[closing+2:]
	}
	current := track.child(&varName)
	for _, idx := range indexes[:len(indexes)-1] {
		current = current.child(idx)
	}
	last := indexes[len(indexes)-1]
	if last == nil {
		current.appendValue(value)
	} else {
		current.set(*last, value)
	}
}

// ParseStr ports PHP's parse_str(), which RoadRunner uses to build the request's query params.
func ParseStr(query string) map[string]any {
	track := newPHPArray()
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		name, value, _ := strings.Cut(pair, "=")
		registerVariable(track, phpx.URLDecode(name), phpx.URLDecode(value))
	}
	out := make(map[string]any, len(track.keys))
	for _, k := range track.keys {
		out[k] = convertPHPValue(track.values[k])
	}
	return out
}
