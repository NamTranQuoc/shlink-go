package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/crawler"
	"github.com/NamTranQuoc/shlink-go/internal/importer"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

func init() {
	register("short-url:import", Body{
		Interact: askImportSource,
		Execute:  importShortURLs,
	})
}

// askImportSource ports the #[Ask] interaction of the backed enum source argument: an invalid answer fails right away.
func askImportSource(_ *Env, io *console.IO) error {
	if v := io.Input.ArgumentString("source"); v != nil {
		return nil
	}
	q := console.NewQuestion("What is the source you want to import from", nil)
	q.StrictNormalizer = func(answer any) (any, error) {
		s, _ := answer.(string)
		if err := validateImportSource(s); err != nil {
			return nil, err
		}
		return s, nil
	}
	answer, err := io.Style().AskQuestion(q)
	if err != nil || answer == nil {
		return err
	}
	io.Input.SetArgument("source", answer)
	return nil
}

// validateImportSource ports BackedEnumValueResolver for the source argument.
func validateImportSource(raw string) error {
	for _, s := range importer.Sources {
		if s == raw {
			return nil
		}
	}
	return console.NewConsoleError(`Symfony\Component\Console\Exception\InvalidArgumentException`,
		fmt.Sprintf(`The value "%s" is not valid for the "source" argument. Supported values are "%s".`, raw,
			strings.Join(importer.Sources, `", "`)), "InvalidArgumentException.php", 30)
}

// importParam is one entry of a ParamsConsoleHelperInterface::requestParams() map: a key and the question asking it.
type importParam struct {
	key string
	ask func(io *console.SymfonyStyle) (any, error)
}

func askParam(question string) func(*console.SymfonyStyle) (any, error) {
	return func(io *console.SymfonyStyle) (any, error) { return io.Ask(question, nil, nil) }
}

func confirmParam(question string, def bool) func(*console.SymfonyStyle) (any, error) {
	return func(io *console.SymfonyStyle) (any, error) { return io.Confirm(question, def) }
}

// importParams ports the params console helpers of every source.
func importParams(source importer.Source) []importParam {
	switch source {
	case importer.Bitly:
		return []importParam{
			{"access_token", askParam("What is your Bit.ly's API access token? (you can generate one here https://bitly.is/accesstoken)")},
			{"import_short_codes", confirmParam("Do you want to import short-codes/slugs as they are? Otherwise, new unique "+
				"short-codes will be generated for every imported URL.", true)},
			{"import_tags", confirmParam("Do you want to import tags?", true)},
			{"import_custom_domains", confirmParam("Do you want to import custom domains? (any domain other than bit.ly)", false)},
			{"keep_creation_date", confirmParam("Do you want to keep the original creation date? Otherwise, all imported URLs "+
				"will have current date as its creation date", true)},
			{"ignore_archived", confirmParam("Do you want to ignore archived URLs?", false)},
			{"continue_token", askParam(`If you already run this command once and a warning was displayed, you might have ` +
				`been provided with a "continue token". If that's the case, paste it here. If this is the first time you ` +
				`run this command, ignore this`)},
		}
	case importer.Yourls:
		return []importParam{
			{"base_url", askParam("What is your YOURLS instance base URL?")},
			{"username", askParam("What is your YOURLS instance username?")},
			{"password", askParam("What is your YOURLS instance password?")},
			{"import_visits", confirmParam("Do you want to import each short URL's visits too?", true)},
			{"domain", askParam("To what domain do you want the URLs to be linked? (leave empty to link them to default domain)")},
		}
	case importer.CSV:
		return []importParam{
			{"stream", func(io *console.SymfonyStyle) (any, error) {
				return io.Ask("What's the path for the CSV file you want to import", nil, csvPathToStream)
			}},
			{"delimiter", func(io *console.SymfonyStyle) (any, error) {
				return io.AskQuestion(console.NewAssocChoiceQuestion("What's the delimiter used to separate values?",
					[]string{",", ";"}, []string{"Comma", "Semicolon"}, ","))
			}},
		}
	case importer.Shlink:
		return []importParam{
			{"base_url", askParam("What is your Shlink instance base URL?")},
			{"api_key", askParam("What is your Shlink instance API key?")},
			{"import_visits", confirmParam("Do you want to import each short URL's visits too?", true)},
			{"import_orphan_visits", confirmParam("Do you want to import orphan visits too?", true)},
		}
	case importer.Kutt:
		return []importParam{
			{"base_url", askParam("What is your Kutt.it instance base URL?")},
			{"api_key", askParam("What is your Kutt.it instance API key?")},
			{"import_all_urls", confirmParam("Do you want to import URLs created anonymously?", false)},
		}
	}
	return nil
}

