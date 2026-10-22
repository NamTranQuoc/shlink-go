package harness

import (
	"encoding/json"
	"net/http"
)

// ImportSourcesPath prefixes the fake third party APIs short-url:import reads from: a Shlink instance, a YOURLS
// instance with the shlink import plugin, and a Kutt.it instance. Only the "valid" API key is accepted.
const ImportSourcesPath = "/import-sources"

// ImportSourcesAPIKey is the API key the fake Shlink and Kutt.it instances accept.
const ImportSourcesAPIKey = "import_api_key"

func registerImportSources(mux *http.ServeMux) {
	writeJSON := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	authorized := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("X-Api-Key") != ImportSourcesAPIKey {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"title": "Invalid API key", "status": 401})
			return false
		}
		return true
	}

	// Shlink: two pages of short URLs, visits for one of them, redirect rules (and an instance without them), and
	// orphan visits. The "broken" instance fails when the second page is requested.
	for _, instance := range []string{"shlink", "shlink-broken", "shlink-invalid-device", "shlink-invalid-browser"} {
		base := ImportSourcesPath + "/" + instance + "/rest/v3"
		broken := instance == "shlink-broken"
		mux.HandleFunc("GET "+base+"/short-urls", func(w http.ResponseWriter, r *http.Request) {
			if !authorized(w, r) {
				return
			}
			page := r.URL.Query().Get("page")
			if page == "2" && broken {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"title": "Internal error"})
				return
			}
			data := shlinkSourcePage1
			if page == "2" {
				data = shlinkSourcePage2
			}
			current := 1
			if page == "2" {
				current = 2
			}
			writeJSON(w, http.StatusOK, map[string]any{"shortUrls": map[string]any{
				"data":       data,
				"pagination": map[string]any{"currentPage": current, "pagesCount": 2, "itemsPerPage": 50, "itemsInCurrentPage": len(data), "totalItems": 3},
			}})
		})
		mux.HandleFunc("GET "+base+"/short-urls/{shortCode}/visits", func(w http.ResponseWriter, r *http.Request) {
			if !authorized(w, r) {
				return
			}
			visits := []any{}
			if r.PathValue("shortCode") == "imported-1" {
				visits = shlinkSourceVisits
			}
			writeJSON(w, http.StatusOK, map[string]any{"visits": map[string]any{"data": visits}})
		})
		mux.HandleFunc("GET "+base+"/short-urls/{shortCode}/redirect-rules", func(w http.ResponseWriter, r *http.Request) {
			if !authorized(w, r) {
				return
			}
			if r.PathValue("shortCode") != "imported-1" {
				writeJSON(w, http.StatusNotFound, map[string]any{"title": "Not found"})
				return
			}
			rules := shlinkSourceRules
			switch instance {
			case "shlink-invalid-device":
				rules = []any{link("longUrl", "https://shlink.io/watch", "priority", 1, "conditions", []any{
					link("type", "device", "matchKey", nil, "matchValue", "smartwatch")})}
			case "shlink-invalid-browser":
				rules = []any{link("longUrl", "https://shlink.io/browser", "priority", 1, "conditions", []any{
					link("type", "language", "matchKey", nil, "matchValue", "en"),
					link("type", "browser", "matchKey", nil, "matchValue", "netscape")})}
			}
			writeJSON(w, http.StatusOK, map[string]any{"defaultLongUrl": "https://shlink.io", "redirectRules": rules})
		})
		mux.HandleFunc("GET "+base+"/visits", func(w http.ResponseWriter, r *http.Request) {
			if !authorized(w, r) {
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"visits": map[string]any{
				"nonOrphanVisits": map[string]any{"total": 2}, "orphanVisits": map[string]any{"total": len(shlinkSourceOrphanVisits)},
			}})
		})
		mux.HandleFunc("GET "+base+"/visits/orphan", func(w http.ResponseWriter, r *http.Request) {
			if !authorized(w, r) {
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"visits": map[string]any{"data": shlinkSourceOrphanVisits}})
		})
	}

	// YOURLS with the shlink import plugin, and one without it.
	mux.HandleFunc("GET "+ImportSourcesPath+"/yourls/yourls-api.php", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("username") != "admin" || q.Get("password") != "secret" {
			writeJSON(w, http.StatusForbidden, map[string]any{"message": "Please log in", "errorCode": 403})
			return
		}
		switch q.Get("action") {
		case "shlink-list":
			writeJSON(w, http.StatusOK, map[string]any{"result": yourlsSourceLinks})
		case "shlink-link-visits":
			visits := []any{}
			if q.Get("shortCode") == "yourls-1" {
				visits = yourlsSourceVisits
			}
			writeJSON(w, http.StatusOK, map[string]any{"result": visits})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"errorCode": 400, "message": `Unknown or missing "action" parameter`})
		}
	})
	mux.HandleFunc("GET "+ImportSourcesPath+"/yourls-without-plugin/yourls-api.php", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errorCode": 400, "message": `Unknown or missing "action" parameter`})
	})

	// Kutt.it: two pages, the second one only when anonymous links are included.
	mux.HandleFunc("GET "+ImportSourcesPath+"/kutt/api/v2/links", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		q := r.URL.Query()
		data, total := kuttSourceLinks, len(kuttSourceLinks)
		if q.Get("all") == "true" {
			total = 51
			if q.Get("skip") == "50" {
				data = kuttSourceAnonymousLinks
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"limit": 50, "skip": q.Get("skip"), "total": total, "data": data})
	})
}

func link(fields ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(fields); i += 2 {
		m[fields[i].(string)] = fields[i+1]
	}
	return m
}

