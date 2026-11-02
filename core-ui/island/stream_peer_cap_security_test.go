package island

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// One anonymous client could fill the global UI stream cap: a session
// costs one same-origin POST, and the caps counted streams per session
// and per replica, never per client. 256 sessions of 16 streams took
// all 4096 default slots and every other visitor's stream answered 429
// (2026-10-27 R26). The per-client cap counts a signed-in user, or an
// anonymous caller's address, across every session it holds.

func TestPeerCapRefusesOneClient(t *testing.T) {
	mgr := NewManager(WithStreamCaps(0, 0), WithPeerStreamCap(2))
	_, c1, _ := mgr.ConnectSession("a", "ip:203.0.113.7")
	_, c2, _ := mgr.ConnectSession("b", "ip:203.0.113.7")
	defer c2()
	if ch, _, err := mgr.ConnectSession("c", "ip:203.0.113.7"); err != ErrPeerStreamLimit || ch != nil {
		t.Fatalf("third stream for one client across sessions: ch=%v err=%v, want nil, ErrPeerStreamLimit", ch, err)
	}
	_, other, err := mgr.ConnectSession("d", "ip:198.51.100.1")
	if err != nil {
		t.Fatalf("another client was refused: %v", err)
	}
	defer other()
	c1()
	_, c3, err := mgr.ConnectSession("c", "ip:203.0.113.7")
	if err != nil {
		t.Fatalf("a released slot was not reusable: %v", err)
	}
	c3()
	mgr.mu.RLock()
	left := mgr.peerSubs["ip:203.0.113.7"]
	mgr.mu.RUnlock()
	if left != 1 {
		t.Fatalf("peer count after cancels = %d, want 1", left)
	}
}

// A refused connect must not count: the refusals a flood earns would
// otherwise keep the client locked out after its streams close.
func TestRefusedConnectTakesNoPeerSlot(t *testing.T) {
	mgr := NewManager(WithStreamCaps(1, 0), WithPeerStreamCap(4))
	_, c1, _ := mgr.ConnectSession("a", "ip:203.0.113.7")
	if _, _, err := mgr.ConnectSession("a", "ip:203.0.113.7"); err != ErrSessionStreamLimit {
		t.Fatalf("second stream on one session: %v, want ErrSessionStreamLimit", err)
	}
	c1()
	mgr.mu.RLock()
	_, kept := mgr.peerSubs["ip:203.0.113.7"]
	mgr.mu.RUnlock()
	if kept {
		t.Fatal("a client with no live stream still has a peer entry")
	}
}

// Subscribe is the server's own programmatic subscription; it has no
// client, so only the session and global caps apply.
func TestSubscribeHasNoPeer(t *testing.T) {
	mgr := NewManager(WithStreamCaps(0, 0), WithPeerStreamCap(1))
	_, c1 := mgr.Subscribe("a")
	defer c1()
	second, c2 := mgr.Subscribe("b")
	defer c2()
	mgr.PushUpdate(IslandUpdate{IslandID: "x"}, "b")
	if _, open := <-second; !open {
		t.Fatal("a second server subscription was refused by the per-client cap")
	}
	if _, _, err := mgr.ConnectSession("c", "ip:203.0.113.7"); err != nil {
		t.Fatalf("server subscriptions counted against a client: %v", err)
	}
}

