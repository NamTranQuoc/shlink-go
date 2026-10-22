package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/console"
	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

func init() {
	register("short-url:create", Body{Interact: createShortURLInteract, Execute: createShortURL})
	register("short-url:resolve", Body{
		Interact: func(_ *Env, io *console.IO) error {
			return askArgument(io, "short-code", "A short code was not provided. Which short code do you want to resolve?")
		},
		Execute: resolveShortURL,
	})
	register("short-url:list", Body{Execute: listShortURLs})
	register("short-url:edit", Body{Execute: editShortURL})
	register("short-url:delete", Body{Execute: deleteShortURL})
	register("short-url:visits", Body{
		Interact: func(_ *Env, io *console.IO) error {
			return askArgument(io, "short-code", "Which short code do you want to use?")
		},
		Execute: shortURLVisits,
	})
	register("short-url:visits-delete", Body{Execute: deleteShortURLVisits})
	register("short-url:delete-expired", Body{Execute: deleteExpiredShortURLs})
}

const (
	shortURLDataInputClass     = `Shlinkio\Shlink\CLI\Command\ShortUrl\Input\ShortUrlDataInput`
	shortURLCreationInputClass = `Shlinkio\Shlink\CLI\Command\ShortUrl\Input\ShortUrlCreationInput`
)

// shortURLIdentifier ports ShortUrlIdentifier::fromShortCodeAndDomain() for the short-code argument and domain option.
func shortURLIdentifier(in *console.Input) core.ShortURLIdentifier {
	return core.NewIdentifier(deref(in.ArgumentString("short-code"), ""), in.OptionString("domain"))
}

// shortURLDataInput ports ShortUrlDataInput::toArray(): the options that were provided, then the long URL.
func shortURLDataInput(in *console.Input, longURL *string) (phpx.Map, error) {
	var m phpx.Map
	if tags := in.OptionStrings("tag"); len(tags) > 0 {
		list := make([]any, len(tags))
		for i, t := range tags {
			list[i] = t
		}
		m = append(m, phpx.MapItem{Key: "tags", Value: list})
	}
	for _, o := range [][2]string{{"validSince", "valid-since"}, {"validUntil", "valid-until"}} {
		if v := in.OptionString(o[1]); v != nil {
			m = append(m, phpx.MapItem{Key: o[0], Value: *v})
		}
	}
	maxVisits, err := intOption(in, "max-visits")
	if err != nil {
		return nil, err
	}
	if maxVisits != nil {
		m = append(m, phpx.MapItem{Key: "maxVisits", Value: *maxVisits})
	}
	if v := in.OptionString("title"); v != nil {
		m = append(m, phpx.MapItem{Key: "title", Value: *v})
	}
	for _, o := range [][2]string{{"crawlable", "crawlable"}, {"noForwardQuery", "no-forward-query"}} {
		if v, ok := in.Option(o[1]).(bool); ok {
			m = append(m, phpx.MapItem{Key: o[0], Value: v})
		}
	}
	var long any
	if longURL != nil {
		long = *longURL
	}
	return m.Set("longUrl", long), nil
}

// checkDates parses the given date options like the converters do while mapping: an unparseable date aborts mapping
// with Chronos' exception, before any mapping error is reported.
func checkDates(in *console.Input, options ...string) error {
	for _, o := range options {
		if _, err := normalizeOptionalDate(in.OptionString(o)); err != nil {
			return err
		}
	}
	return nil
}

func createShortURLInteract(_ *Env, io *console.IO) error {
	err := checkMapInputInts(io.Input, `Shlinkio\Shlink\CLI\Command\ShortUrl\CreateShortUrlCommand`, []mapInputInt{
		{option: "max-visits", class: shortURLDataInputClass, property: "maxVisits", typ: "?int", nested: true},
		{option: "short-code-length", class: shortURLCreationInputClass, property: "shortCodeLength", typ: "?int"},
	})
	if err != nil {
		return err
	}
	return askArgument(io, "long-url", "Which URL do you want to shorten?")
}