var (
	shlinkSourcePage1 = []any{
		link("shortCode", "imported-1", "shortUrl", "https://source.test/imported-1", "longUrl", "https://shlink.io/imported-1",
			"dateCreated", "2021-03-04T05:06:07+00:00", "tags", []string{"foo", "Imported Tag"},
			"meta", link("validSince", nil, "validUntil", "2040-01-01T00:00:00+02:00", "maxVisits", 50),
			"domain", nil, "title", "Imported title", "crawlable", false, "forwardQuery", true,
			"visitsSummary", link("total", 3, "nonBots", 2, "bots", 1)),
		link("shortCode", "abc123", "shortUrl", "https://source.test/abc123", "longUrl", "https://shlink.io/conflicting",
			"dateCreated", "2021-03-05T00:00:00+00:00", "tags", []string{},
			"meta", link("validSince", "2021-01-01T00:00:00+00:00", "validUntil", nil, "maxVisits", nil),
			"domain", nil, "title", nil, "visitsCount", 0),
	}
	shlinkSourcePage2 = []any{
		link("shortCode", "imported-3", "shortUrl", "https://other.test/imported-3", "longUrl", "https://shlink.io/imported-3",
			"dateCreated", "2022-01-01T00:00:00+00:00", "tags", []string{"bar"},
			"meta", link("validSince", nil, "validUntil", nil, "maxVisits", nil),
			"domain", "imported.example.com", "title", nil, "visitsSummary", link("total", 0, "nonBots", 0, "bots", 0)),
	}
	// Newest first, like Shlink returns them
	shlinkSourceVisits = []any{
		link("referer", "https://google.com", "date", "2021-05-03T00:00:00+00:00", "userAgent", "Googlebot/2.1",
			"visitLocation", nil, "potentialBot", true),
		link("referer", "", "date", "2021-05-02T00:00:00+00:00", "userAgent", "Mozilla/5.0 (X11; Linux x86_64) Firefox/120.0",
			"visitLocation", link("countryCode", "ES", "countryName", "Spain", "regionName", "Madrid", "cityName", "Madrid",
				"latitude", 40.4, "longitude", -3.7, "timezone", "Europe/Madrid", "isEmpty", false), "potentialBot", false),
		link("referer", "https://example.com", "date", "2021-05-01T00:00:00+00:00", "userAgent", "curl/8.0", "visitLocation", nil),
	}
	shlinkSourceRules = []any{
		link("longUrl", "https://shlink.io/android", "priority", 1, "conditions", []any{
			link("type", "device", "matchKey", nil, "matchValue", "android"),
			link("type", "unknown-condition", "matchKey", nil, "matchValue", "ignored"),
		}),
		link("longUrl", "https://shlink.io/query", "priority", 2, "conditions", []any{
			link("type", "query-param", "matchKey", "foo", "matchValue", "bar"),
			link("type", "any-value-query-param", "matchKey", "baz", "matchValue", "baz"),
			link("type", "before-date", "matchKey", nil, "matchValue", "2030-01-01T10:00:00+02:00"),
		}),
	}
	// Dated in the future, so they are newer than the orphan visits in the fixtures
	shlinkSourceOrphanVisits = []any{
		link("referer", "", "date", "2099-06-02T00:00:00+00:00", "userAgent", "Mozilla/5.0", "visitLocation", nil,
			"visitedUrl", "https://source.test/foo", "type", "regular_404"),
		link("referer", "", "date", "2099-06-01T00:00:00+00:00", "userAgent", "Mozilla/5.0", "visitLocation", nil,
			"visitedUrl", "https://source.test", "type", "something-else"),
	}

	yourlsSourceLinks = []any{
		link("keyword", "yourls-1", "url", "https://yourls.org/one", "title", "YOURLS one", "timestamp", "2020-02-03 04:05:06",
			"ip", "127.0.0.1", "clicks", "2"),
		link("keyword", "custom", "url", "https://yourls.org/conflicting", "title", "", "timestamp", "2020-02-04 00:00:00",
			"ip", "127.0.0.1", "clicks", "0"),
	}
	yourlsSourceVisits = []any{
		link("click_id", "1", "click_time", "2020-03-01 10:00:00", "shorturl", "yourls-1", "referrer", "direct",
			"user_agent", "Mozilla/5.0", "ip_address", "1.2.3.4", "country_code", "US"),
		link("click_id", "2", "click_time", "2020-03-02 10:00:00", "shorturl", "yourls-1", "referrer", "https://t.co",
			"user_agent", "Mozilla/5.0", "ip_address", "1.2.3.4", "country_code", "FR"),
	}

	kuttSourceLinks = []any{
		link("id", "1", "address", "kutt-1", "description", "Kutt link", "banned", false, "password", false,
			"expire_in", "2040-05-06T07:08:09.000Z", "target", "https://kutt.it/one", "visit_count", 4,
			"created_at", "2021-07-08T09:10:11.000Z", "updated_at", "2021-07-08T09:10:11.000Z", "link", "https://kutt.it/kutt-1",
			"domain", nil),
		link("id", "2", "address", "kutt-2", "banned", false, "password", false, "target", "https://kutt.it/two",
			"visit_count", 0, "created_at", "2021-07-09T00:00:00.000Z", "updated_at", "2021-07-09T00:00:00.000Z",
			"link", "https://custom.kutt.test/kutt-2", "domain", "custom.kutt.test"),
	}
	kuttSourceAnonymousLinks = []any{
		link("id", "3", "address", "anonymous", "banned", false, "password", false, "target", "https://kutt.it/anonymous",
			"visit_count", 0, "created_at", "2021-07-10T00:00:00.000Z", "link", "https://kutt.it/anonymous"),
	}
)
