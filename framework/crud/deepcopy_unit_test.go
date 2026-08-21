package crud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// deepCopyRecord is what keeps one holder of an event record from writing into
// another's. A copy that stops at the top level is indistinguishable from a
// correct one until a hook masks something NESTED, which is how a shallow
// version shipped and reached the event bus through a shared sub-map.

func TestDeepCopyRebuildsEveryNestedContainer(t *testing.T) {
	src := map[string]any{
		"id":     "r1",
		"scalar": 42,
		"nil":    nil,
		"object": map[string]any{"ssn": "111-22-3333"},
		"rows":   []map[string]any{{"pin": "9999"}},
		"mixed":  []any{map[string]any{"tok": "abc"}, "plain", 7},
	}
	cp := deepCopyRecord(src)

	// Mutating every nested container in the copy must leave the source alone.
	cp["object"].(map[string]any)["ssn"] = "***"
	cp["rows"].([]map[string]any)[0]["pin"] = "***"
	cp["mixed"].([]any)[0].(map[string]any)["tok"] = "***"

	if got := src["object"].(map[string]any)["ssn"]; got != "111-22-3333" {
		t.Errorf("nested map is shared with the copy: ssn=%v", got)
	}
	if got := src["rows"].([]map[string]any)[0]["pin"]; got != "9999" {
		t.Errorf("[]map[string]any element is shared with the copy: pin=%v", got)
	}
	if got := src["mixed"].([]any)[0].(map[string]any)["tok"]; got != "abc" {
		t.Errorf("[]any element is shared with the copy: tok=%v", got)
	}

	// Scalars and nil survive the round trip unchanged.
	if cp["scalar"] != 42 || cp["id"] != "r1" || cp["nil"] != nil {
		t.Errorf("scalars did not survive the copy: %#v", cp)
	}
}

// A scalar must come back EQUAL, not merely non-nil. The first version of this
// asserted `got == nil && v != nil`, which is unsatisfiable for every input it
// listed, returning a constant from the default branch left it green.
func TestDeepCopyValuePassesScalarsThrough(t *testing.T) {
	for _, v := range []any{nil, 1, "s", 2.5, true, int64(7), time.Unix(0, 0).UTC()} {
		if got := deepCopyValue(v); got != v {
			t.Errorf("deepCopyValue(%#v) = %#v, want the same value", v, got)
		}
	}
	// []byte is a slice, so it is COPIED rather than passed through, equal
	// contents, different backing array.
	b := []byte("secret")
	got, ok := deepCopyValue(b).([]byte)
	if !ok || string(got) != "secret" {
		t.Fatalf("deepCopyValue([]byte) = %#v, want equal contents", got)
	}
	got[0] = 'X'
	if b[0] != 's' {
		t.Error("[]byte was passed through by reference; a hook masking a BLOB column would " +
			"write into the event record")
	}
}

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
	ch := &CrudHandler{Entity: ent, PrimaryKey: "id", Hooks: reg}

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
	ch := &CrudHandler{Entity: ent, PrimaryKey: "id", Hooks: reg}

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
	ch := &CrudHandler{Entity: ent, PrimaryKey: "id", Hooks: reg}

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
	ch := &CrudHandler{Entity: ent, PrimaryKey: "id", Hooks: reg}
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

// The type switch names the shapes the framework itself produces. The shapes
// that matter are the ones an application's write hook injects, and a
// hand-written switch only covers what its author thought of. Each entry here
// was verified to be SHARED (not copied) by the first version.
func TestDeepCopyRebuildsUnnamedContainerShapes(t *testing.T) {
	src := map[string]any{
		"bytes":     []byte("4111111111111111"),
		"strings":   []string{"a", "b"},
		"strmap":    map[string]string{"ssn": "111-22-3333"},
		"mapslice":  map[string][]any{"k": {"v"}},
		"nestedmap": [][]map[string]any{{{"pin": "9999"}}},
		"ints":      []int{1, 2, 3},
	}
	cp := deepCopyRecord(src)

	// Mutate every container in the copy.
	copy(cp["bytes"].([]byte), []byte("****************"))
	cp["strings"].([]string)[0] = "***"
	cp["strmap"].(map[string]string)["ssn"] = "***"
	cp["mapslice"].(map[string][]any)["k"][0] = "***"
	cp["nestedmap"].([][]map[string]any)[0][0]["pin"] = "***"
	cp["ints"].([]int)[0] = 99

	if got := string(src["bytes"].([]byte)); got != "4111111111111111" {
		t.Errorf("[]byte is shared with the copy: %q", got)
	}
	if got := src["strings"].([]string)[0]; got != "a" {
		t.Errorf("[]string is shared with the copy: %q", got)
	}
	if got := src["strmap"].(map[string]string)["ssn"]; got != "111-22-3333" {
		t.Errorf("map[string]string is shared with the copy: %q", got)
	}
	if got := src["mapslice"].(map[string][]any)["k"][0]; got != "v" {
		t.Errorf("map[string][]any is shared with the copy: %v", got)
	}
	if got := src["nestedmap"].([][]map[string]any)[0][0]["pin"]; got != "9999" {
		t.Errorf("[][]map[string]any is shared with the copy: %v", got)
	}
	if got := src["ints"].([]int)[0]; got != 1 {
		t.Errorf("[]int is shared with the copy: %v", got)
	}
}

