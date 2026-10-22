package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// goldenCase runs a CLI command and compares its stdout, stderr and exit code with a golden captured from upstream
// (CONTRACT_UPDATE_GOLDEN=1 with the php target).
type goldenCase struct {
	name   string
	args   []string
	inputs []string
	// seed purges the database and loads the fixtures first
	seed bool
	// commands whose bodies run, for skipIfNotPorted
	runs []string
}

// helpCommands are the commands whose help is compared. completion is left out: its help embeds the real path of
// the CLI script, which depends on the image layout.
var helpCommands = []string{
	"help", "list", "short-url:import", "short-url:create", "short-url:edit", "short-url:resolve", "short-url:list",
	"short-url:visits", "short-url:delete", "short-url:visits-delete", "short-url:delete-expired", "visit:locate",
	"visit:download-db", "visit:orphan", "visit:orphan-delete", "visit:non-orphan", "api-key:generate",
	"api-key:disable", "api-key:delete", "api-key:list", "api-key:initial", "api-key:rename", "tag:list", "tag:rename",
	"tag:delete", "tag:visits", "domain:list", "domain:redirects", "domain:visits", "db:create", "db:migrate",
	"short-url:manage-rules", "integration:matomo:send-visits", "env-var:read", "_complete",
}

func TestCommandGoldens(t *testing.T) {
	cases := []goldenCase{
		{name: "list", args: []string{"list"}},
		{name: "no-command", args: []string{}},
		{name: "no-command-help", args: []string{"--help"}},
		{name: "version", args: []string{"--version"}},
		{name: "list-raw", args: []string{"list", "--raw"}},
		{name: "list-json", args: []string{"list", "--format=json"}},
		{name: "list-xml", args: []string{"list", "--format=xml", "--short"}},
		{name: "list-md-short", args: []string{"list", "--format=md", "--short"}},
		{name: "list-rst-namespace", args: []string{"list", "--format=rst", "api-key"}},
		{name: "list-namespace", args: []string{"list", "visit"}},
		{name: "list-unknown-namespace", args: []string{"list", "foo"}},
		{name: "namespace-as-command", args: []string{"tag"}},
		{name: "unknown-command", args: []string{"foo"}},
		{name: "unknown-command-in-namespace", args: []string{"tag:foo"}},
		{name: "ambiguous-command", args: []string{"s:v"}},
		{name: "abbreviated-command-help", args: []string{"a:gen", "-h"}},
		{name: "alternative-not-confirmed", args: []string{"tag:lst"}, inputs: []string{"no"}},
		{name: "alternative-non-interactive", args: []string{"tag:lst", "-n"}},
		{name: "hidden-command-abbreviation", args: []string{"db"}},
		{name: "unknown-option", args: []string{"tag:list", "--foo"}},
		{name: "unknown-shortcut", args: []string{"tag:list", "-x"}},
		{name: "too-many-arguments", args: []string{"tag:list", "extra"}},
		{name: "option-requires-value", args: []string{"api-key:generate", "--name"}},
		{name: "missing-arguments", args: []string{"tag:rename", "-n"}},
		{name: "unsupported-help-format", args: []string{"help", "--format=foo", "tag:list"}},
		{name: "json-help", args: []string{"help", "--format=json", "short-url:create"}},
		{name: "invalid-enum-option", args: []string{"visit:orphan", "--format=bad"}},
		{name: "invalid-env-var-non-interactive", args: []string{"env-var:read", "FOO", "-n"}},
		{name: "invalid-env-var-interactive", args: []string{"env-var:read", "FOO"}},
		{name: "env-var", args: []string{"env-var:read", "DEFAULT_DOMAIN"}},
		{name: "env-var-bool", args: []string{"env-var:read", "IS_HTTPS_ENABLED"}},
		{name: "env-var-chosen", args: []string{"env-var:read"}, inputs: []string{"TIMEZONE"}},
		{name: "completion-bash", args: []string{"completion", "bash"}},
		{name: "completion-unsupported", args: []string{"completion", "pwsh"}},
		{name: "complete-command-names", args: []string{"_complete", "-sbash", "-c1", "-ishlink", "-it"}},
		{name: "complete-options", args: []string{"_complete", "-szsh", "-c2", "-ishlink", "-itag:list", "-i--"}},
		{name: "complete-option-values", args: []string{"_complete", "-sfish", "-c3", "-ishlink", "-ihelp", "-i--format", "-i"}},

		{name: "tag-list", args: []string{"tag:list"}, seed: true},
		{name: "tag-rename", args: []string{"tag:rename", "foo", "renamed"}, seed: true},
		{name: "tag-rename-conflict", args: []string{"tag:rename", "foo", "bar"}, seed: true},
		{name: "tag-rename-not-found", args: []string{"tag:rename", "invalid", "x"}, seed: true},
		{name: "tag-delete", args: []string{"tag:delete", "-t", "foo", "--name=bar"}, seed: true},
		{name: "tag-delete-without-names", args: []string{"tag:delete"}, seed: true},
		{name: "tag-visits", args: []string{"tag:visits", "foo"}, seed: true},
		{name: "tag-visits-asked", args: []string{"tag:visits"}, inputs: []string{"bar"}, seed: true},
		{name: "tag-visits-not-found", args: []string{"tag:visits", "invalid"}, seed: true},
		{name: "domain-list", args: []string{"domain:list"}, seed: true},
		{name: "domain-list-redirects", args: []string{"domain:list", "-r"}, seed: true},
		{name: "domain-visits", args: []string{"domain:visits", "example.com"}, seed: true},
		{name: "domain-visits-not-found", args: []string{"domain:visits", "invalid.com"}, seed: true},
		{name: "domain-redirects", args: []string{"domain:redirects", "example.com"},
			inputs: []string{"https://a.com", "", "https://c.com"}, seed: true},
		{name: "domain-redirects-chosen", args: []string{"domain:redirects"},
			inputs: []string{"1", "new.example.com", "", "", ""}, seed: true},
		{name: "orphan-visits", args: []string{"visit:orphan"}, seed: true},
		{name: "orphan-visits-by-type", args: []string{"visit:orphan", "--type=regular_404"}, seed: true},
		{name: "orphan-visits-csv", args: []string{"visit:orphan", "--format=csv"}, seed: true},
		{name: "orphan-visits-paginated", args: []string{"visit:orphan", "-f", "paginated", "-s", "2020-01-15"}, seed: true},
		{name: "non-orphan-visits", args: []string{"visit:non-orphan"}, seed: true},
		{name: "non-orphan-visits-csv", args: []string{"visit:non-orphan", "--format=csv", "-d", "DEFAULT"}, seed: true},
		{name: "orphan-visits-delete-aborted", args: []string{"visit:orphan-delete"}, inputs: []string{""}, seed: true},
		{name: "orphan-visits-delete", args: []string{"visit:orphan-delete"}, inputs: []string{"yes"}, seed: true},
		{name: "api-key-generate-with-roles", args: []string{"api-key:generate", "--name=new", "-a", "-d", "example.com", "-o",
			"--expiration-date=2030-01-01T00:00:00+00:00"}, seed: true},
		{name: "api-key-generate-duplicated-name", args: []string{"api-key:generate", "--name=valid_api_key"}, seed: true},
		{name: "api-key-generate-default-domain", args: []string{"api-key:generate", "-d", "s.test"}, seed: true},
		{name: "api-key-generate-invalid-date", args: []string{"api-key:generate", "-e", "invalid"}, seed: true},
		{name: "api-key-disable", args: []string{"api-key:disable", "valid_api_key"}, seed: true},
		{name: "api-key-disable-chosen", args: []string{"api-key:disable"}, inputs: []string{"1"}, seed: true},
		{name: "api-key-disable-not-found", args: []string{"api-key:disable", "invalid"}, seed: true},
		{name: "api-key-delete-confirmed", args: []string{"api-key:delete", "valid_api_key"}, inputs: []string{"y"}, seed: true},
		{name: "api-key-delete-aborted", args: []string{"api-key:delete", "valid_api_key"}, inputs: []string{"no"}, seed: true},
		{name: "api-key-delete-not-found", args: []string{"api-key:delete", "invalid", "-n"}, seed: true},
		{name: "api-key-rename-asked", args: []string{"api-key:rename"}, inputs: []string{"valid_api_key", "renamed"}, seed: true},
		{name: "api-key-rename-conflict", args: []string{"api-key:rename", "valid_api_key", "author_api_key"}, seed: true},
		{name: "download-db-without-license", args: []string{"visit:download-db"}, seed: true},
		{name: "locate-visits", args: []string{"visit:locate"}, seed: true},
		{name: "locate-all-visits-aborted", args: []string{"visit:locate", "-a", "-r"}, inputs: []string{"no"}, seed: true},
		{name: "matomo-disabled", args: []string{"integration:matomo:send-visits"}, seed: true},
		{name: "db-create-existing", args: []string{"db:create"}, seed: true},
		{name: "db-migrate", args: []string{"db:migrate", "-vv"}, seed: true},

		{name: "short-url-list", args: []string{"short-url:list"}, seed: true, runs: r("short-url:list")},
		{name: "short-url-list-all-columns", args: []string{"short-url:list", "--show-tags", "--show-domain", "-k", "--all"},
			seed: true, runs: r("short-url:list")},
		{name: "short-url-list-filtered", args: []string{"short-url:list", "-o", "title-DESC", "-t", "foo", "-t", "bar",
			"--tags-all", "--exclude-tag", "baz", "--search-term", "shlink"}, seed: true, runs: r("short-url:list")},
		{name: "short-url-list-two-letter-shortcut", args: []string{"short-url:list", "-st", "abc"}, seed: true,
			runs: r("short-url:list")},
		{name: "short-url-list-api-key-name", args: []string{"short-url:list", "-kn", "author_api_key", "-k"}, seed: true,
			runs: r("short-url:list")},
		{name: "short-url-list-page-out-of-range", args: []string{"short-url:list", "-p", "2"}, seed: true, runs: r("short-url:list")},
		{name: "short-url-list-mapping-errors", args: []string{"short-url:list", "-p", "0", "-o", "bad"}, seed: true,
			runs: r("short-url:list")},
		{name: "short-url-list-invalid-order", args: []string{"short-url:list", "-o", "title-X"}, seed: true, runs: r("short-url:list")},
		{name: "short-url-list-non-numeric-page", args: []string{"short-url:list", "-p", "x"}, seed: true, runs: r("short-url:list")},
		{name: "short-url-list-invalid-date", args: []string{"short-url:list", "-s", "nope", "-o", "bad"}, seed: true,
			runs: r("short-url:list")},
		{name: "short-url-create", args: []string{"short-url:create", "https://example.com/new", "-c", "new-slug", "-t", "Foo",
			"-t", "bar baz", "--title", "The title", "-m", "3", "-s", "2030-01-01", "-r", "-w"}, seed: true,
			runs: r("short-url:create")},
		{name: "short-url-create-asked", args: []string{"short-url:create", "-c", "asked", "-d", "example.com"},
			inputs: []string{"https://example.com/asked"}, seed: true, runs: r("short-url:create")},
		{name: "short-url-create-conflict", args: []string{"short-url:create", "https://example.com", "-c", "custom"}, seed: true,
			runs: r("short-url:create")},
		{name: "short-url-create-slugified", args: []string{"short-url:create", "https://example.com", "-c", "with/slash and space",
			"-p", "pre"}, seed: true, runs: r("short-url:create")},
		{name: "short-url-create-reserved-chars", args: []string{"short-url:create", "https://example.com", "-c", "a?b#"},
			seed: true, runs: r("short-url:create")},
		{name: "short-url-create-invalid-url", args: []string{"short-url:create", "foo"}, seed: true, runs: r("short-url:create")},
		{name: "short-url-create-invalid-domain", args: []string{"short-url:create", "https://example.com", "-d", "a:1:2"},
			seed: true, runs: r("short-url:create")},
		{name: "short-url-create-mapping-errors", args: []string{"short-url:create", "foo", "-d", "a b", "-t", "x", "--title",
			"it's a \"quoted\" title that is longer than fifty characters"}, seed: true, runs: r("short-url:create")},
		{name: "short-url-create-invalid-date", args: []string{"short-url:create", "foo", "-u", "nope"}, seed: true,
			runs: r("short-url:create")},
		{name: "short-url-create-non-numeric-interactive", args: []string{"short-url:create", "https://example.com", "-m", "x"},
			seed: true, runs: r("short-url:create")},
		{name: "short-url-create-non-numeric-length-interactive", args: []string{"short-url:create", "https://example.com", "-l",
			"x"}, seed: true, runs: r("short-url:create")},
		{name: "short-url-create-non-numeric-non-interactive", args: []string{"short-url:create", "https://example.com", "-l",
			"x", "-n"}, seed: true, runs: r("short-url:create")},
		{name: "short-url-create-not-asked", args: []string{"short-url:create", "-n"}, seed: true, runs: r("short-url:create")},
		{name: "short-url-edit", args: []string{"short-url:edit", "abc123", "-t", "new", "--title", "Edited", "-s", "2020-01-01",
			"-l", "https://example.com/edited", "--no-crawlable"}, seed: true, runs: r("short-url:edit")},
		{name: "short-url-edit-with-domain", args: []string{"short-url:edit", "ghi789", "-d", "example.com", "-m", "10"}, seed: true,
			runs: r("short-url:edit")},
		{name: "short-url-edit-invalid-url", args: []string{"short-url:edit", "abc123", "-l", "foo"}, seed: true,
			runs: r("short-url:edit")},
		{name: "short-url-edit-not-found", args: []string{"short-url:edit", "nope", "-m", "3"}, seed: true, runs: r("short-url:edit")},
		{name: "short-url-edit-non-numeric", args: []string{"short-url:edit", "abc123", "-m", "x"}, seed: true,
			runs: r("short-url:edit")},
		{name: "short-url-resolve", args: []string{"short-url:resolve", "abc123"}, seed: true, runs: r("short-url:resolve")},
		{name: "short-url-resolve-asked", args: []string{"short-url:resolve", "-d", "example.com"}, inputs: []string{"ghi789"},
			seed: true, runs: r("short-url:resolve")},
		{name: "short-url-resolve-not-found", args: []string{"short-url:resolve", "nope", "-d", "foo.com"}, seed: true,
			runs: r("short-url:resolve")},
		{name: "short-url-visits", args: []string{"short-url:visits", "abc123"}, seed: true, runs: r("short-url:visits")},
		{name: "short-url-visits-asked", args: []string{"short-url:visits", "--format=csv"}, inputs: []string{"def456"},
			seed: true, runs: r("short-url:visits")},
		{name: "short-url-visits-not-found", args: []string{"short-url:visits", "nope"}, seed: true, runs: r("short-url:visits")},
		{name: "short-url-delete-aborted", args: []string{"short-url:delete", "abc123"}, inputs: []string{"no"}, seed: true,
			runs: r("short-url:delete")},
		{name: "short-url-delete-forced", args: []string{"short-url:delete", "abc123"}, inputs: []string{"yes"}, seed: true,
			runs: r("short-url:delete")},
		{name: "short-url-delete-ignoring-threshold", args: []string{"short-url:delete", "def456", "-i"}, seed: true,
			runs: r("short-url:delete")},
		{name: "short-url-delete", args: []string{"short-url:delete", "custom-with-domain", "-d", "some-domain.com"}, seed: true,
			runs: r("short-url:delete")},
		{name: "short-url-delete-not-found", args: []string{"short-url:delete", "nope"}, seed: true, runs: r("short-url:delete")},
		{name: "short-url-visits-delete", args: []string{"short-url:visits-delete", "ghi789", "-d", "example.com"},
			inputs: []string{"yes"}, seed: true, runs: r("short-url:visits-delete")},
		{name: "short-url-visits-delete-not-found", args: []string{"short-url:visits-delete", "nope"}, inputs: []string{"yes"},
			seed: true, runs: r("short-url:visits-delete")},
		{name: "short-url-visits-delete-aborted", args: []string{"short-url:visits-delete", "abc123"}, inputs: []string{""},
			seed: true, runs: r("short-url:visits-delete")},
		{name: "short-url-delete-expired-dry-run", args: []string{"short-url:delete-expired", "--dry-run"}, seed: true,
			runs: r("short-url:delete-expired")},
		{name: "short-url-delete-expired-dry-run-max-visits", args: []string{"short-url:delete-expired", "--dry-run",
			"--evaluate-max-visits"}, seed: true, runs: r("short-url:delete-expired")},
		{name: "short-url-delete-expired-aborted", args: []string{"short-url:delete-expired"}, inputs: []string{"no"}, seed: true,
			runs: r("short-url:delete-expired")},
		{name: "short-url-delete-expired", args: []string{"short-url:delete-expired", "-n", "--evaluate-max-visits"}, seed: true,
			runs: r("short-url:delete-expired")},
		{name: "short-url-delete-expired-confirmed", args: []string{"short-url:delete-expired"}, inputs: []string{"yes"},
			seed: true, runs: r("short-url:delete-expired")},
		{name: "manage-rules-save", args: []string{"short-url:manage-rules", "def456"}, inputs: []string{""}, seed: true,
			runs: r("short-url:manage-rules")},
		{name: "manage-rules-no-rules", args: []string{"short-url:manage-rules", "custom"}, inputs: []string{"1", "2", "4"},
			seed: true, runs: r("short-url:manage-rules")},
		{name: "manage-rules-not-found", args: []string{"short-url:manage-rules", "abc123", "-d", "example.com"}, seed: true,
			runs: r("short-url:manage-rules")},
		{name: "manage-rules-non-interactive", args: []string{"short-url:manage-rules", "abc123", "-n"}, seed: true,
			runs: r("short-url:manage-rules")},
		{name: "manage-rules-invalid-values", args: []string{"short-url:manage-rules", "abc123"}, inputs: []string{"0",
			"not-a-number", "1", "invalid-long-url", "https://example.com", "1", "", "es-ES", "no", "4"}, seed: true,
			runs: r("short-url:manage-rules")},
		{name: "manage-rules-add-every-condition", args: []string{"short-url:manage-rules", "def456"}, inputs: []string{
			"0", "", "https://example.com/all", "0", "1", "yes", "2", "k", " v ", "yes", "3", "k2", "yes", "4", "k3", "yes",
			"5", "1.2.3.4", "yes", "6", "ES", "yes", "7", "Madrid", "yes", "8", "2030-01-01", "yes", "9",
			"2020-01-01T10:00:00+02:00", "yes", "10", "3", "no", "0", "1", "https://example.com/first", "1", "en", "no", "3"},
			seed: true, runs: r("short-url:manage-rules")},
		{name: "manage-rules-remove-and-re-arrange", args: []string{"short-url:manage-rules", "def456"}, inputs: []string{
			"1", "2", "2", "1", "5", "1", "2", "8 - https://example.com/linux", "", "3"}, seed: true,
			runs: r("short-url:manage-rules")},
		{name: "manage-rules-invalid-date", args: []string{"short-url:manage-rules", "abc123"}, inputs: []string{"0", "1",
			"https://example.com", "8", "nope"}, seed: true, runs: r("short-url:manage-rules")},
	}
	for _, name := range helpCommands {
		cases = append(cases, goldenCase{name: "help-" + strings.ReplaceAll(name, ":", "-"), args: []string{"help", name}})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			skipIfNotPorted(t, c.runs...)
			if c.seed {
				h.Seed(t)
			}
			assertGolden(t, c.name, h.ExecFull(t, c.args, c.inputs...))
		})
	}
}

