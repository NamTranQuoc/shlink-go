package core

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/db"
)

// EventDispatcher receives domain events (ShortUrlCreated, UrlVisited), like upstream's PSR-14 dispatcher.
type EventDispatcher interface {
	ShortURLCreated(shortURLID int64)
	URLVisited(visitID int64, originalIPAddress *string)
}

type noopDispatcher struct{}

func (noopDispatcher) ShortURLCreated(int64)     {}
func (noopDispatcher) URLVisited(int64, *string) {}

// TitleResolver ports ShortUrlTitleResolutionHelperInterface.
type TitleResolver interface {
	// ResolveTitle returns the resolved title for a long URL, or nil.
	ResolveTitle(ctx context.Context, longURL string) *string
}

// ShortURLService groups upstream's UrlShortener, ShortUrlResolver, ShortUrlService and DeleteShortUrlService.
type ShortURLService struct {
	Store      *Store
	Options    config.URLShortenerOptions
	DeleteOpts config.DeleteShortURLsOptions
	Titles     TitleResolver
	Events     EventDispatcher
}

func (s *ShortURLService) events() EventDispatcher {
	if s.Events == nil {
		return noopDispatcher{}
	}
	return s.Events
}

func (s *ShortURLService) processTitle(ctx context.Context, longURL *string, hasTitle bool) (*string, bool) {
	if !s.Options.AutoResolveTitles || hasTitle || s.Titles == nil {
		return nil, false
	}
	url := ""
	if longURL != nil {
		url = *longURL
	}
	title := s.Titles.ResolveTitle(ctx, url)
	return title, title != nil
}

// Shorten ports UrlShortener::shorten().
func (s *ShortURLService) Shorten(ctx context.Context, c *ShortURLCreation) (*ShortURL, error) {
	if c.FindIfExists {
		existing, err := s.Store.FindOneMatching(ctx, c)
		if err != nil || existing != nil {
			return existing, err
		}
	}
	if title, ok := s.processTitle(ctx, &c.LongURL, c.HasTitle()); ok {
		c.Title = title
		c.TitleWasAutoResolved = true
	}

	var created *ShortURL
	ctx, releaseLocks := s.Store.withRelationLocks(ctx)
	err := s.Store.DB.InTx(ctx, func(tx *sql.Tx) error {
		su, err := s.Store.newShortURL(ctx, tx, c, s.Options.DefaultDomain)
		if err != nil {
			return err
		}
		if err := s.ensureShortCodeUniqueness(ctx, tx, su, c.CustomSlug != nil); err != nil {
			return err
		}
		if err := s.Store.insertShortURL(ctx, tx, su); err != nil {
			return err
		}
		created = su
		return nil
	})
	releaseLocks()
	if err != nil {
		return nil, err
	}
	s.events().ShortURLCreated(created.ID)
	return created, nil
}

// ensureShortCodeUniqueness ports ShortCodeUniquenessHelper.
func (s *ShortURLService) ensureShortCodeUniqueness(ctx context.Context, tx *sql.Tx, su *ShortURL, hasCustomSlug bool) error {
	for {
		inUse, err := s.Store.ShortCodeIsInUse(ctx, tx, su.Identifier(), true)
		if err != nil {
			return err
		}
		if !inUse {
			return nil
		}
		if hasCustomSlug {
			return ErrNonUniqueSlug(su.ShortCode, su.Authority())
		}
		// Like ShortUrl::regenerateShortCode(), the path prefix is not kept on regeneration
		su.ShortCode = GenerateRandomShortCode(su.shortCodeLength, s.Options.Mode)
	}
}

// ResolveShortURL ports ShortUrlResolver::resolveShortUrl().
func (s *ShortURLService) ResolveShortURL(ctx context.Context, id ShortURLIdentifier, apiKey *APIKey) (*ShortURL, error) {
	su, err := s.Store.FindShortURL(ctx, s.Store.DB.DB, id, apiKey)
	if err != nil {
		return nil, err
	}
	if su == nil {
		return nil, ErrShortURLNotFound(id)
	}
	return su, nil
}

