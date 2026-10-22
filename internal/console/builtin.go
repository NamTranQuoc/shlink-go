package console

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed resources/completion.*
var completionScripts embed.FS

// completionAPIVersion is CompleteCommand::COMPLETION_API_VERSION.
const completionAPIVersion = "1"

var supportedShells = []string{"bash", "fish", "zsh"}

var descriptorFormats = []string{"txt", "xml", "json", "md", "rst"}

// helpCommand ports HelpCommand.
func (a *Application) helpCommand() *Command {
	c := &Command{
		Name:        "help",
		Description: "Display help for a command",
		Help: "The <info>%command.name%</info> command displays help for a given command:\n\n" +
			"  <info>%command.full_name% list</info>\n\n" +
			"You can also output the help in other formats by using the <info>--format</info> option:\n\n" +
			"  <info>%command.full_name% --format=xml list</info>\n\n" +
			"To display the list of available commands, please use the <info>list</info> command.",
		IgnoreValidationErrors: true,
		Definition: NewDefinition([]*Argument{
			withArgSuggestions(NewArgument("command_name", ArgOptional, "The command name", "help"), func() []string {
				d, _ := a.newApplicationDescription("", false)
				return d.names
			}),
		}, []*Option{
			withOptSuggestions(NewOption("format", "", OptValueRequired, "The output format (txt, xml, json, or md)", "txt"), func() []string { return descriptorFormats }),
			NewOption("raw", "", OptValueNone, "To output raw command help", nil),
		}),
	}
	c.Execute = func(io *IO) (int, error) {
		target := c.helpTarget
		c.helpTarget = nil
		if target == nil {
			name := ""
			if v := io.Input.ArgumentString("command_name"); v != nil {
				name = *v
			}
			found, err := a.Find(name)
			if err != nil {
				return 0, err
			}
			target = found
		}
		if err := a.describe(io.Output, target, describeOptions{Format: optionOr(io.Input, "format", "txt"), RawText: io.Input.OptionBool("raw")}); err != nil {
			return 0, err
		}
		return 0, nil
	}
	return c
}

func optionOr(in *Input, name, def string) string {
	if v := in.OptionString(name); v != nil {
		return *v
	}
	return def
}

func withArgSuggestions(a *Argument, f func() []string) *Argument { a.Suggest = f; return a }
func withOptSuggestions(o *Option, f func() []string) *Option     { o.Suggest = f; return o }

// listCommand ports ListCommand.
func (a *Application) listCommand() *Command {
	c := &Command{
		Name:        "list",
		Description: "List commands",
		Help: "The <info>%command.name%</info> command lists all commands:\n\n" +
			"  <info>%command.full_name%</info>\n\n" +
			"You can also display the commands for a specific namespace:\n\n" +
			"  <info>%command.full_name% test</info>\n\n" +
			"You can also output the information in other formats by using the <info>--format</info> option:\n\n" +
			"  <info>%command.full_name% --format=xml</info>\n\n" +
			"It's also possible to get raw list of commands (useful for embedding command runner):\n\n" +
			"  <info>%command.full_name% --raw</info>",
		Definition: NewDefinition([]*Argument{
			withArgSuggestions(NewArgument("namespace", ArgOptional, "The namespace name", nil), func() []string {
				d, _ := a.newApplicationDescription("", false)
				var ids []string
				for _, ns := range d.namespaces {
					ids = append(ids, ns.ID)
				}
				return ids
			}),
		}, []*Option{
			NewOption("raw", "", OptValueNone, "To output raw command list", nil),
			withOptSuggestions(NewOption("format", "", OptValueRequired, "The output format (txt, xml, json, or md)", "txt"), func() []string { return descriptorFormats }),
			NewOption("short", "", OptValueNone, "To skip describing commands' arguments", nil),
		}),
	}
	c.Execute = func(io *IO) (int, error) {
		namespace := ""
		if v := io.Input.ArgumentString("namespace"); v != nil {
			namespace = *v
		}
		err := a.describe(io.Output, a, describeOptions{
			Format:    optionOr(io.Input, "format", "txt"),
			RawText:   io.Input.OptionBool("raw"),
			Namespace: namespace,
			Short:     io.Input.OptionBool("short"),
		})
		return 0, err
	}
	return c
}

func guessShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	return phpBasename(shell)
}

// phpBasename ports basename(): trailing slashes are ignored and "/" gives "".
func phpBasename(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	return p[strings.LastIndex(p, "/")+1:]
}

