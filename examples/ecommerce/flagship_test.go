package ecommerce_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startShopfront generates the app from gofastr.yml with the in-tree CLI
// source, builds the generated binary into a temp dir, boots it on a free
// port with an isolated SQLite database, and returns the base URL. Each
// caller gets its own server + database, so tests stay independent.
func startShopfront(t *testing.T) string {
	t.Helper()

	// Regenerate from the blueprint with the current CLI source — this
	// exercises the full declaration → code pipeline against the in-tree
	// generator. --force overwrites the committed app/ so the test always
	// builds FRESH generator output, not a stale snapshot (output_dir: app
	// in gofastr.yml scaffolds into the owned app/ subpackage — see framework/ARCHITECTURE.md).
	gen := exec.Command("go", "run", "github.com/DonaldMurillo/gofastr/cmd/gofastr",
		"generate", "--from=gofastr.yml", "--force")
	gen.Stderr = os.Stderr
	if err := gen.Run(); err != nil {
		t.Fatalf("gofastr generate --from=gofastr.yml: %v", err)
	}

	// Build the generated app (proves the blueprint → buildable Go path).
	bin := filepath.Join(t.TempDir(), "shopfront")
	build := exec.Command("go", "build", "-o", bin, "./app")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("go build ./app: %v", err)
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
	return base
}

