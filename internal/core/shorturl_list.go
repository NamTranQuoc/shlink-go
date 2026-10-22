package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/db"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
	"github.com/NamTranQuoc/shlink-go/internal/valinor"
)

// TagsMode ports ShortUrl\Model\TagsMode.
type TagsMode string

const (
	TagsModeAny TagsMode = "any"
	TagsModeAll TagsMode = "all"
)

// DefaultShortURLsPerPage is ShortUrlsParams::DEFAULT_ITEMS_PER_PAGE.
const DefaultShortURLsPerPage = 10

// ShortURLsOrderableFields lists ShortUrl\Model\OrderableField values, in upstream's order.
var ShortURLsOrderableFields = []string{"longUrl", "shortCode", "dateCreated", "title", "visits", "nonBotVisits"}

// ShortURLsParams ports ShortUrl\Model\ShortUrlsParams.
type ShortURLsParams struct {
	Page                    int
	ItemsPerPage            int
	SearchTerm              *string
	Tags                    []string
	OrderBy                 Ordering
	DateRange               DateRange
	ExcludeMaxVisitsReached bool
	ExcludePastValidUntil   bool
	TagsMode                TagsMode
	Domain                  *string
	ExcludeTags             []string
	ExcludeTagsMode         TagsMode
	APIKeyName              *string
}

// MapShortURLsParams maps raw params (REST query or CLI input) to ShortURLsParams like upstream's TreeMapper does.
// Unparseable dates fail inside the constructor, after mapping succeeded, and surface as internal errors.
func MapShortURLsParams(in valinor.Input, loc *time.Location, now time.Time) (*ShortURLsParams, error) {
	m := newMapper(in, loc, now)
	p := &ShortURLsParams{TagsMode: TagsModeAny, ExcludeTagsMode: TagsModeAny, Tags: []string{}, ExcludeTags: []string{}}
	p.Page = m.intMin("page", 1, 1)
	p.ItemsPerPage = m.itemsPerPage("itemsPerPage", DefaultShortURLsPerPage)
	p.SearchTerm = m.optionalString("searchTerm", nil)
	if tags, _, ok := m.tags("tags", false); ok {
		p.Tags = tags
	}
	p.OrderBy = m.ordering("orderBy", ShortURLsOrderableFields)
	startDate := m.rawDate("startDate")
	endDate := m.rawDate("endDate")
	p.ExcludeMaxVisitsReached = m.boolParam("excludeMaxVisitsReached", false)
	p.ExcludePastValidUntil = m.boolParam("excludePastValidUntil", false)
	p.TagsMode = m.tagsMode("tagsMode")
	p.Domain = m.optionalString("domain", nil)
	if tags, _, ok := m.tags("excludeTags", false); ok {
		p.ExcludeTags = tags
	}
	p.ExcludeTagsMode = m.tagsMode("excludeTagsMode")
	p.APIKeyName = m.optionalString("apiKeyName", nil)
	if err := m.result(); err != nil {
		return nil, err
	}
	var err error
	if p.DateRange.Start, err = m.normalizeDate(startDate); err != nil {
		return nil, err
	}
	if p.DateRange.End, err = m.normalizeDate(endDate); err != nil {
		return nil, err
	}
	return p, nil
}

// itemsPerPage maps a `-1|positive-int` param with a default.
func (m *mapper) itemsPerPage(name string, def int) int {
	v, ok := m.in[name]
	if !ok {
		return def
	}
	i, valid := valinor.ToInt(v)
	if !valid || (i != AllItems && i < 1) {
		m.errs.Add(name, "invalid int")
		return def
	}
	return i
}

