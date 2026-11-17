package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"testing"
)

func TestOrderTotalsUseDecimalScale(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots the storefront")
	}
	base := bootSeededStorefront(t)
	client := e2eAuthedClient(t, base)
	code, body := e2eDo(t, client, "GET", base+"/api/products?price=79.99", "")
	var products struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &products); err != nil || code != http.StatusOK || len(products.Data) == 0 {
		t.Fatalf("79.99 product: %d %s (%v)", code, body, err)
	}
	product := products.Data[0]
	orderID := relRedCreateOrder(t, base, client, "Exact decimals", "0")
	payload := fmt.Sprintf(`{"orderId":%q,"productId":%q,"productName":%q,"quantity":3}`, orderID, product["id"], product["name"])
	if code, body := e2eDo(t, client, "POST", base+"/api/order_items", payload); code != http.StatusCreated {
		t.Fatalf("line item: %d %s", code, body)
	}
	code, body = e2eDo(t, client, "GET", base+"/api/orders/"+orderID, "")
	var order struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &order); err != nil || code != http.StatusOK {
		t.Fatalf("order: %d %s (%v)", code, body, err)
	}
	want := big.NewRat(23997, 100)
	for _, field := range []string{"subtotal", "total"} {
		got, ok := new(big.Rat).SetString(fmt.Sprint(order.Data[field]))
		if !ok || got.Cmp(want) != 0 {
			t.Errorf("%s = %v, want exactly 239.97", field, order.Data[field])
		}
	}
}