// TestFlagship_AllSurfacesFromBlueprint builds and runs the generated
// storefront binary and asserts that every surface the thesis promises —
// REST CRUD, OpenAPI, the MCP tool surface, and the server-rendered UI —
// is live, all from the single gofastr.yml blueprint with zero hand-written
// application code. This is the proof-of-thesis check.
func TestFlagship_AllSurfacesFromBlueprint(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + runs the generated binary; skipped under -short")
	}

	base := startShopfront(t)

	// 1) The OpenAPI surface is wired from the blueprint. The raw spec is
	// auth-gated by secure-by-default (PublicOpenAPI is off), so an
	// unauthenticated request gets 401; a 404 would mean it was never
	// mounted. Either response proves the surface exists.
	if code := httpStatus(t, base+"/openapi.json"); code != http.StatusOK && code != http.StatusUnauthorized {
		t.Errorf("/openapi.json = %d, want 200 or 401 (mounted)", code)
	}

	// 2) REST CRUD round-trip: create a product, then read it back. Entity
	// JSON APIs mount under /api (the blueprint default), leaving the bare
	// /products path for the HTML screen. price is a decimal field, sent as
	// a string per the framework contract.
	created := httpPost(t, base+"/api/products",
		`{"name":"Test Widget","slug":"test-widget","price":"9.99","stock":5,"status":"active"}`)
	if !strings.Contains(created, "test-widget") {
		t.Fatalf("POST /api/products did not echo the created product; got:\n%s", created)
	}
	if list := httpGet(t, base+"/api/products?limit=50"); !strings.Contains(list, "test-widget") {
		t.Errorf("GET /api/products missing the created product; got:\n%.400s", list)
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

// TestOrdersOwnerScoped asserts the orders entity — which carries customer
// PII (name, email, phone, addresses) — is owner-scoped: anonymous REST and
// MCP access is refused outright, while a logged-in customer can create and
// read their own orders. This is the CLAUDE.md hard-rule-6 check; the
// blueprint previously shipped with auth disabled and orders wide open.
func TestOrdersOwnerScoped(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + runs the generated binary; skipped under -short")
	}

	base := startShopfront(t)

	// Anonymous REST reads and writes against orders are rejected.
	if code := httpStatus(t, base+"/api/orders"); code != http.StatusUnauthorized && code != http.StatusForbidden {
		t.Errorf("anonymous GET /orders = %d, want 401/403", code)
	}
	orderJSON := `{"customer_name":"Ada Lovelace","customer_email":"ada@example.com",` +
		`"customer_phone":"555-0100","subtotal":"79.99","total":"79.99"}`
	if code, body := request(t, http.DefaultClient, "POST", base+"/api/orders", orderJSON); code != http.StatusUnauthorized && code != http.StatusForbidden {
		t.Errorf("anonymous POST /orders = %d, want 401/403; body=%.300s", code, body)
	}

	// Anonymous order_items access is rejected too — items are the order's
	// contents (purchase history), the obvious sibling leak.
	if code := httpStatus(t, base+"/api/order_items"); code != http.StatusUnauthorized && code != http.StatusForbidden {
		t.Errorf("anonymous GET /order_items = %d, want 401/403", code)
	}

	// The authorized path: register + login via the blueprint-enabled auth
	// battery, then prove the session is live end-to-end (/auth/me resolves
	// the logged-in customer from the cookie).
	client := authedClient(t, base, "ada@shop.example", "str0ng-passphrase")
	if code, me := request(t, client, "GET", base+"/auth/me", ""); code != http.StatusOK || !strings.Contains(me, "ada@shop.example") {
		t.Errorf("authorized GET /auth/me = %d, want 200 with the user; body=%.300s", code, me)
	}

	// The full customer flow: the generator now mounts
	// auth.SessionMiddleware, so the session cookie resolves to a user and
	// owner-scoped CRUD works for the logged-in customer.
	code, body := request(t, client, "POST", base+"/api/orders", orderJSON)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("authorized POST /orders = %d, want 201; body=%.300s", code, body)
	}
	orderID := jsonField(t, body, "id")

	// The owner sees their own order in the list.
	if code, list := request(t, client, "GET", base+"/api/orders", ""); code != http.StatusOK || !strings.Contains(list, orderID) {
		t.Errorf("owner GET /orders = %d, want 200 listing order %s; body=%.300s", code, orderID, list)
	}

	// A second customer must not see the first customer's order — neither
	// in the list nor by direct id fetch.
	other := authedClient(t, base, "grace@shop.example", "0ther-passphrase")
	if code, list := request(t, other, "GET", base+"/api/orders", ""); code != http.StatusOK {
		t.Errorf("second user GET /orders = %d, want 200 (empty list)", code)
	} else if strings.Contains(list, orderID) {
		t.Errorf("second user's GET /orders leaked another customer's order:\n%.500s", list)
	}
	if code, leak := request(t, other, "GET", base+"/api/orders/"+orderID, ""); code == http.StatusOK {
		t.Errorf("second user read another customer's order by id; body=%.300s", leak)
	}

	// Anonymous MCP tool calls against orders are rejected — the per-entity
	// MCP tools re-dispatch through the same fail-closed CRUD pipeline.
	resp := httpPost(t, base+"/mcp",
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"orders_list","arguments":{}}}`)
	if !strings.Contains(resp, "401") && !strings.Contains(resp, "403") {
		t.Errorf("anonymous MCP orders_list not rejected; got:\n%.500s", resp)
	}
	if strings.Contains(resp, "customerEmail") {
		t.Errorf("anonymous MCP orders_list leaked order rows:\n%.500s", resp)
	}
}

// authedClient registers a fresh user and logs in via the JSON auth API,
// returning an http.Client whose cookie jar carries the session.
func authedClient(t *testing.T, base, email, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	creds := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	if code, body := request(t, client, "POST", base+"/auth/register", creds); code >= 400 {
		t.Fatalf("POST /auth/register = %d; body=%.300s", code, body)
	}
	if code, body := request(t, client, "POST", base+"/auth/login", creds); code >= 400 {
		t.Fatalf("POST /auth/login = %d; body=%.300s", code, body)
	}
	return client
}

// jsonField extracts a top-level string field from a JSON object body.
func jsonField(t *testing.T, body, field string) string {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		t.Fatalf("decode JSON body: %v\nbody=%.300s", err, body)
	}
	val, _ := obj[field].(string)
	if val == "" {
		t.Fatalf("JSON body has no string field %q; body=%.300s", field, body)
	}
	return val
}

// request performs an HTTP call and returns status + body without failing
// the test on 4xx/5xx — callers assert on the status themselves.
func request(t *testing.T, client *http.Client, method, url, body string) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(got)
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
