package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/config"
	"github.com/NamTranQuoc/shlink-go/internal/db"
)

const shortURLFrom = " FROM short_urls s LEFT JOIN domains d ON d.id = s.domain_id"

// FindShortURLWithDomainFallback ports ShortUrlRepository::findOneWithDomainFallback(): the short URL matching both
// short code and domain wins over the one with the same short code and no domain.
func (s *Store) FindShortURLWithDomainFallback(ctx context.Context, id ShortURLIdentifier, mode config.ShortURLMode) (*ShortURL, error) {
	codeCond := "s.short_code = ?"
	code := id.ShortCode
	if mode != config.ModeStrict {
		codeCond = "LOWER(s.short_code) = ?"
		code = strings.ToLower(code)
	}
	query := "SELECT " + shortURLColumns + shortURLFrom + " WHERE " + codeCond
	args := []any{code}
	var order string
	if id.Domain == nil {
		query += " AND s.domain_id IS NULL"
		order = " ORDER BY s.id"
	} else {
		query += " AND (s.domain_id IS NULL OR d.authority = ?)"
		args = append(args, *id.Domain)
		// When ordering DESC, Postgres puts nulls first while the rest of engines put them last
		dir := "DESC"
		if s.DB.Driver == db.Postgres {
			dir = "ASC"
		}
		order = " ORDER BY s.domain_id " + dir
	}
	one := 1
	row := s.DB.QueryRow(ctx, s.DB.DB, s.DB.Limit(query+order, &one, nil), args...)
	su, err := s.scanShortURL(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return su, err
}

// findOneConditions ports createFindOneQueryBuilder(): exact short code, exact domain (or no domain) and API key spec.
func findOneConditions(id ShortURLIdentifier, apiKey *APIKey) ([]string, []any) {
	conds := []string{"s.short_code IS NOT NULL", "s.short_code = ?"}
	args := []any{id.ShortCode}
	if id.Domain != nil {
		conds = append(conds, "d.authority = ?")
		args = append(args, *id.Domain)
	} else {
		conds = append(conds, "s.domain_id IS NULL")
	}
	keyConds, keyArgs := apiKeyShortURLConditions(apiKey, "s")
	return append(conds, keyConds...), append(args, keyArgs...)
}

// FindShortURL ports ShortUrlRepository::findOne(), with tags loaded.
func (s *Store) FindShortURL(ctx context.Context, q db.Querier, id ShortURLIdentifier, apiKey *APIKey) (*ShortURL, error) {
	conds, args := findOneConditions(id, apiKey)
	one := 1
	query := s.DB.Limit("SELECT "+shortURLColumns+shortURLFrom+where(conds)+" ORDER BY s.id", &one, nil)
	su, err := s.scanShortURL(s.DB.QueryRow(ctx, q, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return su, s.loadTags(ctx, q, su)
}

// ShortCodeIsInUse ports shortCodeIsInUse()/shortCodeIsInUseWithLock(). The lock is a row lock taken inside the
// caller's transaction (SQLite serializes writers anyway).
func (s *Store) ShortCodeIsInUse(ctx context.Context, q db.Querier, id ShortURLIdentifier, lock bool) (bool, error) {
	conds, args := findOneConditions(id, nil)
	query := "SELECT s.id" + shortURLFrom + where(conds)
	one := 1
	switch {
	case lock && s.DB.Driver == db.MSSQL:
		query = strings.Replace(query, "FROM short_urls s", "FROM short_urls s WITH (UPDLOCK, ROWLOCK)", 1)
		query = s.DB.Limit(query+" ORDER BY s.id", &one, nil)
	case lock && s.DB.Driver == db.Postgres:
		// Postgres cannot lock the nullable side of the domain outer join
		query = s.DB.Limit(query+" ORDER BY s.id", &one, nil) + " FOR UPDATE OF s"
	case lock && s.DB.Driver != db.SQLite:
		query = s.DB.Limit(query+" ORDER BY s.id", &one, nil) + " FOR UPDATE"
	default:
		query = s.DB.Limit(query+" ORDER BY s.id", &one, nil)
	}
	var found int64
	err := s.DB.QueryRow(ctx, q, query, args...).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// FindOneMatching ports ShortUrlRepository::findOneMatching(), used by findIfExists: same long URL and the same value
// for every optional param that was provided, and exactly the same tags when tags were provided.
func (s *Store) FindOneMatching(ctx context.Context, c *ShortURLCreation) (*ShortURL, error) {
	return s.findOneMatchingQuery(ctx, c)
}

// findOneMatchingQuery builds the query; tag joins bind their params before the WHERE params.
func (s *Store) findOneMatchingQuery(ctx context.Context, c *ShortURLCreation) (*ShortURL, error) {
	var joinArgs, whereArgs []any
	query := "SELECT " + shortURLColumns + shortURLFrom
	for i, tag := range c.Tags {
		n := itoa(i)
		query += " INNER JOIN short_urls_in_tags stj_" + n + " ON stj_" + n + ".short_url_id = s.id" +
			" INNER JOIN tags t_" + n + " ON t_" + n + ".id = stj_" + n + ".tag_id AND t_" + n + ".name = ?"
		joinArgs = append(joinArgs, tag)
	}
	conds := []string{"s.long_url_hash = ?"}
	whereArgs = append(whereArgs, LongURLHash(c.LongURL))
	if c.CustomSlug != nil {
		conds = append(conds, "s.short_code = ?")
		whereArgs = append(whereArgs, *c.CustomSlug)
	}
	if c.MaxVisits != nil {
		conds = append(conds, "s.max_visits = ?")
		whereArgs = append(whereArgs, *c.MaxVisits)
	}
	if c.ValidSince != nil {
		conds = append(conds, "s.valid_since = ?")
		whereArgs = append(whereArgs, s.DB.FormatTime(*c.ValidSince))
	}
	if c.ValidUntil != nil {
		conds = append(conds, "s.valid_until = ?")
		whereArgs = append(whereArgs, s.DB.FormatTime(*c.ValidUntil))
	}
	if c.Domain != nil {
		conds = append(conds, "d.id IS NOT NULL", "d.authority = ?")
		whereArgs = append(whereArgs, *c.Domain)
	}
	keyConds, keyArgs := apiKeyShortURLConditions(c.APIKey, "s")
	conds = append(conds, keyConds...)
	whereArgs = append(whereArgs, keyArgs...)
	query += where(conds)
	args := append(joinArgs, whereArgs...)
	if len(c.Tags) > 0 {
		// Discard short URLs that have more tags than the provided ones
		query += " AND (SELECT COUNT(*) FROM short_urls_in_tags sc WHERE sc.short_url_id = s.id) = ?"
		args = append(args, len(c.Tags))
	}
	one := 1
	su, err := s.scanShortURL(s.DB.QueryRow(ctx, s.DB.DB, s.DB.Limit(query+" ORDER BY s.id", &one, nil), args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return su, s.loadTags(ctx, s.DB.DB, su)
}

// CountVisits counts the visits of a short URL, like the ORM's extra-lazy count($shortUrl->visits).
func (s *Store) CountVisits(ctx context.Context, q db.Querier, shortURLID int64) (int, error) {
	var n db.Int
	err := s.DB.QueryRow(ctx, q, "SELECT COUNT(*) FROM visits WHERE short_url_id = ?", shortURLID).Scan(&n)
	return int(n), err
}

// CountVisitsSummary returns total and non-bot visits counted from the visits table.
func (s *Store) CountVisitsSummary(ctx context.Context, shortURLID int64) (VisitsSummary, error) {
	var total, nonBots db.Int
	err := s.DB.QueryRow(ctx, s.DB.DB, "SELECT COUNT(*), COALESCE(SUM(CASE WHEN potential_bot = ? THEN 1 ELSE 0 END), 0) FROM visits WHERE short_url_id = ?",
		false, shortURLID).Scan(&total, &nonBots)
	return VisitsSummary{Total: int(total), NonBots: int(nonBots)}, err
}

// HasRedirectRules tells if a short URL has at least one redirect rule.
func (s *Store) HasRedirectRules(ctx context.Context, shortURLID int64) (bool, error) {
	var n db.Int
	err := s.DB.QueryRow(ctx, s.DB.DB, "SELECT COUNT(*) FROM short_url_redirect_rules WHERE short_url_id = ?", shortURLID).Scan(&n)
	return n > 0, err
}

func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return digits[i : i+1]
	}
	return itoa(i/10) + digits[i%10:i%10+1]
}
