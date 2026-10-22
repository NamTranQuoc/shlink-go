package core

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/config"
)

// ImportedShortURLData holds what ShortUrl::fromImport() reads from an ImportedShlinkUrl.
type ImportedShortURLData struct {
	Source     string
	LongURL    string
	Tags       []string
	CreatedAt  time.Time
	Domain     *string
	ShortCode  string
	Title      *string
	ValidSince *time.Time
	ValidUntil *time.Time
	MaxVisits  *int
}

// ImportTx runs the persistence of ImportedLinksProcessor inside the transaction DoctrineBatchHelper::wrapIterable()
// opens: everything is rolled back if the import fails half way.
type ImportTx struct {
	ctx           context.Context
	store         *Store
	tx            *sql.Tx
	defaultDomain string
	mode          config.ShortURLMode
}

// InImportTx runs fn in an import transaction, committed when fn returns no error.
func (s *Store) InImportTx(ctx context.Context, o config.URLShortenerOptions, fn func(t *ImportTx) error) error {
	return s.DB.InTx(ctx, func(tx *sql.Tx) error {
		return fn(&ImportTx{ctx: ctx, store: s, tx: tx, defaultDomain: o.DefaultDomain, mode: o.Mode})
	})
}

// FindImportedShortURL ports ShortUrlRepository::findOneByImportedUrl().
func (t *ImportTx) FindImportedShortURL(source, shortCode string, domain *string) (*ShortURL, error) {
	conds := []string{"s.import_original_short_code = ?", "s.import_source = ?"}
	args := []any{shortCode, source}
	if domain != nil {
		conds = append(conds, "d.authority = ?")
		args = append(args, *domain)
	} else {
		conds = append(conds, "s.domain_id IS NULL")
	}
	one := 1
	query := t.store.DB.Limit("SELECT "+shortURLColumns+shortURLFrom+where(conds)+" ORDER BY s.id", &one, nil)
	su, err := t.store.scanShortURL(t.store.DB.QueryRow(t.ctx, t.tx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return su, err
}

// NewShortURL ports ShortUrl::fromImport() with ShortUrlCreation's defaults (strict mode, no multi-segment slugs,
// default short code length). The domain is only resolved when the short URL is persisted, like the relation resolver
// memoizes new domains until the entity manager flushes them.
func (t *ImportTx) NewShortURL(d ImportedShortURLData, importShortCode bool) (*ShortURL, error) {
	var slug *string
	if importShortCode {
		var err error
		code := d.ShortCode
		if slug, err = filterSlugLike(&code, config.ModeStrict, false); err != nil {
			return nil, err
		}
	}
	loc := t.store.Loc()
	su := &ShortURL{
		LongURL:                 d.LongURL,
		DateCreated:             d.CreatedAt.In(loc),
		ValidSince:              inLoc(d.ValidSince, loc),
		ValidUntil:              inLoc(d.ValidUntil, loc),
		MaxVisits:               d.MaxVisits,
		Title:                   d.Title,
		ForwardQuery:            true,
		ImportSource:            &d.Source,
		ImportOriginalShortCode: &d.ShortCode,
		customSlugWasProvided:   slug != nil,
		shortCodeLength:         config.DefaultShortCodesLengthValue,
	}
	if slug != nil {
		su.ShortCode = *slug
	} else {
		su.ShortCode = GenerateRandomShortCode(config.DefaultShortCodesLengthValue, config.ModeStrict)
	}
	su.Tags, _ = uniqueStrings(d.Tags, nil)
	if d.Domain != nil && *d.Domain != t.defaultDomain {
		su.Domain = &Domain{Authority: *d.Domain}
	}
	return su, nil
}

func inLoc(t *time.Time, loc *time.Location) *time.Time {
	if t == nil {
		return nil
	}
	v := t.In(loc)
	return &v
}

// EnsureShortCodeUniqueness ports ShortCodeUniquenessHelper::ensureShortCodeUniqueness(): false when the short code
// is in use and it cannot be regenerated.
func (t *ImportTx) EnsureShortCodeUniqueness(su *ShortURL, hasCustomSlug bool) (bool, error) {
	for {
		inUse, err := t.store.ShortCodeIsInUse(t.ctx, t.tx, su.Identifier(), true)
		if err != nil || !inUse {
			return err == nil, err
		}
		if hasCustomSlug {
			return false, nil
		}
		su.ShortCode = GenerateRandomShortCode(su.shortCodeLength, t.mode)
	}
}

// Persist inserts a short URL created by NewShortURL, with its domain and tags.
func (t *ImportTx) Persist(su *ShortURL) error {
	if su.Domain != nil && su.Domain.ID == 0 {
		d, err := t.store.findOrCreateDomain(t.ctx, t.tx, su.Domain.Authority)
		if err != nil {
			return err
		}
		su.Domain = d
	}
	return t.store.insertShortURL(t.ctx, t.tx, su)
}

// MostRecentImportedVisitDate ports ShortUrl::mostRecentImportedVisitDate().
func (t *ImportTx) MostRecentImportedVisitDate(shortURLID int64) (*time.Time, error) {
	return t.mostRecentVisitDate("short_url_id = ? AND type = ?", shortURLID, string(VisitImported))
}

// MostRecentOrphanVisitDate ports VisitRepository::findMostRecentOrphanVisit()'s date.
func (t *ImportTx) MostRecentOrphanVisitDate() (*time.Time, error) {
	return t.mostRecentVisitDate("short_url_id IS NULL")
}

func (t *ImportTx) mostRecentVisitDate(cond string, args ...any) (*time.Time, error) {
	one := 1
	date := t.store.nullTime()
	err := t.store.DB.QueryRow(t.ctx, t.tx, t.store.DB.Limit("SELECT date FROM visits WHERE "+cond+" ORDER BY id DESC", &one, nil),
		args...).Scan(&date)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !date.Valid) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &date.Time, nil
}

// InsertVisit persists an imported visit.
func (t *ImportTx) InsertVisit(v *Visit) error {
	return t.store.InsertVisit(t.ctx, t.tx, v)
}

// SetRules ports ShortUrlRedirectRuleService::saveRulesForShortUrl().
func (t *ImportTx) SetRules(shortURLID int64, rules []RedirectRule) error {
	return t.store.setRules(t.ctx, t.tx, shortURLID, rules)
}
