// Command seed fills a PostgreSQL database that Shlink already created and migrated with the benchmark data set:
// 5 domains, 200 tags, 100k short URLs and 1M visits, deterministic for a given seed. It also writes the request
// inputs the k6 scenarios pick from.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	domainsCount = 5
	tagsCount    = 200
	alphabet     = "23456789abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ"
	// BenchAPIKey is the plain API key the scenarios use; it is fake and only valid in the benchmark database.
	BenchAPIKey = "bench-api-key-0000-0000-000000000000"
)

var words = []string{"go", "php", "cloud", "news", "sport", "music", "travel", "food", "tech", "science", "health",
	"games", "movies", "books", "design", "finance", "jobs", "events", "photos", "docs"}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Safari/537.36",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Mobile Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
	"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
}

// shortCode derives a unique code from an index, so codes never collide.
func shortCode(i int) string {
	b := []byte{}
	n := i + 1_000_000
	for n > 0 {
		b = append(b, alphabet[n%len(alphabet)])
		n /= len(alphabet)
	}
	return string(b)
}

// zipf picks an index in [0, n) with a skewed popularity, like real short URL traffic.
type zipf struct{ z *rand.Zipf }

func newZipf(r *rand.Rand, n int) zipf { return zipf{rand.NewZipf(r, 1.1, 1, uint64(n-1))} }
func (z zipf) next() int               { return int(z.z.Uint64()) }