// ordering ports OrderingConverter: a `string|null` parsed as "field-DIR".
func (m *mapper) ordering(name string, validFields []string) Ordering {
	v, ok := m.in[name]
	if !ok || v == nil {
		return Ordering{Direction: "ASC"}
	}
	s, valid := valinor.ToString(v)
	if !valid {
		m.errs.Add(name, "invalid string")
		return Ordering{Direction: "ASC"}
	}
	if s == "" {
		return Ordering{Direction: "ASC"}
	}
	o := OrderingFromOptionalString(&s)
	fieldOK := false
	for _, f := range validFields {
		if f == *o.Field {
			fieldOK = true
		}
	}
	if !fieldOK {
		m.errs.Add(name, fmt.Sprintf(`Resolved order field is not one of ["%s"]`, strings.Join(validFields, `", "`)))
		return Ordering{Direction: "ASC"}
	}
	if strings.Contains(s, "-") && o.Direction != "ASC" && o.Direction != "DESC" {
		m.errs.Add(name, `Resolved order direction has to be one of ["ASC", "DESC"]`)
		return Ordering{Direction: "ASC"}
	}
	return o
}

// tagsMode maps a TagsMode enum param.
func (m *mapper) tagsMode(name string) TagsMode {
	v, ok := m.in[name]
	if !ok {
		return TagsModeAny
	}
	s, valid := valinor.ToString(v)
	if !valid || (s != string(TagsModeAny) && s != string(TagsModeAll)) {
		m.errs.Add(name, "invalid enum value")
		return TagsModeAny
	}
	return TagsMode(s)
}

// rawDate maps a `DateTimeInterface|string|null` param, deferring the parsing to normalizeDate.
func (m *mapper) rawDate(name string) any {
	v, ok := m.in[name]
	if !ok || v == nil {
		return nil
	}
	if _, isString := valinor.ToString(v); !isString {
		m.errs.Add(name, "invalid date")
		return nil
	}
	return v
}

// normalizeDate ports normalizeOptionalDate() for a value accepted by rawDate.
func (m *mapper) normalizeDate(v any) (*time.Time, error) {
	if v == nil {
		return nil, nil
	}
	t, err := m.parseDateValue(v)
	if err != nil {
		if errors.Is(err, phpx.ErrMalformedDate) {
			return nil, internal(err)
		}
		return nil, err
	}
	return &t, nil
}

// phpEmptyString tells if a string is "empty" for PHP's empty().
func phpEmptyString(s *string) bool { return s == nil || *s == "" || *s == "0" }

// shortURLsFilter ports ShortUrlsCountFiltering / ShortUrlsListFiltering.
type shortURLsFilter struct {
	p                           *ShortURLsParams
	apiKey                      *APIKey
	apiKeyName                  *string
	searchIncludesDefaultDomain bool
}

func newShortURLsFilter(p *ShortURLsParams, k *APIKey, defaultDomain string) shortURLsFilter {
	f := shortURLsFilter{p: p, apiKey: k}
	f.searchIncludesDefaultDomain = !phpEmptyString(p.SearchTerm) && defaultDomain != "" && defaultDomain != "0" &&
		strings.Contains(asciiLower(defaultDomain), asciiLower(*p.SearchTerm))
	// Filtering by API key name is only allowed if the API key used in the request is an admin one, or it matches
	// the API key name
	if p.APIKeyName != nil && ((k != nil && k.Name == *p.APIKeyName) || IsAdmin(k)) {
		f.apiKeyName = p.APIKeyName
	}
	return f
}

// shortURLHasTags returns an EXISTS condition matching short URLs (alias s) linked to a tag satisfying extra.
func shortURLHasTags(extra string) string {
	return "EXISTS (SELECT 1 FROM short_urls_in_tags lst INNER JOIN tags lt ON lt.id = lst.tag_id WHERE lst.short_url_id = s.id AND " +
		extra + ")"
}

// tagsCondition matches short URLs having any (or all) of the tags.
func tagsCondition(tags []string, mode TagsMode) (string, []any) {
	args := make([]any, len(tags))
	for i, t := range tags {
		args[i] = t
	}
	if mode == TagsModeAll {
		conds := make([]string, len(tags))
		for i := range tags {
			conds[i] = shortURLHasTags("lt.name = ?")
		}
		return "(" + strings.Join(conds, " AND ") + ")", args
	}
	return shortURLHasTags("lt.name IN (" + placeholders(len(tags)) + ")"), args
}

