package handler

import (
	"net/http/httptest"
	"testing"
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