// Nil containers and scalars must survive the reflective path unchanged.
func TestDeepCopyHandlesNilContainersAndScalars(t *testing.T) {
	var nilMap map[string]string
	var nilSlice []string
	src := map[string]any{
		"nilmap":   nilMap,
		"nilslice": nilSlice,
		"time":     time.Unix(0, 0).UTC(),
		"arr":      [2]string{"a", "b"},
	}
	cp := deepCopyRecord(src)
	if cp["nilmap"] != nil && !reflect.ValueOf(cp["nilmap"]).IsNil() {
		t.Errorf("nil map became non-nil: %#v", cp["nilmap"])
	}
	if cp["nilslice"] != nil && !reflect.ValueOf(cp["nilslice"]).IsNil() {
		t.Errorf("nil slice became non-nil: %#v", cp["nilslice"])
	}
	if cp["time"] != src["time"] {
		t.Errorf("time.Time did not survive: %v", cp["time"])
	}
	// An array is a value type, so the copy is independent either way; assert
	// the contents survived rather than identity.
	if cp["arr"].([2]string)[0] != "a" {
		t.Errorf("array did not survive: %#v", cp["arr"])
	}
}

// The reflective fallback's remaining shapes: an array of containers, a nil
// interface element, and a value kind it does not traverse.
func TestDeepCopyReflectCoversArraysAndNilElements(t *testing.T) {
	type holder struct{ N int }
	src := map[string]any{
		"arrOfMaps": [2]map[string]string{{"a": "1"}, {"b": "2"}},
		"withNil":   []any{nil, map[string]any{"k": "v"}},
		"structptr": &holder{N: 1},
	}
	cp := deepCopyRecord(src)

	cp["arrOfMaps"].([2]map[string]string)[0]["a"] = "MUT"
	if got := src["arrOfMaps"].([2]map[string]string)[0]["a"]; got != "1" {
		t.Errorf("an array's element maps are shared with the copy: %q", got)
	}
	cp["withNil"].([]any)[1].(map[string]any)["k"] = "MUT"
	if got := src["withNil"].([]any)[1].(map[string]any)["k"]; got != "v" {
		t.Errorf("a slice element beside a nil is shared with the copy: %q", got)
	}
	if cp["withNil"].([]any)[0] != nil {
		t.Errorf("a nil element did not survive: %#v", cp["withNil"])
	}
	// A pointer to a STRUCT is deliberately not traversed, cloning an
	// arbitrary struct means cloning whatever it embeds (a mutex, a file
	// handle, a driver connection).
	if cp["structptr"] != src["structptr"] {
		t.Errorf("a pointer to a struct should be passed through, not cloned")
	}
}

// A pointer to a plain CONTAINER is not covered by the struct rationale above:
// it is the record's own data one indirection out, and leaving it aliased put
// the response-hook copy and the record already handed to the event goroutine
// back on the same map, the concurrent-map-writes throw the reflective
// fallback exists to prevent.
func TestDeepCopyCopiesPointerContainers(t *testing.T) {
	nested := map[string]any{"secret": "stored"}
	list := []map[string]any{{"secret": "stored"}}
	anys := []any{map[string]any{"secret": "stored"}}
	src := map[string]any{"profile": &nested, "rows": &list, "items": &anys}

	cp := deepCopyRecord(src)

	(*cp["profile"].(*map[string]any))["secret"] = "***"
	if nested["secret"] != "stored" {
		t.Errorf("*map[string]any is shared with the copy: %#v", nested)
	}
	(*cp["rows"].(*[]map[string]any))[0]["secret"] = "***"
	if list[0]["secret"] != "stored" {
		t.Errorf("*[]map[string]any is shared with the copy: %#v", list)
	}
	(*cp["items"].(*[]any))[0].(map[string]any)["secret"] = "***"
	if anys[0].(map[string]any)["secret"] != "stored" {
		t.Errorf("*[]any is shared with the copy: %#v", anys)
	}
}

