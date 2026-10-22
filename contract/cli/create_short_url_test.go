package cli

import (
	"strings"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestCreateShortUrl_defaultDomainIsIgnoredWhenExplicitlyProvided(t *testing.T) {
	skipIfNotPorted(t, "short-url:create", "short-url:list")
	h.Seed(t)
	slug := "testing-default-domain"
	defaultDomain := "s.test"

	output, exitCode := h.Exec(t, []string{"short-url:create", "https://example.com", "--domain", defaultDomain, "--custom-slug", slug})

	h.AssertEquals(t, 0, exitCode)
	assertContains(t, output, "Generated short URL: http://"+defaultDomain+"/"+slug)

	listOutput, _ := h.Exec(t, []string{"short-url:list", "--show-domain", "--search-term", slug})
	assertContains(t, listOutput, "DEFAULT")
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q, got:\n%s", needle, haystack)
	}
}
