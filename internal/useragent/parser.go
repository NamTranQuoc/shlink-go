// Package useragent is a port of donatj/phpuseragentparser v1.12.0 (platform, browser and browser
// version detection) and of the two Shlink models built on it, DeviceType and Browser.
package useragent

import (
	"slices"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/pcre"
)

// UserAgent is the result of Parse. A field PHP leaves null is "" (PHP can also return an empty
// browser or version; Shlink only compares Platform and Browser with non-empty names).
type UserAgent struct {
	Platform       string
	Browser        string
	BrowserVersion string
}

// Parse parses a user agent string like (new UserAgentParser())->parse($ua).
func Parse(userAgent string) UserAgent {
	r := parse(userAgent)
	return UserAgent{Platform: r.platform.s, Browser: r.browser.s, BrowserVersion: r.version.s}
}

// nullable is a PHP ?string: set is false for null.
type nullable struct {
	s   string
	set bool
}

func str(s string) nullable { return nullable{s: s, set: true} }

type result struct{ platform, browser, version nullable }

// The patterns of parse_user_agent() in src/UserAgentParser.php, verbatim with their modifiers,
// run by pcre with PHP's semantics (bytes, ASCII-only /i, PCRE's \s and $).
var (
	parenRe = pcre.MustCompile(`\((.*?)\)`, pcre.Multiline)

	platformRe = pcre.MustCompile(`(?P<platform>BB\d+;|Android|Adr|Symbian|Sailfish|CrOS|Fuchsia|Tizen|iPhone|iPad|iPod|Linux|(?:Open|Net|Free)BSD|Macintosh|
Windows(?:\ Phone)?|Silk|linux-gnu|BlackBerry|PlayBook|X11|(?:New\ )?Nintendo\ (?:WiiU?|3?DS|Switch)|Xbox(?:\ One)?)
(?:\ [^;]*)?
(?:;|$)`, pcre.Caseless|pcre.Multiline|pcre.Extended)

	androidRe = pcre.MustCompile(`(?P<platform>Android)[:/ ]`, pcre.Caseless|pcre.Extended)

	browserRe = pcre.MustCompile(`(?P<prev>.)?(?P<browser>Camino|Kindle(\ Fire)?|Firefox|Iceweasel|IceCat|Safari|MSIE|Trident|AppleWebKit|
TizenBrowser|(?:Headless)?Chrome|YaBrowser|Vivaldi|IEMobile|Opera|OPR|Silk|Midori|(?-i:Edge)|EdgA?|CriOS|UCBrowser|Puffin|
OculusBrowser|SamsungBrowser|SailfishBrowser|XiaoMi/MiuiBrowser|YaApp_Android|Whale|
Applebot|Facebot|Googlebot|YandexBot|bingbot|Lynx|Version|
Valve\ Steam\ Tenfoot|Mastodon|
NintendoBrowser|PLAYSTATION\ (?:\d|Vita)+)
\)?;?
(?:[:/ ](?P<version>[0-9A-Z.]+)|/[A-Z]*)`, pcre.Caseless|pcre.Extended)

	contactBotRe = pcre.MustCompile(`(?P<browser>[A-Z0-9\-]+)[/ ](?P<version>[0-9A-Z.]+)\s*\(\+(?:https?:|[a-z0-9._-]+@[a-z0-9._-]+)`, pcre.Caseless|pcre.Extended)

	// PHP's pattern starts with ^(?!Mozilla); parse checks that lookahead with hasPrefixFoldASCII.
	leadingBotRe = pcre.MustCompile(`^(?P<browser>[A-Z0-9\-]+)([/ :](?P<version>[0-9A-Z.]+))?`, pcre.Caseless|pcre.Extended)

	commentBotRe = pcre.MustCompile(`[(;]\s*(?P<browser>[^(/;]+)
(?:[:/ ]v?(?P<version>[0-9A-Z.]+)[^;)\s]*)?
;?(?:\s*robot;)?\s*\+(?:https?:|[a-z0-9._-]+@[a-z0-9._-]+)`, pcre.Caseless|pcre.Extended)

	rvRe = pcre.MustCompile(`rv:(?P<version>[0-9A-Z.]+)`, pcre.Caseless)

	playStationRe = pcre.MustCompile(`playstation \d`, pcre.Caseless)
)

var platformPriority = []string{"Xbox One", "Xbox", "Windows Phone", "Tizen", "Android", "FreeBSD", "NetBSD", "OpenBSD", "CrOS", "Fuchsia", "X11", "Sailfish"}

var (
	browserPrevGroup = browserRe.SubexpIndex("prev")
	browserGroup     = browserRe.SubexpIndex("browser")
	browserVerGroup  = browserRe.SubexpIndex("version")
)

// group returns capture g, "" when it did not participate.
func group(s string, caps []int, g int) string {
	if caps[2*g] < 0 {
		return ""
	}
	return s[caps[2*g]:caps[2*g+1]]
}

// maxGroups is the most capture groups any pattern above has (browserRe).
const maxGroups = 4

