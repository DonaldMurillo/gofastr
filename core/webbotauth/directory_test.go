package webbotauth

// directory_test.go: every fetch-path guard from draft section 6.7 /
// Appendix C, proved by execution. The production transport
// (guardedTransport(false)) is exercised against real loopback dials to
// show the dial-time check fires; checkHostPublic covers literal-IP and
// name cases without network.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseAgentRef_SchemeAndShapeRules(t *testing.T) {
	cases := []struct {
		raw  string
		typ  discoveryType
		want string // error substring, "" for success
	}{
		{"https://agent.example", discoveryDirectory, ""},
		{"https://agent.example/", discoveryDirectory, ""},
		{"https://agent.example/jwks.json", discoveryJWKS, ""},
		{"http://agent.example", discoveryDirectory, "scheme must be https"},
		{"file:///etc/passwd", discoveryDirectory, "scheme must be https"},
		{"https://agent.example/keys", discoveryDirectory, "must be an origin"},
		{"https://agent.example?q=1", discoveryDirectory, "query or fragment"},
		{"https://user:pw@agent.example", discoveryDirectory, "userinfo"},
		{"https://agent.example", "cimd", "unsupported signature-agent discovery type"},
	}
	for _, tc := range cases {
		_, err := parseAgentRef(tc.raw, tc.typ)
		if tc.want == "" {
			if err != nil {
				t.Errorf("parseAgentRef(%q, %q): unexpected error %v", tc.raw, tc.typ, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("parseAgentRef(%q, %q): got %v, want error containing %q", tc.raw, tc.typ, err, tc.want)
		}
	}
	// The directory identifier is the well-known URI at the origin.
	ref, err := parseAgentRef("https://Agent.Example", discoveryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if ref.identifier != "https://agent.example"+directoryWellKnown {
		t.Errorf("identifier = %q", ref.identifier)
	}
}

func TestCheckHostPublic_RefusesInternal(t *testing.T) {
	hosts := []string{
		"localhost",
		"foo.localhost",
		"svc.internal",
		"metadata.google.internal",
		"127.0.0.1",
		"169.254.169.254",          // cloud instance metadata
		"10.0.0.1",                 // RFC1918
		"192.168.1.1",              // RFC1918
		"172.16.0.1",               // RFC1918
		"100.64.0.1",               // RFC 6598 CGNAT
		"::1",                      // v6 loopback
		"fe80::1",                  // link-local
		"fd00::1",                  // unique local
		"::ffff:127.0.0.1",         // v4-mapped loopback
		"64:ff9b::a9fe:a9fe",       // RFC 6052 NAT64 of 169.254.169.254
		"64:ff9b:1:a9fe:a9:fe00::", // RFC 8215 local-use NAT64 (split embedding)
		"2002:7f00:1::",            // 6to4 of 127.0.0.1
	}
	for _, h := range hosts {
		if err := checkHostPublic(h); err == nil {
			t.Errorf("checkHostPublic(%q) = nil, want refusal", h)
		}
	}
	// A public literal passes without network access.
	if err := checkHostPublic("8.8.8.8"); err != nil {
		t.Errorf("checkHostPublic(8.8.8.8) = %v, want nil", err)
	}
}

func TestDialGuard_RefusesInternalDial(t *testing.T) {
	// A real TLS server on loopback: the production transport's
	// dial-time Control hook must refuse the connect even though
	// nothing about the URL is checked here. This is the TOCTOU half:
	// whatever DNS said earlier, the dial itself is refused.
	srv := startTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("guarded transport reached the internal server")
	})
	res := newDirectoryResolver(nil)
	res.client = &http.Client{Transport: guardedTransport(false)} // production posture
	ref := &agentRef{identifier: srv.srv.URL, fetchURL: srv.srv.URL, typ: discoveryJWKS}
	_, err := res.resolve(context.Background(), ref)
	if err == nil {
		t.Fatal("production transport fetched a loopback URL")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("err = %v, want a netguard refusal", err)
	}
}

// mutableDirectory is a TLS test directory whose body and behavior can
// change between calls (rotation, outage, size, redirect tests).
type mutableDirectory struct {
	srv  *testTLSServer
	body atomic.Value // string
	fail atomic.Bool  // serve 500 when true
	hits atomic.Int32
}

func newMutableDirectory(t *testing.T, body string) *mutableDirectory {
	md := &mutableDirectory{}
	md.body.Store(body)
	md.srv = startTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		md.hits.Add(1)
		if md.fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, md.body.Load().(string))
	})
	return md
}

func (md *mutableDirectory) resolver(t *testing.T) *directoryResolver {
	t.Helper()
	res := resolverForServer(t, md.srv)
	return res
}

