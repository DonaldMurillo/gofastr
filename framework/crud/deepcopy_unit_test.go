package crud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// redactEventRecord hands each subscriber its own copy, so one subscriber's
// in-place redaction cannot reach another's delivery, the bus's record, or the
// durable outbox row.
//
// With no AfterGet hook registered it returns the envelope untouched on
// purpose: there is nothing to redact, and deep-copying every delivery to
// protect against a subscriber that mutates what it was handed is not a trade
// worth making. The isolation guarantee starts where redaction does.
func TestRedactEventRecordIsolatesEachSubscriber(t *testing.T) {
	ent := entity.Define("rows", entity.EntityConfig{
		Fields: []schema.Field{{Name: "id", Type: schema.String}},
	})
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		// Masks a NESTED field, the case a shallow copy fails.
		if prof, ok := p.Result["profile"].(map[string]any); ok {
			prof["ssn"] = "***-**-****"
		}
		return nil
	})
	ch := &CrudHandler{Entity: ent, PrimaryKey: []string{"id"}, Hooks: reg}

	record := map[string]any{"id": "r1", "profile": map[string]any{"ssn": "111-22-3333"}}
	ev := event.Event{
		Type: event.EntityUpdated,
		Data: map[string]any{"entity": "rows", "ownerId": "u1", "record": record},
	}
	req := httptest.NewRequest(http.MethodGet, "/rows/_events", nil)

	first := ch.redactEventRecord(req, ev)
	second := ch.redactEventRecord(req, ev)

	for i, out := range []event.Event{first, second} {
		data, _ := out.Data.(map[string]any)
		rec, _ := data["record"].(map[string]any)
		prof, _ := rec["profile"].(map[string]any)
		if prof == nil {
			t.Fatalf("delivery %d lost the nested object: %#v", i, out.Data)
		}
		if prof["ssn"] != "***-**-****" {
			t.Errorf("delivery %d was not redacted: ssn=%v", i, prof["ssn"])
		}
		// Delivery-routing stamps are carried over untouched.
		if data["ownerId"] != "u1" || data["entity"] != "rows" {
			t.Errorf("delivery %d lost its routing stamps: %#v", i, data)
		}
	}

	// The source record, the bus's copy, and the outbox's, is untouched.
	if got := record["profile"].(map[string]any)["ssn"]; got != "111-22-3333" {
		t.Errorf("the redaction reached the source record: ssn=%v", got)
	}
}

// A hook failure omits the record rather than publishing it raw.
func TestRedactEventRecordOmitsTheRecordOnHookError(t *testing.T) {
	ent := entity.Define("rows", entity.EntityConfig{
		Fields: []schema.Field{{Name: "id", Type: schema.String}},
	})
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		return errors.New("redactor unavailable")
	})
	ch := &CrudHandler{Entity: ent, PrimaryKey: []string{"id"}, Hooks: reg}

	ev := event.Event{
		Type: event.EntityUpdated,
		Data: map[string]any{"entity": "rows", "record": map[string]any{"id": "r1", "secret": "S"}},
	}
	out := ch.redactEventRecord(httptest.NewRequest(http.MethodGet, "/rows/_events", nil), ev)
	data, _ := out.Data.(map[string]any)
	// The key is present holding a nil map, which marshals to "record": null.
	// Compare the map, not the any, a nil map boxed in an interface is not
	// itself nil.
	rec, _ := data["record"].(map[string]any)
	if rec != nil {
		t.Errorf("a failed redactor published the record anyway: %#v", rec)
	}
	if body, err := json.Marshal(out); err != nil {
		t.Fatalf("marshal: %v", err)
	} else if !strings.Contains(string(body), `"record":null`) {
		t.Errorf("the delivered event should carry a null record, got: %s", body)
	}
}