// ResolvePublicShortURL ports ShortUrlResolver::resolvePublicShortUrl().
func (s *ShortURLService) ResolvePublicShortURL(ctx context.Context, id ShortURLIdentifier) (*ShortURL, error) {
	su, err := s.Store.FindShortURLWithDomainFallback(ctx, id, s.Options.Mode)
	if err != nil {
		return nil, err
	}
	if su == nil {
		return nil, ErrShortURLNotFound(id)
	}
	return su, nil
}

// ResolveEnabledShortURL ports ShortUrlResolver::resolveEnabledShortUrl().
func (s *ShortURLService) ResolveEnabledShortURL(ctx context.Context, id ShortURLIdentifier) (*ShortURL, error) {
	su, err := s.ResolvePublicShortURL(ctx, id)
	if err != nil {
		return nil, err
	}
	visits := 0
	if su.MaxVisits != nil {
		if visits, err = s.Store.CountVisits(ctx, s.Store.DB.DB, su.ID); err != nil {
			return nil, err
		}
	}
	if !su.IsEnabled(visits, s.Store.Now()) {
		return nil, ErrShortURLNotFound(id)
	}
	return su, nil
}

// UpdateShortURL ports ShortUrlService::updateShortUrl().
func (s *ShortURLService) UpdateShortURL(ctx context.Context, id ShortURLIdentifier, e *ShortURLEdition, apiKey *APIKey) (*ShortURL, error) {
	if e.LongURL != nil {
		if title, ok := s.processTitle(ctx, e.LongURL, e.TitleWasProvided); ok {
			e.Title = title
			e.TitleWasProvided = true
			e.TitleWasAutoResolved = true
		}
	}
	su, err := s.ResolveShortURL(ctx, id, apiKey)
	if err != nil {
		return nil, err
	}
	ctx, releaseLocks := s.Store.withRelationLocks(ctx)
	err = s.Store.DB.InTx(ctx, func(tx *sql.Tx) error {
		return s.Store.updateShortURL(ctx, tx, su, e)
	})
	releaseLocks()
	if err != nil {
		return nil, err
	}
	return su, nil
}

// DeleteByShortCode ports DeleteShortUrlService::deleteByShortCode().
func (s *ShortURLService) DeleteByShortCode(ctx context.Context, id ShortURLIdentifier, ignoreThreshold bool, apiKey *APIKey) error {
	su, err := s.ResolveShortURL(ctx, id, apiKey)
	if err != nil {
		return err
	}
	if !ignoreThreshold && s.DeleteOpts.CheckVisitsThreshold {
		visits, err := s.Store.CountVisits(ctx, s.Store.DB.DB, su.ID)
		if err != nil {
			return err
		}
		if visits >= s.DeleteOpts.VisitsThreshold {
			return ErrDeleteShortURLThreshold(s.DeleteOpts.VisitsThreshold, id)
		}
	}
	return s.Store.deleteShortURL(ctx, su.ID)
}

// newShortURL ports ShortUrl::create() with PersistenceShortUrlRelationResolver: the domain is resolved (or created),
// tags are resolved later on insert.
func (s *Store) newShortURL(ctx context.Context, tx *sql.Tx, c *ShortURLCreation, defaultDomain string) (*ShortURL, error) {
	code := ""
	if c.PathPrefix != nil {
		code = *c.PathPrefix
	}
	if c.CustomSlug != nil {
		code += *c.CustomSlug
	} else {
		code += GenerateRandomShortCode(c.ShortCodeLength, c.ShortURLMode)
	}
	su := &ShortURL{
		LongURL:               c.LongURL,
		ShortCode:             code,
		DateCreated:           s.NowIn(),
		ValidSince:            c.ValidSince,
		ValidUntil:            c.ValidUntil,
		MaxVisits:             c.MaxVisits,
		Title:                 c.Title,
		TitleWasAutoResolved:  c.TitleWasAutoResolved,
		Crawlable:             c.Crawlable,
		ForwardQuery:          c.ForwardQuery,
		customSlugWasProvided: c.CustomSlug != nil,
		shortCodeLength:       c.ShortCodeLength,
	}
	su.Tags, su.TagKeys = uniqueStrings(c.Tags, c.TagKeys)
	var domain *string
	if c.Domain != nil && *c.Domain != defaultDomain {
		domain = c.Domain
	}
	if err := s.lockRelations(ctx, tx, su.Tags, domain); err != nil {
		return nil, err
	}
	if c.APIKey != nil && c.APIKey.ID != 0 {
		id := c.APIKey.ID
		su.AuthorAPIKeyID = &id
	}
	if domain != nil {
		d, err := s.findOrCreateDomain(ctx, tx, *domain)
		if err != nil {
			return nil, err
		}
		su.Domain = d
	}
	return su, nil
}

