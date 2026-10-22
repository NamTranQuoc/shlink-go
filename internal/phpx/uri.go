package phpx

import (
	"regexp"
	"strconv"
	"strings"
)

// URI ports GuzzleHttp\Psr7\Uri: parsing, normalization (lower-cased scheme and host, default ports removed,
// percent-encoding of characters not allowed in path, query and fragment) and serialization.
type URI struct {
	Scheme   string
	UserInfo string
	Host     string
	Port     *int
	Path     string
	Query    string
	Fragment string
}

var defaultPorts = map[string]int{
	"http": 80, "https": 443, "ftp": 21, "gopher": 70, "nntp": 119, "news": 119, "telnet": 23, "tn3270": 23,
	"imap": 143, "pop": 110, "ldap": 389,
}

// ParseURI parses a URI the way Guzzle's Uri constructor does. ok is false when PHP's parse_url() would fail.
func ParseURI(raw string) (URI, bool) {
	parts, ok := parseURL(raw)
	if !ok {
		return URI{}, false
	}
	var u URI
	if v, ok := parts["scheme"]; ok {
		u.Scheme = strings.ToLower(v)
	}
	if v, ok := parts["user"]; ok {
		u.UserInfo = filterUserInfo(v)
	}
	if v, ok := parts["host"]; ok {
		u.Host = strings.ToLower(v)
	}
	if v, ok := parts["port"]; ok {
		p, _ := strconv.Atoi(v)
		u.Port = &p
	}
	if v, ok := parts["path"]; ok {
		u.Path = FilterPath(v)
	}
	if v, ok := parts["query"]; ok {
		u.Query = FilterQueryOrFragment(v)
	}
	if v, ok := parts["fragment"]; ok {
		u.Fragment = FilterQueryOrFragment(v)
	}
	if v, ok := parts["pass"]; ok {
		u.UserInfo += ":" + filterUserInfo(v)
	}
	if u.Port != nil {
		if def, ok := defaultPorts[u.Scheme]; ok && def == *u.Port {
			u.Port = nil
		}
	}
	return u, true
}

// Authority returns userinfo@host:port.
func (u URI) Authority() string {
	authority := u.Host
	if u.UserInfo != "" {
		authority = u.UserInfo + "@" + authority
	}
	if u.Port != nil {
		authority += ":" + strconv.Itoa(*u.Port)
	}
	return authority
}

// WithPath returns a copy with a filtered path, like Uri::withPath().
func (u URI) WithPath(path string) URI {
	u.Path = FilterPath(path)
	u.validate()
	return u
}

// WithQuery returns a copy with a filtered query, like Uri::withQuery().
func (u URI) WithQuery(query string) URI {
	u.Query = FilterQueryOrFragment(query)
	return u
}

func (u *URI) validate() {
	if u.Host == "" && (u.Scheme == "http" || u.Scheme == "https") {
		u.Host = "localhost"
	}
}

// String ports Uri::composeComponents().
func (u URI) String() string {
	return ComposeURI(u.Scheme, u.Authority(), u.Path, u.Query, u.Fragment)
}

// ComposeURI ports Uri::composeComponents().
func ComposeURI(scheme, authority, path, query, fragment string) string {
	var b strings.Builder
	if scheme != "" {
		b.WriteString(scheme + ":")
	}
	if authority != "" || scheme == "file" {
		b.WriteString("//" + authority)
	}
	if authority != "" && path != "" && path[0] != '/' {
		path = "/" + path
	}
	b.WriteString(path)
	if query != "" {
		b.WriteString("?" + query)
	}
	if fragment != "" {
		b.WriteString("#" + fragment)
	}
	return b.String()
}

const (
	charUnreserved = "a-zA-Z0-9_\\-\\.~"
	charSubDelims  = "!\\$&'\\(\\)\\*\\+,;="
)

var (
	pathFilter     = regexp.MustCompile(`(?:[^` + charUnreserved + charSubDelims + `%:@/]+|%[^A-Fa-f0-9]|%[A-Fa-f0-9][^A-Fa-f0-9]|%[A-Fa-f0-9]?$)`)
	queryFilter    = regexp.MustCompile(`(?:[^` + charUnreserved + charSubDelims + `%:@/\?]+|%[^A-Fa-f0-9]|%[A-Fa-f0-9][^A-Fa-f0-9]|%[A-Fa-f0-9]?$)`)
	userInfoFilter = regexp.MustCompile(`(?:[^%` + charUnreserved + charSubDelims + `]+|%[^A-Fa-f0-9]|%[A-Fa-f0-9][^A-Fa-f0-9]|%[A-Fa-f0-9]?$)`)
)

