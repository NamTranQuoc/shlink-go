package console

import (
	"fmt"
	"strconv"
	"strings"
)

// varExport ports var_export() for option and argument defaults, with newlines removed like MarkdownDescriptor does.
func varExport(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case string:
		return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(t) + "'"
	case []string:
		var b strings.Builder
		b.WriteString("array (")
		for i, s := range t {
			fmt.Fprintf(&b, "  %d => %s,", i, varExport(s))
		}
		b.WriteString(")")
		return b.String()
	}
	return fmt.Sprint(v)
}

type markdownDescriptor struct{ out Output }

func (d *markdownDescriptor) write(s string) { d.out.Write(s, false, OutputNormal) }

func (d *markdownDescriptor) describeArgument(a *Argument) {
	name := a.Name
	if name == "" {
		name = "<none>"
	}
	desc := ""
	if a.Description != "" {
		desc = lineBreakSpaces.ReplaceAllLiteralString(a.Description, "\n") + "\n\n"
	}
	d.write("#### `" + name + "`\n\n" + desc +
		"* Is required: " + yesNo(a.IsRequired()) + "\n" +
		"* Is array: " + yesNo(a.IsArray()) + "\n" +
		"* Default: `" + varExport(a.Default) + "`")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func (d *markdownDescriptor) describeOption(o *Option) {
	name := "--" + o.Name
	if o.IsNegatable() {
		name += "|--no-" + o.Name
	}
	if o.Shortcut != "" {
		name += "|-" + strings.ReplaceAll(o.Shortcut, "|", "|-")
	}
	desc := ""
	if o.Description != "" {
		desc = lineBreakSpaces.ReplaceAllLiteralString(o.Description, "\n") + "\n\n"
	}
	d.write("#### `" + name + "`\n\n" + desc +
		"* Accept value: " + yesNo(o.AcceptValue()) + "\n" +
		"* Is value required: " + yesNo(o.IsValueRequired()) + "\n" +
		"* Is multiple: " + yesNo(o.IsArray()) + "\n" +
		"* Is negatable: " + yesNo(o.IsNegatable()) + "\n" +
		"* Default: `" + varExport(o.Default) + "`")
}

func (d *markdownDescriptor) describeDefinition(def *Definition) {
	showArguments := len(def.Arguments()) > 0
	if showArguments {
		d.write("### Arguments")
		for _, a := range def.Arguments() {
			d.write("\n\n")
			d.describeArgument(a)
		}
	}
	if len(def.Options()) > 0 {
		if showArguments {
			d.write("\n\n")
		}
		d.write("### Options")
		for _, o := range def.Options() {
			d.write("\n\n")
			d.describeOption(o)
		}
	}
}

func (d *markdownDescriptor) describeCommand(c *Command, short bool) {
	head := "`" + c.Name + "`\n" + strings.Repeat("-", Width(c.Name)+2) + "\n\n"
	if c.Description != "" {
		head += c.Description + "\n\n"
	}
	head += "### Usage\n\n"
	if short {
		for _, u := range c.Aliases {
			head += "* `" + u + "`\n"
		}
		d.write(head)
		return
	}
	c.MergeApplicationDefinition(false)
	for _, u := range append(append([]string{c.Synopsis(false)}, c.Aliases...), c.Usages...) {
		head += "* `" + u + "`\n"
	}
	d.write(head)
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

func (d *markdownDescriptor) describeApplication(a *Application, namespace string, short bool) error {
	title := a.Name + " " + a.Version
	d.write(title + "\n" + strings.Repeat("=", Width(title)))
	desc, err := a.newApplicationDescription(namespace, false)
	if err != nil {
		return err
	}
	for _, ns := range desc.namespaces {
		if ns.ID != globalNamespace {
			d.write("\n\n")
			d.write("**" + ns.ID + ":**")
		}
		d.write("\n\n")
		links := make([]string, len(ns.Commands))
		for i, name := range ns.Commands {
			c := desc.commands[name]
			if c == nil {
				c = desc.aliases[name]
			}
			links[i] = fmt.Sprintf("* [`%s`](#%s)", name, strings.ReplaceAll(c.Name, ":", ""))
		}
		d.write(strings.Join(links, "\n"))
	}
	for _, name := range desc.names {
		d.write("\n\n")
		d.describeCommand(desc.commands[name], short)
	}
	return nil
}

// xmlNode is a minimal DOM element for XmlDescriptor's documents.
type xmlNode struct {
	name     string
	attrs    [][2]string
	text     *string
	children []*xmlNode
}

func (n *xmlNode) attr(k, v string) *xmlNode { n.attrs = append(n.attrs, [2]string{k, v}); return n }
func (n *xmlNode) add(c *xmlNode) *xmlNode   { n.children = append(n.children, c); return c }
func newXMLText(name, text string) *xmlNode  { return &xmlNode{name: name, text: &text} }

var (
	xmlTextEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#13;")
	xmlAttrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "\n", "&#10;", "\r", "&#13;", "\t", "&#9;")
)

// serialize ports libxml's formatted output: elements with element children are indented by two spaces per level,
// elements with text are written inline, and empty elements are self-closed.
func (n *xmlNode) serialize(b *strings.Builder, depth int) {
	indent := strings.Repeat("  ", depth)
	b.WriteString(indent + "<" + n.name)
	for _, a := range n.attrs {
		b.WriteString(" " + a[0] + `="` + xmlAttrEscaper.Replace(a[1]) + `"`)
	}
	switch {
	case n.text != nil && *n.text != "":
		b.WriteString(">" + xmlTextEscaper.Replace(*n.text) + "</" + n.name + ">\n")
	case len(n.children) == 0:
		b.WriteString("/>\n")
	default:
		b.WriteString(">\n")
		for _, c := range n.children {
			c.serialize(b, depth+1)
		}
		b.WriteString(indent + "</" + n.name + ">\n")
	}
}

func boolAttr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func xmlDefaults(def any) []string {
	switch t := def.(type) {
	case []string:
		return t
	case bool:
		return []string{strconv.FormatBool(t)}
	case nil:
		return nil
	case string:
		if t == "" || t == "0" {
			return nil
		}
		return []string{t}
	case int:
		if t == 0 {
			return nil
		}
		return []string{strconv.Itoa(t)}
	}
	return []string{fmt.Sprint(def)}
}

func xmlArgument(a *Argument) *xmlNode {
	n := &xmlNode{name: "argument"}
	n.attr("name", a.Name).attr("is_required", boolAttr(a.IsRequired())).attr("is_array", boolAttr(a.IsArray()))
	n.add(newXMLText("description", a.Description))
	defaults := n.add(&xmlNode{name: "defaults"})
	for _, v := range xmlDefaults(a.Default) {
		defaults.add(newXMLText("default", v))
	}
	return n
}

func xmlOptions(o *Option) []*xmlNode {
	n := &xmlNode{name: "option"}
	n.attr("name", "--"+o.Name)
	if pos := strings.Index(o.Shortcut, "|"); pos >= 0 {
		n.attr("shortcut", "-"+o.Shortcut[:pos]).attr("shortcuts", "-"+strings.ReplaceAll(o.Shortcut, "|", "|-"))
	} else if o.Shortcut != "" {
		n.attr("shortcut", "-"+o.Shortcut)
	} else {
		n.attr("shortcut", "")
	}
	n.attr("accept_value", boolAttr(o.AcceptValue())).attr("is_value_required", boolAttr(o.IsValueRequired())).
		attr("is_multiple", boolAttr(o.IsArray()))
	n.add(newXMLText("description", o.Description))
	if o.AcceptValue() {
		defaults := n.add(&xmlNode{name: "defaults"})
		for _, v := range xmlDefaults(o.Default) {
			defaults.add(newXMLText("default", v))
		}
	}
	nodes := []*xmlNode{n}
	if o.IsNegatable() {
		neg := &xmlNode{name: "option"}
		neg.attr("name", "--no-"+o.Name).attr("shortcut", "").attr("accept_value", "0").attr("is_value_required", "0").
			attr("is_multiple", "0")
		neg.add(newXMLText("description", `Negate the "--`+o.Name+`" option`))
		nodes = append(nodes, neg)
	}
	return nodes
}

func xmlCommand(c *Command, short bool) *xmlNode {
	n := &xmlNode{name: "command"}
	n.attr("id", c.Name).attr("name", c.Name).attr("hidden", boolAttr(c.Hidden))
	usages := n.add(&xmlNode{name: "usages"})
	n.add(newXMLText("description", strings.ReplaceAll(c.Description, "\n", "\n ")))
	if short {
		for _, u := range c.Aliases {
			usages.add(newXMLText("usage", u))
		}
		return n
	}
	c.MergeApplicationDefinition(false)
	for _, u := range append(append([]string{c.Synopsis(false)}, c.Aliases...), c.Usages...) {
		usages.add(newXMLText("usage", u))
	}
	n.add(newXMLText("help", strings.ReplaceAll(c.ProcessedHelp(), "\n", "\n ")))
	def := c.FullDefinition()
	args := n.add(&xmlNode{name: "arguments"})
	for _, a := range def.Arguments() {
		args.add(xmlArgument(a))
	}
	opts := n.add(&xmlNode{name: "options"})
	for _, o := range def.Options() {
		for _, on := range xmlOptions(o) {
			opts.add(on)
		}
	}
	return n
}

func writeXML(out Output, root *xmlNode) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	root.serialize(&b, 0)
	out.Write(b.String(), false, OutputRaw)
}

func (a *Application) describeXML(out Output, object any, opts describeOptions) error {
	if c, ok := object.(*Command); ok {
		writeXML(out, xmlCommand(c, opts.Short))
		return nil
	}
	desc, err := a.newApplicationDescription(opts.Namespace, true)
	if err != nil {
		return err
	}
	root := &xmlNode{name: "symfony"}
	root.attr("name", a.Name).attr("version", a.Version)
	commands := root.add(&xmlNode{name: "commands"})
	if opts.Namespace != "" {
		commands.attr("namespace", opts.Namespace)
	}
	for _, name := range desc.names {
		commands.add(xmlCommand(desc.commands[name], opts.Short))
	}
	if opts.Namespace == "" {
		namespaces := root.add(&xmlNode{name: "namespaces"})
		for _, ns := range desc.namespaces {
			nsNode := namespaces.add(&xmlNode{name: "namespace"})
			nsNode.attr("id", ns.ID)
			for _, name := range ns.Commands {
				nsNode.add(newXMLText("command", name))
			}
		}
	}
	writeXML(out, root)
	return nil
}
