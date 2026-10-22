package console

import (
	"fmt"
	"regexp"
	"strings"
)

// Argument modes, like InputArgument's constants.
const (
	ArgRequired = 1
	ArgOptional = 2
	ArgIsArray  = 4
)

// Option modes, like InputOption's constants.
const (
	OptValueNone      = 1
	OptValueRequired  = 2
	OptValueOptional  = 4
	OptValueIsArray   = 8
	OptValueNegatable = 16
)

// Argument ports InputArgument. Default is nil, a string or a []string.
type Argument struct {
	Name        string
	Mode        int
	Description string
	Default     any
	// Suggest returns the values suggested by shell completion.
	Suggest func() []string
}

// NewArgument ports the InputArgument constructor's mode normalization and default handling.
func NewArgument(name string, mode int, description string, def any) *Argument {
	if mode&ArgRequired == 0 {
		mode |= ArgOptional
	}
	a := &Argument{Name: name, Mode: mode, Description: description}
	if a.IsArray() && def == nil {
		def = []string{}
	}
	a.Default = def
	return a
}

func (a *Argument) IsRequired() bool { return a.Mode&ArgRequired != 0 }
func (a *Argument) IsArray() bool    { return a.Mode&ArgIsArray != 0 }

// Option ports InputOption. Default is nil, false/true, a string or a []string.
type Option struct {
	Name        string
	Shortcut    string
	Mode        int
	Description string
	Default     any
	// Suggest returns the values suggested by shell completion.
	Suggest func() []string
}

var shortcutSplit = regexp.MustCompile(`(\|)-?`)

// NewOption ports the InputOption constructor's normalization of name, shortcut, mode and default.
func NewOption(name, shortcut string, mode int, description string, def any) *Option {
	name = strings.TrimPrefix(name, "--")
	if shortcut != "" {
		parts := shortcutSplit.Split(strings.TrimLeft(shortcut, "-"), -1)
		var kept []string
		for _, p := range parts {
			if p != "" {
				kept = append(kept, p)
			}
		}
		shortcut = strings.Join(kept, "|")
	}
	if mode&OptValueRequired == 0 && mode&OptValueOptional == 0 {
		mode |= OptValueNone
	}
	o := &Option{Name: name, Shortcut: shortcut, Mode: mode, Description: description}
	if o.IsArray() && def == nil {
		def = []string{}
	}
	if o.AcceptValue() || o.IsNegatable() {
		o.Default = def
	} else {
		o.Default = false
	}
	return o
}

func (o *Option) AcceptValue() bool     { return o.IsValueRequired() || o.IsValueOptional() }
func (o *Option) IsValueRequired() bool { return o.Mode&OptValueRequired != 0 }
func (o *Option) IsValueOptional() bool { return o.Mode&OptValueOptional != 0 }
func (o *Option) IsArray() bool         { return o.Mode&OptValueIsArray != 0 }
func (o *Option) IsNegatable() bool     { return o.Mode&OptValueNegatable != 0 }

// Definition ports InputDefinition, keeping insertion order.
type Definition struct {
	arguments    []*Argument
	options      []*Option
	shortcuts    map[string]string
	negations    map[string]string
	lastArray    *Argument
	lastOptional *Argument
}

// NewDefinition creates a definition from arguments and options.
func NewDefinition(args []*Argument, opts []*Option) *Definition {
	d := &Definition{}
	d.SetArguments(args)
	d.SetOptions(opts)
	return d
}

func (d *Definition) SetArguments(args []*Argument) {
	d.arguments, d.lastArray, d.lastOptional = nil, nil, nil
	for _, a := range args {
		d.AddArgument(a)
	}
}

func (d *Definition) AddArgument(a *Argument) {
	if d.HasArgument(a.Name) {
		panic(fmt.Sprintf(`An argument with name "%s" already exists.`, a.Name))
	}
	if d.lastArray != nil {
		panic(fmt.Sprintf(`Cannot add a required argument "%s" after an array argument "%s".`, a.Name, d.lastArray.Name))
	}
	if a.IsRequired() && d.lastOptional != nil {
		panic(fmt.Sprintf(`Cannot add a required argument "%s" after an optional one "%s".`, a.Name, d.lastOptional.Name))
	}
	if a.IsArray() {
		d.lastArray = a
	}
	if !a.IsRequired() {
		d.lastOptional = a
	}
	d.arguments = append(d.arguments, a)
}