// unseekableStream is a stream league/csv cannot read, like php://stdin.
type unseekableStream struct{ io.Reader }

// csvPathToStream ports CsvParamsConsoleHelper::pathToStream().
func csvPathToStream(answer any) (any, error) {
	value, _ := answer.(string)
	value = strings.Trim(value, " \t\n\r\x00\x0B")
	if value == "" {
		return nil, &console.PHPError{Class: `Shlinkio\Shlink\Importer\Sources\Csv\InvalidPathException`,
			Message: "The path of the file is required.", File: "InvalidPathException.php", Line: 16}
	}
	if value == "php://stdin" {
		return unseekableStream{}, nil
	}
	f, err := os.Open(value)
	if err == nil {
		if info, statErr := f.Stat(); statErr == nil && !info.IsDir() {
			return f, nil
		}
		f.Close()
	}
	return nil, &console.PHPError{Class: `Shlinkio\Shlink\Importer\Sources\Csv\InvalidPathException`,
		Message: fmt.Sprintf(`The file "%s" does not seem to exist. Try another one.`, value),
		File:    "InvalidPathException.php", Line: 21}
}

// requestImportParams ports ImportSource::toParamsWithCallableMap(): the import_* params are asked first, then the
// rest in the helper's order.
func requestImportParams(io *console.SymfonyStyle, source importer.Source) (importer.Params, error) {
	p := importer.Params{Source: source, ImportShortCodes: true, Extra: map[string]any{}}
	params := importParams(source)
	for _, key := range []string{"import_short_codes", "import_visits", "import_orphan_visits"} {
		for _, param := range params {
			if param.key != key {
				continue
			}
			v, err := param.ask(io)
			if err != nil {
				return p, err
			}
			b, _ := v.(bool)
			switch key {
			case "import_short_codes":
				p.ImportShortCodes = b
			case "import_visits":
				p.ImportVisits = b
			case "import_orphan_visits":
				p.ImportOrphanVisits = b
			}
		}
	}
	for _, param := range params {
		if param.key == "import_short_codes" || param.key == "import_visits" || param.key == "import_orphan_visits" {
			continue
		}
		v, err := param.ask(io)
		if err != nil {
			return p, err
		}
		p.Extra[param.key] = v
	}
	return p, nil
}

func importShortURLs(env *Env, cio *console.IO) (int, error) {
	style := cio.Style()
	raw := deref(cio.Input.ArgumentString("source"), "")
	if err := validateImportSource(raw); err != nil {
		return 0, err
	}
	source := importer.Source(raw)
	params, err := requestImportParams(style, source)
	if err != nil {
		return 0, err
	}
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	loc := s.Store.Loc()
	result, err := importer.New(source, importer.NewHTTPClient(), time.Now, loc).Import(params)
	if errors.Is(err, importer.BitlyMissingTokenError) {
		return 0, bitlyMissingTokenFatal(params)
	}
	if err != nil {
		return 0, err
	}
	err = processImport(env, s, style, result, params)
	var ie *importer.Error
	if errors.As(err, &ie) {
		if ie.ContinueToken == nil {
			style.Error("An error occurred while importing URLs.")
		} else {
			style.Warning(fmt.Sprintf(`Not all URLs were properly imported. Wait a few minutes, and then try executing this `+
				`command again, providing "%s" when the "continue token" is requested. That will ensure already processed `+
				`URLs are skipped.`, *ie.ContinueToken))
		}
		return console.Failure, nil
	}
	if err != nil {
		return 0, importFailure(err)
	}
	return console.Success, nil
}