func createShortURL(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	in := io.Input
	data, err := shortURLDataInput(in, in.ArgumentString("long-url"))
	if err != nil {
		return 0, err
	}
	shortCodeLength, err := intOption(in, "short-code-length")
	if err != nil {
		return 0, err
	}
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	o := s.URLs
	length := o.DefaultShortCodesLength
	if shortCodeLength != nil {
		length = *shortCodeLength
	}
	length = max(config.MinShortCodesLength, length)
	source := data
	for _, item := range []phpx.MapItem{
		{Key: "longUrl", Value: anyString(in.ArgumentString("long-url"))},
		{Key: "domain", Value: anyString(in.OptionString("domain"))},
		{Key: "customSlug", Value: anyString(in.OptionString("custom-slug"))},
		{Key: "shortCodeLength", Value: length},
		{Key: "pathPrefix", Value: anyString(in.OptionString("path-prefix"))},
		{Key: "findIfExists", Value: in.OptionBool("find-if-exists")},
		{Key: "shortUrlMode", Value: phpEnum(o.Mode)},
		{Key: "multiSegmentSlugsEnabled", Value: o.MultiSegmentSlugsEnabled},
	} {
		source = source.Set(item.Key, item.Value)
	}
	if err := checkDates(in, "valid-since", "valid-until"); err != nil {
		return 0, err
	}
	creation, err := core.MapShortURLCreation(valinorInput(source), core.CreationDefaults{
		ShortURLMode: o.Mode, MultiSegmentSlugsEnabled: o.MultiSegmentSlugsEnabled, DefaultShortCodeLength: length,
	}, s.Store.Loc(), s.Store.NowIn())
	if err != nil {
		var ie *core.InternalError
		if errors.As(err, &ie) && strings.HasPrefix(ie.Error(), "URL-reserved characters") {
			// The constructor's MappingError makes Valinor try every other constructor, the last one being ApiKey's
			return 0, &console.PHPError{Class: `CuyZ\Valinor\Mapper\Object\Exception\NoConstructorFound`,
				Message: "No available constructor found for class `Shlinkio\\Shlink\\Rest\\Entity\\ApiKey`.",
				File:    "ConstructorObjectBuilderFactory.php", Line: 55}
		}
		return 0, mapperError(err, `Shlinkio\Shlink\Core\ShortUrl\Model\ShortUrlCreation`, source)
	}
	su, err := s.ShortURLs.Shorten(env.Ctx, creation)
	if err != nil {
		if core.IsKind(err, core.KindNonUniqueSlug) {
			style.Error(err.Error())
			return console.Failure, nil
		}
		return 0, err
	}
	style.WritelnAll([]string{
		fmt.Sprintf("Processed long URL: <info>%s</info>", su.LongURL),
		fmt.Sprintf("Generated short URL: <info>%s</info>", s.Transformer.Stringifier.Stringify(su.Identifier())),
	})
	return console.Success, nil
}

func anyString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func resolveShortURL(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	su, err := s.ShortURLs.ResolveShortURL(env.Ctx, shortURLIdentifier(io.Input), nil)
	if err != nil {
		if core.IsKind(err, core.KindShortURLNotFound) {
			style.Error(err.Error())
			return console.Failure, nil
		}
		return 0, err
	}
	style.Writeln(fmt.Sprintf("Long URL: <info>%s</info>", su.LongURL), console.OutputNormal)
	return console.Success, nil
}

// shortURLsColumn ports one entry of ListShortUrlsCommand's columns map.
type shortURLsColumn struct {
	header string
	value  func(serialized phpx.Map, su *core.ShortURL) (any, error)
}

