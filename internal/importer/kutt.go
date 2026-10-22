package importer

import (
	"fmt"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

const kuttShortURLsPerPage = 50

// kuttImporter ports KuttImporter.
type kuttImporter struct {
	api *apiConsumer
	now func() time.Time
	loc *time.Location
}

func (i *kuttImporter) Import(p Params) (Result, error) {
	baseURL, apiKey := deref(p.extraString("base_url")), deref(p.extraString("api_key"))
	all := p.extraBool("import_all_urls", false)
	return Result{ShortURLs: func(yield func(*ShortURL, error) bool) {
		for skip := 0; ; skip += kuttShortURLsPerPage {
			allParam := "false"
			if all {
				allParam = "true"
			}
			url := fmt.Sprintf("%s/api/v2/links?%s", baseURL, buildQuery("limit", kuttShortURLsPerPage, "skip", skip, "all", allParam))
			body, err := i.api.callAPI(url, header{"X-Api-Key", apiKey}, header{"Accept", "application/json"})
			if err != nil {
				yield(nil, errorFrom(err))
				return
			}
			data, hasData := body["data"].([]any)
			total := intPtr(body["total"])
			if !hasData || total == nil {
				yield(nil, errorFrom(fmt.Errorf("unexpected Kutt.it response")))
				return
			}
			urls, err := i.mapURLs(data)
			if err != nil {
				yield(nil, errorFrom(err))
				return
			}
			for _, u := range urls {
				if !yield(u, nil) {
					return
				}
			}
			if *total <= skip+kuttShortURLsPerPage {
				return
			}
		}
	}, OrphanVisits: noOrphanVisits}, nil
}

func noOrphanVisits(func(OrphanVisit, error) bool) {}

func noVisits(func(Visit, error) bool) {}

func (i *kuttImporter) mapURLs(urls []any) ([]*ShortURL, error) {
	var out []*ShortURL
	for _, raw := range urls {
		u, _ := raw.(map[string]any)
		created, err := phpx.ParseDate(strOr(u["created_at"], ""), i.loc, i.now())
		if err != nil {
			return nil, err
		}
		var validUntil *time.Time
		if expire, ok := str(u["expire_in"]); ok {
			t, err := phpx.ParseDate(expire, i.loc, i.now())
			if err != nil {
				return nil, err
			}
			validUntil = &t
		}
		target, ok := str(u["target"])
		if !ok {
			return nil, fmt.Errorf("missing target")
		}
		address, ok := str(u["address"])
		if !ok {
			return nil, fmt.Errorf("missing address")
		}
		out = append(out, &ShortURL{
			Source:      Kutt,
			LongURL:     target,
			CreatedAt:   created,
			Domain:      strPtr(u["domain"]),
			ShortCode:   address,
			Title:       strPtr(u["description"]),
			Visits:      noVisits,
			VisitsCount: intPtr(u["visit_count"]),
			Meta:        Meta{ValidUntil: validUntil},
		})
	}
	return out, nil
}
