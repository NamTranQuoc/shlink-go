package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/db"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
	"github.com/NamTranQuoc/shlink-go/internal/valinor"
)

// VisitsParamsFromQuery ports VisitsParams/WithDomainVisitsParams/OrphanVisitsParams::fromRawData(). withType enables
// the orphan visit type param. Unparseable dates and non-string values are upstream exceptions (internal errors).
func VisitsParamsFromQuery(q valinor.Input, withType bool, loc *time.Location, now time.Time) (VisitsParams, error) {
	var p VisitsParams
	var err error
	if p.DateRange.Start, err = parseDateFromQuery(q, "startDate", loc, now); err != nil {
		return p, err
	}
	if p.DateRange.End, err = parseDateFromQuery(q, "endDate", loc, now); err != nil {
		return p, err
	}
	p.Page = 1
	if v, ok := q["page"]; ok && v != nil {
		if page := phpIntCastAny(v); page > 0 {
			p.Page = page
		}
	}
	p.ItemsPerPage = AllItems
	if v, ok := q["itemsPerPage"]; ok && v != nil {
		if perPage := phpIntCastAny(v); perPage >= 0 {
			p.ItemsPerPage = perPage
		}
	}
	if v, ok := q["excludeBots"]; ok && v != nil {
		p.ExcludeBots = true
	}
	if v, ok := q["domain"]; ok && v != nil {
		s, isString := v.(string)
		if !isString {
			return p, internal(errors.New("domain must be a string"))
		}
		p.Domain = &s
	}
	if v, ok := q["type"]; withType && ok && v != nil {
		s, isString := v.(string)
		if !isString {
			return p, internal(errors.New("type must be a string"))
		}
		valid := false
		for _, t := range OrphanVisitTypes {
			if string(t) == s {
				valid = true
			}
		}
		if !valid {
			return p, ErrValidationWithNames([]any{"type"})
		}
		t := VisitType(s)
		p.OrphanType = &t
	}
	return p, nil
}

// parseDateFromQuery ports parseDateFromQuery(): empty values are ignored.
func parseDateFromQuery(q valinor.Input, name string, loc *time.Location, now time.Time) (*time.Time, error) {
	v, ok := q[name]
	if !ok || v == nil {
		return nil, nil
	}
	s, isString := v.(string)
	if !isString {
		if list, isList := v.([]any); isList && len(list) == 0 {
			return nil, nil
		}
		return nil, internal(fmt.Errorf("%s must be a string", name))
	}
	if s == "" || s == "0" {
		return nil, nil
	}
	t, err := phpx.ParseDate(s, loc, now)
	if err != nil {
		return nil, internal(err)
	}
	t = t.In(loc)
	return &t, nil
}

var leadingNumber = regexp.MustCompile(`^[ \t\n\r\v\f]*[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?`)

// phpIntCastAny ports PHP's (int) cast for query values: strings use their leading numeric part, arrays give 0 or 1.
func phpIntCastAny(v any) int {
	switch t := v.(type) {
	case string:
		m := strings.TrimSpace(leadingNumber.FindString(t))
		if m == "" {
			return 0
		}
		if i, err := strconv.ParseInt(m, 10, 64); err == nil {
			return int(i)
		}
		f, err := strconv.ParseFloat(m, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0
		}
		return int(f)
	case json.Number:
		return phpIntCastAny(t.String())
	case []any:
		if len(t) == 0 {
			return 0
		}
		return 1
	case map[string]any:
		if len(t) == 0 {
			return 0
		}
		return 1
	case bool:
		if t {
			return 1
		}
		return 0
	case int:
		return t
	}
	return 0
}

// VisitsStatsHelper ports Visit\VisitsStatsHelper, VisitsDeleter and ShortUrlVisitsDeleter.
type VisitsStatsHelper struct {
	Store         *Store
	DefaultDomain string
}

// GetVisitsStats ports VisitsStatsHelper::getVisitsStats().
func (h *VisitsStatsHelper) GetVisitsStats(ctx context.Context, k *APIKey) (VisitsStats, error) {
	var stats VisitsStats
	var err error
	if stats.NonOrphanVisits.Total, err = h.Store.CountNonOrphanVisitsFromCounters(ctx, false, k); err != nil {
		return stats, err
	}
	if stats.OrphanVisits.Total, err = h.Store.CountOrphanVisitsFromCounters(ctx, false, k); err != nil {
		return stats, err
	}
	if stats.NonOrphanVisits.NonBots, err = h.Store.CountNonOrphanVisitsFromCounters(ctx, true, k); err != nil {
		return stats, err
	}
	stats.OrphanVisits.NonBots, err = h.Store.CountOrphanVisitsFromCounters(ctx, true, k)
	return stats, err
}

func (h *VisitsStatsHelper) filter(p VisitsParams, k *APIKey) VisitsFilter {
	return VisitsFilter{DateRange: p.DateRange, ExcludeBots: p.ExcludeBots, APIKey: k}
}

// VisitsForShortURL ports VisitsStatsHelper::visitsForShortUrl().
func (h *VisitsStatsHelper) VisitsForShortURL(ctx context.Context, id ShortURLIdentifier, p VisitsParams, k *APIKey) (*Paginator[*Visit], error) {
	inUse, err := h.shortCodeIsInUse(ctx, id, k)
	if err != nil {
		return nil, err
	}
	if !inUse {
		return nil, ErrShortURLNotFound(id)
	}
	q, err := h.Store.ShortURLVisitsQuery(ctx, id, h.filter(p, k))
	if err != nil {
		return nil, err
	}
	return h.Store.VisitsPaginator(ctx, q, p)
}

