// Runtime regression guards for the generated storefront.
//
// These assertions live OUTSIDE the generator-emitted e2e_test.go (which
// `gofastr generate --force` rewrites from the blueprint) so they survive
// regeneration. They guard two regressions the blueprint validator now
// catches at generate time but that must also fail loudly at runtime:
//
//  1. Reviews declare a required product_id relation. Before the seed wired
//     it via `@products.slug=…`, every review row failed CreateOne and the
//     storefront shipped zero reviews ("seed reviews: skipping row").
//  2. product_detail's route must carry {id}, or the list's "View" link
//     (/products/<id>) matches no registered route and renders nothing.
//
// Category linkage (products -> categories via @categories.slug=…) is not
// rendered on the anonymous storefront, so it is verified through the REST
// API as a logged-in customer: each product carries a resolved category_id,
// and the distinct set covers every seeded category.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestSeedAndDetailGuards boots a freshly-seeded storefront (isolated SQLite
// DB) and asserts the seed loaded and the detail route resolves.
func TestSeedAndDetailGuards(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	base := bootSeededStorefront(t)

	// (1) Reviews seeded: a missing required product_id skips every row, so
	// the list falls back to its empty state. The seeded author must appear.
	code, body := e2eDo(t, http.DefaultClient, "GET", base+"/reviews", "")
	if code != http.StatusOK {
		t.Fatalf("reviews page = %d, want 200", code)
	}
	if strings.Contains(body, "No reviews yet") {
		t.Error("reviews page shows the empty state — seed rows were skipped (product_id @-ref unresolved?)")
	}
	if !strings.Contains(body, "Sarah M.") {
		t.Error("reviews page missing seeded author 'Sarah M.' — reviews seed did not load")
	}

	// (2) Detail route alive + View link well-formed: pull the first
	// /products/<id> View link from the catalog (the link the validator now
	// requires {id} on) and fetch it. A dead link 404s.
	_, prodBody := e2eDo(t, http.DefaultClient, "GET", base+"/products", "")
	m := regexp.MustCompile(`/products/([0-9a-fA-F-]{36})`).FindStringSubmatch(prodBody)
	if m == nil {
		t.Fatal("products catalog has no /products/<id> View link — list did not render seeded rows or link shape changed")
	}
	href := m[0]
	dcode, dbody := e2eDo(t, http.DefaultClient, "GET", base+href, "")
	if dcode != http.StatusOK {
		t.Errorf("product detail %s = %d, want 200 (dead View link?)", href, dcode)
	}
	if !strings.Contains(dbody, "Wireless Headphones") && !strings.Contains(dbody, "USB-C Hub") &&
		!strings.Contains(dbody, "Mechanical Keyboard") && !strings.Contains(dbody, "Webcam HD") {
		t.Errorf("product detail %s did not render a seeded product", href)
	}

	// (3) Every category has >=1 product. The storefront does not render the
	// category column, so read the records through the REST API as a logged-in
	// customer and check category_id resolved on every product, with the
	// distinct set spanning all three seeded categories.
	client := e2eAuthedClient(t, base)
	rcode, rbody := e2eDo(t, client, "GET", base+"/api/products", "")
	if rcode != http.StatusOK {
		t.Fatalf("GET /api/products = %d; body=%.300s", rcode, rbody)
	}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(rbody), &resp); err != nil {
		t.Fatalf("decode products JSON: %v; body=%.300s", err, rbody)
	}
	if len(resp.Data) < 4 {
		t.Errorf("expected >=4 seeded products via REST, got %d", len(resp.Data))
	}
	categories := map[string]bool{}
	for _, p := range resp.Data {
		cid := p["categoryId"]
		if cid == nil {
			t.Errorf("product id=%v has nil categoryId — @categories.slug @-ref unresolved", p["id"])
			continue
		}
		categories[fmt.Sprintf("%v", cid)] = true
	}
	if len(categories) < 3 {
		t.Errorf("seeded products span %d distinct categories, want >=3 (every category should have >=1 product)", len(categories))
	}
}

// bootSeededStorefront builds the generated binary and boots it on a free port
// with a throwaway SQLite database, returning its base URL.
func bootSeededStorefront(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "app")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}
	addr := e2eFreeAddr(t)
	srv := exec.Command(bin)
	srv.Dir = dir
	// guard.db is a fresh file every run, so the blueprint's admin seed runs
	// and the app refuses to boot without a password for it.
	seedPw := os.Getenv("ADMIN_SEED_PASSWORD")
	if seedPw == "" {
		seedPw = "guard-seed-admin-pw"
	}
	srv.Env = append(os.Environ(), "PORT="+addr, "DATABASE_URL=file:"+filepath.Join(dir, "guard.db"),
		"ADMIN_SEED_PASSWORD="+seedPw)
	srv.Stdout, srv.Stderr = nil, nil
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Process.Kill(); _, _ = srv.Process.Wait() })
	base := "http://" + addr
	e2eWaitReady(t, base)
	return base
}

// e2eAuthedClient registers a fresh customer and returns a client whose cookie
// jar carries the session, so session-gated REST reads succeed.
func e2eAuthedClient(t *testing.T, base string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	creds := `{"email":"e2e-guard@shop.test","password":"e2e-guard-pw-123"}`
	if code, body := e2eDo(t, client, "POST", base+"/auth/register", creds); code >= 400 {
		t.Fatalf("POST /auth/register = %d; body=%.300s", code, body)
	}
	if code, body := e2eDo(t, client, "POST", base+"/auth/login", creds); code >= 400 {
		t.Fatalf("POST /auth/login = %d; body=%.300s", code, body)
	}
	return client
}
