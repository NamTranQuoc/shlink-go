package console

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

const globalNamespace = "_global"

var lineBreakSpaces = regexp.MustCompile(`\s*[\r\n]\s*`)

// describeOptions are the DescriptorHelper::describe() options.
type describeOptions struct {
	Format    string
	RawText   bool
	Namespace string
	Short     bool
}

// describe ports DescriptorHelper::describe() for the txt, json, xml, md and rst formats.
func (a *Application) describe(out Output, object any, opts describeOptions) error {
	if opts.Format == "" {
		opts.Format = "txt"
	}
	switch opts.Format {
	case "txt":
		d := &textDescriptor{out: out, rawText: opts.RawText}
		switch o := object.(type) {
		case *Command:
			d.describeCommand(o)
		case *Application:
			return d.describeApplication(o, opts.Namespace)
		}
	case "json":
		d := &jsonDescriptor{out: out}
		switch o := object.(type) {
		case *Command:
			d.write(d.commandData(o, opts.Short))
		case *Application:
			return d.describeApplication(o, opts.Namespace, opts.Short)
		}
	case "xml":
		return a.describeXML(out, object, opts)
	case "md":
		// MarkdownDescriptor writes undecorated output
		decorated := out.IsDecorated()
		out.SetDecorated(false)
		defer out.SetDecorated(decorated)
		d := &markdownDescriptor{out: out}
		switch o := object.(type) {
		case *Command:
			d.describeCommand(o, opts.Short)
		case *Application:
			return d.describeApplication(o, opts.Namespace, opts.Short)
		}
	case "rst":
		// ReStructuredTextDescriptor writes undecorated output
		decorated := out.IsDecorated()
		out.SetDecorated(false)
		defer out.SetDecorated(decorated)
		d := &rstDescriptor{out: out}
		switch o := object.(type) {
		case *Command:
			d.describeCommand(o, opts.Short)
		case *Application:
			return d.describeApplication(o, opts.Namespace, opts.Short)
		}
	default:
		return &InvalidArgumentError{Message: fmt.Sprintf(`Unsupported format "%s".`, opts.Format), File: "DescriptorHelper.php", Line: 65}
	}
	return nil
}

// describeApplication renders the command list of a namespace, like ListCommand does when a namespace is run.
func (a *Application) describeApplication(out Output, namespace string, rawText bool) {
	_ = a.describe(out, a, describeOptions{Namespace: namespace, RawText: rawText})
}

// extractNamespace ports Application::extractNamespace() (limit < 0 means no limit).
func extractNamespace(name string, limit int) string {
	parts := strings.Split(name, ":")
	parts = parts[:len(parts)-1]
	if limit >= 0 && limit < len(parts) {
		parts = parts[:limit]
	}
	return strings.Join(parts, ":")
}

// all ports Application::all(): every name (commands and aliases), the loaded ones first in the order they were
// loaded, then the rest in the command loader's order, loading them.
func (a *Application) all(namespace *string) []string {
	matches := func(name string) bool {
		return namespace == nil || *namespace == extractNamespace(name, strings.Count(*namespace, ":")+1)
	}
	var names []string
	seen := map[string]bool{}
	for _, name := range append([]string(nil), a.loaded...) {
		seen[name] = true
		if matches(name) {
			names = append(names, name)
		}
	}
	for _, name := range a.order {
		if !seen[name] && matches(name) && a.has(name) {
			names = append(names, name)
		}
	}
	return names
}

type namespaceDescription struct {
	ID       string
	Commands []string
}

// applicationDescription ports ApplicationDescription.
type applicationDescription struct {
	namespaces []namespaceDescription
	commands   map[string]*Command
	names      []string
	aliases    map[string]*Command
}

func (a *Application) newApplicationDescription(namespace string, showHidden bool) (*applicationDescription, error) {
	var ns *string
	if namespace != "" {
		found, err := a.FindNamespace(namespace)
		if err != nil {
			return nil, err
		}
		ns = &found
	}
	d := &applicationDescription{commands: map[string]*Command{}, aliases: map[string]*Command{}}
	var global []string
	grouped := map[string][]string{}
	var keys []string
	for _, name := range a.all(ns) {
		key := extractNamespace(name, 1)
		if key == "" || key == globalNamespace {
			global = append(global, name)
			continue
		}
		if _, ok := grouped[key]; !ok {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], name)
	}
	add := func(id string, names []string) {
		sort.Strings(names)
		var listed []string
		for _, name := range names {
			c := a.commands[name]
			if !showHidden && c.Hidden {
				continue
			}
			if c.Name == name {
				d.commands[name] = c
				d.names = append(d.names, name)
			} else {
				d.aliases[name] = c
			}
			listed = append(listed, name)
		}
		d.namespaces = append(d.namespaces, namespaceDescription{ID: id, Commands: listed})
	}
	if len(global) > 0 {
		add(globalNamespace, global)
	}
	sort.Strings(keys)
	for _, k := range keys {
		add(k, grouped[k])
	}
	return d, nil
}

