package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// NeedsSameSiteHop hops a cross-site arrival and one with no Fetch
// Metadata (a browser sends none to a plain-HTTP LAN address), and lets
// a same-origin or typed-URL request, or a hop's own reload, through.
func TestNeedsSameSiteHop(t *testing.T) {
	cases := []struct {
		name, url, site, origin string
		want                    bool
	}{
		{"cross-site", "/x", "cross-site", "", true},
		{"no fetch metadata", "/x?t=1", "", "", true},
		{"no metadata, foreign Origin", "/x", "", "https://evil.example", true},
		{"hop reload", "/x?t=1&" + SameSiteHopParam + "=1", "", "", false},
		{"marker on a cross-site link", "/x?" + SameSiteHopParam + "=1", "cross-site", "", true},
		{"same-origin", "/x", "same-origin", "", false},
		{"typed URL", "/x", "none", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://192.168.1.5:8080"+c.url, nil)
			if c.site != "" {
				r.Header.Set("Sec-Fetch-Site", c.site)
			}
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			if got := NeedsSameSiteHop(r); got != c.want {
				t.Fatalf("NeedsSameSiteHop = %v, want %v", got, c.want)
			}
		})
	}
}

// The marker rides only a reload that will carry no Fetch Metadata, and
// only once.
func TestSameSiteHopTarget(t *testing.T) {
	noFM := httptest.NewRequest("GET", "/x", nil)
	fm := httptest.NewRequest("GET", "/x", nil)
	fm.Header.Set("Sec-Fetch-Site", "cross-site")
	cases := []struct {
		name, target string
		fm           bool
		want         string
	}{
		{"no query", "/setup", false, "/setup?" + SameSiteHopParam + "=1"},
		{"query", "/setup?token=a", false, "/setup?token=a&" + SameSiteHopParam + "=1"},
		{"already marked", "/setup?" + SameSiteHopParam + "=1", false, "/setup?" + SameSiteHopParam + "=1"},
		{"fetch metadata", "/setup?token=a", true, "/setup?token=a"},
		{"fragment", "/setup#step2", false, "/setup?" + SameSiteHopParam + "=1#step2"},
		{"query and fragment", "/setup?token=a#step2", false, "/setup?token=a&" + SameSiteHopParam + "=1#step2"},
		{"marked query with fragment", "/setup?" + SameSiteHopParam + "#step2", false, "/setup?" + SameSiteHopParam + "#step2"},
		{"marker in fragment", "/setup#step2?" + SameSiteHopParam + "=1", false, "/setup?" + SameSiteHopParam + "=1#step2?" + SameSiteHopParam + "=1"},
		{"fragment only", "#step2", false, "?" + SameSiteHopParam + "=1#step2"},
		{"empty fragment and query", "/setup?#", false, "/setup?" + SameSiteHopParam + "=1#"},
		{"malformed query escape", "/setup?token=%zz", false, "/setup?token=%zz&" + SameSiteHopParam + "=1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := noFM
			if c.fm {
				r = fm
			}
			if got := SameSiteHopTarget(r, c.target); got != c.want {
				t.Fatalf("SameSiteHopTarget(%q) = %q, want %q", c.target, got, c.want)
			}
		})
	}
}

func TestSameSiteHopFragmentReload(t *testing.T) {
	const refreshPrefix = "0;url="
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		needsHop := NeedsSameSiteHop(r)
		t.Logf("server received %s (Sec-Fetch-Site=%q, %s=%q, needs hop=%t)",
			r.RequestURI, r.Header.Get("Sec-Fetch-Site"), SameSiteHopParam, r.URL.Query().Get(SameSiteHopParam), needsHop)
		if needsHop {
			w.Header().Set("Refresh", refreshPrefix+SameSiteHopTarget(r, "/setup#step2"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := srv.Client()
	client.Timeout = 5 * time.Second
	first, err := client.Get(srv.URL + "/setup#step2")
	if err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	refresh := first.Header.Get("Refresh")
	t.Logf("first response: %s, Refresh: %s", first.Status, refresh)
	target, ok := strings.CutPrefix(refresh, refreshPrefix)
	if !ok {
		t.Fatalf("expected a hop, got Refresh %q", refresh)
	}
	reload, err := client.Get(srv.URL + target)
	if err != nil {
		t.Fatal(err)
	}
	reload.Body.Close()
	refresh = reload.Header.Get("Refresh")
	t.Logf("reload response: %s, Refresh: %q", reload.Status, refresh)
	if reload.StatusCode != http.StatusNoContent || refresh != "" {
		t.Fatal("expected the hop to terminate after one reload")
	}
}