// filterComponent percent-encodes every run of disallowed bytes and every '%' not starting a valid escape.
// RE2 has no lookahead, so a lone '%' is matched together with what follows it and only the '%' is encoded.
func filterComponent(re *regexp.Regexp, s string) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		if m[0] == '%' {
			return "%25" + filterRest(re, m[1:])
		}
		return RawURLEncode(m)
	})
}

func filterRest(re *regexp.Regexp, rest string) string {
	if rest == "" {
		return ""
	}
	return filterComponent(re, rest)
}

// FilterPath ports Uri::filterPath().
func FilterPath(path string) string { return filterComponent(pathFilter, path) }

// FilterQueryOrFragment ports Uri::filterQueryAndFragment().
func FilterQueryOrFragment(s string) string { return filterComponent(queryFilter, s) }

func filterUserInfo(s string) string { return filterComponent(userInfoFilter, s) }

var (
	schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*$`)
)

// parseURL is a pragmatic port of PHP's parse_url() for absolute and relative URLs, preceded by Guzzle's
// path-no-scheme special case.
func parseURL(raw string) (map[string]string, bool) {
	parts := map[string]string{}
	if isPathNoSchemeReference(raw) {
		rest := raw
		if i := strings.IndexByte(rest, '#'); i >= 0 {
			parts["fragment"] = rest[i+1:]
			rest = rest[:i]
		}
		if i := strings.IndexByte(rest, '?'); i >= 0 {
			parts["query"] = rest[i+1:]
			rest = rest[:i]
		}
		parts["path"] = rest
		return parts, true
	}
	rest := raw
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		parts["fragment"] = rest[i+1:]
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		parts["query"] = rest[i+1:]
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, ':'); i > 0 && schemeRe.MatchString(rest[:i]) {
		after := rest[i+1:]
		// "host:port" without scheme is parsed by parse_url as host + port only when it is all digits after ':'
		if !strings.HasPrefix(after, "//") && isAllDigits(after) && after != "" {
			parts["host"] = rest[:i]
			parts["port"] = after
			return parts, true
		}
		parts["scheme"] = rest[:i]
		rest = after
	}
	if strings.HasPrefix(rest, "//") {
		rest = rest[2:]
		authority := rest
		path := ""
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			authority, path = rest[:j], rest[j:]
		}
		if at := strings.LastIndexByte(authority, '@'); at >= 0 {
			userInfo := authority[:at]
			authority = authority[at+1:]
			user, pass, hasPass := strings.Cut(userInfo, ":")
			parts["user"] = user
			if hasPass {
				parts["pass"] = pass
			}
		}
		host := authority
		if strings.HasPrefix(host, "[") {
			if end := strings.IndexByte(host, ']'); end >= 0 {
				if port := host[end+1:]; strings.HasPrefix(port, ":") {
					if port[1:] != "" {
						if !isAllDigits(port[1:]) {
							return nil, false
						}
						parts["port"] = port[1:]
					}
				}
				host = host[:end+1]
			}
		} else if c := strings.LastIndexByte(host, ':'); c >= 0 {
			port := host[c+1:]
			if port != "" {
				if !isAllDigits(port) {
					return nil, false
				}
				if n, _ := strconv.Atoi(port); n > 65535 {
					return nil, false
				}
				parts["port"] = port
			}
			host = host[:c]
		}
		if host == "" && parts["scheme"] != "" && parts["scheme"] != "file" {
			if _, hasPort := parts["port"]; !hasPort && authority == "" {
				return nil, false
			}
		}
		parts["host"] = host
		if path != "" {
			parts["path"] = path
		}
		return parts, true
	}
	if rest != "" {
		parts["path"] = rest
	}
	return parts, true
}

func isPathNoSchemeReference(s string) bool {
	if s == "" || s[0] == '/' || s[0] == '?' || s[0] == '#' {
		return false
	}
	end := strings.IndexAny(s, "/?#")
	first := s
	if end >= 0 {
		first = s[:end]
	}
	return !strings.Contains(first, ":")
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
