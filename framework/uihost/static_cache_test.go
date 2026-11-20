package uihost

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// newStaticDirHost serves one project static file (nav.js) out of a temp
// static dir on the same shape a generated project uses.
func newStaticDirHost(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nav.js"), []byte("console.log('nav v2')"), 0644); err != nil {
		t.Fatal(err)
	}
	a := app.NewApp("static-cache-test")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	server := httptest.NewServer(New(a, WithStaticDir(dir)))
	t.Cleanup(server.Close)
	return server
}

// getStatic fetches url and returns the response with a drained body.
func getStatic(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// --- #219: project static assets must not be heuristically cached in dev ---

func TestDevServesStaticWithNoStore(t *testing.T) {
	server := newStaticDirHost(t)
	// Pin both vars so the outcome doesn't depend on the outer env.
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")

	resp := getStatic(t, server.URL+"/nav.js")
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("dev static response Cache-Control = %q, want no-store", cc)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "console.log('nav v2')" {
		t.Errorf("dev static body = %q, want the file bytes", body)
	}
}

func TestDevServesStaticFSWithNoStore(t *testing.T) {
	fsys := fstest.MapFS{"app.css": &fstest.MapFile{Data: []byte("body{margin:0}")}}
	a := app.NewApp("static-cache-fs-test")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)
	ds.SetStaticFS(fsys)
	server := httptest.NewServer(ds)
	t.Cleanup(server.Close)

	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")

	resp := getStatic(t, server.URL+"/app.css")
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("dev static-FS response Cache-Control = %q, want no-store", cc)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "body{margin:0}" {
		t.Errorf("dev static-FS body = %q, want the file bytes", body)
	}
}

type nonSeekingStaticFS struct{ fs.FS }

func (s nonSeekingStaticFS) Open(name string) (fs.File, error) {
	f, err := s.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return struct{ fs.File }{f}, nil
}

func TestStaticFSNonSeekingFile(t *testing.T) {
	const body = "body{margin:0}"
	modified := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{
		"app.css": {Data: []byte(body), ModTime: modified},
		"asset":   {Data: []byte(body), ModTime: modified},
	}
	a := app.NewApp("static-nonseeking-test")
	ds := New(a)
	ds.SetStaticFS(nonSeekingStaticFS{fsys})

	for _, tc := range []struct {
		name, method, target, byteRange, wantBody, wantType string
	}{
		{"get", http.MethodGet, "/app.css", "", body, "text/css; charset=utf-8"},
		{"extensionless", http.MethodGet, "/asset", "", body, "application/octet-stream"},
		{"range", http.MethodGet, "/app.css", "bytes=0-3", body, "text/css; charset=utf-8"},
		{"head", http.MethodHead, "/app.css", "", "", "text/css; charset=utf-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
			if tc.byteRange != "" {
				req.Header.Set("Range", tc.byteRange)
			}
			rec := httptest.NewRecorder()
			ds.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || rec.Body.String() != tc.wantBody {
				t.Fatalf("status = %d, body = %q, want 200 and %q", rec.Code, rec.Body.String(), tc.wantBody)
			}
			if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
				t.Errorf("Content-Length = %q, want %d", got, len(body))
			}
			if got := rec.Header().Get("Content-Type"); got != tc.wantType {
				t.Errorf("Content-Type = %q, want %q", got, tc.wantType)
			}
			if got := rec.Header().Get("Accept-Ranges"); got != "" {
				t.Errorf("Accept-Ranges = %q, want empty", got)
			}
		})
	}

	get := httptest.NewRecorder()
	ds.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	if got, want := get.Header().Get("Last-Modified"), modified.Format(http.TimeFormat); got != want {
		t.Fatalf("non-seeking Last-Modified = %q, want %q", got, want)
	}
	reval := httptest.NewRequest(http.MethodGet, "/app.css", nil)
	reval.Header.Set("If-Modified-Since", modified.Format(http.TimeFormat))
	rec304 := httptest.NewRecorder()
	ds.ServeHTTP(rec304, reval)
	if rec304.Code != http.StatusNotModified || rec304.Body.Len() != 0 {
		t.Fatalf("non-seeking conditional: status = %d, body = %q, want 304 with no body", rec304.Code, rec304.Body.String())
	}

	post := httptest.NewRequest(http.MethodPost, "/app.css", nil)
	post.Header.Set("If-Modified-Since", modified.Format(http.TimeFormat))
	recPost := httptest.NewRecorder()
	ds.ServeHTTP(recPost, post)
	if recPost.Code == http.StatusNotModified {
		t.Fatalf("non-seeking POST with If-Modified-Since answered 304; the date condition applies to GET and HEAD only")
	}
	postStar := httptest.NewRequest(http.MethodPost, "/app.css", nil)
	postStar.Header.Set("If-None-Match", "*")
	recPostStar := httptest.NewRecorder()
	ds.ServeHTTP(recPostStar, postStar)
	if recPostStar.Code != http.StatusPreconditionFailed {
		t.Fatalf("non-seeking POST with If-None-Match *: status = %d, want 412", recPostStar.Code)
	}
	star := httptest.NewRequest(http.MethodGet, "/app.css", nil)
	star.Header.Set("If-None-Match", "*")
	recStar := httptest.NewRecorder()
	ds.ServeHTTP(recStar, star)
	if recStar.Code != http.StatusNotModified || recStar.Body.Len() != 0 {
		t.Fatalf("non-seeking If-None-Match *: status = %d, body = %q, want 304 with no body", recStar.Code, recStar.Body.String())
	}

	ds.SetStaticFS(fsys)
	req := httptest.NewRequest(http.MethodGet, "/app.css", nil)
	req.Header.Set("Range", "bytes=0-3")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "body" {
		t.Fatalf("seekable range: status = %d, body = %q, want 206 and body", rec.Code, rec.Body.String())
	}
	req.Header.Set("If-Modified-Since", modified.Format(http.TimeFormat))
	rec = httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("seekable conditional: status = %d, body = %q, want 304 with no body", rec.Code, rec.Body.String())
	}
}

