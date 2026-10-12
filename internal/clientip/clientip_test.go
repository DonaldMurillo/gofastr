package clientip

import (
	"net/http"
	"testing"
)

func TestForwardedWalksFromTheRight(t *testing.T) {
	trusted := ParseProxies([]string{"10.0.0.0/8", "192.0.2.1", "not-an-ip", ""})
	cases := []struct {
		name  string
		lines []string
		want  string
		ok    bool
	}{
		{"single", []string{"203.0.113.7"}, "203.0.113.7", true},
		{"appended", []string{"198.51.100.1, 203.0.113.7"}, "203.0.113.7", true},
		{"skips trusted tiers", []string{"1.1.1.1, 203.0.113.7, 10.1.1.1, 192.0.2.1"}, "203.0.113.7", true},
		{"split lines", []string{"1.1.1.1, 203.0.113.7", "10.1.1.1"}, "203.0.113.7", true},
		{"port form", []string{"[2001:db8::1]:443"}, "2001:db8::1", true},
		{"junk before untrusted", []string{"203.0.113.7, junk"}, "", false},
		{"all trusted", []string{"10.0.0.2, 192.0.2.1"}, "", false},
		{"absent", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := http.Header{}
			for _, l := range c.lines {
				h.Add("X-Forwarded-For", l)
			}
			ip, ok := Forwarded(h, trusted)
			if ok != c.ok || (ok && ip.String() != c.want) {
				t.Fatalf("Forwarded = %v, %v; want %q, %v", ip, ok, c.want, c.ok)
			}
		})
	}
}

func TestProxiesZeroValueTrustsNothing(t *testing.T) {
	var p Proxies
	if !p.Empty() || p.ContainsAddr("10.0.0.1:80") {
		t.Fatal("zero Proxies must trust nothing")
	}
	if !ParseProxies([]string{"10.0.0.1"}).ContainsAddr("10.0.0.1:80") {
		t.Fatal("bare IP with a port must match")
	}
}
