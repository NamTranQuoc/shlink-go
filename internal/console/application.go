// Package console ports the parts of symfony/console Shlink's CLI relies on (Application, input parsing, help and
// list descriptors, output formatting, SymfonyStyle, tables and questions), so commands print exactly what the
// upstream PHP CLI prints.
package console

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Exit codes, like Command's constants.
const (
	Success = 0
	Failure = 1
	Invalid = 2
)

// IO is what a command receives: the bound input, the output and the application.
type IO struct {
	Input  *Input
	Output Output
	App    *Application
	stdin  *bufio.Reader
}

// Style creates a fresh SymfonyStyle, like each invokable command parameter resolution does.
func (io *IO) Style() *SymfonyStyle { return NewSymfonyStyle(io.Input, io.Output, io.stdin) }

// Command ports Command for Shlink's invokable commands.
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Help        string
	Hidden      bool
	Usages      []string
	Definition  *Definition
	// IgnoreValidationErrors ports Command::ignoreValidationErrors().
	IgnoreValidationErrors bool
	// Interact runs before validation when the input is interactive (Command::interact() plus the invokable command's
	// #[Ask]/#[Interact] interactions, in that order).
	Interact func(io *IO) error
	// Execute runs the command.
	Execute func(io *IO) (int, error)

	app            *Application
	fullDefinition *Definition
	helpTarget     *Command
}

// NativeDefinition returns the command's own definition.
func (c *Command) NativeDefinition() *Definition {
	if c.Definition == nil {
		c.Definition = NewDefinition(nil, nil)
	}
	return c.Definition
}

// FullDefinition returns the merged definition when available, like Command::getDefinition().
func (c *Command) FullDefinition() *Definition {
	if c.fullDefinition != nil {
		return c.fullDefinition
	}
	return c.NativeDefinition()
}

// MergeApplicationDefinition ports Command::mergeApplicationDefinition().
func (c *Command) MergeApplicationDefinition(mergeArgs bool) {
	if c.app == nil {
		return
	}
	full := NewDefinition(nil, nil)
	full.SetOptions(append([]*Option(nil), c.NativeDefinition().Options()...))
	full.AddOptions(c.app.definition().Options())
	if mergeArgs {
		full.SetArguments(append([]*Argument(nil), c.app.definition().Arguments()...))
		for _, a := range c.NativeDefinition().Arguments() {
			full.AddArgument(a)
		}
	} else {
		full.SetArguments(append([]*Argument(nil), c.NativeDefinition().Arguments()...))
	}
	c.fullDefinition = full
}

// Synopsis ports Command::getSynopsis().
func (c *Command) Synopsis(short bool) string {
	return strings.TrimSpace(c.Name + " " + c.NativeDefinition().Synopsis(short))
}

// ProcessedHelp ports Command::getProcessedHelp().
func (c *Command) ProcessedHelp() string {
	help := c.Help
	if help == "" {
		help = c.Description
	}
	self := phpSelf()
	return strings.NewReplacer("%command.name%", c.Name, "%command.full_name%", self+" "+c.Name).Replace(help)
}

// phpSelf reproduces $_SERVER['PHP_SELF']: the script path as it was invoked (resolved through PATH when it has no
// slash, like the kernel does for a shebang script).
func phpSelf() string {
	self := os.Args[0]
	if !strings.Contains(self, "/") {
		if p, err := exec.LookPath(self); err == nil {
			return p
		}
	}
	return self
}

func (c *Command) run(io *IO) (int, error) {
	c.MergeApplicationDefinition(true)
	if err := io.Input.Bind(c.FullDefinition()); err != nil && !c.IgnoreValidationErrors {
		return 0, err
	}
	if io.Input.HasArgument("command") {
		io.Input.SetArgument("command", c.Name)
	}
	if io.Input.IsInteractive() && c.Interact != nil {
		if err := c.Interact(io); err != nil {
			return 0, err
		}
	}
	if err := io.Input.Validate(); err != nil {
		return 0, err
	}
	return c.Execute(io)
}

// Application ports Application with a command loader.
type Application struct {
	Name    string
	Version string

	commands map[string]*Command
	// order lists every name in registration order: the default commands, then the command loader's names.
	order []string
	// loaded lists the names in the order they were loaded, like the keys of Application::$commands. Default commands
	// are loaded upfront; the others are loaded the first time has() or get() sees them, which decides the order of
	// all().
	loaded         []string
	isLoaded       map[string]bool
	appDefinition  *Definition
	runningCommand *Command
	wantHelps      bool
	defaultCommand string
	Stdout, Stderr io.Writer
	Stdin          io.Reader
}

