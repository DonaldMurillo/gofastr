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

// TestClosedOrderRefusesChanges: once an order ships, is cancelled or
// is refunded, its items, tax and shipping_cost are settled. Adding,
// editing, deleting or moving a line, or editing a charge, is refused
// and changes nothing; a non-money edit and an open order still work.
func TestClosedOrderRefusesChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots the storefront")
	}
	s := newShop(t)
	headphones := s.product("Wireless Headphones")
	withItem := func() (orderID, itemPath string) {
		orderID = s.order("0", "0")
		item := s.must(http.StatusCreated, "POST", "/api/order_items", itemBody(orderID, headphones, "1"))
		return orderID, "/api/order_items/" + fmt.Sprint(item["id"])
	}
	move := func(orderID string, keys ...string) {
		for _, key := range keys {
			s.must(http.StatusOK, "POST", "/api/orders/"+orderID+"/transitions/"+key, "")
		}
	}
	refused := func(what, method, path, body string) {
		t.Helper()
		if code, _, raw := s.do(method, path, body); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400; body=%.300s", what, code, raw)
		}
	}
	snapshot := func(orderID string) string {
		t.Helper()
		o := s.must(http.StatusOK, "GET", "/api/orders/"+orderID+"?include=items", "")
		items, _ := o["items"].([]any)
		return fmt.Sprintf("items=%d subtotal=%v tax=%v shipping=%v total=%v", len(items), o["subtotal"], o["tax"], o["shippingCost"], o["total"])
	}

	refundedID, refundedItem := withItem()
	move(refundedID, "confirm", "ship", "deliver", "refund")
	openID, openItem := withItem()
	before, openBefore := snapshot(refundedID), snapshot(openID)

	refused("add a line to a refunded order", "POST", "/api/order_items", itemBody(refundedID, headphones, "5"))
	refused("edit a refunded line", "PATCH", refundedItem, `{"quantity":5}`)
	refused("move a refunded line out", "PATCH", refundedItem, fmt.Sprintf(`{"orderId":%q}`, openID))
	refused("move a line into a refunded order", "PATCH", openItem, fmt.Sprintf(`{"orderId":%q}`, refundedID))
	refused("delete a refunded line", "DELETE", refundedItem, "")
	refused("edit a refunded order's tax", "PATCH", "/api/orders/"+refundedID, `{"tax":"100"}`)
	refused("edit a refunded order's shipping", "PATCH", "/api/orders/"+refundedID, `{"shippingCost":"9"}`)
	s.must(http.StatusOK, "PATCH", "/api/orders/"+refundedID, `{"notes":"refund sent"}`)
	if after := snapshot(refundedID); after != before {
		t.Errorf("refunded order changed:\n before %s\n after  %s", before, after)
	}
	if after := snapshot(openID); after != openBefore {
		t.Errorf("open order changed by refused moves:\n before %s\n after  %s", openBefore, after)
	}

	cancelledID := s.order("0", "0")
	move(cancelledID, "cancel")
	refused("add a line to a cancelled order", "POST", "/api/order_items", itemBody(cancelledID, headphones, "1"))
	shippedID, shippedItem := withItem()
	move(shippedID, "confirm", "ship")
	refused("edit a shipped line", "PATCH", shippedItem, `{"quantity":2}`)
	refused("edit a shipped order's tax", "PATCH", "/api/orders/"+shippedID, `{"tax":"1"}`)

	// An order that has not shipped still takes changes.
	s.must(http.StatusOK, "PATCH", openItem, `{"quantity":2}`)
	s.must(http.StatusOK, "PATCH", "/api/orders/"+openID, `{"tax":"1.50"}`)
	move(openID, "confirm")
	s.must(http.StatusCreated, "POST", "/api/order_items", itemBody(openID, headphones, "1"))
}

// TestOrderCreateNeedsNoTotals: subtotal and total are derived, so a
// create need not send them, and a value sent is not stored.
func TestOrderCreateNeedsNoTotals(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots the storefront")
	}
	s := newShop(t)
	bare := s.must(http.StatusCreated, "POST", "/api/orders", `{"customerName":"Bare","customerEmail":"bare@shop.test"}`)
	spoofed := s.must(http.StatusCreated, "POST", "/api/orders", `{"customerName":"Spoof","customerEmail":"spoof@shop.test","subtotal":"99999","total":"99999"}`)
	for name, row := range map[string]map[string]any{"bare": bare, "spoofed": spoofed} {
		stored := s.must(http.StatusOK, "GET", "/api/orders/"+fmt.Sprint(row["id"]), "")
		if s.money(stored["subtotal"]).Sign() != 0 || s.money(stored["total"]).Sign() != 0 {
			t.Errorf("%s empty order stores subtotal %v total %v, want 0 and 0", name, stored["subtotal"], stored["total"])
		}
	}
	s.readOnlyInOpenAPI("orders", "subtotal", "total")
}

// readOnlyInOpenAPI fails unless each field of the entity's OpenAPI
// schema is readOnly: a property a request does not send (OpenAPI 3:
// required then applies to responses only).
func (s *shop) readOnlyInOpenAPI(entity string, fields ...string) {
	s.t.Helper()
	code, _, raw := s.do("GET", "/openapi.json", "")
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					ReadOnly bool `json:"readOnly"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(raw), &spec); err != nil || code != http.StatusOK {
		s.t.Fatalf("GET /openapi.json = %d (%v)", code, err)
	}
	for _, field := range fields {
		if !spec.Components.Schemas[entity].Properties[field].ReadOnly {
			s.t.Errorf("OpenAPI %s.%s is not readOnly: clients are told to send it", entity, field)
		}
	}
}

// TestLineNameComesFromProduct: a line's product_name is the catalog
// name of the product it names, like its price: a create need not send
// it, a sent name is not stored, and a product switch renames the line.
func TestLineNameComesFromProduct(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots the storefront")
	}
	s := newShop(t)
	headphones, hub := s.product("Wireless Headphones"), s.product("USB-C Hub")
	orderID := s.order("0", "0")
	named := func(what string, row map[string]any, want map[string]any) {
		t.Helper()
		stored := s.must(http.StatusOK, "GET", "/api/order_items/"+fmt.Sprint(row["id"]), "")
		for label, r := range map[string]map[string]any{"response": row, "stored": stored} {
			if r["productName"] != want["name"] {
				t.Errorf("%s: %s productName %v, want %v", what, label, r["productName"], want["name"])
			}
		}
	}
	spoofed := s.must(http.StatusCreated, "POST", "/api/order_items", fmt.Sprintf(
		`{"orderId":%q,"productId":%q,"productName":"FREE GIFT CARD","quantity":1}`, orderID, headphones["id"]))
	named("spoofed create", spoofed, headphones)
	bare := s.must(http.StatusCreated, "POST", "/api/order_items", fmt.Sprintf(
		`{"orderId":%q,"productId":%q,"quantity":1}`, orderID, headphones["id"]))
	named("nameless create", bare, headphones)
	path := "/api/order_items/" + fmt.Sprint(spoofed["id"])
	named("product switch", s.must(http.StatusOK, "PATCH", path, fmt.Sprintf(`{"productId":%q}`, hub["id"])), hub)
	code, row, raw := s.do("PATCH", path, `{"productName":"Renamed"}`)
	switch code {
	case http.StatusOK:
		named("rename", row, hub)
	case http.StatusBadRequest:
		named("rename", s.must(http.StatusOK, "GET", path, ""), hub)
	default:
		t.Fatalf("rename = %d, want 200 (ignored) or 400; body=%.300s", code, raw)
	}
	s.readOnlyInOpenAPI("order_items", "productName")
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