// bitlyMissingTokenFatal ports the TypeError BitlyApiParams' constructor throws without an access token.
func bitlyMissingTokenFatal(p importer.Params) error {
	const params = "/etc/shlink/vendor/shlinkio/shlink-importer/src/Sources/Bitly/BitlyApiParams.php"
	arg := func(key string, def bool) string {
		v, ok := p.Extra[key].(bool)
		if !ok {
			v = def
		}
		if v {
			return "true"
		}
		return "false"
	}
	token := "NULL"
	if t, ok := p.Extra["continue_token"].(string); ok {
		token = phpTraceArg(t)
	}
	return &console.PHPFatalError{Output: "\nFatal error: Uncaught TypeError: Shlinkio\\Shlink\\Importer\\Sources\\Bitly\\BitlyApiParams::__construct(): " +
		"Argument #1 ($accessToken) must be of type string, null given, called in " + params + " on line 22 and defined in " +
		params + ":12\nStack trace:\n" +
		"#0 " + params + "(22): Shlinkio\\Shlink\\Importer\\Sources\\Bitly\\BitlyApiParams->__construct(NULL, " +
		arg("import_tags", true) + ", " + arg("import_custom_domains", false) + ", " + arg("keep_creation_date", true) + ", " +
		arg("ignore_archived", false) + ", " + token + ")\n" +
		"#1 /etc/shlink/vendor/shlinkio/shlink-importer/src/Sources/Bitly/BitlyApiImporter.php(37): Shlinkio\\Shlink\\Importer\\Sources\\Bitly\\BitlyApiParams::fromImportParams(Object(Shlinkio\\Shlink\\Importer\\Params\\ImportParams))\n" +
		"#2 /etc/shlink/vendor/shlinkio/shlink-importer/src/Command/ImportCommand.php(47): Shlinkio\\Shlink\\Importer\\Sources\\Bitly\\BitlyApiImporter->import(Object(Shlinkio\\Shlink\\Importer\\Params\\ImportParams))\n" +
		"#3 [internal function]: Shlinkio\\Shlink\\Importer\\Command\\ImportCommand->__invoke(Object(Symfony\\Component\\Console\\Style\\SymfonyStyle), Shlinkio\\Shlink\\Importer\\Sources\\ImportSource::BITLY)\n" +
		"#4 /etc/shlink/vendor/symfony/console/Command/InvokableCommand.php(65): ReflectionFunction->invoke(Object(Symfony\\Component\\Console\\Style\\SymfonyStyle), Shlinkio\\Shlink\\Importer\\Sources\\ImportSource::BITLY)\n" +
		invokableTrace(5, "Shlinkio\\Shlink\\Importer\\Command\\ImportCommand") +
		"  thrown in " + params + " on line 12\n"}
}

// csvEmptyPathFatal ports the ValueError league/csv's Reader throws when the CSV importer gets no stream.
func csvEmptyPathFatal() error {
	const stream = "/etc/shlink/vendor/league/csv/src/Stream.php"
	return &console.PHPFatalError{Output: "\nFatal error: Uncaught ValueError: Path must not be empty in " + stream + ":138\n" +
		"Stack trace:\n" +
		"#0 " + stream + "(138): fopen('', 'r', false, NULL)\n" +
		"#1 /etc/shlink/vendor/league/csv/src/AbstractCsv.php(108): League\\Csv\\Stream::from('', 'r', NULL)\n" +
		"#2 /etc/shlink/vendor/league/csv/src/Reader.php(55): League\\Csv\\AbstractCsv::from('', 'r', NULL)\n" +
		"#3 " + csvImporterFile + "(47): League\\Csv\\Reader::from('')\n" +
		csvImportTrace(4) +
		"  thrown in " + stream + " on line 138\n"}
}

const (
	csvImporterFile       = "/etc/shlink/vendor/shlinkio/shlink-importer/src/Sources/Csv/CsvImporter.php"
	importedShlinkURLFile = "/etc/shlink/vendor/shlinkio/shlink-importer/src/Model/ImportedShlinkUrl.php"
)

