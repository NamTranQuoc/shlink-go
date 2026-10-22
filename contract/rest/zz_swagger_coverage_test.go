package rest

import (
	"os"
	"regexp"
	"strings"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// TestZZSwaggerCoverage is the coverage gate: every operation of upstream's swagger.json (extracted into
// testdata/swagger_operations.txt) must have been exercised by some test of this package. It runs last because Go
// runs tests in file order.
func TestZZSwaggerCoverage(t *testing.T) {
	if os.Getenv("CONTRACT_SKIP_COVERAGE") == "1" {
		t.Skip("coverage gate disabled for partial runs")
	}
	raw, err := os.ReadFile("testdata/swagger_operations.txt")
	if err != nil {
		t.Fatal(err)
	}
	calls := h.RecordedCalls()
	placeholder := regexp.MustCompile(`\\\{([a-zA-Z]+)\\\}`)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		method, path, _ := strings.Cut(line, " ")
		pattern := placeholder.ReplaceAllStringFunc(regexp.QuoteMeta(path), func(m string) string {
			if strings.Contains(m, "version") {
				return "[123]"
			}
			return "[^/]+"
		})
		re := regexp.MustCompile("^" + method + " " + pattern + "$")
		covered := false
		for _, c := range calls {
			if re.MatchString(c) && !(path == "/{shortCode}" && strings.HasPrefix(c, method+" /rest")) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("swagger operation not covered by any contract test: %s", line)
		}
	}
}
