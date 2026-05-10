package widget_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/core/router"
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/widget"
	"github.com/gofastr/gofastr/core-ui/widget/preset"
)

// stubComponent is a minimal Component for tests.
type stubComponent struct{ html string }

func (s stubComponent) Render() render.HTML { return render.HTML(s.html) }

func TestBuilderDefaults(t *testing.T) {
	def := widget.New("demo").Build()
	if def.Name != "demo" {
		t.Errorf("Name = %q, want demo", def.Name)
	}
	if def.Position != widget.BottomRight {
		t.Errorf("default Position = %q, want bottom-right", def.Position)
	}
	if def.Bootstrap != widget.AutoScript {
		t.Errorf("default Bootstrap = %q, want auto-script", def.Bootstrap)
	}
	if def.Backdrop {
		t.Errorf("default Backdrop should be false")
	}
}

func TestBuilderModalSetsBackdropAndCloseOnEscape(t *testing.T) {
	def := preset.Modal("confirm").Build()
	if !def.Backdrop {
		t.Errorf("Modal preset must enable Backdrop")
	}
	if !def.CloseOnEscape {
		t.Errorf("Modal preset must enable CloseOnEscape")
	}
	if !def.CloseOnClickOutside {
		t.Errorf("Modal preset must enable CloseOnClickOutside")
	}
}

func TestSlotsRenderInOrder(t *testing.T) {
	def := widget.New("demo").
		Slot("header", stubComponent{`<h1>Hello</h1>`}).
		Slot("body", stubComponent{`<p>World</p>`}).
		Build()
	if len(def.Slots) != 2 {
		t.Fatalf("expected 2 slots, got %d", len(def.Slots))
	}
	if def.Slots[0].Name != "header" || def.Slots[1].Name != "body" {
		t.Errorf("slot order wrong: %+v", def.Slots)
	}
}

func TestSignalRegistration(t *testing.T) {
	def := widget.New("demo").
		Signal("count", widget.SignalFunc(func() (any, error) { return 42, nil })).
		Build()
	if _, ok := def.Signals["count"]; !ok {
		t.Errorf("signal 'count' not registered")
	}
}

func TestSSEBinding(t *testing.T) {
	def := widget.New("demo").
		SSE("/.events", "world_edit", "page").
		Build()
	if len(def.SSE) != 1 {
		t.Fatalf("expected 1 SSE binding")
	}
	b := def.SSE[0]
	if b.Path != "/.events" || b.Event != "world_edit" || b.Signal != "page" {
		t.Errorf("SSE binding wrong: %+v", b)
	}
}

func TestRPCDefaultMethodIsPOST(t *testing.T) {
	def := widget.New("demo").
		RPC("", "/x", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		Build()
	if len(def.RPCs) != 1 {
		t.Fatalf("expected 1 RPC")
	}
	if def.RPCs[0].Method != "POST" {
		t.Errorf("default method = %q, want POST", def.RPCs[0].Method)
	}
}

func TestMountServesBootstrapStyleAndState(t *testing.T) {
	def := widget.New("kiln-test").
		Slot("header", stubComponent{`<span class="hi">hi</span>`}).
		Signal("page", widget.SignalFunc(func() (any, error) { return "/dashboard", nil })).
		SSE("/.events", "tick", "page").
		Build()

	r := router.New()
	tag := widget.Mount(r, &def)
	if !strings.Contains(tag, def.BootstrapPath) {
		t.Errorf("returned tag missing bootstrap path: %q", tag)
	}

	srv := httptest.NewServer(r)
	defer srv.Close()

	// /core-ui/widget/<name>/bootstrap.js
	resp, err := http.Get(srv.URL + def.BootstrapPath)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("bootstrap status: %v code=%d", err, resp.StatusCode)
	}
	body := readAll(t, resp)
	// Slot HTML is JSON-encoded into a JS string literal, so quotes
	// are backslash-escaped in the bootstrap output.
	for _, want := range []string{
		"window.__fui",
		`"name":"kiln-test"`,
		`"signal":"page"`,
		`fui-slot-header`,
		`<span class=\"hi\">hi</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("bootstrap missing %q", want)
		}
	}

	// /core-ui/widget/<name>/style.css
	resp, err = http.Get(srv.URL + def.StylePath)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("style status: %v code=%d", err, resp.StatusCode)
	}
	style := readAll(t, resp)
	for _, want := range []string{":root", ".fui-widget", ".fui-pos-bottom-right"} {
		if !strings.Contains(style, want) {
			t.Errorf("style missing %q", want)
		}
	}

	// /core-ui/widget/<name>/state
	resp, err = http.Get(srv.URL + def.StatePath)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("state status: %v code=%d", err, resp.StatusCode)
	}
	var state map[string]any
	if err := json.Unmarshal([]byte(readAll(t, resp)), &state); err != nil {
		t.Fatalf("state JSON: %v", err)
	}
	if state["page"] != "/dashboard" {
		t.Errorf("page signal value = %v, want /dashboard", state["page"])
	}
}

func TestMountWiresRPCEndpoint(t *testing.T) {
	called := false
	def := widget.New("demo").
		RPC("POST", "/demo/click", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.Write([]byte(`{"ok":true}`))
		})).
		Build()

	r := router.New()
	widget.Mount(r, &def)

	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/demo/click", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if !called {
		t.Errorf("RPC handler not invoked")
	}
}

// _ ensures preset+component compile-tested.
var _ component.Component = stubComponent{}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(buf)
}