// csvMissingLongURLFatal ports the TypeError ImportedShlinkUrl's constructor throws for a record without long URL,
// preceded by PHP's warning when the column does not exist.
func csvMissingLongURLFatal(e *importer.CSVMissingLongURLError) error {
	arg := func(s *string) string {
		if s == nil {
			return "NULL"
		}
		return phpTraceArg(*s)
	}
	warning := ""
	if e.KeyMissing {
		warning = "\nWarning: Undefined array key \"longurl\" in " + csvImporterFile + " on line 59\n"
	}
	return &console.PHPFatalError{Output: warning + "\nFatal error: Uncaught TypeError: " +
		"Shlinkio\\Shlink\\Importer\\Model\\ImportedShlinkUrl::__construct(): Argument #2 ($longUrl) must be of type string, " +
		"null given, called in " + csvImporterFile + " on line 57 and defined in " + importedShlinkURLFile + ":18\n" +
		"Stack trace:\n" +
		"#0 " + csvImporterFile + "(57): Shlinkio\\Shlink\\Importer\\Model\\ImportedShlinkUrl->__construct(" +
		"Shlinkio\\Shlink\\Importer\\Sources\\ImportSource::CSV, NULL, Array, Object(DateTimeImmutable), " + arg(e.Domain) +
		", " + phpTraceArg(e.ShortCode) + ", " + arg(e.Title) + ")\n" +
		csvImportTrace(1) +
		"  thrown in " + importedShlinkURLFile + " on line 18\n"}
}

// csvImportTrace renders the frames of a PHP stack trace from the CSV importer's generator up to bin/cli, starting at
// frame n.
func csvImportTrace(n int) string {
	const processor = "/etc/shlink/module/Core/src/Importer/ImportedLinksProcessor.php"
	frames := []string{
		"/etc/shlink/module/Core/src/Util/DoctrineBatchHelper.php(29): Shlinkio\\Shlink\\Importer\\Sources\\Csv\\CsvImporter->importShortUrls(Object(Shlinkio\\Shlink\\Importer\\Sources\\Csv\\CsvParams))",
		processor + "(60): Shlinkio\\Shlink\\Core\\Util\\DoctrineBatchHelper->wrapIterable(Object(Generator), 100)",
		processor + "(42): Shlinkio\\Shlink\\Core\\Importer\\ImportedLinksProcessor->importShortUrls(Object(Symfony\\Component\\Console\\Style\\SymfonyStyle), Object(Generator), Object(Shlinkio\\Shlink\\Importer\\Params\\ImportParams))",
		"/etc/shlink/vendor/shlinkio/shlink-importer/src/Command/ImportCommand.php(48): Shlinkio\\Shlink\\Core\\Importer\\ImportedLinksProcessor->process(Object(Symfony\\Component\\Console\\Style\\SymfonyStyle), Object(Shlinkio\\Shlink\\Importer\\Model\\ImportResult), Object(Shlinkio\\Shlink\\Importer\\Params\\ImportParams))",
		"[internal function]: Shlinkio\\Shlink\\Importer\\Command\\ImportCommand->__invoke(Object(Symfony\\Component\\Console\\Style\\SymfonyStyle), Shlinkio\\Shlink\\Importer\\Sources\\ImportSource::CSV)",
		"/etc/shlink/vendor/symfony/console/Command/InvokableCommand.php(65): ReflectionFunction->invoke(Object(Symfony\\Component\\Console\\Style\\SymfonyStyle), Shlinkio\\Shlink\\Importer\\Sources\\ImportSource::CSV)",
	}
	var b strings.Builder
	for i, f := range frames {
		fmt.Fprintf(&b, "#%d %s\n", n+i, f)
	}
	return b.String() + invokableTrace(n+len(frames), "Shlinkio\\Shlink\\Importer\\Command\\ImportCommand")
}

