package entities

// order_totals_hooks.go derives every order's money fields server-side,
// the CONTRACT the round-5 totals probe pinned: subtotal and total are
// computed from the order_items rows on EVERY order and order_items
// mutation (lifecycle hooks, the host-app way), never trusted from the
// request body. tax and shipping_cost stay stored columns
// (schema-validated min-0 inputs); subtotal = Σ(unit_price × quantity)
// over the order's items, and total = subtotal + tax + shipping_cost,
// both in exact decimal arithmetic. The only refusal is a derived value
// the database cannot store exactly (SQLite keeps 15 significant
// digits of a decimal).
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
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

// reasonNotForSale refuses a line item naming a product the catalog does
// not sell.
var reasonNotForSale = i18n.Define("shopfront.orders.not_for_sale", "is not for sale")

// reasonOrderClosed refuses a change to the items or charges of an order
// whose goods have shipped or whose money has settled.
var reasonOrderClosed = i18n.Define("shopfront.orders.closed", "cannot change: the order is {{status}}")

// closedOrderStatuses are the states whose contents and charges are
// settled: the goods left (shipped, delivered) or the order ended
// (cancelled, refunded). An order's items, tax and shipping_cost change
// only before it ships.
var closedOrderStatuses = []string{"shipped", "delivered", "cancelled", "refunded"}

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
	app.HookRegistry("orders").RegisterHook(framework.AfterUpdate, func(ctx context.Context, data any) error {
		if err := order(ctx, data); err != nil {
			return err
		}
		row, _ := rowOf("orders", data) // order checked the payload
		return refuseClosedCharges(ctx, idFromRow(row))
	})

	item := func(ctx context.Context, data any) error {
		row, err := rowOf("order_items", data)
		if err != nil {
			return err
		}
		if err := priceOrderItem(ctx, row); err != nil {
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
		if err := priceOrderItem(ctx, row); err != nil {
			return err
		}
		// An update may move the item to a different order: recompute
		// the old parent too (the pre-image carries the old order_id).
		if pre := crud.AuditPreImageSnakeFromContext(ctx); pre != nil {
			if old, ok := idFromKey(pre, "order_id"); ok && old != "" && old != orderIDFromRow(row) {
				if err := recomputeOpenOrder(ctx, old); err != nil {
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
		return recomputeOpenOrder(ctx, orderID)
	})
}

func priceOrderItem(ctx context.Context, row map[string]any) error {
	exec, ok := framework.TxFromContext(ctx)
	if !ok {
		return fmt.Errorf("order pricing: no transaction on context")
	}
	id := idFromRow(row)
	if id == "" {
		return fmt.Errorf("order pricing: missing line item id")
	}
	var price, name string
	var quantity int64
	var onSale bool
	err := exec.QueryRowContext(ctx, `SELECT CAST(products.price AS TEXT), products.name, order_items.quantity,
			products.status = 'active' AND products.deleted_at IS NULL
		FROM order_items JOIN products ON products.id = order_items.product_id
		WHERE order_items.id = $1`, id).Scan(&price, &name, &quantity, &onSale)
	if err != nil {
		return fmt.Errorf("order pricing: load product price: %w", err)
	}
	if !onSale {
		// A draft, archived or deleted product is not for sale, whatever
		// its price says: refused on create, on a switch to it, and on
		// any edit of a line that names it.
		return crud.NewValidationError(map[string][]schema.Violation{
			"product_id": {{Reason: reasonNotForSale}},
		})
	}
	unit, ok := new(big.Rat).SetString(price)
	if !ok || unit.Sign() < 0 || quantity < 1 {
		return fmt.Errorf("order pricing: invalid stored price or quantity")
	}
	total := new(big.Rat).Mul(unit, new(big.Rat).SetInt64(quantity))
	unitPrice, totalPrice := unit.FloatString(4), total.FloatString(4)
	if err := refuseInexact(ctx, exec, map[string]string{"total_price": totalPrice}); err != nil {
		return err
	}
	// The line's name is the catalog's too, copied with the price, so a
	// receipt never shows a name the client chose or one left behind by
	// a product switch.
	if _, err := exec.ExecContext(ctx, `UPDATE order_items SET unit_price = $1, total_price = $2, product_name = $3 WHERE id = $4`, unitPrice, totalPrice, name, id); err != nil {
		return fmt.Errorf("order pricing: persist line prices: %w", err)
	}
	for key, value := range map[string]string{"unitPrice": unitPrice, "totalPrice": totalPrice, "productName": name} {
		if _, exists := row[key]; exists {
			row[key] = value
		} else {
			row[snakeTwin(key)] = value
		}
	}
	return nil
}

// recomputeFromItemRow recomputes the parent order named by an item row,
// refusing the item write when that order is closed.
func recomputeFromItemRow(ctx context.Context, row map[string]any) error {
	orderID := orderIDFromRow(row)
	if orderID == "" {
		return fmt.Errorf("order totals: order_items payload carries no order id")
	}
	return recomputeOpenOrder(ctx, orderID)
}

// recomputeOpenOrder recomputes an order whose items just changed, and
// refuses the change when the order is closed. The check runs after the
// recompute's UPDATE: on Postgres that UPDATE waits for a move racing it
// on the same row, so the status read next is the one that move left.
func recomputeOpenOrder(ctx context.Context, orderID string) error {
	if err := recomputeOrderTotals(ctx, orderID); err != nil {
		return err
	}
	exec, ok := framework.TxFromContext(ctx)
	if !ok {
		return fmt.Errorf("order totals: no transaction on context for order %s", orderID)
	}
	status, err := orderStatus(ctx, exec, orderID)
	if err != nil || !slices.Contains(closedOrderStatuses, status) {
		return err
	}
	return crud.NewValidationError(map[string][]schema.Violation{
		"order_id": {{Reason: reasonOrderClosed, Params: map[string]any{"status": status}}},
	})
}

// refuseClosedCharges refuses an order update that changed tax or
// shipping_cost on a closed order. A move changes neither (it writes the
// status and its stamp), so it is let through without the pre-image; any
// other update of a closed order with no pre-image to compare is refused.
func refuseClosedCharges(ctx context.Context, orderID string) error {
	if crud.TransitionFromContext(ctx) != "" {
		return nil
	}
	exec, ok := framework.TxFromContext(ctx)
	if !ok {
		return fmt.Errorf("order totals: no transaction on context for order %s", orderID)
	}
	status, err := orderStatus(ctx, exec, orderID)
	if err != nil || !slices.Contains(closedOrderStatuses, status) {
		return err
	}
	var tax, shipping string
	if err := exec.QueryRowContext(ctx, `SELECT COALESCE(CAST(tax AS TEXT), '0'), COALESCE(CAST(shipping_cost AS TEXT), '0')
		FROM orders WHERE id = $1`, orderID).Scan(&tax, &shipping); err != nil {
		return fmt.Errorf("order totals: load charges of order %s: %w", orderID, err)
	}
	pre := crud.AuditPreImageSnakeFromContext(ctx)
	for _, charge := range [...]struct{ column, now string }{{"tax", tax}, {"shipping_cost", shipping}} {
		if pre == nil || !sameDecimal(pre[charge.column], charge.now) {
			return crud.NewValidationError(map[string][]schema.Violation{
				charge.column: {{Reason: reasonOrderClosed, Params: map[string]any{"status": status}}},
			})
		}
	}
	return nil
}

// orderStatus reads an order's state; a missing order reads as "".
func orderStatus(ctx context.Context, exec *sql.Tx, orderID string) (string, error) {
	var status sql.NullString
	err := exec.QueryRowContext(ctx, `SELECT status FROM orders WHERE id = $1`, orderID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("order totals: load status of order %s: %w", orderID, err)
	}
	return status.String, nil
}

// sameDecimal reports whether a pre-image value (nil, a number or its
// text) is the decimal stored now.
func sameDecimal(before any, now string) bool {
	text := "0"
	if before != nil {
		text = fmt.Sprint(before)
	}
	a, okA := new(big.Rat).SetString(text)
	b, okB := new(big.Rat).SetString(now)
	return okA && okB && a.Cmp(b) == 0
}

// recomputeOrderTotals rewrites subtotal and total from the order's
// current line items, inside the caller's transaction. The sums are
// exact decimal arithmetic in Go, never SQL: SQLite evaluates SUM over
// float columns, and past 2^53 no ROUND can restore the digits it drops
// (39990000000000005.01 summed to 39990000000000008). A total the
// database cannot store exactly is refused, rolling the write back, the
// way priceOrderItem refuses a line price.
func recomputeOrderTotals(ctx context.Context, orderID string) error {
	exec, ok := framework.TxFromContext(ctx)
	if !ok {
		return fmt.Errorf("order totals: no transaction on context for order %s", orderID)
	}
	var taxText, shippingText string
	err := exec.QueryRowContext(ctx, `SELECT COALESCE(CAST(tax AS TEXT), '0'), COALESCE(CAST(shipping_cost AS TEXT), '0')
		FROM orders WHERE id = $1`, orderID).Scan(&taxText, &shippingText)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // no such order: nothing to derive
	}
	if err != nil {
		return fmt.Errorf("order totals: load order %s: %w", orderID, err)
	}
	subtotal, err := sumOrderLines(ctx, exec, orderID)
	if err != nil {
		return err
	}
	total := new(big.Rat).Set(subtotal)
	for _, text := range []string{taxText, shippingText} {
		amount, ok := new(big.Rat).SetString(text)
		if !ok {
			return fmt.Errorf("order totals: order %s stores a charge that is not a decimal", orderID)
		}
		total.Add(total, amount)
	}
	subtotalText, totalText := subtotal.FloatString(4), total.FloatString(4)
	if err := refuseInexact(ctx, exec, map[string]string{"subtotal": subtotalText, "total": totalText}); err != nil {
		return err
	}
	if _, err := exec.ExecContext(ctx, `UPDATE orders SET subtotal = $1, total = $2 WHERE id = $3`, subtotalText, totalText, orderID); err != nil {
		return fmt.Errorf("order totals: recompute order %s: %w", orderID, err)
	}
	return nil
}

// sumOrderLines is Σ(unit_price × quantity) over the order's items,
// exactly. Each stored unit price is one priceOrderItem accepted, so its
// text reads back as the decimal that was written.
func sumOrderLines(ctx context.Context, exec *sql.Tx, orderID string) (*big.Rat, error) {
	rows, err := exec.QueryContext(ctx, `SELECT CAST(unit_price AS TEXT), quantity FROM order_items WHERE order_id = $1`, orderID)
	if err != nil {
		return nil, fmt.Errorf("order totals: load items of order %s: %w", orderID, err)
	}
	defer rows.Close()
	sum := new(big.Rat)
	for rows.Next() {
		var price sql.NullString
		var quantity int64
		if err := rows.Scan(&price, &quantity); err != nil {
			return nil, fmt.Errorf("order totals: read item of order %s: %w", orderID, err)
		}
		if !price.Valid {
			continue // an unpriced line counts as zero, as SUM skipped NULL
		}
		unit, ok := new(big.Rat).SetString(price.String)
		if !ok {
			return nil, fmt.Errorf("order totals: order %s has an item whose price is not a decimal", orderID)
		}
		sum.Add(sum, unit.Mul(unit, new(big.Rat).SetInt64(quantity)))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("order totals: read items of order %s: %w", orderID, err)
	}
	return sum, nil
}

// refuseInexact answers a validation error naming the first column (in
// name order) whose derived decimal a SQLite DECIMAL(19,4) column would
// not store as computed: it keeps 15 significant digits. Postgres
// NUMERIC stores every value exactly, so nothing is refused there.
func refuseInexact(ctx context.Context, exec *sql.Tx, values map[string]string) error {
	for _, column := range slices.Sorted(maps.Keys(values)) {
		violation, refused := schema.SQLiteDecimalRefusal(schema.DecimalColumnType, values[column])
		if !refused {
			continue
		}
		var version string
		if err := exec.QueryRowContext(ctx, "SELECT version()").Scan(&version); err == nil && strings.Contains(strings.ToLower(version), "postgresql") {
			return nil
		}
		return crud.NewValidationError(map[string][]schema.Violation{column: {violation}})
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