// conditions ports ShortUrlListRepository::createListQueryBuilder(). Joins with tags are expressed as EXISTS
// sub-queries, which select the same short URLs as upstream's joins followed by DISTINCT.
func (s *Store) shortURLsListConditions(f shortURLsFilter) (joins string, conds []string, args []any) {
	p := f.p
	if p.DateRange.Start != nil {
		conds = append(conds, "s.date_created >= ?")
		args = append(args, s.DB.FormatTime(*p.DateRange.Start))
	}
	if p.DateRange.End != nil {
		conds = append(conds, "s.date_created <= ?")
		args = append(args, s.DB.FormatTime(*p.DateRange.End))
	}
	if !phpEmptyString(p.SearchTerm) {
		pattern := "%" + *p.SearchTerm + "%"
		search := []string{"s.original_url LIKE ?", "s.short_code LIKE ?", "s.title LIKE ?", "d.authority LIKE ?"}
		searchArgs := []any{pattern, pattern, pattern, pattern}
		// Include default domain in search if included, and a domain was not explicitly provided
		if f.searchIncludesDefaultDomain && p.Domain == nil {
			search = append(search, "s.domain_id IS NULL")
		}
		// Apply tag conditions, only when not filtering by all provided tags. When filtering by any tag, the tag
		// matching the search term has to be one of those tags, as upstream reuses the same join.
		if len(p.Tags) == 0 {
			search = append(search, shortURLHasTags("lt.name LIKE ?"))
			searchArgs = append(searchArgs, pattern)
		} else if p.TagsMode == TagsModeAny {
			search = append(search, shortURLHasTags("lt.name IN ("+placeholders(len(p.Tags))+") AND lt.name LIKE ?"))
			for _, t := range p.Tags {
				searchArgs = append(searchArgs, t)
			}
			searchArgs = append(searchArgs, pattern)
		}
		conds = append(conds, "("+strings.Join(search, " OR ")+")")
		args = append(args, searchArgs...)
	}
	if len(p.Tags) > 0 {
		c, a := tagsCondition(p.Tags, p.TagsMode)
		conds = append(conds, c)
		args = append(args, a...)
	}
	if len(p.ExcludeTags) > 0 {
		c, a := tagsCondition(p.ExcludeTags, p.ExcludeTagsMode)
		conds = append(conds, "NOT "+c)
		args = append(args, a...)
	}
	if p.Domain != nil && *p.Domain == DefaultAuthority {
		conds = append(conds, "s.domain_id IS NULL")
	} else if p.Domain != nil {
		conds = append(conds, "d.authority = ?")
		args = append(args, *p.Domain)
	}
	if p.ExcludeMaxVisitsReached {
		conds = append(conds, "(s.max_visits IS NULL OR s.max_visits > "+
			"(SELECT COALESCE(SUM(vc.count), 0) FROM short_url_visits_counts vc WHERE vc.short_url_id = s.id))")
	}
	if p.ExcludePastValidUntil {
		conds = append(conds, "(s.valid_until IS NULL OR s.valid_until >= ?)")
		args = append(args, phpx.DateTimeString(s.NowIn()))
	}
	if f.apiKeyName != nil {
		joins = " INNER JOIN api_keys a ON a.id = s.author_api_key_id"
		conds = append(conds, "a.name = ?")
		args = append(args, *f.apiKeyName)
	}
	keyConds, keyArgs := apiKeyShortURLConditions(f.apiKey, "s")
	return joins, append(conds, keyConds...), append(args, keyArgs...)
}

// CountShortURLsList ports ShortUrlListRepository::countList().
func (s *Store) CountShortURLsList(ctx context.Context, p *ShortURLsParams, k *APIKey, defaultDomain string) (int, error) {
	joins, conds, args := s.shortURLsListConditions(newShortURLsFilter(p, k, defaultDomain))
	var n db.Int
	err := s.DB.QueryRow(ctx, s.DB.DB, "SELECT COUNT(s.id)"+shortURLFrom+joins+where(conds), args...).Scan(&n)
	return int(n), err
}

var shortURLsOrderColumns = map[string]string{
	"longUrl": "s.original_url", "shortCode": "s.short_code", "dateCreated": "s.date_created",
	"title": "sclr_title", "visits": "sclr_visits", "nonBotVisits": "sclr_non_bot_visits",
}

