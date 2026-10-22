// Package diff holds the differential suite: requests listed per env profile are sent to the target and the responses
// compared with goldens captured from upstream (CONTRACT_UPDATE_GOLDEN=1 with the php target). It covers behavior
// upstream's own tests do not pin down, like non-default env configurations and protocol edge cases.
package diff

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// Case is one request of a profile.
type Case struct {
	Name    string            `json:"name"`
	Seed    bool              `json:"seed,omitempty"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	// BodyBase64 carries bodies that are not valid UTF-8.
	BodyBase64 string `json:"bodyBase64,omitempty"`
	// APIKey sends the fixtures' valid API key in X-Api-Key.
	APIKey bool `json:"apiKey,omitempty"`
	// SkipEngines lists DB engines where upstream's result is engine-specific.
	SkipEngines []string `json:"skipEngines,omitempty"`
	// Before lists requests sent first, whose responses are not compared (e.g. a visit before listing visits).
	Before []Case `json:"before,omitempty"`
}

const validAPIKey = "valid_api_key"

// Result is the normalized response.
type Result struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// comparedHeaders are the response headers whose values are compared.
var comparedHeaders = []string{
	"Content-Type", "Content-Length", "Location", "Cache-Control", "Allow", "Access-Control-Allow-Origin",
	"Access-Control-Allow-Credentials", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers",
	"Access-Control-Max-Age",
}

var (
	isoNow  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}`)
	jwtLike = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
)

// normalizeBody replaces values that depend on the run (timestamps of rows created now).
func normalizeBody(body string) string {
	now := time.Now().UTC()
	body = isoNow.ReplaceAllStringFunc(body, func(m string) string {
		t, err := time.Parse(time.RFC3339, m)
		if err == nil && t.Sub(now) < time.Hour && now.Sub(t) < time.Hour {
			return "<now>"
		}
		return m
	})
	return jwtLike.ReplaceAllString(body, "<jwt>")
}

func run(t *testing.T, c Case) Result {
	headers := http.Header{}
	for k, v := range c.Headers {
		headers.Set(k, v)
	}
	if headers.Get("Host") == "" {
		headers.Set("Host", "localhost:8080")
	}
	if c.APIKey {
		headers.Set("X-Api-Key", validAPIKey)
	}
	opts := h.Options{Headers: headers}
	if c.Body != "" {
		opts.Body = []byte(c.Body)
	}
	if c.BodyBase64 != "" {
		opts.Body, _ = base64.StdEncoding.DecodeString(c.BodyBase64)
	}
	resp := h.Request(t, c.Method, c.Path, opts)
	res := Result{Status: resp.StatusCode, Headers: map[string]string{}, Body: normalizeBody(string(resp.Body))}
	for _, name := range comparedHeaders {
		if values := resp.Header.Values(name); len(values) > 0 {
			res.Headers[name] = strings.Join(values, ", ")
		}
	}
	return res
}

func TestDifferential(t *testing.T) {
	profile := os.Getenv("CONTRACT_PROFILE")
	if profile == "" {
		profile = "default"
	}
	raw, err := os.ReadFile(filepath.Join("testdata", profile+".cases.json"))
	if err != nil {
		t.Skipf("no differential cases for profile %s", profile)
	}
	var cases []Case
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", profile+".golden.json")
	update := os.Getenv("CONTRACT_UPDATE_GOLDEN") == "1"
	if update && os.Getenv("CONTRACT_TARGET") != "php" {
		t.Fatal("goldens can only be captured from the upstream target")
	}
	golden := map[string]Result{}
	if !update {
		if raw, err := os.ReadFile(goldenPath); err == nil {
			_ = json.Unmarshal(raw, &golden)
		}
	}
	engine := h.LoadConfig().DBDriver
	captured := map[string]Result{}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			for _, e := range c.SkipEngines {
				if e == engine {
					t.Skipf("engine-specific upstream result on %s", engine)
				}
			}
			if c.Seed {
				h.Seed(t)
			}
			for _, b := range c.Before {
				run(t, b)
			}
			res := run(t, c)
			if update {
				captured[c.Name] = res
				return
			}
			want, ok := golden[c.Name]
			if !ok {
				t.Fatalf("no golden for case %q", c.Name)
			}
			h.AssertEquals(t, want, res)
		})
	}
	if update {
		names := make([]string, 0, len(captured))
		for n := range captured {
			names = append(names, n)
		}
		sort.Strings(names)
		out, _ := json.MarshalIndent(captured, "", "  ")
		if err := os.WriteFile(goldenPath, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
