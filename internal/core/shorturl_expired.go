package core

import (
	"context"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/db"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// ExpiredShortURLsConditions ports ShortUrl\Model\ExpiredShortUrlsConditions.
type ExpiredShortURLsConditions struct {
	PastValidUntil   bool
	MaxVisitsReached bool
}

// expiredConditions ports ExpiredShortUrlsRepository::applyConditions(): the WHERE clause, or "" when there are no
// conditions.
func (s *Store) expiredConditions(c ExpiredShortURLsConditions) (string, []any) {
	var conds []string
	var args []any
	if c.PastValidUntil {
		conds = append(conds, "(valid_until IS NOT NULL AND valid_until < ?)")
		args = append(args, phpx.DateTimeString(s.NowIn()))
	}
	if c.MaxVisitsReached {
		conds = append(conds, "(max_visits IS NOT NULL AND max_visits <= "+
			"(SELECT COALESCE(SUM(vc.count), 0) FROM short_url_visits_counts vc WHERE vc.short_url_id = short_urls.id))")
	}
	return strings.Join(conds, " OR "), args
}

// DeleteExpiredShortURLs ports ExpiredShortUrlsRepository::delete(): a single bulk DELETE, which the ORM runs without
// cascading to related rows itself.
func (s *Store) DeleteExpiredShortURLs(ctx context.Context, c ExpiredShortURLsConditions) (int, error) {
	where, args := s.expiredConditions(c)
	if where == "" {
		return 0, nil
	}
	res, err := s.DB.Exec(ctx, s.DB.DB, "DELETE FROM short_urls WHERE "+where, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// CountExpiredShortURLs ports ExpiredShortUrlsRepository::dryCount().
func (s *Store) CountExpiredShortURLs(ctx context.Context, c ExpiredShortURLsConditions) (int, error) {
	where, args := s.expiredConditions(c)
	if where == "" {
		return 0, nil
	}
	var n db.Int
	err := s.DB.QueryRow(ctx, s.DB.DB, "SELECT COUNT(id) FROM short_urls WHERE "+where, args...).Scan(&n)
	return int(n), err
}