func TestFetch_SizeCap(t *testing.T) {
	big := `{"keys":[` + strings.Repeat(`{"kty":"OKP","crv":"Ed25519","x":"JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs"},`, 12000) + `]}`
	md := newMutableDirectory(t, big)
	res := md.resolver(t)
	ref := &agentRef{identifier: "https://big.test/jwks", fetchURL: md.srv.srv.URL + "/jwks", typ: discoveryJWKS}
	_, err := res.resolve(context.Background(), ref)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want size cap failure", err)
	}
}

func TestFetch_WallClockTimeout(t *testing.T) {
	var hits atomic.Int32
	srv := startTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(3 * time.Second)
		fmt.Fprint(w, `{"keys":[]}`)
	})
	res := resolverForServer(t, srv)
	ref := &agentRef{identifier: "https://slow.test/jwks", fetchURL: srv.srv.URL, typ: discoveryJWKS}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := res.resolve(ctx, ref)
	if err == nil {
		t.Fatal("slow directory fetch succeeded")
	}
	if time.Since(start) > time.Second {
		t.Errorf("fetch took %s, wall clock bound did not apply", time.Since(start))
	}
}

func TestFetch_DoesNotFollowRedirects(t *testing.T) {
	var hop2Hits atomic.Int32
	var redirects atomic.Int32
	srv := startTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jwks":
			redirects.Add(1)
			w.Header().Set("Location", "/hop2")
			w.WriteHeader(http.StatusFound)
		case "/hop2":
			hop2Hits.Add(1)
			fmt.Fprint(w, `{"keys":[]}`)
		default:
			http.NotFound(w, r)
		}
	})
	res := resolverForServer(t, srv)
	ref := &agentRef{identifier: "https://redir.test/jwks", fetchURL: srv.srv.URL + "/jwks", typ: discoveryJWKS}
	_, err := res.resolve(context.Background(), ref)
	if err == nil || !strings.Contains(err.Error(), "status 302") {
		t.Fatalf("err = %v, want status-302 discovery failure", err)
	}
	if hop2Hits.Load() != 0 {
		t.Errorf("redirect was followed: %d hits on hop2", hop2Hits.Load())
	}
	if redirects.Load() != 1 {
		t.Errorf("redirect endpoint hit %d times", redirects.Load())
	}
}

func TestFetch_Non200AndContentType(t *testing.T) {
	md := newMutableDirectory(t, `{"keys":[]}`)
	md.fail.Store(true)
	res := md.resolver(t)
	ref := &agentRef{identifier: "https://failing.test/x", fetchURL: md.srv.srv.URL, typ: discoveryJWKS}
	if _, err := res.resolve(context.Background(), ref); err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("err = %v, want status 500 failure", err)
	}

	srv := startTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html>not a directory</html>`)
	})
	res2 := resolverForServer(t, srv)
	ref2 := &agentRef{identifier: "https://html.test/x", fetchURL: srv.srv.URL, typ: discoveryJWKS}
	if _, err := res2.resolve(context.Background(), ref2); err == nil || !strings.Contains(err.Error(), "content type") {
		t.Fatalf("err = %v, want content-type refusal", err)
	}
}

func TestNegativeCache_PreventsRefetchStorm(t *testing.T) {
	md := newMutableDirectory(t, `{"keys":[]}`)
	md.fail.Store(true)
	res := md.resolver(t)
	ref := &agentRef{identifier: "https://down.test/x", fetchURL: md.srv.srv.URL, typ: discoveryJWKS}
	for range 5 {
		if _, err := res.resolve(context.Background(), ref); err == nil {
			t.Fatal("failing directory resolved")
		}
	}
	if md.hits.Load() != 1 {
		t.Errorf("failing directory fetched %d times, want 1 (negative cache)", md.hits.Load())
	}
}

func TestCacheEviction_JunkURLsCannotEvictRealKeys(t *testing.T) {
	md := newMutableDirectory(t, `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs","use":"sig"}]}`)
	res := md.resolver(t)
	res.neg.max = 8 // shrink the negative map so the flood exceeds it

	real := &agentRef{identifier: "https://real.test/x", fetchURL: md.srv.srv.URL, typ: discoveryJWKS}
	if _, err := res.resolve(context.Background(), real); err != nil {
		t.Fatal(err)
	}
	// Flood the negative map past its bound with unresolvable URLs.
	for i := range 40 {
		junk := &agentRef{
			identifier: fmt.Sprintf("https://junk%d.test/x", i),
			fetchURL:   md.srv.srv.URL,
			typ:        discoveryJWKS,
		}
		md.fail.Store(true)
		_, _ = res.resolve(context.Background(), junk)
		md.fail.Store(false)
	}
	before := md.hits.Load()
	if _, err := res.resolve(context.Background(), real); err != nil {
		t.Fatalf("real entry lost: %v", err)
	}
	if md.hits.Load() != before {
		t.Errorf("real agent's keys were evicted by junk: %d -> %d fetches", before, md.hits.Load())
	}
}