func (d *Definition) Arguments() []*Argument { return d.arguments }
func (d *Definition) Options() []*Option     { return d.options }

func (d *Definition) HasArgument(name string) bool { return d.Argument(name) != nil }

// Argument returns an argument by name, or nil.
func (d *Definition) Argument(name string) *Argument {
	for _, a := range d.arguments {
		if a.Name == name {
			return a
		}
	}
	return nil
}

// ArgumentAt returns the argument at a position, or nil.
func (d *Definition) ArgumentAt(i int) *Argument {
	if i < 0 || i >= len(d.arguments) {
		return nil
	}
	return d.arguments[i]
}

func (d *Definition) SetOptions(opts []*Option) {
	d.options, d.shortcuts, d.negations = nil, map[string]string{}, map[string]string{}
	d.AddOptions(opts)
}

func (d *Definition) AddOptions(opts []*Option) {
	for _, o := range opts {
		d.AddOption(o)
	}
}

func (d *Definition) AddOption(o *Option) {
	if d.shortcuts == nil {
		d.shortcuts, d.negations = map[string]string{}, map[string]string{}
	}
	replaced := false
	for i, existing := range d.options {
		if existing.Name == o.Name {
			d.options[i] = o
			replaced = true
		}
	}
	if !replaced {
		d.options = append(d.options, o)
	}
	if o.Shortcut != "" {
		for _, s := range strings.Split(o.Shortcut, "|") {
			d.shortcuts[s] = o.Name
		}
	}
	if o.IsNegatable() {
		d.negations["no-"+o.Name] = o.Name
	}
}

// Option returns an option by name, or nil.
func (d *Definition) Option(name string) *Option {
	for _, o := range d.options {
		if o.Name == name {
			return o
		}
	}
	return nil
}

func (d *Definition) HasOption(name string) bool     { return d.Option(name) != nil }
func (d *Definition) HasShortcut(name string) bool   { _, ok := d.shortcuts[name]; return ok }
func (d *Definition) HasNegation(name string) bool   { _, ok := d.negations[name]; return ok }
func (d *Definition) ShortcutToName(s string) string { return d.shortcuts[s] }

// Synopsis ports InputDefinition::getSynopsis().
func (d *Definition) Synopsis(short bool) string {
	var elements []string
	if short && len(d.options) > 0 {
		elements = append(elements, "[options]")
	} else if !short {
		for _, o := range d.options {
			value := ""
			if o.AcceptValue() {
				l, r := "", ""
				if o.IsValueOptional() {
					l, r = "[", "]"
				}
				value = " " + l + strings.ToUpper(o.Name) + r
			}
			shortcut := ""
			if o.Shortcut != "" {
				shortcut = "-" + o.Shortcut + "|"
			}
			negation := ""
			if o.IsNegatable() {
				negation = "|--no-" + o.Name
			}
			elements = append(elements, fmt.Sprintf("[%s--%s%s%s]", shortcut, o.Name, value, negation))
		}
	}
	if len(elements) > 0 && len(d.arguments) > 0 {
		elements = append(elements, "[--]")
	}
	tail := ""
	for _, a := range d.arguments {
		el := "<" + a.Name + ">"
		if a.IsArray() {
			el += "..."
		}
		if !a.IsRequired() {
			el = "[" + el
			tail += "]"
		}
		elements = append(elements, el)
	}
	return strings.Join(elements, " ") + tail
}

// Input ports Input/ArgvInput: raw tokens plus the values bound to a definition.
type Input struct {
	tokens      []string
	parsed      []string
	definition  *Definition
	arguments   map[string]any
	argOrder    []string
	options     map[string]any
	interactive bool
	// array marks an ArrayInput (used by Application to describe the default command)
	array map[string]any
	// lenient ignores parse errors, like CompletionInput::parseToken()
	lenient bool
}

// NewArgvInput creates an input from command line arguments (without the program name).
func NewArgvInput(tokens []string) *Input {
	return &Input{tokens: tokens, definition: NewDefinition(nil, nil), arguments: map[string]any{}, options: map[string]any{}, interactive: true}
}