// invokableTrace renders the frames of a PHP stack trace from InvokableCommand::__invoke() up to bin/cli, starting
// at frame n.
func invokableTrace(n int, command string) string {
	const argv = "Object(Symfony\\Component\\Console\\Input\\ArgvInput)"
	const output = "Object(Symfony\\Component\\Console\\Output\\ConsoleOutput)"
	frames := []string{
		"/etc/shlink/vendor/symfony/console/Command/Command.php(282): Symfony\\Component\\Console\\Command\\InvokableCommand->__invoke(" + argv + ", " + output + ")",
		"/etc/shlink/vendor/symfony/console/Application.php(1129): Symfony\\Component\\Console\\Command\\Command->run(" + argv + ", " + output + ")",
		"/etc/shlink/vendor/symfony/console/Application.php(380): Symfony\\Component\\Console\\Application->doRunCommand(Object(" + command + "), " + argv + ", " + output + ")",
		"/etc/shlink/vendor/symfony/console/Application.php(219): Symfony\\Component\\Console\\Application->doRun(" + argv + ", " + output + ")",
		"/etc/shlink/bin/cli(10): Symfony\\Component\\Console\\Application->run()",
		"{main}",
	}
	var b strings.Builder
	for i, f := range frames {
		fmt.Fprintf(&b, "#%d %s\n", n+i, f)
	}
	return b.String()
}

// importFailure converts errors that escape the import into the exceptions upstream lets bubble up.
func importFailure(err error) error {
	var ir *importer.InvalidRequestError
	switch {
	case errors.As(err, &ir):
		return &console.PHPError{Class: `Shlinkio\Shlink\Importer\Http\InvalidRequestException`, Message: ir.Error(),
			File: "InvalidRequestException.php", Line: 26}
	case errors.Is(err, errUnseekableStream):
		return &console.PHPError{Class: `League\Csv\UnavailableFeature`, Message: "stream does not support seeking.",
			File: "UnavailableFeature.php", Line: 41}
	case errors.Is(err, importer.ErrCSVEmptyPath):
		return csvEmptyPathFatal()
	}
	var syntax *importer.CSVSyntaxError
	if errors.As(err, &syntax) {
		return &console.PHPError{Class: `League\Csv\SyntaxError`, Message: syntax.Message, File: "SyntaxError.php", Line: syntax.Line}
	}
	var missingLongURL *importer.CSVMissingLongURLError
	if errors.As(err, &missingLongURL) {
		return csvMissingLongURLFatal(missingLongURL)
	}
	var invalidEnum *invalidEnumValueError
	if errors.As(err, &invalidEnum) {
		return invalidEnumValueFatal(invalidEnum)
	}
	return err
}

// invalidEnumValueError is the ValueError DeviceType::from() or Browser::from() throws for an imported redirect
// condition value they do not know.
type invalidEnumValueError struct {
	typ   core.RedirectConditionType
	value string
	rule  int
}

func (e *invalidEnumValueError) Error() string {
	return fmt.Sprintf(`"%s" is not a valid backing value for enum`, e.value)
}

