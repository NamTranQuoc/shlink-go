package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// SeedFixtures writes the same rows upstream's API test fixtures produce (module/Rest/test-api/Fixtures), in the same
// order Doctrine's fixture loader resolves them: domains, tags, api keys, short URLs, visits, redirect rules.
func (d *DB) SeedFixtures() error {
	s := seeder{db: d, now: time.Now().UTC()}
	steps := []func() error{s.domains, s.tags, s.apiKeys, s.shortUrls, s.visits, s.redirectRules}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

const dateLayout = "2006-01-02 15:04:05"

type seeder struct {
	db       *DB
	now      time.Time
	domains_ map[string]int64
	tags_    map[string]int64
	keys     map[string]int64
	urls     map[string]int64
}

func (s *seeder) domains() error {
	s.domains_ = map[string]int64{}
	rows := []struct {
		authority     string
		baseURL, n404 any
	}{
		{"example.com", nil, nil},
		{"this_domain_is_detached.com", nil, nil},
		{"detached-with-redirects.com", "foo.com", "bar.com"},
	}
	for _, r := range rows {
		id, err := s.db.Insert("domains",
			[]string{"authority", "base_url_redirect", "regular_not_found_redirect", "invalid_short_url_redirect"},
			r.authority, r.baseURL, r.n404, nil)
		if err != nil {
			return err
		}
		s.domains_[r.authority] = id
	}
	return nil
}

func (s *seeder) tags() error {
	s.tags_ = map[string]int64{}
	for _, name := range []string{"foo", "bar", "baz"} {
		id, err := s.db.Insert("tags", []string{"name"}, name)
		if err != nil {
			return err
		}
		s.tags_[name] = id
	}
	return nil
}

func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (s *seeder) apiKeys() error {
	s.keys = map[string]int64{}
	startOfYesterday := time.Date(s.now.Year(), s.now.Month(), s.now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	exampleDomainID := s.domains_["example.com"]
	type role struct{ name, meta string }
	keys := []struct {
		key     string
		enabled bool
		expires any
		roles   []role
	}{
		{"valid_api_key", true, nil, nil},
		{"disabled_api_key", false, nil, nil},
		{"expired_api_key", true, startOfYesterday.Format(dateLayout), nil},
		{"author_api_key", true, nil, []role{{"AUTHORED_SHORT_URLS", "[]"}}},
		{"domain_api_key", true, nil, []role{{
			"DOMAIN_SPECIFIC",
			fmt.Sprintf(`{"domain_id":"%d","authority":"example.com"}`, exampleDomainID),
		}}},
		{"no_orphans_api_key", true, nil, []role{{"NO_ORPHAN_VISITS", "[]"}}},
	}
	for _, k := range keys {
		id, err := s.db.Insert("api_keys", []string{"key", "name", "expiration_date", "enabled"},
			HashKey(k.key), k.key, k.expires, k.enabled)
		if err != nil {
			return err
		}
		s.keys[k.key] = id
		for _, r := range k.roles {
			if _, err := s.db.Insert("api_key_roles", []string{"role_name", "meta", "api_key_id"}, r.name, r.meta, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func longURLHash(longURL string) []byte {
	sum := sha256.Sum256([]byte(longURL))
	return sum[:]
}

func (s *seeder) domainID(authority string) (any, error) {
	if authority == "" {
		return nil, nil
	}
	if id, ok := s.domains_[authority]; ok {
		return id, nil
	}
	id, err := s.db.Insert("domains",
		[]string{"authority", "base_url_redirect", "regular_not_found_redirect", "invalid_short_url_redirect"},
		authority, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	s.domains_[authority] = id
	return id, nil
}

func (s *seeder) shortUrls() error {
	s.urls = map[string]int64{}
	author := s.keys["author_api_key"]
	type su struct {
		ref, longURL, slug, domain, created string
		validSince, validUntil, title       any
		maxVisits                           any
		author                              any
		tags                                []string
		crawlable, forwardQuery             bool
	}
	urls := []su{
		{ref: "abc123", longURL: "https://shlink.io", slug: "abc123", maxVisits: 2, author: author,
			tags: []string{"foo"}, title: "My cool title", crawlable: true, forwardQuery: true, created: "2018-05-01 00:00:00"},
		{ref: "def456", longURL: "https://blog.alejandrocelaya.com/2017/12/09/acmailer-7-0-the-most-important-release-in-a-long-time/",
			validSince: "2020-05-01 00:00:00", slug: "def456", author: author, tags: []string{"foo", "bar"},
			forwardQuery: true, created: "2019-01-01 00:00:10"},
		{ref: "custom", longURL: "https://shlink.io", slug: "custom", maxVisits: 2, author: author, crawlable: true,
			forwardQuery: false, created: "2019-01-01 00:00:20"},
		{ref: "ghi789", longURL: "https://shlink.io/documentation/", validUntil: "2020-05-01 00:00:00", slug: "ghi789",
			forwardQuery: true, created: "2018-05-01 00:00:00"},
		{ref: "example", longURL: "https://blog.alejandrocelaya.com/2019/04/27/considerations-to-properly-use-open-source-software-projects/",
			slug: "ghi789", domain: "example.com", tags: []string{"foo"}, forwardQuery: true, created: "2019-01-01 00:00:30"},
		{ref: "custom-with-domain", longURL: "https://google.com", slug: "custom-with-domain", domain: "some-domain.com",
			forwardQuery: true, created: "2018-10-20 00:00:00"},
	}
	for _, u := range urls {
		domainID, err := s.domainID(u.domain)
		if err != nil {
			return err
		}
		id, err := s.db.Insert("short_urls", []string{
			"original_url", "long_url_hash", "short_code", "date_created", "valid_since", "valid_until", "max_visits",
			"import_source", "import_original_short_code", "title", "title_was_auto_resolved", "crawlable",
			"forward_query", "domain_id", "author_api_key_id",
		}, u.longURL, longURLHash(u.longURL), u.slug, u.created, u.validSince, u.validUntil, u.maxVisits,
			nil, nil, u.title, false, u.crawlable, u.forwardQuery, domainID, u.author)
		if err != nil {
			return err
		}
		s.urls[u.ref] = id
		for _, t := range u.tags {
			if err := s.db.InsertNoID("short_urls_in_tags", []string{"short_url_id", "tag_id"}, id, s.tags_[t]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *seeder) visits() error {
	now := s.now.Format(dateLayout)
	type visit struct {
		shortURL         string
		typ, ua, referer string
		remoteAddr       any
		visitedURL       string
		bot              bool
		date             string
	}
	visits := []visit{
		{"abc123", "valid_short_url", "shlink-tests-agent", "", "44.55.66.0", "", false, now},
		{"abc123", "valid_short_url", "shlink-tests-agent", "https://google.com", "4.5.6.0", "", false, now},
		{"abc123", "valid_short_url", "shlink-tests-agent", "", "1.2.3.0", "", false, now},
		{"def456", "valid_short_url", "cf-facebook", "", "127.0.0.1", "", true, now},
		{"def456", "valid_short_url", "shlink-tests-agent", "https://app.shlink.io", nil, "", false, now},
		{"ghi789", "valid_short_url", "shlink-tests-agent", "", "1.2.3.0", "", false, now},
		{"ghi789", "valid_short_url", "shlink-tests-agent", "https://app.shlink.io", nil, "", false, now},
		{"", "base_url", "shlink-tests-agent", "https://s.test", "1.2.3.0", "https://s.test/foo", false, "2020-01-01 00:00:00"},
		{"", "regular_404", "shlink-tests-agent", "https://s.test/foo/bar", "1.2.3.0", "https://s.test/bar", false, "2020-02-01 00:00:00"},
		{"", "invalid_short_url", "cf-facebook", "https://s.test/foo", "1.2.3.0", "https://example.com/short", true, "2020-03-01 00:00:00"},
	}
	type countKey struct {
		shortURL int64
		bot      bool
	}
	counts := map[countKey]int64{}
	var countOrder []countKey
	// Upstream's OrphanVisitsCountTracker listener fills orphan_visits_counts when the orphan visits are flushed.
	orphanCounts := map[bool]int64{}
	var orphanOrder []bool
	for _, v := range visits {
		var shortURLID any
		if v.shortURL == "" {
			if _, ok := orphanCounts[v.bot]; !ok {
				orphanOrder = append(orphanOrder, v.bot)
			}
			orphanCounts[v.bot]++
		} else {
			id := s.urls[v.shortURL]
			shortURLID = id
			k := countKey{id, v.bot}
			if _, ok := counts[k]; !ok {
				countOrder = append(countOrder, k)
			}
			counts[k]++
		}
		_, err := s.db.Insert("visits", []string{
			"referer", "date", "remote_addr", "user_agent", "visited_url", "type", "potential_bot", "redirect_url",
			"short_url_id", "visit_location_id",
		}, v.referer, v.date, v.remoteAddr, v.ua, v.visitedURL, v.typ, v.bot, nil, shortURLID, nil)
		if err != nil {
			return err
		}
	}
	for _, k := range countOrder {
		if _, err := s.db.Insert("short_url_visits_counts", []string{"short_url_id", "potential_bot", "slot_id", "count"},
			k.shortURL, k.bot, 1, counts[k]); err != nil {
			return err
		}
	}
	for _, bot := range orphanOrder {
		if _, err := s.db.Insert("orphan_visits_counts", []string{"potential_bot", "slot_id", "count"},
			bot, 1, orphanCounts[bot]); err != nil {
			return err
		}
	}
	return nil
}

func (s *seeder) redirectRules() error {
	def := s.urls["def456"]
	type cond struct {
		typ        string
		key, value any
	}
	rules := []struct {
		priority int
		longURL  string
		conds    []cond
	}{
		{2, "https://example.com/multiple-query-params", []cond{{"query-param", "hello", "world"}, {"query-param", "foo", "bar"}}},
		{1, "https://example.com/english-and-foo-query", []cond{{"language", nil, "en"}, {"any-value-query-param", "foo", nil}}},
		{4, "android://foo/bar", []cond{{"device", nil, "android"}}},
		{3, "https://example.com/only-english", []cond{{"language", nil, "en"}}},
		{5, "fb://profile/33138223345", []cond{{"device", nil, "ios"}}},
		{6, "https://example.com/static-ip-address", []cond{{"ip-address", nil, "1.2.3.4"}}},
		{7, "https://example.com/linux", []cond{{"device", nil, "linux"}}},
	}
	for _, r := range rules {
		ruleID, err := s.db.Insert("short_url_redirect_rules", []string{"priority", "long_url", "short_url_id"}, r.priority, r.longURL, def)
		if err != nil {
			return err
		}
		for _, c := range r.conds {
			condID, err := s.db.Insert("redirect_conditions", []string{"type", "match_key", "match_value"}, c.typ, c.key, c.value)
			if err != nil {
				return err
			}
			if err := s.db.InsertNoID("redirect_conditions_in_short_url_redirect_rules",
				[]string{"short_url_redirect_rule_id", "redirect_condition_id"}, ruleID, condID); err != nil {
				return err
			}
		}
	}
	return nil
}
