package phpx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// UnsupportedPHPError tells that a PHP file uses something ParseReturnFile does not evaluate.
type UnsupportedPHPError struct {
	Line   int
	Reason string
}

func (e *UnsupportedPHPError) Error() string {
	return fmt.Sprintf("line %d: %s (only files returning literal arrays and scalars, like the ones shlink-installer "+
		"writes, are supported)", e.Line, e.Reason)
}

// ParseReturnFile evaluates the value a PHP file returns, for config files like the ones shlink-installer writes:
// `<?php`, optional comments and `declare(strict_types=1);`, then `return <value>;`. Values are literal arrays
// (`array (...)` or `[...]`), strings (single or double quoted, without interpolation, concatenated with `.`), ints,
// floats, true, false and null. Anything else is an *UnsupportedPHPError.
func ParseReturnFile(src []byte) (any, error) {
	p := &phpParser{src: string(src), line: 1}
	if !strings.HasPrefix(p.src, "<?php") {
		return nil, p.fail("the file does not start with <?php")
	}
	p.advance(len("<?php"))
	p.skip()
	if p.keyword("declare") {
		p.skip()
		for _, t := range []string{"(", "strict_types", "=", "1", ")", ";"} {
			if !p.consume(t) {
				return nil, p.fail("unsupported declare statement")
			}
		}
		p.skip()
	}
	if !p.keyword("return") {
		return nil, p.unexpected()
	}
	value, err := p.expr()
	if err != nil {
		return nil, err
	}
	if !p.consume(";") {
		return nil, p.unexpected()
	}
	p.consume("?>")
	p.skip()
	if p.pos < len(p.src) {
		return nil, p.unexpected()
	}
	return value, nil
}

type phpParser struct {
	src  string
	pos  int
	line int
}

func (p *phpParser) fail(reason string) error {
	return &UnsupportedPHPError{Line: p.line, Reason: reason}
}

func (p *phpParser) unexpected() error {
	if p.pos >= len(p.src) {
		return p.fail("unexpected end of file")
	}
	end := p.pos
	for end < len(p.src) && end-p.pos < 20 && p.src[end] != '\n' {
		end++
	}
	return p.fail(fmt.Sprintf("unexpected %q", p.src[p.pos:end]))
}

func (p *phpParser) advance(n int) {
	p.line += strings.Count(p.src[p.pos:p.pos+n], "\n")
	p.pos += n
}

// skip skips whitespace and comments.
func (p *phpParser) skip() {
	for p.pos < len(p.src) {
		rest := p.src[p.pos:]
		switch {
		case strings.ContainsRune(" \t\r\n\v\f", rune(rest[0])):
			p.advance(1)
		case strings.HasPrefix(rest, "/*"):
			end := strings.Index(rest[2:], "*/")
			if end < 0 {
				p.advance(len(rest))
			} else {
				p.advance(end + 4)
			}
		case strings.HasPrefix(rest, "//"), rest[0] == '#' && !strings.HasPrefix(rest, "#["):
			end := strings.IndexByte(rest, '\n')
			if end < 0 {
				end = len(rest)
			}
			p.advance(end)
		default:
			return
		}
	}
}

func (p *phpParser) consume(token string) bool {
	p.skip()
	if strings.HasPrefix(p.src[p.pos:], token) {
		p.advance(len(token))
		return true
	}
	return false
}