func main() {
	dsn := flag.String("dsn", "postgres://postgres:root@127.0.0.1:35432/shlink?sslmode=disable", "PostgreSQL DSN")
	urls := flag.Int("short-urls", 100_000, "short URLs to create")
	visits := flag.Int("visits", 1_000_000, "visits to create")
	out := flag.String("out", "bench/data", "directory for the scenario inputs")
	seed := flag.Uint64("seed", 42, "random seed")
	flag.Parse()

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, *dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close(ctx)
	r := rand.New(rand.NewPCG(*seed, *seed))
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	start := time.Now()

	exec := func(sql string, args ...any) {
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			log.Fatalf("%s: %v", sql, err)
		}
	}
	copyRows := func(table string, cols []string, rows [][]any) {
		if _, err := conn.CopyFrom(ctx, pgx.Identifier{table}, cols, pgx.CopyFromRows(rows)); err != nil {
			log.Fatalf("copy %s: %v", table, err)
		}
	}

	sum := sha256.Sum256([]byte(BenchAPIKey))
	exec(`INSERT INTO api_keys (id, "key", "name", enabled) VALUES (1, $1, 'bench', true)`, hex.EncodeToString(sum[:]))

	var rows [][]any
	for i := 1; i <= domainsCount; i++ {
		rows = append(rows, []any{int64(i), fmt.Sprintf("d%d.test", i)})
	}
	copyRows("domains", []string{"id", "authority"}, rows)

	rows = nil
	tags := make([]string, tagsCount)
	for i := range tags {
		tags[i] = fmt.Sprintf("%s-%03d", words[i%len(words)], i)
		rows = append(rows, []any{int64(i + 1), tags[i]})
	}
	copyRows("tags", []string{"id", "name"}, rows)

	rows = nil
	var tagRows [][]any
	tagPick := newZipf(r, tagsCount)
	codes := make([]string, *urls)
	for i := 0; i < *urls; i++ {
		codes[i] = shortCode(i)
		word := words[r.IntN(len(words))]
		long := fmt.Sprintf("https://example.com/%s/articles/%d?ref=shlink", word, i)
		hash := sha256.Sum256([]byte(long))
		var title, domain any
		if r.IntN(2) == 0 {
			title = fmt.Sprintf("Article %d about %s", i, word)
		}
		if r.IntN(5) == 0 {
			domain = int64(r.IntN(domainsCount) + 1)
		}
		created := now.Add(-time.Duration(r.Int64N(int64(2 * 365 * 24 * time.Hour)))).Truncate(time.Second)
		rows = append(rows, []any{int64(i + 1), long, hash[:], codes[i], created, title, r.IntN(10) == 0, domain})
		seen := map[int]bool{}
		for range r.IntN(4) {
			t := tagPick.next()
			if !seen[t] {
				seen[t] = true
				tagRows = append(tagRows, []any{int64(i + 1), int64(t + 1)})
			}
		}
	}
	copyRows("short_urls", []string{"id", "original_url", "long_url_hash", "short_code", "date_created", "title",
		"crawlable", "domain_id"}, rows)
	copyRows("short_urls_in_tags", []string{"short_url_id", "tag_id"}, tagRows)

	urlPick := newZipf(r, *urls)
	orphanTypes := []string{"base_url", "regular_404", "invalid_short_url"}
	const batch = 100_000
	for done := 0; done < *visits; done += batch {
		rows = rows[:0]
		for i := done; i < min(done+batch, *visits); i++ {
			ua := userAgents[r.IntN(len(userAgents))]
			bot := ua == userAgents[len(userAgents)-1]
			date := now.Add(-time.Duration(r.Int64N(int64(365 * 24 * time.Hour)))).Truncate(time.Second)
			remote := fmt.Sprintf("10.%d.%d.0", r.IntN(256), r.IntN(256))
			if r.IntN(20) == 0 {
				typ := orphanTypes[r.IntN(len(orphanTypes))]
				rows = append(rows, []any{int64(i + 1), nil, typ, ua, "", bot, remote, "http://s.test/missing", date})
				continue
			}
			rows = append(rows, []any{int64(i + 1), int64(urlPick.next() + 1), "valid_short_url", ua,
				"https://referrer.test/", bot, remote, nil, date})
		}
		copyRows("visits", []string{"id", "short_url_id", "type", "user_agent", "referer", "potential_bot",
			"remote_addr", "visited_url", "date"}, rows)
	}

	// Counters as ShortUrlVisitsCountTracker and OrphanVisitsCountTracker leave them, in one slot per group
	exec(`INSERT INTO short_url_visits_counts (short_url_id, potential_bot, slot_id, count)
		SELECT short_url_id, potential_bot, 1, COUNT(*) FROM visits WHERE short_url_id IS NOT NULL GROUP BY short_url_id, potential_bot`)
	exec(`INSERT INTO orphan_visits_counts (potential_bot, slot_id, count)
		SELECT potential_bot, 1, COUNT(*) FROM visits WHERE short_url_id IS NULL GROUP BY potential_bot`)
	for _, t := range []string{"api_keys", "domains", "tags", "short_urls", "visits"} {
		exec(fmt.Sprintf(`SELECT setval(pg_get_serial_sequence('%s', 'id'), (SELECT MAX(id) FROM %s))`, t, t))
	}
	exec("ANALYZE")

	// Scenario inputs: popular codes (weighted like the visits), all tags and search words
	sample := make([]string, 0, 5000)
	popular := newZipf(rand.New(rand.NewPCG(*seed+1, *seed+1)), *urls)
	for len(sample) < cap(sample) {
		i := popular.next()
		sample = append(sample, codes[i])
	}
	// Only default-domain codes resolve without a Host header; keep the ones without a domain
	var defaultCodes []string
	rowsQ, err := conn.Query(ctx, "SELECT short_code FROM short_urls WHERE domain_id IS NULL AND short_code = ANY($1)", sample)
	if err != nil {
		log.Fatal(err)
	}
	set := map[string]bool{}
	for rowsQ.Next() {
		var c string
		_ = rowsQ.Scan(&c)
		set[c] = true
	}
	rowsQ.Close()
	for _, c := range sample {
		if set[c] {
			defaultCodes = append(defaultCodes, c)
		}
	}
	inputs := map[string]any{"apiKey": BenchAPIKey, "codes": defaultCodes, "tags": tags, "words": words,
		"pages": int(math.Ceil(float64(*urls) / 20))}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	raw, _ := json.Marshal(inputs)
	if err := os.WriteFile(filepath.Join(*out, "inputs.json"), raw, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("seeded %d short URLs, %d visits in %s", *urls, *visits, time.Since(start).Round(time.Second))
}
