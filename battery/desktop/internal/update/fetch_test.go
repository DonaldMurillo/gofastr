package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAllowedURLSchemeRules(t *testing.T) {
	ok := []string{
		"https://example.com/manifest.json",
		"https://updates.example.com/a/b/manifest.json",
		"http://127.0.0.1:8099/manifest.json",
		"http://127.0.0.1/manifest.json.sig",
		"https://user@ex.com/manifest.json", // userinfo does not weaken https
		"HTTPS://example.com/x",             // url.Parse normalizes scheme case
	}
	for _, u := range ok {
		if !AllowedURL(u) {
			t.Errorf("AllowedURL(%q) = false, want true", u)
		}
	}
	bad := []string{
		"http://localhost/manifest.json",   // not the literal 127.0.0.1
		"http://example.com/manifest.json", // plain http off loopback
		"ftp://example.com/x",
		"file:///etc/passwd",
		"https://", // no host
		"not a url",
		"",
	}
	for _, u := range bad {
		if AllowedURL(u) {
			t.Errorf("AllowedURL(%q) = true, want false", u)
		}
	}
}

func TestFetchServesCappedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()
	body, err := Fetch(context.Background(), srv.Client(), srv.URL+"/x", 10)
	if err != nil || string(body) != "0123456789" {
		t.Fatalf("Fetch = %q, %v", body, err)
	}
}

func TestFetchRefusesBodyOverCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789A")) // 11 bytes
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), srv.Client(), srv.URL+"/x", 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestFetchRefusesBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), srv.Client(), srv.URL+"/x", 10); !errors.Is(err, ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch", err)
	}
}

// A redirect is held to the same scheme rule as the first URL: an
// allowed URL that redirects to plain http off loopback is refused, and
// the target is never asked. A redirect between allowed URLs follows.
func TestFetchRefusesRedirectOffRule(t *testing.T) {
	var targetHits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits++
		_, _ = w.Write([]byte("evil"))
	}))
	defer target.Close()
	// "localhost" is plain http off the literal 127.0.0.1, which the rule
	// refuses, while the listener still answers on it.
	offRule := strings.Replace(target.URL, "127.0.0.1", "localhost", 1) + "/x"
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			http.Redirect(w, r, target.URL+"/x", http.StatusFound)
			return
		}
		http.Redirect(w, r, offRule, http.StatusFound)
	}))
	defer src.Close()

	if _, err := Fetch(context.Background(), src.Client(), src.URL+"/bad", 10); !errors.Is(err, ErrBadURL) {
		t.Fatalf("err = %v, want ErrBadURL", err)
	}
	if targetHits != 0 {
		t.Fatalf("the off-rule target was asked %d times", targetHits)
	}
	body, err := Fetch(context.Background(), src.Client(), src.URL+"/ok", 10)
	if err != nil || string(body) != "evil" {
		t.Fatalf("allowed redirect: Fetch = %q, %v", body, err)
	}
}

// The redirect check wraps the caller's client without replacing its
// own CheckRedirect, and with none set a redirect loop still stops at
// Go's default of ten hops.
func TestFetchKeepsRedirectPolicy(t *testing.T) {
	var hops int
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	defer loop.Close()

	if _, err := Fetch(context.Background(), loop.Client(), loop.URL+"/x", 10); !errors.Is(err, ErrFetch) {
		t.Fatalf("loop: err = %v, want ErrFetch", err)
	}
	if hops != 10 {
		t.Fatalf("loop: %d requests, want 10", hops)
	}

	hops = 0
	c := loop.Client()
	var asked int
	c.CheckRedirect = func(*http.Request, []*http.Request) error {
		asked++
		return http.ErrUseLastResponse
	}
	if _, err := Fetch(context.Background(), c, loop.URL+"/x", 10); !errors.Is(err, ErrFetch) {
		t.Fatalf("own policy: err = %v, want ErrFetch (the 302 is not a 200)", err)
	}
	if asked != 1 || hops != 1 {
		t.Fatalf("own policy: asked %d, %d requests, want 1 and 1", asked, hops)
	}
}

func TestFetchRefusesDisallowedURLBeforeDialing(t *testing.T) {
	// A plain-http URL to a non-loopback host must be refused without
	// any request going out; the https loopback URL dials and fails as
	// a fetch error, proving the two rules differ.
	if _, err := Fetch(context.Background(), http.DefaultClient, "http://example.com/x", 10); !errors.Is(err, ErrBadURL) {
		t.Fatalf("err = %v, want ErrBadURL", err)
	}
	if _, err := Fetch(context.Background(), http.DefaultClient, "https://127.0.0.1:1/x", 10); !errors.Is(err, ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch", err)
	}
	// Caps out of range are refused outright.
	if _, err := Fetch(context.Background(), http.DefaultClient, "https://example.com/x", 0); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("zero cap err = %v", err)
	}
	if _, err := Fetch(context.Background(), http.DefaultClient, "https://example.com/x", MaxArchiveSize+1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over-cap err = %v", err)
	}
}