// shortCodeIsInUse ports ShortUrlRepository::shortCodeIsInUse() with the API key spec applied.
func (h *VisitsStatsHelper) shortCodeIsInUse(ctx context.Context, id ShortURLIdentifier, k *APIKey) (bool, error) {
	s := h.Store
	conds, args := findOneConditions(id, k)
	one := 1
	var found int64
	err := s.DB.QueryRow(ctx, s.DB.DB, s.DB.Limit("SELECT s.id"+shortURLFrom+where(conds)+" ORDER BY s.id", &one, nil), args...).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// VisitsForTag ports VisitsStatsHelper::visitsForTag().
func (h *VisitsStatsHelper) VisitsForTag(ctx context.Context, tag string, p VisitsParams, k *APIKey) (*Paginator[*Visit], error) {
	exists, err := h.Store.TagExists(ctx, tag, k)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrTagNotFound(tag)
	}
	f := h.filter(p, k)
	f.Domain = p.Domain
	return h.Store.VisitsPaginator(ctx, h.Store.TagVisitsQuery(tag, f), p)
}

// VisitsForDomain ports VisitsStatsHelper::visitsForDomain().
func (h *VisitsStatsHelper) VisitsForDomain(ctx context.Context, domain string, p VisitsParams, k *APIKey) (*Paginator[*Visit], error) {
	if domain != DefaultAuthority {
		exists, err := h.domainExists(ctx, domain, k)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrDomainNotFoundByAuthority(domain)
		}
	}
	return h.Store.VisitsPaginator(ctx, h.Store.DomainVisitsQuery(domain, h.filter(p, k)), p)
}

// domainExists ports DomainRepository::domainExists(): the domain left-joined with its short URLs, with
// DOMAIN_SPECIFIC keys restricted to their domain and AUTHORED_SHORT_URLS keys to their short URLs.
func (h *VisitsStatsHelper) domainExists(ctx context.Context, authority string, k *APIKey) (bool, error) {
	conds := []string{"d.authority = ?"}
	args := []any{authority}
	if k != nil {
		for _, r := range k.Roles {
			switch r.Role {
			case RoleDomainSpecific:
				conds = append(conds, "d.id = ?")
				args = append(args, domainIDArg(r.DomainID()))
			case RoleAuthoredShortURLs:
				conds = append(conds, "s.author_api_key_id = ?")
				args = append(args, k.ID)
			}
		}
	}
	var n db.Int
	err := h.Store.DB.QueryRow(ctx, h.Store.DB.DB, "SELECT COUNT(d.id) FROM domains d LEFT JOIN short_urls s ON s.domain_id = d.id"+
		where(conds), args...).Scan(&n)
	return n > 0, err
}

// OrphanVisits ports VisitsStatsHelper::orphanVisits().
func (h *VisitsStatsHelper) OrphanVisits(ctx context.Context, p VisitsParams, k *APIKey) (*Paginator[*Visit], error) {
	f := h.filter(p, k)
	f.Domain = p.Domain
	f.OrphanType = p.OrphanType
	f.DefaultDomain = h.DefaultDomain
	return h.Store.VisitsPaginator(ctx, h.Store.OrphanVisitsQuery(f), p)
}

// NonOrphanVisits ports VisitsStatsHelper::nonOrphanVisits().
func (h *VisitsStatsHelper) NonOrphanVisits(ctx context.Context, p VisitsParams, k *APIKey) (*Paginator[*Visit], error) {
	f := h.filter(p, k)
	f.Domain = p.Domain
	return h.Store.VisitsPaginator(ctx, h.Store.NonOrphanVisitsQuery(f), p)
}

// DeleteOrphanVisits ports VisitsDeleter::deleteOrphanVisits() and VisitDeleterRepository::deleteOrphanVisits().
func (h *VisitsStatsHelper) DeleteOrphanVisits(ctx context.Context, k *APIKey) (int, error) {
	if k.HasRole(RoleNoOrphanVisits) {
		return 0, nil
	}
	var affected int64
	err := h.Store.DB.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := h.Store.DB.Exec(ctx, tx, "DELETE FROM orphan_visits_counts"); err != nil {
			return err
		}
		res, err := h.Store.DB.Exec(ctx, tx, "DELETE FROM visits WHERE short_url_id IS NULL")
		if err != nil {
			return err
		}
		affected, err = res.RowsAffected()
		return err
	})
	return int(affected), err
}

// DeleteShortURLVisits ports ShortUrlVisitsDeleter::deleteShortUrlVisits() and
// VisitDeleterRepository::deleteShortUrlVisits().
func (h *VisitsStatsHelper) DeleteShortURLVisits(ctx context.Context, id ShortURLIdentifier, k *APIKey) (int, error) {
	su, err := h.Store.FindShortURL(ctx, h.Store.DB.DB, id, k)
	if err != nil {
		return 0, err
	}
	if su == nil {
		return 0, ErrShortURLNotFound(id)
	}
	var affected int64
	err = h.Store.DB.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := h.Store.DB.Exec(ctx, tx, "DELETE FROM short_url_visits_counts WHERE short_url_id = ?", su.ID); err != nil {
			return err
		}
		res, err := h.Store.DB.Exec(ctx, tx, "DELETE FROM visits WHERE short_url_id = ?", su.ID)
		if err != nil {
			return err
		}
		affected, err = res.RowsAffected()
		return err
	})
	return int(affected), err
}