// invalidEnumValueFatal renders the uncaught ValueError of an invalidEnumValueError.
func invalidEnumValueFatal(e *invalidEnumValueError) error {
	const conditionFile = "/etc/shlink/module/Core/src/RedirectRule/Entity/RedirectCondition.php"
	const importing = "/etc/shlink/module/Core/src/Importer/ShortUrlImporting.php"
	enum, line := `Shlinkio\Shlink\Core\Model\DeviceType`, 114
	if e.typ == core.CondBrowser {
		enum, line = `Shlinkio\Shlink\Core\Model\Browser`, 120
	}
	const processor = "/etc/shlink/module/Core/src/Importer/ImportedLinksProcessor.php"
	const shlinkSource = "Shlinkio\\Shlink\\Importer\\Sources\\ImportSource::SHLINK"
	frames := []string{
		fmt.Sprintf("%s(%d): %s::from(%s)", conditionFile, line, enum, phpTraceArg(e.value)),
		importing + "(83): Shlinkio\\Shlink\\Core\\RedirectRule\\Entity\\RedirectCondition::fromImport(Object(Shlinkio\\Shlink\\Importer\\Model\\ImportedShlinkRedirectCondition))",
		fmt.Sprintf("/etc/shlink/module/Core/functions/array-utils.php(60): Shlinkio\\Shlink\\Core\\Importer\\ShortUrlImporting::{closure:Shlinkio\\Shlink\\Core\\Importer\\ShortUrlImporting::importRedirectRules():78}(Object(Shlinkio\\Shlink\\Importer\\Model\\ImportedShlinkRedirectRule), %d)", e.rule),
		importing + "(76): Shlinkio\\Shlink\\Core\\ArrayUtils\\map(Array, Object(Closure))",
		processor + "(88): Shlinkio\\Shlink\\Core\\Importer\\ShortUrlImporting->importRedirectRules(Array, Object(Shlinkio\\Shlink\\Common\\Doctrine\\ReopeningEntityManager), Object(Shlinkio\\Shlink\\Core\\RedirectRule\\ShortUrlRedirectRuleService))",
		processor + "(42): Shlinkio\\Shlink\\Core\\Importer\\ImportedLinksProcessor->importShortUrls(Object(Symfony\\Component\\Console\\Style\\SymfonyStyle), Object(Generator), Object(Shlinkio\\Shlink\\Importer\\Params\\ImportParams))",
		"/etc/shlink/vendor/shlinkio/shlink-importer/src/Command/ImportCommand.php(48): Shlinkio\\Shlink\\Core\\Importer\\ImportedLinksProcessor->process(Object(Symfony\\Component\\Console\\Style\\SymfonyStyle), Object(Shlinkio\\Shlink\\Importer\\Model\\ImportResult), Object(Shlinkio\\Shlink\\Importer\\Params\\ImportParams))",
		"[internal function]: Shlinkio\\Shlink\\Importer\\Command\\ImportCommand->__invoke(Object(Symfony\\Component\\Console\\Style\\SymfonyStyle), " + shlinkSource + ")",
		"/etc/shlink/vendor/symfony/console/Command/InvokableCommand.php(65): ReflectionFunction->invoke(Object(Symfony\\Component\\Console\\Style\\SymfonyStyle), " + shlinkSource + ")",
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nFatal error: Uncaught ValueError: \"%s\" is not a valid backing value for enum %s in %s:%d\nStack trace:\n",
		e.value, enum, conditionFile, line)
	for i, f := range frames {
		fmt.Fprintf(&b, "#%d %s\n", i, f)
	}
	b.WriteString(invokableTrace(len(frames), "Shlinkio\\Shlink\\Importer\\Command\\ImportCommand"))
	fmt.Fprintf(&b, "  thrown in %s on line %d\n", conditionFile, line)
	return &console.PHPFatalError{Output: b.String()}
}

var errUnseekableStream = errors.New("stream does not support seeking")

// processImport ports ImportedLinksProcessor::process().
func processImport(env *Env, s *Services, io *console.SymfonyStyle, result importer.Result, params importer.Params) error {
	io.Title("Importing short URLs")
	if _, unseekable := params.Extra["stream"].(unseekableStream); unseekable {
		return errUnseekableStream
	}
	err := s.Store.InImportTx(env.Ctx, s.URLs, func(t *core.ImportTx) error {
		return importShortURLsInTx(t, io, s.Store.Loc(), result, params)
	})
	if err != nil {
		return err
	}
	if params.ImportOrphanVisits {
		io.Title("Importing orphan visits")
		err := s.Store.InImportTx(env.Ctx, s.URLs, func(t *core.ImportTx) error {
			return importOrphanVisits(t, io, s.Store.Loc(), result)
		})
		if err != nil {
			return err
		}
	}
	io.Success("Data properly imported!")
	return nil
}

