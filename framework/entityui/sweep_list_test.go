package entityui

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A page size is capped by the entity's Pagination.MaxListLimit.
func TestSweepListPageSizeCappedByMaxListLimit(t *testing.T) {
	cfg := ordersConfig()
	cfg.Pagination = &entity.PaginationConfig{MaxListLimit: 1}
	x := newTestUI(t, map[string]entity.EntityConfig{"orders": cfg}, map[string][]map[string]any{"orders": ordersRows()})
	html := listHTML(t, x.ui.List("orders").PageSize(5), x.ctx("/orders", ""))
	if strings.Contains(html, "alpha") == strings.Contains(html, "zeta") {
		t.Fatalf("want exactly one of two rows under MaxListLimit 1:\n%s", html)
	}
}

// The per-request list key registry drains once its requests are collected.
func TestSweepListKeyRegistryDrains(t *testing.T) {
	x := newTestUI(t, map[string]entity.EntityConfig{"orders": ordersConfig()}, map[string][]map[string]any{"orders": ordersRows()})
	render := func() { _ = listHTML(t, x.ui.List("orders"), x.ctx("/orders", "")) }
	for range 20 {
		render()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		runtime.GC()
		listKeyRegMu.Lock()
		n := len(listKeyReg)
		listKeyRegMu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d request entries still held after GC", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A rows-per-page menu with fewer than two sizes under MaxListLimit is not drawn.
func TestSweepListOneSizeOffersNoMenu(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Pagination = &entity.PaginationConfig{MaxListLimit: 30}
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"] = nil
	for i := 1; i <= 40; i++ {
		rows["invoices"] = append(rows["invoices"], map[string]any{
			"id": fmt.Sprintf("inv-%02d", i), "number": fmt.Sprintf("INV-%02d", i), "status": "draft", "customer_id": "cus-1",
		})
	}
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	h := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", "", "u1"))
	if strings.Contains(h, "Rows per page") || !strings.Contains(h, "1–25 of 40") {
		t.Fatalf("one size on offer drew a menu, or lost the range:\n%s", h)
	}
}