// NewApplication creates an application with the default help, list, completion and _complete commands.
func NewApplication(name, version string) *Application {
	a := &Application{Name: name, Version: version, commands: map[string]*Command{}, isLoaded: map[string]bool{},
		defaultCommand: "list", Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}
	for _, c := range []*Command{a.helpCommand(), a.listCommand(), a.completeCommand(), a.dumpCompletionCommand()} {
		a.Add(c)
		a.load(c.Name)
	}
	return a
}

// Add registers a command and its aliases in the command loader.
func (a *Application) Add(c *Command) {
	c.app = a
	c.NativeDefinition()
	if _, exists := a.commands[c.Name]; !exists {
		a.order = append(a.order, c.Name)
	}
	a.commands[c.Name] = c
	for _, alias := range c.Aliases {
		if _, exists := a.commands[alias]; !exists {
			a.order = append(a.order, alias)
		}
		a.commands[alias] = c
	}
}

func (a *Application) definition() *Definition {
	if a.appDefinition == nil {
		a.appDefinition = NewDefinition([]*Argument{NewArgument("command", ArgRequired, "The command to execute", nil)}, []*Option{
			NewOption("--help", "-h", OptValueNone, "Display help for the given command. When no command is given display help for the <info>"+a.defaultCommand+"</info> command", nil),
			NewOption("--silent", "", OptValueNone, "Do not output any message", nil),
			NewOption("--quiet", "-q", OptValueNone, "Only errors are displayed. All other output is suppressed", nil),
			NewOption("--verbose", "-v|vv|vvv", OptValueNone, "Increase the verbosity of messages: 1 for normal output, 2 for more verbose output and 3 for debug", nil),
			NewOption("--version", "-V", OptValueNone, "Display this application version", nil),
			NewOption("--ansi", "", OptValueNegatable, "Force (or disable --no-ansi) ANSI output", nil),
			NewOption("--no-interaction", "-n", OptValueNone, "Do not ask any interactive question", nil),
		})
	}
	return a.appDefinition
}

// LongVersion ports Application::getLongVersion().
func (a *Application) LongVersion() string {
	if a.Name != "UNKNOWN" {
		if a.Version != "UNKNOWN" {
			return fmt.Sprintf("%s <info>%s</info>", a.Name, a.Version)
		}
		return a.Name
	}
	return "Console Tool"
}

// Run ports Application::run(); it returns the process exit code.
func (a *Application) Run(args []string) int {
	os.Setenv("LINES", strconv.Itoa(TerminalHeight()))
	os.Setenv("COLUMNS", strconv.Itoa(TerminalWidth()))
	input := NewArgvInput(args)
	output := NewConsoleOutput(a.Stdout, a.Stderr)
	stdin := bufio.NewReader(a.Stdin)
	var exitCode int
	func() {
		defer func() {
			if r := recover(); r != nil {
				if ex, ok := r.(Exception); ok {
					exitCode = a.handleError(ex, output)
					return
				}
				panic(r)
			}
		}()
		a.configureIO(input, output)
		code, err := a.doRun(input, output, stdin)
		if err != nil {
			exitCode = a.handleError(err, output)
			return
		}
		exitCode = code
	}()
	if exitCode > 255 {
		exitCode = 255
	}
	return exitCode
}

func (a *Application) handleError(err error, output *ConsoleOutput) int {
	var fatal *PHPFatalError
	if errors.As(err, &fatal) {
		output.Write(fatal.Output, false, OutputRaw)
		return 255
	}
	ex := asException(err)
	a.RenderThrowable(ex, output.Err)
	if code := ex.Code(); code > 0 {
		return code
	}
	return 1
}