func importShortURLsInTx(t *core.ImportTx, io *console.SymfonyStyle, loc *time.Location, result importer.Result, params importer.Params) error {
	// DoctrineBatchHelper::wrapIterable()'s batch size
	batchSize := 100
	if params.ImportVisits {
		batchSize = 10
	}
	iteration := 0
	for imported, err := range result.ShortURLs {
		if err != nil {
			return err
		}
		if iteration++; iteration > 1 && (iteration-1)%batchSize == 0 {
			t.Flush()
		}
		longURL := imported.LongURL
		su, isNew, err := resolveImportedShortURL(t, io, imported, params.ImportShortCodes)
		var nonUnique errNonUniqueImport
		switch {
		case errors.As(err, &nonUnique):
			io.Text(fmt.Sprintf("%s: <fg=red>Error</>", longURL))
			continue
		case errors.As(err, new(*skipError)):
			io.Text(fmt.Sprintf("%s: <comment>Skipped</comment>. Reason: %s.", longURL, err.Error()))
			continue
		case err != nil:
			return err
		}
		if err := importRedirectRules(t, su, isNew, imported.RedirectRules); err != nil {
			return err
		}
		message, err := importVisits(t, su, isNew, loc, imported)
		if err != nil {
			return err
		}
		io.Text(fmt.Sprintf("%s: %s", longURL, message))
	}
	return nil
}

// errNonUniqueImport ports NonUniqueSlugException::fromImport().
type errNonUniqueImport struct{}

func (errNonUniqueImport) Error() string { return "non-unique slug" }

// skipError is any other failure while resolving an imported short URL, which skips it.
type skipError struct{ cause error }

func (e *skipError) Error() string { return e.cause.Error() }

// resolveImportedShortURL ports ImportedLinksProcessor::resolveShortUrl().
func resolveImportedShortURL(t *core.ImportTx, io *console.SymfonyStyle, imported *importer.ShortURL,
	importShortCodes bool) (*core.ShortURL, bool, error) {
	existing, err := t.FindImportedShortURL(string(imported.Source), imported.ShortCode, imported.Domain)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, false, nil
	}
	su, err := t.NewShortURL(core.ImportedShortURLData{
		Source: string(imported.Source), LongURL: imported.LongURL, Tags: imported.Tags, CreatedAt: imported.CreatedAt,
		Domain: imported.Domain, ShortCode: imported.ShortCode, Title: imported.Title,
		ValidSince: imported.Meta.ValidSince, ValidUntil: imported.Meta.ValidUntil, MaxVisits: imported.Meta.MaxVisits,
	}, importShortCodes)
	if err != nil {
		return nil, false, &skipError{err}
	}
	unique, err := t.EnsureShortCodeUniqueness(su, importShortCodes)
	if err != nil {
		return nil, false, err
	}
	if !unique {
		answer, err := io.Choice(fmt.Sprintf(`Failed to import URL "%s" because its short-code "%s" is already in use. Do you `+
			`want to generate a new one or skip it?`, imported.LongURL, imported.ShortCode),
			[]string{"Generate new short-code", "Skip"}, strPtr("Skip"))
		if err != nil {
			return nil, false, err
		}
		if answer == "Skip" {
			return nil, false, errNonUniqueImport{}
		}
		if _, err := t.EnsureShortCodeUniqueness(su, false); err != nil {
			return nil, false, err
		}
	}
	if err := t.Persist(su); err != nil {
		return nil, false, err
	}
	return su, true, nil
}

// importRedirectRules ports ShortUrlImporting::importRedirectRules().
func importRedirectRules(t *core.ImportTx, su *core.ShortURL, isNew bool, rules []importer.RedirectRule) error {
	if isNew && len(rules) == 0 {
		return nil
	}
	redirectRules := make([]core.RedirectRule, 0, len(rules))
	for i, r := range rules {
		rule := core.RedirectRule{Priority: i + 1, LongURL: r.LongURL, Conditions: []core.RedirectCondition{}}
		for _, c := range r.Conditions {
			cond, ok, err := redirectConditionFromImport(c)
			var invalid *invalidEnumValueError
			if errors.As(err, &invalid) {
				invalid.rule = i
			}
			if err != nil {
				return err
			}
			if ok {
				rule.Conditions = append(rule.Conditions, cond)
			}
		}
		redirectRules = append(redirectRules, rule)
	}
	return t.SetRules(su.ID, redirectRules)
}

