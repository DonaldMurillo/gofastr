//go:build red

package main

// CONTRACT-QUESTION: no framework doc promises server-side recomputation of
// order totals, and examples/ecommerce/gofastr.yml is developer input, not
// attacker input — this test asserts the money-integrity policy a commerce
// TEMPLATE should carry (exif-precedent shape: the maintainer either
// promotes it — recompute/refuse — or documents "totals are caller-supplied
// in this scaffold" on the orders entity and deletes this test).
//
// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
//
// Property: an order's money fields are server-derived from its line items,
// never trusted verbatim from the request body.
//
// Surfaces: examples/ecommerce/gofastr.yml orders entity (subtotal/tax/
// shipping_cost/total decimal fields, lines ~219-234, min-0/non-finite
// only) + the generated app/screen_orders_crud.go + entities/orders.go
// (registerOrders declares the fields with no hook recompute) — the scaffold
// posts the client's totals verbatim; core/schema pins only min-0 and
// finite-form, no arithmetic anywhere.
//
// Finding (verified: no money arithmetic in the generated app): a signed-in
// customer POSTs /api/orders with total "0.01" and attaches an order_items
// row of unit_price 79.99 x quantity 3 ($239.97 of goods); the order stores
// with total 0.01.
//
// Fix direction: recompute subtotal/total from the order_items rows on every
// order/items mutation (or refuse an order whose declared totals disagree
// with its items); until then a customer names their own price.
//
// Sibling harness: seed_guard_test.go (bootSeededStorefront /
// e2eAuthedClient / e2eDo), e2e_test.go (e2eFreeAddr / e2eWaitReady).

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"
)

