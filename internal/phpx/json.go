package phpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// JSON encoding flags, with PHP's values.
const (
	JSONHexTag           = 1
	JSONHexAmp           = 2
	JSONHexApos          = 4
	JSONHexQuot          = 8
	JSONUnescapedSlashes = 64
	JSONPrettyPrint      = 128
	JSONUnescapedUnicode = 256
	JSONPreserveZeroFrac = 1024
	// DiactorosJSONFlags are Laminas\Diactoros\Response\JsonResponse::DEFAULT_JSON_FLAGS.
	DiactorosJSONFlags = JSONHexTag | JSONHexApos | JSONHexAmp | JSONHexQuot | JSONUnescapedSlashes
	// ProblemDetailsJSONFlags are Mezzio's problem details defaults outside debug mode.
	ProblemDetailsJSONFlags = JSONUnescapedSlashes | JSONUnescapedUnicode | JSONPreserveZeroFrac
)

// Map is an ordered JSON object.
type Map []MapItem

// MapItem is one member of an ordered JSON object.
type MapItem struct {
	Key   string
	Value any
}

// Set appends or replaces a key.
func (m Map) Set(key string, value any) Map {
	for i, it := range m {
		if it.Key == key {
			m[i].Value = value
			return m
		}
	}
	return append(m, MapItem{key, value})
}

// Get returns a key's value.
func (m Map) Get(key string) (any, bool) {
	for _, it := range m {
		if it.Key == key {
			return it.Value, true
		}
	}
	return nil, false
}

// Raw is pre-encoded JSON inserted as-is.
type Raw []byte

// Encoder is implemented by types that know their PHP json_encode output.
type Encoder interface {
	EncodePHP(flags int) any
}

// Encode ports PHP's json_encode() for the values Shlink serializes: null, bools, ints, floats, strings, lists
// ([]T), ordered objects (Map) and plain Go maps (sorted keys) and structs (via encoding/json as a fallback).
func Encode(v any, flags int) []byte {
	var b bytes.Buffer
	encodeValue(&b, v, flags, 0)
	return b.Bytes()
}

func encodeValue(b *bytes.Buffer, v any, flags, depth int) {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case Encoder:
		encodeValue(b, t.EncodePHP(flags), flags, depth)
	case Raw:
		b.Write(t)
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int:
		b.WriteString(strconv.Itoa(t))
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case float64:
		b.WriteString(formatFloat(t, flags))
	case string:
		encodeString(b, t, flags)
	case *string:
		if t == nil {
			b.WriteString("null")
		} else {
			encodeString(b, *t, flags)
		}
	case *int:
		if t == nil {
			b.WriteString("null")
		} else {
			b.WriteString(strconv.Itoa(*t))
		}
	case Map:
		encodeObject(b, t, flags, depth)
	case []any:
		encodeList(b, len(t), func(i int) any { return t[i] }, flags, depth)
	case []string:
		encodeList(b, len(t), func(i int) any { return t[i] }, flags, depth)
	case []Map:
		encodeList(b, len(t), func(i int) any { return t[i] }, flags, depth)
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		m := make(Map, 0, len(keys))
		for _, k := range keys {
			m = append(m, MapItem{k, t[k]})
		}
		encodeObject(b, m, flags, depth)
	case json.Marshaler:
		raw, err := t.MarshalJSON()
		if err != nil {
			b.WriteString("null")
			return
		}
		var decoded any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&decoded); err != nil {
			b.Write(raw)
			return
		}
		encodeValue(b, fromDecoded(decoded), flags, depth)
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Slice {
			encodeList(b, rv.Len(), func(i int) any { return rv.Index(i).Interface() }, flags, depth)
			return
		}
		if rv.Kind() == reflect.Pointer && rv.IsNil() {
			b.WriteString("null")
			return
		}
		raw, err := json.Marshal(v)
		if err != nil {
			b.WriteString("null")
			return
		}
		b.Write(raw)
	}
}

