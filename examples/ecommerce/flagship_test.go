package ecommerce_test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFlagship_AllSurfacesFromBlueprint builds and runs the generated
// storefront binary and asserts that every surface the thesis promises —
// REST CRUD, OpenAPI, the MCP tool surface, and the server-rendered UI —
// is live, all from the single gofastr.yml blueprint with zero hand-written
// application code. This is the proof-of-thesis check.
func TestFlagship_AllSurfacesFromBlueprint(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + runs the generated binary; skipped under -short")
	}

	// 0) Generate the app from the blueprint with the current CLI source —
	// this exercises the full declaration → code pipeline, and means the
	// gitignored gen/ need not be committed. go run uses the in-tree source,
	// so the test never depends on a stale installed binary.
	gen := exec.Command("go", "run", "github.com/DonaldMurillo/gofastr/cmd/gofastr",
		"generate", "--from=gofastr.yml")
	gen.Stderr = os.Stderr
	if err := gen.Run(); err != nil {
		t.Fatalf("gofastr generate --from=gofastr.yml: %v", err)
	}

	// Build the generated app (proves the blueprint → buildable Go path).
	bin := filepath.Join(t.TempDir(), "shopfront")
	build := exec.Command("go", "build", "-o", bin, "./gen")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("go build ./gen: %v", err)
	}

	addr := freeAddr(t)
	// Run from a clean dir so the app does not pick up this directory's
	// gofastr.yml (which is a blueprint, not an isolation config).
	runDir := t.TempDir()
	srv := exec.Command(bin)
	srv.Dir = runDir
	srv.Env = append(os.Environ(),
		"PORT="+addr,
		"DATABASE_URL=file:"+filepath.Join(runDir, "shop.db"),
	)
	srv.Stdout, srv.Stderr = io.Discard, io.Discard
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Process.Kill(); _, _ = srv.Process.Wait() })

	base := "http://" + addr
	waitReady(t, base)

	// 1) The OpenAPI surface is wired from the blueprint. The raw spec is
	// auth-gated by secure-by-default (PublicOpenAPI is off), so an
	// unauthenticated request gets 401; a 404 would mean it was never
	// mounted. Either response proves the surface exists.
	if code := httpStatus(t, base+"/openapi.json"); code != http.StatusOK && code != http.StatusUnauthorized {
		t.Errorf("/openapi.json = %d, want 200 or 401 (mounted)", code)
	}

	// 2) REST CRUD round-trip: create a product, then read it back.
	// price is a decimal field, sent as a string per the framework contract.
	created := httpPost(t, base+"/products",
		`{"name":"Test Widget","slug":"test-widget","price":"9.99","stock":5,"status":"active"}`)
	if !strings.Contains(created, "test-widget") {
		t.Fatalf("POST /products did not echo the created product; got:\n%s", created)
	}
	if list := httpGet(t, base+"/products?limit=50"); !strings.Contains(list, "test-widget") {
		t.Errorf("GET /products missing the created product; got:\n%.400s", list)
	}

	// 3) MCP tool surface advertises the generated per-entity tools.
	tools := httpPost(t, base+"/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	for _, want := range []string{"products_list", "products_create", "categories_list", "orders_list", "reviews_list"} {
		if !strings.Contains(tools, want) {
			t.Errorf("MCP tools/list missing %q", want)
		}
	}

	// 4) Server-rendered UI home page is live.
	if home := httpGet(t, base+"/"); !strings.Contains(home, "ShopFront") {
		t.Errorf("GET / did not render the storefront; got:\n%.400s", home)
	}
}

// freeAddr returns a localhost host:port that was free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func waitReady(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("server did not become ready within 15s")
}

func httpStatus(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func httpGet(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func httpPost(t *testing.T, url, body string) string {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		t.Fatalf("POST %s = %d; body=%s", url, resp.StatusCode, string(got))
	}
	return fmt.Sprintf("%s", got)
}
