package console

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
)

var (
	colors       = map[string]int{"black": 0, "red": 1, "green": 2, "yellow": 3, "blue": 4, "magenta": 5, "cyan": 6, "white": 7, "default": 9}
	brightColors = map[string]int{"gray": 0, "bright-red": 1, "bright-green": 2, "bright-yellow": 3, "bright-blue": 4, "bright-magenta": 5, "bright-cyan": 6, "bright-white": 7}
	colorOptions = map[string][2]int{"bold": {1, 22}, "underscore": {4, 24}, "blink": {5, 25}, "reverse": {7, 27}, "conceal": {8, 28}}
)

// Style ports OutputFormatterStyle (with Color).
type Style struct {
	fg, bg  string
	options []string
	href    string
}

// NewStyle creates a style; it panics with an InvalidArgumentError for unknown colors or options, like Color does.
func NewStyle(fg, bg string, options ...string) *Style {
	s := &Style{options: options}
	s.setFg(fg)
	s.setBg(bg)
	for _, o := range options {
		if _, ok := colorOptions[o]; !ok {
			panic(&InvalidArgumentError{Message: fmt.Sprintf(`Invalid option specified: "%s". Expected one of (bold, underscore, blink, reverse, conceal).`, o)})
		}
	}
	return s
}

func parseColor(color string, background bool) string {
	if color == "" {
		return ""
	}
	if color[0] == '#' {
		// Ansi4 color mode: hex colors map to the closest basic color
		return hexToAnsi4(color, background)
	}
	if c, ok := colors[color]; ok {
		if background {
			return fmt.Sprintf("4%d", c)
		}
		return fmt.Sprintf("3%d", c)
	}
	if c, ok := brightColors[color]; ok {
		if background {
			return fmt.Sprintf("10%d", c)
		}
		return fmt.Sprintf("9%d", c)
	}
	panic(&InvalidArgumentError{Message: fmt.Sprintf(`Invalid "%s" color; expected one of (black, red, green, yellow, blue, magenta, cyan, white, default, gray, bright-red, bright-green, bright-yellow, bright-blue, bright-magenta, bright-cyan, bright-white).`, color)})
}

// hexToAnsi4 ports AnsiColorMode::Ansi4's degradation of hex colors (Shlink itself never uses hex colors).
func hexToAnsi4(color string, background bool) string {
	hex := strings.TrimPrefix(color, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		panic(&InvalidArgumentError{Message: fmt.Sprintf(`Invalid "#%s" color.`, hex)})
	}
	var r, g, b int
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	round := func(v int) int { return int(math.Round(float64(v) / 255)) }
	code := round(b)<<2 | round(g)<<1 | round(r)
	if background {
		return fmt.Sprintf("4%d", code)
	}
	return fmt.Sprintf("3%d", code)
}

func (s *Style) setFg(c string) { s.fg = parseColor(c, false) }
func (s *Style) setBg(c string) { s.bg = parseColor(c, true) }

func (s *Style) setCodes() string {
	var codes []string
	if s.fg != "" {
		codes = append(codes, s.fg)
	}
	if s.bg != "" {
		codes = append(codes, s.bg)
	}
	for _, o := range s.options {
		codes = append(codes, fmt.Sprint(colorOptions[o][0]))
	}
	if len(codes) == 0 {
		return ""
	}
	return "\033[" + strings.Join(codes, ";") + "m"
}

func (s *Style) unsetCodes() string {
	var codes []string
	if s.fg != "" {
		codes = append(codes, "39")
	}
	if s.bg != "" {
		codes = append(codes, "49")
	}
	for _, o := range s.options {
		codes = append(codes, fmt.Sprint(colorOptions[o][1]))
	}
	if len(codes) == 0 {
		return ""
	}
	return "\033[" + strings.Join(codes, ";") + "m"
}

// Apply ports OutputFormatterStyle::apply().
func (s *Style) Apply(text string) string {
	if s.href != "" && os.Getenv("TERMINAL_EMULATOR") != "JetBrains-JediTerm" {
		text = "\033]8;;" + s.href + "\033\\" + text + "\033]8;;\033\\"
	}
	return s.setCodes() + text + s.unsetCodes()
}

// Formatter ports OutputFormatter.
type Formatter struct {
	decorated bool
	styles    map[string]*Style
	stack     []*Style
}

// NewFormatter creates a formatter with the default styles.
func NewFormatter(decorated bool) *Formatter {
	return &Formatter{decorated: decorated, styles: map[string]*Style{
		"error":    NewStyle("white", "red"),
		"info":     NewStyle("green", ""),
		"comment":  NewStyle("yellow", ""),
		"question": NewStyle("black", "cyan"),
	}}
}