// assertGolden compares a command's outcome with testdata/<name>.golden, or captures it with CONTRACT_UPDATE_GOLDEN=1
// (upstream only).
func assertGolden(t *testing.T, name string, r h.CLIResult) {
	t.Helper()
	update := os.Getenv("CONTRACT_UPDATE_GOLDEN") == "1"
	if update && os.Getenv("CONTRACT_TARGET") != "php" {
		t.Fatal("goldens can only be captured from upstream (CONTRACT_TARGET=php)")
	}
	actual := normalizeCLIOutput(fmt.Sprintf("exit: %d\n--- stdout\n%s--- stderr\n%s", r.ExitCode, r.Stdout, r.Stderr))
	path := filepath.Join("testdata", name+".golden")
	if update {
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s: %v", path, err)
	}
	h.AssertEquals(t, string(expected), actual)
}

// r lists the commands a golden case runs, for skipIfNotPorted.
func r(commands ...string) []string { return commands }

var (
	uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	// fixtures date some visits at the time they are seeded
	recentDatePattern = regexp.MustCompile(`20[2-9][1-9]-\d\d-\d\dT\d\d:\d\d:\d\d\+00:00`)
	// fixture ids keep growing between seeds on engines with sequences
	domainIDPattern = regexp.MustCompile(`'domain_id' => \d+ *`)
	// the completion help embeds the real path of the CLI script, which depends on the image layout
	completionPathPattern = regexp.MustCompile(`(eval \\?"\$\()\S+?( completion)`)
)

// normalizeCLIOutput replaces values that change on every run (generated API keys, seeding dates) with placeholders of
// the same width, so tables keep their layout.
func normalizeCLIOutput(s string) string {
	s = uuidPattern.ReplaceAllString(s, "xxxxxxxx-xxxx-4xxx-xxxx-xxxxxxxxxxxx")
	s = completionPathPattern.ReplaceAllString(s, "${1}<cli-script>${2}")
	s = domainIDPattern.ReplaceAllStringFunc(s, func(m string) string {
		return "'domain_id' => N" + strings.Repeat(" ", len(m)-len("'domain_id' => N"))
	})
	return recentDatePattern.ReplaceAllString(s, "YYYY-MM-DDTHH:MM:SS+00:00")
}
