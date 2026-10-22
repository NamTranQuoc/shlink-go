package console

import (
	"strings"
)

// rstDescriptor ports ReStructuredTextDescriptor.
type rstDescriptor struct {
	out               Output
	visibleNamespaces []string
}

func (d *rstDescriptor) write(s string) { d.out.Write(s, false, OutputNormal) }

func (d *rstDescriptor) describeArgument(a *Argument) {
	// Upstream's operator precedence makes a named argument print its name only
	if a.Name != "" {
		d.write(a.Name)
		return
	}
	desc := ""
	if a.Description != "" {
		desc = lineBreakSpaces.ReplaceAllLiteralString(a.Description, "\n") + "\n\n"
	}
	d.write("<none>\n\n" + desc +
		"- **Is required**: " + yesNo(a.IsRequired()) + "\n" +
		"- **Is array**: " + yesNo(a.IsArray()) + "\n" +
		"- **Default**: ``" + varExport(a.Default) + "``")
}

func (d *rstDescriptor) describeOption(o *Option) {
	name := `\-\-` + o.Name
	if o.IsNegatable() {
		name += `|\-\-no-` + o.Name
	}
	if o.Shortcut != "" {
		name += "|-" + strings.ReplaceAll(o.Shortcut, "|", "|-")
	}
	desc := ""
	if o.Description != "" {
		desc = lineBreakSpaces.ReplaceAllLiteralString(o.Description, "\n\n") + "\n\n"
	}
	d.write(name + "\n" + strings.Repeat(`"`, Width(name)) + "\n\n" + desc +
		"- **Accept value**: " + yesNo(o.AcceptValue()) + "\n" +
		"- **Is value required**: " + yesNo(o.IsValueRequired()) + "\n" +
		"- **Is multiple**: " + yesNo(o.IsArray()) + "\n" +
		"- **Is negatable**: " + yesNo(o.IsNegatable()) + "\n" +
		"- **Default**: ``" + varExport(o.Default) + "``\n")
}

var rstGlobalOptions = map[string]bool{"help": true, "silent": true, "quiet": true, "verbose": true, "version": true,
	"ansi": true, "no-interaction": true}

func (d *rstDescriptor) describeDefinition(def *Definition) {
	showArguments := len(def.Arguments()) > 0
	if showArguments {
		d.write("Arguments\n" + strings.Repeat("^", 9))
		for _, a := range def.Arguments() {
			d.write("\n\n")
			d.describeArgument(a)
		}
	}
	var options []*Option
	for _, o := range def.Options() {
		if !rstGlobalOptions[o.Name] {
			options = append(options, o)
		}
	}
	if len(options) > 0 {
		if showArguments {
			d.write("\n\n")
		}
		d.write("Options\n" + strings.Repeat("^", 7) + "\n\n")
		for _, o := range options {
			d.describeOption(o)
			d.write("\n")
		}
	}
}

func (d *rstDescriptor) describeCommand(c *Command, short bool) {
	if short {
		s := "``" + c.Name + "``\n" + strings.Repeat(".", Width(c.Name)) + "\n\n"
		if c.Description != "" {
			s += c.Description + "\n\n"
		}
		s += "Usage\n" + strings.Repeat(`"`, 5) + "\n\n"
		for _, u := range c.Aliases {
			s += "- ``" + u + "``\n"
		}
		d.write(s)
		return
	}
	c.MergeApplicationDefinition(false)
	for _, alias := range c.Aliases {
		d.write(".. _" + alias + ":\n\n")
	}
	s := c.Name + "\n" + strings.Repeat(".", Width(c.Name)) + "\n\n"
	if c.Description != "" {
		s += c.Description + "\n\n"
	}
	s += "Usage\n" + strings.Repeat("^", 5) + "\n\n"
	for _, u := range append(append([]string{c.Synopsis(false)}, c.Aliases...), c.Usages...) {
		s += "- ``" + u + "``\n"
	}
	d.write(s)
	if help := c.ProcessedHelp(); help != "" {
		d.write("\n")
		d.write(help)
	}
	def := c.FullDefinition()
	if len(def.Options()) > 0 || len(def.Arguments()) > 0 {
		d.write("\n\n")
		d.describeDefinition(def)
	}
}

// visibleCommands ports removeAliasesAndHiddenCommands() over Application::all($namespace).
func (d *rstDescriptor) visibleCommands(a *Application, namespace string) []*Command {
	var out []*Command
	for _, name := range a.all(&namespace) {
		c := a.commands[name]
		if c.Hidden || name != c.Name || name == "completion" {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (d *rstDescriptor) describeApplication(a *Application, namespace string, short bool) error {
	title := a.Name + " " + a.Version
	d.write(title + "\n" + strings.Repeat("=", Width(title)))
	desc, err := a.newApplicationDescription(namespace, false)
	if err != nil {
		return err
	}
	for _, ns := range desc.namespaces {
		visible := false
		for _, name := range ns.Commands {
			if c, ok := desc.commands[name]; ok && !c.Hidden {
				visible = true
			}
		}
		if visible {
			d.visibleNamespaces = append(d.visibleNamespaces, ns.ID)
		}
	}
	chapter := "Table of Contents"
	d.write("\n\n" + chapter + "\n" + strings.Repeat("-", Width(chapter)) + "\n\n")
	for _, ns := range d.visibleNamespaces {
		lookup := ns
		if ns == globalNamespace {
			lookup = ""
		} else {
			d.write("\n\n")
			d.write(ns + "\n" + strings.Repeat("~", Width(ns)) + "\n\n")
		}
		var links []string
		for _, c := range d.visibleCommands(a, lookup) {
			links = append(links, "- `"+c.Name+"`_")
		}
		d.write("\n\n")
		d.write(strings.Join(links, "\n"))
	}
	commandsTitle := "Commands"
	d.write("\n\n" + commandsTitle + "\n" + strings.Repeat("-", Width(commandsTitle)) + "\n\n")
	for _, ns := range d.visibleNamespaces {
		lookup := ns
		if ns == globalNamespace {
			lookup = ""
			d.write("Global\n" + strings.Repeat("~", Width("Global")) + "\n\n")
		} else {
			d.write(ns + "\n" + strings.Repeat("~", Width(ns)) + "\n\n")
		}
		for _, c := range d.visibleCommands(a, lookup) {
			d.describeCommand(c, short)
			d.write("\n\n")
		}
	}
	return nil
}