func (a *Application) configureIO(input *Input, output *ConsoleOutput) {
	if input.HasParameterOption([]string{"--ansi"}, true) {
		output.SetDecorated(true)
	} else if input.HasParameterOption([]string{"--no-ansi"}, true) {
		output.SetDecorated(false)
	}
	verbosityValue, hasVerbose := input.ParameterOption([]string{"--verbose"}, true)
	var shell int
	switch {
	case input.HasParameterOption([]string{"--silent"}, true):
		shell = -2
	case input.HasParameterOption([]string{"--quiet", "-q"}, true):
		shell = -1
	case input.HasParameterOption([]string{"-vvv"}, true) || input.HasParameterOption([]string{"--verbose=3"}, true):
		shell = 3
	case input.HasParameterOption([]string{"-vv"}, true) || input.HasParameterOption([]string{"--verbose=2"}, true):
		shell = 2
	case input.HasParameterOption([]string{"-v"}, true) || input.HasParameterOption([]string{"--verbose=1"}, true) ||
		input.HasParameterOption([]string{"--verbose"}, true) || (hasVerbose && verbosityValue != "" && verbosityValue != "0"):
		shell = 1
	default:
		shell = phpIntCast(os.Getenv("SHELL_VERBOSITY"))
	}
	switch shell {
	case -2:
		output.SetVerbosity(VerbositySilent)
	case -1:
		output.SetVerbosity(VerbosityQuiet)
	case 1:
		output.SetVerbosity(VerbosityVerbose)
	case 2:
		output.SetVerbosity(VerbosityVeryVerbose)
	case 3:
		output.SetVerbosity(VerbosityDebug)
	default:
		shell = 0
	}
	if shell < 0 || input.HasParameterOption([]string{"--no-interaction", "-n"}, true) {
		input.SetInteractive(false)
	}
	os.Setenv("SHELL_VERBOSITY", strconv.Itoa(shell))
}

var leadingInt = regexp.MustCompile(`^\s*[+-]?\d+`)

func phpIntCast(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(leadingInt.FindString(s)))
	return n
}

func (a *Application) doRun(input *Input, output *ConsoleOutput, stdin *bufio.Reader) (int, error) {
	if input.HasParameterOption([]string{"--version", "-V"}, true) {
		output.Writeln(a.LongVersion(), OutputNormal)
		return 0, nil
	}
	_ = input.Bind(a.definition())
	name := input.FirstArgument()
	if input.HasParameterOption([]string{"--help", "-h"}, true) {
		if name == "" {
			name = "help"
			input = NewArrayInput(map[string]any{"command_name": a.defaultCommand})
		} else {
			a.wantHelps = true
		}
	}
	if name == "" {
		name = a.defaultCommand
		d := a.definition()
		args := append([]*Argument(nil), d.Arguments()...)
		for i, arg := range args {
			if arg.Name == "command" {
				args[i] = NewArgument("command", ArgOptional, arg.Description, name)
			}
		}
		d.SetArguments(args)
	}
	a.runningCommand = nil
	command, err := a.Find(name)
	if err != nil {
		if nf, ok := err.(*CommandNotFoundError); ok && !nf.Namespace && len(nf.Alternatives) == 1 && input.IsInteractive() {
			alternative := nf.Alternatives[0]
			style := NewSymfonyStyle(input, output, stdin)
			output.Writeln("", OutputNormal)
			output.WritelnAll(formatBlock([]string{fmt.Sprintf(`Command "%s" is not defined.`, name)}, "error", true))
			ok, cerr := style.Confirm(fmt.Sprintf(`Do you want to run "%s" instead? `, alternative), false)
			if cerr != nil {
				return 0, cerr
			}
			if !ok {
				return 1, nil
			}
			command, err = a.Find(alternative)
			if err != nil {
				return 0, err
			}
		} else {
			if _, ok := err.(*CommandNotFoundError); ok {
				if namespace, nsErr := a.FindNamespace(name); nsErr == nil && namespace != "" {
					a.describeApplication(output.Err, namespace, false)
					return 1, nil
				}
			}
			return 0, err
		}
	}
	a.runningCommand = command
	io := &IO{Input: input, Output: output, App: a, stdin: stdin}
	code, err := command.run(io)
	if err != nil {
		return 0, err
	}
	a.runningCommand = nil
	return code, nil
}

// WritelnAll writes several lines to a stream output.
func (o *StreamOutput) WritelnAll(lines []string) {
	for _, l := range lines {
		o.Writeln(l, OutputNormal)
	}
}

