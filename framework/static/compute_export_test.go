package static

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/compute"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// Exported pages carry the compute manifest and the compute module, which
// fetches /__gofastr/compute/<name>.js|.wasm. A static host resolves those
// by path, so every registered worker and WebAssembly module must be on
// disk query-free, byte-for-byte (the base-path rewrite is text-only and
// must never touch a .wasm), or every compute task on the site 404s.
func TestBuildExportsComputeAssets(t *testing.T) {
	worker := []byte("self.onmessage=(e)=>postMessage(e.data)")
	wasm := []byte("\x00asm\x01\x00\x00\x00/__gofastr/")
	compute.RegisterWorker("export-echo", worker)
	compute.RegisterWASM("export-echo", wasm)

	a := coreapp.NewApp("SSGCompute")
	a.Register("/", &homeScreen{}, nil)
	out := t.TempDir()
	if _, err := (&Builder{Host: uihost.New(a), OutDir: out, BasePath: "/docs"}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(out, "__gofastr", "compute", "export-echo.js"))
	if err != nil {
		t.Fatalf("worker not exported: %v", err)
	}
	if !bytes.Equal(got, worker) {
		t.Errorf("exported worker = %q, want %q", got, worker)
	}
	got, err = os.ReadFile(filepath.Join(out, "__gofastr", "compute", "export-echo.wasm"))
	if err != nil {
		t.Fatalf("wasm not exported: %v", err)
	}
	if !bytes.Equal(got, wasm) {
		t.Errorf("exported wasm = %q, want the registered bytes %q untouched by the base-path rewrite", got, wasm)
	}
}