func TestRotation_RemovedKeyStopsVerifyingAfterTTL(t *testing.T) {
	keyA := `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs","use":"sig"}]}`
	keyB := `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"nAbx7kkSTy1igV4dw4JYVxmU0RgsYy7cYK6WVz0IP2Q","use":"sig"}]}`
	md := newMutableDirectory(t, keyA)
	res := md.resolver(t)
	now := time.Now()
	res.now = func() time.Time { return now }
	ref := &agentRef{identifier: "https://rot.test/x", fetchURL: md.srv.srv.URL, typ: discoveryJWKS}

	set1, err := res.resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	thumbA := okpThumbprint("Ed25519", "OKP", "JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs")
	if set1.selectKey(thumbA, now) == nil {
		t.Fatal("key A not selectable before rotation")
	}

	// Within the TTL the cached set answers without a fetch.
	hits := md.hits.Load()
	res.resolve(context.Background(), ref)
	if md.hits.Load() != hits {
		t.Errorf("fresh cache entry caused a refetch")
	}

	// Rotate: the directory now publishes only key B. After the TTL a
	// refetch must replace the whole set - a resolved directory that
	// lacks the key is newer evidence (draft 6.10).
	md.body.Store(keyB)
	now = now.Add(25 * time.Hour) // past the cached entry's TTL
	thumbB := okpThumbprint("Ed25519", "OKP", "nAbx7kkSTy1igV4dw4JYVxmU0RgsYy7cYK6WVz0IP2Q")
	set2, err := res.resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if set2.selectKey(thumbA, now) != nil {
		t.Errorf("rotated-away key A still selectable after TTL refresh")
	}
	if set2.selectKey(thumbB, now) == nil {
		t.Errorf("new key B not selectable after rotation")
	}
}

func TestFetchFailure_DoesNotEvictCachedKeys(t *testing.T) {
	keyA := `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs","use":"sig"}]}`
	md := newMutableDirectory(t, keyA)
	res := md.resolver(t)
	now := time.Now()
	res.now = func() time.Time { return now }
	ref := &agentRef{identifier: "https://out.test/x", fetchURL: md.srv.srv.URL, typ: discoveryJWKS}

	if _, err := res.resolve(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	// The directory goes down and the TTL lapses: the cached keys keep
	// serving (draft 6.10 - an operator outage must not revoke its keys
	// at every verifier at once).
	md.fail.Store(true)
	now = now.Add(2 * time.Hour)
	set, err := res.resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("stale entry not served during outage: %v", err)
	}
	thumbA := okpThumbprint("Ed25519", "OKP", "JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs")
	if set.selectKey(thumbA, now) == nil {
		t.Error("stale-served key set lost its keys")
	}
}

func TestResolveCoalesces_ConcurrentFetches(t *testing.T) {
	release := make(chan struct{})
	var started sync.WaitGroup
	var hits atomic.Int32
	started.Add(1)
	srv := startTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		started.Done()
		<-release
		fmt.Fprint(w, `{"keys":[]}`)
	})
	res := resolverForServer(t, srv)
	ref := &agentRef{identifier: "https://burst.test/x", fetchURL: srv.srv.URL, typ: discoveryJWKS}

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = res.resolve(context.Background(), ref)
		}()
	}
	started.Wait()
	close(release)
	wg.Wait()
	if hits.Load() != 1 {
		t.Errorf("coalesced fetch hit the directory %d times, want 1", hits.Load())
	}
}

func TestResponseTTL_Clamping(t *testing.T) {
	now := time.Now()
	h := http.Header{}
	h.Set("Cache-Control", "max-age=1")
	if d, ok := responseTTL(h, now); !ok || d != minPositiveTTL {
		t.Errorf("max-age=1 -> %v (ok=%v), want clamp to %v", d, ok, minPositiveTTL)
	}
	h.Set("Cache-Control", "max-age=99999999")
	if d, _ := responseTTL(h, now); d != maxPositiveTTL {
		t.Errorf("huge max-age -> %v, want clamp to %v", d, maxPositiveTTL)
	}
	h.Del("Cache-Control")
	if _, ok := responseTTL(h, now); ok {
		t.Error("no caching headers should report ok=false")
	}
}

func TestKeyCountCap(t *testing.T) {
	items := make([]string, 33)
	for i := range items {
		items[i] = `{"kty":"OKP","crv":"Ed25519","x":"JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs"}`
	}
	tooMany := `{"keys":[` + strings.Join(items, ",") + `]}`
	if _, err := parseJWKS([]byte(tooMany)); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("33-key JWKS accepted: %v", err)
	}
}