// The helper is called with the SSE handler's request today, but a nil there
// would panic inside the delivery goroutine and kill the stream.
func TestRedactEventRecordSurvivesANilRequest(t *testing.T) {
	ent := entity.Define("rows", entity.EntityConfig{
		Fields: []schema.Field{{Name: "id", Type: schema.String}},
	})
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, _ := data.(*hook.GetPayload)
		if p != nil && p.Result != nil {
			p.Result["secret"] = "***"
		}
		return nil
	})
	ch := &CrudHandler{Entity: ent, PrimaryKey: []string{"id"}, Hooks: reg}

	ev := event.Event{
		Type: event.EntityUpdated,
		Data: map[string]any{"entity": "rows", "record": map[string]any{"id": "r1", "secret": "S"}},
	}
	out := ch.redactEventRecord(nil, ev)
	data, _ := out.Data.(map[string]any)
	rec, _ := data["record"].(map[string]any)
	if rec == nil || rec["secret"] != "***" {
		t.Fatalf("nil request path did not redact: %#v", data)
	}
}

// A delete stages a primary-key-only stub. Redacting it as though it were a
// full row destroyed the payload once already.
func TestRedactEventRecordPassesDeleteStubsThrough(t *testing.T) {
	// A registry with a mutating AfterGet, or redactEventRecord returns at its
	// first line and the EntityDeleted branch is never reached, which is what
	// the first version of this test did.
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, _ := data.(*hook.GetPayload)
		if p != nil && p.Result != nil {
			p.Result["id"] = "REDACTED"
		}
		return nil
	})
	ent := entity.Define("rows", entity.EntityConfig{
		Fields: []schema.Field{{Name: "id", Type: schema.String}},
	})
	ch := &CrudHandler{Entity: ent, PrimaryKey: []string{"id"}, Hooks: reg}
	ev := event.Event{
		Type: event.EntityDeleted,
		Data: map[string]any{"entity": "rows", "record": map[string]any{"id": "r1"}},
	}
	out := ch.redactEventRecord(nil, ev)
	data, _ := out.Data.(map[string]any)
	rec, _ := data["record"].(map[string]any)
	if rec == nil || rec["id"] != "r1" {
		t.Fatalf("the delete stub did not survive delivery: %#v", out.Data)
	}
}

// StageEvent is a no-op without an outbox: an app that never configured
// one must not get an error from a write path that merely wanted to
// record an event.
func TestStageEventWithoutOutbox(t *testing.T) {
	ch := &CrudHandler{}
	if err := ch.StageEvent(context.Background(), "created", map[string]any{"id": "1"}); err != nil {
		t.Fatalf("StageEvent with no outbox = %v, want nil", err)
	}
}

// fakeOutbox records what CRUD staged, so the outbox-present paths are
// exercised without a database.
type fakeOutbox struct {
	appended []string
	nudged   int
}

func (f *fakeOutbox) AppendFor(_ context.Context, _ DBExecutor, _, eventType string, _ any) (string, error) {
	f.appended = append(f.appended, eventType)
	return "1", nil
}
func (f *fakeOutbox) Nudge() { f.nudged++ }

// With an outbox configured, StageEvent writes through it and EmitEvent
// nudges the relay, the two halves of "the write committed, go deliver".
func TestStageEventWritesToOutbox(t *testing.T) {
	fo := &fakeOutbox{}
	// eventData needs a real entity behind it, so build a real handler.
	ch := maskedRowHandler(t, hook.AfterGet)
	ch.Outbox = fo

	if err := ch.StageEvent(context.Background(), "created", map[string]any{"id": "1"}); err != nil {
		t.Fatalf("StageEvent: %v", err)
	}
	if len(fo.appended) != 1 || fo.appended[0] != "created" {
		t.Fatalf("outbox recorded %v, want one \"created\"", fo.appended)
	}

	ch.EmitEvent(context.Background(), "created", map[string]any{"id": "1"})
	if fo.nudged == 0 {
		t.Error("EmitEvent did not nudge the outbox relay")
	}
}
