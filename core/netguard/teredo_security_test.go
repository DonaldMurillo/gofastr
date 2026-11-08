package netguard

import (
	"net"
	"testing"
)

func TestTeredoChecksBothIPv4Addresses(t *testing.T) {
	teredo := func(server, client string) net.IP {
		ip := net.ParseIP("2001:0::").To16()
		copy(ip[4:8], net.ParseIP(server).To4())
		for i, b := range net.ParseIP(client).To4() {
			ip[12+i] = ^b
		}
		return ip
	}
	for _, internal := range []string{"127.0.0.2", "10.3.4.5", "172.20.1.2", "192.168.2.3", "169.254.169.254", "100.64.1.2", "0.1.2.3", "224.1.2.3", "240.1.2.3"} {
		for _, ip := range []net.IP{teredo(internal, "8.8.8.8"), teredo("8.8.8.8", internal)} {
			if !IsInternal(ip) || Reason(ip) == "" {
				t.Errorf("Teredo %s carrying %s is public", ip, internal)
			}
		}
	}
	for _, ip := range []net.IP{teredo("8.8.8.8", "1.1.1.1"), net.ParseIP("2001:1::1")} {
		if IsInternal(ip) || Reason(ip) != "" {
			t.Errorf("public address %s refused: %s", ip, Reason(ip))
		}
	}
}
