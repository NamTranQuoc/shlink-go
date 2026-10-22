// Package importer ports shlinkio/shlink-importer: the sources short URLs can be imported from (Bit.ly, YOURLS, a
// CSV file, another Shlink instance and Kutt.it) and the models they produce. Persisting what they produce is
// ImportedLinksProcessor's job, ported in the cli package.
package importer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// Source ports ImportSource.
type Source string

const (
	Bitly  Source = "bitly"
	Yourls Source = "yourls"
	CSV    Source = "csv"
	Shlink Source = "shlink"
	Kutt   Source = "kutt"
)

// Sources lists ImportSource::values(), in upstream's order.
var Sources = []string{string(Bitly), string(Yourls), string(CSV), string(Shlink), string(Kutt)}

// Params ports ImportParams.
type Params struct {
	Source             Source
	ImportShortCodes   bool
	ImportVisits       bool
	ImportOrphanVisits bool
	Extra              map[string]any
}

// extraString returns a string extra param, or nil when missing or null.
func (p Params) extraString(key string) *string {
	if s, ok := p.Extra[key].(string); ok {
		return &s
	}
	return nil
}

// extraBool returns `(bool) (extra ?? def)`.
func (p Params) extraBool(key string, def bool) bool {
	v, ok := p.Extra[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t != "" && t != "0"
	}
	return true
}

// ShortURL ports ImportedShlinkUrl.
type ShortURL struct {
	Source        Source
	LongURL       string
	Tags          []string
	CreatedAt     time.Time
	Domain        *string
	ShortCode     string
	Title         *string
	Visits        iter.Seq2[Visit, error]
	VisitsCount   *int
	Meta          Meta
	RedirectRules []RedirectRule
}

// Meta ports ImportedShlinkUrlMeta.
type Meta struct {
	ValidSince *time.Time
	ValidUntil *time.Time
	MaxVisits  *int
}

// Visit ports ImportedShlinkVisit.
type Visit struct {
	Referer   string
	UserAgent string
	Date      time.Time
	Location  *Location
}

// OrphanVisit ports ImportedShlinkOrphanVisit.
type OrphanVisit struct {
	Visit
	VisitedURL string
	Type       string
}

// Location ports ImportedShlinkVisitLocation.
type Location struct {
	CountryCode string
	CountryName string
	RegionName  string
	CityName    string
	Timezone    string
	Latitude    float64
	Longitude   float64
}

// RedirectRule ports ImportedShlinkRedirectRule.
type RedirectRule struct {
	LongURL    string
	Conditions []RedirectCondition
}

// RedirectCondition ports ImportedShlinkRedirectCondition.
type RedirectCondition struct {
	Type       string
	MatchValue string
	MatchKey   *string
}

// Result ports ImportResult. Both sequences are lazy: sources only call their APIs while they are iterated.
type Result struct {
	ShortURLs    iter.Seq2[*ShortURL, error]
	OrphanVisits iter.Seq2[OrphanVisit, error]
}

// Error ports ImportException: the import failed, and may be resumed with ContinueToken.
type Error struct {
	Message       string
	ContinueToken *string
	Cause         error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

// errorFrom ports ImportException::fromError(), keeping import errors as they are.
func errorFrom(err error) error {
	var ie *Error
	if errors.As(err, &ie) {
		return err
	}
	return &Error{Message: "An error occurred while importing URLs", Cause: err}
}

// InvalidRequestError ports InvalidRequestException.
type InvalidRequestError struct {
	URL        string
	StatusCode int
	Body       string
}

func (e *InvalidRequestError) Error() string {
	return fmt.Sprintf("Request to %s failed with status code %d", e.URL, e.StatusCode)
}

// Importer imports from one source, like ImporterStrategyInterface.
type Importer interface {
	Import(p Params) (Result, error)
}

// New returns the importer for a source, like ImporterStrategyManager.
func New(source Source, client *http.Client, now func() time.Time, loc *time.Location) Importer {
	api := &apiConsumer{client: client}
	switch source {
	case Bitly:
		return &bitlyImporter{api: api, now: now}
	case Yourls:
		return &yourlsImporter{api: api, now: now, loc: loc}
	case CSV:
		return &csvImporter{now: now}
	case Shlink:
		return &shlinkImporter{api: api, now: now, loc: loc}
	case Kutt:
		return &kuttImporter{api: api, now: now, loc: loc}
	}
	return nil
}

// apiConsumer ports RestApiConsumer.
type apiConsumer struct {
	client *http.Client
}

// header is a request header, kept in the order upstream sets them.
type header struct{ name, value string }

func (a *apiConsumer) callAPI(url string, headers ...header) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for _, h := range headers {
		req.Header.Set(h.name, h.value)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, &InvalidRequestError{URL: url, StatusCode: resp.StatusCode, Body: string(body)}
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return nil, err
	}
	switch t := decoded.(type) {
	case map[string]any:
		return t, nil
	case []any:
		m := make(map[string]any, len(t))
		for i, v := range t {
			m[fmt.Sprint(i)] = v
		}
		return m, nil
	}
	return nil, fmt.Errorf("return value must be of type array, %T returned", decoded)
}

// buildQuery ports http_build_query() for scalar values in order; nil values are left out.
func buildQuery(pairs ...any) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		v := pairs[i+1]
		if v == nil {
			continue
		}
		if p, ok := v.(*string); ok {
			if p == nil {
				continue
			}
			v = *p
		}
		parts = append(parts, phpx.URLEncode(pairs[i].(string))+"="+phpx.URLEncode(fmt.Sprint(v)))
	}
	return strings.Join(parts, "&")
}

// Accessors for decoded JSON, with PHP's `??` semantics: missing and null are the same.

func get(m map[string]any, keys ...string) any {
	var v any = m
	for _, k := range keys {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[k]
	}
	return v
}

func str(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case bool:
		if t {
			return "1", true
		}
		return "", true
	}
	return "", false
}

func strOr(v any, def string) string {
	if s, ok := str(v); ok {
		return s
	}
	return def
}

func strPtr(v any) *string {
	if s, ok := str(v); ok {
		return &s
	}
	return nil
}

func intPtr(v any) *int {
	if n, ok := v.(json.Number); ok {
		if i, err := n.Int64(); err == nil {
			r := int(i)
			return &r
		}
		if f, err := n.Float64(); err == nil {
			r := int(f)
			return &r
		}
	}
	return nil
}

func floatOr(v any, def float64) float64 {
	if n, ok := v.(json.Number); ok {
		if f, err := n.Float64(); err == nil {
			return f
		}
	}
	return def
}

func list(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case map[string]any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			out = append(out, item)
		}
		return out
	}
	return nil
}

func stringList(v any) []string {
	var out []string
	for _, item := range list(v) {
		if s, ok := str(item); ok {
			out = append(out, s)
		}
	}
	return out
}

// dateFromAtom ports DateHelper::dateFromAtom().
func dateFromAtom(s string) (time.Time, error) {
	return time.Parse("2006-01-02T15:04:05Z07:00", s)
}

// nullableDateFromAtom ports DateHelper::nullableDateFromAtom().
func nullableDateFromAtom(v any) (*time.Time, error) {
	s, ok := str(v)
	if !ok {
		return nil, nil
	}
	t, err := dateFromAtom(s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
