package core

import "context"

// CrawlableShortCodes ports CrawlableShortCodesQuery: distinct short codes of crawlable short URLs, ordered.
func (s *Store) CrawlableShortCodes(ctx context.Context) ([]string, error) {
	rows, err := s.DB.Query(ctx, s.DB.DB, "SELECT DISTINCT short_code FROM short_urls WHERE crawlable = ? ORDER BY short_code", true)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	codes := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}
