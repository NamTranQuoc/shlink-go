package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// Upstream exception classes and where they are created, used to render errors like PHP does.
type phpExceptionInfo struct {
	class string
	file  string
	line  int
}

var problemExceptions = map[string]phpExceptionInfo{
	core.KindTagNotFound:      {`Shlinkio\Shlink\Core\Exception\TagNotFoundException`, "TagNotFoundException.php", 23},
	core.KindTagConflict:      {`Shlinkio\Shlink\Core\Exception\TagConflictException`, "TagConflictException.php", 24},
	core.KindDomainNotFound:   {`Shlinkio\Shlink\Core\Exception\DomainNotFoundException`, "DomainNotFoundException.php", 39},
	core.KindShortURLNotFound: {`Shlinkio\Shlink\Core\Exception\ShortUrlNotFoundException`, "ShortUrlNotFoundException.php", 27},
	core.KindForbiddenTagOp:   {`Shlinkio\Shlink\Core\Exception\ForbiddenTagOperationException`, "ForbiddenTagOperationException.php", 32},
	core.KindNonUniqueSlug:    {`Shlinkio\Shlink\Core\Exception\NonUniqueSlugException`, "NonUniqueSlugException.php", 25},
	core.KindDeleteShortURL:   {`Shlinkio\Shlink\Core\Exception\DeleteShortUrlException`, "DeleteShortUrlException.php", 27},
	core.KindValidation:       {`Shlinkio\Shlink\Core\Exception\ValidationException`, "ValidationException.php", 34},
}

// phpException converts errors returned by core services into the exceptions upstream would throw, so the console
// application renders them identically. Console exceptions and unknown errors pass through.
func phpException(err error) error {
	if err == nil {
		return nil
	}
	var ex console.Exception
	if errors.As(err, &ex) {
		return err
	}
	var pe *core.ProblemError
	if errors.As(err, &pe) {
		if info, ok := problemExceptions[pe.Kind]; ok {
			code := 0
			if pe.Kind == core.KindValidation {
				code = pe.Status
			}
			return &console.PHPError{Class: info.class, Message: pe.Detail, File: info.file, Line: info.line, ExCode: code}
		}
	}
	var outOfRange core.OutOfRangePageError
	if errors.As(err, &outOfRange) {
		return &console.PHPError{Class: `Pagerfanta\Exception\OutOfRangeCurrentPageException`, Message: outOfRange.Error(),
			File: "Pagerfanta.php", Line: 221}
	}
	var notFound core.APIKeyNotFoundError
	if errors.As(err, &notFound) {
		return &console.PHPError{Class: `Shlinkio\Shlink\Rest\Exception\ApiKeyNotFoundException`, Message: notFound.Error(),
			File: "ApiKeyNotFoundException.php", Line: 13}
	}
	var conflict core.APIKeyConflictError
	if errors.As(err, &conflict) {
		return &console.PHPError{Class: `Shlinkio\Shlink\Rest\Exception\ApiKeyConflictException`, Message: conflict.Error(),
			File: "ApiKeyConflictException.php", Line: 13}
	}
	var invalid core.InvalidArgumentError
	if errors.As(err, &invalid) {
		return &console.PHPError{Class: `Shlinkio\Shlink\Common\Exception\InvalidArgumentException`, Message: invalid.Message,
			File: "ApiKeyService.php", Line: 45}
	}
	return err
}

// shlinkTable ports ShlinkTable::render(), with ShlinkTable::withRowSeparators() when separators is true.
func shlinkTable(out console.Output, separators bool, headers []any, rows [][]any, footerTitle, headerTitle string) {
	style := console.TableStyleDefinition("default")
	style.FooterTitleFormat = "<options=bold> %s </>"
	style.HeaderTitleFormat = "<options=bold> %s </>"
	tableRows := make([]any, 0, len(rows)*2)
	for i, r := range rows {
		if separators && i > 0 {
			tableRows = append(tableRows, console.TableSeparator{})
		}
		tableRows = append(tableRows, r)
	}
	console.NewTable(out).SetStyle(style).SetHeaders(headers).SetRows(tableRows).
		SetFooterTitle(footerTitle).SetHeaderTitle(headerTitle).Render()
}

