package webmcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

type eventLog struct {
	events []ToolEvent
}

func (l *eventLog) observe(ev ToolEvent) { l.events = append(l.events, ev) }

func (l *eventLog) byPhase(p ToolPhase) []ToolEvent {
	var out []ToolEvent
	for _, ev := range l.events {
		if ev.Phase == p {
			out = append(out, ev)
		}
	}
	return out
}

func TestRegisterFailuresReachObserver(t *testing.T) {
	var log eventLog
	h := New(WithObserver(log.observe))
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	// Invalid path.
	bad := validTool()
	bad.Name = "bad_path"
	bad.Path = "not/absolute"
	if err := h.Register(bad); err == nil {
		t.Fatal("bad path accepted")
	}
	// Duplicate name.
	if err := h.Register(validTool()); err == nil {
		t.Fatal("duplicate accepted")
	}
	// After mount.
	if _, err := h.Mount(router.New(), nil); err != nil {
		t.Fatal(err)
	}
	late := validTool()
	late.Name = "late"
	late.Path = "/api/late"
	if err := h.Register(late); err == nil {
		t.Fatal("post-mount register accepted")
	}

	got := log.byPhase(PhaseRegister)
	if len(got) != 3 {
		t.Fatalf("register events: %+v", got)
	}
	want := []struct{ name, class string }{
		{"bad_path", "path"},
		{"echo", "duplicate_name"},
		{"late", "after_mount"},
	}
	for i, w := range want {
		if got[i].Name != w.name || got[i].ErrClass != w.class {
			t.Fatalf("event %d = {name:%q class:%q}, want {name:%q class:%q}", i, got[i].Name, got[i].ErrClass, w.name, w.class)
		}
		if got[i].Method == "" || got[i].Path == "" {
			t.Fatalf("event %d lacks routing metadata: %+v", i, got[i])
		}
	}
	// The successful registration never became an event: diagnostics
	// stay failure-scoped.
	if n := len(log.events); n != 3 {
		t.Fatalf("success produced events: %+v", log.events)
	}
}

func TestHandleFailuresReachObserver(t *testing.T) {
	var log eventLog
	h := New(WithObserver(log.observe))
	rt := router.New()
	if err := h.Handle(rt, validTool(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})); err != nil {
		t.Fatal(err)
	}
	dup := validTool()
	dup.Name = "second"
	if err := h.Handle(rt, dup, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})); err == nil {
		t.Fatal("route conflict accepted")
	}
	got := log.byPhase(PhaseRegister)
	if len(got) != 1 || got[0].Name != "second" || got[0].ErrClass != "route_conflict" {
		t.Fatalf("handle failure events: %+v", got)
	}
}

