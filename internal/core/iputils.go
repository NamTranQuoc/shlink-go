package core

import (
	"net/netip"
	"strings"
)

// IPAddressMatchesGroups ports Util\IpAddressUtils::ipAddressMatchesGroups(). Each group is a static IP, a CIDR block
// or a wildcard pattern (11.22.*.*). ok is false when the IP is not a valid IPv4 address.
func IPAddressMatchesGroups(ip string, groups []string) (matches bool, ok bool) {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() {
		return false, false
	}
	parts := strings.Split(ip, ".")
	for _, g := range groups {
		if r, valid := candidateToRange(g, parts); valid && r.contains(addr) {
			return true, true
		}
	}
	return false, true
}

// IsStaticIPCidrOrWildcard ports IpAddressUtils::isStaticIpCidrOrWildcard().
func IsStaticIPCidrOrWildcard(candidate string) bool {
	_, ok := candidateToRange(candidate, []string{"0", "0", "0", "0"})
	return ok
}

type ipRange struct {
	prefix netip.Prefix
}

func (r ipRange) contains(a netip.Addr) bool { return r.prefix.Contains(a) }

func candidateToRange(candidate string, ipParts []string) (ipRange, bool) {
	if strings.Contains(candidate, "*") {
		octets := strings.Split(candidate, ".")
		for i, o := range octets {
			if o == "*" && i < len(ipParts) {
				octets[i] = ipParts[i]
			}
		}
		candidate = strings.Join(octets, ".")
	}
	return parseRangeString(candidate)
}

// parseRangeString approximates IPLib's Factory::parseRangeString(): a single address, a CIDR block, or an IPv4
// pattern with trailing '*' octets.
func parseRangeString(s string) (ipRange, bool) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return ipRange{}, false
		}
		return ipRange{p.Masked()}, true
	}
	if strings.Contains(s, "*") {
		octets := strings.Split(s, ".")
		if len(octets) != 4 {
			return ipRange{}, false
		}
		bits := 32
		for i := 3; i >= 0 && octets[i] == "*"; i-- {
			octets[i] = "0"
			bits -= 8
		}
		if strings.Contains(strings.Join(octets, "."), "*") {
			return ipRange{}, false
		}
		a, err := netip.ParseAddr(strings.Join(octets, "."))
		if err != nil {
			return ipRange{}, false
		}
		return ipRange{netip.PrefixFrom(a, bits)}, true
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return ipRange{}, false
	}
	return ipRange{netip.PrefixFrom(a, a.BitLen())}, true
}