func listShortURLs(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	in := io.Input
	page, err := intOption(in, "page")
	if err != nil {
		return 0, err
	}
	currentPage := 1
	if page != nil {
		currentPage = *page
	}
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	columns := shortURLsColumns(env, s, in)
	all := in.OptionBool("all")
	for {
		source := shortURLsParamsInput(in, currentPage)
		if err := checkDates(in, "start-date", "end-date"); err != nil {
			return 0, err
		}
		params, err := core.MapShortURLsParams(valinorInput(source), s.Store.Loc(), s.Store.NowIn())
		if err != nil {
			return 0, mapperError(err, `Shlinkio\Shlink\Core\ShortUrl\Model\ShortUrlsParams`, source)
		}
		paginator, err := s.ShortURLs.ListShortURLs(env.Ctx, params, nil)
		if err != nil {
			return 0, err
		}
		if err := renderShortURLsPage(env, s, style, columns, paginator, all); err != nil {
			return 0, err
		}
		currentPage++
		next, err := paginator.HasNextPage(env.Ctx)
		if err != nil {
			return 0, err
		}
		if !next {
			break
		}
		proceed, err := style.Confirm(fmt.Sprintf("Continue with page <options=bold>%d</>?", currentPage), false)
		if err != nil {
			return 0, err
		}
		if !proceed {
			break
		}
	}
	style.NewLine(1)
	style.Success("Short URLs properly listed")
	return console.Success, nil
}

// shortURLsParamsInput ports ShortUrlsParamsInput::toArray().
func shortURLsParamsInput(in *console.Input, page int) phpx.Map {
	all := in.OptionBool("all")
	itemsPerPage := core.DefaultShortURLsPerPage
	if all {
		itemsPerPage = core.AllItems
	}
	tagsMode := func(option string) phpEnum {
		if in.OptionBool(option) {
			return phpEnum(core.TagsModeAll)
		}
		return phpEnum(core.TagsModeAny)
	}
	list := func(option string) []any {
		values := in.OptionStrings(option)
		out := make([]any, len(values))
		for i, v := range values {
			out[i] = v
		}
		return out
	}
	return phpx.Map{
		{Key: "page", Value: page},
		{Key: "all", Value: all},
		{Key: "itemsPerPage", Value: itemsPerPage},
		{Key: "startDate", Value: anyString(in.OptionString("start-date"))},
		{Key: "endDate", Value: anyString(in.OptionString("end-date"))},
		{Key: "searchTerm", Value: anyString(in.OptionString("search-term"))},
		{Key: "domain", Value: anyString(in.OptionString("domain"))},
		{Key: "tags", Value: list("tag")},
		{Key: "tagsAll", Value: in.OptionBool("tags-all")},
		{Key: "tagsMode", Value: tagsMode("tags-all")},
		{Key: "excludeTags", Value: list("exclude-tag")},
		{Key: "excludeTagsAll", Value: in.OptionBool("exclude-tags-all")},
		{Key: "excludeTagsMode", Value: tagsMode("exclude-tags-all")},
		{Key: "excludeMaxVisitsReached", Value: in.OptionBool("exclude-max-visits-reached")},
		{Key: "excludePastValidUntil", Value: in.OptionBool("exclude-past-valid-until")},
		{Key: "orderBy", Value: anyString(in.OptionString("order-by"))},
		{Key: "apiKeyName", Value: anyString(in.OptionString("api-key-name"))},
		{Key: "showTags", Value: in.OptionBool("show-tags")},
		{Key: "showDomain", Value: in.OptionBool("show-domain")},
		{Key: "showApiKey", Value: in.OptionBool("show-api-key")},
	}
}

