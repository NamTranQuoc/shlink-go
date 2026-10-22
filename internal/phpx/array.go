package phpx

import (
	"math"
	"strconv"
	"strings"
)

// Array is an ordered PHP array. Keys are strings or ints; values are nil, bool, int, float64, string or *Array.
type Array struct {
	items     []ArrayItem
	nextIndex int
}

// ArrayItem is one element of an Array.
type ArrayItem struct {
	Key   any
	Value any
}

// NewArray creates an empty array.
func NewArray() *Array { return &Array{} }

// NewList creates a list with the given values.
func NewList(values ...any) *Array {
	a := NewArray()
	for _, v := range values {
		a.Append(v)
	}
	return a
}

// ArrayKey ports PHP's array key casting: strings holding a canonical decimal integer become ints, bools become 0 or
// 1, null becomes "" and floats are truncated.
func ArrayKey(key any) any {
	switch k := key.(type) {
	case nil:
		return ""
	case bool:
		if k {
			return 1
		}
		return 0
	case float64:
		return int(k)
	case string:
		if i, ok := canonicalInt(k); ok {
			return i
		}
	}
	return key
}

func canonicalInt(s string) (int, bool) {
	if s == "" || len(s) > 20 {
		return 0, false
	}
	digits := strings.TrimPrefix(s, "-")
	if digits == "" || (digits[0] == '0' && (len(digits) > 1 || s[0] == '-')) {
		return 0, false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	i, err := strconv.Atoi(s)
	return i, err == nil
}

// Len returns the number of elements.
func (a *Array) Len() int { return len(a.items) }

// Items returns the elements in order.
func (a *Array) Items() []ArrayItem { return a.items }

func (a *Array) index(key any) int {
	key = ArrayKey(key)
	for i, it := range a.items {
		if it.Key == key {
			return i
		}
	}
	return -1
}

// Has ports array_key_exists().
func (a *Array) Has(key any) bool { return a.index(key) >= 0 }

// Get returns a key's value, or nil when it does not exist.
func (a *Array) Get(key any) any {
	if i := a.index(key); i >= 0 {
		return a.items[i].Value
	}
	return nil
}

// Set ports $array[$key] = $value.
func (a *Array) Set(key, value any) {
	key = ArrayKey(key)
	if i := a.index(key); i >= 0 {
		a.items[i].Value = value
		return
	}
	if i, ok := key.(int); ok && i >= a.nextIndex {
		a.nextIndex = i + 1
	}
	a.items = append(a.items, ArrayItem{key, value})
}

// Append ports $array[] = $value.
func (a *Array) Append(value any) { a.Set(a.nextIndex, value) }

// Filter returns a new array with the elements whose key passes keep, like array_filter() with ARRAY_FILTER_USE_KEY.
func (a *Array) Filter(keep func(key any) bool) *Array {
	out := NewArray()
	for _, it := range a.items {
		if keep(it.Key) {
			out.items = append(out.items, it)
		}
	}
	out.nextIndex = a.nextIndex
	return out
}

// Merge ports Laminas\Stdlib\ArrayUtils::merge() without preserving numeric keys: integer keys present in both are
// appended, nested arrays are merged recursively and any other value is replaced.
func Merge(a, b *Array) *Array {
	out := a.clone()
	for _, it := range b.items {
		if !out.Has(it.Key) {
			out.Set(it.Key, it.Value)
			continue
		}
		if _, isInt := it.Key.(int); isInt {
			out.Append(it.Value)
			continue
		}
		existing, ok1 := out.Get(it.Key).(*Array)
		value, ok2 := it.Value.(*Array)
		if ok1 && ok2 {
			out.Set(it.Key, Merge(existing, value))
			continue
		}
		out.Set(it.Key, it.Value)
	}
	return out
}

func (a *Array) clone() *Array {
	return &Array{items: append([]ArrayItem(nil), a.items...), nextIndex: a.nextIndex}
}

// Truthy ports PHP's (bool) cast.
func Truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case int:
		return t != 0
	case float64:
		return t != 0
	case string:
		return t != "" && t != "0"
	case *Array:
		return t.Len() > 0
	}
	return true
}

