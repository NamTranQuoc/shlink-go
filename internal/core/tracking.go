package core

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/crawler"
	"github.com/NamTranQuoc/shlink-go/internal/db"
)

// Max lengths of the visitor fields, from Visit\Model\Visitor.
const (
	userAgentMaxLength     = 512
	refererMaxLength       = 1024
	remoteAddressMaxLength = 256
	visitedURLMaxLength    = 2048
	redirectURLMaxLength   = 2048
)

// Location is a resolved geolocation, ported from shlink-ip-geolocation's Location model.
type Location struct {
	CountryCode string
	CountryName string
	RegionName  string
	City        string
	Latitude    float64
	Longitude   float64
	TimeZone    string
}

// Visitor ports Visit\Model\Visitor.
type Visitor struct {
	UserAgent     string
	Referer       string
	RemoteAddress *string
	VisitedURL    string
	PotentialBot  bool
	Geolocation   *Location
	RedirectURL   *string
}

func cropBytes(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// NewVisitor ports Visitor::fromParams().
func NewVisitor(userAgent, referer string, remoteAddress *string, visitedURL string, geo *Location, redirectURL *string) Visitor {
	v := Visitor{
		UserAgent:    cropBytes(userAgent, userAgentMaxLength),
		Referer:      cropBytes(referer, refererMaxLength),
		VisitedURL:   cropBytes(visitedURL, visitedURLMaxLength),
		PotentialBot: crawler.IsCrawler(userAgent),
		Geolocation:  geo,
	}
	if remoteAddress != nil {
		a := cropBytes(*remoteAddress, remoteAddressMaxLength)
		v.RemoteAddress = &a
	}
	if redirectURL != nil {
		r := cropBytes(*redirectURL, redirectURLMaxLength)
		v.RedirectURL = &r
	}
	return v
}

// normalizeForTrackingOptions ports Visitor::normalizeForTrackingOptions().
func (v Visitor) normalizeForTrackingOptions(o config.TrackingOptions) Visitor {
	if o.DisableUATracking {
		v.UserAgent = ""
	}
	if o.DisableReferrerTracking {
		v.Referer = ""
	}
	if o.DisableIPTracking {
		v.RemoteAddress = nil
	}
	return v
}

// LocalhostIP is shlink-common's IpAddress::LOCALHOST.
const LocalhostIP = "127.0.0.1"

// AnonymizeIP ports Visit::processAddress(): IPv4 addresses lose their last octet; anything that is not 4
// dot-separated parts becomes null.
func AnonymizeIP(address *string, anonymize bool) *string {
	if !anonymize || address == nil || *address == LocalhostIP {
		return address
	}
	trimmed := strings.TrimSpace(*address)
	parts := strings.Split(trimmed, ".")
	if len(parts) != 4 {
		return nil
	}
	out := parts[0] + "." + parts[1] + "." + parts[2] + ".0"
	return &out
}

// VisitsTracker ports Visit\VisitsTracker.
type VisitsTracker struct {
	Store   *Store
	Options config.TrackingOptions
	Events  EventDispatcher
}

// Track records a visit to a short URL.
func (t *VisitsTracker) Track(ctx context.Context, su *ShortURL, v Visitor) (*Visit, error) {
	id := su.ID
	return t.trackVisit(ctx, VisitValidShortURL, &id, v)
}

// TrackOrphan records an orphan visit of given type, if orphan visits tracking is enabled.
func (t *VisitsTracker) TrackOrphan(ctx context.Context, typ VisitType, v Visitor) (*Visit, error) {
	if !t.Options.TrackOrphanVisits {
		return nil, nil
	}
	return t.trackVisit(ctx, typ, nil, v)
}

func (t *VisitsTracker) trackVisit(ctx context.Context, typ VisitType, shortURLID *int64, original Visitor) (*Visit, error) {
	if t.Options.DisableTracking {
		return nil, nil
	}
	v := original.normalizeForTrackingOptions(t.Options)
	visit := &Visit{
		ShortURLID:   shortURLID,
		Type:         typ,
		UserAgent:    v.UserAgent,
		Referer:      v.Referer,
		PotentialBot: v.PotentialBot,
		RemoteAddr:   AnonymizeIP(v.RemoteAddress, t.Options.AnonymizeRemoteAddr),
		VisitedURL:   &v.VisitedURL,
		RedirectURL:  v.RedirectURL,
		Date:         t.Store.NowIn(),
	}
	if v.Geolocation != nil {
		g := v.Geolocation
		loc := NewVisitLocation(g.CountryCode, g.CountryName, g.RegionName, g.City, g.Latitude, g.Longitude, g.TimeZone)
		visit.Location = &loc
	}
	err := t.Store.DB.InTx(ctx, func(tx *sql.Tx) error {
		return t.Store.InsertVisit(ctx, tx, visit)
	})
	if err != nil {
		return nil, err
	}
	if t.Events != nil {
		t.Events.URLVisited(visit.ID, original.RemoteAddress)
	}
	return visit, nil
}

// InsertVisit persists a visit, its location and the matching visits counter, like the ORM flush plus the
// ShortUrlVisitsCountTracker/OrphanVisitsCountTracker listeners do.
func (s *Store) InsertVisit(ctx context.Context, q db.Querier, v *Visit) error {
	var locationID any
	if v.Location != nil {
		id, err := s.insertLocation(ctx, q, v.Location)
		if err != nil {
			return err
		}
		locationID = id
	}
	var shortURLID any
	if v.ShortURLID != nil {
		shortURLID = *v.ShortURLID
	}
	id, err := s.DB.Insert(ctx, q, "visits", []string{
		"referer", "date", "remote_addr", "user_agent", "visited_url", "type", "potential_bot", "redirect_url",
		"short_url_id", "visit_location_id",
	}, v.Referer, s.DB.FormatTime(v.Date), nullable(v.RemoteAddr), v.UserAgent, nullable(v.VisitedURL), string(v.Type),
		v.PotentialBot, nullable(v.RedirectURL), shortURLID, locationID)
	if err != nil {
		return err
	}
	v.ID = id
	if v.ShortURLID != nil {
		return s.incrementVisitsCount(ctx, q, "short_url_visits_counts", v.ShortURLID, v.PotentialBot)
	}
	return s.incrementVisitsCount(ctx, q, "orphan_visits_counts", nil, v.PotentialBot)
}

func (s *Store) insertLocation(ctx context.Context, q db.Querier, l *VisitLocation) (int64, error) {
	id, err := s.DB.Insert(ctx, q, "visit_locations", []string{
		"country_code", "country_name", "region_name", "city_name", "timezone", "lat", "lon", "is_empty",
	}, l.CountryCode, l.CountryName, l.RegionName, l.CityName, l.Timezone, l.Latitude, l.Longitude, l.IsEmpty)
	l.ID = id
	return id, err
}

// incrementVisitsCount ports the count trackers' upsert into a random slot.
func (s *Store) incrementVisitsCount(ctx context.Context, q db.Querier, table string, shortURLID *int64, bot bool) error {
	keyCols, keyMarks, conflict := "potential_bot, slot_id", "?", "potential_bot, slot_id"
	args := []any{bot}
	if shortURLID != nil {
		keyCols, keyMarks, conflict = "short_url_id, potential_bot, slot_id", "?, ?", "short_url_id, potential_bot, slot_id"
		args = []any{*shortURLID, bot}
	}
	switch s.DB.Driver {
	case db.Postgres:
		_, err := s.DB.Exec(ctx, q, "INSERT INTO "+table+" ("+keyCols+", count) VALUES ("+keyMarks+", random() * 100, 1) ON CONFLICT ("+
			conflict+") DO UPDATE SET count = "+table+".count + 1", args...)
		return err
	case db.MySQL, db.Maria:
		_, err := s.DB.Exec(ctx, q, "INSERT INTO "+table+" ("+keyCols+", count) VALUES ("+keyMarks+", RAND() * 100, 1) ON DUPLICATE KEY UPDATE count = count + 1", args...)
		return err
	}
	slot := rand.IntN(100) + 1
	conds := "potential_bot = ? AND slot_id = ?"
	selArgs := []any{bot, slot}
	if shortURLID != nil {
		conds = "short_url_id = ? AND " + conds
		selArgs = append([]any{*shortURLID}, selArgs...)
	}
	from := table
	if s.DB.Driver == db.MSSQL {
		from += " WITH (UPDLOCK, ROWLOCK)"
	}
	one := 1
	var id int64
	err := s.DB.QueryRow(ctx, q, s.DB.Limit("SELECT id FROM "+from+" WHERE "+conds+" ORDER BY id", &one, nil), selArgs...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.DB.Exec(ctx, q, "INSERT INTO "+table+" ("+keyCols+") VALUES ("+keyMarks+", ?)", append(args, slot)...)
		return err
	}
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, q, "UPDATE "+table+" SET count = count + 1 WHERE id = ?", id)
	return err
}
