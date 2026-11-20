//go:build linux

package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

func TestAssetURLRewriteKeepingModTime(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "0")
	for _, mode := range []string{"same recent revision", "changed inode", "changed ctime"} {
		t.Run(mode, func(t *testing.T) {
			ds := New(app.NewApp("asset-revision-test"))
			now := time.Now()
			ctime := now
			if mode != "same recent revision" {
				ctime = now.Add(-time.Hour)
			}
			st := &syscall.Stat_t{Ino: 42, Ctim: syscall.NsecToTimespec(ctime.UnixNano())}
			f := &fstest.MapFile{Data: []byte("<svg>v1</svg>"), ModTime: time.Unix(1_700_000_000, 0), Sys: st}
			ds.SetStaticFS(fstest.MapFS{"logo.svg": f})
			before := ds.AssetURL("/logo.svg")
			if !strings.Contains(before, "?v=") {
				t.Fatalf("initial URL = %q, want content hash", before)
			}
			f.Data = []byte("<svg>v2</svg>")
			switch mode {
			case "changed inode":
				st.Ino++
			case "changed ctime":
				st.Ctim = syscall.NsecToTimespec(ctime.Add(time.Second).UnixNano())
			}
			after := ds.AssetURL("/logo.svg")
			if after == before || !strings.Contains(after, "?v=") {
				t.Errorf("AssetURL = %q after rewrite, want a new content hash (was %q)", after, before)
			}
			for _, url := range []string{before, after} {
				rec := httptest.NewRecorder()
				ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
				if rec.Code != http.StatusOK || rec.Body.String() != string(f.Data) {
					t.Fatalf("GET %s: status %d, body %q, want 200 with new bytes", url, rec.Code, rec.Body.String())
				}
				immutable := strings.Contains(rec.Header().Get("Cache-Control"), "immutable")
				if want := url != before; immutable != want {
					t.Errorf("GET %s: immutable = %v, want %v", url, immutable, want)
				}
			}
		})
	}
}

func TestAssetVersionMemoization(t *testing.T) {
	for _, tc := range []struct {
		name        string
		ctime       time.Time
		wantEntries int
	}{
		{"missing ctime", time.Time{}, 1},
		{"old ctime", time.Now().Add(-time.Hour), 1},
		{"recent ctime", time.Now(), 0},
		{"future ctime", time.Now().Add(time.Hour), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fstest.MapFile{}
			if !tc.ctime.IsZero() {
				f.Sys = &syscall.Stat_t{Ino: 42, Ctim: syscall.NsecToTimespec(tc.ctime.UnixNano())}
			}
			ds := &UIHost{staticFS: fstest.MapFS{"empty.css": f}}
			first := ds.AssetURL("/empty.css")
			if first != "/empty.css?v=e3b0c44298fc" || ds.AssetURL("/empty.css") != first {
				t.Fatalf("empty asset URL = %q, want stable empty content hash", first)
			}
			if got := len(ds.versions.entries); got != tc.wantEntries {
				t.Errorf("cached entries = %d, want %d", got, tc.wantEntries)
			}
		})
	}
}