// fromDecoded converts values decoded with UseNumber back into encodable values; object key order is lost, so
// MarshalJSON implementations should prefer returning Map via Encoder.
func fromDecoded(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		f, _ := t.Float64()
		return f
	case []any:
		for i := range t {
			t[i] = fromDecoded(t[i])
		}
		return t
	case map[string]any:
		for k := range t {
			t[k] = fromDecoded(t[k])
		}
		return t
	}
	return v
}

func newline(b *bytes.Buffer, flags, depth int) {
	if flags&JSONPrettyPrint == 0 {
		return
	}
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("    ", depth))
}

func encodeList(b *bytes.Buffer, n int, at func(int) any, flags, depth int) {
	if n == 0 {
		b.WriteString("[]")
		return
	}
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		newline(b, flags, depth+1)
		encodeValue(b, at(i), flags, depth+1)
	}
	newline(b, flags, depth)
	b.WriteByte(']')
}

func encodeObject(b *bytes.Buffer, m Map, flags, depth int) {
	if len(m) == 0 {
		b.WriteString("{}")
		return
	}
	b.WriteByte('{')
	for i, it := range m {
		if i > 0 {
			b.WriteByte(',')
		}
		newline(b, flags, depth+1)
		encodeString(b, it.Key, flags)
		b.WriteByte(':')
		if flags&JSONPrettyPrint != 0 {
			b.WriteByte(' ')
		}
		encodeValue(b, it.Value, flags, depth+1)
	}
	newline(b, flags, depth)
	b.WriteByte('}')
}

func encodeString(b *bytes.Buffer, s string, flags int) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '"':
				if flags&JSONHexQuot != 0 {
					b.WriteString(`\u0022`)
				} else {
					b.WriteString(`\"`)
				}
			case c == '\\':
				b.WriteString(`\\`)
			case c == '/':
				if flags&JSONUnescapedSlashes != 0 {
					b.WriteByte('/')
				} else {
					b.WriteString(`\/`)
				}
			case c == '\b':
				b.WriteString(`\b`)
			case c == '\f':
				b.WriteString(`\f`)
			case c == '\n':
				b.WriteString(`\n`)
			case c == '\r':
				b.WriteString(`\r`)
			case c == '\t':
				b.WriteString(`\t`)
			case c == '<' && flags&JSONHexTag != 0:
				b.WriteString(`\u003C`)
			case c == '>' && flags&JSONHexTag != 0:
				b.WriteString(`\u003E`)
			case c == '&' && flags&JSONHexAmp != 0:
				b.WriteString(`\u0026`)
			case c == '\'' && flags&JSONHexApos != 0:
				b.WriteString(`\u0027`)
			case c < 0x20:
				fmt.Fprintf(b, `\u%04x`, c)
			default:
				b.WriteByte(c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8: PHP fails the whole encoding; substitute to keep output valid.
			b.WriteString(`\ufffd`)
			i++
			continue
		}
		if flags&JSONUnescapedUnicode != 0 && r != '\u2028' && r != '\u2029' {
			b.WriteString(s[i : i+size])
		} else if r >= 0x10000 {
			r -= 0x10000
			fmt.Fprintf(b, `\u%04x\u%04x`, 0xD800+(r>>10), 0xDC00+(r&0x3FF))
		} else {
			fmt.Fprintf(b, `\u%04x`, r)
		}
		i += size
	}
	b.WriteByte('"')
}

// formatFloat ports PHP's float serialization with serialize_precision = -1.
func formatFloat(f float64, flags int) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return "0"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		if flags&JSONPreserveZeroFrac != 0 {
			s += ".0"
		}
		return s
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if strings.Contains(s, "e") {
		mant, exp, _ := strings.Cut(s, "e")
		if !strings.Contains(mant, ".") {
			mant += ".0"
		}
		if exp[0] == '+' {
			exp = "+" + strings.TrimLeft(exp[1:], "0")
		} else {
			exp = "-" + strings.TrimLeft(exp[1:], "0")
		}
		s = mant + "e" + exp
	}
	return s
}
