// Package harness runs the contract suite against any Shlink implementation: it seeds the target's database directly,
// calls its HTTP API and runs its CLI, mirroring upstream's ApiTestCase and CliTestCase from shlink-test-utils.
package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// Config comes from environment variables so the same suite can target either implementation:
//
//	SHLINK_TARGET_URL   base URL of the HTTP server, e.g. http://127.0.0.1:18080
//	SHLINK_CLI          command prefix that runs the CLI, e.g. "docker exec -i shlink-go-shlink-php-1 shlink"
//	CONTRACT_DB_DRIVER  sqlite, postgres, mysql, maria or mssql
//	CONTRACT_DB_DSN     Go database/sql DSN of the target's database, as seen from the test process
//	CONTRACT_HELPER_URL base URL under which the test process' helper server is reachable from the target
type Config struct {
	TargetURL string
	CLI       []string
	DBDriver  string
	DBDSN     string
	HelperURL string
}

var (
	cfgOnce sync.Once
	cfg     Config
	db      *DB
	dbErr   error
)

func LoadConfig() Config {
	cfgOnce.Do(func() {
		cfg = Config{
			TargetURL: strings.TrimRight(envOr("SHLINK_TARGET_URL", "http://127.0.0.1:18080"), "/"),
			CLI:       strings.Fields(os.Getenv("SHLINK_CLI")),
			DBDriver:  envOr("CONTRACT_DB_DRIVER", "sqlite"),
			DBDSN:     os.Getenv("CONTRACT_DB_DSN"),
			HelperURL: strings.TrimRight(envOr("CONTRACT_HELPER_URL", "http://host.docker.internal:19999"), "/"),
		}
		if cfg.DBDSN != "" {
			db, dbErr = OpenDB(cfg.DBDriver, cfg.DBDSN)
		}
	})
	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Seed purges the target database and loads upstream fixtures. Every test that talks to the target calls it first,
// like upstream's SeededTestCase does before each test.
func Seed(t testing.TB) {
	t.Helper()
	LoadConfig()
	skipKnownUpstreamFailure(t)
	if db == nil {
		if dbErr != nil {
			t.Fatalf("cannot open contract DB: %v", dbErr)
		}
		t.Fatal("CONTRACT_DB_DSN is not set")
	}
	// The SQLite file is shared with the target's container through a Docker Desktop bind mount, where journal file
	// operations occasionally fail with a disk I/O error; purging and seeding again from scratch is safe.
	for attempt := 1; ; attempt++ {
		err := db.Purge()
		if err != nil {
			err = fmt.Errorf("purge: %w", err)
		} else if err = db.SeedFixtures(); err != nil {
			err = fmt.Errorf("seed: %w", err)
		}
		if err == nil {
			break
		}
		if attempt == 3 || db.Driver != "sqlite" || !strings.Contains(err.Error(), "disk I/O error") {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TargetDB gives tests direct access to the target's database for assertions upstream does through repositories.
func TargetDB(t testing.TB) *DB {
	t.Helper()
	LoadConfig()
	if db == nil {
		t.Fatal("CONTRACT_DB_DSN is not set")
	}
	return db
}

var client = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// Response is a fully read HTTP response.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// JSON decodes the body into a generic value (map[string]any, []any, ...). Numbers decode as float64.
func (r *Response) JSON(t testing.TB) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.Body, &out); err != nil {
		t.Fatalf("response is not a JSON object: %v\nbody: %s", err, r.Body)
	}
	return out
}

func (r *Response) JSONAny(t testing.TB) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(r.Body, &out); err != nil {
		t.Fatalf("response is not JSON: %v\nbody: %s", err, r.Body)
	}
	return out
}

func (r *Response) Text() string { return string(r.Body) }

// Request options, mirroring the Guzzle options upstream tests pass.
type Options struct {
	Headers http.Header
	Query   Query  // replaces the URI's query string, like Guzzle's `query` option
	JSON    any    // encoded as the request body with Content-Type application/json
	Body    []byte // raw body, used when JSON is nil
}

const restPathPrefix = "/rest/v2"

// CallAPI calls a REST endpoint. Paths not starting with /rest get the /rest/v2 prefix, like upstream's ApiTestCase.
func CallAPI(t testing.TB, method, uri string, opts ...Options) *Response {
	t.Helper()
	if !strings.HasPrefix(uri, "/rest") {
		uri = restPathPrefix + uri
	}
	return Request(t, method, uri, opts...)
}

// CallAPIWithKey calls a REST endpoint with an X-Api-Key header (valid_api_key when key is empty).
func CallAPIWithKey(t testing.TB, method, uri string, opts ...Options) *Response {
	t.Helper()
	return CallAPIWithKeyNamed(t, method, uri, "valid_api_key", opts...)
}

func CallAPIWithKeyNamed(t testing.TB, method, uri, apiKey string, opts ...Options) *Response {
	t.Helper()
	o := first(opts)
	if o.Headers == nil {
		o.Headers = http.Header{}
	}
	o.Headers.Set("X-Api-Key", apiKey)
	return CallAPI(t, method, uri, o)
}

// CallShortURL performs GET /{shortCode} without following redirects.
func CallShortURL(t testing.TB, shortCode string, opts ...Options) *Response {
	t.Helper()
	return Request(t, http.MethodGet, "/"+shortCode, opts...)
}

// CallShortURLWithUA performs GET /{shortCode} with the given User-Agent.
func CallShortURLWithUA(t testing.TB, shortCode, userAgent string) *Response {
	t.Helper()
	return CallShortURL(t, shortCode, Options{Headers: http.Header{"User-Agent": {userAgent}}})
}

// Request performs a raw request against the target. The URI is used as-is (it may contain a query string).
func Request(t testing.TB, method, uri string, opts ...Options) *Response {
	t.Helper()
	c := LoadConfig()
	o := first(opts)
	target := c.TargetURL + uri
	if o.Query != nil {
		target, _, _ = strings.Cut(target, "?")
		if enc := o.Query.Encode(); enc != "" {
			target += "?" + enc
		}
	}
	var body io.Reader
	if o.JSON != nil {
		raw, err := json.Marshal(o.JSON)
		if err != nil {
			t.Fatalf("encode JSON body: %v", err)
		}
		body = bytes.NewReader(raw)
	} else if o.Body != nil {
		body = bytes.NewReader(o.Body)
	}
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if o.JSON != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, vs := range o.Headers {
		for _, v := range vs {
			if strings.EqualFold(k, "Host") {
				req.Host = v
				continue
			}
			req.Header.Add(k, v)
		}
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "GuzzleHttp/7")
	}
	recordCall(method, req.URL.Path)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, uri, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: raw}
}

func first(opts []Options) Options {
	if len(opts) == 0 {
		return Options{}
	}
	return opts[0]
}

// Exec runs a CLI command against the target, like upstream's CliTestCase::exec: `--no-ansi` is appended and inputs
// are written to stdin separated by newlines. It returns stdout (like Symfony Process::getOutput) and the exit code.
func Exec(t testing.TB, command []string, inputs ...string) (string, int) {
	t.Helper()
	c := LoadConfig()
	if len(c.CLI) == 0 {
		t.Fatal("SHLINK_CLI is not set")
	}
	args := append(append([]string{}, c.CLI[1:]...), command...)
	args = append(args, "--no-ansi")
	cmd := exec.Command(c.CLI[0], args...)
	cmd.Stdin = strings.NewReader(strings.Join(inputs, "\n"))
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	code := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run CLI %v: %v\n%s", command, err, errOut.String())
		}
		code = exitErr.ExitCode()
	}
	return out.String(), code
}
