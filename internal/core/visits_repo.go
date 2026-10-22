package core

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/db"
)

// DateRange ports shlink-common's DateRange.
type DateRange struct {
	Start *time.Time
	End   *time.Time
}

// VisitsParams ports Visit\Model\VisitsParams and its WithDomain/Orphan subclasses.
type VisitsParams struct {
	DateRange    DateRange
	Page         int
	ItemsPerPage int
	ExcludeBots  bool
	Domain       *string
	OrphanType   *VisitType
}

// VisitsFilter is the union of upstream's visits filtering objects.
type VisitsFilter struct {
	DateRange     DateRange
	ExcludeBots   bool
	APIKey        *APIKey
	Domain        *string
	OrphanType    *VisitType
	DefaultDomain string
}

// visitsQuery is the FROM/WHERE part of a visits query (alias v), with bound args.
type visitsQuery struct {
	joins []string
	conds []string
	args  []any
	// empty forces an empty result (e.g. no orphan visits for NO_ORPHAN_VISITS keys)
	empty bool
}

func (q *visitsQuery) where(cond string, args ...any) {
	q.conds = append(q.conds, cond)
	q.args = append(q.args, args...)
}

func (q *visitsQuery) sql() string {
	return " FROM visits v" + strings.Join(q.joins, "") + where(q.conds)
}

func (s *Store) applyVisitsCommon(q *visitsQuery, f VisitsFilter) {
	if f.ExcludeBots {
		q.where("v.potential_bot = ?", false)
	}
	if f.DateRange.Start != nil {
		q.where("v."+s.DB.Q("date")+" >= ?", s.dateLiteral(*f.DateRange.Start))
	}
	if f.DateRange.End != nil {
		q.where("v."+s.DB.Q("date")+" <= ?", s.dateLiteral(*f.DateRange.End))
	}
}

// dateLiteral is how upstream inlines dates in visits queries: toDateTimeString() in the date's own timezone.
func (s *Store) dateLiteral(t time.Time) string {
	return t.In(s.DB.Loc).Format("2006-01-02 15:04:05")
}

func (s *Store) applyInlinedSpec(q *visitsQuery, k *APIKey) {
	conds, args := apiKeyShortURLConditions(k, "s")
	for i, c := range conds {
		q.where(c, args[i])
	}
}

// ShortURLVisitsQuery ports createVisitsByShortCodeQueryBuilder().
func (s *Store) ShortURLVisitsQuery(ctx context.Context, id ShortURLIdentifier, f VisitsFilter) (*visitsQuery, error) {
	su, err := s.FindShortURL(ctx, s.DB.DB, id, f.APIKey)
	if err != nil {
		return nil, err
	}
	q := &visitsQuery{}
	shortURLID := int64(-1)
	if su != nil {
		shortURLID = su.ID
	}
	q.where("v.short_url_id = ?", shortURLID)
	s.applyVisitsCommon(q, f)
	return q, nil
}

// TagVisitsQuery ports createVisitsByTagQueryBuilder().
func (s *Store) TagVisitsQuery(tag string, f VisitsFilter) *visitsQuery {
	q := &visitsQuery{joins: []string{
		" INNER JOIN short_urls s ON s.id = v.short_url_id",
		" INNER JOIN short_urls_in_tags st ON st.short_url_id = s.id",
		" INNER JOIN tags t ON t.id = st.tag_id",
	}}
	q.where("t.name = ?", tag)
	if f.ExcludeBots {
		q.where("v.potential_bot = ?", false)
	}
	if f.Domain != nil && *f.Domain == DefaultAuthority {
		q.where("s.domain_id IS NULL")
	} else if f.Domain != nil {
		q.joins = append(q.joins, " INNER JOIN domains d ON d.id = s.domain_id")
		q.where("d.authority = ?", *f.Domain)
	}
	f.ExcludeBots = false
	s.applyVisitsCommon(q, f)
	s.applyInlinedSpec(q, f.APIKey)
	return q
}

// DomainVisitsQuery ports createVisitsByDomainQueryBuilder().
func (s *Store) DomainVisitsQuery(domain string, f VisitsFilter) *visitsQuery {
	q := &visitsQuery{joins: []string{" INNER JOIN short_urls s ON s.id = v.short_url_id"}}
	if domain == DefaultAuthority {
		q.where("s.domain_id IS NULL")
	} else {
		q.joins = append(q.joins, " INNER JOIN domains d ON d.id = s.domain_id")
		q.where("d.authority = ?", domain)
	}
	s.applyVisitsCommon(q, f)
	s.applyInlinedSpec(q, f.APIKey)
	return q
}

// OrphanVisitsQuery ports createOrphanVisitsQueryBuilder().
func (s *Store) OrphanVisitsQuery(f VisitsFilter) *visitsQuery {
	q := &visitsQuery{}
	if f.APIKey.HasRole(RoleNoOrphanVisits) {
		q.empty = true
		return q
	}
	s.applyVisitsCommon(q, f)
	q.where("v.short_url_id IS NULL")
	if f.OrphanType != nil {
		q.where("v.type = ?", string(*f.OrphanType))
	}
	domain := f.Domain
	if domain != nil && *domain == DefaultAuthority {
		d := f.DefaultDomain
		domain = &d
	}
	if domain != nil {
		q.where("v.visited_url LIKE ?", "%"+*domain+"%")
	}
	return q
}