// uniqueStrings ports array_unique() as resolveTags() applies it: first occurrences are kept, with their keys.
// It returns nil keys when they are sequential.
func uniqueStrings(in, keys []string) ([]string, []string) {
	out := make([]string, 0, len(in))
	outKeys := make([]string, 0, len(in))
	seen := map[string]bool{}
	for i, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
		if keys != nil {
			outKeys = append(outKeys, keys[i])
		} else {
			outKeys = append(outKeys, strconv.Itoa(i))
		}
	}
	for i, k := range outKeys {
		if k != strconv.Itoa(i) {
			return out, outKeys
		}
	}
	return out, nil
}

// FindDomainByAuthority finds a domain by authority, without API key restrictions.
func (s *Store) FindDomainByAuthority(ctx context.Context, q db.Querier, authority string) (*Domain, error) {
	var d Domain
	var base, n404, inv sql.NullString
	one := 1
	err := s.DB.QueryRow(ctx, q, s.DB.Limit(`SELECT id, authority, base_url_redirect, regular_not_found_redirect,
		invalid_short_url_redirect FROM domains WHERE authority = ? ORDER BY id`, &one, nil), authority).
		Scan(&d.ID, &d.Authority, &base, &n404, &inv)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.BaseURLRedirect, d.Regular404Redirect, d.InvalidShortURLRedirect = strPtr(base), strPtr(n404), strPtr(inv)
	return &d, nil
}

