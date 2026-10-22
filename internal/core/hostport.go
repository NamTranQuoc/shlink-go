package core

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/NamTranQuoc/shlink-go/internal/valinor"
)

// hostAndPort ports shlink-common's HostAndPortConverter, followed by the global trim/strip_tags converter it chains to.
func hostAndPort(value string) (string, string) {
	parts := strings.Split(value, ":")
	if len(parts) > 2 {
		return "", `Provided value, once split using the ":" separator, returned more than 2 parts`
	}
	host := parts[0]
	if host != "localhost" && !IsValidDomain(host) {
		return "", "The host part of the value is not valid. It must be a hostname or IP address"
	}
	if len(parts) == 2 {
		port := parts[1]
		n, err := strconv.Atoi(port)
		if err != nil || strconv.Itoa(n) != port || n <= 0 || n > 65535 {
			return "", "The port part of the value is not valid. It must be a number between 1 and 65535"
		}
	}
	return valinor.TrimStrip(value), ""
}

// IsValidDomain ports Respect\Validation's Domain rule with tldCheck disabled.
func IsValidDomain(input string) bool {
	if strings.IndexFunc(input, unicode.IsSpace) >= 0 || !strings.Contains(input, ".") || utf8.RuneCountInString(input) < 3 {
		return false
	}
	parts := strings.Split(input, ".")
	tld := parts[len(parts)-1]
	if strings.HasPrefix(tld, "-") || utf8.RuneCountInString(tld) < 2 {
		return false
	}
	for _, part := range parts[:len(parts)-1] {
		if !validDomainPart(part) {
			return false
		}
	}
	return true
}

func validDomainPart(part string) bool {
	if part == "" {
		return false
	}
	filtered := strings.ReplaceAll(part, "-", "")
	for i := 0; i < len(filtered); i++ {
		c := filtered[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	if strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") {
		return false
	}
	return !strings.Contains(part, "--") || strings.Count(part, "--") == 1
}
