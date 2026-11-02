package entityui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// ownerMaskHook installs the common "mask unless the caller owns the row"
// AfterGet hook on invoices: it looks the record's owner up by id (a
// stand-in for the app's own query), refuses an id it does not know,
// and masks memo for anyone but the owner. calls counts its runs by the
// id it was handed.
func ownerMaskHook(t *testing.T, x *testUI, owners map[string]string) func() map[string]int {
	t.Helper()
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	calls := map[string]int{}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		mu.Lock()
		calls[p.ID]++
		mu.Unlock()
		own, known := owners[p.ID]
		if !known {
			return errors.New("no such invoice")
		}
		if own != "u1" {
			p.Result["memo"] = "****"
		}
		return nil
	})
	return func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		out := map[string]int{}
		for k, v := range calls {
			out[k] = v
		}
		return out
	}
}

// changeEntry is an audit edit of record id that changed memo and the
// issue date.
func changeEntry(id string) (before, after map[string]any) {
	return map[string]any{"id": id, "memo": "old-secret-memo", "issuedOn": "2026-01-05"},
		map[string]any{"id": id, "memo": "new-secret-memo", "issuedOn": "2026-02-09"}
}

// The audit log's change list asks the app's read hooks about the record
// the entry names, with its real id and stored row, the way the record
// screen does. An ownership hook masking memo for an admin who owns
// nothing keeps memo out and lets the issue date show. Probing a made-up
// row with no id made that hook refuse, and the fail-closed probe hid
// every field: the audit log showed no changes at all.
func TestChangesAsksHooksAboutTheRealRecord(t *testing.T) {
	x := newInvoiceUI(t)
	ownerMaskHook(t, x, map[string]string{"inv-1": "someone-else"})
	before, after := changeEntry("inv-1")
	body := string(x.ui.Changes(x.userCtx("/admin/audit", "", "u1"), "invoices", before, after))
	if strings.Contains(body, "secret-memo") {
		t.Fatalf("SECURITY: the change list drew memo, which the hook masks for this caller:\n%s", body)
	}
	if !strings.Contains(body, `data-cui-internal="">Issued On</span>`) {
		t.Fatalf("the unmasked issue-date change is missing:\n%s", body)
	}

	// A hook that refuses the caller's read of the record (here: it does
	// not know the id) hides every field of it, as the record screen
	// answers not found.
	x = newInvoiceUI(t)
	ownerMaskHook(t, x, map[string]string{})
	if got := x.ui.Changes(x.userCtx("/admin/audit", "", "u1"), "invoices", before, after); got != "" {
		t.Fatalf("SECURITY: a record the caller's hooked read refuses still drew changes:\n%s", got)
	}
}

// Under WithChangeCache the hooks run once per record, not once per audit
// entry: the audit page draws up to a page of entries for one record.
func TestChangesRunsHooksOncePerRecord(t *testing.T) {
	x := newInvoiceUI(t)
	calls := ownerMaskHook(t, x, map[string]string{"inv-1": "someone-else"})
	ctx := WithChangeCache(x.userCtx("/admin/audit", "", "u1"))
	before, after := changeEntry("inv-1")
	if x.ui.Changes(ctx, "invoices", before, after) == "" {
		t.Fatal("the first entry drew no change")
	}
	first := calls()["inv-1"]
	if first == 0 {
		t.Fatal("the hooks never saw the record's id")
	}
	for range 4 {
		if x.ui.Changes(ctx, "invoices", before, after) == "" {
			t.Fatal("a later entry drew no change")
		}
	}
	if got := calls(); got["inv-1"] != first || got[""] != 0 {
		t.Fatalf("five entries of one record ran the hooks %v times, want %d (one entry's worth)", got, first)
	}
}

// A record that no longer exists (deleted, purged) has no row to ask
// about, so the hooks are probed with a made-up row, as before: a hook
// that masks memo keeps it out, and one that refuses the probe hides
// every field it was asked about (fail-closed).
func TestChangesDeletedRecordFallsBackToProbe(t *testing.T) {
	x := newInvoiceUI(t)
	maskInvoiceField(t, x, "memo")
	before, after := changeEntry("inv-gone")
	body := string(x.ui.Changes(x.userCtx("/admin/audit", "", "u1"), "invoices", before, after))
	if strings.Contains(body, "secret-memo") {
		t.Fatalf("SECURITY: a deleted record's change list drew a hook-masked field:\n%s", body)
	}
	if !strings.Contains(body, `data-cui-internal="">Issued On</span>`) {
		t.Fatalf("the unmasked issue-date change is missing:\n%s", body)
	}

	x = newInvoiceUI(t)
	calls := ownerMaskHook(t, x, map[string]string{"inv-1": "someone-else"})
	if got := x.ui.Changes(x.userCtx("/admin/audit", "", "u1"), "invoices", before, after); got != "" {
		t.Fatalf("SECURITY: a probe the hook refused still drew changes:\n%s", got)
	}
	if calls()[""] == 0 {
		t.Fatalf("the deleted record was not probed: hook calls %v", calls())
	}
}