// shortURLsColumns ports ListShortUrlsCommand::resolveColumnsMap().
func shortURLsColumns(env *Env, s *Services, in *console.Input) []shortURLsColumn {
	pick := func(prop string) func(phpx.Map, *core.ShortURL) (any, error) {
		return func(m phpx.Map, _ *core.ShortURL) (any, error) {
			v, _ := m.Get(prop)
			if p, ok := v.(*string); ok {
				return anyString(p), nil
			}
			return v, nil
		}
	}
	columns := []shortURLsColumn{
		{"Short Code", pick("shortCode")},
		{"Title", pick("title")},
		{"Short URL", pick("shortUrl")},
		{"Long URL", pick("longUrl")},
		{"Date created", pick("dateCreated")},
		{"Visits count", func(m phpx.Map, _ *core.ShortURL) (any, error) {
			v, _ := m.Get("visitsSummary")
			return v.(core.VisitsSummary).Total, nil
		}},
	}
	if in.OptionBool("show-tags") {
		columns = append(columns, shortURLsColumn{"Tags", func(_ phpx.Map, su *core.ShortURL) (any, error) {
			return strings.Join(su.Tags, ", "), nil
		}})
	}
	if in.OptionBool("show-domain") {
		columns = append(columns, shortURLsColumn{"Domain", func(_ phpx.Map, su *core.ShortURL) (any, error) {
			if su.Domain == nil {
				return core.DefaultAuthority, nil
			}
			return su.Domain.Authority, nil
		}})
	}
	if in.OptionBool("show-api-key") {
		names := map[int64]*string{}
		columns = append(columns, shortURLsColumn{"API Key Name", func(_ phpx.Map, su *core.ShortURL) (any, error) {
			if su.AuthorAPIKeyID == nil {
				return nil, nil
			}
			name, ok := names[*su.AuthorAPIKeyID]
			if !ok {
				var n *string
				err := s.DB.QueryRow(env.Ctx, s.DB.DB, "SELECT name FROM api_keys WHERE id = ?", *su.AuthorAPIKeyID).Scan(&n)
				if err != nil {
					return nil, err
				}
				names[*su.AuthorAPIKeyID], name = n, n
			}
			return anyString(name), nil
		}})
	}
	return columns
}

// renderShortURLsPage ports ListShortUrlsCommand::renderPage().
func renderShortURLsPage(env *Env, s *Services, out console.Output, columns []shortURLsColumn,
	paginator *core.Paginator[core.ShortURLWithDeps], all bool) error {
	shortURLs, err := paginator.Results(env.Ctx)
	if err != nil {
		return err
	}
	rows := make([][]any, len(shortURLs))
	for i, su := range shortURLs {
		serialized := s.Transformer.Transform(su)
		row := make([]any, len(columns))
		for j, c := range columns {
			if row[j], err = c.value(serialized, su.ShortURL); err != nil {
				return err
			}
		}
		rows[i] = row
	}
	headers := make([]any, len(columns))
	for i, c := range columns {
		headers[i] = c.header
	}
	footer := ""
	if !all {
		pages, err := paginator.NbPages(env.Ctx)
		if err != nil {
			return err
		}
		footer = fmt.Sprintf("Page %d of %d", paginator.CurrentPage(), pages)
	}
	shlinkTable(out, false, headers, rows, footer, "")
	return nil
}

func editShortURL(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	in := io.Input
	source, err := shortURLDataInput(in, in.OptionString("long-url"))
	if err != nil {
		return 0, err
	}
	id := shortURLIdentifier(in)
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	if err := checkDates(in, "valid-since", "valid-until"); err != nil {
		return 0, err
	}
	edition, err := core.MapShortURLEdition(valinorInput(source), s.Store.Loc(), s.Store.NowIn())
	if err != nil {
		return 0, mapperError(err, `Shlinkio\Shlink\Core\ShortUrl\Model\ShortUrlEdition`, source)
	}
	su, err := s.ShortURLs.UpdateShortURL(env.Ctx, id, edition, nil)
	if err != nil {
		if core.IsKind(err, core.KindShortURLNotFound) {
			style.Error(fmt.Sprintf(`Short URL not found for "%s"`, id))
			if style.IsVerbose() {
				var ex console.Exception
				if errors.As(phpException(err), &ex) {
					io.App.RenderThrowable(ex, style)
				}
			}
			return console.Failure, nil
		}
		return 0, err
	}
	style.Success(fmt.Sprintf(`Short URL "%s" properly edited`, s.Transformer.Stringifier.Stringify(su.Identifier())))
	return console.Success, nil
}