// formatBlock ports FormatterHelper::formatBlock().
func formatBlock(messages []string, style string, large bool) []string {
	length := 0
	var lines []string
	for _, m := range messages {
		m = Escape(m)
		if large {
			lines = append(lines, "  "+m+"  ")
		} else {
			lines = append(lines, " "+m+" ")
		}
		length = max(length, Width(m))
		if large {
			length = max(length, Width(m)+4)
		} else {
			length = max(length, Width(m)+2)
		}
	}
	var out []string
	if large {
		out = append(out, strings.Repeat(" ", length))
	}
	for _, l := range lines {
		out = append(out, l+strings.Repeat(" ", length-Width(l)))
	}
	if large {
		out = append(out, strings.Repeat(" ", length))
	}
	for i := range out {
		out[i] = fmt.Sprintf("<%s>%s</%s>", style, out[i], style)
	}
	return out
}

func (a *Application) load(name string) {
	if !a.isLoaded[name] {
		a.isLoaded[name] = true
		a.loaded = append(a.loaded, name)
	}
}

// has ports Application::has(), which loads the command from the command loader.
func (a *Application) has(name string) bool {
	if _, ok := a.commands[name]; !ok {
		return false
	}
	a.load(name)
	return true
}

func (a *Application) get(name string) (*Command, error) {
	c := a.commands[name]
	if !a.has(name) {
		return nil, &CommandNotFoundError{Message: fmt.Sprintf(`The command "%s" does not exist.`, name), line: 636}
	}
	if a.wantHelps {
		a.wantHelps = false
		help, _ := a.get("help")
		help.helpTarget = c
		return help, nil
	}
	return c, nil
}

func extractAllNamespaces(name string) []string {
	parts := strings.Split(name, ":")
	parts = parts[:len(parts)-1]
	var out []string
	for _, p := range parts {
		if len(out) > 0 {
			out = append(out, out[len(out)-1]+":"+p)
		} else {
			out = append(out, p)
		}
	}
	return out
}

// Namespaces ports Application::getNamespaces().
func (a *Application) Namespaces() []string {
	var all []string
	seen := map[string]bool{}
	for _, name := range a.all(nil) {
		c := a.commands[name]
		if c.Hidden || name != c.Name {
			continue
		}
		names := append([]string{c.Name}, c.Aliases...)
		for _, n := range names {
			for _, ns := range extractAllNamespaces(n) {
				if ns != "" && !seen[ns] {
					seen[ns] = true
					all = append(all, ns)
				}
			}
		}
	}
	return all
}

func namePattern(name string, anchored bool, insensitive bool) *regexp.Regexp {
	parts := strings.Split(name, ":")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	expr := "^" + strings.Join(parts, "[^:]*:") + "[^:]*"
	if anchored {
		expr += "$"
	}
	if insensitive {
		expr = "(?i)" + expr
	}
	return regexp.MustCompile(expr)
}

// FindNamespace ports Application::findNamespace().
func (a *Application) FindNamespace(namespace string) (string, error) {
	all := a.Namespaces()
	re := namePattern(namespace, false, false)
	var matches []string
	for _, ns := range all {
		if re.MatchString(ns) {
			matches = append(matches, ns)
		}
	}
	if len(matches) == 0 {
		message := fmt.Sprintf(`There are no commands defined in the "%s" namespace.`, namespace)
		alternatives := findAlternatives(namespace, all)
		if len(alternatives) > 0 {
			if len(alternatives) == 1 {
				message += "\n\nDid you mean this?\n    "
			} else {
				message += "\n\nDid you mean one of these?\n    "
			}
			message += strings.Join(alternatives, "\n    ")
		}
		return "", &CommandNotFoundError{Message: message, Alternatives: alternatives, Namespace: true, line: 717}
	}
	exact := false
	for _, m := range matches {
		if m == namespace {
			exact = true
		}
	}
	if len(matches) > 1 && !exact {
		sort.Strings(matches)
		return "", &CommandNotFoundError{Message: fmt.Sprintf("The namespace \"%s\" is ambiguous.\nDid you mean one of these?\n%s.", namespace, "    "+strings.Join(matches, "\n    ")), Alternatives: matches, Namespace: true, line: 724}
	}
	if exact {
		return namespace, nil
	}
	return matches[0], nil
}

