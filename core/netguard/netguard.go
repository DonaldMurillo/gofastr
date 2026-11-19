// Package netguard decides whether an address is internal and guards
// outbound clients against reaching one.
package netguard

import "net"

var cgnatRange = net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

var thisNetworkV4 = net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: net.CIDRMask(8, 32)}

var reservedV4 = net.IPNet{IP: net.IPv4(240, 0, 0, 0), Mask: net.CIDRMask(4, 32)}

var siteLocalV6 = net.IPNet{IP: net.ParseIP("fec0::"), Mask: net.CIDRMask(10, 128)}

var metadataIPv4 = net.IPv4(169, 254, 169, 254)

// IsInternal reports whether ip is nil, in a non-public range, or an
// IPv4-translation address carrying an IPv4 in one.
func IsInternal(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if internalRange(ip) {
		return true
	}
	if v4 := translatedV4(ip); v4 != nil {
		return internalRange(v4)
	}
	return false
}

func internalRange(ip net.IP) bool {
	switch {
	case ip.IsUnspecified(),
		ip.IsLoopback(),
		ip.IsPrivate(),
		ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(),
		ip.IsMulticast(),
		ip.IsInterfaceLocalMulticast():
		return true
	}
	if cgnatRange.Contains(ip) || thisNetworkV4.Contains(ip) || reservedV4.Contains(ip) ||
		siteLocalV6.Contains(ip) {
		return true
	}
	return ip.Equal(metadataIPv4)
}

func translatedV4(ip net.IP) net.IP {
	if len(ip) != net.IPv6len {
		return nil
	}
	switch {
	case ip[0] == 0x00 && ip[1] == 0x64 && ip[2] == 0xff && ip[3] == 0x9b:
		if ip[4] == 0x00 && ip[5] == 0x01 && ip[8] == 0x00 && isZero(ip[11:]) {
			return net.IPv4(ip[6], ip[7], ip[9], ip[10])
		}
		if isZero(ip[4:12]) {
			return net.IPv4(ip[12], ip[13], ip[14], ip[15])
		}
	case ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0 && ip[3] == 0:
		server := net.IPv4(ip[4], ip[5], ip[6], ip[7])
		if internalRange(server) {
			return server
		}
		return net.IPv4(^ip[12], ^ip[13], ^ip[14], ^ip[15])
	case ip[0] == 0x20 && ip[1] == 0x02:
		return net.IPv4(ip[2], ip[3], ip[4], ip[5])
	case isZero(ip[0:12]):
		return net.IPv4(ip[12], ip[13], ip[14], ip[15])
	}
	return nil
}

func isZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// Reason describes why ip is internal, or returns "" when IsInternal(ip)
// is false.
func Reason(ip net.IP) string {
	if ip == nil {
		return "nil address"
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if r := rangeReason(ip); r != "" {
		return r
	}
	if v4 := translatedV4(ip); v4 != nil {
		if r := rangeReason(v4); r != "" {
			return r + ", carried by an IPv4-translation address"
		}
	}
	return ""
}

func rangeReason(ip net.IP) string {
	switch {
	case ip.IsUnspecified():
		return "unspecified address"
	case ip.IsLoopback():
		return "loopback address"
	case ip.IsPrivate():
		return "private address"
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		return "link-local address (covers cloud instance metadata)"
	case ip.IsMulticast(), ip.IsInterfaceLocalMulticast():
		return "multicast address"
	case thisNetworkV4.Contains(ip):
		return `reserved "this network" address (0.0.0.0/8)`
	case cgnatRange.Contains(ip):
		return "carrier-grade NAT address (RFC 6598)"
	case reservedV4.Contains(ip):
		return "reserved address (240.0.0.0/4; includes limited broadcast)"
	case siteLocalV6.Contains(ip):
		return "site-local address (fec0::/10; deprecated by RFC 3879)"
	case ip.Equal(metadataIPv4):
		return "cloud instance-metadata address"
	}
	return ""
}
