package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/NamTranQuoc/shlink-go/internal/db"
)

// Store gives repositories access to the database.
type Store struct {
	DB *db.DB
	// Now returns the current time; replaceable for tests.
	Now func() time.Time
	// Locks guards the creation of domains and tags across requests and processes; nil disables it.
	Locks Locker
}

// NewStore creates a store bound to a database.
func NewStore(d *db.DB) *Store {
	return &Store{DB: d, Now: time.Now}
}

// Loc returns the configured timezone.
func (s *Store) Loc() *time.Location { return s.DB.Loc }

// NowIn returns the current time in the configured timezone, truncated to seconds like datetime columns store it.
func (s *Store) NowIn() time.Time {
	return s.Now().In(s.DB.Loc)
}

func (s *Store) nullTime() db.Time { return db.Time{Loc: s.DB.Loc} }

// shortURLColumns selects every short URL column plus the joined domain (alias d).
const shortURLColumns = `s.id, s.original_url, s.short_code, s.date_created, s.valid_since, s.valid_until, s.max_visits,
 s.import_source, s.import_original_short_code, s.title, s.title_was_auto_resolved, s.crawlable, s.forward_query,
 s.author_api_key_id, d.id, d.authority, d.base_url_redirect, d.regular_not_found_redirect, d.invalid_short_url_redirect`

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanShortURL(row rowScanner) (*ShortURL, error) {
	var (
		su                         ShortURL
		created                    = s.nullTime()
		since                      = s.nullTime()
		until                      = s.nullTime()
		maxVisits                  sql.NullInt64
		importSource, importCode   sql.NullString
		title                      sql.NullString
		titleAuto, crawlable, fwdQ db.Bool
		author                     sql.NullInt64
		domainID                   sql.NullInt64
		authority, base, n404, inv sql.NullString
	)
	err := row.Scan(&su.ID, &su.LongURL, &su.ShortCode, &created, &since, &until, &maxVisits, &importSource, &importCode,
		&title, &titleAuto, &crawlable, &fwdQ, &author, &domainID, &authority, &base, &n404, &inv)
	if err != nil {
		return nil, err
	}
	su.DateCreated = created.Time
	su.ValidSince = since.Ptr()
	su.ValidUntil = until.Ptr()
	if maxVisits.Valid {
		v := int(maxVisits.Int64)
		su.MaxVisits = &v
	}
	su.ImportSource = strPtr(importSource)
	su.ImportOriginalShortCode = strPtr(importCode)
	su.Title = strPtr(title)
	su.TitleWasAutoResolved = bool(titleAuto)
	su.Crawlable = bool(crawlable)
	su.ForwardQuery = bool(fwdQ)
	if author.Valid {
		v := author.Int64
		su.AuthorAPIKeyID = &v
	}
	if domainID.Valid {
		su.Domain = &Domain{
			ID: domainID.Int64, Authority: authority.String,
			BaseURLRedirect: strPtr(base), Regular404Redirect: strPtr(n404), InvalidShortURLRedirect: strPtr(inv),
		}
	}
	su.Tags = []string{}
	return &su, nil
}

func strPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

// loadTags fills the tags of the given short URLs, sorted by name like the ORM mapping's orderBy.
func (s *Store) loadTags(ctx context.Context, q db.Querier, urls ...*ShortURL) error {
	if len(urls) == 0 {
		return nil
	}
	byID := make(map[int64]*ShortURL, len(urls))
	ids := make([]any, 0, len(urls))
	marks := make([]string, 0, len(urls))
	for _, u := range urls {
		if _, ok := byID[u.ID]; ok {
			continue
		}
		byID[u.ID] = u
		ids = append(ids, u.ID)
		marks = append(marks, "?")
	}
	rows, err := s.DB.Query(ctx, q, `SELECT st.short_url_id, t.name FROM short_urls_in_tags st
		INNER JOIN tags t ON t.id = st.tag_id WHERE st.short_url_id IN (`+strings.Join(marks, ",")+`) ORDER BY t.name ASC`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		if u := byID[id]; u != nil {
			u.Tags = append(u.Tags, name)
		}
	}
	return rows.Err()
}

// loadAPIKeyRoles loads the roles of an API key.
func (s *Store) loadAPIKeyRoles(ctx context.Context, q db.Querier, keys ...*APIKey) error {
	if len(keys) == 0 {
		return nil
	}
	byID := map[int64]*APIKey{}
	ids := []any{}
	marks := []string{}
	for _, k := range keys {
		byID[k.ID] = k
		k.Roles = nil
		ids = append(ids, k.ID)
		marks = append(marks, "?")
	}
	rows, err := s.DB.Query(ctx, q, "SELECT id, api_key_id, role_name, meta FROM api_key_roles WHERE api_key_id IN ("+
		strings.Join(marks, ",")+") ORDER BY id ASC", ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var role APIKeyRole
		var keyID int64
		var meta string
		if err := rows.Scan(&role.ID, &keyID, &role.Role, &meta); err != nil {
			return err
		}
		role.Meta = decodeMeta(meta)
		if k := byID[keyID]; k != nil {
			k.Roles = append(k.Roles, role)
		}
	}
	return rows.Err()
}

func decodeMeta(raw string) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// EncodeMeta encodes role meta the way Doctrine's JSON type does: an empty PHP array becomes `[]`.
func EncodeMeta(meta map[string]any) string {
	if len(meta) == 0 {
		return "[]"
	}
	// domain_id first, then authority, matching RoleDefinition::forDomain()
	var b strings.Builder
	b.WriteByte('{')
	first := true
	write := func(k string, v any) {
		if !first {
			b.WriteByte(',')
		}
		first = false
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(v)
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	for _, k := range []string{"domain_id", "authority"} {
		if v, ok := meta[k]; ok {
			write(k, v)
		}
	}
	for k, v := range meta {
		if k != "domain_id" && k != "authority" {
			write(k, v)
		}
	}
	b.WriteByte('}')
	return b.String()
}

// apiKeyShortURLConditions ports ApiKey::spec() applied to a short URL alias: AUTHORED_SHORT_URLS restricts by author,
// DOMAIN_SPECIFIC by domain id.
func apiKeyShortURLConditions(k *APIKey, alias string) (conds []string, args []any) {
	if k == nil {
		return nil, nil
	}
	for _, r := range k.Roles {
		switch r.Role {
		case RoleAuthoredShortURLs:
			conds = append(conds, alias+".author_api_key_id = ?")
			args = append(args, k.ID)
		case RoleDomainSpecific:
			conds = append(conds, alias+".domain_id = ?")
			args = append(args, domainIDArg(r.DomainID()))
		}
	}
	return conds, args
}

func domainIDArg(id string) any {
	if n, err := strconv.ParseInt(id, 10, 64); err == nil {
		return n
	}
	return id
}

// where joins conditions with AND, returning "" for none.
func where(conds []string) string {
	if len(conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conds, " AND ")
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