// dumpCompletionCommand ports DumpCompletionCommand.
func (a *Application) dumpCompletionCommand() *Command {
	fullCommand := phpSelf()
	commandName := phpBasename(fullCommand)
	if real, err := filepath.EvalSymlinks(fullCommand); err == nil {
		if abs, err := filepath.Abs(real); err == nil {
			fullCommand = abs
		}
	}
	shell := guessShell()
	var rcFile, completionFile string
	switch shell {
	case "fish":
		rcFile, completionFile = "~/.config/fish/config.fish", "/etc/fish/completions/"+commandName+".fish"
	case "zsh":
		rcFile, completionFile = "~/.zshrc", "$fpath[1]/_"+commandName
	default:
		rcFile, completionFile = "~/.bashrc", "/etc/bash_completion.d/"+commandName
	}
	help := fmt.Sprintf(`The <info>%%command.name%%</> command dumps the shell completion script required
to use shell autocompletion (currently, %[1]s completion are supported).

<comment>Static installation
-------------------</>

Dump the script to a global completion file and restart your shell:

    <info>%%command.full_name%% %[2]s | sudo tee %[3]s</>

Or dump the script to a local file and source it:

    <info>%%command.full_name%% %[2]s > completion.sh</>

    <comment># source the file whenever you use the project</>
    <info>source completion.sh</>

    <comment># or add this line at the end of your "%[4]s" file:</>
    <info>source /path/to/completion.sh</>

<comment>Dynamic installation
--------------------</>

Add this to the end of your shell configuration file (e.g. <info>"%[4]s"</>):

    <info>eval "$(%[5]s completion %[2]s)"</>`, strings.Join(supportedShells, ", "), shell, completionFile, rcFile, fullCommand)
	c := &Command{
		Name:        "completion",
		Description: "Dump the shell completion script",
		Help:        help,
		Definition: NewDefinition([]*Argument{
			withArgSuggestions(NewArgument("shell", ArgOptional, `The shell type (e.g. "bash"), the value of the "$SHELL" env var will be used if this is not given`, nil), func() []string { return supportedShells }),
		}, []*Option{
			NewOption("debug", "", OptValueNone, "Tail the completion debug log", nil),
		}),
	}
	c.Execute = func(io *IO) (int, error) {
		name := phpBasename(os.Args[0])
		if io.Input.OptionBool("debug") {
			debugFile := filepath.Join(os.TempDir(), "sf_"+name+".log")
			if f, err := os.OpenFile(debugFile, os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				f.Close()
			}
			cmd := exec.Command("tail", "-f", debugFile)
			cmd.Stdout = writerFunc(func(p []byte) { io.Output.Write(string(p), false, OutputNormal) })
			_ = cmd.Run()
			return 0, nil
		}
		shell := guessShell()
		if v := io.Input.ArgumentString("shell"); v != nil {
			shell = *v
		}
		script, err := completionScripts.ReadFile("resources/completion." + shell)
		if err != nil || strings.Contains(shell, "/") {
			out := ErrorOutput(io.Output)
			if shell != "" {
				out.Writeln(fmt.Sprintf(`<error>Detected shell "%s", which is not supported by Symfony shell completion (supported shells: "%s").</>`, shell, strings.Join(supportedShells, `", "`)), OutputNormal)
			} else {
				out.Writeln(fmt.Sprintf(`<error>Shell not detected, Symfony shell completion only supports "%s").</>`, strings.Join(supportedShells, `", "`)), OutputNormal)
			}
			return 2, nil
		}
		io.Output.Write(strings.NewReplacer("{{ COMMAND_NAME }}", name, "{{ VERSION }}", completionAPIVersion).Replace(string(script)), false, OutputNormal)
		return 0, nil
	}
	return c
}

type writerFunc func(p []byte)

func (f writerFunc) Write(p []byte) (int, error) { f(p); return len(p), nil }

// Completion types, like CompletionInput's constants.
const (
	completionArgumentValue = "argument_value"
	completionOptionValue   = "option_value"
	completionOptionName    = "option_name"
	completionNone          = "none"
)

// completionInput ports CompletionInput.
type completionInput struct {
	*Input
	tokens       []string
	currentIndex int
	typ          string
	name         string
	value        string
}

func newCompletionInput(tokens []string, current int) *completionInput {
	// Like the ArgvInput constructor, parsing skips the first token (the program name)
	var parsed []string
	if len(tokens) > 0 {
		parsed = tokens[1:]
	}
	in := NewArgvInput(parsed)
	in.lenient = true
	return &completionInput{Input: in, tokens: tokens, currentIndex: current}
}

func (ci *completionInput) cursorFree() bool { return ci.currentIndex >= len(ci.tokens) }

func (ci *completionInput) relevantToken() string {
	if ci.cursorFree() {
		return ci.tokens[ci.currentIndex-1]
	}
	return ci.tokens[ci.currentIndex]
}