func isIdentChar(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

// identifier reads the identifier at the current position, without consuming it.
func (p *phpParser) identifier() string {
	p.skip()
	end := p.pos
	for end < len(p.src) && (isIdentChar(p.src[end]) || p.src[end] == '\\') {
		end++
	}
	return p.src[p.pos:end]
}

// keyword consumes a case-insensitive keyword.
func (p *phpParser) keyword(kw string) bool {
	id := p.identifier()
	if !strings.EqualFold(id, kw) {
		return false
	}
	p.advance(len(id))
	return true
}

// expr parses a value, with `.` concatenations.
func (p *phpParser) expr() (any, error) {
	value, err := p.term()
	if err != nil {
		return nil, err
	}
	for {
		p.skip()
		if p.pos >= len(p.src) || p.src[p.pos] != '.' || p.pos+1 < len(p.src) && p.src[p.pos+1] >= '0' && p.src[p.pos+1] <= '9' {
			return value, nil
		}
		p.advance(1)
		next, err := p.term()
		if err != nil {
			return nil, err
		}
		_, leftArray := value.(*Array)
		_, rightArray := next.(*Array)
		if leftArray || rightArray {
			return nil, p.fail("concatenation of arrays")
		}
		value = ToString(value) + ToString(next)
	}
}

func (p *phpParser) term() (any, error) {
	p.skip()
	if p.pos >= len(p.src) {
		return nil, p.unexpected()
	}
	switch c := p.src[p.pos]; {
	case c == '\'':
		return p.singleQuoted()
	case c == '"':
		return p.doubleQuoted()
	case c == '[':
		p.advance(1)
		return p.elements("]")
	case c == '-' || c == '+':
		p.advance(1)
		v, err := p.term()
		if err != nil {
			return nil, err
		}
		switch n := v.(type) {
		case int:
			if c == '-' {
				return -n, nil
			}
			return n, nil
		case float64:
			if c == '-' {
				return -n, nil
			}
			return n, nil
		}
		return nil, p.fail("unary operator on a non-number")
	case c >= '0' && c <= '9' || c == '.':
		return p.number()
	case c == '(':
		p.advance(1)
		v, err := p.expr()
		if err != nil {
			return nil, err
		}
		if !p.consume(")") {
			return nil, p.unexpected()
		}
		return v, nil
	}
	id := p.identifier()
	switch strings.ToLower(id) {
	case "true":
		p.advance(len(id))
		return true, nil
	case "false":
		p.advance(len(id))
		return false, nil
	case "null":
		p.advance(len(id))
		return nil, nil
	case "array":
		p.advance(len(id))
		if !p.consume("(") {
			return nil, p.unexpected()
		}
		return p.elements(")")
	}
	return nil, p.unexpected()
}

// elements parses the elements of an array literal up to its closing token.
func (p *phpParser) elements(closing string) (any, error) {
	a := NewArray()
	for {
		if p.consume(closing) {
			return a, nil
		}
		first, err := p.expr()
		if err != nil {
			return nil, err
		}
		if p.consume("=>") {
			if _, isArray := first.(*Array); isArray {
				return nil, p.fail("array used as an array key")
			}
			value, err := p.expr()
			if err != nil {
				return nil, err
			}
			a.Set(first, value)
		} else {
			a.Append(first)
		}
		if !p.consume(",") {
			if p.consume(closing) {
				return a, nil
			}
			return nil, p.unexpected()
		}
	}
}

func (p *phpParser) singleQuoted() (any, error) {
	var b strings.Builder
	i := p.pos + 1
	for i < len(p.src) {
		c := p.src[i]
		switch {
		case c == '\'':
			p.advance(i + 1 - p.pos)
			return b.String(), nil
		case c == '\\' && i+1 < len(p.src) && (p.src[i+1] == '\\' || p.src[i+1] == '\''):
			b.WriteByte(p.src[i+1])
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}
	return nil, p.fail("unterminated string")
}

func (p *phpParser) doubleQuoted() (any, error) {
	var b strings.Builder
	i := p.pos + 1
	for i < len(p.src) {
		c := p.src[i]
		switch {
		case c == '"':
			p.advance(i + 1 - p.pos)
			return b.String(), nil
		case c == '$' && i+1 < len(p.src) && (p.src[i+1] == '{' || p.src[i+1] == '_' || isIdentChar(p.src[i+1]) &&
			!(p.src[i+1] >= '0' && p.src[i+1] <= '9')):
			p.advance(i - p.pos)
			return nil, p.fail("string interpolation")
		case c == '{' && i+1 < len(p.src) && p.src[i+1] == '$':
			p.advance(i - p.pos)
			return nil, p.fail("string interpolation")
		case c == '\\' && i+1 < len(p.src):
			n, consumed := p.escape(i + 1)
			b.WriteString(n)
			i += 1 + consumed
		default:
			b.WriteByte(c)
			i++
		}
	}
	return nil, p.fail("unterminated string")
}

// escape decodes the escape sequence of a double-quoted string after its backslash, returning the decoded text and
// how many bytes it took.
func (p *phpParser) escape(i int) (string, int) {
	c := p.src[i]
	switch c {
	case 'n':
		return "\n", 1
	case 't':
		return "\t", 1
	case 'r':
		return "\r", 1
	case 'v':
		return "\v", 1
	case 'e':
		return "\x1b", 1
	case 'f':
		return "\f", 1
	case '\\', '$', '"':
		return string(c), 1
	case 'x':
		end := i + 1
		for end < len(p.src) && end < i+3 && isHex(p.src[end]) {
			end++
		}
		if end == i+1 {
			return `\x`, 1
		}
		n, _ := strconv.ParseUint(p.src[i+1:end], 16, 8)
		return string([]byte{byte(n)}), end - i
	case 'u':
		if i+1 < len(p.src) && p.src[i+1] == '{' {
			if end := strings.IndexByte(p.src[i:], '}'); end > 2 {
				if n, err := strconv.ParseUint(p.src[i+2:i+end], 16, 32); err == nil {
					return string(utf8.AppendRune(nil, rune(n))), end + 1
				}
			}
		}
		return `\u`, 1
	}
	if c >= '0' && c <= '7' {
		end := i
		for end < len(p.src) && end < i+3 && p.src[end] >= '0' && p.src[end] <= '7' {
			end++
		}
		n, _ := strconv.ParseUint(p.src[i:end], 8, 16)
		return string([]byte{byte(n)}), end - i
	}
	return `\` + string(c), 1
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func (p *phpParser) number() (any, error) {
	end := p.pos
	for end < len(p.src) && (isIdentChar(p.src[end]) || p.src[end] == '.' ||
		(p.src[end] == '+' || p.src[end] == '-') && end > p.pos && (p.src[end-1] == 'e' || p.src[end-1] == 'E')) {
		end++
	}
	raw := p.src[p.pos:end]
	literal := strings.ReplaceAll(raw, "_", "")
	lower := strings.ToLower(literal)
	var value any
	switch {
	case strings.HasPrefix(lower, "0x"), strings.HasPrefix(lower, "0b"), strings.HasPrefix(lower, "0o"):
		base := map[byte]int{'x': 16, 'b': 2, 'o': 8}[lower[1]]
		value = parseInteger(lower[2:], base)
	case strings.ContainsAny(lower, ".e"):
		f, err := strconv.ParseFloat(lower, 64)
		if err != nil && !math.IsInf(f, 0) {
			value = nil
		} else {
			value = f
		}
	case len(lower) > 1 && lower[0] == '0':
		value = parseInteger(lower[1:], 8)
	default:
		value = parseInteger(lower, 10)
	}
	if value == nil {
		return nil, p.fail(fmt.Sprintf("invalid number %q", raw))
	}
	p.advance(len(raw))
	return value, nil
}

// parseInteger parses an integer literal, which becomes a float when it overflows, like PHP does. It returns nil for
// an invalid literal.
func parseInteger(digits string, base int) any {
	if i, err := strconv.ParseInt(digits, base, 64); err == nil {
		return int(i)
	}
	if digits == "" {
		return nil
	}
	f := 0.0
	for _, c := range digits {
		d, err := strconv.ParseUint(string(c), base, 8)
		if err != nil {
			return nil
		}
		f = f*float64(base) + float64(d)
	}
	return f
}
