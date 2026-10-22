package web

import (
	"net"
	"regexp"
	"strconv"
	"strings"
)

// ipHeadersToInspect ports the headers_to_inspect list of config/autoload/ip-address.global.php.
var ipHeadersToInspect = []string{
	"CF-Connecting-IP", "X-Forwarded-For", "X-Forwarded", "Forwarded", "True-Client-IP", "X-Real-IP",
	"X-Cluster-Client-Ip", "Client-Ip",
}

// IPResolver ports RKA\Middleware\IpAddress with proxy headers checking enabled.
type IPResolver struct {
	trusted   []string
	wildcards [][]string
	cidrs     [][2]int64
	hopCount  int
}

// NewIPResolver builds a resolver from the TRUSTED_PROXIES value: a hop count or a comma-separated list.
func NewIPResolver(trustedProxies []string, hopCount int) *IPResolver {
	r := &IPResolver{hopCount: hopCount}
	for _, p := range trustedProxies {
		switch {
		case strings.Contains(p, "*"):
			r.wildcards = append(r.wildcards, splitIPParts(p))
		case strings.Index(p, "/") > 6:
			r.cidrs = append(r.cidrs, parseCidr(p))
		default:
			r.trusted = append(r.trusted, p)
		}
	}
	return r
}

func splitIPParts(ip string) []string {
	if strings.Index(ip, ".") > 0 {
		return strings.SplitN(ip, ".", 4)
	}
	return strings.SplitN(ip, ":", 8)
}

// ip2long ports PHP's ip2long(); ok is false for anything but a dotted IPv4 address.
func ip2long(ip string) (int64, bool) {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil || strings.Contains(ip, ":") {
		return 0, false
	}
	v4 := parsed.To4()
	return int64(v4[0])<<24 | int64(v4[1])<<16 | int64(v4[2])<<8 | int64(v4[3]), true
}

func parseCidr(cidr string) [2]int64 {
	subnet, bitsStr, _ := strings.Cut(cidr, "/")
	long, _ := ip2long(subnet)
	bits, _ := strconv.Atoi(strings.TrimSpace(bitsStr))
	mask := int64(-1) << (32 - bits)
	return [2]int64{long & mask, long | ^mask}
}

func isValidIP(ip string) bool {
	return ip != "" && !strings.Contains(ip, "%") && net.ParseIP(ip) != nil
}

func isValidIPv4(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.To4() != nil && !strings.Contains(ip, ":")
}

var bracketPort = regexp.MustCompile(`\]:\d+$`)
var bracketed = regexp.MustCompile(`\[(.*?)\]`)

func extractIPAddress(ip string) string {
	parts := strings.Split(ip, ":")
	if len(parts) == 1 {
		return ip
	}
	if len(parts) == 2 && isValidIPv4(parts[0]) {
		return parts[0]
	}
	ip = strings.Trim(ip, `"'`)
	if strings.HasPrefix(ip, "[") && (strings.HasSuffix(ip, "]") || bracketPort.MatchString(ip)) {
		if m := bracketed.FindStringSubmatch(ip); m != nil {
			ip = m[1]
		}
	}
	return ip
}

var forwardedFor = regexp.MustCompile(`(?i)for=([^,;]+)`)

// Resolve ports IpAddress::determineClientIpAddress(); remoteAddr is the connection's address without port.
func (r *IPResolver) Resolve(remoteAddr string, header func(string) string) *string {
	ip := extractIPAddress(remoteAddr)
	if !isValidIP(ip) {
		return nil
	}
	trusted := r.trusted
	if len(trusted) == 0 && len(r.wildcards) == 0 && len(r.cidrs) == 0 {
		trusted = []string{ip}
	}
	for _, name := range ipHeadersToInspect {
		if value := header(name); value != "" && value != "0" {
			ip = r.fromHeader(name, value, ip, trusted)
			break
		}
	}
	if ip == "" || ip == "0" {
		return nil
	}
	return &ip
}

func (r *IPResolver) fromHeader(name, value, current string, trusted []string) string {
	var list []string
	if strings.EqualFold(name, "forwarded") {
		for _, m := range forwardedFor.FindAllStringSubmatch(value, -1) {
			if !isValidIP(extractIPAddress(m[1])) {
				return current
			}
			list = append(list, m[1])
		}
	} else {
		list = strings.Split(value, ",")
	}
	list = append(list, current)
	for i := range list {
		list[i] = extractIPAddress(strings.TrimSpace(list[i]))
		if !isValidIP(list[i]) {
			return current
		}
	}
	count := 0
	for i := len(list) - 1; i >= 0; i-- {
		count++
		if !r.isTrusted(list[i], trusted) {
			if count <= r.hopCount {
				continue
			}
			return list[i]
		}
	}
	return current
}

func (r *IPResolver) isTrusted(ip string, trusted []string) bool {
	for _, t := range trusted {
		if t == ip {
			return true
		}
	}
	if len(r.wildcards) > 0 {
		parts := splitIPParts(ip)
		expected := 8
		if strings.Index(ip, ".") > 0 {
			expected = 4
		}
		for _, w := range r.wildcards {
			if len(w) != expected {
				continue
			}
			match := true
			for i, part := range w {
				if part != "*" && (i >= len(parts) || part != parts[i]) {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	if len(r.cidrs) > 0 {
		if long, ok := ip2long(ip); ok && long != 0 {
			for _, c := range r.cidrs {
				if c[0] <= long && long <= c[1] {
					return true
				}
			}
		}
	}
	return false
}