// deepCopyReflectValue's Interface arm: reached only for an interface-typed
// element inside a REFLECTIVELY copied container, map[string][]any, or a
// named map type. The typed fast paths cover map[string]any and []any
// directly, so nothing in the suite exercised this, and it is the same
// write-through class one container deeper.
func TestDeepCopyReflectDescendsIntoInterfaceElements(t *testing.T) {
	inner := map[string]any{"pin": "9999"}
	type Extra map[string]any
	src := map[string]any{
		"mapOfAnySlice": map[string][]any{"k": {inner}},
		"namedMap":      Extra{"nested": map[string]any{"ssn": "111"}},
	}
	cp := deepCopyRecord(src)

	cp["mapOfAnySlice"].(map[string][]any)["k"][0].(map[string]any)["pin"] = "***"
	if got := src["mapOfAnySlice"].(map[string][]any)["k"][0].(map[string]any)["pin"]; got != "9999" {
		t.Errorf("an interface element inside map[string][]any is shared with the copy: %q", got)
	}
	cp["namedMap"].(Extra)["nested"].(map[string]any)["ssn"] = "***"
	if got := src["namedMap"].(Extra)["nested"].(map[string]any)["ssn"]; got != "111" {
		t.Errorf("an interface element inside a named map type is shared with the copy: %q", got)
	}
}

// The pointer arm traverses only pointers to containers, and only when
// there is something to traverse. These are the refusals: a nil pointer
// has no pointee, and a pointer to a struct is deliberately left aliased
// (cloning an arbitrary struct means cloning whatever it embeds, a
// mutex, a file handle, a driver connection).
func TestDeepCopyRefusesUncopyablePointers(t *testing.T) {
	type opaque struct{ N int }
	var nilMap *map[string]any
	ptrStruct := &opaque{N: 1}

	row := map[string]any{
		"nil_map_ptr": nilMap,
		"struct_ptr":  ptrStruct,
		"nil_iface":   nil,
		"iface_slice": []any{nil, map[string]any{"k": "v"}},
		"ptr_slice":   []*opaque{nil, ptrStruct},
	}
	cp := deepCopyRecord(row)

	if cp["nil_map_ptr"] != any(nilMap) {
		t.Error("a nil *map was not passed through untouched")
	}
	if cp["struct_ptr"] != any(ptrStruct) {
		t.Error("a *struct was cloned; the struct exclusion is deliberate")
	}
	if cp["nil_iface"] != nil {
		t.Error("a nil interface value was not passed through")
	}
	// The container around them is still rebuilt, which is the point.
	inner := cp["iface_slice"].([]any)[1].(map[string]any)
	inner["k"] = "mutated"
	if row["iface_slice"].([]any)[1].(map[string]any)["k"] != "v" {
		t.Error("mutating the copy reached the original through an interface element")
	}
	if got := cp["ptr_slice"].([]*opaque); got[0] != nil || got[1] != ptrStruct {
		t.Error("a []*struct element was not passed through untouched")
	}
}

// Arrays and maps reach elements through deepCopyReflectValue rather than
// the typed fast paths, so they exercise the interface- and pointer-element
// arms. A nil interface element and a nil container pointer both have
// nothing to clone and must survive the walk untouched.
func TestDeepCopyWalksArrayAndMapElements(t *testing.T) {
	inner := map[string]any{"k": "v"}
	var nilMapPtr *map[string]any

	row := map[string]any{
		"arr":       [2]any{nil, inner},
		"ptr_map":   map[string]*map[string]any{"a": nilMapPtr, "b": &inner},
		"nil_slice": []any(nil),
	}
	cp := deepCopyRecord(row)

	arr := cp["arr"].([2]any)
	if arr[0] != nil {
		t.Error("a nil interface element did not survive the array walk")
	}
	arr[1].(map[string]any)["k"] = "mutated"
	if inner["k"] != "v" {
		t.Error("mutating the copied array element reached the original map")
	}

	m := cp["ptr_map"].(map[string]*map[string]any)
	if m["a"] != nil {
		t.Error("a nil *map value did not survive the map walk")
	}
	if m["b"] == &inner {
		t.Error("a *map value was aliased rather than copied")
	}
}

// Anything that is not a container falls through untouched. Channels and
// funcs are the honest examples: there is no meaningful clone, and the
// pointer work must not have widened the walk into them.
func TestDeepCopyPassesThroughOpaqueKinds(t *testing.T) {
	ch := make(chan int, 1)
	fn := func() {}
	row := map[string]any{"chan": ch, "func": fn}

	cp := deepCopyRecord(row)
	if cp["chan"] != any(ch) {
		t.Error("a channel was not passed through untouched")
	}
	if cp["func"] == nil {
		t.Error("a func was dropped rather than passed through")
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

func (f *fakeOutbox) Append(_ context.Context, _ DBExecutor, eventType string, _ any) (string, error) {
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
