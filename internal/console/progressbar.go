package console

import (
	"fmt"
	"math"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ProgressBar ports the parts of Symfony's ProgressBar that Shlink uses (constructor, setMaxSteps(), setProgress(),
// finish()) with the default formats. Without decoration every redraw goes to a new line; with it, the line is
// overwritten and the OSC 9;4 taskbar progress is reported.
type ProgressBar struct {
	out *SymfonyStyle
	// now returns the current time; replaceable for tests.
	now func() time.Time

	barWidth        int
	max             *int
	step            int
	startingStep    int
	stepWidth       int
	percent         float64
	format          *string
	writeCount      int
	lastWriteTime   float64
	minSecsBetween  float64
	maxSecsBetween  float64
	overwrite       bool
	previousMessage *string
	startTime       int64
	oscState        *int
	oscPercent      *int
}

// NewProgressBar ports new ProgressBar($output) with the default 1/25s between redraws.
func NewProgressBar(out *SymfonyStyle) *ProgressBar {
	return newProgressBar(out, time.Now)
}

func newProgressBar(out *SymfonyStyle, now func() time.Time) *ProgressBar {
	p := &ProgressBar{out: out, now: now, barWidth: 28, minSecsBetween: 1.0 / 25, maxSecsBetween: 1, overwrite: true}
	p.SetMaxSteps(0)
	if !out.IsDecorated() {
		p.overwrite = false
	}
	p.startTime = now().Unix()
	return p
}

func (p *ProgressBar) microtime() float64 { return float64(p.now().UnixMicro()) / 1e6 }

// SetMaxSteps ports setMaxSteps(); 0 means unknown.
func (p *ProgressBar) SetMaxSteps(max int) {
	p.format = nil
	if max == 0 {
		p.max = nil
		p.stepWidth = 4
		return
	}
	m := maxInt(0, max)
	p.max = &m
	p.stepWidth = Width(strconv.Itoa(m))
}

// SetProgress ports setProgress().
func (p *ProgressBar) SetProgress(step int) {
	if p.max != nil && *p.max != 0 && step > *p.max {
		*p.max = step
	} else if step < 0 {
		step = 0
	}
	// redrawFreq is always null here, so a period is a tenth of the max
	redrawFreq := 1.0
	if p.max != nil {
		redrawFreq = float64(*p.max) / 10
	}
	prevPeriod, currPeriod := 0, 0
	if redrawFreq != 0 {
		prevPeriod, currPeriod = int(float64(p.step)/redrawFreq), int(float64(step)/redrawFreq)
	}
	p.step = step
	switch {
	case p.max == nil:
		p.percent = 0
	case *p.max == 0:
		p.percent = 1
	default:
		p.percent = float64(p.step) / float64(*p.max)
	}
	interval := p.microtime() - p.lastWriteTime
	if p.max != nil && *p.max == step {
		p.Display()
		return
	}
	if interval < p.minSecsBetween {
		return
	}
	if prevPeriod != currPeriod || interval >= p.maxSecsBetween {
		p.Display()
	}
}

// Finish ports finish().
func (p *ProgressBar) Finish() {
	if p.max == nil {
		s := p.step
		p.max = &s
	}
	if !(p.step == *p.max && !p.overwrite) {
		p.SetProgress(*p.max)
	}
	p.writeOscProgressReset()
}

// Display ports display().
func (p *ProgressBar) Display() {
	if p.out.Verbosity() == VerbosityQuiet {
		return
	}
	if p.format == nil {
		f := p.bestFormat()
		p.format = &f
	}
	p.write(p.buildLine())
	p.writeOscProgress()
}

var progressFormats = map[string]string{
	"normal":             " %current%/%max% [%bar%] %percent:3s%%",
	"normal_nomax":       " %current% [%bar%]",
	"verbose":            " %current%/%max% [%bar%] %percent:3s%% %elapsed:6s%",
	"verbose_nomax":      " %current% [%bar%] %elapsed:6s%",
	"very_verbose":       " %current%/%max% [%bar%] %percent:3s%% %elapsed:6s%/%estimated:-6s%",
	"very_verbose_nomax": " %current% [%bar%] %elapsed:6s%",
	"debug":              " %current%/%max% [%bar%] %percent:3s%% %elapsed:6s%/%estimated:-6s% %memory:6s%",
	"debug_nomax":        " %current% [%bar%] %elapsed:6s% %memory:6s%",
}

// bestFormat ports determineBestFormat() followed by setRealFormat().
func (p *ProgressBar) bestFormat() string {
	name := "normal"
	switch p.out.Verbosity() {
	case VerbosityVerbose:
		name = "verbose"
	case VerbosityVeryVerbose:
		name = "very_verbose"
	case VerbosityDebug:
		name = "debug"
	}
	if p.max == nil || *p.max == 0 {
		name += "_nomax"
	}
	return progressFormats[name]
}

func (p *ProgressBar) maxSteps() int {
	if p.max == nil {
		return 0
	}
	return *p.max
}

func (p *ProgressBar) barOffset() int {
	if p.max != nil {
		return int(math.Floor(p.percent * float64(p.barWidth)))
	}
	return int(math.Min(5, float64(p.barWidth)/15)*float64(p.writeCount)) % p.barWidth
}

func (p *ProgressBar) estimated(remaining bool) float64 {
	if p.step == 0 || p.step == p.startingStep {
		return 0
	}
	steps := p.maxSteps()
	if remaining {
		steps -= p.step
	}
	return math.Round(float64(p.now().Unix()-p.startTime) / float64(p.step-p.startingStep) * float64(steps))
}

func (p *ProgressBar) placeholder(name string) (string, bool) {
	switch name {
	case "bar":
		complete := p.barOffset()
		barChar := "-"
		if p.max != nil {
			barChar = "="
		}
		display := strings.Repeat(barChar, complete)
		if complete < p.barWidth {
			empty := p.barWidth - complete - 1
			display += ">" + strings.Repeat("-", maxInt(0, empty))
		}
		return display, true
	case "elapsed":
		return FormatTime(float64(p.now().Unix()-p.startTime), 2), true
	case "remaining":
		return FormatTime(p.estimated(true), 2), true
	case "estimated":
		return FormatTime(p.estimated(false), 2), true
	case "memory":
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return FormatMemory(int64(m.Sys)), true
	case "current":
		return strPad(strconv.Itoa(p.step), p.stepWidth, " ", PadLeft), true
	case "max":
		return strconv.Itoa(p.maxSteps()), true
	case "percent":
		return strconv.FormatFloat(math.Floor(p.percent*100), 'f', -1, 64), true
	}
	return "", false
}

var progressPlaceholder = regexp.MustCompile(`(?i)%([a-z\-_]+)(?::([^%]+))?%`)

// buildLine ports buildLine(), shrinking the bar when the line is wider than the terminal.
func (p *ProgressBar) buildLine() string {
	build := func() string {
		return progressPlaceholder.ReplaceAllStringFunc(*p.format, func(m string) string {
			sub := progressPlaceholder.FindStringSubmatch(m)
			text, ok := p.placeholder(sub[1])
			if !ok {
				return m
			}
			if sub[2] != "" {
				text = phpSprintfString(sub[2], text)
			}
			return text
		})
	}
	line := build()
	width := 0
	for _, sub := range strings.Split(line, "\n") {
		width = maxInt(width, Width(RemoveDecoration(p.out.Formatter(), strings.TrimRight(sub, "\r"))))
	}
	if terminal := TerminalWidth(); width > terminal {
		p.barWidth = maxInt(1, p.barWidth-width+terminal)
		return build()
	}
	return line
}

// phpSprintfString ports sprintf('%'.$spec, $text) for the string specs the formats use ("3s", "6s", "-6s").
func phpSprintfString(spec, text string) string {
	left := strings.HasPrefix(spec, "-")
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(spec, "-"), "s"))
	if err != nil || !strings.HasSuffix(spec, "s") {
		return text
	}
	if left {
		return fmt.Sprintf("%-*s", n, text)
	}
	return fmt.Sprintf("%*s", n, text)
}