func TestObserverReportsMarkedCallsOnly(t *testing.T) {
	var log eventLog
	h := New(WithObserver(log.observe))
	rt := router.New()
	hits := 0
	if err := h.Handle(rt, validTool(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}

	// An ordinary call and an agent call reach the same handler, but
	// only the marked one is an invocation event: the marker attributes
	// without changing behavior.
	for _, marked := range []bool{false, true, false, true} {
		req := httptest.NewRequest(http.MethodPost, "/api/echo", nil)
		if marked {
			req.Header.Set(MarkerHeader, "1")
		}
		rt.ServeHTTP(httptest.NewRecorder(), req)
	}
	if hits != 4 {
		t.Fatalf("handler hits: %d", hits)
	}
	inv := log.byPhase(PhaseInvoke)
	if len(inv) != 2 {
		t.Fatalf("invocation events: %+v", inv)
	}
	for _, ev := range inv {
		if ev.Name != "echo" || ev.Method != http.MethodPost || ev.Path != "/api/echo" {
			t.Fatalf("event metadata: %+v", ev)
		}
		if ev.StatusCode != http.StatusOK || ev.ErrClass != "" {
			t.Fatalf("event outcome: %+v", ev)
		}
		if ev.Duration < 0 || ev.InvocationID == "" {
			t.Fatalf("event lacks duration or id: %+v", ev)
		}
	}
}

// The guard that keeps observability metadata-safe: a secret planted in
// the request's query string and body must never reach the observer —
// not via Path, not via any field, not even partially.
func TestObserverEventsNeverCarryInputs(t *testing.T) {
	const secret = "hunter2-super-secret-token"
	var log eventLog
	h := New(WithObserver(log.observe))
	rt := router.New()
	if err := h.Handle(rt, Tool{
		Name:        "echo",
		Description: "Echoes.",
		Method:      http.MethodGet,
		Path:        "/api/echo",
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/echo?token="+secret, nil)
	req.Header.Set(MarkerHeader, "1")
	rt.ServeHTTP(httptest.NewRecorder(), req)

	events := log.byPhase(PhaseInvoke)
	if len(events) != 1 {
		t.Fatalf("invocation events: %+v", events)
	}
	if events[0].Path != "/api/echo" {
		t.Fatalf("event path is not the declared path: %q", events[0].Path)
	}
	if s := fmt.Sprint(log.events); strings.Contains(s, secret) || strings.Contains(s, "token") {
		t.Fatalf("observer saw request data: %s", s)
	}
}

func TestObserverReportsErrorClasses(t *testing.T) {
	var log eventLog
	h := New(WithObserver(log.observe))
	rt := router.New()
	if err := h.Handle(rt, validTool(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/echo", nil)
	req.Header.Set(MarkerHeader, "1")
	rt.ServeHTTP(httptest.NewRecorder(), req)
	inv := log.byPhase(PhaseInvoke)
	if len(inv) != 1 || inv[0].StatusCode != http.StatusInternalServerError || inv[0].ErrClass != "http_500" {
		t.Fatalf("error events: %+v", inv)
	}
}

// The correlation contract: the id the handler reads from its request
// context is the id on the observer's event and the response header, so
// "command -> delivery -> acknowledgement" chains can be joined on one
// value.
func TestInvocationIDCorrelatesEventAndHandler(t *testing.T) {
	var log eventLog
	h := New(WithObserver(log.observe))
	rt := router.New()
	var ack string
	if err := h.Handle(rt, validTool(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The app's own delivery acknowledgement, tagged with the same
		// id the observer will report.
		ack = "ack:" + InvocationID(r.Context())
		_, _ = w.Write([]byte("ok"))
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/echo", nil)
	req.Header.Set(MarkerHeader, "1")
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, req)

	inv := log.byPhase(PhaseInvoke)
	if len(inv) != 1 {
		t.Fatalf("invocation events: %+v", inv)
	}
	if inv[0].InvocationID == "" {
		t.Fatal("event has no invocation id")
	}
	if hdr := rec.Header().Get(InvocationHeader); hdr != inv[0].InvocationID {
		t.Fatalf("response header %q != event id %q", hdr, inv[0].InvocationID)
	}
	if ack != "ack:"+inv[0].InvocationID {
		t.Fatalf("handler acknowledgement %q is not correlated to event id %q", ack, inv[0].InvocationID)
	}
	// Unmarked traffic carries no id at all.
	req = httptest.NewRequest(http.MethodPost, "/api/echo", nil)
	req = req.WithContext(context.WithValue(req.Context(), invocationKey{}, "nope"))
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	if rec.Header().Get(InvocationHeader) != "" {
		t.Fatal("unmarked call received an invocation header")
	}
}

func TestBridgeDebugLiteralIsOptIn(t *testing.T) {
	rt, url := mountedHost(t)
	rec := get(t, rt, url)
	if !strings.Contains(rec.Body.String(), "const debug = false;") {
		t.Fatal("default bridge does not disable debug state")
	}
	rt2, url2 := mountedHost(t, WithBridgeDebug())
	rec = get(t, rt2, url2)
	if !strings.Contains(rec.Body.String(), "const debug = true;") {
		t.Fatal("WithBridgeDebug did not enable the debug literal")
	}
}
