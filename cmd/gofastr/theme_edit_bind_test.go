package main

import (
	"net"
	"strings"
	"testing"
)

// The bind guard must fail CLOSED on anything it cannot parse.
//
// Both guards this replaced were fail-open on a parse error, in sequence:
// loopbackifyThemeAddr returns its input unchanged when SplitHostPort fails,
// and the non-loopback refusal was gated on `err == nil &&`. The address that
// exercises both is the empty string: SplitHostPort("") errors, and
// net.Listen("tcp", "") binds every interface. It arrives as
// `--addr=$THEME_ADDR` with the variable unset.
func TestThemeEditBindAddrFailsClosed(t *testing.T) {
	refused := []struct{ name, addr string }{
		{"empty — net.Listen would bind every interface", ""},
		{"whitespace only", "   "},
		{"no port", "127.0.0.1"},
		{"host only, unparseable", "localhost"},
		{"explicit LAN address", "192.168.1.10:8090"},
		{"explicit public address", "203.0.113.7:8090"},
		{"a hostname that is not loopback", "example.com:8090"},
		{"garbage", "not-an-address"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			got, err := themeEditBindAddr(tc.addr)
			if err == nil {
				t.Fatalf("themeEditBindAddr(%q) = %q, want a refusal — the editor serves an "+
					"unauthenticated page carrying its own bearer token and writes Go source to disk",
					tc.addr, got)
			}
		})
	}
}

// The forms an operator actually types must still work, and a wildcard must
// resolve to loopback rather than being refused outright; ":8090" means "port
// 8090 on this machine", which is what they want.
func TestThemeEditBindAddrAcceptsLoopbackForms(t *testing.T) {
	accepted := []struct{ addr, wantHost string }{
		{"127.0.0.1:0", "127.0.0.1"},
		{"127.0.0.1:8090", "127.0.0.1"},
		{"localhost:8090", "localhost"},
		{"[::1]:8090", "::1"},
		{":8090", "127.0.0.1"},
		{"0.0.0.0:8090", "127.0.0.1"},
		{"[::]:8090", "127.0.0.1"},
	}
	for _, tc := range accepted {
		t.Run(tc.addr, func(t *testing.T) {
			got, err := themeEditBindAddr(tc.addr)
			if err != nil {
				t.Fatalf("themeEditBindAddr(%q) = %v, want it accepted", tc.addr, err)
			}
			host, _, err := net.SplitHostPort(got)
			if err != nil {
				t.Fatalf("resolved %q is not host:port: %v", got, err)
			}
			if host != tc.wantHost {
				t.Fatalf("themeEditBindAddr(%q) bound host %q, want %q", tc.addr, host, tc.wantHost)
			}
		})
	}
}

// Whatever the guard returns must be something net.Listen actually confines to
// loopback; the assertion that matters is the socket, not the string.
func TestThemeEditBindAddrResolvesToALoopbackSocket(t *testing.T) {
	for _, addr := range []string{":0", "0.0.0.0:0", "127.0.0.1:0"} {
		resolved, err := themeEditBindAddr(addr)
		if err != nil {
			t.Fatalf("themeEditBindAddr(%q): %v", addr, err)
		}
		ln, err := net.Listen("tcp", resolved)
		if err != nil {
			t.Fatalf("listen %q: %v", resolved, err)
		}
		bound := ln.Addr().String()
		_ = ln.Close()
		host, _, err := net.SplitHostPort(bound)
		if err != nil {
			t.Fatalf("bound %q is not host:port: %v", bound, err)
		}
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			t.Fatalf("themeEditBindAddr(%q) produced a socket on %q, which is not loopback", addr, bound)
		}
		if strings.HasPrefix(bound, "[::]") || strings.HasPrefix(bound, "0.0.0.0") {
			t.Fatalf("themeEditBindAddr(%q) bound the wildcard %q", addr, bound)
		}
	}
}
