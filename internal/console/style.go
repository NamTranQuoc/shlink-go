package console

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxLineLength = 120

// TerminalWidth ports Terminal::getWidth(): the COLUMNS env var, else what `stty -a` reports for the inherited
// stdin, else 80.
func TerminalWidth() int {
	if v, ok := os.LookupEnv("COLUMNS"); ok {
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	}
	w, _ := sttyDimensions()
	if w == 0 {
		return 80
	}
	return w
}

// TerminalHeight ports Terminal::getHeight().
func TerminalHeight() int {
	if v, ok := os.LookupEnv("LINES"); ok {
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	}
	_, h := sttyDimensions()
	if h == 0 {
		return 50
	}
	return h
}

var sttyRowsCols = regexp.MustCompile(`(?is)rows.(\d+);.columns.(\d+);`)
var sttyColsRows = regexp.MustCompile(`(?is);.(\d+).rows;.(\d+).columns`)

func sttyDimensions() (int, int) {
	cmd := exec.Command("sh", "-c", "stty -a | grep columns")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return 0, 0
	}
	if m := sttyRowsCols.FindStringSubmatch(string(out)); m != nil {
		w, _ := strconv.Atoi(m[2])
		h, _ := strconv.Atoi(m[1])
		return w, h
	}
	if m := sttyColsRows.FindStringSubmatch(string(out)); m != nil {
		w, _ := strconv.Atoi(m[2])
		h, _ := strconv.Atoi(m[1])
		return w, h
	}
	return 0, 0
}

// Style ports SymfonyStyle. It also implements Output, so tables can render through it.
type SymfonyStyle struct {
	input      *Input
	output     Output
	buffered   *StreamOutput
	lineLength int
	stdin      *bufio.Reader
}

// NewSymfonyStyle creates a style over an input and output.
func NewSymfonyStyle(in *Input, out Output, stdin *bufio.Reader) *SymfonyStyle {
	width := TerminalWidth()
	if width == 0 {
		width = maxLineLength
	}
	buffered := &StreamOutput{w: io.Discard, verbosity: out.Verbosity(), formatter: out.Formatter().Clone(), trim: 2}
	buffered.formatter.decorated = false
	return &SymfonyStyle{input: in, output: out, buffered: buffered, lineLength: min(width, maxLineLength), stdin: stdin}
}

func (s *SymfonyStyle) Verbosity() int        { return s.output.Verbosity() }
func (s *SymfonyStyle) SetVerbosity(v int)    { s.output.SetVerbosity(v) }
func (s *SymfonyStyle) Formatter() *Formatter { return s.output.Formatter() }
func (s *SymfonyStyle) IsDecorated() bool     { return s.output.IsDecorated() }
func (s *SymfonyStyle) SetDecorated(d bool)   { s.output.SetDecorated(d) }
func (s *SymfonyStyle) IsVerbose() bool       { return s.output.Verbosity() >= VerbosityVerbose }
func (s *SymfonyStyle) IsVeryVerbose() bool   { return s.output.Verbosity() >= VerbosityVeryVerbose }
func (s *SymfonyStyle) IsDebug() bool         { return s.output.Verbosity() >= VerbosityDebug }
func (s *SymfonyStyle) Input() *Input         { return s.input }
func (s *SymfonyStyle) Output() Output        { return s.output }

// Writeln ports SymfonyStyle::writeln().
func (s *SymfonyStyle) Writeln(message string, options int) {
	s.output.Writeln(message, options)
	s.buffered.Write(message, true, options)
}

// Write ports SymfonyStyle::write().
func (s *SymfonyStyle) Write(message string, newline bool, options int) {
	s.output.Write(message, newline, options)
	s.buffered.Write(message, newline, options)
}

// WritelnAll writes several lines.
func (s *SymfonyStyle) WritelnAll(messages []string) {
	for _, m := range messages {
		s.Writeln(m, OutputNormal)
	}
}

// NewLine ports SymfonyStyle::newLine().
func (s *SymfonyStyle) NewLine(count int) {
	s.output.Write(strings.Repeat("\n", count), false, OutputNormal)
	s.buffered.Write(strings.Repeat("\n", count), false, OutputNormal)
}