// NewArrayInput ports ArrayInput for the parameters Application builds itself.
func NewArrayInput(params map[string]any) *Input {
	return &Input{array: params, definition: NewDefinition(nil, nil), arguments: map[string]any{}, options: map[string]any{}, interactive: true}
}

func (in *Input) Definition() *Definition { return in.definition }
func (in *Input) IsInteractive() bool     { return in.interactive }
func (in *Input) SetInteractive(i bool)   { in.interactive = i }

// Bind ports Input::bind(): values are reset and parsed again against the definition.
func (in *Input) Bind(d *Definition) error {
	in.arguments, in.argOrder, in.options = map[string]any{}, nil, map[string]any{}
	in.definition = d
	if in.array != nil {
		return in.parseArray()
	}
	in.parsed = append([]string(nil), in.tokens...)
	parseOptions := true
	for len(in.parsed) > 0 {
		token := in.parsed[0]
		in.parsed = in.parsed[1:]
		next, err := in.parseToken(token, parseOptions)
		if err != nil {
			if in.lenient {
				// CompletionInput suppresses parse errors: completed input is almost never valid
				continue
			}
			return err
		}
		parseOptions = next
	}
	return nil
}

func (in *Input) parseArray() error {
	for key, value := range in.array {
		switch {
		case key == "--":
			return nil
		case strings.HasPrefix(key, "--"):
			name := key[2:]
			if !in.definition.HasOption(name) {
				return newRuntimeError(fmt.Sprintf(`The "--%s" option does not exist.`, name), "ArrayInput.php", 156)
			}
			if opt := in.definition.Option(name); value == nil && !opt.IsValueOptional() {
				if opt.IsValueRequired() {
					return newRuntimeError(fmt.Sprintf(`The "--%s" option requires a value.`, name), "ArrayInput.php", 166)
				}
				value = true
			}
			in.options[name] = value
		case strings.HasPrefix(key, "-"):
			name := in.definition.ShortcutToName(key[1:])
			in.options[name] = value
		default:
			if !in.definition.HasArgument(key) {
				return &InvalidArgumentError{Message: fmt.Sprintf(`The "%s" argument does not exist.`, key), File: "ArrayInput.php", Line: 186}
			}
			in.setArg(key, value)
		}
	}
	return nil
}

func (in *Input) setArg(name string, value any) {
	if _, ok := in.arguments[name]; !ok {
		in.argOrder = append(in.argOrder, name)
	}
	in.arguments[name] = value
}

func (in *Input) parseToken(token string, parseOptions bool) (bool, error) {
	switch {
	case parseOptions && token == "":
		return parseOptions, in.parseArgument(token)
	case parseOptions && token == "--":
		return false, nil
	case parseOptions && strings.HasPrefix(token, "--"):
		return parseOptions, in.parseLongOption(token)
	case parseOptions && token[0] == '-' && token != "-":
		return parseOptions, in.parseShortOption(token)
	}
	return parseOptions, in.parseArgument(token)
}

func (in *Input) parseShortOption(token string) error {
	name := token[1:]
	if len(name) > 1 {
		if in.definition.HasShortcut(name[:1]) && in.definition.Option(in.definition.ShortcutToName(name[:1])).AcceptValue() {
			return in.addShortOption(name[:1], ptr(name[1:]))
		}
		return in.parseShortOptionSet(name)
	}
	return in.addShortOption(name, nil)
}

func ptr(s string) *string { return &s }

func (in *Input) parseShortOptionSet(name string) error {
	for i := 0; i < len(name); i++ {
		c := name[i : i+1]
		if !in.definition.HasShortcut(c) {
			return newRuntimeError(fmt.Sprintf(`The "-%s" option does not exist.`, firstChar(name[i:])), "ArgvInput.php", 128)
		}
		o := in.definition.Option(in.definition.ShortcutToName(c))
		if o.AcceptValue() {
			if i == len(name)-1 {
				return in.addLongOption(o.Name, nil)
			}
			return in.addLongOption(o.Name, ptr(name[i+1:]))
		}
		if err := in.addLongOption(o.Name, nil); err != nil {
			return err
		}
	}
	return nil
}

// firstChar returns the first UTF-8 character, like mb_substr($name, $i, 1).
func firstChar(s string) string {
	for _, r := range s {
		return string(r)
	}
	return s
}