// write ports overwrite().
func (p *ProgressBar) write(message string) {
	if p.previousMessage != nil && *p.previousMessage == message {
		return
	}
	original := message
	if p.overwrite {
		if p.previousMessage != nil {
			for i := 0; i < strings.Count(*p.previousMessage, "\n"); i++ {
				p.out.Write("\x1b[1G", false, OutputNormal)
				p.out.Write("\x1b[2K", false, OutputNormal)
				p.out.Write("\x1b[1A", false, OutputNormal)
			}
			p.out.Write("\x1b[1G", false, OutputNormal)
			p.out.Write("\x1b[2K", false, OutputNormal)
		}
	} else if p.step > 0 {
		message = "\n" + message
	}
	p.previousMessage = &original
	p.lastWriteTime = p.microtime()
	p.out.Write(message, false, OutputNormal)
	p.writeCount++
}

func (p *ProgressBar) writeOscProgress() {
	if !p.out.IsDecorated() {
		return
	}
	state, percent := 1, int(math.Floor(math.Max(0, math.Min(1, p.percent))*100))
	if p.max == nil {
		state, percent = 3, 0
	}
	if p.oscState != nil && *p.oscState == state && *p.oscPercent == percent {
		return
	}
	p.oscState, p.oscPercent = &state, &percent
	p.out.Write(fmt.Sprintf("\x1b]9;4;%d;%d\x1b\\", state, percent), false, OutputNormal)
}

func (p *ProgressBar) writeOscProgressReset() {
	if p.oscState == nil {
		return
	}
	p.oscState, p.oscPercent = nil, nil
	if p.out.IsDecorated() {
		p.out.Write("\x1b]9;4;0;0\x1b\\", false, OutputNormal)
	}
}

// FormatTime ports Helper::formatTime().
func FormatTime(secs float64, precision int) string {
	ms := int64(secs * 1000)
	if ms == 0 {
		return "< 1 ms"
	}
	units := []struct {
		ms   int64
		name string
	}{{1, "ms"}, {1000, "s"}, {60000, "min"}, {3600000, "h"}, {86_400_000, "d"}}
	times := map[int]string{}
	for i, u := range units {
		milli := ms
		if i+1 < len(units) {
			milli = ms % units[i+1].ms
		}
		delete(times, i-precision)
		if milli == 0 {
			continue
		}
		times[i] = phpNumber(float64(milli)/float64(u.ms)) + " " + u.name
		if ms == milli {
			break
		}
		ms -= milli
	}
	var out []string
	for i := len(units) - 1; i >= 0; i-- {
		if t, ok := times[i]; ok {
			out = append(out, t)
		}
	}
	return strings.Join(out, ", ")
}

// phpNumber ports PHP's string conversion of an int|float division result.
func phpNumber(f float64) string {
	return strconv.FormatFloat(f, 'G', 14, 64)
}

// FormatMemory ports Helper::formatMemory().
func FormatMemory(memory int64) string {
	switch {
	case memory >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GiB", float64(memory)/1024/1024/1024)
	case memory >= 1024*1024:
		return fmt.Sprintf("%.1f MiB", float64(memory)/1024/1024)
	case memory >= 1024:
		return fmt.Sprintf("%d KiB", memory/1024)
	}
	return fmt.Sprintf("%d B", memory)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