func (s *SymfonyStyle) autoPrependBlock() {
	chars := s.buffered.fetch()
	if len(chars) > 2 {
		chars = chars[len(chars)-2:]
	}
	if chars == "" {
		s.NewLine(1)
		return
	}
	s.NewLine(2 - strings.Count(chars, "\n"))
}

func (s *SymfonyStyle) autoPrependText() {
	fetched := s.buffered.fetch()
	if fetched != "" && !strings.HasSuffix(fetched, "\n") {
		s.NewLine(1)
	}
}

// Block ports SymfonyStyle::block().
func (s *SymfonyStyle) Block(messages []string, typ, style, prefix string, padding, escape bool) {
	s.autoPrependBlock()
	s.WritelnAll(s.createBlock(messages, typ, style, prefix, padding, escape))
	s.NewLine(1)
}

func (s *SymfonyStyle) Title(message string) {
	s.autoPrependBlock()
	s.WritelnAll([]string{
		fmt.Sprintf("<comment>%s</>", EscapeTrailingBackslash(message)),
		fmt.Sprintf("<comment>%s</>", strings.Repeat("=", Width(RemoveDecoration(s.Formatter(), message)))),
	})
	s.NewLine(1)
}

func (s *SymfonyStyle) Section(message string) {
	s.autoPrependBlock()
	s.WritelnAll([]string{
		fmt.Sprintf("<comment>%s</>", EscapeTrailingBackslash(message)),
		fmt.Sprintf("<comment>%s</>", strings.Repeat("-", Width(RemoveDecoration(s.Formatter(), message)))),
	})
	s.NewLine(1)
}

func (s *SymfonyStyle) Listing(elements []string) {
	s.autoPrependText()
	for _, e := range elements {
		s.Writeln(" * "+e, OutputNormal)
	}
	s.NewLine(1)
}

func (s *SymfonyStyle) Text(messages ...string) {
	s.autoPrependText()
	for _, m := range messages {
		s.Writeln(" "+m, OutputNormal)
	}
}

func (s *SymfonyStyle) Comment(messages ...string) {
	s.Block(messages, "", "", "<fg=default;bg=default> // </>", false, false)
}

func (s *SymfonyStyle) Success(messages ...string) {
	s.Block(messages, "OK", "fg=black;bg=green", " ", true, true)
}

func (s *SymfonyStyle) Error(messages ...string) {
	s.Block(messages, "ERROR", "fg=white;bg=red", " ", true, true)
}

func (s *SymfonyStyle) Warning(messages ...string) {
	s.Block(messages, "WARNING", "fg=black;bg=yellow", " ", true, true)
}

func (s *SymfonyStyle) Note(messages ...string) {
	s.Block(messages, "NOTE", "fg=yellow", " ! ", false, true)
}

func (s *SymfonyStyle) Info(messages ...string) {
	s.Block(messages, "INFO", "fg=green", " ", true, true)
}

func (s *SymfonyStyle) Caution(messages ...string) {
	s.Block(messages, "CAUTION", "fg=white;bg=red", " ! ", true, true)
}

// Table ports SymfonyStyle::table().
func (s *SymfonyStyle) Table(headers []any, rows []any) {
	s.createTable().SetHeaders(headers).SetRows(rows).Render()
	s.NewLine(1)
}

// HorizontalTable ports SymfonyStyle::horizontalTable().
func (s *SymfonyStyle) HorizontalTable(headers []any, rows []any) {
	s.createTable().SetHorizontal(true).SetHeaders(headers).SetRows(rows).Render()
	s.NewLine(1)
}

// DefinitionList ports SymfonyStyle::definitionList(): items are strings (titles), TableSeparator or [2]string pairs.
func (s *SymfonyStyle) DefinitionList(items ...any) {
	var headers []any
	var row []any
	for _, v := range items {
		switch t := v.(type) {
		case TableSeparator:
			headers = append(headers, t)
			row = append(row, t)
		case string:
			headers = append(headers, &TableCell{Value: t, Colspan: 2})
			row = append(row, nil)
		case [2]string:
			headers = append(headers, t[0])
			row = append(row, t[1])
		}
	}
	s.HorizontalTable(headers, []any{row})
}

func (s *SymfonyStyle) createTable() *Table {
	style := tableStyles["symfony-style-guide"].Clone()
	style.CellHeaderFormat = "<info>%s</info>"
	// Upstream renders into a section of the console output, so the table bypasses the style's buffered output
	return NewTable(s.output).SetStyle(style)
}

