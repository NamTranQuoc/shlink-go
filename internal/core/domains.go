package core

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/db"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
	"github.com/NamTranQuoc/shlink-go/internal/valinor"
)

// EncodePHP serializes like NotFoundRedirects::jsonSerialize().
func (r NotFoundRedirects) EncodePHP(int) any {
	return phpx.Map{
		{Key: "baseUrlRedirect", Value: r.BaseURLRedirect},
		{Key: "regular404Redirect", Value: r.Regular404Redirect},
		{Key: "invalidShortUrlRedirect", Value: r.InvalidShortURLRedirect},
	}
}

// NotFoundRedirectsFromOptions ports NotFoundRedirects::fromConfig() for the env-configured defaults.
func NotFoundRedirectsFromOptions(o config.NotFoundRedirectOptions) NotFoundRedirects {
	return NotFoundRedirects{
		BaseURLRedirect:         o.BaseURLRedirect,
		Regular404Redirect:      o.Regular404Redirect,
		InvalidShortURLRedirect: o.InvalidShortURLRedirect,
	}
}

// DomainItem ports Domain\Model\DomainItem.
type DomainItem struct {
	Authority string
	IsDefault bool
	Redirects NotFoundRedirects
}

// EncodePHP serializes like DomainItem::jsonSerialize().
func (d DomainItem) EncodePHP(int) any {
	return phpx.Map{
		{Key: "domain", Value: d.Authority},
		{Key: "isDefault", Value: d.IsDefault},
		{Key: "redirects", Value: d.Redirects},
	}
}

// DomainService ports Domain\DomainService.
type DomainService struct {
	Store         *Store
	DefaultDomain string
	// NotFoundRedirects are the env-configured default redirects (NotFoundRedirectOptions).
	NotFoundRedirects config.NotFoundRedirectOptions
}

const domainColumns = "d.id, d.authority, d.base_url_redirect, d.regular_not_found_redirect, d.invalid_short_url_redirect"

func scanDomain(row rowScanner) (*Domain, error) {
	var d Domain
	var base, n404, inv sql.NullString
	if err := row.Scan(&d.ID, &d.Authority, &base, &n404, &inv); err != nil {
		return nil, err
	}
	d.BaseURLRedirect, d.Regular404Redirect, d.InvalidShortURLRedirect = strPtr(base), strPtr(n404), strPtr(inv)
	return &d, nil
}

// domainAPIKeyConditions ports DomainRepository::determineExtraSpecs(): DOMAIN_SPECIFIC filters the domain itself,
// AUTHORED_SHORT_URLS filters the joined short URLs (alias s).
func domainAPIKeyConditions(k *APIKey) (conds []string, args []any) {
	if k == nil {
		return nil, nil
	}
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
	return conds, args
}

