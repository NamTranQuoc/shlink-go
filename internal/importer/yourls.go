package importer

import (
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"
)

const (
	yourlsLinksAction  = "shlink-list"
	yourlsVisitsAction = "shlink-link-visits"
	yourlsDateFormat   = "2006-01-02 15:04:05"
)

// yourlsImporter ports YourlsImporter.
type yourlsImporter struct {
	api *apiConsumer
	now func() time.Time
	loc *time.Location
}

type yourlsParams struct {
	baseURL, username, password string
	importVisits                bool
	domain                      *string
}

func (i *yourlsImporter) Import(p Params) (Result, error) {
	params := yourlsParams{
		baseURL: deref(p.extraString("base_url")), username: deref(p.extraString("username")),
		password: deref(p.extraString("password")), importVisits: p.ImportVisits, domain: p.extraString("domain"),
	}
	return Result{ShortURLs: func(yield func(*ShortURL, error) bool) {
		urls, err := i.loadURLs(params)
		if err != nil {
			var ir *InvalidRequestError
			if errors.As(err, &ir) && strings.Contains(ir.Body, `"message":"Unknown or missing \"action\" parameter"`) {
				// YourlsMissingPluginException
				yield(nil, &Error{Message: "The YOURLS instance from where you are trying to import links, does not have the " +
					`"yourls-shlink-import-plugin" installed, or it is not enabled. Go to https://slnk.to/yourls-import ` +
					"and follow the installation instructions, then try to import again.", Cause: err})
				return
			}
			yield(nil, errorFrom(err))
			return
		}
		for _, u := range urls {
			if !yield(u, nil) {
				return
			}
		}
	}, OrphanVisits: noOrphanVisits}, nil
}

func (i *yourlsImporter) loadURLs(p yourlsParams) ([]*ShortURL, error) {
	result, err := i.callAPI(yourlsLinksAction, p, "")
	if err != nil {
		return nil, err
	}
	var out []*ShortURL
	for _, raw := range result {
		u, _ := raw.(map[string]any)
		shortCode := strOr(u["keyword"], "")
		created, err := time.ParseInLocation(yourlsDateFormat, strOr(u["timestamp"], ""), i.loc)
		if err != nil {
			return nil, err
		}
		visits := iter.Seq2[Visit, error](noVisits)
		if p.importVisits {
			visits = i.loadVisits(shortCode, p)
		}
		clicks := 0
		if c := intPtr(u["clicks"]); c != nil {
			clicks = *c
		} else if s, ok := str(u["clicks"]); ok {
			fmt.Sscanf(s, "%d", &clicks)
		}
		out = append(out, &ShortURL{
			Source:      Yourls,
			LongURL:     strOr(u["url"], ""),
			CreatedAt:   created,
			Domain:      p.domain,
			ShortCode:   shortCode,
			Title:       strPtr(u["title"]),
			Visits:      visits,
			VisitsCount: &clicks,
		})
	}
	return out, nil
}

func (i *yourlsImporter) loadVisits(shortCode string, p yourlsParams) iter.Seq2[Visit, error] {
	return func(yield func(Visit, error) bool) {
		result, err := i.callAPI(yourlsVisitsAction, p, shortCode)
		if err != nil {
			yield(Visit{}, err)
			return
		}
		for _, raw := range result {
			v, _ := raw.(map[string]any)
			referer := strOr(v["referrer"], "")
			if referer == "direct" {
				referer = ""
			}
			date := i.now()
			if clickTime, ok := str(v["click_time"]); ok {
				if date, err = time.ParseInLocation(yourlsDateFormat, clickTime, i.loc); err != nil {
					yield(Visit{}, err)
					return
				}
			}
			visit := Visit{
				Referer:   referer,
				UserAgent: strOr(v["user_agent"], ""),
				Date:      date,
				Location:  &Location{CountryCode: strOr(v["country_code"], "")},
			}
			if !yield(visit, nil) {
				return
			}
		}
	}
}

func (i *yourlsImporter) callAPI(action string, p yourlsParams, shortCode string) ([]any, error) {
	query := buildQuery("format", "json", "action", action, "shortCode", shortCode, "username", p.username,
		"password", p.password)
	resp, err := i.api.callAPI(fmt.Sprintf("%s/yourls-api.php?%s", p.baseURL, query))
	if err != nil {
		return nil, err
	}
	return list(resp["result"]), nil
}