func TestClientKey(t *testing.T) {
	plain := NewManager()
	proxied := NewManager(WithTrustedProxies("10.0.0.0/8"))
	req := func(remote, xff string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/__gofastr/sse?session=s", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}
	anon := PresenceIdentity{}
	same := func(name string, m *Manager, a, b *http.Request) {
		t.Helper()
		if ka, kb := m.ClientKey(a, anon), m.ClientKey(b, anon); ka != kb {
			t.Errorf("%s: %q != %q, want one client", name, ka, kb)
		}
	}
	differ := func(name string, m *Manager, a, b *http.Request) {
		t.Helper()
		if ka, kb := m.ClientKey(a, anon), m.ClientKey(b, anon); ka == kb {
			t.Errorf("%s: both %q, want two clients", name, ka)
		}
	}
	same("port", plain, req("203.0.113.7:1", ""), req("203.0.113.7:2", ""))
	differ("address", plain, req("203.0.113.7:1", ""), req("203.0.113.8:1", ""))
	same("ipv6 /64", plain, req("[2001:db8::1]:1", ""), req("[2001:db8::ffff]:1", ""))
	differ("ipv6 other /64", plain, req("[2001:db8::1]:1", ""), req("[2001:db8:0:1::1]:1", ""))
	same("v4-mapped", plain, req("203.0.113.7:1", ""), req("[::ffff:203.0.113.7]:1", ""))
	same("xff untrusted", plain, req("10.0.0.1:1", "198.51.100.1"), req("10.0.0.1:1", "198.51.100.2"))
	differ("xff trusted", proxied, req("10.0.0.1:1", "198.51.100.1"), req("10.0.0.1:1", "198.51.100.2"))
	same("xff from a peer outside the list", proxied, req("203.0.113.7:1", "198.51.100.1"), req("203.0.113.7:1", "198.51.100.2"))
	same("xff names the peer", proxied, req("10.0.0.1:1", "198.51.100.1"), req("198.51.100.1:9", ""))

	user := PresenceIdentity{UserID: "u1"}
	if a, b := plain.ClientKey(req("203.0.113.7:1", ""), user), plain.ClientKey(req("198.51.100.1:1", ""), user); a != b {
		t.Errorf("one signed-in user from two addresses: %q != %q", a, b)
	}
	if plain.ClientKey(req("203.0.113.7:1", ""), user) == plain.ClientKey(req("203.0.113.7:1", ""), anon) {
		t.Error("a signed-in user shares the anonymous budget of its address")
	}
}

// The SSE entry point counts the request's client: past the cap it
// answers 429 like the other caps.
func TestSSEPeerCapAnswers429(t *testing.T) {
	mgr := NewManager(WithPeerStreamCap(1))
	r := httptest.NewRequest(http.MethodGet, "/__gofastr/sse?session=second", nil)
	r.RemoteAddr = "203.0.113.7:4711"
	_, held, err := mgr.ConnectSession("first", mgr.ClientKey(r, PresenceIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	defer held()
	rec := httptest.NewRecorder()
	mgr.ServeSSEWithPresence(rec, r, PresenceIdentity{}, PresenceTopics{})
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("over the client cap: %d Retry-After=%q, want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestDefaultPeerCap(t *testing.T) {
	if got := NewManager().caps.perPeer; got != DefaultStreamsPerPeer || got <= 0 || got >= DefaultGlobalStreams {
		t.Fatalf("default per-client cap = %d, want DefaultStreamsPerPeer (%d), below the global cap", got, DefaultStreamsPerPeer)
	}
}

// Behind a reverse proxy nobody listed, every visitor is the proxy's
// address: the first refusal from a private peer says how to fix it,
// once. A public peer, or a manager that trusts a proxy, logs nothing.
func TestPeerCapHintsAtAnUnlistedProxy(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	refuse := func(m *Manager, remote string) {
		r := httptest.NewRequest(http.MethodGet, "/__gofastr/sse?session=b", nil)
		r.RemoteAddr = remote
		_, held, err := m.ConnectSession("a", m.ClientKey(r, PresenceIdentity{}))
		if err != nil {
			t.Fatal(err)
		}
		defer held()
		m.ServeSSEWithPresence(httptest.NewRecorder(), r, PresenceIdentity{}, PresenceTopics{})
	}
	refuse(NewManager(WithPeerStreamCap(1)), "203.0.113.7:1")
	refuse(NewManager(WithPeerStreamCap(1), WithTrustedProxies("192.0.2.1")), "10.0.0.1:1")
	if buf.Len() != 0 {
		t.Fatalf("logged for a public peer or a listed proxy: %s", buf.String())
	}
	m := NewManager(WithPeerStreamCap(1))
	refuse(m, "10.0.0.1:1")
	refuse(m, "10.0.0.1:2")
	if n := strings.Count(buf.String(), "WithTrustedProxies"); n != 1 {
		t.Fatalf("hint logged %d times, want once: %s", n, buf.String())
	}
}
