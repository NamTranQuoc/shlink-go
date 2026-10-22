package harness

import "time"

// User agents from upstream's config/test/constants.php.
const (
	AndroidUserAgent = "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/109.0.5414.86 Mobile Safari/537.36"
	IOSUserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 16_2 like Mac OS X) AppleWebKit/605.1.15 " +
		"(KHTML, like Gecko) FxiOS/109.0 Mobile/15E148 Safari/605.1.15"
	WindowsUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/138.0.0.0 Safari/537.36 Edg/138.0.3351.95"
	LinuxUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"HeadlessChrome/81.0.4044.113 Safari/537.36"
	MacOSUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_5) AppleWebKit/605.1.15 (KHTML, like Gecko) " +
		"Version/18.4 Safari/605.1.15"
	ChromeOSUserAgent = "Mozilla/5.0 (X11; CrOS x86_64 16181.61.0) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/134.0.6998.198 Safari/537.36"
	BrowserChromeUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/146.0.0.0 Safari/537.36"
	BrowserFirefoxUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:148.0) Gecko/20100101 Firefox/148.0"
	BrowserEdgeUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/146.0.0.0 Safari/537.36 Edg/146.0.3856.62"
	BrowserSafariUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_7_4) AppleWebKit/605.1.15 " +
		"(KHTML, like Gecko) Version/26.0 Safari/605.1.15"
	BrowserOperaUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/146.0.0.0 Safari/537.36 OPR/128.0.0.0"
)

// IPAddressHeaders is rka.ip_address.headers_to_inspect from upstream's config/autoload/ip-address.global.php.
var IPAddressHeaders = []string{
	"CF-Connecting-IP",
	"X-Forwarded-For",
	"X-Forwarded",
	"Forwarded",
	"True-Client-IP",
	"X-Real-IP",
	"X-Cluster-Client-Ip",
	"Client-Ip",
}

// AtomString formats a time like Chronos' toAtomString (DATE_ATOM), in UTC, the timezone of the test stack.
func AtomString(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05-07:00")
}