func (ci *completionInput) optionFromToken(token string) *Option {
	name := strings.TrimLeft(token, "-")
	if name == "" || name == "0" {
		return nil
	}
	if len(token) > 1 && token[1] == '-' {
		return ci.definition.Option(name)
	}
	if ci.definition.HasShortcut(name[:1]) {
		return ci.definition.Option(ci.definition.ShortcutToName(name[:1]))
	}
	return nil
}

func (ci *completionInput) bind(d *Definition) {
	_ = ci.Input.Bind(d)
	relevant := ci.relevantToken()
	if strings.HasPrefix(relevant, "-") {
		optionToken, optionValue, _ := strings.Cut(relevant, "=")
		option := ci.optionFromToken(optionToken)
		if option == nil && !ci.cursorFree() {
			ci.typ, ci.value = completionOptionName, relevant
			return
		}
		if option != nil && option.AcceptValue() {
			ci.typ, ci.name = completionOptionValue, option.Name
			switch {
			case optionValue != "" && optionValue != "0":
				ci.value = optionValue
			case !strings.HasPrefix(optionToken, "--") && len(optionToken) > 2:
				ci.value = optionToken[2:]
			default:
				ci.value = ""
			}
			return
		}
	}
	previous := ci.tokens[ci.currentIndex-1]
	if strings.HasPrefix(previous, "-") && strings.Trim(previous, "-") != "" {
		if option := ci.optionFromToken(previous); option != nil && option.AcceptValue() {
			ci.typ, ci.name, ci.value = completionOptionValue, option.Name, relevant
			return
		}
	}
	ci.typ = completionArgumentValue
	lastName := ""
	for _, arg := range ci.definition.Arguments() {
		lastName = arg.Name
		v, ok := ci.arguments[arg.Name]
		if !ok || v == nil {
			break
		}
		ci.name = arg.Name
		switch t := v.(type) {
		case []any:
			ci.value = ""
			if len(t) > 0 {
				ci.value = fmt.Sprint(t[len(t)-1])
			}
		default:
			ci.value = fmt.Sprint(t)
		}
	}
	if ci.currentIndex >= len(ci.tokens) {
		v, ok := ci.arguments[lastName]
		if !ok || v == nil || ci.definition.Argument(lastName).IsArray() {
			ci.name = lastName
		} else {
			ci.typ, ci.name = completionNone, ""
		}
		ci.value = ""
	}
}

func (ci *completionInput) String() string {
	var b strings.Builder
	i := -1
	for i = range ci.tokens {
		b.WriteString(ci.tokens[i])
		if ci.currentIndex == i {
			b.WriteString("|")
		}
		b.WriteString(" ")
	}
	if ci.currentIndex > i {
		b.WriteString("|")
	}
	return strings.TrimRight(b.String(), " \t\n\r\x00\x0B")
}

type suggestion struct{ value, description string }

type suggestions struct {
	values  []suggestion
	options []*Option
}

func (s *suggestions) suggestValues(values []string) {
	for _, v := range values {
		s.values = append(s.values, suggestion{value: v})
	}
}

func completeDefinition(d *Definition, ci *completionInput, s *suggestions) {
	switch ci.typ {
	case completionOptionValue:
		if o := d.Option(ci.name); o != nil && o.Suggest != nil {
			s.suggestValues(o.Suggest())
		}
	case completionArgumentValue:
		if a := d.Argument(ci.name); a != nil && a.Suggest != nil {
			s.suggestValues(a.Suggest())
		}
	}
}

