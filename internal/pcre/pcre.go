// Package pcre runs PHP preg_* patterns the way PHP runs them without the /u modifier, for the
// pattern subset that jaybizzle/crawler-detect and donatj/phpuseragentparser use.
//
// Go's regexp differs from PCRE in ways that change results on real user agents: it decodes UTF-8
// (PCRE without /u matches bytes, so "." is one byte), its /i folds Unicode ("k" matches U+212A
// KELVIN SIGN, PCRE in the C locale folds ASCII only), its \s lacks \v, and its "$" is the end of
// text (PCRE's also matches before a final "\n"). This package is a small backtracking matcher with
// PCRE's semantics instead: bytes, ASCII folding, PCRE's \s and "$", and PCRE's order of trying
// alternatives (first first) and quantifiers (greedy first, lazy last). Patterns are parsed with
// regexp/syntax after a PCRE-to-Go translation of escapes and /x, so a construct RE2 lacks
// (lookarounds, backreferences) or one whose meaning differs and is not translated fails to compile
// instead of being silently misread.
package pcre

import (
	"fmt"
	"regexp/syntax"
	"strings"
)

// Flags are the PCRE modifiers.
type Flags uint8

const (
	Caseless  Flags = 1 << iota // /i
	Multiline                   // /m
	Extended                    // /x
)

// stepLimit bounds the backtracking work of one search, like PHP's pcre.backtrack_limit bounds
// PCRE's. PHP gives up after 1,000,000 of its own steps, which count differently, so the two engines
// stop on different pathological inputs (both treat giving up as "no match").
const stepLimit = 1_000_000

// Regexp is a compiled pattern. It is safe for concurrent use.
type Regexp struct {
	prog     *inst
	ncap     int // capture groups, without the whole match
	names    []string
	canStart [256]bool // bytes a match can start with
	nullable bool      // a match may start without consuming a byte, so every position is a candidate
	required string    // a literal every match contains (lowercased when requiredFold), "" if none
	reqFold  bool
}

// Compile compiles a PCRE pattern (without delimiters) with the given modifiers.
func Compile(pattern string, flags Flags) (*Regexp, error) {
	re, err := Parse(pattern, flags)
	if err != nil {
		return nil, err
	}
	return CompileSyntax(re)
}

// MustCompile is Compile that panics on error.
func MustCompile(pattern string, flags Flags) *Regexp {
	re, err := Compile(pattern, flags)
	if err != nil {
		panic("pcre: " + err.Error())
	}
	return re
}

// Parse translates a PCRE pattern to regexp/syntax and parses it.
func Parse(pattern string, flags Flags) (*syntax.Regexp, error) {
	src, err := translate(pattern, flags&Extended != 0)
	if err != nil {
		return nil, fmt.Errorf("pattern %q: %w", pattern, err)
	}
	f := syntax.Perl
	if flags&Caseless != 0 {
		f |= syntax.FoldCase
	}
	if flags&Multiline != 0 {
		f &^= syntax.OneLine
	}
	re, err := syntax.Parse(src, f)
	if err != nil {
		return nil, fmt.Errorf("pattern %q: %w", pattern, err)
	}
	return re, nil
}

// CompileSyntax compiles a tree returned by Parse, or an alternation or concatenation of such trees.
func CompileSyntax(re *syntax.Regexp) (*Regexp, error) {
	re = re.Simplify()
	c := compiler{}
	prog := c.compile(re, &inst{op: opMatch})
	if c.err != nil {
		return nil, c.err
	}
	for _, loop := range c.loops {
		loop.possessify()
	}
	for _, alt := range c.alts {
		alt.buildDispatch()
	}
	r := &Regexp{prog: prog, ncap: re.MaxCap(), names: re.CapNames()}
	r.nullable = firstBytes(prog, &r.canStart, map[*inst]bool{})
	r.required, r.reqFold = requiredLiteral(re)
	return r, nil
}

