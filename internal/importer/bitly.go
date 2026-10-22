package importer

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// bitlyImporter ports BitlyApiImporter.
type bitlyImporter struct {
	api *apiConsumer
	now func() time.Time
}

// BitlyMissingTokenError ports the TypeError BitlyApiParams' constructor throws when no access token was provided.
var BitlyMissingTokenError = errors.New("missing Bit.ly access token")

type bitlyParams struct {
	accessToken                                                       string
	importTags, importCustomDomains, keepCreationDate, ignoreArchived bool
	continueToken                                                     *string
}

func (i *bitlyImporter) Import(p Params) (Result, error) {
	token := p.extraString("access_token")
	if token == nil {
		return Result{}, BitlyMissingTokenError
	}
	params := bitlyParams{
		accessToken:         *token,
		importTags:          p.extraBool("import_tags", true),
		importCustomDomains: p.extraBool("import_custom_domains", false),
		keepCreationDate:    p.extraBool("keep_creation_date", true),
		ignoreArchived:      p.extraBool("ignore_archived", false),
		continueToken:       p.extraString("continue_token"),
	}
	return Result{ShortURLs: func(yield func(*ShortURL, error) bool) {
		tracker := newBitlyProgressTracker(params, i.now())
		initialGroup := tracker.initialGroup()
		initialGroupFound := initialGroup == nil
		body, err := i.call("/groups", params, tracker)
		if err != nil {
			yield(nil, errorFrom(err))
			return
		}
		for _, raw := range list(body["groups"]) {
			group, _ := raw.(map[string]any)
			groupID := strOr(group["guid"], "")
			initialGroupFound = initialGroupFound || (initialGroup != nil && groupID == *initialGroup)
			if !initialGroupFound {
				continue
			}
			if !i.loadURLsForGroup(groupID, params, tracker, yield) {
				return
			}
		}
	}, OrphanVisits: noOrphanVisits}, nil
}

// loadURLsForGroup yields a group's links, page by page. It returns false when iteration has to stop.
func (i *bitlyImporter) loadURLsForGroup(groupID string, p bitlyParams, tracker *bitlyProgressTracker, yield func(*ShortURL, error) bool) bool {
	archived := "both"
	if p.ignoreArchived {
		archived = "off"
	}
	createdBefore := ""
	if g := tracker.initialGroup(); g != nil && groupID == *g {
		createdBefore = tracker.createdBefore()
	}
	next := ""
	for {
		u := next
		if u == "" {
			u = fmt.Sprintf("/groups/%s/bitlinks?archived=%s&created_before=%s", groupID, archived, createdBefore)
		}
		body, err := i.call(u, p, tracker)
		if err != nil {
			yield(nil, errorFrom(err))
			return false
		}
		tracker.lastProcessedGroup = &groupID
		for _, raw := range list(body["links"]) {
			link, _ := raw.(map[string]any)
			longURL, ok := str(link["long_url"])
			if !ok || longURL == "" || longURL == "0" {
				continue
			}
			createdAt, hasCreatedDate := str(link["created_at"])
			if hasCreatedDate {
				tracker.lastProcessedURLDate = &createdAt
			}
			date := tracker.startDate
			if hasCreatedDate && p.keepCreationDate {
				if date, err = dateFromAtom(createdAt); err != nil {
					yield(nil, errorFrom(err))
					return false
				}
			}
			rawLink := strOr(link["link"], "")
			if customs := list(link["custom_bitlinks"]); len(customs) > 0 && customs[0] != nil {
				rawLink = strOr(customs[0], "")
			}
			var host *string
			var path string
			if parsed, err := url.Parse(rawLink); err == nil {
				if parsed.Host != "" {
					h := parsed.Hostname()
					host = &h
				}
				path = parsed.Path
			}
			var domain *string
			if host != nil && *host != "bit.ly" && p.importCustomDomains {
				domain = host
			}
			var tags []string
			if p.importTags {
				tags = stringList(link["tags"])
			}
			su := &ShortURL{Source: Bitly, LongURL: longURL, Tags: tags, CreatedAt: date, Domain: domain,
				ShortCode: strings.TrimLeft(path, "/"), Title: strPtr(link["title"]), Visits: noVisits}
			if !yield(su, nil) {
				return false
			}
		}
		pagination, _ := body["pagination"].(map[string]any)
		next = strOr(pagination["next"], "")
		if next == "" || next == "0" {
			return true
		}
	}
}

func (i *bitlyImporter) call(u string, p bitlyParams, tracker *bitlyProgressTracker) (map[string]any, error) {
	if !strings.HasPrefix(u, "http") {
		u = "https://api-ssl.bitly.com/v4" + u
	}
	body, err := i.api.callAPI(u, header{"Authorization", "Bearer " + p.accessToken})
	var ir *InvalidRequestError
	if errors.As(err, &ir) {
		token := tracker.generateContinueToken()
		if token == nil {
			token = p.continueToken
		}
		return nil, &Error{Message: fmt.Sprintf(`Request to Bitly API v4 to URL "%s" failed with status code "%d" and body "%s"`,
			ir.URL, ir.StatusCode, ir.Body), ContinueToken: token, Cause: err}
	}
	return body, err
}

// bitlyProgressTracker ports BitlyApiProgressTracker.
type bitlyProgressTracker struct {
	lastProcessedGroup   *string
	lastProcessedURLDate *string
	originalTokenParts   []string
	startDate            time.Time
}

func newBitlyProgressTracker(p bitlyParams, now time.Time) *bitlyProgressTracker {
	t := &bitlyProgressTracker{startDate: now}
	if p.continueToken == nil {
		return t
	}
	decoded, err := base64.StdEncoding.DecodeString(*p.continueToken)
	if err != nil {
		// base64_decode(strict: true) returns false, which explode() reads as ""
		decoded = nil
	}
	t.originalTokenParts = strings.Split(string(decoded), "__")
	return t
}

func (t *bitlyProgressTracker) initialGroup() *string {
	if len(t.originalTokenParts) > 0 {
		return &t.originalTokenParts[0]
	}
	return nil
}

func (t *bitlyProgressTracker) createdBefore() string {
	if len(t.originalTokenParts) > 1 {
		return t.originalTokenParts[1]
	}
	return ""
}

func (t *bitlyProgressTracker) generateContinueToken() *string {
	if t.lastProcessedGroup == nil {
		return nil
	}
	if t.lastProcessedURLDate == nil {
		token := base64.StdEncoding.EncodeToString([]byte(*t.lastProcessedGroup))
		return &token
	}
	d, err := dateFromAtom(*t.lastProcessedURLDate)
	if err != nil {
		return nil
	}
	createdBefore := strconv.FormatInt(d.Add(-time.Second).Unix(), 10)
	token := base64.StdEncoding.EncodeToString([]byte(*t.lastProcessedGroup + "__" + createdBefore))
	return &token
}