func (in *Input) parseLongOption(token string) error {
	name := token[2:]
	if pos := strings.IndexByte(name, '='); pos >= 0 {
		value := name[pos+1:]
		if value == "" {
			in.parsed = append([]string{value}, in.parsed...)
		}
		return in.addLongOption(name[:pos], &value)
	}
	return in.addLongOption(name, nil)
}

func (in *Input) parseArgument(token string) error {
	c := len(in.arguments)
	if a := in.definition.ArgumentAt(c); a != nil {
		if a.IsArray() {
			in.setArg(a.Name, []string{token})
		} else {
			in.setArg(a.Name, token)
		}
		return nil
	}
	if a := in.definition.ArgumentAt(c - 1); a != nil && a.IsArray() {
		in.arguments[a.Name] = append(in.arguments[a.Name].([]string), token)
		return nil
	}
	all := in.definition.Arguments()
	var commandName any
	if len(all) > 0 && all[0].Name == "command" {
		commandName = in.arguments["command"]
		all = all[1:]
	}
	var names []string
	for _, a := range all {
		names = append(names, a.Name)
	}
	cmd, _ := commandName.(string)
	var message string
	switch {
	case len(names) > 0 && cmd != "":
		message = fmt.Sprintf(`Too many arguments to "%s" command, expected arguments "%s".`, cmd, strings.Join(names, `" "`))
	case len(names) > 0:
		message = fmt.Sprintf(`Too many arguments, expected arguments "%s".`, strings.Join(names, `" "`))
	case cmd != "":
		message = fmt.Sprintf(`No arguments expected for "%s" command, got "%s".`, cmd, token)
	default:
		message = fmt.Sprintf(`No arguments expected, got "%s".`, token)
	}
	return newRuntimeError(message, "ArgvInput.php", 199)
}

func (in *Input) addShortOption(shortcut string, value *string) error {
	if !in.definition.HasShortcut(shortcut) {
		return newRuntimeError(fmt.Sprintf(`The "-%s" option does not exist.`, shortcut), "ArgvInput.php", 211)
	}
	return in.addLongOption(in.definition.ShortcutToName(shortcut), value)
}

func (in *Input) addLongOption(name string, value *string) error {
	o := in.definition.Option(name)
	if o == nil {
		target, ok := in.definition.negations[name]
		if !ok {
			return newRuntimeError(fmt.Sprintf(`The "--%s" option does not exist.`, name), "ArgvInput.php", 226)
		}
		if value != nil {
			return newRuntimeError(fmt.Sprintf(`The "--%s" option does not accept a value.`, name), "ArgvInput.php", 231)
		}
		in.options[target] = false
		return nil
	}
	if value != nil && !o.AcceptValue() {
		return newRuntimeError(fmt.Sprintf(`The "--%s" option does not accept a value.`, name), "ArgvInput.php", 241)
	}
	if (value == nil || *value == "") && o.AcceptValue() && len(in.parsed) > 0 {
		next := in.parsed[0]
		in.parsed = in.parsed[1:]
		if (next != "" && next[0] != '-') || next == "" {
			value = &next
		} else {
			in.parsed = append([]string{next}, in.parsed...)
		}
	}
	var v any
	if value != nil {
		v = *value
	}
	if value == nil {
		if o.IsValueRequired() {
			return newRuntimeError(fmt.Sprintf(`The "--%s" option requires a value.`, name), "ArgvInput.php", 257)
		}
		if !o.IsArray() && !o.IsValueOptional() {
			v = true
		}
	}
	if o.IsArray() {
		list, _ := in.options[name].([]any)
		in.options[name] = append(list, v)
	} else {
		in.options[name] = v
	}
	return nil
}

// FirstArgument ports ArgvInput::getFirstArgument().
func (in *Input) FirstArgument() string {
	if in.array != nil {
		for k, v := range in.array {
			if !strings.HasPrefix(k, "-") {
				s, _ := v.(string)
				return s
			}
		}
		return ""
	}
	isOption := false
	for i, token := range in.tokens {
		if token != "" && token[0] == '-' {
			if strings.Contains(token, "=") || i+1 >= len(in.tokens) {
				continue
			}
			var name string
			if len(token) > 1 && token[1] == '-' {
				name = token[2:]
			} else {
				name = token[len(token)-1:]
			}
			_, set := in.options[name]
			if !set && !in.definition.HasShortcut(name) {
				// noop
			} else {
				if !set {
					name = in.definition.ShortcutToName(name)
				}
				if v, ok := in.options[name].(string); ok && in.tokens[i+1] == v {
					isOption = true
				}
			}
			continue
		}
		if isOption {
			isOption = false
			continue
		}
		return token
	}
	return ""
}

