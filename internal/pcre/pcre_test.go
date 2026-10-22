package pcre

import (
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"
)

// Go's regexp is the oracle for everything both engines read alike. It decodes UTF-8 and folds
// Unicode, so its input gets every byte >= 0x80 mapped to its own private-use rune, and the inputs
// never end in "\n" (PCRE's "$" also matches before a final "\n", Go's does not).

func toByteRunes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x80 {
			b.WriteByte(c)
		} else {
			b.WriteRune(rune(0xE000 + int(c)))
		}
	}
	return b.String()
}

func fromByteRunes(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0xE080 && r <= 0xE0FF {
			b.WriteByte(byte(r - 0xE000))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func oracle(t *testing.T, pattern string, flags Flags) *regexp.Regexp {
	src, err := translate(pattern, flags&Extended != 0)
	if err != nil {
		t.Fatal(err)
	}
	prefix := ""
	if flags&Caseless != 0 {
		prefix += "(?i)"
	}
	if flags&Multiline != 0 {
		prefix += "(?m)"
	}
	return regexp.MustCompile(prefix + src)
}

var differentialPatterns = []struct {
	pattern string
	flags   Flags
}{
	// phpuseragentparser.
	{`\((.*?)\)`, Multiline},
	{"(?P<platform>BB\\d+;|Android|Adr|Symbian|Linux|(?:Open|Net|Free)BSD|Macintosh|\nWindows(?:\\ Phone)?|X11|(?:New\\ )?Nintendo\\ (?:WiiU?|3?DS|Switch)|Xbox(?:\\ One)?)\n(?:\\ [^;]*)?\n(?:;|$)", Caseless | Multiline | Extended},
	{"(?P<prev>.)?(?P<browser>Kindle(\\ Fire)?|Firefox|Safari|AppleWebKit|(?:Headless)?Chrome|Opera|OPR|(?-i:Edge)|EdgA?|XiaoMi/MiuiBrowser|Version|PLAYSTATION\\ (?:\\d|Vita)+)\n\\)?;?\n(?:[:/ ](?P<version>[0-9A-Z.]+)|/[A-Z]*)", Caseless | Extended},
	{`(?P<browser>[A-Z0-9\-]+)[/ ](?P<version>[0-9A-Z.]+)\s*\(\+(?:https?:|[a-z0-9._-]+@[a-z0-9._-]+)`, Caseless | Extended},
	{`^(?P<browser>[A-Z0-9\-]+)([/ :](?P<version>[0-9A-Z.]+))?`, Caseless | Extended},
	{"[(;]\\s*(?P<browser>[^(/;]+)\n(?:[:/ ]v?(?P<version>[0-9A-Z.]+)[^;)\\s]*)?\n;?(?:\\s*robot;)?\\s*\\+(?:https?:|[a-z0-9._-]+@[a-z0-9._-]+)", Caseless | Extended},
	// crawler-detect.
	{`Opera Mini\/\d{1,2}\.\d{1,2}\.[\d\.]*\/\d{1,2}\.`, Caseless},
	{`(like )?Gecko(\/[\d\.]*)?`, Caseless},
	{`CPU iPhone OS ([0-9_])* like Mac OS X`, Caseless},
	{`x86_..|Safari\/[\d\.]*| Chrome\/[\d\.]*|Mac OS X [\d_]*`, Caseless},
	{`[a-z0-9\-_]*(bot|crawl|headless)`, Caseless},
	{`Java.*outbrain|NewsBlur .*Finder|centuryb.o.t9[at]gmail.com|Y!J-[A-Z][A-Z][A-Z]|^b0t$`, Caseless},
	// Backtracking order.
	{`(a|ab)(c|bcd)(d*)`, 0},
	{`x(?:ab|a)*?(b*)y`, 0},
	{`(a+?)(a*)(b?)`, 0},
	{`(\d+)\.(\d+)|(\d+)`, 0},
	{`\bab\B`, Caseless},
}

const alphabet = "aAbBcCdDkKsSxX09._-/:;() +@\t\v\r\f\x00\xa0\xc3\xe2\x84\xaa\xc5\xbf"

// fragments recur in the patterns with escapes that the token split below loses.
var fragments = []string{"(+", "(+http:", "(+https:", "+a@b.c", "+http://", "/1.2", "/ ", "1.2.3", "12.34.5/67.",
	"Opera Mini/", " robot;", " v2", "; ", "(", ")", "Mozilla/5.0 ", "/", ".", ":"}

func randomInput(r *rand.Rand, pattern string) string {
	tokens := strings.FieldsFunc(pattern, func(c rune) bool { return strings.ContainsRune(`\()[]|?*+{}^$`, c) })
	tokens = append(tokens, fragments...)
	var b strings.Builder
	for n := r.IntN(8); n >= 0; n-- {
		if len(tokens) > 0 && r.IntN(2) == 0 {
			tok := tokens[r.IntN(len(tokens))]
			if r.IntN(3) == 0 {
				tok = strings.ToUpper(tok)
			}
			b.WriteString(tok)
		} else {
			for m := r.IntN(4); m >= 0; m-- {
				b.WriteByte(alphabet[r.IntN(len(alphabet))])
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func TestDifferential(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for _, p := range differentialPatterns {
		re := MustCompile(p.pattern, p.flags)
		want := oracle(t, p.pattern, p.flags)
		caps := make([]int, 2*(re.NumSubexp()+1))
		matched := 0
		for range 20000 {
			s := randomInput(r, p.pattern)
			mapped := toByteRunes(s)
			loc := want.FindStringSubmatchIndex(mapped)
			found := re.FindFrom(s, 0, caps)
			if found != (loc != nil) {
				t.Fatalf("%q on %q: found %v, oracle %v", p.pattern, s, found, loc != nil)
			}
			if !found {
				continue
			}
			matched++
			if start := len(fromByteRunes(mapped[:loc[0]])); start != caps[0] {
				t.Fatalf("%q on %q: match starts at %d, oracle %d", p.pattern, s, caps[0], start)
			}
			for g := 0; g < len(caps)/2; g++ {
				var got, exp string
				if caps[2*g] >= 0 {
					got = "[" + s[caps[2*g]:caps[2*g+1]] + "]"
				}
				if loc[2*g] >= 0 {
					exp = "[" + fromByteRunes(mapped[loc[2*g]:loc[2*g+1]]) + "]"
				}
				if got != exp {
					t.Fatalf("%q on %q: group %d = %q, oracle %q", p.pattern, s, g, got, exp)
				}
			}
		}
		t.Logf("%d/20000 inputs match %q", matched, p.pattern)
	}
}

func TestPCRESemantics(t *testing.T) {
	for _, c := range []struct {
		pattern string
		flags   Flags
		subject string
		want    bool
	}{
		{`Kelny`, Caseless, "Kelny", false}, // ASCII-only folding
		{`Slack`, Caseless, "ſlack", false},
		{`KELNY`, Caseless, "kelny", true},
		{`^x86_..$`, 0, "x86_é", true}, // "." is one byte: é is two
		{`^x86_.$`, 0, "x86_é", false},
		{`^a\sb$`, 0, "a\vb", true}, // PCRE's \s includes \v
		{`^a[\s]b$`, 0, "a\vb", true},
		{`^a\Sb$`, 0, "a\vb", false},
		{`abc$`, 0, "abc\n", true}, // "$" also matches before a final "\n"
		{`abc$`, 0, "abc\n\n", false},
		{`abc$`, Multiline, "abc\nx", true},
		{`^abc`, Multiline, "x\nabc", true},
		{`^abc`, 0, "x\nabc", false},
		{`a.c`, 0, "a\nc", false},
		{`a.c`, 0, "a\rc", true},
		{`a[^b]c`, 0, "a\xffc", true}, // negated classes match high bytes
		{"a b # comment\n c", Extended, "abc", true},
		{`a[ ]b`, Extended, "a b", true}, // /x keeps class whitespace
		{`a\ b`, Extended, "a b", true},
	} {
		re, err := Compile(c.pattern, c.flags)
		if err != nil {
			t.Fatal(err)
		}
		if got := re.MatchString(c.subject); got != c.want {
			t.Errorf("%q on %q = %v, want %v", c.pattern, c.subject, got, c.want)
		}
	}
}

func TestUnsupportedSyntax(t *testing.T) {
	for _, p := range []string{`a(?!b)`, `(?<=a)b`, `(a)\1`, `a\z`, `a\Z`, `\h`, `\v`, `\R`, `[\S]`, `\xff`, "é"} {
		if _, err := Compile(p, 0); err == nil {
			t.Errorf("%q compiled", p)
		}
	}
}

func TestPossessiveRuns(t *testing.T) {
	re := MustCompile(`[a-z]+[/ ]x`, Caseless)
	runs := 0
	var walk func(n *inst, seen map[*inst]bool)
	walk = func(n *inst, seen map[*inst]bool) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		if n.op == opRun {
			runs++
		}
		walk(n.next, seen)
		walk(n.x, seen)
		walk(n.y, seen)
		for _, a := range n.alts {
			walk(a, seen)
		}
	}
	walk(re.prog, map[*inst]bool{})
	if runs != 1 {
		t.Errorf("got %d possessive runs, want 1", runs)
	}
	// Not possessive when what follows overlaps the class: "a+ab" must give back an "a".
	if !MustCompile(`a+ab`, 0).MatchString("aaab") {
		t.Error("a+ab did not match aaab")
	}
}

func TestStepLimit(t *testing.T) {
	re := MustCompile(`(?:a|aa)*c`, 0)
	s := strings.Repeat("a", 60)
	if re.MatchString(s) {
		t.Error("matched")
	}
	if _, ok := MustCompile(`(?:a|aa)*c|b`, 0).RemoveAll(s + "b"); ok {
		t.Error("RemoveAll did not report the step limit")
	}
}

func TestRemoveAll(t *testing.T) {
	re := MustCompile(`Safari\/[\d\.]*|x86_..`, Caseless)
	for in, want := range map[string]string{
		"":                       "",
		"none":                   "none",
		"a SAFARI/1.2 b x86_64c": "a  b c",
		"safari/safari/1":        "",
	} {
		if got, ok := re.RemoveAll(in); !ok || got != want {
			t.Errorf("RemoveAll(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}