// ToString ports PHP's (string) cast. Arrays become "Array".
func ToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		if t {
			return "1"
		}
		return ""
	case int:
		return strconv.Itoa(t)
	case float64:
		return FormatFloat(t, 14)
	case string:
		return t
	case *Array:
		return "Array"
	}
	return ""
}

// Implode ports implode().
func Implode(sep string, a *Array) string {
	parts := make([]string, 0, a.Len())
	for _, it := range a.items {
		parts = append(parts, ToString(it.Value))
	}
	return strings.Join(parts, sep)
}

// FormatFloat ports PHP's float to string conversion with the given precision (the `precision` ini, 14, for string
// casts), or the shortest representation that round-trips when precision is 0 (serialize_precision -1, used by
// var_export()).
func FormatFloat(f float64, precision int) string {
	switch {
	case math.IsNaN(f):
		return "NAN"
	case math.IsInf(f, 1):
		return "INF"
	case math.IsInf(f, -1):
		return "-INF"
	}
	ndigit := precision
	var e string
	if precision == 0 {
		ndigit = 17
		e = strconv.FormatFloat(math.Abs(f), 'e', -1, 64)
	} else {
		e = strconv.FormatFloat(math.Abs(f), 'e', precision-1, 64)
	}
	mantissa, exp, _ := strings.Cut(e, "e")
	digits := strings.TrimRight(strings.Replace(mantissa, ".", "", 1), "0")
	if digits == "" {
		digits = "0"
	}
	decpt, _ := strconv.Atoi(exp)
	decpt++
	if f == 0 {
		decpt = 1
	}
	var b strings.Builder
	if math.Signbit(f) {
		b.WriteByte('-')
	}
	if (decpt < 0 && decpt < -3) || (decpt >= 0 && decpt > ndigit) {
		b.WriteByte(digits[0])
		b.WriteByte('.')
		if len(digits) > 1 {
			b.WriteString(digits[1:])
		} else {
			b.WriteByte('0')
		}
		b.WriteByte('E')
		if decpt-1 >= 0 {
			b.WriteByte('+')
		}
		b.WriteString(strconv.Itoa(decpt - 1))
		return b.String()
	}
	switch {
	case decpt <= 0:
		b.WriteString("0.")
		b.WriteString(strings.Repeat("0", -decpt))
		b.WriteString(digits)
	case decpt >= len(digits):
		b.WriteString(digits)
		b.WriteString(strings.Repeat("0", decpt-len(digits)))
	default:
		b.WriteString(digits[:decpt])
		b.WriteByte('.')
		b.WriteString(digits[decpt:])
	}
	return b.String()
}

// VarExport ports var_export($value, true).
func VarExport(v any) string {
	var b strings.Builder
	varExport(&b, v, 1)
	return b.String()
}

func varExport(b *strings.Builder, v any, level int) {
	switch t := v.(type) {
	case nil:
		b.WriteString("NULL")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int:
		if t == math.MinInt64 {
			b.WriteString("-9223372036854775807-1")
			return
		}
		b.WriteString(strconv.Itoa(t))
	case float64:
		s := FormatFloat(t, 0)
		b.WriteString(s)
		if strings.Trim(s, "-0123456789") == "" {
			b.WriteString(".0")
		}
	case string:
		b.WriteByte('\'')
		escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\x00", `' . "\0" . '`).Replace(t)
		b.WriteString(escaped)
		b.WriteByte('\'')
	case *Array:
		if level > 1 {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(" ", level-1))
		}
		b.WriteString("array (\n")
		for _, it := range t.items {
			b.WriteString(strings.Repeat(" ", level+1))
			if s, ok := it.Key.(string); ok {
				varExport(b, s, level+2)
			} else {
				b.WriteString(strconv.Itoa(it.Key.(int)))
			}
			b.WriteString(" => ")
			varExport(b, it.Value, level+2)
			b.WriteString(",\n")
		}
		if level > 1 {
			b.WriteString(strings.Repeat(" ", level-1))
		}
		b.WriteByte(')')
	}
}