// HasParameterOption ports ArgvInput::hasParameterOption().
func (in *Input) HasParameterOption(values []string, onlyParams bool) bool {
	if in.array != nil {
		for k := range in.array {
			for _, v := range values {
				if k == v {
					return true
				}
			}
		}
		return false
	}
	for _, token := range in.tokens {
		if onlyParams && token == "--" {
			return false
		}
		for _, v := range values {
			leading := v
			if strings.HasPrefix(v, "--") {
				leading = v + "="
			}
			if token == v || (leading != "" && strings.HasPrefix(token, leading)) {
				return true
			}
		}
	}
	return false
}

// ParameterOption ports ArgvInput::getParameterOption(); ok is false when the option is absent.
func (in *Input) ParameterOption(values []string, onlyParams bool) (string, bool) {
	tokens := in.tokens
	for len(tokens) > 0 {
		token := tokens[0]
		tokens = tokens[1:]
		if onlyParams && token == "--" {
			return "", false
		}
		for _, v := range values {
			if token == v {
				if len(tokens) == 0 {
					return "", true
				}
				return tokens[0], true
			}
			leading := v
			if strings.HasPrefix(v, "--") {
				leading = v + "="
			}
			if leading != "" && strings.HasPrefix(token, leading) {
				return token[len(leading):], true
			}
		}
	}
	return "", false
}

// Validate ports Input::validate().
func (in *Input) Validate() error {
	var missing []string
	for _, a := range in.definition.Arguments() {
		if _, ok := in.arguments[a.Name]; !ok && a.IsRequired() {
			missing = append(missing, a.Name)
		}
	}
	if len(missing) > 0 {
		return newRuntimeError(fmt.Sprintf(`Not enough arguments (missing: "%s").`, strings.Join(missing, ", ")), "Input.php", 69)
	}
	return nil
}

// Argument ports Input::getArgument().
func (in *Input) Argument(name string) any {
	if v, ok := in.arguments[name]; ok {
		return v
	}
	if a := in.definition.Argument(name); a != nil {
		return a.Default
	}
	return nil
}

// ArgumentString returns an argument as a string pointer (nil when null).
func (in *Input) ArgumentString(name string) *string {
	if s, ok := in.Argument(name).(string); ok {
		return &s
	}
	return nil
}

// SetArgument ports Input::setArgument().
func (in *Input) SetArgument(name string, value any) { in.setArg(name, value) }

// HasArgument ports Input::hasArgument().
func (in *Input) HasArgument(name string) bool { return in.definition.HasArgument(name) }

// Option ports Input::getOption().
func (in *Input) Option(name string) any {
	if target, ok := in.definition.negations[name]; ok {
		v := in.Option(target)
		if b, ok := v.(bool); ok {
			return !b
		}
		return v
	}
	if v, ok := in.options[name]; ok {
		return v
	}
	if o := in.definition.Option(name); o != nil {
		return o.Default
	}
	return nil
}

// OptionString returns a string-valued option (nil when not given or null).
func (in *Input) OptionString(name string) *string {
	if s, ok := in.Option(name).(string); ok {
		return &s
	}
	return nil
}

// OptionBool returns a flag option.
func (in *Input) OptionBool(name string) bool {
	b, _ := in.Option(name).(bool)
	return b
}

// OptionStrings returns an array option's values; null values become "".
func (in *Input) OptionStrings(name string) []string {
	var out []string
	switch v := in.Option(name).(type) {
	case []any:
		for _, item := range v {
			s, _ := item.(string)
			out = append(out, s)
		}
	case []string:
		out = append(out, v...)
	}
	return out
}

// HasRawOption tells if an option was given on the command line.
func (in *Input) HasRawOption(name string) bool { _, ok := in.options[name]; return ok }
