package relay

import (
	"io"
	"net/http"
	"testing"
	"time"
)

// TestForwardedNotRelayedUpstream pins that an inbound RFC 7239
// Forwarded header never reaches the vendor.
//
// Recon flagged this as a live hole (relay's stripInbound denylist
// covers credentials and the X-Forwarded- prefix only). At runtime the
// hole is closed one layer BELOW the relay: httputil.ReverseProxy in
// Rewrite mode removes Forwarded, X-Forwarded, X-Forwarded-Host, and
// X-Forwarded-Proto from the outbound request before Rewrite is called
// (Rewrite field docs, net/http/httputil, go1.27). Relay uses Rewrite
// (newProxy), so the stdlib performs this strip; relay's own
// stripInbound does NOT know the name.
//
// This pin keeps the documented contract ("Forwarded metadata is
// derived from the connection, never relayed from the client" —
// proxy.go; "Inbound forwarding headers are never trusted" — relay.md)
// from silently regressing if the proxy is ever switched to the
// deprecated Director hook, which does not perform the removal, or if
// Forwarded is re-added explicitly.
func TestForwardedNotRelayedUpstream(t *testing.T) {
	base, cap := startRelayCap(t, defaultCfg)

	req, err := http.NewRequest(http.MethodGet, base+"/__gofastr/t/e/x", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Forwarded", "for=203.0.113.7;host=trusted.internal;proto=https")
	req.Header.Add("Forwarded", "for=198.51.100.9")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the relay must answer normally)", res.StatusCode)
	}

	_, _, _, _, h := cap.snapshot()
	if got := h.Values("Forwarded"); len(got) != 0 {
		t.Errorf("inbound Forwarded reached the upstream verbatim: %q — forwarding metadata must be derived from the connection, never relayed from the client", got)
	}
}

// TestClientIPHeadersNotRelayedUpstream pins that client-claimed
// forwarding-identity headers never reach the vendor verbatim.
//
// The documented inbound posture strips "spoofable forwarding
// metadata" (stripInbound, proxy.go) and never trusts inbound
// forwarding headers, "one `curl -H` away from arbitrary values"
// (relay.md hardening table). X-Forwarded-* and RFC 7239 Forwarded are
// covered — by stripInbound's prefix rule and the stdlib Rewrite-mode
// removal respectively — but the de-facto client-IP forwarding names
// (nginx's X-Real-IP, CDN/Akamai's True-Client-IP, Azure/various
// X-Client-IP) match neither rule and reach the vendor untouched.
//
// Attack: curl -H 'X-Real-IP: 203.0.113.7' against the public mount; a
// vendor (or vendor-side WAF/rate-limiter) that honors any of these
// names records attacker-chosen client identity behind the app's own
// origin, defeating the same contract X-Forwarded-For stripping
// defends. The asserted set is the established trio, not an exhaustive
// enumeration; the contract line is the class: client-claimed
// forwarding identity must not pass through.
func TestClientIPHeadersNotRelayedUpstream(t *testing.T) {
	base, cap := startRelayCap(t, defaultCfg)

	req, err := http.NewRequest(http.MethodGet, base+"/__gofastr/t/e/x", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for name, val := range map[string]string{
		"X-Real-IP":      "203.0.113.7",
		"X-Client-IP":    "203.0.113.7",
		"True-Client-IP": "203.0.113.7",
	} {
		req.Header.Set(name, val)
	}
	// Control: the one client-IP header the relay already handles must
	// come through as the derived peer, not the spoofed value.
	req.Header.Set("X-Forwarded-For", "203.0.113.7")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the relay must answer normally)", res.StatusCode)
	}

	_, _, _, _, h := cap.snapshot()
	for _, name := range []string{"X-Real-Ip", "X-Client-Ip", "True-Client-Ip"} {
		if got := h.Values(name); len(got) != 0 {
			t.Errorf("inbound %s reached the upstream verbatim: %q — client-claimed forwarding identity is spoofable metadata (one curl -H away from arbitrary values) and must be derived from the connection, never relayed", name, got)
		}
	}
	if got := h.Get("X-Forwarded-For"); got == "203.0.113.7" || got == "" {
		t.Errorf("X-Forwarded-For = %q, want the relay-derived peer address (spoofed inbound value must not survive)", got)
	}
}

// TestUpstreamResponseBodyCapped pins a bound on the proxied response
// direction.
//
// The request direction is braked twice — MaxBodyBytes (default 8 MiB,
// 413 on overflow) and relay.md's "Egress is your cost now" warning —
// but nothing bounds what the upstream sends back: handler caps
// req.Body only, modifyResponse touches headers only, and ReverseProxy
// streams the vendor's body to the client unbounded. A vendor endpoint
// that never ends its response holds a goroutine, a socket pair, and
// bandwidth for the full 30s request deadline per open client request.
//
// The upstream here streams zeros forever and buffers nothing, so the
// test pays only for the bytes the client actually reads. The asserted
// budget is defaultMaxBodyBytes — the same brake the relay documents
// for the request direction; if the fix lands a dedicated
// response-side default instead, adjust this one constant.
func TestUpstreamResponseBodyCapped(t *testing.T) {
	base := startRelay(t, defaultCfg, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64<<10)
		fl := w.(http.Flusher)
		for r.Context().Err() == nil {
			if _, err := w.Write(buf); err != nil {
				return
			}
			fl.Flush()
		}
	})

	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Get(base + "/__gofastr/t/e/stream")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	n, readErr := io.Copy(io.Discard, io.LimitReader(res.Body, defaultMaxBodyBytes+1))
	res.Body.Close()

	if n > defaultMaxBodyBytes {
		t.Errorf("client read %d bytes from one upstream response (readErr=%v) with no cap hit: the response direction needs the same bound the request direction enforces (defaultMaxBodyBytes=%d)", n, readErr, defaultMaxBodyBytes)
	}
}