type textDescriptor struct {
	out     Output
	rawText bool
}

func (d *textDescriptor) writeText(content string) {
	if d.rawText {
		content = stripTags(content)
	}
	d.out.Write(content, false, OutputNormal)
}

func formatDefaultValue(def any) string {
	switch v := def.(type) {
	case string:
		def = Escape(v)
	case []string:
		escaped := make([]string, len(v))
		for i, s := range v {
			escaped[i] = Escape(s)
		}
		def = escaped
	case []any:
		escaped := make([]any, len(v))
		for i, s := range v {
			if str, ok := s.(string); ok {
				escaped[i] = Escape(str)
			} else {
				escaped[i] = s
			}
		}
		def = escaped
	}
	return strings.ReplaceAll(string(phpx.Encode(def, phpx.JSONUnescapedSlashes|phpx.JSONUnescapedUnicode)), `\\`, `\`)
}

func hasDefault(def any) bool {
	switch v := def.(type) {
	case nil:
		return false
	case []string:
		return len(v) > 0
	case []any:
		return len(v) > 0
	}
	return true
}

func (d *textDescriptor) describeArgument(a *Argument, totalWidth int) {
	def := ""
	if hasDefault(a.Default) {
		def = fmt.Sprintf("<comment> [default: %s]</comment>", formatDefaultValue(a.Default))
	}
	spacing := totalWidth - Width(a.Name)
	d.writeText(fmt.Sprintf("  <info>%s</info>  %s%s%s", a.Name, strings.Repeat(" ", spacing),
		lineBreakSpaces.ReplaceAllLiteralString(a.Description, "\n"+strings.Repeat(" ", totalWidth+4)), def))
}

func (d *textDescriptor) describeOption(o *Option, totalWidth int) {
	def := ""
	if o.AcceptValue() && hasDefault(o.Default) {
		def = fmt.Sprintf("<comment> [default: %s]</comment>", formatDefaultValue(o.Default))
	}
	value := ""
	if o.AcceptValue() {
		value = "=" + strings.ToUpper(o.Name)
		if o.IsValueOptional() {
			value = "[" + value + "]"
		}
	}
	synopsis := "    "
	if o.Shortcut != "" {
		synopsis = fmt.Sprintf("-%s, ", o.Shortcut)
	}
	if o.IsNegatable() {
		synopsis += fmt.Sprintf("--%s|--no-%s", o.Name, o.Name)
	} else {
		synopsis += fmt.Sprintf("--%s%s", o.Name, value)
	}
	multiple := ""
	if o.IsArray() {
		multiple = "<comment> (multiple values allowed)</comment>"
	}
	spacing := totalWidth - Width(synopsis)
	d.writeText(fmt.Sprintf("  <info>%s</info>  %s%s%s%s", synopsis, strings.Repeat(" ", max(0, spacing)),
		lineBreakSpaces.ReplaceAllLiteralString(o.Description, "\n"+strings.Repeat(" ", totalWidth+4)), def, multiple))
}

func totalWidthForOptions(opts []*Option) int {
	total := 0
	for _, o := range opts {
		n := 1 + max(Width(o.Shortcut), 1) + 4 + Width(o.Name)
		if o.IsNegatable() {
			n += 6 + Width(o.Name)
		} else if o.AcceptValue() {
			v := 1 + Width(o.Name)
			if o.IsValueOptional() {
				v += 2
			}
			n += v
		}
		total = max(total, n)
	}
	return total
}

func (d *textDescriptor) describeDefinition(def *Definition) {
	total := totalWidthForOptions(def.Options())
	for _, a := range def.Arguments() {
		total = max(total, Width(a.Name))
	}
	if len(def.Arguments()) > 0 {
		d.writeText("<comment>Arguments:</comment>")
		d.writeText("\n")
		for _, a := range def.Arguments() {
			d.describeArgument(a, total)
			d.writeText("\n")
		}
	}
	if len(def.Arguments()) > 0 && len(def.Options()) > 0 {
		d.writeText("\n")
	}
	if len(def.Options()) > 0 {
		var later []*Option
		d.writeText("<comment>Options:</comment>")
		for _, o := range def.Options() {
			if len(o.Shortcut) > 1 {
				later = append(later, o)
				continue
			}
			d.writeText("\n")
			d.describeOption(o, total)
		}
		for _, o := range later {
			d.writeText("\n")
			d.describeOption(o, total)
		}
	}
}

func (d *textDescriptor) describeCommand(c *Command) {
	c.MergeApplicationDefinition(false)
	if c.Description != "" {
		d.writeText("<comment>Description:</comment>")
		d.writeText("\n")
		d.writeText("  " + c.Description)
		d.writeText("\n\n")
	}
	d.writeText("<comment>Usage:</comment>")
	usages := append(append([]string{c.Synopsis(true)}, c.Aliases...), c.Usages...)
	for _, u := range usages {
		d.writeText("\n")
		d.writeText("  " + Escape(u))
	}
	d.writeText("\n")
	def := c.FullDefinition()
	if len(def.Options()) > 0 || len(def.Arguments()) > 0 {
		d.writeText("\n")
		d.describeDefinition(def)
		d.writeText("\n")
	}
	help := c.ProcessedHelp()
	if help != "" && help != c.Description {
		d.writeText("\n")
		d.writeText("<comment>Help:</comment>")
		d.writeText("\n")
		d.writeText("  " + strings.ReplaceAll(help, "\n", "\n  "))
		d.writeText("\n")
	}
}

func columnWidth(a *Application, names []string, withAliases bool) int {
	w := -1
	for _, n := range names {
		w = max(w, Width(n))
		if withAliases {
			for _, alias := range a.commands[n].Aliases {
				w = max(w, Width(alias))
			}
		}
	}
	if w < 0 {
		return 0
	}
	return w + 2
}

func (d *textDescriptor) describeApplication(a *Application, namespace string) error {
	if d.rawText {
		desc, err := a.newApplicationDescription(namespace, false)
		if err != nil {
			return err
		}
		width := columnWidth(a, desc.names, true)
		for _, name := range desc.names {
			d.writeText(fmt.Sprintf("%-*s %s", width, name, desc.commands[name].Description))
			d.writeText("\n")
		}
		return nil
	}
	if help := a.LongVersion(); help != "" {
		d.writeText(help + "\n\n")
	}
	d.writeText("<comment>Usage:</comment>\n")
	d.writeText("  command [options] [arguments]\n\n")
	d.describeDefinition(NewDefinition(nil, a.definition().Options()))
	d.writeText("\n")
	d.writeText("\n")
	// ApplicationDescription inspects the application lazily, after the header is written
	desc, err := a.newApplicationDescription(namespace, false)
	if err != nil {
		return err
	}
	commands := map[string]*Command{}
	for name, c := range desc.commands {
		commands[name] = c
	}
	if namespace != "" && len(desc.namespaces) > 0 {
		for _, name := range desc.namespaces[0].Commands {
			if c, ok := desc.commands[name]; ok {
				commands[name] = c
			} else {
				commands[name] = desc.aliases[name]
			}
		}
	}
	var listed []string
	for _, ns := range desc.namespaces {
		for _, name := range ns.Commands {
			if _, ok := commands[name]; ok {
				listed = append(listed, name)
			}
		}
	}
	width := columnWidth(a, listed, false)
	if namespace != "" {
		d.writeText(fmt.Sprintf(`<comment>Available commands for the "%s" namespace:</comment>`, namespace))
	} else {
		d.writeText("<comment>Available commands:</comment>")
	}
	for _, ns := range desc.namespaces {
		var names []string
		for _, name := range ns.Commands {
			if _, ok := commands[name]; ok {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			continue
		}
		if namespace == "" && ns.ID != globalNamespace {
			d.writeText("\n")
			d.writeText(" <comment>" + ns.ID + "</comment>")
		}
		for _, name := range names {
			d.writeText("\n")
			c := commands[name]
			aliases := ""
			if name == c.Name && len(c.Aliases) > 0 {
				aliases = "[" + strings.Join(c.Aliases, "|") + "] "
			}
			d.writeText(fmt.Sprintf("  <info>%s</info>%s%s", name, strings.Repeat(" ", width-Width(name)), aliases+c.Description))
		}
	}
	d.writeText("\n")
	return nil
}

type jsonDescriptor struct{ out Output }

func (d *jsonDescriptor) write(data any) {
	d.out.Write(string(phpx.Encode(data, 0)), false, OutputRaw)
}

func jsonDefault(def any) any {
	if def == nil {
		return nil
	}
	return def
}

func argumentData(a *Argument) phpx.Map {
	return phpx.Map{
		{Key: "name", Value: a.Name},
		{Key: "is_required", Value: a.IsRequired()},
		{Key: "is_array", Value: a.IsArray()},
		{Key: "description", Value: lineBreakSpaces.ReplaceAllLiteralString(a.Description, " ")},
		{Key: "default", Value: jsonDefault(a.Default)},
	}
}

func optionData(o *Option, negated bool) phpx.Map {
	if negated {
		var def any
		if b, ok := o.Default.(bool); ok {
			def = !b
		} else if o.Default != nil {
			def = !phpTruthy(o.Default)
		}
		return phpx.Map{
			{Key: "name", Value: "--no-" + o.Name},
			{Key: "shortcut", Value: ""},
			{Key: "accept_value", Value: false},
			{Key: "is_value_required", Value: false},
			{Key: "is_multiple", Value: false},
			{Key: "description", Value: `Negate the "--` + o.Name + `" option`},
			{Key: "default", Value: def},
		}
	}
	shortcut := ""
	if o.Shortcut != "" {
		shortcut = "-" + strings.ReplaceAll(o.Shortcut, "|", "|-")
	}
	return phpx.Map{
		{Key: "name", Value: "--" + o.Name},
		{Key: "shortcut", Value: shortcut},
		{Key: "accept_value", Value: o.AcceptValue()},
		{Key: "is_value_required", Value: o.IsValueRequired()},
		{Key: "is_multiple", Value: o.IsArray()},
		{Key: "description", Value: lineBreakSpaces.ReplaceAllLiteralString(o.Description, " ")},
		{Key: "default", Value: jsonDefault(o.Default)},
	}
}

func phpTruthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != "" && t != "0"
	case []string:
		return len(t) > 0
	case []any:
		return len(t) > 0
	}
	return true
}

func definitionData(def *Definition) phpx.Map {
	var args, opts any = []any{}, []any{}
	if len(def.Arguments()) > 0 {
		m := phpx.Map{}
		for _, a := range def.Arguments() {
			m = append(m, phpx.MapItem{Key: a.Name, Value: argumentData(a)})
		}
		args = m
	}
	if len(def.Options()) > 0 {
		m := phpx.Map{}
		for _, o := range def.Options() {
			m = append(m, phpx.MapItem{Key: o.Name, Value: optionData(o, false)})
			if o.IsNegatable() {
				m = append(m, phpx.MapItem{Key: "no-" + o.Name, Value: optionData(o, true)})
			}
		}
		opts = m
	}
	return phpx.Map{{Key: "arguments", Value: args}, {Key: "options", Value: opts}}
}

func (d *jsonDescriptor) commandData(c *Command, short bool) phpx.Map {
	data := phpx.Map{{Key: "name", Value: c.Name}, {Key: "description", Value: c.Description}}
	if short {
		data = append(data, phpx.MapItem{Key: "usage", Value: append([]string{}, c.Aliases...)})
	} else {
		c.MergeApplicationDefinition(false)
		usage := append(append([]string{c.Synopsis(false)}, c.Usages...), c.Aliases...)
		data = append(data,
			phpx.MapItem{Key: "usage", Value: usage},
			phpx.MapItem{Key: "help", Value: c.ProcessedHelp()},
			phpx.MapItem{Key: "definition", Value: definitionData(c.FullDefinition())},
		)
	}
	return append(data, phpx.MapItem{Key: "hidden", Value: c.Hidden})
}

func (d *jsonDescriptor) describeApplication(a *Application, namespace string, short bool) error {
	desc, err := a.newApplicationDescription(namespace, true)
	if err != nil {
		return err
	}
	commands := []any{}
	for _, name := range desc.names {
		commands = append(commands, d.commandData(desc.commands[name], short))
	}
	data := phpx.Map{}
	if a.Name != "UNKNOWN" {
		app := phpx.Map{{Key: "name", Value: a.Name}}
		if a.Version != "UNKNOWN" {
			app = append(app, phpx.MapItem{Key: "version", Value: a.Version})
		}
		data = append(data, phpx.MapItem{Key: "application", Value: app})
	}
	data = append(data, phpx.MapItem{Key: "commands", Value: commands})
	if namespace != "" {
		data = append(data, phpx.MapItem{Key: "namespace", Value: namespace})
	} else {
		namespaces := []any{}
		for _, ns := range desc.namespaces {
			names := append([]string{}, ns.Commands...)
			namespaces = append(namespaces, phpx.Map{{Key: "id", Value: ns.ID}, {Key: "commands", Value: names}})
		}
		data = append(data, phpx.MapItem{Key: "namespaces", Value: namespaces})
	}
	d.write(data)
	return nil
}
