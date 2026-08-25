package uihost

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// Every text asset under /__gofastr/* follows one policy: ETag + 304 on
// every response, immutable exactly when ?v= matches the current hash,
// no-cache otherwise. Pinned as a table so a new asset endpoint can't
// quietly ship without a validator again.
func TestVersionedAssetPolicy(t *testing.T) {
	application := app.NewApp("t")
	application.Register("/", &testHomeComp{}, nil)
	ds := New(application)

	get := func(url, inm string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", url, nil)
		if inm != "" {
			req.Header.Set("If-None-Match", inm)
		}
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, req)
		return w
	}

	for _, path := range []string{"/__gofastr/app.css", "/__gofastr/runtime.js", "/__gofastr/color-scheme.js"} {
		t.Run(path, func(t *testing.T) {
			bare := get(path, "")
			if bare.Code != 200 {
				t.Fatalf("bare GET: status %d", bare.Code)
			}
			etag := bare.Header().Get("ETag")
			if etag == "" {
				t.Fatal("bare GET carries no ETag — every re-visit re-downloads the full body")
			}
			if cc := bare.Header().Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
				t.Fatalf("bare GET Cache-Control = %q, want no-cache (URL carries no version)", cc)
			}
			if bare.Body.Len() == 0 {
				t.Fatal("bare GET returned an empty body")
			}

			hash := strings.Trim(etag, `"`)
			versioned := get(path+"?v="+hash, "")
			if cc := versioned.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
				t.Errorf("matching ?v= Cache-Control = %q, want immutable", cc)
			}

			stale := get(path+"?v=old-deploy", "")
			if cc := stale.Header().Get("Cache-Control"); strings.Contains(cc, "immutable") {
				t.Errorf("stale ?v= must not be immutable (would poison the old URL): %q", cc)
			}

			notMod := get(path, etag)
			if notMod.Code != 304 {
				t.Errorf("If-None-Match revalidation: status %d, want 304", notMod.Code)
			}
			if notMod.Body.Len() != 0 {
				t.Errorf("304 must have an empty body, got %d bytes", notMod.Body.Len())
			}
		})
	}
}

// The injected chrome must reference the versioned URLs, or the immutable
// path never gets exercised by real pages.
func TestChromeReferencesVersionedAssets(t *testing.T) {
	application := app.NewApp("t")
	application.Register("/", &testHomeComp{}, nil)
	ds := New(application)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	page := w.Body.String()

	for _, want := range []string{"/__gofastr/app.css?v=", "/__gofastr/runtime.js?v=", "/__gofastr/color-scheme.js?v="} {
		if !strings.Contains(page, want) {
			t.Errorf("rendered chrome missing versioned reference %q", want)
		}
	}
}

// ScriptHandler is the exported serving half for host-app bootstrap
// scripts: same versioned-text policy as every /__gofastr script.
func TestScriptHandlerHeadersAndETag(t *testing.T) {
	js := []byte("window.plug = 1;\n")
	w := httptest.NewRecorder()
	ScriptHandler(js).ServeHTTP(w, httptest.NewRequest("GET", "/plug.js", nil))

	if ct := w.Header().Get("Content-Type"); ct != "application/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/javascript; charset=utf-8", ct)
	}
	if xo := w.Header().Get("X-Content-Type-Options"); xo != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", xo)
	}
	if etag := w.Header().Get("ETag"); etag != `"`+hashStrings(string(js))+`"` {
		t.Errorf("ETag = %q, want quoted hashStrings hash", etag)
	}
	if w.Body.String() != string(js) {
		t.Errorf("body = %q, want %q", w.Body.String(), js)
	}
}

func TestScriptHandlerImmutableOnlyWithV(t *testing.T) {
	h := ScriptHandler([]byte("x"))
	get := func(url string) string {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		return w.Header().Get("Cache-Control")
	}
	if cc := get("/plug.js?v=" + hashStrings("x")); cc != "public, max-age=31536000, immutable" {
		t.Errorf("matching ?v= Cache-Control = %q, want immutable", cc)
	}
	if cc := get("/plug.js?v=stale-deploy"); cc != "no-cache" {
		t.Errorf("stale ?v= Cache-Control = %q, want no-cache", cc)
	}
	if cc := get("/plug.js"); cc != "no-cache" {
		t.Errorf("bare Cache-Control = %q, want no-cache", cc)
	}
}

func TestScriptHandler304Revalidation(t *testing.T) {
	h := ScriptHandler([]byte("x"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/plug.js", nil))
	etag := w.Header().Get("ETag")

	req := httptest.NewRequest("GET", "/plug.js", nil)
	req.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req)
	if w2.Code != 304 {
		t.Fatalf("If-None-Match revalidation: status %d, want 304", w2.Code)
	}
	if w2.Body.Len() != 0 {
		t.Errorf("304 must have an empty body, got %d bytes", w2.Body.Len())
	}
}

// ScriptURL and ScriptHandler must share the hash primitive: the URL the
// app hands to RegisterExternalScript must hit the handler's immutable
// branch, and the hash must change when the bytes do.
func TestScriptURLRoundTripImmutable(t *testing.T) {
	js := []byte("console.log('plug')\n")
	mux := http.NewServeMux()
	mux.Handle("/plug.js", ScriptHandler(js))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	url := ScriptURL("/plug.js", js)
	if !strings.HasPrefix(url, "/plug.js?v=") {
		t.Fatalf("ScriptURL = %q, want /plug.js?v=<hash>", url)
	}
	if ScriptURL("/plug.js", []byte("other")) == url {
		t.Error("ScriptURL must change when the script bytes change")
	}

	resp, err := http.Get(srv.URL + url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("round-trip Cache-Control = %q, want immutable (ScriptURL/ScriptHandler hash mismatch?)", cc)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != string(js) {
		t.Errorf("round-trip body = %q, want %q", body, js)
	}
}
