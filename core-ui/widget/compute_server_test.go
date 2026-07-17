package widget

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/compute"
)

func TestServeComputeWorker(t *testing.T) {
	compute.RegisterWorker("route-worker", []byte("self.onmessage=function(){}"))
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/compute/route-worker.js?v=hash", nil)
	rec := httptest.NewRecorder()

	ServeComputeAsset(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("Content-Type=%q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'" {
		t.Fatalf("Content-Security-Policy=%q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control=%q", got)
	}
	if got := rec.Body.String(); got != "self.onmessage=function(){}" {
		t.Fatalf("body=%q", got)
	}
}

func TestServeComputeWASM(t *testing.T) {
	wasm := []byte("\x00asm\x01\x00\x00\x00")
	compute.RegisterWASM("route-wasm", wasm)
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/compute/route-wasm.wasm?v=hash", nil)
	rec := httptest.NewRecorder()

	ServeComputeAsset(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/wasm" {
		t.Fatalf("Content-Type=%q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control=%q", got)
	}
	if got := rec.Body.Bytes(); string(got) != string(wasm) {
		t.Fatalf("body=%q want %q", got, wasm)
	}
}

func TestServeComputeUnknown(t *testing.T) {
	for _, path := range []string{
		"/__gofastr/compute/missing.js",
		"/__gofastr/compute/missing.wasm",
		"/__gofastr/compute/nested/name.js",
		"/__gofastr/compute/name.txt",
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ServeComputeAsset(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status=%d want %d", rec.Code, http.StatusNotFound)
			}
		})
	}
}

func TestComputeManifestScript(t *testing.T) {
	compute.RegisterWorker("manifest-pair", []byte("worker"))
	compute.RegisterWASM("manifest-pair", []byte("wasm"))
	script := ComputeManifestScript()
	if !strings.HasPrefix(script, `<script type="application/json" id="gofastr-compute-assets">`) {
		t.Fatalf("unexpected script: %q", script)
	}
	start := strings.IndexByte(script, '>') + 1
	end := strings.LastIndex(script, "</script>")
	var manifest map[string]compute.Versions
	if err := json.Unmarshal([]byte(script[start:end]), &manifest); err != nil {
		t.Fatalf("manifest JSON: %v", err)
	}
	entry := manifest["manifest-pair"]
	if entry.JS == "" || entry.WASM == "" {
		t.Fatalf("entry=%+v", entry)
	}
	if !strings.Contains(RuntimeModuleManifestScript(), script) {
		t.Fatal("compute manifest not emitted alongside runtime module manifest")
	}
}

func TestComputeManifestEscapesClosingScript(t *testing.T) {
	script := computeManifestScript(map[string]compute.Versions{
		"x": {JS: "</script><script>alert(1)</script>"},
	})
	inner := script[strings.IndexByte(script, '>')+1 : strings.LastIndex(script, "</script>")]
	if strings.Contains(inner, "</") {
		t.Fatalf("manifest contains raw closing tag: %q", inner)
	}
	var got map[string]compute.Versions
	if err := json.Unmarshal([]byte(inner), &got); err != nil {
		t.Fatalf("escaped manifest JSON: %v", err)
	}
	if got["x"].JS != "</script><script>alert(1)</script>" {
		t.Fatalf("escaped value=%q", got["x"].JS)
	}
}