// TestOrdersTotalRedServerDerived: a logged-in customer creates an order
// whose declared total is 0.01 while its single line item is 79.99 x 3. The
// stored order must NOT carry the caller's 0.01 — either the totals were
// recomputed from the items, or the create was refused. A consistent order
// (totals matching its items) must still succeed, so a fix that refuses
// everything cannot pass by brute force.
func TestOrdersTotalRedServerDerived(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	base := bootSeededStorefront(t)
	client := e2eAuthedClient(t, base)

	// One real product to hang both orders' line items on.
	pcode, pbody := e2eDo(t, client, "GET", base+"/api/products", "")
	if pcode != http.StatusOK {
		t.Fatalf("setup broken: GET /api/products = %d; body=%.300s", pcode, pbody)
	}
	var list struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(pbody), &list); err != nil || len(list.Data) == 0 {
		t.Fatalf("setup broken: products decode (%v) or empty: %.300s", err, pbody)
	}
	prod := list.Data[0]
	productID, _ := prod["id"].(string)
	productName, _ := prod["name"].(string)
	if productID == "" || productName == "" {
		t.Fatalf("setup broken: first product lacks id/name: %v", prod)
	}

	rat := func(s string) *big.Rat {
		r, ok := new(big.Rat).SetString(s)
		if !ok {
			t.Fatalf("setup broken: decimal %q does not parse", s)
		}
		return r
	}
	// flexRat reads a money value off the REST wire, which carries
	// decimals as JSON strings on create and as JSON numbers on read.
	flexRat := func(v any) *big.Rat {
		switch x := v.(type) {
		case string:
			return rat(x)
		case float64:
			return new(big.Rat).SetFloat64(x)
		case json.Number:
			return rat(x.String())
		}
		t.Fatalf("setup broken: money value has unexpected wire type %T (%v)", v, v)
		return nil
	}
	trunc := func(s string) string {
		if len(s) > 120 {
			s = s[:120]
		}
		return s
	}
	refusal := func(id string) (bool, string, string) {
		if rest, ok := strings.CutPrefix(id, "REFUSED:"); ok {
			if i := strings.Index(rest, ":"); i >= 0 {
				return true, rest[:i], rest[i+1:]
			}
		}
		return false, "", ""
	}
	orderTotal := func(t *testing.T, orderID string) (*big.Rat, *big.Rat) {
		t.Helper()
		gcode, gbody := e2eDo(t, client, "GET", base+"/api/orders/"+orderID+"?include=items", "")
		if gcode != http.StatusOK {
			t.Fatalf("setup broken: GET order %s = %d; body=%.300s", orderID, gcode, gbody)
		}
		var one struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(gbody), &one); err != nil || one.Data == nil {
			t.Fatalf("setup broken: order decode (%v): %.300s", err, gbody)
		}
		totalRat := flexRat(one.Data["total"])
		if totalRat == nil {
			t.Fatalf("setup broken: order carries no total: %s", gbody)
		}
		sum := new(big.Rat)
		rawItems, _ := one.Data["items"].([]any)
		for _, it := range rawItems {
			row, _ := it.(map[string]any)
			qty, _ := row["quantity"].(float64)
			if row["unitPrice"] == nil {
				t.Fatalf("setup broken: line item lacks unitPrice: %v", row)
			}
			sum.Add(sum, new(big.Rat).Mul(flexRat(row["unitPrice"]), new(big.Rat).SetInt64(int64(qty))))
		}
		return totalRat, sum
	}
	createOrder := func(t *testing.T, subtotal, total string) string {
		t.Helper()
		body := fmt.Sprintf(`{"customerName":"Red Totals","customerEmail":"red-totals@shop.test","subtotal":%q,"tax":"0","shippingCost":"0","total":%q}`, subtotal, total)
		code, resp := e2eDo(t, client, "POST", base+"/api/orders", body)
		if code != http.StatusCreated {
			return fmt.Sprintf("REFUSED:%d:%s", code, trunc(resp))
		}
		var one struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(resp), &one); err != nil || one.Data == nil {
			t.Fatalf("setup broken: order create decode (%v): %.300s", err, resp)
		}
		id, _ := one.Data["id"].(string)
		if id == "" {
			t.Fatalf("setup broken: created order carries no id: %.300s", resp)
		}
		return id
	}
	addItem := func(t *testing.T, orderID string, unitPrice string, qty int) {
		t.Helper()
		lineTotal := new(big.Rat).Mul(rat(unitPrice), new(big.Rat).SetInt64(int64(qty)))
		body := fmt.Sprintf(`{"orderId":%q,"productId":%q,"productName":%q,"quantity":%d,"unitPrice":%q,"totalPrice":%q}`,
			orderID, productID, productName, qty, unitPrice, lineTotal.FloatString(2))
		code, resp := e2eDo(t, client, "POST", base+"/api/order_items", body)
		if code != http.StatusCreated {
			t.Fatalf("setup broken: POST /api/order_items = %d; body=%.300s", code, resp)
		}
	}

	// ── Attack: totals say 0.01, the goods say 79.99 x 3 = 239.97. ──
	attackID := createOrder(t, "0.01", "0.01")
	if refused, code, why := refusal(attackID); refused {
		// The secure shape "refuse the disagreeing order" is acceptable —
		// the positive control below still has to pass.
		t.Logf("attack order refused (%s: %s) — checking the positive control still passes", code, why)
	} else {
		addItem(t, attackID, "79.99", 3)
		stored, items := orderTotal(t, attackID)
		if stored.FloatString(2) == "0.01" && items.FloatString(2) == "239.97" {
			t.Errorf("SECURITY: [ecommerce-client-totals] order %s stored total 0.01 while its line items are 79.99 x 3 = 239.97: the scaffold persists the customer-supplied subtotal/tax/shipping_cost/total verbatim (no recompute, no consistency check), so any signed-in customer names their own price for $239.97 of goods", attackID)
		}
	}

	// ── Positive control: an honest order (totals matching its items)
	// must succeed and round-trip, so a fix that refuses every create
	// cannot pass this test by brute force. ──
	unitRat := flexRat(prod["price"])
	unit := unitRat.FloatString(2)
	honest := new(big.Rat).Mul(unitRat, new(big.Rat).SetInt64(2))
	controlID := createOrder(t, honest.FloatString(2), honest.FloatString(2))
	if refused, code, why := refusal(controlID); refused {
		t.Errorf("SECURITY: [ecommerce-client-totals] the consistent control order (totals matching its items) was refused too (%s: %s) — the fix must recompute or selectively refuse, not block order creation outright", code, why)
		return
	}
	addItem(t, controlID, unit, 2)
	stored, _ := orderTotal(t, controlID)
	if stored.FloatString(2) != honest.FloatString(2) {
		t.Errorf("SECURITY: [ecommerce-client-totals] consistent control order stored total %s, want %s (2 x %s)", stored.FloatString(2), honest.FloatString(2), unit)
	}
}
