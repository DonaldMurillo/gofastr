package uihost

import (
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
