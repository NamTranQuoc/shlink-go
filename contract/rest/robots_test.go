package rest

import (
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

func TestRobots_expectedListOfCrawlableShortCodesIsReturned(t *testing.T) {
	h.Seed(t)

	resp := h.CallShortURL(t, "robots.txt")

	h.AssertEquals(t, 200, resp.StatusCode)
	h.AssertEquals(t, `# For more information about the robots.txt standard, see:
# https://www.robotstxt.org/orig.html

User-agent: *
Allow: /abc123
Allow: /custom
Disallow: /`, resp.Text())
}