// redirectConditionFromImport ports RedirectCondition::fromImport(): unknown types are ignored.
func redirectConditionFromImport(c importer.RedirectCondition) (core.RedirectCondition, bool, error) {
	typ := core.RedirectConditionType(c.Type)
	value := c.MatchValue
	cond := core.RedirectCondition{Type: typ, MatchValue: &value}
	switch typ {
	case core.CondQueryParam:
		key := deref(c.MatchKey, "")
		cond.MatchKey = &key
	case core.CondAnyValueQueryParam, core.CondValuelessQueryParam:
		cond.MatchKey, cond.MatchValue = &value, nil
	case core.CondLanguage, core.CondIPAddress, core.CondGeolocationCountryCode, core.CondGeolocationCityName:
	case core.CondDevice, core.CondBrowser:
		values := core.DeviceTypeValues
		if typ == core.CondBrowser {
			values = core.BrowserValues
		}
		if !contains(values, value) {
			return cond, false, &invalidEnumValueError{typ: typ, value: value}
		}
	case core.CondBeforeDate, core.CondAfterDate:
		t, err := normalizeOptionalDate(&value)
		if err != nil {
			return cond, false, err
		}
		atom := phpx.AtomString(*t)
		cond.MatchValue = &atom
	default:
		return cond, false, nil
	}
	return cond, true, nil
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// importVisits ports ShortUrlImporting::importVisits().
func importVisits(t *core.ImportTx, su *core.ShortURL, isNew bool, loc *time.Location, imported *importer.ShortURL) (string, error) {
	var mostRecent *time.Time
	if !isNew {
		var err error
		if mostRecent, err = t.MostRecentImportedVisitDate(su.ID); err != nil {
			return "", err
		}
	}
	count := 0
	for v, err := range imported.Visits {
		if err != nil {
			return "", err
		}
		date := v.Date.In(loc)
		if mostRecent != nil && !mostRecent.Before(date) {
			continue
		}
		id := su.ID
		if err := t.InsertVisit(importedVisit(v, &id, core.VisitImported, nil, date)); err != nil {
			return "", err
		}
		count++
	}
	switch {
	case count == 0 && isNew:
		return "<info>Imported</info>", nil
	case count == 0:
		return "<comment>Skipped</comment>", nil
	case isNew:
		return fmt.Sprintf("<info>Imported</info> with <info>%d</info> visits", count), nil
	}
	return fmt.Sprintf("<comment>Skipped</comment>. Imported <info>%d</info> visits", count), nil
}

// importedVisit ports Visit::fromImportOrOrphanImport().
func importedVisit(v importer.Visit, shortURLID *int64, typ core.VisitType, visitedURL *string, date time.Time) *core.Visit {
	var location *core.VisitLocation
	if l := v.Location; l != nil {
		vl := core.NewVisitLocation(l.CountryCode, l.CountryName, l.RegionName, l.CityName, l.Latitude, l.Longitude, l.Timezone)
		location = &vl
	}
	return &core.Visit{ShortURLID: shortURLID, Type: typ, UserAgent: v.UserAgent, Referer: v.Referer,
		PotentialBot: crawler.IsCrawler(v.UserAgent), VisitedURL: visitedURL, Location: location, Date: date}
}

// importOrphanVisits ports ImportedLinksProcessor::importOrphanVisits().
func importOrphanVisits(t *core.ImportTx, io *console.SymfonyStyle, loc *time.Location, result importer.Result) error {
	mostRecent, err := t.MostRecentOrphanVisitDate()
	if err != nil {
		return err
	}
	count := 0
	for v, err := range result.OrphanVisits {
		if err != nil {
			return err
		}
		date := v.Date.In(loc)
		if mostRecent != nil && !mostRecent.Before(date) {
			continue
		}
		typ := core.VisitImported
		for _, known := range []core.VisitType{core.VisitValidShortURL, core.VisitImported, core.VisitInvalidShortURL,
			core.VisitBaseURL, core.VisitRegular404} {
			if string(known) == v.Type {
				typ = known
			}
		}
		visitedURL := v.VisitedURL
		if err := t.InsertVisit(importedVisit(v.Visit, nil, typ, &visitedURL, date)); err != nil {
			return err
		}
		count++
	}
	io.Text(fmt.Sprintf("<info>Imported %d</info> orphan visits.", count))
	return nil
}
