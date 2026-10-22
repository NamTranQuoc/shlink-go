package core

import (
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/valinor"
)

// ShortURLEdition ports ShortUrl\Model\ShortUrlEdition. The *WasProvided flags replace upstream's NoValue marker.
type ShortURLEdition struct {
	LongURL               *string
	ValidSince            *time.Time
	ValidSinceWasProvided bool
	ValidUntil            *time.Time
	ValidUntilWasProvided bool
	MaxVisits             *int
	MaxVisitsWasProvided  bool
	Tags                  []string
	TagKeys               []string
	Title                 *string
	TitleWasProvided      bool
	TitleWasAutoResolved  bool
	Crawlable             *bool
	ForwardQuery          *bool
}

// MapShortURLEdition maps a payload to ShortURLEdition like upstream's TreeMapper does.
func MapShortURLEdition(in valinor.Input, loc *time.Location, now time.Time) (*ShortURLEdition, error) {
	m := newMapper(in, loc, now)
	e := &ShortURLEdition{}
	e.LongURL = m.optionalString("longUrl", looseURIConverter)
	e.ValidSince, e.ValidSinceWasProvided = m.date("validSince"), in.Has("validSince")
	e.ValidUntil, e.ValidUntilWasProvided = m.date("validUntil"), in.Has("validUntil")
	if m.fatal != nil {
		return nil, m.fatal
	}
	e.MaxVisits, e.MaxVisitsWasProvided = m.optionalInt("maxVisits"), in.Has("maxVisits")
	if tags, keys, ok := m.tags("tags", true); ok {
		e.Tags, e.TagKeys = tags, keys
	}
	e.Title, e.TitleWasProvided = m.optionalString("title", substring512), in.Has("title")
	e.TitleWasAutoResolved = m.boolParam("titleWasAutoResolved", false)
	e.Crawlable = m.optionalBool("crawlable")
	e.ForwardQuery = m.optionalBool("forwardQuery")
	if err := m.result(); err != nil {
		return nil, err
	}
	return e, nil
}
