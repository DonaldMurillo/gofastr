package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
)

// shop drives one booted storefront as one signed-in customer. The
// order hooks in entities/order_totals_hooks.go are what these tests
// pin, so every call goes through the REST surface a client uses.
type shop struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newShop(t *testing.T) *shop {
	t.Helper()
	base := bootSeededStorefront(t)
	return &shop{t: t, base: base, client: e2eAuthedClient(t, base)}
}

// do sends a JSON request (the transition routes need the JSON content
// type even with no body) and decodes the "data" envelope with
// UseNumber, so a large money value reads back digit for digit.
func (s *shop) do(method, path, body string) (int, map[string]any, string) {
	s.t.Helper()
	req, err := http.NewRequest(method, s.base+path, strings.NewReader(body))
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env struct {
		Data map[string]any `json:"data"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&env) //gofastr:allow(discardeddecode) error bodies carry no data envelope; callers check the status first
	return resp.StatusCode, env.Data, string(raw)
}

// must runs a request that has to answer want and returns its row.
func (s *shop) must(want int, method, path, body string) map[string]any {
	s.t.Helper()
	code, row, raw := s.do(method, path, body)
	if code != want {
		s.t.Fatalf("%s %s %s = %d, want %d; body=%.400s", method, path, body, code, want, raw)
	}
	return row
}

// product returns the first catalog row whose name is name.
func (s *shop) product(name string) map[string]any {
	s.t.Helper()
	code, _, raw := s.do("GET", "/api/products?limit=100", "")
	var list struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &list); err != nil || code != http.StatusOK {
		s.t.Fatalf("GET /api/products = %d (%v): %.300s", code, err, raw)
	}
	for _, p := range list.Data {
		if p["name"] == name {
			return p
		}
	}
	s.t.Fatalf("no product named %q in the catalog", name)
	return nil
}

// order creates an order with the given tax and shipping and returns
// its id.
func (s *shop) order(tax, shipping string) string {
	s.t.Helper()
	row := s.must(http.StatusCreated, "POST", "/api/orders", fmt.Sprintf(
		`{"customerName":"Integrity","customerEmail":"integrity@shop.test","subtotal":"0","total":"0","tax":%q,"shippingCost":%q}`, tax, shipping))
	return fmt.Sprint(row["id"])
}

// itemBody is an order_items create body.
func itemBody(orderID string, product map[string]any, quantity string) string {
	return fmt.Sprintf(`{"orderId":%q,"productId":%q,"productName":%q,"quantity":%s}`,
		orderID, product["id"], product["name"], quantity)
}

// money reads a decimal off the wire exactly.
func (s *shop) money(v any) *big.Rat {
	s.t.Helper()
	r, ok := new(big.Rat).SetString(fmt.Sprint(v))
	if !ok {
		s.t.Fatalf("money value %v does not parse", v)
	}
	return r
}

// TestOrderTotalIsExactOrRefused: an order total is subtotal + tax +
// shipping_cost to the cent, or the write is refused and rolled back.
// SQLite stores a decimal as a float, exact to 15 significant digits;
// a SUM past that used to store a drifted total (39990000000000008 for
// 39990000000000005.01) that the API then served.
func TestOrderTotalIsExactOrRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots the storefront")
	}
	s := newShop(t)
	headphones := s.product("Wireless Headphones") // 79.99

	exact := func(orderID string) {
		t.Helper()
		order := s.must(http.StatusOK, "GET", "/api/orders/"+orderID, "")
		want := new(big.Rat).Add(s.money(order["subtotal"]), s.money(order["tax"]))
		want.Add(want, s.money(order["shippingCost"]))
		if got := s.money(order["total"]); got.Cmp(want) != 0 {
			t.Errorf("order total %v, want subtotal %v + tax %v + shipping %v = %s",
				order["total"], order["subtotal"], order["tax"], order["shippingCost"], want.FloatString(4))
		}
	}

	// An ordinary order sums exactly.
	small := s.order("0.01", "5.00")
	s.must(http.StatusCreated, "POST", "/api/order_items", itemBody(small, headphones, "3"))
	exact(small)

	// A line whose own price is exact (7999 x 10^13) but whose order
	// total is not storable: exact, or refused with nothing written.
	huge := s.order("0.01", "5.00")
	code, _, raw := s.do("POST", "/api/order_items", itemBody(huge, headphones, "1000000000000000"))
	switch code {
	case http.StatusCreated:
		exact(huge)
	case http.StatusBadRequest:
		order := s.must(http.StatusOK, "GET", "/api/orders/"+huge+"?include=items", "")
		if items, _ := order["items"].([]any); len(items) != 0 || s.money(order["subtotal"]).Sign() != 0 {
			t.Errorf("refused line left a trace: items=%v subtotal=%v", order["items"], order["subtotal"])
		}
	default:
		t.Fatalf("huge line = %d, want 201 with an exact total or 400; body=%.400s", code, raw)
	}

	// The order side: a tax edit that pushes the total past the
	// storable digits is exact or refused too.
	wide := s.order("0", "0")
	s.must(http.StatusCreated, "POST", "/api/order_items", itemBody(wide, headphones, "10000000000000")) // 799900000000000
	code, _, raw = s.do("PATCH", "/api/orders/"+wide, `{"tax":"0.01"}`)
	switch code {
	case http.StatusOK:
		exact(wide)
	case http.StatusBadRequest:
		order := s.must(http.StatusOK, "GET", "/api/orders/"+wide, "")
		if s.money(order["tax"]).Sign() != 0 {
			t.Errorf("refused tax edit stored tax %v", order["tax"])
		}
		exact(wide)
	default:
		t.Fatalf("tax edit = %d, want 200 with an exact total or 400; body=%.400s", code, raw)
	}
}

// TestOnlyActiveProductsCanBeOrdered: a line item names a product the
// catalog sells. Webcam HD is seeded as a draft; a draft, an archived
// or a deleted product is refused on create and on a product switch,
// and the order keeps no trace of the refused line.
func TestOnlyActiveProductsCanBeOrdered(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots the storefront")
	}
	s := newShop(t)
	headphones, draft := s.product("Wireless Headphones"), s.product("Webcam HD")
	if draft["status"] != "draft" {
		t.Fatalf("fixture drift: Webcam HD status %v, want draft", draft["status"])
	}
	orderID := s.order("0", "0")
	items := func() int {
		t.Helper()
		order := s.must(http.StatusOK, "GET", "/api/orders/"+orderID+"?include=items", "")
		list, _ := order["items"].([]any)
		return len(list)
	}

	if code, _, raw := s.do("POST", "/api/order_items", itemBody(orderID, draft, "1")); code != http.StatusBadRequest {
		t.Errorf("draft product line = %d, want 400; body=%.300s", code, raw)
	}
	if n := items(); n != 0 {
		t.Errorf("refused draft line left %d items", n)
	}

	item := s.must(http.StatusCreated, "POST", "/api/order_items", itemBody(orderID, headphones, "2"))
	path := "/api/order_items/" + fmt.Sprint(item["id"])
	if code, _, raw := s.do("PATCH", path, fmt.Sprintf(`{"productId":%q}`, draft["id"])); code != http.StatusBadRequest {
		t.Errorf("switch to draft product = %d, want 400; body=%.300s", code, raw)
	}
	if row := s.must(http.StatusOK, "GET", path, ""); row["productId"] != headphones["id"] {
		t.Errorf("refused switch stored product %v, want %v", row["productId"], headphones["id"])
	}
	s.must(http.StatusOK, "PATCH", path, `{"quantity":3}`) // an active line still edits

	// Archived and soft-deleted products are off sale too.
	admin := &shop{t: t, base: s.base, client: relRedAdmin(t, s.base)}
	hub, keyboard := s.product("USB-C Hub"), s.product("Mechanical Keyboard")
	admin.must(http.StatusOK, "PATCH", "/api/products/"+fmt.Sprint(hub["id"]), `{"status":"archived"}`)
	admin.must(http.StatusNoContent, "DELETE", "/api/products/"+fmt.Sprint(keyboard["id"]), "")
	for name, p := range map[string]map[string]any{"archived": hub, "deleted": keyboard} {
		if code, _, raw := s.do("POST", "/api/order_items", itemBody(orderID, p, "1")); code < 400 || code >= 500 {
			t.Errorf("%s product line = %d, want a 4xx refusal; body=%.300s", name, code, raw)
		}
	}
	if n := items(); n != 1 {
		t.Errorf("order holds %d items after the refusals, want 1", n)
	}
}

// relRedAdmin signs in the seeded admin, whose role holds catalog:write.
func relRedAdmin(t *testing.T, base string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: e2eClient.Timeout}
	creds := fmt.Sprintf(`{"email":"admin@shop.example","password":%q}`, seedAdminPassword())
	if code, body := e2eDo(t, client, "POST", base+"/auth/login", creds); code != http.StatusOK {
		t.Fatalf("admin login = %d; body=%.300s", code, body)
	}
	return client
}

// seedAdminPassword is the password bootSeededStorefront seeds the admin
// with.
func seedAdminPassword() string {
	if pw := os.Getenv("ADMIN_SEED_PASSWORD"); pw != "" {
		return pw
	}
	return "guard-seed-admin-pw" // not-a-secret: bootSeededStorefront's throwaway default
}
