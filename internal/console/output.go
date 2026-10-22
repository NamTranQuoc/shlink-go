package console

import (
	"io"
	"os"
	"regexp"
	"strings"
)

// Verbosity levels, like OutputInterface's constants.
const (
	VerbositySilent      = 8
	VerbosityQuiet       = 16
	VerbosityNormal      = 32
	VerbosityVerbose     = 64
	VerbosityVeryVerbose = 128
	VerbosityDebug       = 256
)

// Output types.
const (
	OutputNormal = 1
	OutputRaw    = 2
	OutputPlain  = 4
)

// Output ports OutputInterface's write side over an io.Writer (StreamOutput) or a trimmed buffer.
type Output interface {
	Write(message string, newline bool, options int)
	Writeln(message string, options int)
	Verbosity() int
	SetVerbosity(int)
	Formatter() *Formatter
	IsDecorated() bool
	SetDecorated(bool)
}

// StreamOutput ports StreamOutput.
type StreamOutput struct {
	w         io.Writer
	verbosity int
	formatter *Formatter
	// trim keeps only the last trim bytes, porting TrimmedBufferOutput when > 0.
	trim   int
	buffer strings.Builder
}

// NewStreamOutput creates an output over a writer; decorated is detected like StreamOutput::hasColorSupport().
func NewStreamOutput(w io.Writer) *StreamOutput {
	return &StreamOutput{w: w, verbosity: VerbosityNormal, formatter: NewFormatter(hasColorSupport(w))}
}

var colorTerm = regexp.MustCompile(`^((screen|xterm|vt100|vt220|putty|rxvt|ansi|cygwin|linux).*)|(.*-256(color)?(-bce)?)$`)

func hasColorSupport(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if os.Getenv("TERM_PROGRAM") == "Hyper" {
		return true
	}
	if _, ok := os.LookupEnv("COLORTERM"); ok {
		return true
	}
	if _, ok := os.LookupEnv("ANSICON"); ok {
		return true
	}
	if os.Getenv("ConEmuANSI") == "ON" {
		return true
	}
	term := os.Getenv("TERM")
	if term == "dumb" {
		return false
	}
	return colorTerm.MatchString(term)
}

func (o *StreamOutput) Verbosity() int           { return o.verbosity }
func (o *StreamOutput) SetVerbosity(v int)       { o.verbosity = v }
func (o *StreamOutput) Formatter() *Formatter    { return o.formatter }
func (o *StreamOutput) IsDecorated() bool        { return o.formatter.decorated }
func (o *StreamOutput) SetDecorated(d bool)      { o.formatter.decorated = d }
func (o *StreamOutput) Writeln(m string, op int) { o.Write(m, true, op) }

// Write ports Output::write().
func (o *StreamOutput) Write(message string, newline bool, options int) {
	typ := options & (OutputNormal | OutputRaw | OutputPlain)
	if typ == 0 {
		typ = OutputNormal
	}
	verbosity := options & (VerbosityQuiet | VerbosityNormal | VerbosityVerbose | VerbosityVeryVerbose | VerbosityDebug)
	if verbosity == 0 {
		verbosity = VerbosityNormal
	}
	if verbosity > o.verbosity {
		return
	}
	switch typ {
	case OutputNormal:
		message = o.formatter.Format(message)
	case OutputPlain:
		message = stripTags(o.formatter.Format(message))
	}
	if newline {
		message += "\n"
	}
	if o.trim > 0 {
		o.buffer.WriteString(message)
		if b := o.buffer.String(); len(b) > o.trim {
			o.buffer.Reset()
			o.buffer.WriteString(b[len(b)-o.trim:])
		}
		return
	}
	_, _ = io.WriteString(o.w, message)
}

// fetch ports TrimmedBufferOutput::fetch().
func (o *StreamOutput) fetch() string {
	s := o.buffer.String()
	o.buffer.Reset()
	return s
}

var htmlTag = regexp.MustCompile(`<[^>]*>?`)

// stripTags approximates strip_tags() for console messages.
func stripTags(s string) string { return htmlTag.ReplaceAllString(s, "") }

// ConsoleOutput ports ConsoleOutput: stdout plus an error output sharing verbosity and decoration.
type ConsoleOutput struct {
	*StreamOutput
	Err *StreamOutput
}

// NewConsoleOutput creates an output over stdout/stderr.
func NewConsoleOutput(stdout, stderr io.Writer) *ConsoleOutput {
	return &ConsoleOutput{StreamOutput: NewStreamOutput(stdout), Err: NewStreamOutput(stderr)}
}

func (o *ConsoleOutput) SetVerbosity(v int) { o.StreamOutput.SetVerbosity(v); o.Err.SetVerbosity(v) }
func (o *ConsoleOutput) SetDecorated(d bool) {
	o.StreamOutput.SetDecorated(d)
	o.Err.SetDecorated(d)
}

// ErrorOutput returns the error output for an output (itself when it is not a console output).
func ErrorOutput(o Output) Output {
	if c, ok := o.(*ConsoleOutput); ok {
		return c.Err
	}
	return o
}
