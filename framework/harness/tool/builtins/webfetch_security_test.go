package builtins

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWebFetchSSRFGuard asserts WebFetch fails closed against
// private/loopback/link-local destinations and does not follow a
// redirect into one.
func TestWebFetchSSRFGuard(t *testing.T) {
	// Direct fetch of loopback / link-local / private literals is refused.
	for _, raw := range []string{
		"http://127.0.0.1/",
		"http://169.254.169.254/latest/meta-data/", // cloud metadata
		"http://[::1]/",
		"http://10.0.0.5/",
	} {
		res, _ := (WebFetch{}).Run(context.Background(), mustCall(t, map[string]any{
			"url": raw,
		}), nil)
		if res == nil || !res.IsError {
			t.Errorf("WebFetch reached internal address %q: %+v", raw, res)
		}
	}

	// Redirect into loopback must be re-validated and blocked on every
	// hop. AllowPrivateHosts relaxes only the INITIAL preflight (so a
	// test can reach an httptest redirector that listens on loopback);
	// the redirect target is still re-validated and refused.
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("SECRET-INTERNAL-BODY"))
	}))
	defer internal.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL, http.StatusFound)
	}))
	defer redirector.Close()

	tl := WebFetch{HTTPClient: redirector.Client(), AllowPrivateHosts: true}
	res, _ := tl.Run(context.Background(), mustCall(t, map[string]any{"url": redirector.URL}), nil)
	if res != nil && !res.IsError && strings.Contains(res.Content[0].Text, "SECRET-INTERNAL-BODY") {
		t.Errorf("WebFetch followed redirect into internal address: %+v", res)
	}
}

// TestInternalIPRanges asserts isInternalIP covers CGNAT and
// IPv4-mapped IPv6 forms of internal addresses, not just the
// canonical IPv4/IPv6 literals.
func TestInternalIPRanges(t *testing.T) {
	internal := []string{
		"100.64.0.1",              // CGNAT (RFC 6598)
		"100.127.255.255",         // CGNAT upper edge
		"::ffff:169.254.169.254",  // IPv4-mapped cloud metadata
		"::ffff:10.0.0.5",         // IPv4-mapped private
		"::ffff:127.0.0.1",        // IPv4-mapped loopback
	}
	for _, s := range internal {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("bad test IP %q", s)
		}
		if !isInternalIP(ip) {
			t.Errorf("isInternalIP(%s) = false; want internal", s)
		}
	}
	// Happy path: a genuine public address is not flagged.
	if isInternalIP(net.ParseIP("93.184.216.34")) {
		t.Errorf("isInternalIP flagged a public address")
	}
}

// TestWebFetchRebindDialReject asserts the transport refuses to
// complete a connection to an internal IP at dial time (closing the
// DNS-rebinding TOCTOU where preflight saw a public IP). We assert
// the dial-time guard rejects a loopback target.
func TestWebFetchRebindDialReject(t *testing.T) {
	// AllowPrivateHosts skips the preflight (simulating a hostname that
	// preflighted as public) but the dial-time Control hook must still
	// reject the actual internal IP it connects to.
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("SECRET-INTERNAL-BODY"))
	}))
	defer internal.Close()

	tl := WebFetch{AllowPrivateHosts: true}
	res, _ := tl.Run(context.Background(), mustCall(t, map[string]any{"url": internal.URL}), nil)
	if res != nil && !res.IsError && strings.Contains(res.Content[0].Text, "SECRET-INTERNAL-BODY") {
		t.Errorf("WebFetch dialed an internal address despite dial-time guard: %+v", res)
	}
}
