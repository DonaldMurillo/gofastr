//go:build red

package main

// RED TEST — open finding, 2026-09-06/07 adversarial round 5 (fringe wave; tier T2).
//
// Property: an owner-scoped row's belongs_to relation is a write-side
// trust boundary — order_items.orderId targeting ANOTHER customer's order
// must be refused exactly as reads of that order are.
//
// Surfaces: examples/ecommerce/gofastr.yml order_items (owner_field
// user_id, order_id required relation to orders; the :250-252 posture
// comment scopes reads: "can't be enumerated anonymously or across
// customers via the sibling entity" — nothing covers writes).
// framework/crud/crud_ops.go::doCreate (:25-131) persists body order_id
// verbatim (field loop :81-91); InjectOwner (:27) stamps only the row's
// own user_id column, and the migrate FK is existence-only, so a REAL
// foreign order satisfies it. doUpdate (:133-243) reuses the same path,
// so PATCH retargets a row just as freely. MCP tools re-dispatch through
// the same router, so REST == MCP here.
//
// Finding (code-verified; expects 201/200 live): signed-in customer alice
// attaches an order_items row to bob's order by POSTing his orderId (201),
// and can retarget her own row at his order via PATCH (200) — the read
// side of the relation is scoped, the write side trusts the body.
//
// Fix direction: on create/update of an entity whose belongs_to relation
// targets an owner-scoped entity, resolve the related row and require the
// caller to own it (the framework already resolves relations for reads;
// reuse that resolver on the write path). Pinned sibling read-half guard:
// include=order must not surface a foreign order. Harness:
// seed_guard_test.go (bootSeededStorefront / e2eAuthedClient — the
// per-user variant below), e2e_test.go (e2eDo / e2eFreeAddr /
// e2eWaitReady); sibling red test: orders_total_red_test.go (field
// spellings).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// relRedClient registers + logs in a FRESH customer (unique email) and
// returns a client whose cookie jar carries the session. e2eAuthedClient
// is single-account (fixed email); cross-owner scenarios need two.
func relRedClient(t *testing.T, base, email string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("setup broken: cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	creds := fmt.Sprintf(`{"email":%q,"password":"relred-pw-12345"}`, email)
	if code, body := e2eDo(t, client, "POST", base+"/auth/register", creds); code >= 400 {
		t.Fatalf("setup broken: POST /auth/register (%s) = %d; body=%.300s", email, code, body)
	}
	if code, body := e2eDo(t, client, "POST", base+"/auth/login", creds); code >= 400 {
		t.Fatalf("setup broken: POST /auth/login (%s) = %d; body=%.300s", email, code, body)
	}
	return client
}

// relRedCreateOrder creates one order for the caller and returns its id.
func relRedCreateOrder(t *testing.T, base string, client *http.Client, name, total string) string {
	t.Helper()
	body := fmt.Sprintf(`{"customerName":%q,"customerEmail":"relred@shop.test","subtotal":%q,"tax":"0","shippingCost":"0","total":%q}`, name, total, total)
	code, resp := e2eDo(t, client, "POST", base+"/api/orders", body)
	if code != http.StatusCreated {
		t.Fatalf("setup broken: POST /api/orders = %d; body=%.300s", code, resp)
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

// TestOrderItemRelRedOwnTargetOnly: alice must only attach order_items to
// her OWN orders — create and PATCH both. Guards: her own-order create
// succeeds and round-trips (a fix that refuses everything cannot pass),
// and the read half (include=order) never surfaces bob's order.
func TestOrderItemRelRedOwnTargetOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	base := bootSeededStorefront(t)

	bob := relRedClient(t, base, "relred-bob@shop.test")
	alice := relRedClient(t, base, "relred-alice@shop.test")

	// A real seeded product to hang line items on.
	pcode, pbody := e2eDo(t, alice, "GET", base+"/api/products", "")
	if pcode != http.StatusOK {
		t.Fatalf("setup broken: GET /api/products = %d; body=%.300s", pcode, pbody)
	}
	var list struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(pbody), &list); err != nil || len(list.Data) == 0 {
		t.Fatalf("setup broken: products decode (%v) or empty: %.300s", err, pbody)
	}
	productID, _ := list.Data[0]["id"].(string)
	productName, _ := list.Data[0]["name"].(string)
	if productID == "" || productName == "" {
		t.Fatalf("setup broken: first product lacks id/name: %v", list.Data[0])
	}

	// bob's order is the cross-owner target; its customerName is a marker
	// the read-leg assertions can look for in any response body.
	bobOrder := relRedCreateOrder(t, base, bob, "RelRedBobMarker", "9.00")

	// ── Guard: alice's own order + item create succeeds and round-trips. ──
	aliceOrder := relRedCreateOrder(t, base, alice, "RelRedAliceMarker", "2.00")
	ownBody := fmt.Sprintf(`{"orderId":%q,"productId":%q,"productName":%q,"quantity":1,"unitPrice":"2.00","totalPrice":"2.00"}`,
		aliceOrder, productID, productName)
	ownCode, ownResp := e2eDo(t, alice, "POST", base+"/api/order_items", ownBody)
	if ownCode != http.StatusCreated {
		t.Fatalf("setup broken: own-order POST /api/order_items = %d; body=%.300s (if a fix refuses this too, it is refusing everything, not the cross-owner write)", ownCode, ownResp)
	}
	var ownOne struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(ownResp), &ownOne); err != nil || ownOne.Data == nil {
		t.Fatalf("setup broken: own item create decode (%v): %.300s", err, ownResp)
	}
	ownItem, _ := ownOne.Data["id"].(string)
	if ownItem == "" {
		t.Fatalf("setup broken: created item carries no id: %.300s", ownResp)
	}
	gcode, gbody := e2eDo(t, alice, "GET", base+"/api/order_items/"+ownItem, "")
	if gcode != http.StatusOK || !strings.Contains(gbody, aliceOrder) {
		t.Fatalf("setup broken: own item round-trip = %d, hasOrder=%v; body=%.300s", gcode, strings.Contains(gbody, aliceOrder), gbody)
	}

	// ── Leg A (create): alice posts a line item targeting bob's order. ──
	attackBody := fmt.Sprintf(`{"orderId":%q,"productId":%q,"productName":%q,"quantity":1,"unitPrice":"1.00","totalPrice":"1.00"}`,
		bobOrder, productID, productName)
	aCode, aResp := e2eDo(t, alice, "POST", base+"/api/order_items", attackBody)
	forgedItem := ""
	if aCode < 400 {
		var forged struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(aResp), &forged); err == nil && forged.Data != nil {
			forgedItem, _ = forged.Data["id"].(string)
		}
		t.Errorf("SECURITY: [orderitem-crossowner-relwrite] cross-owner CREATE accepted: alice POSTed /api/order_items with orderId=<bob's order %s> and got %d (want >=400) — doCreate (crud_ops.go:25-131) persists body order_id verbatim and InjectOwner stamps only user_id, so any signed-in customer attaches rows to a foreign order; body=%.200s", bobOrder, aCode, aResp)
	}
	// Re-GET: alice's own order_items list must not reference bob's order.
	lCode, lBody := e2eDo(t, alice, "GET", base+"/api/order_items", "")
	if lCode != http.StatusOK {
		t.Fatalf("setup broken: GET /api/order_items = %d; body=%.300s", lCode, lBody)
	}
	var items struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(lBody), &items); err != nil {
		t.Fatalf("setup broken: order_items decode: %v; body=%.300s", err, lBody)
	}
	for _, row := range items.Data {
		if fmt.Sprintf("%v", row["orderId"]) == bobOrder {
			t.Errorf("SECURITY: [orderitem-crossowner-relwrite] forged row persisted: alice's order_items list contains a row with orderId=<bob's order %s> — the cross-owner relation write landed (row id=%v)", bobOrder, row["id"])
		}
	}

	// ── Read-leg guard: the read half stays scoped. Only meaningful when
	// today's bug produced a forged row: even though the row's order_id
	// points at bob's order, the include must resolve it to null, and
	// bob's order data (RelRedBobMarker) must not leak. The row's own
	// orderId column naming bob's order is the WRITE bug — leg A. ──
	if forgedItem != "" {
		icode, ibody := e2eDo(t, alice, "GET", base+"/api/order_items/"+forgedItem+"?include=order", "")
		if icode != http.StatusOK {
			t.Fatalf("setup broken: GET forged item include=order = %d; body=%.300s", icode, ibody)
		}
		var inc struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(ibody), &inc); err != nil || inc.Data == nil {
			t.Fatalf("setup broken: include decode (%v): %.300s", err, ibody)
		}
		if inc.Data["order"] != nil {
			t.Errorf("SECURITY: [orderitem-crossowner-relwrite] cross-owner READ via relation: GET order_items/%s?include=order resolved the foreign order object — the read half of the relation must stay scoped (order must be null); body=%.300s", forgedItem, ibody)
		}
		if strings.Contains(ibody, "RelRedBobMarker") {
			t.Errorf("SECURITY: [orderitem-crossowner-relwrite] cross-owner READ via relation: GET order_items/%s?include=order leaked bob's order data (customerName marker present) — the read half must stay scoped; body=%.300s", forgedItem, ibody)
		}
	}

	// ── Leg B (update): alice retargets her OWN item at bob's order. ──
	patchBody := fmt.Sprintf(`{"orderId":%q,"productId":%q,"productName":%q,"quantity":1,"unitPrice":"2.00","totalPrice":"2.00"}`,
		bobOrder, productID, productName)
	pCode, pResp := e2eDo(t, alice, "PATCH", base+"/api/order_items/"+ownItem, patchBody)
	if pCode < 400 {
		after := ""
		if acode, abody := e2eDo(t, alice, "GET", base+"/api/order_items/"+ownItem, ""); acode == http.StatusOK {
			after = fmt.Sprintf(" (row now has orderId=%v)", jsonKey(abody, "orderId"))
		}
		t.Errorf("SECURITY: [orderitem-crossowner-relwrite] cross-owner UPDATE accepted: alice PATCHed her own item to orderId=<bob's order %s> and got %d (want >=400) — doUpdate reuses the same verbatim-relation path, so ownership of the ROW is checked but ownership of the TARGET never is%s; body=%.200s", bobOrder, pCode, after, pResp)
	}
}

// jsonKey pulls one value out of a small {"data":{…}} JSON blob for a
// diagnostic message. Best-effort: returns "" on any miss.
func jsonKey(body, key string) string {
	var m struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return ""
	}
	if v, ok := m.Data[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}