// translate rewrites PCRE syntax that regexp/syntax would read differently, and drops /x
// whitespace and comments.
func translate(p string, extended bool) (string, error) {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c >= 0x80 {
			return "", fmt.Errorf("non-ASCII pattern")
		}
		switch {
		case c == '\\':
			if i+1 == len(p) {
				return "", fmt.Errorf("trailing backslash")
			}
			i++
			e := p[i]
			switch {
			case e == 's' && inClass:
				b.WriteString(`\t\n\v\f\r `)
			case e == 's':
				b.WriteString(`[\t\n\v\f\r ]`)
			case e == 'S' && !inClass:
				b.WriteString(`[^\t\n\v\f\r ]`)
			case strings.IndexByte("dDwWtnrf", e) >= 0, strings.IndexByte("bBA", e) >= 0 && !inClass,
				strings.IndexByte(`\/.^$|?*+()[]{}-:;,<>=!'"#&~% @_`, e) >= 0:
				b.WriteByte('\\')
				b.WriteByte(e)
			default:
				// \S in a class, \z, \Z, \h, \v, \R, \x, back references, ... differ or are unused.
				return "", fmt.Errorf(`unsupported escape \%c`, e)
			}
			continue
		case inClass:
			if c == ']' {
				inClass = false
			}
		case c == '[':
			inClass = true
			b.WriteByte(c)
			// A ']' right after '[' or '[^' is a literal member.
			if i+1 < len(p) && p[i+1] == '^' {
				i++
				b.WriteByte('^')
			}
			if i+1 < len(p) && p[i+1] == ']' {
				i++
				b.WriteString(`\]`)
			}
			continue
		case extended && strings.IndexByte(" \t\n\r\f\v", c) >= 0:
			continue
		case extended && c == '#':
			for i < len(p) && p[i] != '\n' {
				i++
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String(), nil
}

type opcode uint8

const (
	opMatch opcode = iota
	opLiteral
	opClass
	opRun // possessive repeat of a byte class: no other split of the run can lead to a match
	opBeginText
	opEndText // PCRE "$" without /m: the end, or before a final "\n"
	opBeginLine
	opEndLine
	opWordBoundary
	opNoWordBoundary
	opSplit
	opAlt
	opSave
)

type inst struct {
	op   opcode
	lit  string     // opLiteral, lowercased when fold
	fold bool       // opLiteral
	set  *[256]bool // opClass, opRun, and the byte set of a single-byte star loop
	slot int        // opSave
	next *inst      // ops that continue
	x, y *inst      // opSplit: try x, then y

	// opAlt: alternatives in order, and per next byte the ones that can match there.
	alts     []*inst
	dispatch *[256][]uint16
	atEnd    []uint16
}

type compiler struct {
	err   error
	alts  []*inst
	loops []*inst // greedy single-byte star loops, candidates for opRun
}

func (c *compiler) fail(re *syntax.Regexp, why string) *inst {
	if c.err == nil {
		c.err = fmt.Errorf("%s: %s", re, why)
	}
	return &inst{op: opMatch}
}

// byteSet returns the bytes a single-byte expression matches.
func byteSet(re *syntax.Regexp) (*[256]bool, bool) {
	var set [256]bool
	switch re.Op {
	case syntax.OpLiteral:
		if len(re.Rune) != 1 || re.Rune[0] >= 0x80 {
			return nil, false
		}
		ch := byte(re.Rune[0])
		set[ch] = true
		if re.Flags&syntax.FoldCase != 0 {
			set[lowerASCII(ch)] = true
			set[upperASCII(ch)] = true
		}
	case syntax.OpCharClass:
		// Patterns are ASCII, so a member >= 0x80 comes from negation, which PCRE without /u also
		// applies to bytes 0x80-0xFF, or from folding (U+017F, U+212A), which no byte can be.
		for i := 0; i < len(re.Rune); i += 2 {
			for r := re.Rune[i]; r <= re.Rune[i+1] && r < 0x100; r++ {
				set[r] = true
			}
		}
	case syntax.OpAnyCharNotNL, syntax.OpAnyChar:
		for i := range set {
			set[i] = i != '\n' || re.Op == syntax.OpAnyChar
		}
	default:
		return nil, false
	}
	return &set, true
}

// compile returns the program for re followed by next.
func (c *compiler) compile(re *syntax.Regexp, next *inst) *inst {
	switch re.Op {
	case syntax.OpEmptyMatch:
		return next
	case syntax.OpLiteral:
		b := make([]byte, len(re.Rune))
		fold := re.Flags&syntax.FoldCase != 0
		for i, r := range re.Rune {
			if r >= 0x80 {
				return c.fail(re, "non-ASCII literal")
			}
			b[i] = byte(r)
			if fold {
				b[i] = lowerASCII(b[i])
			}
		}
		return &inst{op: opLiteral, lit: string(b), fold: fold, next: next}
	case syntax.OpCharClass, syntax.OpAnyCharNotNL, syntax.OpAnyChar:
		set, _ := byteSet(re)
		return &inst{op: opClass, set: set, next: next}
	case syntax.OpBeginText:
		return &inst{op: opBeginText, next: next}
	case syntax.OpEndText:
		return &inst{op: opEndText, next: next}
	case syntax.OpBeginLine:
		return &inst{op: opBeginLine, next: next}
	case syntax.OpEndLine:
		return &inst{op: opEndLine, next: next}
	case syntax.OpWordBoundary:
		return &inst{op: opWordBoundary, next: next}
	case syntax.OpNoWordBoundary:
		return &inst{op: opNoWordBoundary, next: next}
	case syntax.OpCapture:
		end := &inst{op: opSave, slot: 2*re.Cap + 1, next: next}
		return &inst{op: opSave, slot: 2 * re.Cap, next: c.compile(re.Sub[0], end)}
	case syntax.OpConcat:
		for i := len(re.Sub) - 1; i >= 0; i-- {
			next = c.compile(re.Sub[i], next)
		}
		return next
	case syntax.OpAlternate:
		alt := &inst{op: opAlt, alts: make([]*inst, len(re.Sub))}
		for i, sub := range re.Sub {
			alt.alts[i] = c.compile(sub, next)
		}
		c.alts = append(c.alts, alt)
		return alt
	case syntax.OpQuest:
		body := c.compile(re.Sub[0], next)
		if re.Flags&syntax.NonGreedy != 0 {
			return &inst{op: opSplit, x: next, y: body}
		}
		return &inst{op: opSplit, x: body, y: next}
	case syntax.OpStar, syntax.OpPlus:
		if minLen(re.Sub[0]) == 0 {
			return c.fail(re, "repeated sub-pattern can match empty")
		}
		loop := &inst{op: opSplit}
		body := c.compile(re.Sub[0], loop)
		if re.Flags&syntax.NonGreedy != 0 {
			loop.x, loop.y = next, body
		} else {
			loop.x, loop.y = body, next
			if set, ok := byteSet(re.Sub[0]); ok {
				loop.set = set
				c.loops = append(c.loops, loop)
			}
		}
		if re.Op == syntax.OpPlus {
			return body
		}
		return loop
	}
	return c.fail(re, "unsupported construct")
}

// possessify turns a greedy single-byte star loop into opRun when what follows can neither start
// with one of its bytes nor match without consuming one: then giving back part of the run can never
// lead to a match, which is how PCRE's auto-possessification reasons too.
func (loop *inst) possessify() {
	var follow [256]bool
	if firstBytes(loop.y, &follow, map[*inst]bool{}) {
		return
	}
	for i := range follow {
		if follow[i] && loop.set[i] {
			return
		}
	}
	loop.op, loop.next, loop.x, loop.y = opRun, loop.y, nil, nil
}

func (a *inst) buildDispatch() {
	a.dispatch = new([256][]uint16)
	for k, alt := range a.alts {
		var set [256]bool
		nullable := firstBytes(alt, &set, map[*inst]bool{})
		for ch := range set {
			if set[ch] || nullable {
				a.dispatch[ch] = append(a.dispatch[ch], uint16(k))
			}
		}
		if nullable {
			a.atEnd = append(a.atEnd, uint16(k))
		}
	}
}

// minLen is the length of the shortest string re matches.
func minLen(re *syntax.Regexp) int {
	switch re.Op {
	case syntax.OpLiteral:
		return len(re.Rune)
	case syntax.OpCharClass, syntax.OpAnyCharNotNL, syntax.OpAnyChar:
		return 1
	case syntax.OpCapture, syntax.OpPlus:
		return minLen(re.Sub[0])
	case syntax.OpRepeat:
		return re.Min * minLen(re.Sub[0])
	case syntax.OpConcat:
		n := 0
		for _, sub := range re.Sub {
			n += minLen(sub)
		}
		return n
	case syntax.OpAlternate:
		n := -1
		for _, sub := range re.Sub {
			if m := minLen(sub); n < 0 || m < n {
				n = m
			}
		}
		return max(n, 0)
	}
	return 0
}

// requiredLiteral returns the longest literal of re's top-level concatenation: every match
// contains it.
func requiredLiteral(re *syntax.Regexp) (string, bool) {
	parts := []*syntax.Regexp{re}
	if re.Op == syntax.OpConcat {
		parts = re.Sub
	}
	var best *syntax.Regexp
	for _, p := range parts {
		for p.Op == syntax.OpCapture {
			p = p.Sub[0]
		}
		if p.Op == syntax.OpLiteral && (best == nil || len(p.Rune) > len(best.Rune)) {
			best = p
		}
	}
	if best == nil {
		return "", false
	}
	fold := best.Flags&syntax.FoldCase != 0
	b := make([]byte, len(best.Rune))
	for i, r := range best.Rune {
		b[i] = byte(r)
		if fold {
			b[i] = lowerASCII(b[i])
		}
	}
	return string(b), fold
}

// firstBytes adds the bytes a match starting at n can begin with to set, and reports whether it
// can get past an assertion or to the end without consuming a byte (then every position counts).
func firstBytes(n *inst, set *[256]bool, seen map[*inst]bool) bool {
	if seen[n] {
		return false
	}
	seen[n] = true
	switch n.op {
	case opLiteral:
		ch := n.lit[0]
		set[ch] = true
		if n.fold {
			set[upperASCII(ch)] = true
		}
		return false
	case opClass, opRun:
		for i, ok := range n.set {
			if ok {
				set[i] = true
			}
		}
		if n.op == opRun {
			return firstBytes(n.next, set, seen)
		}
		return false
	case opBeginText, opSave:
		return firstBytes(n.next, set, seen)
	case opSplit:
		a := firstBytes(n.x, set, seen)
		b := firstBytes(n.y, set, seen)
		return a || b
	case opAlt:
		nullable := false
		for _, alt := range n.alts {
			nullable = firstBytes(alt, set, seen) || nullable
		}
		return nullable
	}
	return true // opMatch and the other assertions
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func upperASCII(c byte) byte {
	if 'a' <= c && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}

func isWordByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_'
}

// machine is the state of one search.
type machine struct {
	s     string
	caps  []int
	steps int
}

const aborted = -2

// run returns the end of the match PCRE prefers starting at i, -1 if there is none, or aborted
// once the step limit is spent. caps is restored on backtracking.
func (m *machine) run(n *inst, i int) int {
	s := m.s
	for {
		switch n.op {
		case opMatch:
			return i
		case opLiteral:
			if len(s)-i < len(n.lit) {
				return -1
			}
			if n.fold {
				for j := 0; j < len(n.lit); j++ {
					if lowerASCII(s[i+j]) != n.lit[j] {
						return -1
					}
				}
			} else if s[i:i+len(n.lit)] != n.lit {
				return -1
			}
			i += len(n.lit)
		case opClass:
			if i >= len(s) || !n.set[s[i]] {
				return -1
			}
			i++
		case opRun:
			for i < len(s) && n.set[s[i]] {
				i++
			}
		case opBeginText:
			if i != 0 {
				return -1
			}
		case opEndText:
			if i != len(s) && (i != len(s)-1 || s[i] != '\n') {
				return -1
			}
		case opBeginLine:
			if i != 0 && s[i-1] != '\n' {
				return -1
			}
		case opEndLine:
			if i != len(s) && s[i] != '\n' {
				return -1
			}
		case opWordBoundary, opNoWordBoundary:
			before := i > 0 && isWordByte(s[i-1])
			after := i < len(s) && isWordByte(s[i])
			if (before != after) != (n.op == opWordBoundary) {
				return -1
			}
		case opSave:
			if n.slot >= len(m.caps) {
				break
			}
			old := m.caps[n.slot]
			m.caps[n.slot] = i
			end := m.run(n.next, i)
			if end < 0 {
				m.caps[n.slot] = old
			}
			return end
		case opSplit:
			if m.steps++; m.steps > stepLimit {
				return aborted
			}
			if end := m.run(n.x, i); end != -1 {
				return end
			}
			n = n.y
			continue
		case opAlt:
			if m.steps++; m.steps > stepLimit {
				return aborted
			}
			list := n.atEnd
			if i < len(s) {
				list = n.dispatch[s[i]]
			}
			for _, k := range list {
				if end := m.run(n.alts[k], i); end != -1 {
					return end
				}
			}
			return -1
		}
		n = n.next
	}
}

// NumSubexp returns the number of capture groups.
func (re *Regexp) NumSubexp() int { return re.ncap }

// SubexpIndex returns the index of the named group, or -1.
func (re *Regexp) SubexpIndex(name string) int {
	for i, n := range re.names {
		if n == name && name != "" {
			return i
		}
	}
	return -1
}

// FindFrom finds the leftmost match starting at or after from, like preg_match with an offset, and
// reports whether there is one. caps, if not nil, receives the match and group positions: caps[2g]
// and caps[2g+1] for group g (0 is the whole match), -1 for a group that did not participate. It
// should have room for 2*(NumSubexp()+1) entries; groups past its end are not recorded. A search
// that hits the step limit finds nothing, as preg_match does when PCRE gives up.
func (re *Regexp) FindFrom(s string, from int, caps []int) bool {
	ok, _ := re.find(s, from, caps)
	return ok
}

func (re *Regexp) find(s string, from int, caps []int) (found, complete bool) {
	for i := range caps {
		caps[i] = -1
	}
	if re.required != "" && !containsLiteral(s[from:], re.required, re.reqFold) {
		return false, true
	}
	m := machine{s: s, caps: caps}
	for i := from; i <= len(s); i++ {
		if !re.nullable && (i == len(s) || !re.canStart[s[i]]) {
			continue
		}
		switch end := m.run(re.prog, i); end {
		case -1:
		case aborted:
			return false, false
		default:
			if len(caps) >= 2 {
				caps[0], caps[1] = i, end
			}
			return true, true
		}
	}
	return false, true
}

func containsLiteral(s, lit string, fold bool) bool {
	if !fold {
		return strings.Contains(s, lit)
	}
	first, firstUpper := lit[0], upperASCII(lit[0])
	for i := 0; i+len(lit) <= len(s); i++ {
		if c := s[i]; c != first && c != firstUpper {
			continue
		}
		j := 1
		for j < len(lit) && lowerASCII(s[i+j]) == lit[j] {
			j++
		}
		if j == len(lit) {
			return true
		}
	}
	return false
}

// MatchString reports whether s contains a match.
func (re *Regexp) MatchString(s string) bool {
	return re.FindFrom(s, 0, nil)
}

// RemoveAll is preg_replace($re, ”, $s). ok is false when a search hit the step limit, where
// preg_replace returns null. The pattern must not match the empty string.
func (re *Regexp) RemoveAll(s string) (result string, ok bool) {
	var buf [512]byte
	var caps [2]int
	out := buf[:0]
	removed := false
	last := 0
	for pos := 0; pos <= len(s); {
		found, complete := re.find(s, pos, caps[:])
		if !complete {
			return "", false
		}
		if !found {
			break
		}
		if caps[1] == caps[0] {
			panic("pcre: RemoveAll on an empty match")
		}
		removed = true
		out = append(out, s[last:caps[0]]...)
		last, pos = caps[1], caps[1]
	}
	if !removed {
		return s, true
	}
	return string(append(out, s[last:]...)), true
}
