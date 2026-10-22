package cli

import (
	"fmt"
	"strings"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// importStep is one command of an import scenario. Every step runs against the state the previous ones left, and its
// outcome is compared with the golden <scenario>-<step name>.
type importStep struct {
	name   string
	args   []string
	inputs []string
}

// TestImportGoldens imports from every source upstream supports (except Bit.ly, whose API URL is fixed), served by the
// helper server, and checks what was persisted with other commands.
func TestImportGoldens(t *testing.T) {
	csvContents := "Long URL,Tags,Domain,Short Code,Title\n" +
		"https://shlink.io/csv-1,foo|bar,,csv-1,CSV title\n" +
		"https://shlink.io/csv-2,,s.test,custom,\n" +
		"https://shlink.io/csv-3,\"a,b\",new.example.com,csv/3,\n" +
		"https://shlink.io/csv-4,,,a?b,\n" +
		"https://shlink.io/csv-5,,,abc123,\n"
	scenarios := []struct {
		name  string
		setup func(t *testing.T) map[string]string
		steps []importStep
	}{
		{name: "import-csv", setup: func(t *testing.T) map[string]string {
			return map[string]string{"{csv}": writeTargetFile(t, "/tmp/shlink_golden_import.csv", csvContents)}
		}, steps: []importStep{
			{"first", []string{"short-url:import", "csv"}, []string{"{csv}", "", "0", ""}},
			{"second", []string{"short-url:import", "csv"}, []string{"{csv}", "Comma", "1", "1"}},
			{"list-1", []string{"short-url:list", "--search-term", "shlink.io/csv-1", "--show-tags", "--show-domain"}, nil},
			{"list-3", []string{"short-url:list", "--search-term", "shlink.io/csv-3", "--show-tags", "--show-domain"}, nil},
		}},
		{name: "import-csv-semicolon", setup: func(t *testing.T) map[string]string {
			return map[string]string{"{csv}": writeTargetFile(t, "/tmp/shlink_golden_import_semicolon.csv",
				"\xef\xbb\xbfshortUrl;LONGURL;tags\nhttps://some.test/inferred;https://shlink.io/inferred;0\n;https://shlink.io/random;\n")}
		}, steps: []importStep{
			{"import", []string{"short-url:import"}, []string{"csv", "{csv}", ";"}},
			{"list", []string{"short-url:list", "--search-term", "inferred", "--show-domain"}, nil},
		}},
		{name: "import-csv-duplicates", setup: func(t *testing.T) map[string]string {
			return map[string]string{"{csv}": writeTargetFile(t, "/tmp/shlink_golden_import_duplicates.csv",
				"longURL,short code,domain,tags\nhttps://dup.test/1,dup-1,new1.test,newtag\nhttps://dup.test/2,dup-1,new1.test,newtag\n"+
					"https://dup.test/3,dup-3,new1.test,newtag\nhttps://dup.test/4,dup-4,,\nhttps://dup.test/5,dup-4,,\n")}
		}, steps: []importStep{
			{"import", []string{"short-url:import", "csv"}, []string{"{csv}", ","}},
			{"list", []string{"short-url:list", "--search-term", "dup.test", "--show-domain", "--show-tags"}, nil},
		}},
		{name: "import-csv-malformed", setup: func(t *testing.T) map[string]string {
			files := map[string]string{
				"{duplicated-header}": "longUrl,tags,tags\nhttps://x.com,a,b\n",
				"{empty}":             "",
				"{blank-header}":      "\nlongUrl\nhttps://blank-header.com\n",
				"{no-long-url}":       "url,shortCode,title\nhttps://no-long-url.com,nl-1,Some long title here\n",
				"{short-row}":         "shortCode,longUrl,domain,title\nsr-1,https://short-row.com,,\nsr-2\n",
				"{quotes}": "longUrl,shortCode,title\n\n  \"https://quotes.com/a\" ,q-1,\"multi\nline title\"\n" +
					"\"https://quotes.com/\"\"b\"\"\",q-2,\n\"https://quotes.com/c\"tail,q-3,\n",
			}
			replacements := map[string]string{}
			i := 0
			for k, contents := range files {
				i++
				replacements[k] = writeTargetFile(t, fmt.Sprintf("/tmp/shlink_golden_malformed_%d.csv", i), contents)
			}
			return replacements
		}, steps: []importStep{
			{"duplicated-header", []string{"short-url:import", "csv"}, []string{"{duplicated-header}", ","}},
			{"empty", []string{"short-url:import", "csv"}, []string{"{empty}", ","}},
			{"blank-header", []string{"short-url:import", "csv"}, []string{"{blank-header}", ","}},
			{"no-long-url", []string{"short-url:import", "csv"}, []string{"{no-long-url}", ","}},
			{"short-row", []string{"short-url:import", "csv"}, []string{"{short-row}", ","}},
			{"short-row-rolled-back", []string{"short-url:list", "--search-term", "short-row"}, nil},
			{"quotes", []string{"short-url:import", "csv"}, []string{"{quotes}", ","}},
			{"quotes-list", []string{"short-url:list", "--search-term", "quotes.com", "-o", "shortCode"}, nil},
		}},
		{name: "import-shlink-invalid-rules", steps: []importStep{
			{"device", []string{"short-url:import", "shlink"}, []string{"no", "no", "{helper}/shlink-invalid-device", h.ImportSourcesAPIKey}},
			{"browser", []string{"short-url:import", "shlink"}, []string{"no", "no", "{helper}/shlink-invalid-browser", h.ImportSourcesAPIKey}},
			{"rolled-back", []string{"short-url:list", "--search-term", "imported"}, nil},
		}},
		{name: "import-csv-errors", steps: []importStep{
			{"missing-file", []string{"short-url:import", "csv"}, []string{"/nope", ""}},
			{"stdin", []string{"short-url:import", "csv"}, []string{"php://stdin", ";", "longUrl", "https://shlink.io"}},
			{"non-interactive", []string{"short-url:import", "csv", "-n"}, nil},
		}},
		{name: "import-source", steps: []importStep{
			{"invalid", []string{"short-url:import", "foo"}, nil},
			{"invalid-asked", []string{"short-url:import"}, []string{"foo"}},
			{"bitly-non-interactive", []string{"short-url:import", "bitly", "-n"}, nil},
			{"shlink-non-interactive", []string{"short-url:import", "shlink", "-n"}, nil},
			{"kutt-non-interactive", []string{"short-url:import", "kutt", "-n"}, nil},
			{"yourls-non-interactive", []string{"short-url:import", "yourls", "-n"}, nil},
		}},
		{name: "import-shlink", steps: []importStep{
			{"first", []string{"short-url:import", "shlink"}, []string{"yes", "yes", "{helper}/shlink", h.ImportSourcesAPIKey, ""}},
			{"second", []string{"short-url:import", "shlink"}, []string{"", "", "{helper}/shlink", h.ImportSourcesAPIKey, "1"}},
			{"list", []string{"short-url:list", "--search-term", "imported", "--show-tags", "--show-domain"}, nil},
			{"visits", []string{"short-url:visits", "imported-1"}, nil},
			{"rules", []string{"short-url:manage-rules", "imported-1"}, []string{"4"}},
			{"orphan-visits", []string{"visit:orphan", "-s", "2099-01-01"}, nil},
		}},
		{name: "import-shlink-without-visits", steps: []importStep{
			{"import", []string{"short-url:import", "shlink"}, []string{"no", "no", "{helper}/shlink", h.ImportSourcesAPIKey, "0"}},
			{"visits", []string{"short-url:visits", "imported-1"}, nil},
		}},
		{name: "import-shlink-errors", steps: []importStep{
			{"broken", []string{"short-url:import", "shlink"}, []string{"yes", "no", "{helper}/shlink-broken", h.ImportSourcesAPIKey, ""}},
			{"rolled-back", []string{"short-url:list", "--search-term", "imported"}, nil},
			{"invalid-api-key", []string{"short-url:import", "shlink"}, []string{"yes", "yes", "{helper}/shlink", "invalid"}},
		}},
		{name: "import-yourls", steps: []importStep{
			{"import", []string{"short-url:import", "yourls"}, []string{"yes", "{helper}/yourls", "admin", "secret", "yourls.test"}},
			{"list", []string{"short-url:list", "-d", "yourls.test", "--show-domain"}, nil},
			{"visits", []string{"short-url:visits", "yourls-1", "-d", "yourls.test"}, nil},
			{"without-plugin", []string{"short-url:import", "yourls"}, []string{"no", "{helper}/yourls-without-plugin", "admin", "secret", ""}},
			{"invalid-credentials", []string{"short-url:import", "yourls"}, []string{"no", "{helper}/yourls", "admin", "wrong", ""}},
		}},
		{name: "import-kutt", steps: []importStep{
			{"import", []string{"short-url:import", "kutt"}, []string{"{helper}/kutt", h.ImportSourcesAPIKey, "yes"}},
			{"list", []string{"short-url:list", "--search-term", "kutt", "--show-domain"}, nil},
			{"invalid-api-key", []string{"short-url:import", "kutt"}, []string{"{helper}/kutt", "invalid", ""}},
		}},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			skipIfNotPorted(t, "short-url:import", "short-url:list", "short-url:visits", "short-url:manage-rules")
			h.Seed(t)
			replacements := map[string]string{"{helper}": h.HelperURL(t) + h.ImportSourcesPath}
			if sc.setup != nil {
				for k, v := range sc.setup(t) {
					replacements[k] = v
				}
			}
			for _, step := range sc.steps {
				inputs := make([]string, len(step.inputs))
				for i, in := range step.inputs {
					for k, v := range replacements {
						in = strings.ReplaceAll(in, k, v)
					}
					inputs[i] = in
				}
				assertGolden(t, sc.name+"-"+step.name, h.ExecFull(t, step.args, inputs...))
			}
		})
	}
}

// TestInstallerGoldens runs the shlink-installer commands whose outcome does not depend on the environment.
func TestInstallerGoldens(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"installer-list", []string{"list"}},
		{"installer-version", []string{"--version"}},
		{"installer-help-init", []string{"help", "init"}},
		{"installer-help-install", []string{"help", "install"}},
		{"installer-help-update", []string{"help", "update"}},
		{"installer-help-set-option", []string{"help", "set-option"}},
		{"installer-init", []string{"init", "-n", "--skip-download-geolite", "--clear-db-cache"}},
		{"installer-init-verbose", []string{"init", "-n", "--skip-initialize-db", "--skip-download-geolite",
			"--initial-api-key=valid_api_key", "-v"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h.Seed(t)
			assertGolden(t, c.name, h.ExecInstaller(t, c.args))
		})
	}
}