func TestStaticFSPreconditionParity(t *testing.T) {
	modified := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{"app.css": {Data: []byte("body{margin:0}"), ModTime: modified}}
	host := func(fsys fs.FS) *UIHost {
		ds := New(app.NewApp("static-precondition-test"))
		ds.SetStaticFS(fsys)
		return ds
	}
	seekable, stream := host(fsys), host(nonSeekingStaticFS{fsys})
	before := modified.Add(-24 * time.Hour).Format(http.TimeFormat)
	after := modified.Add(24 * time.Hour).Format(http.TimeFormat)
	for _, tc := range []struct {
		name, method string
		headers      map[string]string
		want         int
	}{
		{"if-match stale", http.MethodGet, map[string]string{"If-Match": `"stale"`}, http.StatusPreconditionFailed},
		{"if-match star", http.MethodGet, map[string]string{"If-Match": "*"}, http.StatusOK},
		{"if-match list with star", http.MethodGet, map[string]string{"If-Match": `"a", *`}, http.StatusOK},
		{"if-match malformed before star", http.MethodGet, map[string]string{"If-Match": "junk, *"}, http.StatusPreconditionFailed},
		{"unmodified-since before", http.MethodGet, map[string]string{"If-Unmodified-Since": before}, http.StatusPreconditionFailed},
		{"unmodified-since before head", http.MethodHead, map[string]string{"If-Unmodified-Since": before}, http.StatusPreconditionFailed},
		{"unmodified-since after", http.MethodGet, map[string]string{"If-Unmodified-Since": after}, http.StatusOK},
		{"unmodified-since bad date", http.MethodGet, map[string]string{"If-Unmodified-Since": "yesterday"}, http.StatusOK},
		{"if-match star beats unmodified-since", http.MethodGet, map[string]string{"If-Match": "*", "If-Unmodified-Since": before}, http.StatusOK},
		{"if-match stale beats none-match star", http.MethodGet, map[string]string{"If-Match": `"stale"`, "If-None-Match": "*"}, http.StatusPreconditionFailed},
		{"unmodified-since beats none-match star", http.MethodGet, map[string]string{"If-Unmodified-Since": before, "If-None-Match": "*"}, http.StatusPreconditionFailed},
		{"unmodified-since beats modified-since", http.MethodGet, map[string]string{"If-Unmodified-Since": before, "If-Modified-Since": after}, http.StatusPreconditionFailed},
		{"none-match list with star", http.MethodGet, map[string]string{"If-None-Match": `"a", *`}, http.StatusNotModified},
		{"post if-match stale", http.MethodPost, map[string]string{"If-Match": `"stale"`}, http.StatusPreconditionFailed},
		{"post if-match star", http.MethodPost, map[string]string{"If-Match": "*"}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serve := func(ds *UIHost) *httptest.ResponseRecorder {
				req := httptest.NewRequest(tc.method, "/app.css", nil)
				for k, v := range tc.headers {
					req.Header.Set(k, v)
				}
				rec := httptest.NewRecorder()
				ds.ServeHTTP(rec, req)
				return rec
			}
			seek, str := serve(seekable), serve(stream)
			if seek.Code != tc.want {
				t.Fatalf("seekable status = %d, want %d", seek.Code, tc.want)
			}
			if str.Code != seek.Code || str.Body.String() != seek.Body.String() {
				t.Fatalf("stream = %d %q, seekable = %d %q", str.Code, str.Body.String(), seek.Code, seek.Body.String())
			}
		})
	}
}

func TestProdStaticSetsNoCacheControl(t *testing.T) {
	server := newStaticDirHost(t)

	// dev.Enabled() is evaluated per request, so one host can serve both
	// non-dev configurations.
	t.Run("dev flag off", func(t *testing.T) {
		t.Setenv("GOFASTR_DEV", "0")
		resp := getStatic(t, server.URL+"/nav.js")
		if cc := resp.Header.Get("Cache-Control"); cc != "" {
			t.Errorf("non-dev static response Cache-Control = %q, want empty", cc)
		}
	})
	t.Run("production env wins", func(t *testing.T) {
		t.Setenv("GOFASTR_ENV", "production")
		t.Setenv("GOFASTR_DEV", "1")
		resp := getStatic(t, server.URL+"/nav.js")
		if cc := resp.Header.Get("Cache-Control"); cc != "" {
			t.Errorf("production static response Cache-Control = %q, want empty", cc)
		}
	})
}