type browserMatch struct{ prev, browser, version string }

func parse(u string) result {
	var r result
	if u == "" || u == "0" { // !$u_agent
		return r
	}
	var caps [2 * (maxGroups + 1)]int

	if parenRe.FindFrom(u, 0, caps[:]) {
		inner := group(u, caps[:], 1)
		var buf [8]string
		platforms := buf[:0]
		for pos := 0; platformRe.FindFrom(inner, pos, caps[:]); pos = caps[1] { // preg_match_all
			if p := group(inner, caps[:], 1); !slices.Contains(platforms, p) { // array_unique
				platforms = append(platforms, p)
			}
		}
		if len(platforms) > 1 {
			r.platform = str(platforms[0])
			for _, p := range platformPriority { // reset(array_intersect($priority, ...))
				if slices.Contains(platforms, p) {
					r.platform = str(p)
					break
				}
			}
		} else if len(platforms) == 1 {
			r.platform = str(platforms[0])
		}
	}

	switch {
	case r.platform.set && (r.platform.s == "linux-gnu" || r.platform.s == "X11"):
		r.platform = str(PlatformLinux)
	case r.platform.set && r.platform.s == "CrOS":
		r.platform = str(PlatformChromeOS)
	case r.platform.set && r.platform.s == "Adr":
		r.platform = str(PlatformAndroid)
	case !r.platform.set:
		if androidRe.FindFrom(u, 0, caps[:]) {
			r.platform = str(group(u, caps[:], 1))
		}
	}

	var mbuf [8]browserMatch
	matches := mbuf[:0]
	for pos := 0; browserRe.FindFrom(u, pos, caps[:]); pos = caps[1] { // preg_match_all
		matches = append(matches, browserMatch{
			prev:    group(u, caps[:], browserPrevGroup),
			browser: group(u, caps[:], browserGroup),
			version: group(u, caps[:], browserVerGroup),
		})
	}

	if len(matches) == 0 {
		if contactBotRe.FindFrom(u, 0, caps[:]) {
			return result{platform: r.platform, browser: str(group(u, caps[:], 1)), version: str(group(u, caps[:], 2))}
		}
		if !hasPrefixFoldASCII(u, "mozilla") && leadingBotRe.FindFrom(u, 0, caps[:]) {
			return result{platform: r.platform, browser: str(group(u, caps[:], 1)), version: nonEmpty(group(u, caps[:], 3))}
		}
	}

	if len(matches) == 0 || matches[0].prev != "" {
		if commentBotRe.FindFrom(u, 0, caps[:]) {
			return result{platform: r.platform, browser: str(trimPHP(group(u, caps[:], 1))), version: nonEmpty(group(u, caps[:], 2))}
		}
	}

	if len(matches) == 0 {
		return r
	}

	var rv nullable // a truthy $rv_result
	if rvRe.FindFrom(u, 0, caps[:]) {
		if v := group(u, caps[:], 1); truthy(v) {
			rv = str(v)
		}
	}

	r.browser, r.version = str(matches[0].browser), str(matches[0].version)

	// find is the $find closure: the first search value present among the matched browser names
	// (compared lowercased), and the index of its first match.
	find := func(search ...string) (key int, value string, ok bool) {
		for _, v := range search {
			for k, m := range matches {
				if equalFoldASCII(m.browser, v) {
					return k, v, true
				}
			}
		}
		return 0, "", false
	}

	// $key starts at 0 and only a successful find changes it.
	if k, v, ok := find("OPR", "Facebot", "UCBrowser", "YaBrowser", "YaApp_Android", "Iceweasel", "Icecat", "CriOS", "Edg", "EdgA", "XiaoMi/MiuiBrowser"); ok {
		r.browser = str(renamedBrowsers[v])
		r.version = nullable{}
		if startsWithDigit(matches[k].version) {
			r.version = str(matches[k].version)
		}
	} else if _, _, ok := find("Playstation Vita"); ok {
		r.platform = str(PlatformPlayStationVita)
		r.browser = str(BrowserNameBrowser)
	} else if k, v, ok := find("Kindle Fire", "Silk"); ok {
		if v == "Silk" {
			r.browser = str(BrowserNameSilk)
		} else {
			r.browser = str(BrowserNameKindle)
		}
		r.platform = str(PlatformKindleFire)
		r.version = str(matches[k].version)
		if !truthy(matches[k].version) || !startsWithDigit(matches[k].version) {
			r.version = str(matches[max(versionIndex(matches), 0)].version)
		}
	} else if k, _, ok := find("NintendoBrowser"); ok || r.platform.set && r.platform.s == PlatformNintendo3DS {
		r.browser = str(BrowserNameNintendoBrowser)
		r.version = str(matches[k].version)
	} else if k, _, ok := find("Kindle"); ok {
		r.platform = str(PlatformKindle)
		r.browser = str(matches[k].browser)
		r.version = str(matches[k].version)
	} else if k, _, ok := find("Opera"); ok {
		r.browser = str(BrowserNameOpera)
		if kv, _, ok := find("Version"); ok {
			k = kv
		}
		r.version = str(matches[k].version)
	} else if k, _, ok := find("Puffin"); ok {
		r.browser = str(BrowserNamePuffin)
		version := matches[k].version
		if len(version) > 3 {
			if part := version[len(version)-2:]; isUpperASCII(part) {
				version = version[:len(version)-2]
				if p, ok := puffinPlatforms[part]; ok {
					r.platform = str(p)
				}
			}
		}
		r.version = str(version)
	} else if k, v, ok := find("Googlebot", "Applebot", "IEMobile", "Edge", "Midori", "Whale", "Vivaldi", "OculusBrowser", "SamsungBrowser", "Valve Steam Tenfoot", "Chrome", "HeadlessChrome", "SailfishBrowser"); ok {
		r.browser = str(v)
		r.version = str(matches[k].version)
	} else if _, _, ok := find("Trident"); rv.set && ok {
		r.browser = str(BrowserNameMSIE)
		r.version = rv
	} else if matches[0].browser == "AppleWebKit" {
		k := 0
		switch {
		case r.platform.set && r.platform.s == PlatformAndroid:
			r.browser = str(BrowserNameAndroidBrowser)
		case r.platform.set && strings.HasPrefix(r.platform.s, "BB"):
			r.browser = str(BrowserNameBlackBerryBrowser)
			r.platform = str(PlatformBlackBerry)
		case r.platform.set && (r.platform.s == PlatformBlackBerry || r.platform.s == PlatformPlayBook):
			r.browser = str(BrowserNameBlackBerryBrowser)
		default:
			if ks, v, ok := find("Safari"); ok {
				k = ks
				r.browser, r.version = str(v), str(matches[k].version)
			} else if kt, v, ok := find("TizenBrowser"); ok {
				k = kt
				r.browser, r.version = str(v), str(matches[k].version)
			} else {
				k = len(matches) - 1
				r.browser, r.version = str(matches[k].browser), str(matches[k].version)
			}
		}
		if kv, _, ok := find("Version"); ok {
			r.version = str(matches[kv].version)
		}
	} else {
		for _, m := range matches { // reset(preg_grep('/playstation \d/i', ...))
			if playStationRe.MatchString(m.browser) {
				r.platform = str("PlayStation " + digitsOnly(m.browser))
				r.browser = str(BrowserNameNetFront)
				break
			}
		}
	}
	return r
}