// completeCommand ports CompleteCommand.
func (a *Application) completeCommand() *Command {
	c := &Command{
		Name:        "_complete",
		Hidden:      true,
		Description: "Internal command to provide shell completion suggestions",
		Definition: NewDefinition(nil, []*Option{
			NewOption("shell", "s", OptValueRequired, `The shell type ("bash", "fish", "zsh")`, nil),
			NewOption("input", "i", OptValueRequired|OptValueIsArray, "An array of input tokens (e.g. COMP_WORDS or argv)", nil),
			NewOption("current", "c", OptValueRequired, `The index of the "input" array that the cursor is in (e.g. COMP_CWORD)`, nil),
			NewOption("api-version", "a", OptValueRequired, "The API version of the completion script", nil),
			NewOption("symfony", "S", OptValueRequired, "deprecated", nil),
		}),
	}
	c.Execute = func(io *IO) (int, error) {
		debug := phpFilterBool(os.Getenv("SYMFONY_COMPLETION_DEBUG"))
		log := func(messages ...string) {
			if !debug {
				return
			}
			f, err := os.OpenFile(filepath.Join(os.TempDir(), "sf_"+phpBasename(os.Args[0])+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return
			}
			defer f.Close()
			f.WriteString(strings.Join(messages, "\n") + "\n")
		}
		code, err := a.runComplete(io, log)
		if err != nil {
			log("<error>Error!</error>", err.Error())
			if io.Output.Verbosity() >= VerbosityDebug {
				return 0, err
			}
			return 2, nil
		}
		return code, nil
	}
	return c
}

func phpFilterBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

var digitsOnly = regexp.MustCompile(`^\d+$`)

func (a *Application) runComplete(io *IO, log func(...string)) (int, error) {
	version := optionOr(io.Input, "api-version", "")
	if v := io.Input.OptionString("symfony"); v != nil && phpTruthy(*v) {
		version = "1"
	}
	if phpTruthy(version) && versionLess(version, completionAPIVersion) {
		message := fmt.Sprintf(`Completion script version is not supported ("%s" given, ">=%s" required).`, version, completionAPIVersion)
		log(message)
		io.Output.Writeln(message+` Install the Symfony completion script again by using the "completion" command.`, OutputNormal)
		return 126, nil
	}
	shell := optionOr(io.Input, "shell", "")
	if !phpTruthy(shell) {
		return 0, &PHPError{Class: "RuntimeException", Message: `The "--shell" option must be set.`}
	}
	if !contains(supportedShells, shell) {
		return 0, &PHPError{Class: "RuntimeException", Message: fmt.Sprintf(`Shell completion is not supported for your shell: "%s" (supported: "%s").`, shell, strings.Join(supportedShells, `", "`))}
	}
	current := optionOr(io.Input, "current", "")
	if !phpTruthy(current) || !digitsOnly.MatchString(current) {
		return 0, &PHPError{Class: "RuntimeException", Message: `The "--current" option must be set and it must be an integer.`}
	}
	index, _ := strconv.Atoi(current)
	tokens := io.Input.OptionStrings("input")
	if index > len(tokens) {
		return 0, &PHPError{Class: "LogicException", Message: "Current index is invalid, it must be the number of input tokens or one more."}
	}
	ci := newCompletionInput(tokens, index)
	ci.bind(a.definition())
	s := &suggestions{}
	log("", "<comment>"+time.Now().Format("2006-01-02 15:04:05")+"</>", `<info>Input:</> <comment>("|" indicates the cursor position)</>`,
		"  "+ci.String(), "<info>Command:</>", "  "+strings.Join(os.Args, " "), "<info>Messages:</>")

	var command *Command
	if first := ci.FirstArgument(); first != "" {
		if found, err := a.Find(first); err == nil {
			command = found
		}
	}
	if command != nil {
		command.MergeApplicationDefinition(true)
		ci.bind(command.FullDefinition())
	}
	switch {
	case command == nil:
		log("  No command found, completing using the Application class.")
		a.complete(ci, s)
	case ci.typ == completionArgumentValue && ci.name == "command":
		log("  Command found, completing command name.")
		names := append([]string{command.Name}, command.Aliases...)
		for _, n := range names {
			if strings.HasPrefix(n, ci.value) {
				names = []string{n}
				break
			}
		}
		s.suggestValues(names)
	case ci.typ == completionOptionName:
		s.options = append(s.options, command.FullDefinition().Options()...)
	default:
		completeDefinition(command.FullDefinition(), ci, s)
	}
	log("<info>Suggestions:</>")
	writeSuggestions(shell, s, io.Output)
	return 0, nil
}

// versionLess approximates version_compare($a, $b, '<') for plain numeric versions.
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return len(pa) < len(pb)
}

// complete ports Application::complete().
func (a *Application) complete(ci *completionInput, s *suggestions) {
	if ci.typ == completionArgumentValue && ci.name == "command" {
		for _, name := range a.all(nil) {
			c := a.commands[name]
			if c.Hidden || c.Name != name {
				continue
			}
			s.values = append(s.values, suggestion{c.Name, c.Description})
			for _, alias := range c.Aliases {
				s.values = append(s.values, suggestion{alias, c.Description})
			}
		}
		return
	}
	if ci.typ == completionOptionName {
		s.options = append(s.options, a.definition().Options()...)
	}
	if ci.typ == completionOptionValue {
		completeDefinition(a.definition(), ci, s)
	}
}

func writeSuggestions(shell string, s *suggestions, out Output) {
	var values []string
	withDescription := func(v, d string) string {
		if shell == "bash" || d == "" {
			return v
		}
		return v + "\t" + d
	}
	for _, v := range s.values {
		values = append(values, withDescription(v.value, v.description))
	}
	for _, o := range s.options {
		values = append(values, withDescription("--"+o.Name, o.Description))
		if o.IsNegatable() {
			values = append(values, withDescription("--no-"+o.Name, o.Description))
		}
	}
	switch shell {
	case "bash":
		out.Writeln(strings.Join(values, "\n"), OutputNormal)
	case "zsh":
		out.Write(strings.Join(values, "\n")+"\n", false, OutputNormal)
	default:
		out.Write(strings.Join(values, "\n"), false, OutputNormal)
	}
}
