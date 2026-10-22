package cli

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
	"github.com/NamTranQuoc/shlink-go/internal/valinor"
)

// phpNumeric matches the strings PHP's is_numeric() accepts.
var phpNumeric = regexp.MustCompile(`^[ \t\n\r\v\f]*[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?[ \t\n\r\v\f]*$`)

// intOption ports BuiltinTypeValueResolver for an `int` option: nil when not given, an InvalidOptionException when the
// value is not numeric, and PHP's coercion of numeric strings to int otherwise.
func intOption(in *console.Input, name string) (*int, error) {
	v := in.OptionString(name)
	if v == nil {
		return nil, nil
	}
	if !phpNumeric.MatchString(*v) {
		return nil, console.NewConsoleError(`Symfony\Component\Console\Exception\InvalidOptionException`,
			fmt.Sprintf(`The value "%s" is not valid for the "%s" option. Expected a value of type "int".`, *v, name),
			"InvalidOptionException.php", 40)
	}
	s := strings.Trim(*v, " \t\n\r\v\f")
	if i, err := strconv.Atoi(s); err == nil {
		return &i, nil
	}
	f, _ := strconv.ParseFloat(s, 64)
	i := int(math.Trunc(f))
	return &i, nil
}

// mapInputTypeError ports the TypeError PHP throws when MapInput::createInstance() assigns a non-numeric string to an
// int property. It only happens while a command with interactive attributes runs its interactions: the value
// resolvers used otherwise reject the value first.
func mapInputTypeError(class, property, nullable string, command string, nested bool) error {
	const mapInput = "/etc/shlink/vendor/symfony/console/Attribute/MapInput.php"
	const argv = `Object(Symfony\Component\Console\Input\ArgvInput)`
	const output = `Object(Symfony\Component\Console\Output\ConsoleOutput)`
	frames := []string{}
	if nested {
		frames = append(frames, mapInput+`(184): Symfony\Component\Console\Attribute\MapInput->createInstance(`+argv+`)`)
	}
	frames = append(frames,
		`/etc/shlink/vendor/symfony/console/Interaction/Interaction.php(38): Symfony\Component\Console\Attribute\MapInput->createInstance(`+argv+`)`,
		`/etc/shlink/vendor/symfony/console/Command/InvokableCommand.php(223): Symfony\Component\Console\Interaction\Interaction->interact(`+argv+`, `+output+`, Object(Closure))`,
		`/etc/shlink/vendor/symfony/console/Command/Command.php(275): Symfony\Component\Console\Command\InvokableCommand->interact(`+argv+`, `+output+`)`,
		`/etc/shlink/vendor/symfony/console/Application.php(1129): Symfony\Component\Console\Command\Command->run(`+argv+`, `+output+`)`,
		`/etc/shlink/vendor/symfony/console/Application.php(380): Symfony\Component\Console\Application->doRunCommand(Object(`+command+`), `+argv+`, `+output+`)`,
		`/etc/shlink/vendor/symfony/console/Application.php(219): Symfony\Component\Console\Application->doRun(`+argv+`, `+output+`)`,
		`/etc/shlink/bin/cli(10): Symfony\Component\Console\Application->run()`,
		"{main}",
	)
	var b strings.Builder
	fmt.Fprintf(&b, "\nFatal error: Uncaught TypeError: Cannot assign string to property %s::$%s of type %s in %s:182\nStack trace:\n",
		class, property, nullable, mapInput)
	for i, f := range frames {
		fmt.Fprintf(&b, "#%d %s\n", i, f)
	}
	fmt.Fprintf(&b, "  thrown in %s on line 182\n", mapInput)
	return &console.PHPFatalError{Output: b.String()}
}

// checkMapInputInts ports the MapInput instance creation done by interactive commands: the first given int option
// with a non-numeric value is a TypeError.
func checkMapInputInts(in *console.Input, command string, props []mapInputInt) error {
	for _, p := range props {
		v := in.OptionString(p.option)
		if v != nil && !phpNumeric.MatchString(*v) {
			return mapInputTypeError(p.class, p.property, p.typ, command, p.nested)
		}
	}
	return nil
}

// mapInputInt describes an int property of a MapInput class.
type mapInputInt struct {
	option, class, property, typ string
	// nested tells if the class is mapped inside another MapInput class
	nested bool
}

// mapperError converts a validation error raised while mapping CLI input into the TypeTreeMapperError Valinor throws.
// source is the mapped array, in the order upstream builds it.
func mapperError(err error, rootType string, source phpx.Map) error {
	var pe *core.ProblemError
	if !errors.As(err, &pe) || pe.Kind != core.KindValidation || len(pe.Mapping) == 0 {
		return err
	}
	var msg string
	if len(pe.Mapping) == 1 {
		msg = fmt.Sprintf("Could not map type `%s`. An error occurred at path %s: %s", rootType, pe.Mapping[0].Path,
			pe.Mapping[0].Message)
	} else {
		msg = fmt.Sprintf("Could not map type `%s` with value %s. A total of %d errors were encountered.", rootType,
			valueDump(source, true), len(pe.Mapping))
	}
	return &console.PHPError{Class: `CuyZ\Valinor\Mapper\TypeTreeMapperError`, Message: msg, File: "TypeTreeMapper.php", Line: 38}
}

// valinorInput converts an ordered PHP array into mapper input.
func valinorInput(m phpx.Map) valinor.Input {
	in := valinor.Input{}
	for _, item := range m {
		if e, ok := item.Value.(phpEnum); ok {
			in[item.Key] = string(e)
			continue
		}
		in[item.Key] = item.Value
	}
	return in
}

// valueDump ports Valinor's ValueDumper::dump() for the values CLI input holds.
func valueDump(v any, goDeeper bool) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(t)
	case string:
		return dumpString(t)
	case phpEnum:
		return "'" + string(t) + "'"
	case []any:
		m := make(phpx.Map, len(t))
		for i, item := range t {
			m[i] = phpx.MapItem{Key: strconv.Itoa(i), Value: item}
		}
		return valueDump(m, goDeeper)
	case phpx.Map:
		if len(t) == 0 {
			return "array (empty)"
		}
		if !goDeeper {
			return "array{…}"
		}
		var values []string
		for i, item := range t {
			values = append(values, item.Key+": "+valueDump(item.Value, false))
			if i >= 5 {
				values = append(values, "…")
				break
			}
		}
		return "array{" + strings.Join(values, ", ") + "}"
	}
	return fmt.Sprint(v)
}

// phpEnum is a string-backed enum case in a mapped array.
type phpEnum string

func dumpString(s string) string {
	if len(s) > 50 {
		cut := s[:51]
		cropped := ""
		for i := len(cut) - 1; i > 10; i-- {
			if cut[i] == ' ' {
				cropped = cut[:i] + "…"
				break
			}
		}
		if cropped == "" {
			cropped = cut + "…"
		}
		s = cropped
	}
	switch {
	case strings.Contains(s, "'") && strings.Contains(s, `"`):
		return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
	case strings.Contains(s, "'"):
		return `"` + s + `"`
	}
	return "'" + s + "'"
}
