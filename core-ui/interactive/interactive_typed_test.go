package interactive

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// rawBtn renders a <button> with sorted attrs (the way ui.Button /
// render.Tag emit), so these tests can prove the typed wrappers produce
// byte-identical output to a hand-written attribute map.
func rawBtn(attrs map[string]string, label string) render.HTML {
	if attrs == nil {
		return render.Tag("button", nil, render.Text(label))
	}
	return render.Tag("button", attrs, render.Text(label))
}

// ─── WithBody / Attrs ───────────────────────────────────────────────

func TestWithBodyEmitsAttr(t *testing.T) {
	attrs := Post("/api/x").WithBody(`{"id":42}`).Attrs()
	if attrs["data-fui-rpc-body"] != `{"id":42}` {
		t.Errorf("data-fui-rpc-body = %q, want {\"id\":42}", attrs["data-fui-rpc-body"])
	}
}

func TestWithBodyRejectsInvalidJSON(t *testing.T) {
	defer expectPanic(t, "invalid JSON body should panic")
	Post("/api/x").WithBody(`not json`)
}

func TestWithBodyAcceptsObject(t *testing.T) {
	// An empty object is valid JSON.
	if got := Post("/api/x").WithBody(`{}`).Attrs()["data-fui-rpc-body"]; got != `{}` {
		t.Errorf("empty-object body = %q, want {}", got)
	}
}

func TestAttrsContainsRPCAndMethod(t *testing.T) {
	attrs := Delete("/api/d/1").WithConfirm("sure?").OnSuccess(SetSignal("rows")).Attrs()
	want := map[string]string{
		"data-fui-rpc":        "/api/d/1",
		"data-fui-rpc-method": "DELETE",
		"data-fui-confirm":    "sure?",
		"data-fui-rpc-signal": "rows",
	}
	for k, v := range want {
		if attrs[k] != v {
			t.Errorf("Attrs()[%q] = %q, want %q", k, attrs[k], v)
		}
	}
}

func TestAttrsIsCopy(t *testing.T) {
	a := Post("/api/x")
	m := a.Attrs()
	m["data-fui-rpc"] = "/mutated"
	if a.Attrs()["data-fui-rpc"] != "/api/x" {
		t.Error("Attrs() returned the live map, not a copy")
	}
}

// ─── OpenOnClick ────────────────────────────────────────────────────

func TestOpenOnClickEmitsAttr(t *testing.T) {
	got := string(OpenOnClick(rawBtn(nil, "Open"), "site-modal"))
	if !strings.Contains(got, `data-fui-open="site-modal"`) {
		t.Errorf("missing data-fui-open: %s", got)
	}
}

func TestOpenOnClickByteIdenticalToMap(t *testing.T) {
	// The wrapper appends data-fui-open after the tag's existing attrs.
	// On a classless button that is also the sorted position.
	got := string(OpenOnClick(rawBtn(nil, "Open"), "m"))
	want := string(rawBtn(map[string]string{"data-fui-open": "m"}, "Open"))
	if got != want {
		t.Errorf("OpenOnClick not byte-identical to map:\n got: %s\nwant: %s", got, want)
	}
}

// ─── ToastOnClick ───────────────────────────────────────────────────

func TestToastOnClickMatchesHandWrittenLiteral(t *testing.T) {
	// The exact toast the site demos hand-write today
	// (examples/site/components.go). The typed wrapper must render
	// byte-identically to the raw attribute map.
	json := `{"variant":"success","title":"Saved","body":"Triggered from JS, no round-trip.","ttl":5000}`
	got := string(ToastOnClick(rawBtn(nil, "Client: success"), Toast{
		Variant: "success",
		Title:   "Saved",
		Body:    "Triggered from JS, no round-trip.",
		TTLMs:   5000,
	}))
	want := string(rawBtn(map[string]string{"data-fui-toast": json}, "Client: success"))
	if got != want {
		t.Errorf("toast not byte-identical to hand-written literal:\n got: %s\nwant: %s", got, want)
	}
}