// FindDomains ports DomainRepository::findDomains(): domains with short URLs or with any not-found redirect.
func (s *Store) FindDomains(ctx context.Context, k *APIKey) ([]*Domain, error) {
	conds, args := domainAPIKeyConditions(k)
	// Doctrine expands GROUP BY on an entity alias to all its columns
	query := "SELECT " + domainColumns + " FROM domains d LEFT JOIN short_urls s ON s.domain_id = d.id" + where(conds) +
		" GROUP BY " + domainColumns +
		" HAVING COUNT(s.id) > 0 OR d.base_url_redirect IS NOT NULL OR d.regular_not_found_redirect IS NOT NULL" +
		" OR d.invalid_short_url_redirect IS NOT NULL ORDER BY d.authority ASC"
	rows, err := s.DB.Query(ctx, s.DB.DB, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	domains := []*Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	return domains, rows.Err()
}

// FindDomainByAuthorityWithAPIKey ports DomainRepository::findOneByAuthority().
func (s *Store) FindDomainByAuthorityWithAPIKey(ctx context.Context, q db.Querier, authority string, k *APIKey) (*Domain, error) {
	conds, args := domainAPIKeyConditions(k)
	conds = append([]string{"d.authority = ?"}, conds...)
	args = append([]any{authority}, args...)
	one := 1
	query := s.DB.Limit("SELECT "+domainColumns+" FROM domains d LEFT JOIN short_urls s ON s.domain_id = d.id"+
		where(conds)+" ORDER BY d.id", &one, nil)
	d, err := scanDomain(s.DB.QueryRow(ctx, q, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

// DomainExists ports DomainRepository::domainExists().
func (s *Store) DomainExists(ctx context.Context, authority string, k *APIKey) (bool, error) {
	d, err := s.FindDomainByAuthorityWithAPIKey(ctx, s.DB.DB, authority, k)
	return d != nil, err
}

// ListDomains ports DomainService::listDomains().
func (ds *DomainService) ListDomains(ctx context.Context, k *APIKey) ([]DomainItem, error) {
	all, err := ds.Store.FindDomains(ctx, k)
	if err != nil {
		return nil, err
	}
	var def *Domain
	items := []DomainItem{}
	for _, d := range all {
		if d.Authority == ds.DefaultDomain {
			def = d
			continue
		}
		items = append(items, DomainItem{Authority: d.Authority, Redirects: d.Redirects()})
	}
	if k.HasRole(RoleDomainSpecific) {
		return items, nil
	}
	defItem := DomainItem{Authority: ds.DefaultDomain, IsDefault: true}
	if def != nil {
		defItem.Redirects = def.Redirects()
	}
	return append([]DomainItem{defItem}, items...), nil
}

// GetDomain ports DomainService::getDomain().
func (ds *DomainService) GetDomain(ctx context.Context, id string) (*Domain, error) {
	one := 1
	d, err := scanDomain(ds.Store.DB.QueryRow(ctx, ds.Store.DB.DB, ds.Store.DB.Limit("SELECT "+domainColumns+
		" FROM domains d WHERE d.id = ? ORDER BY d.id", &one, nil), domainIDArg(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDomainNotFoundByID(id)
	}
	return d, err
}

// FindByAuthority ports DomainService::findByAuthority().
func (ds *DomainService) FindByAuthority(ctx context.Context, authority string, k *APIKey) (*Domain, error) {
	return ds.Store.FindDomainByAuthorityWithAPIKey(ctx, ds.Store.DB.DB, authority, k)
}

// GetOrCreate ports DomainService::getOrCreate().
func (ds *DomainService) GetOrCreate(ctx context.Context, authority string, k *APIKey) (*Domain, error) {
	return ds.persistDomain(ctx, authority, k, nil)
}

// ConfigureNotFoundRedirects ports DomainService::configureNotFoundRedirects().
func (ds *DomainService) ConfigureNotFoundRedirects(ctx context.Context, authority string, r NotFoundRedirects, k *APIKey) (*Domain, error) {
	return ds.persistDomain(ctx, authority, k, &r)
}

// persistDomain ports getPersistedDomain() followed by the flush: an existing domain gets the redirects applied, a
// missing one is inserted. With an AUTHORED_SHORT_URLS key a domain not visible to the key is inserted again, and the
// unique authority index rejects it, like upstream.
func (ds *DomainService) persistDomain(ctx context.Context, authority string, k *APIKey, r *NotFoundRedirects) (*Domain, error) {
	var out *Domain
	err := ds.Store.DB.InTx(ctx, func(tx *sql.Tx) error {
		d, err := ds.Store.FindDomainByAuthorityWithAPIKey(ctx, tx, authority, k)
		if err != nil {
			return err
		}
		if d == nil && k.HasRole(RoleDomainSpecific) {
			// This API key is restricted to one domain and a different one was tried to be fetched
			return ErrDomainNotFoundByAuthority(authority)
		}
		if d == nil {
			d = &Domain{Authority: authority}
			if r != nil {
				d.BaseURLRedirect, d.Regular404Redirect, d.InvalidShortURLRedirect = r.BaseURLRedirect, r.Regular404Redirect, r.InvalidShortURLRedirect
			}
			d.ID, err = ds.Store.DB.Insert(ctx, tx, "domains",
				[]string{"authority", "base_url_redirect", "regular_not_found_redirect", "invalid_short_url_redirect"},
				d.Authority, nullable(d.BaseURLRedirect), nullable(d.Regular404Redirect), nullable(d.InvalidShortURLRedirect))
			out = d
			return err
		}
		if r != nil {
			d.BaseURLRedirect, d.Regular404Redirect, d.InvalidShortURLRedirect = r.BaseURLRedirect, r.Regular404Redirect, r.InvalidShortURLRedirect
			if _, err := ds.Store.DB.Exec(ctx, tx, "UPDATE domains SET base_url_redirect = ?, regular_not_found_redirect = ?, "+
				"invalid_short_url_redirect = ? WHERE id = ?", nullable(d.BaseURLRedirect), nullable(d.Regular404Redirect),
				nullable(d.InvalidShortURLRedirect), d.ID); err != nil {
				return err
			}
		}
		out = d
		return nil
	})
	return out, err
}

// NotFoundRedirectConfigs returns the not-found redirect configs NotFoundRedirectHandler tries for a request
// authority, in order: the matching domain's own redirects (only when the domain exists), then the env defaults. The
// resolver takes the first config that defines a redirect for the not-found type.
func (ds *DomainService) NotFoundRedirectConfigs(ctx context.Context, authority string) ([]NotFoundRedirects, error) {
	defaults := NotFoundRedirectsFromOptions(ds.NotFoundRedirects)
	d, err := ds.FindByAuthority(ctx, authority, nil)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return []NotFoundRedirects{defaults}, nil
	}
	return []NotFoundRedirects{d.Redirects(), defaults}, nil
}

// optionalRedirect is a `NoValue|string|null` property: Set is false for NoValue::NO_VALUE.
type optionalRedirect struct {
	Set   bool
	Value *string
}

func (o optionalRedirect) or(fallback *string) *string {
	if o.Set {
		return o.Value
	}
	return fallback
}

// DomainRedirectsRequest ports Rest\Action\Domain\Request\DomainRedirectsRequest.
type DomainRedirectsRequest struct {
	Authority               string
	BaseURLRedirect         optionalRedirect
	Regular404Redirect      optionalRedirect
	InvalidShortURLRedirect optionalRedirect
}

// MapDomainRedirectsRequest maps a payload to DomainRedirectsRequest like upstream's TreeMapper does.
func MapDomainRedirectsRequest(in valinor.Input) (*DomainRedirectsRequest, error) {
	m := newMapper(in, time.UTC, time.Now())
	r := &DomainRedirectsRequest{Authority: m.requiredString("domain", hostAndPort)}
	optional := func(name string) optionalRedirect {
		if _, ok := m.in[name]; !ok {
			return optionalRedirect{}
		}
		return optionalRedirect{Set: true, Value: m.optionalString(name, trimStrip)}
	}
	r.BaseURLRedirect = optional("baseUrlRedirect")
	r.Regular404Redirect = optional("regular404Redirect")
	r.InvalidShortURLRedirect = optional("invalidShortUrlRedirect")
	if err := m.result(); err != nil {
		return nil, err
	}
	return r, nil
}

// ToNotFoundRedirects ports DomainRedirectsRequest::toNotFoundRedirects(): properties not sent keep the defaults.
func (r *DomainRedirectsRequest) ToNotFoundRedirects(defaults *Domain) NotFoundRedirects {
	var d NotFoundRedirects
	if defaults != nil {
		d = defaults.Redirects()
	}
	return NotFoundRedirects{
		BaseURLRedirect:         r.BaseURLRedirect.or(d.BaseURLRedirect),
		Regular404Redirect:      r.Regular404Redirect.or(d.Regular404Redirect),
		InvalidShortURLRedirect: r.InvalidShortURLRedirect.or(d.InvalidShortURLRedirect),
	}
}