func deleteShortURL(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	id := shortURLIdentifier(io.Input)
	ignoreThreshold := io.Input.OptionBool("ignore-threshold")
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	runDelete := func(ignoreThreshold bool) error {
		if err := s.ShortURLs.DeleteByShortCode(env.Ctx, id, ignoreThreshold, nil); err != nil {
			return err
		}
		style.Success(fmt.Sprintf(`Short URL with short code "%s" successfully deleted.`, id.ShortCode))
		return nil
	}
	err = runDelete(ignoreThreshold)
	switch {
	case err == nil:
		return console.Success, nil
	case core.IsKind(err, core.KindShortURLNotFound):
		style.Error(err.Error())
		return console.Failure, nil
	case core.IsKind(err, core.KindDeleteShortURL):
		style.Writeln(fmt.Sprintf("<bg=yellow>%s</>", err.Error()), console.OutputNormal)
		force, err := style.Confirm("Do you want to delete it anyway?", false)
		if err != nil {
			return 0, err
		}
		if !force {
			style.Warning("Short URL was not deleted.")
			return console.Invalid, nil
		}
		if err := runDelete(true); err != nil {
			return 0, err
		}
		return console.Success, nil
	}
	return 0, err
}

func shortURLVisits(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	input, err := visitsListInput(io.Input)
	if err != nil {
		return 0, err
	}
	id := shortURLIdentifier(io.Input)
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	params, err := input.params()
	if err != nil {
		return 0, err
	}
	paginator, err := s.Visits.VisitsForShortURL(env.Ctx, id, params, nil)
	if err != nil {
		return 0, err
	}
	return console.Success, renderVisits(env, style, input, paginator)
}

func deleteShortURLVisits(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	id := shortURLIdentifier(io.Input)
	return executeWithWarning("You are about to delete all visits for a short URL. This operation cannot be undone", style,
		func() (int, error) {
			s, err := env.Services()
			if err != nil {
				return 0, err
			}
			n, err := s.Visits.DeleteShortURLVisits(env.Ctx, id, nil)
			if err != nil {
				if core.IsKind(err, core.KindShortURLNotFound) {
					style.Warning(fmt.Sprintf(`Short URL not found for "%s"`, id))
					return console.Invalid, nil
				}
				return 0, err
			}
			style.Success(fmt.Sprintf("Successfully deleted %d visits", n))
			return console.Success, nil
		})
}

func deleteExpiredShortURLs(env *Env, io *console.IO) (int, error) {
	style := io.Style()
	conditions := core.ExpiredShortURLsConditions{PastValidUntil: true, MaxVisitsReached: io.Input.OptionBool("evaluate-max-visits")}
	force := io.Input.OptionBool("force") || !io.Input.IsInteractive()
	dryRun := io.Input.OptionBool("dry-run")
	if !force && !dryRun {
		style.Warning(
			"Careful!",
			"You are about to perform a destructive operation that can result in deleted short URLs and visits.",
			"This action cannot be undone. Proceed at your own risk",
		)
		proceed, err := style.Confirm("Continue?", false)
		if err != nil {
			return 0, err
		}
		if !proceed {
			return console.Invalid, nil
		}
	}
	s, err := env.Services()
	if err != nil {
		return 0, err
	}
	if dryRun {
		n, err := s.Store.CountExpiredShortURLs(env.Ctx, conditions)
		if err != nil {
			return 0, err
		}
		style.Success(fmt.Sprintf("There are %d expired short URLs matching provided conditions", n))
		return console.Success, nil
	}
	n, err := s.Store.DeleteExpiredShortURLs(env.Ctx, conditions)
	if err != nil {
		return 0, err
	}
	style.Success(fmt.Sprintf("%d expired short URLs have been deleted", n))
	return console.Success, nil
}