func TestToastOnClickOmitsZeroFields(t *testing.T) {
	got := string(ToastOnClick(rawBtn(nil, "X"), Toast{Title: "only-title"}))
	if !strings.Contains(got, `data-fui-toast="{&quot;title&quot;:&quot;only-title&quot;}"`) {
		t.Errorf("zero fields not omitted: %s", got)
	}
}

func TestToastOnClickIncludesStack(t *testing.T) {
	got := string(ToastOnClick(rawBtn(nil, "X"), Toast{Title: "t", Stack: "alerts", TTLMs: 1000}))
	// stack precedes ttl per struct field order.
	if !strings.Contains(got, `&quot;stack&quot;:&quot;alerts&quot;,&quot;ttl&quot;:1000`) {
		t.Errorf("stack/ttl order wrong: %s", got)
	}
}

// ─── Pane triggers ──────────────────────────────────────────────────

func TestOpenPaneOnClickEmitsAttr(t *testing.T) {
	got := string(OpenPaneOnClick(rawBtn(nil, "Open details"), "secondary"))
	if !strings.Contains(got, `data-fui-pane-open="secondary"`) {
		t.Errorf("missing data-fui-pane-open: %s", got)
	}
}

func TestOpenPaneOnClickRejectsBadPane(t *testing.T) {
	defer expectPanic(t, "bad pane name should panic")
	OpenPaneOnClick(rawBtn(nil, "x"), "primary")
}

func TestClosePaneOnClickEmitsAttr(t *testing.T) {
	got := string(ClosePaneOnClick(rawBtn(nil, "Close"), "secondary"))
	if !strings.Contains(got, `data-fui-pane-close="secondary"`) {
		t.Errorf("missing data-fui-pane-close: %s", got)
	}
}

func TestClosePaneOnClickEmptyClosesTopmost(t *testing.T) {
	got := string(ClosePaneOnClick(rawBtn(nil, "Close"), ""))
	// Empty value → bare-value attribute form, matching the site demo.
	if !strings.Contains(got, `data-fui-pane-close=""`) {
		t.Errorf("empty pane-close should emit data-fui-pane-close=\"\": %s", got)
	}
}

func TestClosePaneOnClickRejectsBadPane(t *testing.T) {
	defer expectPanic(t, "bad pane name should panic")
	ClosePaneOnClick(rawBtn(nil, "x"), "fourth")
}

// ─── Signal display bindings ────────────────────────────────────────

func TestBindHTMLByteIdenticalToSortedMap(t *testing.T) {
	inner := render.Tag("div", nil, render.Text("list"))
	got := string(BindHTML(inner, "opt-create-list"))
	want := string(render.Tag("div", map[string]string{
		"data-fui-signal":      "opt-create-list",
		"data-fui-signal-mode": "html",
	}, render.Text("list")))
	if got != want {
		t.Errorf("BindHTML not byte-identical to sorted map:\n got: %s\nwant: %s", got, want)
	}
}

func TestBindTextByteIdenticalToSortedMap(t *testing.T) {
	inner := render.Tag("span", map[string]string{"class": "demo-signal-out"}, render.Text("0"))
	got := string(BindText(inner, "demo-counter"))
	want := string(render.Tag("span", map[string]string{
		"class":                "demo-signal-out",
		"data-fui-signal":      "demo-counter",
		"data-fui-signal-mode": "text",
	}, render.Text("0")))
	if got != want {
		t.Errorf("BindText not byte-identical to sorted map:\n got: %s\nwant: %s", got, want)
	}
}

func TestBindAttrByteIdenticalToSortedMap(t *testing.T) {
	inner := render.Tag("span", nil, render.Text(""))
	got := string(BindAttr(inner, "tab-active", "data-active"))
	want := string(render.Tag("span", map[string]string{
		"data-fui-signal":      "tab-active",
		"data-fui-signal-attr": "data-active",
		"data-fui-signal-mode": "attr",
	}, render.Text("")))
	if got != want {
		t.Errorf("BindAttr not byte-identical to sorted map:\n got: %s\nwant: %s", got, want)
	}
}

// expectPanic fails the test if the surrounding code does not panic.
func expectPanic(t *testing.T, msg string) {
	t.Helper()
	if r := recover(); r == nil {
		t.Fatalf("expected panic: %s", msg)
	}
}
