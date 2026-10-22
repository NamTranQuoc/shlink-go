package console

//go:generate sh -c "docker run --rm -v $PWD/gen:/x --entrypoint php shlinkio/shlink:5.1.8 /x/gentables.php > width_tables.go"

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func inTable(t [][2]rune, r rune) bool {
	i := sort.Search(len(t), func(i int) bool { return t[i][1] >= r })
	return i < len(t) && t[i][0] <= r
}

func isEmojiChar(s string) bool {
	for _, r := range s {
		if inTable(emojiChars, r) {
			return true
		}
	}
	return false
}

// wcswidth ports AbstractUnicodeString::wcswidth(); it returns -1 for strings with control characters.
func wcswidth(s string) int {
	width := 0
	lastChar := ""
	lastWidth := 0
	joined := false
	for _, r := range s {
		c := string(r)
		if joined {
			joined = false
			continue
		}
		if r >= 0x20 && r < 0x7F {
			width++
			lastChar, lastWidth = c, 1
			continue
		}
		if r == 0 || r == 0x034F || r == 0x061C || (r >= 0x1160 && r <= 0x11FF) || r == 0x180E ||
			(r >= 0x200B && r <= 0x200F) || r == 0x2028 || r == 0x2029 || (r >= 0x202A && r <= 0x202E) ||
			(r >= 0x2060 && r <= 0x206F) || (r >= 0xD7B0 && r <= 0xD7FF) || r == 0xFEFF || (r >= 0xFFF0 && r <= 0xFFFB) {
			joined = r == 0x200D && lastChar != "" && len(lastChar) > 1 && isEmojiChar(lastChar)
			continue
		}
		if r < 0xA0 {
			return -1
		}
		if r == 0xFE0F {
			if lastChar != "" && lastWidth == 1 && isEmojiChar(lastChar) {
				width++
				lastWidth = 2
			}
			continue
		}
		if r > 0xFFFF {
			if (r >= 0x13430 && r <= 0x1343F) || (r >= 0x1BCA0 && r <= 0x1BCA3) || (r >= 0x1D173 && r <= 0x1D17A) ||
				(r >= 0xE0000 && r <= 0xE0FFF) {
				continue
			}
			if r >= 0x1F3FB && r <= 0x1F3FF && lastChar != "" && len(lastChar) > 1 && isEmojiChar(lastChar) {
				if lastWidth == 1 {
					width++
					lastWidth = 2
				}
				continue
			}
		}
		if inTable(wcwidthZero, r) {
			continue
		}
		if inTable(wcwidthWide, r) {
			width += 2
			lastChar, lastWidth = c, 2
			continue
		}
		width++
		lastChar, lastWidth = c, 1
	}
	return width
}

// isControl matches PCRE's [\p{Cc}\x7F].
func isControl(r rune) bool { return unicode.Is(unicode.Cc, r) || r == 0x7F }

// unicodeWidthNoAnsi ports UnicodeString::width(false).
func unicodeWidthNoAnsi(s string) int {
	s = strings.NewReplacer("\x00", "", "\x05", "", "\x07", "").Replace(s)
	if strings.Contains(s, "\r") {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	}
	s = strings.Map(func(r rune) rune {
		if isControl(r) {
			return -1
		}
		return r
	}, s)
	width := 0
	for _, line := range strings.Split(s, "\n") {
		if w := wcswidth(line); w > width {
			width = w
		}
	}
	return width
}

var nonPrintableASCII = regexp.MustCompile(`[^\x20-\x7E]`)

// Width ports Helper::width(): the display width of a string, counting control characters as one column.
func Width(s string) int {
	if s == "" {
		return 0
	}
	if !nonPrintableASCII.MatchString(s) {
		return len(s)
	}
	if !utf8.ValidString(s) {
		return len(s)
	}
	clean := strings.Map(func(r rune) rune {
		if isControl(r) {
			return -1
		}
		return r
	}, s)
	// preg_replace() counts each run of control characters as one replacement
	count := 0
	inRun := false
	for _, r := range s {
		if isControl(r) {
			if !inRun {
				count++
			}
			inRun = true
		} else {
			inRun = false
		}
	}
	return unicodeWidthNoAnsi(clean) + count
}

// graphemes splits a string into grapheme clusters: a base character followed by combining marks, variation
// selectors, emoji modifiers and zero-width-joiner sequences, plus regional indicator pairs.
func graphemes(s string) []string {
	var out []string
	runes := []rune(s)
	for i := 0; i < len(runes); {
		j := i + 1
		if runes[i] == '\r' && j < len(runes) && runes[j] == '\n' {
			out = append(out, "\r\n")
			i = j + 1
			continue
		}
		if runes[i] >= 0x1F1E6 && runes[i] <= 0x1F1FF && j < len(runes) && runes[j] >= 0x1F1E6 && runes[j] <= 0x1F1FF {
			j++
		}
		for j < len(runes) {
			r := runes[j]
			if unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc) || r == 0xFE0F || r == 0xFE0E || (r >= 0x1F3FB && r <= 0x1F3FF) ||
				(r >= 0xE0020 && r <= 0xE007F) || r == 0x200C {
				j++
				continue
			}
			if r == 0x200D && j+1 < len(runes) {
				j += 2
				continue
			}
			break
		}
		out = append(out, string(runes[i:j]))
		i = j
	}
	return out
}

// Length ports Helper::length(): the number of grapheme clusters (bytes for invalid UTF-8).
func Length(s string) int {
	if !utf8.ValidString(s) {
		return len(s)
	}
	return len(graphemes(s))
}

// Substr ports Helper::substr() with a non-negative start; length < 0 means "to the end".
func Substr(s string, from, length int) string {
	if !utf8.ValidString(s) {
		if from > len(s) {
			return ""
		}
		end := len(s)
		if length >= 0 && from+length < end {
			end = from + length
		}
		return s[from:end]
	}
	g := graphemes(s)
	if from < 0 {
		from += len(g)
		if from < 0 {
			from = 0
		}
	}
	if from > len(g) {
		return ""
	}
	end := len(g)
	if length >= 0 && from+length < end {
		end = from + length
	}
	if end < from {
		return ""
	}
	return strings.Join(g[from:end], "")
}

// levenshtein ports PHP's levenshtein() (byte based, unit costs).
func levenshtein(a, b string) int {
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 0; i < len(a); i++ {
		cur[0] = i + 1
		for j := 0; j < len(b); j++ {
			cost := 1
			if a[i] == b[j] {
				cost = 0
			}
			cur[j+1] = min(prev[j]+cost, prev[j+1]+1, cur[j]+1)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// strPad ports str_pad() for the right/left/both pad types used by tables.
func strPad(s string, length int, pad string, padType int) string {
	n := length - len(s)
	if n <= 0 || pad == "" {
		return s
	}
	repeat := func(k int) string {
		return strings.Repeat(pad, k/len(pad)+1)[:k]
	}
	switch padType {
	case PadLeft:
		return repeat(n) + s
	case PadBoth:
		left := n / 2
		return repeat(left) + s + repeat(n-left)
	}
	return s + repeat(n)
}

// Pad types, like PHP's STR_PAD_* constants.
const (
	PadLeft  = 0
	PadRight = 1
	PadBoth  = 2
)