// executeWithWarning ports CommandUtils::executeWithWarning().
func executeWithWarning(warning string, io *console.SymfonyStyle, callback func() (int, error)) (int, error) {
	io.Warning(warning)
	proceed, err := io.Confirm("<comment>Do you want to proceed?</comment>", false)
	if err != nil {
		return 0, err
	}
	if !proceed {
		io.Info("Operation aborted")
		return console.Success, nil
	}
	return callback()
}

// arrayToString ports Shlinkio\Shlink\Core\arrayToString() for flat arrays, with keys in the given order.
func arrayToString(keys []string, values map[string]any, indentSize int) string {
	indent := strings.Repeat(" ", indentSize)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s'%s' => %s", indent, k, phpString(values[k]))
	}
	return b.String()
}

// phpString ports PHP's string conversion of scalars.
func phpString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		if t {
			return "1"
		}
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprint(int64(t))
		}
	}
	return fmt.Sprint(v)
}

// roleMetaKeys returns a role's meta keys in the order upstream builds them (RoleDefinition::forDomain() sets
// domain_id, then authority).
func roleMetaKeys(meta map[string]any) []string {
	var keys []string
	for _, k := range []string{"domain_id", "authority"} {
		if _, ok := meta[k]; ok {
			keys = append(keys, k)
		}
	}
	var rest []string
	for k := range meta {
		if k != "domain_id" && k != "authority" {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

// normalizeOptionalDate ports normalizeOptionalDate() for CLI string options: Chronos::parse() in the configured
// timezone.
func normalizeOptionalDate(v *string) (*time.Time, error) {
	if v == nil {
		return nil, nil
	}
	loc := config.Location()
	t, err := phpx.ParseDate(*v, loc, time.Now())
	if err != nil {
		return nil, malformedDate(*v)
	}
	t = t.In(loc)
	return &t, nil
}

// malformedDate ports the DateMalformedStringException thrown by Chronos when a date cannot be parsed.
func malformedDate(v string) error {
	position, char := 0, ""
	if v != "" {
		char = v[:1]
	}
	return &console.PHPError{Class: "DateMalformedStringException",
		Message: fmt.Sprintf("Failed to parse time string (%s) at position %d (%s): The timezone could not be found in the database", v, position, char),
		File:    "Chronos.php", Line: 242}
}

// dateRange ports buildDateRange(normalizeOptionalDate(start), normalizeOptionalDate(end)).
func dateRange(start, end *string) (core.DateRange, error) {
	s, err := normalizeOptionalDate(start)
	if err != nil {
		return core.DateRange{}, err
	}
	e, err := normalizeOptionalDate(end)
	if err != nil {
		return core.DateRange{}, err
	}
	return core.DateRange{Start: s, End: e}, nil
}

// enumOption ports BackedEnumValueResolver for a string option: nil when not provided, an InvalidOptionException
// when the value is not one of the cases.
func enumOption(in *console.Input, name string, cases []string) (*string, error) {
	v := in.OptionString(name)
	if v == nil {
		return nil, nil
	}
	for _, c := range cases {
		if c == *v {
			return v, nil
		}
	}
	return nil, console.NewConsoleError(`Symfony\Component\Console\Exception\InvalidOptionException`,
		fmt.Sprintf(`The value "%s" is not valid for the "%s" option. Supported values are "%s".`, *v, name, strings.Join(cases, `", "`)),
		"InvalidOptionException.php", 32)
}

func strPtr(s string) *string { return &s }

func deref(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

// phpTraceArg renders a string argument the way PHP stack traces do: quoted and cut to 15 bytes.
func phpTraceArg(s string) string {
	if len(s) > 15 {
		return "'" + s[:15] + "...'"
	}
	return "'" + s + "'"
}
