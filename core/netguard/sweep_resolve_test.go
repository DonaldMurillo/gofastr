package netguard

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// stubResolver answers every A query for a name in addrs with its address
// and every other query with no records, over a pipe instead of the network.
func stubResolver(t *testing.T, addrs map[string]string) {
	t.Helper()
	prev := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go serveStubDNS(server, addrs)
		return client, nil
	}}
	t.Cleanup(func() { net.DefaultResolver = prev })
}

func serveStubDNS(c net.Conn, addrs map[string]string) {
	defer c.Close()
	for {
		var n [2]byte
		if _, err := io.ReadFull(c, n[:]); err != nil {
			return
		}
		buf := make([]byte, binary.BigEndian.Uint16(n[:]))
		if _, err := io.ReadFull(c, buf); err != nil {
			return
		}
		var q dnsmessage.Message
		if q.Unpack(buf) != nil || len(q.Questions) == 0 {
			return
		}
		qq := q.Questions[0]
		resp := dnsmessage.Message{Header: dnsmessage.Header{ID: q.ID, Response: true, Authoritative: true}, Questions: q.Questions}
		name := strings.TrimSuffix(qq.Name.String(), ".")
		ip, ok := addrs[name]
		switch {
		case !ok:
			resp.RCode = dnsmessage.RCodeNameError
		case qq.Type == dnsmessage.TypeA:
			var a [4]byte
			copy(a[:], net.ParseIP(ip).To4())
			resp.Answers = []dnsmessage.Resource{{
				Header: dnsmessage.ResourceHeader{Name: qq.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60},
				Body:   &dnsmessage.AResource{A: a},
			}}
		}
		out, err := resp.Pack()
		if err != nil {
			return
		}
		binary.BigEndian.PutUint16(n[:], uint16(len(out)))
		if _, err := c.Write(append(n[:], out...)); err != nil {
			return
		}
	}
}

func TestSweepResolvedNamesAreCheckedByAddress(t *testing.T) {
	stubResolver(t, map[string]string{"evil.example.test": "10.1.2.3", "ok.example.test": "8.8.8.8"})
	if err := CheckOutboundURL("https://evil.example.test/x", "probe", false); err == nil || !strings.HasPrefix(err.Error(), "probe: ") {
		t.Fatalf("a name resolving to a private address was accepted: %v", err)
	}
	if err := CheckOutboundURL("https://ok.example.test/x", "probe", false); err != nil {
		t.Fatalf("a name resolving to a public address was refused: %v", err)
	}
	if err := CheckOutboundURL("http://%zz/x", "probe", false); err == nil || !strings.Contains(err.Error(), "parse URL") {
		t.Fatalf("an unparsable URL was not refused as such: %v", err)
	}

	inner := &countRT{}
	g := &ResolveGuard{Inner: inner, Prefix: "probe"}
	for _, target := range []string{"http://evil.example.test/x", "http://missing.example.test/x"} {
		req, _ := http.NewRequest(http.MethodGet, target, nil)
		if _, err := g.RoundTrip(req); err == nil || !strings.HasPrefix(err.Error(), "probe: ") {
			t.Fatalf("ResolveGuard let %s through: %v", target, err)
		}
	}
	if inner.calls != 0 {
		t.Fatalf("the inner transport ran %d times for refused names", inner.calls)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://ok.example.test/x", nil)
	_, _ = g.RoundTrip(req)
	if inner.calls != 1 {
		t.Fatal("a name resolving to a public address must reach the inner transport")
	}
}