// FindShortURLsList ports ShortUrlListRepository::findList(): short URLs with their visits summary (from the visits
// counts table) and redirect rules flag.
func (s *Store) FindShortURLsList(ctx context.Context, p *ShortURLsParams, k *APIKey, defaultDomain string, limit, offset int) ([]ShortURLWithDeps, error) {
	joins, conds, args := s.shortURLsListConditions(newShortURLsFilter(p, k, defaultDomain))
	sort, dir := "s.date_created", "DESC"
	if p.OrderBy.Field != nil {
		var err error
		if dir, err = orderDirection(p.OrderBy); err != nil {
			return nil, err
		}
		col, ok := shortURLsOrderColumns[*p.OrderBy.Field]
		if !ok {
			return nil, fmt.Errorf("invalid order field %q", *p.OrderBy.Field)
		}
		sort = col
	}
	// DISTINCT mirrors upstream's DQL; engines that implement it with a sort break ties by the selected columns
	query := "SELECT DISTINCT " + shortURLColumns + `,
		(SELECT COALESCE(SUM(v.count), 0) FROM short_url_visits_counts v WHERE v.short_url_id = s.id) AS sclr_visits,
		(SELECT COALESCE(SUM(v2.count), 0) FROM short_url_visits_counts v2 WHERE v2.short_url_id = s.id AND v2.potential_bot = ?) AS sclr_non_bot_visits,
		COALESCE(s.title, '') AS sclr_title` + shortURLFrom + joins + where(conds) + " ORDER BY " + sort + " " + dir
	query = s.DB.Limit(query, &limit, &offset)
	rows, err := s.DB.Query(ctx, s.DB.DB, query, append([]any{false}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShortURLWithDeps
	var urls []*ShortURL
	for rows.Next() {
		var visits, nonBots db.Int
		var title sql.NullString
		su, err := s.scanShortURL(scanWithExtra{rows, []any{&visits, &nonBots, &title}})
		if err != nil {
			return nil, err
		}
		summary := VisitsSummary{Total: int(visits), NonBots: int(nonBots)}
		out = append(out, ShortURLWithDeps{ShortURL: su, VisitsSummary: &summary})
		urls = append(urls, su)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := s.loadTags(ctx, s.DB.DB, urls...); err != nil {
		return nil, err
	}
	withRules, err := s.shortURLsWithRedirectRules(ctx, urls)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].HasRedirectRules = withRules[out[i].ShortURL.ID]
	}
	if out == nil {
		out = []ShortURLWithDeps{}
	}
	return out, nil
}

// scanWithExtra appends destinations after the ones scanShortURL passes.
type scanWithExtra struct {
	row   rowScanner
	extra []any
}

func (s scanWithExtra) Scan(dest ...any) error { return s.row.Scan(append(dest, s.extra...)...) }

// shortURLsWithRedirectRules returns the ids of the short URLs that have at least one redirect rule.
func (s *Store) shortURLsWithRedirectRules(ctx context.Context, urls []*ShortURL) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(urls) == 0 {
		return out, nil
	}
	ids := make([]any, len(urls))
	for i, u := range urls {
		ids[i] = u.ID
	}
	rows, err := s.DB.Query(ctx, s.DB.DB, "SELECT DISTINCT short_url_id FROM short_url_redirect_rules WHERE short_url_id IN ("+
		placeholders(len(ids))+")", ids...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ListShortURLs ports ShortUrlListService::listShortUrls(). The count is not cached, like ShortUrlRepositoryAdapter.
func (s *ShortURLService) ListShortURLs(ctx context.Context, p *ShortURLsParams, k *APIKey) (*Paginator[ShortURLWithDeps], error) {
	defaultDomain := s.Options.DefaultDomain
	return NewPaginator[ShortURLWithDeps](ctx, FuncAdapter[ShortURLWithDeps]{
		CountFn: func(ctx context.Context) (int, error) { return s.Store.CountShortURLsList(ctx, p, k, defaultDomain) },
		SliceFn: func(ctx context.Context, offset, length int) ([]ShortURLWithDeps, error) {
			return s.Store.FindShortURLsList(ctx, p, k, defaultDomain, length, offset)
		},
	}, p.ItemsPerPage, p.Page)
}
