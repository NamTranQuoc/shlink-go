package cli

import (
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestEditShortUrl_longUrlCanBeEdited(t *testing.T) {
	skipIfNotPorted(t, "short-url:resolve", "short-url:edit")
	h.Seed(t)
	originalOutput, _ := h.Exec(t, []string{"short-url:resolve", "abc123"})
	assertContains(t, originalOutput, "Long URL: https://shlink.io")

	_, exitCode := h.Exec(t, []string{"short-url:edit", "abc123", "--long-url", "https://example.com"})
	h.AssertEquals(t, 0, exitCode)

	newOutput, _ := h.Exec(t, []string{"short-url:resolve", "abc123"})
	assertContains(t, newOutput, "Long URL: https://example.com")
}
