package core

import (
	"context"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// ShortURLWithDeps ports ShortUrl\Model\ShortUrlWithDeps: a short URL plus the values its serialization needs.
// VisitsSummary is nil when it has to be computed from the visits table.
type ShortURLWithDeps struct {
	ShortURL         *ShortURL
	VisitsSummary    *VisitsSummary
	HasRedirectRules bool
}

// WithDeps loads what serializing a single short URL needs, like ShortUrl::toArray() does lazily through the ORM:
// the visits summary from the visits table and whether it has redirect rules.
func (s *Store) WithDeps(ctx context.Context, su *ShortURL) (ShortURLWithDeps, error) {
	summary, err := s.CountVisitsSummary(ctx, su.ID)
	if err != nil {
		return ShortURLWithDeps{}, err
	}
	hasRules, err := s.HasRedirectRules(ctx, su.ID)
	if err != nil {
		return ShortURLWithDeps{}, err
	}
	return ShortURLWithDeps{ShortURL: su, VisitsSummary: &summary, HasRedirectRules: hasRules}, nil
}

// ShortURLStringifier ports ShortUrl\Helper\ShortUrlStringifier.
type ShortURLStringifier struct {
	Options  config.URLShortenerOptions
	BasePath string
}

// Stringify returns the public short URL for an identifier.
func (st ShortURLStringifier) Stringify(id ShortURLIdentifier) string {
	domain := st.Options.DefaultDomain
	if id.Domain != nil {
		domain = *id.Domain
	}
	// Uri::withHost() lower-cases the host; the short code is appended afterwards to avoid it being URL-encoded
	u := phpx.URI{Scheme: strings.ToLower(st.Options.Schema), Host: strings.ToLower(domain)}.WithPath(st.BasePath)
	return u.String() + "/" + id.ShortCode
}

// ShortURLDataTransformer ports ShortUrl\Transformer\ShortUrlDataTransformer.
type ShortURLDataTransformer struct {
	Stringifier ShortURLStringifier
}

// Transform serializes a short URL like ShortUrlDataTransformer::transform().
func (t ShortURLDataTransformer) Transform(d ShortURLWithDeps) phpx.Map {
	su := d.ShortURL
	summary := VisitsSummary{}
	if d.VisitsSummary != nil {
		summary = *d.VisitsSummary
	}
	var tags any = su.Tags
	if su.Tags == nil {
		tags = []string{}
	} else if su.TagKeys != nil {
		m := make(phpx.Map, len(su.Tags))
		for i, tag := range su.Tags {
			m[i] = phpx.MapItem{Key: su.TagKeys[i], Value: tag}
		}
		tags = m
	}
	return phpx.Map{
		{Key: "shortUrl", Value: t.Stringifier.Stringify(su.Identifier())},
		{Key: "shortCode", Value: su.ShortCode},
		{Key: "longUrl", Value: su.LongURL},
		{Key: "dateCreated", Value: phpx.AtomString(su.DateCreated)},
		{Key: "tags", Value: tags},
		{Key: "meta", Value: phpx.Map{
			{Key: "validSince", Value: atomOrNil(su.ValidSince)},
			{Key: "validUntil", Value: atomOrNil(su.ValidUntil)},
			{Key: "maxVisits", Value: su.MaxVisits},
		}},
		{Key: "domain", Value: su.Authority()},
		{Key: "title", Value: su.Title},
		{Key: "crawlable", Value: su.Crawlable},
		{Key: "forwardQuery", Value: su.ForwardQuery},
		{Key: "visitsSummary", Value: summary},
		{Key: "hasRedirectRules", Value: d.HasRedirectRules},
	}
}

func atomOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return phpx.AtomString(*t)
}

// EncodePHP serializes like VisitLocation::jsonSerialize().
func (l *VisitLocation) EncodePHP(int) any {
	if l == nil {
		return nil
	}
	return phpx.Map{
		{Key: "countryCode", Value: l.CountryCode},
		{Key: "countryName", Value: l.CountryName},
		{Key: "regionName", Value: l.RegionName},
		{Key: "cityName", Value: l.CityName},
		{Key: "latitude", Value: l.Latitude},
		{Key: "longitude", Value: l.Longitude},
		{Key: "timezone", Value: l.Timezone},
		{Key: "isEmpty", Value: l.IsEmpty},
	}
}

// EncodePHP serializes like Visit::jsonSerialize(); orphan visits also include their type.
func (v *Visit) EncodePHP(int) any {
	var location any
	if v.Location != nil {
		location = v.Location
	}
	m := phpx.Map{
		{Key: "referer", Value: v.Referer},
		{Key: "date", Value: phpx.AtomString(v.Date)},
		{Key: "userAgent", Value: v.UserAgent},
		{Key: "visitLocation", Value: location},
		{Key: "potentialBot", Value: v.PotentialBot},
		{Key: "visitedUrl", Value: v.VisitedURL},
		{Key: "redirectUrl", Value: v.RedirectURL},
	}
	if v.IsOrphan() {
		m = append(m, phpx.MapItem{Key: "type", Value: string(v.Type)})
	}
	return m
}

// VisitsStats ports Visit\Model\VisitsStats.
type VisitsStats struct {
	NonOrphanVisits VisitsSummary
	OrphanVisits    VisitsSummary
}

// EncodePHP serializes like VisitsStats::jsonSerialize().
func (s VisitsStats) EncodePHP(int) any {
	return phpx.Map{{Key: "nonOrphanVisits", Value: s.NonOrphanVisits}, {Key: "orphanVisits", Value: s.OrphanVisits}}
}