// Clone copies the formatter, like PHP's clone.
func (f *Formatter) Clone() *Formatter {
	c := &Formatter{decorated: f.decorated, styles: map[string]*Style{}}
	for k, v := range f.styles {
		c.styles[k] = v
	}
	return c
}

// SetStyle registers a named style.
func (f *Formatter) SetStyle(name string, s *Style) { f.styles[strings.ToLower(name)] = s }

func (f *Formatter) SetDecorated(d bool) { f.decorated = d }
func (f *Formatter) IsDecorated() bool   { return f.decorated }

// Escape ports OutputFormatter::escape().
func Escape(text string) string {
	text = strings.NewReplacer("<", `\<`, ">", `\>`).Replace(text)
	return EscapeTrailingBackslash(text)
}

// EscapeTrailingBackslash ports OutputFormatter::escapeTrailingBackslash().
func EscapeTrailingBackslash(text string) string {
	if strings.HasSuffix(text, `\`) {
		n := len(text)
		text = strings.TrimRight(text, `\`)
		text = strings.ReplaceAll(text, "\x00", "")
		text += strings.Repeat("\x00", n-len(text))
	}
	return text
}

// Format ports OutputFormatter::format().
func (f *Formatter) Format(message string) string { return f.FormatAndWrap(message, 0) }

var tagRegex = regexp.MustCompile(`(?i)<(([a-z](?:[^\\<>]*|\\.)*)|/([a-z][^<>]*)?)>`)

func (f *Formatter) current() *Style {
	if len(f.stack) == 0 {
		return &Style{}
	}
	return f.stack[len(f.stack)-1]
}

func (f *Formatter) pop(style *Style) {
	if len(f.stack) == 0 {
		return
	}
	if style == nil {
		f.stack = f.stack[:len(f.stack)-1]
		return
	}
	key := style.Apply("")
	for i := len(f.stack) - 1; i >= 0; i-- {
		if f.stack[i].Apply("") == key {
			f.stack = f.stack[:i]
			return
		}
	}
	panic(&InvalidArgumentError{Message: "Incorrectly nested style tag found."})
}

var styleAttr = regexp.MustCompile(`([^=]+)=([^;]+)(;|$)`)
var optionsSplit = regexp.MustCompile(`[^,;]+`)
var escapedAngle = regexp.MustCompile(`\\([<>])`)

func (f *Formatter) createStyleFromString(str string) *Style {
	if s, ok := f.styles[str]; ok {
		return s
	}
	matches := styleAttr.FindAllStringSubmatch(str, -1)
	if matches == nil {
		return nil
	}
	style := &Style{}
	for _, m := range matches {
		key := strings.ToLower(m[1])
		switch key {
		case "fg":
			style.setFg(strings.ToLower(m[2]))
		case "bg":
			style.setBg(strings.ToLower(m[2]))
		case "href":
			style.href = escapedAngle.ReplaceAllString(m[2], "$1")
		case "options":
			for _, o := range optionsSplit.FindAllString(strings.ToLower(m[2]), -1) {
				if _, ok := colorOptions[o]; !ok {
					panic(&InvalidArgumentError{Message: fmt.Sprintf(`Invalid option specified: "%s". Expected one of (bold, underscore, blink, reverse, conceal).`, o)})
				}
				style.options = append(style.options, o)
			}
		default:
			return nil
		}
	}
	return style
}

// FormatAndWrap ports OutputFormatter::formatAndWrap().
func (f *Formatter) FormatAndWrap(message string, width int) string {
	offset := 0
	var out strings.Builder
	lineLength := 0
	for _, m := range tagRegex.FindAllStringSubmatchIndex(message, -1) {
		pos := m[0]
		text := message[m[0]:m[1]]
		if pos != 0 && message[pos-1] == '\\' {
			continue
		}
		out.WriteString(f.applyCurrentStyle(message[offset:pos], out.String(), width, &lineLength))
		offset = m[1]
		open := text[1] != '/'
		tag := ""
		if open {
			tag = message[m[4]:m[5]]
		} else if m[6] >= 0 {
			tag = message[m[6]:m[7]]
		}
		if !open && tag == "" {
			f.pop(nil)
			continue
		}
		style := f.createStyleFromString(tag)
		switch {
		case style == nil:
			out.WriteString(f.applyCurrentStyle(text, out.String(), width, &lineLength))
		case open:
			f.stack = append(f.stack, style)
		default:
			f.pop(style)
		}
	}
	out.WriteString(f.applyCurrentStyle(message[offset:], out.String(), width, &lineLength))
	return strings.NewReplacer("\x00", `\`, `\<`, "<", `\>`, ">").Replace(out.String())
}

var trailingNewline = regexp.MustCompile(`\n$`)

func (f *Formatter) applyCurrentStyle(text, current string, width int, lineLength *int) string {
	if text == "" {
		return ""
	}
	if width == 0 {
		if f.decorated {
			return f.current().Apply(text)
		}
		return text
	}
	if *lineLength == 0 && current != "" {
		text = strings.TrimLeft(text, " \t\n\r\x00\x0B")
	}
	prefix := ""
	if *lineLength != 0 {
		lines := strings.SplitN(text, "\n", 2)
		i := width - *lineLength
		prefix = Substr(lines[0], 0, i) + "\n"
		text = Substr(lines[0], i, -1)
		if len(lines) > 1 {
			if text != "" {
				text += "\n"
			}
			text += lines[1]
		}
	}
	endsWithNewline := trailingNewline.MatchString(text)
	text = prefix + wordwrap(text, width, "\n", true)
	text = strings.TrimRight(text, "\n")
	if endsWithNewline {
		text += "\n"
	}
	if *lineLength == 0 && current != "" && !strings.HasSuffix(current, "\n") {
		text = "\n" + text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i == 0 {
			*lineLength += Length(line)
		} else {
			*lineLength = Length(line)
		}
		if width <= *lineLength {
			*lineLength = 0
		}
	}
	if f.decorated {
		for i, line := range lines {
			lines[i] = f.current().Apply(line)
		}
	}
	return strings.Join(lines, "\n")
}

// phpWordwrapMask ports PHP's wordwrap() for a forced cut, as AbstractString::wordwrap() runs it over a mask.
func phpWordwrap(text string, width int, brk string, cut bool) string {
	if text == "" {
		return ""
	}
	var out strings.Builder
	lastStart, lastSpace := 0, 0
	current := 0
	for ; current < len(text); current++ {
		switch {
		case text[current] == brk[0] && current+len(brk) < len(text) && strings.HasPrefix(text[current:], brk):
			out.WriteString(text[lastStart : current+len(brk)])
			current += len(brk) - 1
			lastStart = current + 1
			lastSpace = current + 1
		case text[current] == ' ':
			if current-lastStart >= width {
				out.WriteString(text[lastStart:current])
				out.WriteString(brk)
				lastStart = current + 1
			}
			lastSpace = current
		case current-lastStart >= width && cut && lastStart >= lastSpace:
			out.WriteString(text[lastStart:current])
			out.WriteString(brk)
			lastStart = current
			lastSpace = current
		case current-lastStart >= width && lastStart < lastSpace:
			out.WriteString(text[lastStart:lastSpace])
			out.WriteString(brk)
			lastSpace++
			lastStart = lastSpace
		}
	}
	if lastStart != current {
		out.WriteString(text[lastStart:current])
	}
	return out.String()
}

// wordwrap ports AbstractString::wordwrap() over grapheme clusters.
func wordwrap(s string, width int, brk string, cut bool) string {
	lines := []string{s}
	if brk != "" {
		lines = strings.Split(s, brk)
	}
	if len(lines) == 1 && lines[0] == "" {
		return ""
	}
	var chars []string
	var mask strings.Builder
	for i, line := range lines {
		if i > 0 {
			chars = append(chars, brk)
			mask.WriteByte('#')
		}
		for _, c := range graphemes(line) {
			chars = append(chars, c)
			if c == " " {
				mask.WriteByte(' ')
			} else {
				mask.WriteByte('?')
			}
		}
	}
	m := mask.String()
	wrapped := strings.TrimRight(m, "#")
	m = phpWordwrap(wrapped, width, "#", cut) + m[len(wrapped):]
	var out strings.Builder
	j, i := 0, -1
	b := -1
	for {
		next := strings.IndexByte(m[b+1:], '#')
		if next < 0 {
			break
		}
		b = b + 1 + next
		for i++; i < b; i++ {
			out.WriteString(chars[j])
			j++
		}
		if j < len(chars) && (chars[j] == brk || chars[j] == " ") {
			j++
		}
		out.WriteString(brk)
	}
	for ; j < len(chars); j++ {
		out.WriteString(chars[j])
	}
	return out.String()
}

// RemoveDecoration ports Helper::removeDecoration().
func RemoveDecoration(f *Formatter, s string) string {
	if !strings.Contains(s, "<") && !strings.Contains(s, "\033") {
		return s
	}
	decorated := f.decorated
	f.decorated = false
	s = f.Format(s)
	s = ansiSGR.ReplaceAllString(s, "")
	s = ansiHyperlink.ReplaceAllString(s, "")
	f.decorated = decorated
	return s
}

var (
	ansiSGR       = regexp.MustCompile("\033\\[[^m]*m")
	ansiHyperlink = regexp.MustCompile("\033]8;[^;]*;[^\033]*\033\\\\")
)
