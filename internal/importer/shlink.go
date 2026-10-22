package importer

import (
	"fmt"
	"iter"
	"math"
	"slices"
	"time"
)

const (
	shlinkShortURLsPerPage = 50
	shlinkVisitsPerPage    = 300
)

// shlinkImporter ports ShlinkImporter with ShlinkMapper.
type shlinkImporter struct {
	api *apiConsumer
	now func() time.Time
	loc *time.Location
}

type shlinkParams struct {
	baseURL, apiKey                  string
	importVisits, importOrphanVisits bool
}

func (i *shlinkImporter) Import(p Params) (Result, error) {
	start := i.now()
	params := shlinkParams{
		baseURL: deref(p.extraString("base_url")), apiKey: deref(p.extraString("api_key")),
		importVisits: p.ImportVisits, importOrphanVisits: p.ImportOrphanVisits,
	}
	return Result{
		ShortURLs:    i.importShortURLs(params, start),
		OrphanVisits: i.importOrphanVisits(params, start),
	}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (i *shlinkImporter) headers(p shlinkParams) []header {
	return []header{{"X-Api-Key", p.apiKey}, {"Accept", "application/json"}}
}

func (i *shlinkImporter) importShortURLs(p shlinkParams, start time.Time) iter.Seq2[*ShortURL, error] {
	return func(yield func(*ShortURL, error) bool) {
		for page := 1; ; page++ {
			url := fmt.Sprintf("%s/rest/v3/short-urls?%s", p.baseURL, buildQuery("page", page, "itemsPerPage", shlinkShortURLsPerPage))
			body, err := i.api.callAPI(url, i.headers(p)...)
			if err != nil {
				yield(nil, errorFrom(err))
				return
			}
			urls, err := i.mapURLs(list(get(body, "shortUrls", "data")), p, start)
			if err != nil {
				yield(nil, errorFrom(err))
				return
			}
			for _, u := range urls {
				if !yield(u, nil) {
					return
				}
			}
			pagination, _ := get(body, "shortUrls", "pagination").(map[string]any)
			current, pages := intPtr(pagination["currentPage"]), intPtr(pagination["pagesCount"])
			if current == nil || pages == nil || *current >= *pages {
				return
			}
		}
	}
}

func (i *shlinkImporter) mapURLs(urls []any, p shlinkParams, start time.Time) ([]*ShortURL, error) {
	out := make([]*ShortURL, 0, len(urls))
	for _, raw := range urls {
		u, _ := raw.(map[string]any)
		visitsCount := intPtr(u["visitsCount"])
		if visitsCount == nil {
			visitsCount = intPtr(get(u, "visitsSummary", "total"))
		}
		count := 0
		if visitsCount != nil {
			count = *visitsCount
		}
		expectedPages := int(math.Ceil(float64(count) / shlinkVisitsPerPage))
		shortCode := strOr(u["shortCode"], "")
		domain := strPtr(u["domain"])
		visits := iter.Seq2[Visit, error](noVisits)
		if p.importVisits && expectedPages > 0 {
			visits = i.loadVisits(shortCode, domain, p, expectedPages, start)
		}
		rules, err := i.loadRedirectRules(shortCode, domain, p)
		if err != nil {
			return nil, err
		}
		su, err := i.mapShortURL(u, visits, rules, start)
		if err != nil {
			return nil, err
		}
		su.VisitsCount = visitsCount
		out = append(out, su)
	}
	return out, nil
}

// mapShortURL ports ShlinkMapper::mapShortUrl().
func (i *shlinkImporter) mapShortURL(u map[string]any, visits iter.Seq2[Visit, error], rules []RedirectRule, fallback time.Time) (*ShortURL, error) {
	validSince, err := nullableDateFromAtom(get(u, "meta", "validSince"))
	if err != nil {
		return nil, err
	}
	validUntil, err := nullableDateFromAtom(get(u, "meta", "validUntil"))
	if err != nil {
		return nil, err
	}
	created, err := nullableDateFromAtom(u["dateCreated"])
	if err != nil {
		return nil, err
	}
	if created == nil {
		created = &fallback
	}
	return &ShortURL{
		Source:        Shlink,
		LongURL:       strOr(u["longUrl"], ""),
		Tags:          stringList(u["tags"]),
		CreatedAt:     *created,
		Domain:        strPtr(u["domain"]),
		ShortCode:     strOr(u["shortCode"], ""),
		Title:         strPtr(u["title"]),
		Visits:        visits,
		Meta:          Meta{ValidSince: validSince, ValidUntil: validUntil, MaxVisits: intPtr(get(u, "meta", "maxVisits"))},
		RedirectRules: rules,
	}, nil
}

// loadVisits loads visits from the last page to the first one, each page reversed, so they end up from older to newer.
func (i *shlinkImporter) loadVisits(shortCode string, domain *string, p shlinkParams, pages int, fallback time.Time) iter.Seq2[Visit, error] {
	return func(yield func(Visit, error) bool) {
		for page := pages; page >= 1; page-- {
			url := fmt.Sprintf("%s/rest/v3/short-urls/%s/visits?%s", p.baseURL, shortCode,
				buildQuery("page", page, "itemsPerPage", shlinkVisitsPerPage, "domain", domain))
			body, err := i.api.callAPI(url, i.headers(p)...)
			if err != nil {
				yield(Visit{}, err)
				return
			}
			visits := list(get(body, "visits", "data"))
			for _, raw := range slices.Backward(visits) {
				v, err := mapVisit(raw, fallback)
				if !yield(v, err) || err != nil {
					return
				}
			}
		}
	}
}

// mapVisit ports ShlinkMapper::mapVisit().
func mapVisit(raw any, fallback time.Time) (Visit, error) {
	v, _ := raw.(map[string]any)
	date, err := nullableDateFromAtom(v["date"])
	if err != nil {
		return Visit{}, err
	}
	if date == nil {
		date = &fallback
	}
	return Visit{
		Referer:   strOr(v["referer"], ""),
		UserAgent: strOr(v["userAgent"], ""),
		Date:      *date,
		Location:  mapLocation(v["visitLocation"]),
	}, nil
}

func mapLocation(raw any) *Location {
	l, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return &Location{
		CountryCode: strOr(l["countryCode"], ""),
		CountryName: strOr(l["countryName"], ""),
		RegionName:  strOr(l["regionName"], ""),
		CityName:    strOr(l["cityName"], ""),
		Timezone:    strOr(l["timezone"], ""),
		Latitude:    floatOr(l["latitude"], 0),
		Longitude:   floatOr(l["longitude"], 0),
	}
}

// loadRedirectRules ports ShlinkImporter::loadRedirectRules(): instances not supporting redirect rules answer 404.
func (i *shlinkImporter) loadRedirectRules(shortCode string, domain *string, p shlinkParams) ([]RedirectRule, error) {
	url := fmt.Sprintf("%s/rest/v3/short-urls/%s/redirect-rules?%s", p.baseURL, shortCode, buildQuery("domain", domain))
	body, err := i.api.callAPI(url, i.headers(p)...)
	if err != nil {
		if ir, ok := err.(*InvalidRequestError); ok && ir.StatusCode == 404 {
			return nil, nil
		}
		return nil, err
	}
	var rules []RedirectRule
	for _, raw := range list(body["redirectRules"]) {
		r, _ := raw.(map[string]any)
		rule := RedirectRule{LongURL: strOr(r["longUrl"], "")}
		for _, rawCond := range list(r["conditions"]) {
			c, _ := rawCond.(map[string]any)
			rule.Conditions = append(rule.Conditions, RedirectCondition{
				Type: strOr(c["type"], ""), MatchValue: strOr(c["matchValue"], ""), MatchKey: strPtr(c["matchKey"]),
			})
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func (i *shlinkImporter) importOrphanVisits(p shlinkParams, fallback time.Time) iter.Seq2[OrphanVisit, error] {
	return func(yield func(OrphanVisit, error) bool) {
		if !p.importOrphanVisits {
			return
		}
		body, err := i.api.callAPI(fmt.Sprintf("%s/rest/v3/visits", p.baseURL), i.headers(p)...)
		if err != nil {
			yield(OrphanVisit{}, errorFrom(err))
			return
		}
		visits, _ := body["visits"].(map[string]any)
		count := intPtr(visits["orphanVisitsCount"])
		if count == nil {
			count = intPtr(get(visits, "orphanVisits", "total"))
		}
		total := 0
		if count != nil {
			total = *count
		}
		// Like loadOrphanVisits(), the first request is done even when there are no pages
		for page := int(math.Ceil(float64(total) / shlinkVisitsPerPage)); ; page-- {
			url := fmt.Sprintf("%s/rest/v3/visits/orphan?%s", p.baseURL, buildQuery("page", page, "itemsPerPage", shlinkVisitsPerPage))
			body, err := i.api.callAPI(url, i.headers(p)...)
			if err != nil {
				yield(OrphanVisit{}, errorFrom(err))
				return
			}
			for _, raw := range slices.Backward(list(get(body, "visits", "data"))) {
				v, err := mapVisit(raw, fallback)
				if err != nil {
					yield(OrphanVisit{}, errorFrom(err))
					return
				}
				m, _ := raw.(map[string]any)
				if !yield(OrphanVisit{Visit: v, VisitedURL: strOr(m["visitedUrl"], ""), Type: strOr(m["type"], "")}, nil) {
					return
				}
			}
			if page <= 1 {
				return
			}
		}
	}
}
