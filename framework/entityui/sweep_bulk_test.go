package entityui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A panicking app action logs only the panic's type: its text may carry record contents.
func TestSweepBulkActionPanicLogsTypeOnly(t *testing.T) {
	buf := sweepCaptureLog(t)
	ext := Extensions{Entities: map[string]Extension{"invoices": {Actions: []Action{{
		Key: "boom", Bulk: true,
		Run: func(context.Context, ActionContext) error { panic("secret row text") },
	}}}}}
	x := newTestUIExt(t, invoiceEntities(), invoiceRows(), ext, withAPI(map[string]string{"invoices": "/api/invoices"}))
	if code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "run:boom", "scope": "selected", "ids": "inv-1"}); code != http.StatusOK {
		t.Fatalf("status %d: %v", code, out)
	}
	if strings.Contains(buf.String(), "secret row text") {
		t.Fatalf("the action's panic text reached the log: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "action panicked") {
		t.Fatalf("no panic logged: %s", buf.String())
	}
}

// A restore the caller's gates refuse counts as skipped, not failed.
func TestSweepBulkRefusedRestoreIsSkipped(t *testing.T) {
	x, policy := ownerNotesUI(t)
	if err := policy.Grant("deleter", "notes:delete"); err != nil {
		t.Fatal(err)
	}
	rec := postNotesBulk(t, x, bulkCtx("u1", policy, "deleter"),
		map[string]any{"action": "restore", "scope": "deleted", "ids": []string{"u1-dead"}})
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusOK || out["skipped"] != float64(1) || out["failed"] != float64(0) {
		t.Fatalf("restore = %d %v, want one skipped and none failed", rec.Code, out)
	}
}
