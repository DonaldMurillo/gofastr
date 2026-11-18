package webmcp

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestHandleRefusesWhatRegisterRefuses(t *testing.T) {
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	h := New(WithInstructions("Inspect first."))
	tl := validTool()
	tl.Name = InstructionsToolName
	rt := router.New()
	if err := h.Handle(rt, tl, noop); err == nil || !strings.Contains(err.Error(), "reserves") {
		t.Fatalf("Handle accepted the reserved orientation tool name: %v", err)
	}
	if len(rt.Routes()) != 0 {
		t.Fatalf("refused Handle left routes behind: %v", rt.Routes())
	}

	h = New()
	tl = validTool()
	tl.Group = "never_declared"
	rt = router.New()
	if err := h.Handle(rt, tl, noop); err == nil || !strings.Contains(err.Error(), "never declared") {
		t.Fatalf("Handle accepted an undeclared group: %v", err)
	}
	if len(rt.Routes()) != 0 {
		t.Fatalf("refused Handle left routes behind: %v", rt.Routes())
	}
}

func TestPathRejectsEncodedDotSegments(t *testing.T) {
	for _, p := range []string{"/api/%2e%2e/admin", "/api/%2E/x", "/api/%5cx"} {
		if validToolPath(p) {
			t.Errorf("validToolPath(%q) = true, want false", p)
		}
	}
}

func TestRequesterDependentMountsArePrivate(t *testing.T) {
	cases := map[string]mountConfig{
		"private":   {private: true},
		"pageScope": {pageScope: func(*http.Request) bool { return true }},
		"assetMW":   {assetMW: []router.Middleware{func(n http.Handler) http.Handler { return n }}},
	}
	for name, c := range cases {
		if !c.requesterDependent() {
			t.Errorf("%s: requesterDependent() = false, want true", name)
		}
	}
	if (&mountConfig{bridgeDebug: true}).requesterDependent() {
		t.Error("bridgeDebug alone made the mount requester-dependent")
	}
}

type hijackWriter struct {
	http.ResponseWriter
	hijacked bool
}

func (h *hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

func TestStatusRecorderPassesHijack(t *testing.T) {
	under := &hijackWriter{ResponseWriter: httptest.NewRecorder()}
	rec := &statusRecorder{ResponseWriter: under}
	if _, _, err := http.NewResponseController(rec).Hijack(); err != nil || !under.hijacked {
		t.Fatalf("Hijack did not reach the underlying writer: err=%v hijacked=%v", err, under.hijacked)
	}
	if _, _, err := (&statusRecorder{ResponseWriter: httptest.NewRecorder()}).Hijack(); err == nil {
		t.Fatal("Hijack on a writer without Hijacker returned no error")
	}
}

func TestNoObserverMeansNoInstrumentation(t *testing.T) {
	h := New()
	rt := router.New()
	if err := h.Handle(rt, validTool(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if InvocationID(r.Context()) != "" {
			t.Error("handler saw an invocation id with no observer installed")
		}
	})); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/echo", nil)
	req.Header.Set(MarkerHeader, "1")
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	if rec.Header().Get(InvocationHeader) != "" {
		t.Fatal("marked call got an invocation header with no observer installed")
	}
}

func TestFailedMountRetryAddsOrientationToolOnce(t *testing.T) {
	h := New(WithInstructions("Inspect first."))
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	busy := router.New()
	busy.Get(ManifestRoute, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if _, err := h.Mount(busy, nil); err == nil {
		t.Fatal("Mount onto a squatted route succeeded")
	}
	rt := router.New()
	if _, err := h.Mount(rt, nil); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, tl := range manifestTools(t, rt) {
		if tl.Name == InstructionsToolName {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("manifest carries %d orientation tools after a retried Mount, want 1", n)
	}
}

func TestObserverSeesMiddlewareRefusal(t *testing.T) {
	var got []ToolEvent
	h := New(WithObserver(func(e ToolEvent) { got = append(got, e) }))
	deny := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	}
	rt := router.New()
	if err := h.Handle(rt, validTool(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), WithHTTPMiddleware(deny)); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/echo", nil)
	req.Header.Set(MarkerHeader, "1")
	rt.ServeHTTP(httptest.NewRecorder(), req)
	if len(got) != 1 || got[0].StatusCode != http.StatusUnauthorized || got[0].ErrClass != "http_401" {
		t.Fatalf("observer events = %+v, want one invoke event with status 401", got)
	}
}

func TestExampleTypeCheckIgnoresUnknownTypeKeywords(t *testing.T) {
	cases := []struct {
		declared any
		v        any
		want     bool
	}{
		{"future_type", "x", true},
		{float64(3), "x", true},
		{"integer", 1.5, false},
		{"integer", float64(2), true},
		{[]any{"string", "null"}, nil, true},
		{"string", float64(1), false},
	}
	for _, c := range cases {
		if got := typeMatches(c.declared, c.v); got != c.want {
			t.Errorf("typeMatches(%v, %v) = %v, want %v", c.declared, c.v, got, c.want)
		}
	}
}

func TestBadGroupDeclarationFailsMount(t *testing.T) {
	for name, declare := range map[string]func(*Host) *Group{
		"bad name":  func(h *Host) *Group { return h.Group("bad name") },
		"duplicate": func(h *Host) *Group { h.Group("g"); return h.Group("g") },
	} {
		h := New()
		if err := declare(h).Register(validTool()); err == nil {
			t.Fatalf("%s: the badly declared group accepted a tool", name)
		}
		if err := h.Register(validTool()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Mount(router.New(), nil); err == nil {
			t.Fatalf("%s: Mount succeeded with a bad group declaration", name)
		}
	}
}

func TestObservedPanicIsReportedAndRepanicked(t *testing.T) {
	var got []ToolEvent
	h := New(WithObserver(func(e ToolEvent) { got = append(got, e) }))
	rt := router.New()
	if err := h.Handle(rt, validTool(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/echo", nil)
	req.Header.Set(MarkerHeader, "1")
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the handler panic did not propagate past the observer")
			}
		}()
		rt.ServeHTTP(httptest.NewRecorder(), req)
	}()
	if len(got) != 1 || got[0].ErrClass != "panic" {
		t.Fatalf("observer events = %+v, want one with ErrClass panic", got)
	}
}