// Find ports Application::find().
func (a *Application) Find(name string) (*Command, error) {
	if a.has(name) {
		return a.get(name)
	}
	all := append([]string(nil), a.order...)
	var commands []string
	re := namePattern(name, false, false)
	for _, n := range all {
		if re.MatchString(n) {
			commands = append(commands, n)
		}
	}
	if len(commands) == 0 {
		rei := namePattern(name, false, true)
		for _, n := range all {
			if rei.MatchString(n) {
				commands = append(commands, n)
			}
		}
	}
	sort.Strings(commands)
	exactCount := 0
	anchored := namePattern(name, true, true)
	for _, c := range commands {
		if anchored.MatchString(c) {
			exactCount++
		}
	}
	if len(commands) == 0 || exactCount < 1 {
		if pos := strings.LastIndex(name, ":"); pos >= 0 {
			if _, err := a.FindNamespace(name[:pos]); err != nil {
				return nil, err
			}
		}
		message := fmt.Sprintf(`Command "%s" is not defined.`, name)
		alternatives := findAlternatives(name, all)
		if len(alternatives) > 0 {
			var visible []string
			for _, alt := range alternatives {
				a.load(alt)
				if !a.commands[alt].Hidden {
					visible = append(visible, alt)
				}
			}
			alternatives = visible
			if len(alternatives) > 0 {
				which := "one of these"
				if len(alternatives) == 1 {
					which = "this"
				}
				message += fmt.Sprintf("\n\nDid you mean %s?\n    %s", which, strings.Join(alternatives, "\n    "))
			}
		}
		return nil, &CommandNotFoundError{Message: message, Alternatives: alternatives, line: 786}
	}
	if len(commands) > 1 {
		var filtered []string
		seen := map[string]bool{}
		for _, n := range commands {
			cmdName := a.commands[n].Name
			keep := cmdName == n || !contains(commands, cmdName)
			if keep && !seen[n] {
				seen[n] = true
				filtered = append(filtered, n)
			}
		}
		commands = filtered
	}
	if len(commands) > 1 {
		unique := map[string]bool{}
		for _, n := range commands {
			unique[a.commands[n].Name] = true
		}
		if len(unique) == 1 {
			commands = []string{a.commands[commands[0]].Name}
		}
	}
	if len(commands) > 1 {
		sort.Strings(commands)
		usableWidth := TerminalWidth() - 10
		maxLen := 0
		for _, c := range commands {
			maxLen = max(maxLen, Width(c))
		}
		var abbrevs, visible []string
		for _, c := range commands {
			if a.commands[c].Hidden {
				continue
			}
			visible = append(visible, c)
			abbrev := fmt.Sprintf("%-*s %s", maxLen, c, a.commands[c].Description)
			if Width(abbrev) > usableWidth {
				abbrev = Substr(abbrev, 0, usableWidth-3) + "..."
			}
			abbrevs = append(abbrevs, abbrev)
		}
		if len(visible) > 1 {
			return nil, &CommandNotFoundError{Message: fmt.Sprintf("Command \"%s\" is ambiguous.\nDid you mean one of these?\n%s", name, "    "+strings.Join(abbrevs, "\n    ")), Alternatives: visible, line: 843}
		}
		commands = visible
	}
	if len(commands) == 0 {
		return nil, &CommandNotFoundError{Message: fmt.Sprintf(`The command "%s" does not exist.`, name), line: 850}
	}
	c, err := a.get(commands[0])
	if err != nil {
		return nil, err
	}
	if c.Hidden {
		return nil, &CommandNotFoundError{Message: fmt.Sprintf(`The command "%s" does not exist.`, name), line: 850}
	}
	return c, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// findAlternatives ports Application::findAlternatives().
func findAlternatives(name string, collection []string) []string {
	const threshold = 1000
	alternatives := map[string]int{}
	parts := map[string][]string{}
	for _, item := range collection {
		parts[item] = strings.Split(item, ":")
	}
	for i, sub := range strings.Split(name, ":") {
		for _, item := range collection {
			p := parts[item]
			_, exists := alternatives[item]
			if i >= len(p) {
				if exists {
					alternatives[item] += threshold
				}
				continue
			}
			lev := levenshtein(sub, p[i])
			if float64(lev) <= float64(len(sub))/3 || (sub != "" && strings.Contains(p[i], sub)) {
				if exists {
					alternatives[item] += lev
				} else {
					alternatives[item] = lev
				}
			} else if exists {
				alternatives[item] += threshold
			}
		}
	}
	for _, item := range collection {
		lev := levenshtein(name, item)
		if float64(lev) <= float64(len(name))/3 || strings.Contains(item, name) {
			if v, ok := alternatives[item]; ok {
				alternatives[item] = v - lev
			} else {
				alternatives[item] = lev
			}
		}
	}
	var keys []string
	for k, v := range alternatives {
		if v < 2*threshold {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return naturalLess(strings.ToLower(keys[i]), strings.ToLower(keys[j])) })
	return keys
}

var digitsRun = regexp.MustCompile(`\d+|\D+`)

// naturalLess approximates ksort(SORT_NATURAL | SORT_FLAG_CASE).
func naturalLess(a, b string) bool {
	pa, pb := digitsRun.FindAllString(a, -1), digitsRun.FindAllString(b, -1)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] == pb[i] {
			continue
		}
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		if ea == nil && eb == nil {
			return na < nb
		}
		return pa[i] < pb[i]
	}
	return len(pa) < len(pb)
}

