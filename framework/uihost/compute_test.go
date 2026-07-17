package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/compute"
)

func TestUIHostServesComputeAssets(t *testing.T) {
	const worker = "self.onmessage=function(){}"
	compute.RegisterWorker("uihost-worker", []byte(worker))
	asset, _ := compute.LookupWorker("uihost-worker")
	ds := newTestUIHost()

	page := httptest.NewRecorder()
	ds.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("page status=%d", page.Code)
	}
	if body := page.Body.String(); !strings.Contains(body, `id="gofastr-compute-assets"`) ||
		!strings.Contains(body, `"uihost-worker":{"js":"`+asset.Hash()+`"}`) {
		t.Fatalf("page missing compute manifest: %s", body)
	}

	rec := httptest.NewRecorder()
	path := "/__gofastr/compute/uihost-worker.js?v=" + asset.Hash()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status=%d body=%q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("Content-Type=%q", got)
	}
	if rec.Body.String() != worker {
		t.Fatalf("body=%q", rec.Body.String())
	}
}