// findOrCreateDomain ports PersistenceShortUrlRelationResolver::resolveDomain(), including its lock.
func (s *Store) findOrCreateDomain(ctx context.Context, q db.Querier, authority string) (*Domain, error) {
	locks, name := relationLocksFrom(ctx), "domain_"+authority
	if err := locks.lock(ctx, name); err != nil {
		return nil, err
	}
	d, err := s.FindDomainByAuthority(ctx, q, authority)
	if err != nil || d != nil {
		// The lock can be released immediately if the domain is not new
		locks.release(name)
		return d, err
	}
	id, err := s.DB.Insert(ctx, q, "domains",
		[]string{"authority", "base_url_redirect", "regular_not_found_redirect", "invalid_short_url_redirect"},
		authority, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	return &Domain{ID: id, Authority: authority}, nil
}

// resolveTagIDs finds or creates tags by name, preserving order, with PersistenceShortUrlRelationResolver's locks.
func (s *Store) resolveTagIDs(ctx context.Context, q db.Querier, names []string) ([]int64, error) {
	locks := relationLocksFrom(ctx)
	ids := make([]int64, 0, len(names))
	for _, name := range names {
		if err := locks.lock(ctx, "tag_"+name); err != nil {
			return nil, err
		}
		var id int64
		one := 1
		err := s.DB.QueryRow(ctx, q, s.DB.Limit("SELECT id FROM tags WHERE name = ? ORDER BY id", &one, nil), name).Scan(&id)
		if err == nil {
			locks.release("tag_" + name)
		} else if errors.Is(err, sql.ErrNoRows) {
			id, err = s.DB.Insert(ctx, q, "tags", []string{"name"}, name)
		}
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Store) insertShortURL(ctx context.Context, q db.Querier, su *ShortURL) error {
	var domainID any
	if su.Domain != nil {
		domainID = su.Domain.ID
	}
	var maxVisits, author any
	if su.MaxVisits != nil {
		maxVisits = *su.MaxVisits
	}
	if su.AuthorAPIKeyID != nil {
		author = *su.AuthorAPIKeyID
	}
	id, err := s.DB.Insert(ctx, q, "short_urls", []string{
		"original_url", "long_url_hash", "short_code", "date_created", "valid_since", "valid_until", "max_visits",
		"import_source", "import_original_short_code", "title", "title_was_auto_resolved", "crawlable",
		"forward_query", "domain_id", "author_api_key_id",
	}, su.LongURL, LongURLHash(su.LongURL), su.ShortCode, s.DB.FormatTime(su.DateCreated), s.DB.TimeArg(su.ValidSince),
		s.DB.TimeArg(su.ValidUntil), maxVisits, nullable(su.ImportSource), nullable(su.ImportOriginalShortCode),
		nullable(su.Title), su.TitleWasAutoResolved, su.Crawlable, su.ForwardQuery, domainID, author)
	if err != nil {
		return err
	}
	su.ID = id
	return s.setTags(ctx, q, su)
}

func (s *Store) setTags(ctx context.Context, q db.Querier, su *ShortURL) error {
	if _, err := s.DB.Exec(ctx, q, "DELETE FROM short_urls_in_tags WHERE short_url_id = ?", su.ID); err != nil {
		return err
	}
	tagIDs, err := s.resolveTagIDs(ctx, q, su.Tags)
	if err != nil {
		return err
	}
	for _, tagID := range tagIDs {
		if _, err := s.DB.Exec(ctx, q, "INSERT INTO short_urls_in_tags (short_url_id, tag_id) VALUES (?, ?)", su.ID, tagID); err != nil {
			return err
		}
	}
	return nil
}

func nullable(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// updateShortURL ports ShortUrl::update() followed by a flush.
func (s *Store) updateShortURL(ctx context.Context, tx *sql.Tx, su *ShortURL, e *ShortURLEdition) error {
	if e.ValidSinceWasProvided {
		su.ValidSince = e.ValidSince
	}
	if e.ValidUntilWasProvided {
		su.ValidUntil = e.ValidUntil
	}
	if e.MaxVisitsWasProvided {
		su.MaxVisits = e.MaxVisits
	}
	if e.LongURL != nil {
		su.LongURL = *e.LongURL
	}
	tagsChanged := false
	if e.Tags != nil {
		su.Tags, su.TagKeys = uniqueStrings(e.Tags, e.TagKeys)
		if err := s.lockRelations(ctx, tx, su.Tags, nil); err != nil {
			return err
		}
		tagsChanged = true
	}
	if e.Crawlable != nil {
		su.Crawlable = *e.Crawlable
	}
	if su.Title == nil || e.TitleWasProvided || (su.TitleWasAutoResolved && e.TitleWasAutoResolved) {
		su.Title = e.Title
		su.TitleWasAutoResolved = e.TitleWasAutoResolved
	}
	if e.ForwardQuery != nil {
		su.ForwardQuery = *e.ForwardQuery
	}
	var maxVisits any
	if su.MaxVisits != nil {
		maxVisits = *su.MaxVisits
	}
	_, err := s.DB.Exec(ctx, tx, `UPDATE short_urls SET original_url = ?, long_url_hash = ?, valid_since = ?, valid_until = ?,
		max_visits = ?, title = ?, title_was_auto_resolved = ?, crawlable = ?, forward_query = ? WHERE id = ?`,
		su.LongURL, LongURLHash(su.LongURL), s.DB.TimeArg(su.ValidSince), s.DB.TimeArg(su.ValidUntil), maxVisits,
		nullable(su.Title), su.TitleWasAutoResolved, su.Crawlable, su.ForwardQuery, su.ID)
	if err != nil {
		return err
	}
	if tagsChanged {
		return s.setTags(ctx, tx, su)
	}
	return nil
}

// deleteShortURL removes a short URL. Related rows are removed by ON DELETE CASCADE where the engine enforces it; the
// ORM upstream does the same single DELETE.
func (s *Store) deleteShortURL(ctx context.Context, id int64) error {
	return s.DB.InTx(ctx, func(tx *sql.Tx) error {
		// The ORM removes many-to-many join rows itself before deleting the owning entity
		if _, err := s.DB.Exec(ctx, tx, "DELETE FROM short_urls_in_tags WHERE short_url_id = ?", id); err != nil {
			return err
		}
		_, err := s.DB.Exec(ctx, tx, "DELETE FROM short_urls WHERE id = ?", id)
		return err
	})
}