var renamedBrowsers = map[string]string{
	"OPR":                BrowserNameOpera,
	"Facebot":            BrowserNameIMessageBot,
	"UCBrowser":          BrowserNameUCBrowser,
	"YaBrowser":          BrowserNameYandex,
	"YaApp_Android":      BrowserNameYandex,
	"Iceweasel":          BrowserNameFirefox,
	"Icecat":             BrowserNameFirefox,
	"CriOS":              BrowserNameChrome,
	"Edg":                BrowserNameEdge,
	"EdgA":               BrowserNameEdge,
	"XiaoMi/MiuiBrowser": BrowserNameMiuiBrowser,
}

var puffinPlatforms = map[string]string{
	"IP": PlatformIPhone,
	"IT": PlatformIPad,
	"AP": PlatformAndroid,
	"AT": PlatformAndroid,
	"WP": PlatformWindowsPhone,
	"WT": PlatformWindows,
}

// versionIndex is array_search('Version', $result['browser']), -1 for false. PHP reads
// $array[false] as $array[0].
func versionIndex(matches []browserMatch) int {
	for i, m := range matches {
		if m.browser == "Version" {
			return i
		}
	}
	return -1
}

// equalFoldASCII is strtolower($a) === strtolower($b); PHP 8 lowercases ASCII only.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lowerASCII(a[i]) != lowerASCII(b[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// nonEmpty is `empty($s) ? null : $s`.
func nonEmpty(s string) nullable {
	if !truthy(s) {
		return nullable{}
	}
	return str(s)
}

// truthy is PHP's boolean conversion of a string: false for "" and "0".
func truthy(s string) bool { return s != "" && s != "0" }

// startsWithDigit is is_numeric(substr($s, 0, 1)).
func startsWithDigit(s string) bool { return s != "" && '0' <= s[0] && s[0] <= '9' }

// isUpperASCII is ctype_upper($s) for a non-empty string.
func isUpperASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return s != ""
}

// digitsOnly is preg_replace('/\D/', ”, $s).
func digitsOnly(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if '0' <= s[i] && s[i] <= '9' {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// hasPrefixFoldASCII reports whether s starts with the lowercase ASCII prefix, folding A-Z only.
func hasPrefixFoldASCII(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if lowerASCII(s[i]) != prefix[i] {
			return false
		}
	}
	return true
}

// trimPHP is PHP's trim() with its default character list " \t\n\r\0\x0B".
func trimPHP(s string) string {
	return strings.Trim(s, " \t\n\r\x00\x0B")
}
