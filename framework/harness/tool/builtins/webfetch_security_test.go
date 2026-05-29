package builtins

import (
	"context"
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