var crlf = strings.NewReplacer("\r\n", "\n")

func (s *SymfonyStyle) createBlock(messages []string, typ, style, prefix string, padding, escape bool) []string {
	indentLength := 0
	prefixLength := Width(RemoveDecoration(s.Formatter(), prefix))
	var lines []string
	lineIndentation := ""
	if typ != "" {
		typ = "[" + typ + "] "
		indentLength = Width(typ)
		lineIndentation = strings.Repeat(" ", indentLength)
	}
	for key, message := range messages {
		if escape {
			message = Escape(message)
		}
		message = crlf.Replace(message)
		lines = append(lines, strings.Split(wrapOutput(message, s.lineLength-prefixLength-indentLength, "\n"), "\n")...)
		if len(messages) > 1 && key < len(messages)-1 {
			lines = append(lines, "")
		}
	}
	firstLineIndex := 0
	if padding && s.IsDecorated() {
		firstLineIndex = 1
		lines = append([]string{""}, lines...)
		lines = append(lines, "")
	}
	for i, line := range lines {
		if typ != "" {
			if i == firstLineIndex {
				line = typ + line
			} else {
				line = lineIndentation + line
			}
		}
		line = prefix + line
		paddingLength := max(s.lineLength-Width(RemoveDecoration(s.Formatter(), line)), 0)
		if style != "" && s.IsDecorated() && paddingLength > 0 {
			line += fmt.Sprintf("\033[%dX\033[%dC", paddingLength, paddingLength)
		} else {
			line += strings.Repeat(" ", paddingLength)
		}
		if style != "" {
			line = fmt.Sprintf("<%s>%s</>", style, line)
		}
		lines[i] = line
	}
	return lines
}

var wrapperTag = regexp.MustCompile(`^(?i)<(?:(?:[a-z](?:[^\\<>]*|\\.)*)|/(?:[a-z][^<>]*)?)>`)
var wrapperURL = regexp.MustCompile(`^(?i)https?://\S+`)

func isHorizontalSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\f' || c == '\v' }

// wrapTokens returns the end offsets of the row tokens starting at p: a style tag, a URL, or one character; rows never
// cross a line feed.
func wrapTokens(text string, p, limit int) []int {
	var ends []int
	for p < len(text) && text[p] != '\n' && len(ends) < limit {
		if m := wrapperTag.FindString(text[p:]); m != "" {
			p += len(m)
		} else if m := wrapperURL.FindString(text[p:]); m != "" {
			p += len(m)
		} else {
			_, size := utf8.DecodeRuneInString(text[p:])
			p += size
		}
		ends = append(ends, p)
	}
	return ends
}

// wrapOutput ports Helper\OutputWrapper::wrap() (URLs are never cut): rows of at most width tokens are broken at
// whitespace when possible, and cut otherwise.
func wrapOutput(text string, width int, brk string) string {
	if width <= 0 {
		return text
	}
	var out strings.Builder
	p := 0
	for {
		if p >= len(text) {
			out.WriteString(brk)
			break
		}
		if text[p] == '\n' || strings.HasPrefix(text[p:], "\r\n") {
			if text[p] == '\r' {
				p++
			}
			p++
			out.WriteString(brk)
			continue
		}
		ends := wrapTokens(text, p, width)
		matched := -1
		for j := len(ends) - 1; j >= 0 && matched < 0; j-- {
			e := ends[j]
			last := text[e-1]
			switch {
			case isHorizontalSpace(last):
				if e < len(text) && isHorizontalSpace(text[e]) {
					e++
				}
				matched = e
			case e == len(text) || text[e] == '\n' || strings.HasPrefix(text[e:], "\r\n"):
				matched = e
			case isHorizontalSpace(text[e]):
				matched = e + 1
			}
		}
		if matched < 0 {
			matched = ends[len(ends)-1]
		}
		out.WriteString(text[p:matched])
		p = matched
		if strings.HasPrefix(text[p:], "\r\n") {
			p += 2
		} else if p < len(text) && text[p] == '\n' {
			p++
		}
		out.WriteString(brk)
		if p >= len(text) {
			// the empty match at the very end adds one more break, which the final rtrim drops anyway
			break
		}
	}
	result := strings.TrimRight(out.String(), brk)
	return strings.ReplaceAll(result, " "+brk, brk)
}