// NonOrphanVisitsQuery ports createNonOrphanVisitsQueryBuilder().
func (s *Store) NonOrphanVisitsQuery(f VisitsFilter) *visitsQuery {
	q := &visitsQuery{}
	s.applyVisitsCommon(q, f)
	q.where("v.short_url_id IS NOT NULL")
	if IsShortURLRestricted(f.APIKey) || f.Domain != nil {
		q.joins = append(q.joins, " INNER JOIN short_urls s ON s.id = v.short_url_id")
	}
	if f.Domain != nil && *f.Domain == DefaultAuthority {
		q.where("s.domain_id IS NULL")
	} else if f.Domain != nil {
		q.joins = append(q.joins, " INNER JOIN domains d ON d.id = s.domain_id")
		q.where("d.authority = ?", *f.Domain)
	}
	s.applyInlinedSpec(q, f.APIKey)
	return q
}

// CountVisitsQuery counts the visits a query selects.
func (s *Store) CountVisitsQuery(ctx context.Context, q *visitsQuery) (int, error) {
	if q.empty {
		return 0, nil
	}
	var n db.Int
	err := s.DB.QueryRow(ctx, s.DB.DB, "SELECT COUNT(v.id)"+q.sql(), q.args...).Scan(&n)
	return int(n), err
}

// FindVisitsQuery lists the visits a query selects, newest id first, like resolveVisitsWithNativeQuery().
func (s *Store) FindVisitsQuery(ctx context.Context, q *visitsQuery, limit, offset int) ([]*Visit, error) {
	if q.empty {
		return []*Visit{}, nil
	}
	sub := s.DB.Limit("SELECT v.id"+q.sql()+" ORDER BY v.id DESC", &limit, &offset)
	query := `SELECT v.id, v.short_url_id, v.type, v.user_agent, v.referer, v.potential_bot, v.remote_addr, v.visited_url,
		v.redirect_url, v.` + s.DB.Q("date") + `, vl.id, vl.country_code, vl.country_name, vl.region_name, vl.city_name, vl.lat, vl.lon,
		vl.timezone, vl.is_empty
		FROM visits v INNER JOIN (` + sub + `) sq ON sq.id = v.id
		LEFT JOIN visit_locations vl ON vl.id = v.visit_location_id ORDER BY v.id DESC`
	rows, err := s.DB.Query(ctx, s.DB.DB, query, q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	visits := []*Visit{}
	for rows.Next() {
		v, err := s.scanVisit(rows)
		if err != nil {
			return nil, err
		}
		visits = append(visits, v)
	}
	return visits, rows.Err()
}

func (s *Store) scanVisit(row rowScanner) (*Visit, error) {
	var (
		v                         Visit
		shortURLID                sql.NullInt64
		typ, ua, referer          sql.NullString
		bot                       db.Bool
		remote, visited, redirect sql.NullString
		date                      = s.nullTime()
		locID                     sql.NullInt64
		cc, cn, rn, city, tz      sql.NullString
		lat, lon                  sql.NullFloat64
		empty                     db.Bool
	)
	if err := row.Scan(&v.ID, &shortURLID, &typ, &ua, &referer, &bot, &remote, &visited, &redirect, &date,
		&locID, &cc, &cn, &rn, &city, &lat, &lon, &tz, &empty); err != nil {
		return nil, err
	}
	if shortURLID.Valid {
		id := shortURLID.Int64
		v.ShortURLID = &id
	}
	v.Type = VisitType(typ.String)
	v.UserAgent = ua.String
	v.Referer = referer.String
	v.PotentialBot = bool(bot)
	v.RemoteAddr = strPtr(remote)
	v.VisitedURL = strPtr(visited)
	v.RedirectURL = strPtr(redirect)
	v.Date = date.Time
	if locID.Valid {
		v.Location = &VisitLocation{
			ID: locID.Int64, CountryCode: cc.String, CountryName: cn.String, RegionName: rn.String, CityName: city.String,
			Latitude: lat.Float64, Longitude: lon.Float64, Timezone: tz.String, IsEmpty: bool(empty),
		}
	}
	return &v, nil
}

// VisitsPaginator builds a paginator over a visits query.
func (s *Store) VisitsPaginator(ctx context.Context, q *visitsQuery, p VisitsParams) (*Paginator[*Visit], error) {
	return NewPaginator[*Visit](ctx, FuncAdapter[*Visit]{
		CountFn: func(ctx context.Context) (int, error) { return s.CountVisitsQuery(ctx, q) },
		SliceFn: func(ctx context.Context, offset, length int) ([]*Visit, error) {
			return s.FindVisitsQuery(ctx, q, length, offset)
		},
	}, p.ItemsPerPage, p.Page)
}

// CountNonOrphanVisitsFromCounters ports ShortUrlVisitsCountRepository::countNonOrphanVisits().
func (s *Store) CountNonOrphanVisitsFromCounters(ctx context.Context, excludeBots bool, k *APIKey) (int, error) {
	conds, args := apiKeyShortURLConditions(k, "s")
	if excludeBots {
		conds = append([]string{"vc.potential_bot = ?"}, conds...)
		args = append([]any{false}, args...)
	}
	var n db.Int
	err := s.DB.QueryRow(ctx, s.DB.DB, "SELECT COALESCE(SUM(vc.count), 0) FROM short_url_visits_counts vc INNER JOIN short_urls s ON s.id = vc.short_url_id"+
		where(conds), args...).Scan(&n)
	return int(n), err
}

// CountOrphanVisitsFromCounters ports OrphanVisitsCountRepository::countOrphanVisits().
func (s *Store) CountOrphanVisitsFromCounters(ctx context.Context, excludeBots bool, k *APIKey) (int, error) {
	if k.HasRole(RoleNoOrphanVisits) {
		return 0, nil
	}
	query := "SELECT COALESCE(SUM(vc.count), 0) FROM orphan_visits_counts vc"
	var args []any
	if excludeBots {
		query += " WHERE vc.potential_bot = ?"
		args = append(args, false)
	}
	var n db.Int
	err := s.DB.QueryRow(ctx, s.DB.DB, query, args...).Scan(&n)
	return int(n), err
}