// RenderThrowable ports Application::renderThrowable().
func (a *Application) RenderThrowable(ex Exception, output Output) {
	output.Writeln("", VerbosityQuiet)
	a.doRenderThrowable(ex, output)
	if a.runningCommand != nil {
		output.Writeln(fmt.Sprintf("<info>%s</info>", Escape(a.runningCommand.Synopsis(false))), VerbosityQuiet)
		output.Writeln("", VerbosityQuiet)
	}
}

var crlfSplit = regexp.MustCompile(`\r?\n`)

func (a *Application) doRenderThrowable(ex Exception, output Output) {
	message := strings.Trim(ex.Error(), " \t\n\r\x00\x0B")
	verbose := output.Verbosity() >= VerbosityVerbose
	title := ""
	length := 0
	if message == "" || verbose {
		code := ""
		if ex.Code() != 0 {
			code = fmt.Sprintf(" (%d)", ex.Code())
		}
		title = fmt.Sprintf("  [%s%s]  ", ex.PHPClass(), code)
		length = Width(title)
	}
	width := TerminalWidth()
	if width != 0 {
		width--
	} else {
		width = int(^uint(0) >> 1)
	}
	type line struct {
		text   string
		length int
	}
	var lines []line
	if message != "" {
		for _, l := range crlfSplit.Split(message, -1) {
			for _, part := range splitStringByWidth(l, width-4) {
				ll := Width(part) + 4
				lines = append(lines, line{part, ll})
				length = max(length, ll)
			}
		}
	}
	var messages []string
	if !ex.IsConsoleException() || verbose {
		file, ln := ex.PHPLocation()
		if file == "" {
			file = "n/a"
		}
		lineStr := "n/a"
		if ln != 0 {
			lineStr = strconv.Itoa(ln)
		}
		messages = append(messages, fmt.Sprintf("<comment>%s</comment>", Escape(fmt.Sprintf("In %s line %s:", filepath.Base(file), lineStr))))
	}
	emptyLine := fmt.Sprintf("<error>%s</error>", strings.Repeat(" ", length))
	messages = append(messages, emptyLine)
	if message == "" || verbose {
		messages = append(messages, fmt.Sprintf("<error>%s%s</error>", title, strings.Repeat(" ", max(0, length-Width(title)))))
	}
	for _, l := range lines {
		messages = append(messages, fmt.Sprintf("<error>  %s  %s</error>", Escape(l.text), strings.Repeat(" ", length-l.length)))
	}
	messages = append(messages, emptyLine, "")
	for _, m := range messages {
		output.Writeln(m, VerbosityQuiet)
	}
}

// splitStringByWidth ports Application::splitStringByWidth().
func splitStringByWidth(s string, width int) []string {
	var lines []string
	cur := ""
	for _, r := range s {
		c := string(r)
		if Width(cur+c) <= width {
			cur += c
			continue
		}
		lines = append(lines, strPad(cur, width, " ", PadRight))
		cur = c
	}
	if len(lines) > 0 {
		lines = append(lines, strPad(cur, width, " ", PadRight))
	} else {
		lines = append(lines, cur)
	}
	return lines
}

// Run runs a command with the given IO, like Command::run(): the input is bound to the command's definition first.
func (c *Command) Run(io *IO) (int, error) { return c.run(io) }

// With returns an IO for the same application with another input and output, like running a command with
// `$command->run(new ArrayInput([]), $output)`.
func (io *IO) With(in *Input, out Output) *IO {
	return &IO{Input: in, Output: out, App: io.App, stdin: io.stdin}
}

// RenderError ports Application::renderThrowable() for any error.
func (a *Application) RenderError(err error, out Output) { a.RenderThrowable(asException(err), out) }
