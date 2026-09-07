package entities

// order_totals_hooks.go derives every order's money fields server-side,
// the CONTRACT the round-5 totals probe pinned: subtotal and total are
// computed from the order_items rows on EVERY order and order_items
// mutation (lifecycle hooks, the host-app way), never trusted from the
// request body. Nothing is refused — the totals are derived. tax and
// shipping_cost stay stored columns (schema-validated min-0 inputs);
// subtotal = Σ(unit_price × quantity) over the order's items, and
// total = subtotal + tax + shipping_cost.
//
// The hooks run inside the CRUD transaction (framework.TxFromContext),
// so a recompute failure fails the write: money integrity is not
// best-effort.
//
// Wiring: gofastr.yml declares the same five lifecycle points in its
// `hooks:` block — the generator registers those (author stub, no-op)
// from main.go — while THIS file, on the register.go drop-in seam,
// registers the real derivations beside them. Do not delete this file
// in favor of the yml twin: the generator's stub bodies are TODOs, and
// orders_total_security_test.go (app/) is the gate that catches a
// scaffold regressed to no-op totals.

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

func init() {
	registrars = append(registrars, registrar{order: 99, fn: registerOrderTotalsHooks})
}

// registerOrderTotalsHooks wires the money-derivation hooks: every order
// write recomputes that order, every order_items write recomputes the
// parent it names (and, on a move, the parent it left).
func registerOrderTotalsHooks(app *framework.App) {
	rowOf := func(hook string, data any) (map[string]any, error) {
		row, ok := data.(map[string]any)
		if !ok || row == nil {
			return nil, fmt.Errorf("order totals: %s payload type = %T, want map[string]any (framework contract drift?)", hook, data)
		}
		return row, nil
	}
	order := func(ctx context.Context, data any) error {
		row, err := rowOf("orders", data)
		if err != nil {
			return err
		}
		id := idFromRow(row)
		if id == "" {
			return fmt.Errorf("order totals: orders payload carries no row id")
		}
		return recomputeOrderTotals(ctx, id)
	}
	app.HookRegistry("orders").RegisterHook(framework.AfterCreate, order)
	app.HookRegistry("orders").RegisterHook(framework.AfterUpdate, order)

	item := func(ctx context.Context, data any) error {
		row, err := rowOf("order_items", data)
		if err != nil {
			return err
		}
		return recomputeFromItemRow(ctx, row)
	}
	app.HookRegistry("order_items").RegisterHook(framework.AfterCreate, item)
	app.HookRegistry("order_items").RegisterHook(framework.AfterUpdate, func(ctx context.Context, data any) error {
		row, err := rowOf("order_items", data)
		if err != nil {
			return err
		}
		// An update may move the item to a different order: recompute
		// the old parent too (the pre-image carries the old order_id).
		if pre := crud.AuditPreImageSnakeFromContext(ctx); pre != nil {
			if old, ok := idFromKey(pre, "order_id"); ok && old != "" && old != orderIDFromRow(row) {
				if err := recomputeOrderTotals(ctx, old); err != nil {
					return err
				}
			}
		}
		return recomputeFromItemRow(ctx, row)
	})
	app.HookRegistry("order_items").RegisterHook(framework.AfterDelete, func(ctx context.Context, data any) error {
		// The item is already gone; the pre-image names its parent.
		pre := crud.AuditPreImageSnakeFromContext(ctx)
		if pre == nil {
			return fmt.Errorf("order totals: AfterDelete payload carries no pre-image for %v", data)
		}
		orderID, ok := idFromKey(pre, "order_id")
		if !ok || orderID == "" {
			return fmt.Errorf("order totals: deleted order_items pre-image carries no order_id")
		}
		return recomputeOrderTotals(ctx, orderID)
	})
}

// recomputeFromItemRow recomputes the parent order named by an item row.
func recomputeFromItemRow(ctx context.Context, row map[string]any) error {
	orderID := orderIDFromRow(row)
	if orderID == "" {
		return fmt.Errorf("order totals: order_items payload carries no order id")
	}
	return recomputeOrderTotals(ctx, orderID)
}

// recomputeOrderTotals rewrites subtotal and total from the order's
// current line items, inside the caller's transaction. Both dialects in
// play (sqlite, postgres) evaluate the scalar subquery the same way;
// the decimal columns keep their stored precision.
func recomputeOrderTotals(ctx context.Context, orderID string) error {
	exec, ok := framework.TxFromContext(ctx)
	if !ok {
		return fmt.Errorf("order totals: no transaction on context for order %s", orderID)
	}
	const q = `UPDATE orders SET
		subtotal = COALESCE((SELECT SUM(unit_price * quantity) FROM order_items WHERE order_id = $1), 0),
		total = COALESCE((SELECT SUM(unit_price * quantity) FROM order_items WHERE order_id = $1), 0) + COALESCE(tax, 0) + COALESCE(shipping_cost, 0)
		WHERE id = $1`
	if _, err := exec.ExecContext(ctx, q, orderID); err != nil {
		return fmt.Errorf("order totals: recompute order %s: %w", orderID, err)
	}
	return nil
}

// idFromRow reads the row's primary key, camel or snake (the payload
// follows the handler's JSON casing; a single-word key is identical in
// both).
func idFromRow(row map[string]any) string {
	if row == nil {
		return ""
	}
	id, _ := idFromKey(row, "id")
	return id
}

// orderIDFromRow reads the parent order id off an order_items row,
// camel or snake.
func orderIDFromRow(row map[string]any) string {
	if v, ok := idFromKey(row, "orderId"); ok {
		return v
	}
	v, _ := idFromKey(row, "order_id")
	return v
}

// idFromKey reads key (or its snake twin) splitting presence from type:
// a present-but-non-string value is a contract drift the caller refuses,
// not absence.
func idFromKey(row map[string]any, key string) (string, bool) {
	for _, k := range []string{key, snakeTwin(key)} {
		if v, present := row[k]; present {
			s, isString := v.(string)
			if !isString || s == "" {
				return "", false
			}
			return s, true
		}
	}
	return "", false
}

// snakeTwin renders a camelCase key's snake_case twin ("orderId" →
// "order_id"); a key with no case boundary maps to itself.
func snakeTwin(key string) string {
	out := make([]byte, 0, len(key)+4)
	for i := range len(key) {
		c := key[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				out = append(out, '_')
			}
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}
