package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func redactIDHook(ch *CrudHandler) {
	ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok && p.Result != nil {
			p.Result["id"] = "REDACTED"
		}
		return nil
	})
}

// Only a delete's stub skips redaction. An update whose record holds the
// primary key alone is still a row a GET would redact.
func TestRedactEventRecordRedactsKeyOnlyUpdate(t *testing.T) {
	ch, _ := setupRedactedHandler(t)
	redactIDHook(ch)
	ev := event.Event{Type: event.EntityUpdated, Data: map[string]any{eventKeyRecord: map[string]any{"id": "n1"}}}
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/redacted_notes/_events", nil), "u1")
	rec, _ := ch.redactEventRecord(req, ev).Data.(map[string]any)[eventKeyRecord].(map[string]any)
	if rec == nil || rec["id"] != "REDACTED" {
		t.Fatalf("a key-only update skipped AfterGet: %#v", rec)
	}
}

// Redaction builds a new envelope. The bus hands every subscriber the same
// event, so writing the redacted record into it would leak one subscriber's
// view into the next one's.
func TestRedactEventRecordLeavesTheEnvelope(t *testing.T) {
	ch, _ := setupRedactedHandler(t)
	redactIDHook(ch)
	stored := map[string]any{"id": "n1", "owner": "alice", "body": "b"}
	data := map[string]any{eventKeyEntity: "redacted_notes", eventKeyRecord: stored}
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/redacted_notes/_events", nil), "u1")
	ch.redactEventRecord(req, event.Event{Type: event.EntityCreated, Data: data})
	rec, _ := data[eventKeyRecord].(map[string]any)
	if rec == nil || rec["id"] != "n1" {
		t.Fatalf("redaction rewrote the shared envelope: %#v", data)
	}
}

// A declared Mask covers the live feed even with no hooks registered.
func TestRedactEventRecordAppliesMasks(t *testing.T) {
	f := maskCrud(t)
	ev := event.Event{Type: event.EntityUpdated, Data: map[string]any{
		eventKeyRecord: map[string]any{"id": "c1", "number": "4111222233334444", "pin": 1234},
	}}
	req := httptest.NewRequest(http.MethodGet, "/cards/_events", nil)
	rec, _ := f.ch.redactEventRecord(req, ev).Data.(map[string]any)[eventKeyRecord].(map[string]any)
	if rec == nil || rec["number"] != "****4444" || rec["pin"] != 0 {
		t.Fatalf("SECURITY: the event carried unmasked values: %#v", rec)
	}
}
