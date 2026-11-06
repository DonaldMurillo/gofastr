package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"os"
	"testing"
)

func TestOrderPricesComeFromProducts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots the storefront")
	}
	password := os.Getenv("ADMIN_SEED_PASSWORD")
	if password == "" {
		password = t.Name()
		t.Setenv("ADMIN_SEED_PASSWORD", password)
	}
	base := bootSeededStorefront(t)
	client := e2eAuthedClient(t, base)
	code, body := e2eDo(t, client, "GET", base+"/api/products", "")
	var products struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &products); err != nil || code != http.StatusOK || len(products.Data) < 2 {
		t.Fatalf("products: code=%d, err=%v, body=%s", code, err, body)
	}
	readRow := func(method, path, payload string, wantCode int) map[string]any {
		t.Helper()
		code, body := e2eDo(t, client, method, base+path, payload)
		if code == http.StatusNoContent && code == wantCode {
			return nil
		}
		var one struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &one); err != nil || code != wantCode || one.Data == nil {
			t.Fatalf("%s %s: code=%d, err=%v, body=%s", method, path, code, err, body)
		}
		return one.Data
	}
	money := func(value any) *big.Rat {
		t.Helper()
		amount, ok := new(big.Rat).SetString(fmt.Sprint(value))
		if !ok {
			t.Fatalf("invalid money: %v", value)
		}
		return amount
	}
	orderID := relRedCreateOrder(t, base, client, "Catalog-priced", "0")
	check := func(item map[string]any, product map[string]any, quantity int64) {
		t.Helper()
		wantUnit := money(product["price"])
		wantTotal := new(big.Rat).Mul(wantUnit, new(big.Rat).SetInt64(quantity))
		for _, row := range []map[string]any{item, readRow("GET", "/api/order_items/"+fmt.Sprint(item["id"]), "", http.StatusOK)} {
			if money(row["unitPrice"]).FloatString(4) != wantUnit.FloatString(4) || money(row["totalPrice"]).FloatString(4) != wantTotal.FloatString(4) {
				t.Errorf("line trusts client prices: unit=%v total=%v; want %s and %s", row["unitPrice"], row["totalPrice"], wantUnit.FloatString(4), wantTotal.FloatString(4))
			}
		}
		order := readRow("GET", "/api/orders/"+orderID, "", http.StatusOK)
		if money(order["total"]).FloatString(4) != wantTotal.FloatString(4) {
			t.Errorf("order total=%v, want %s", order["total"], wantTotal.FloatString(4))
		}
	}
	first, second := products.Data[0], products.Data[1]
	item := readRow("POST", "/api/order_items", fmt.Sprintf(`{"orderId":%q,"productId":%q,"productName":%q,"quantity":3,"unitPrice":"0.01","totalPrice":"0.01"}`, orderID, first["id"], first["name"]), http.StatusCreated)
	check(item, first, 3)
	path := "/api/order_items/" + fmt.Sprint(item["id"])
	item = readRow("PATCH", path, `{"quantity":4,"unitPrice":"0.01","totalPrice":"0.01"}`, http.StatusOK)
	check(item, first, 4)
	item = readRow("PATCH", path, fmt.Sprintf(`{"productId":%q}`, second["id"]), http.StatusOK)
	check(item, second, 4)
	item = readRow("PATCH", path, `{"quantity":2}`, http.StatusOK)
	check(item, second, 2)
	readRow("DELETE", path, "", http.StatusNoContent)
	item = readRow("POST", "/api/order_items", fmt.Sprintf(`{"orderId":%q,"productId":%q,"productName":%q,"quantity":1}`, orderID, first["id"], first["name"]), http.StatusCreated)
	check(item, first, 1)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	admin := *e2eClient
	admin.Jar = jar
	if code, body := e2eDo(t, &admin, "POST", base+"/auth/login", fmt.Sprintf(`{"email":"admin@shop.example","password":%q}`, password)); code != http.StatusOK {
		t.Fatalf("admin login: %d %s", code, body)
	}
	if code, body := e2eDo(t, &admin, "PATCH", base+"/api/products/"+fmt.Sprint(first["id"]), `{"price":"12345678901.2345"}`); code != http.StatusOK {
		t.Fatalf("catalog price update: %d %s", code, body)
	}
	itemCount := func() int {
		t.Helper()
		order := readRow("GET", "/api/orders/"+orderID+"?include=items", "", http.StatusOK)
		items, ok := order["items"].([]any)
		if !ok {
			t.Fatalf("order items missing from response: %v", order)
		}
		return len(items)
	}
	beforeCount := itemCount()
	if beforeCount != 1 {
		t.Fatalf("rollback fixture has %d items, want 1", beforeCount)
	}
	for _, method := range []string{"POST", "PATCH"} {
		path := "/api/order_items"
		payload := fmt.Sprintf(`{"orderId":%q,"productId":%q,"productName":%q,"quantity":99}`, orderID, first["id"], first["name"])
		if method == "PATCH" {
			path += "/" + fmt.Sprint(item["id"])
			payload = `{"quantity":99}`
		}
		code, response := e2eDo(t, client, method, base+path, payload)
		if code != http.StatusBadRequest {
			t.Errorf("%s derived line price: got %d %s, want 400", method, code, response)
		}
		if afterCount := itemCount(); afterCount != beforeCount {
			t.Errorf("%s rejected line price changed item count: got %d, want %d", method, afterCount, beforeCount)
		}
	}
	check(item, first, 1)
}
