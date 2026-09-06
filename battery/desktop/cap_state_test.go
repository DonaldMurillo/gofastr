package desktop_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// The state capability through the harness: the page's durable store,
// confined to page. keys, cross-window state_changed events,
// persistence across a whole Run, and the manifest/bridge surface.

// stateGet is the get result shape.
func stateGet(t *testing.T, h *desktoptest.Harness, key string) json.RawMessage {
	t.Helper()
	var got struct {
		Value json.RawMessage `json:"value"`
	}
	h.Call("state", "get", map[string]any{"key": key}).MustResult(t, &got)
	return got.Value
}

func TestStateRoundTripThroughPage(t *testing.T) {
	app, d := wsApp(t, desktop.Config{Title: "S"}, t.TempDir(), nil)
	h := desktoptest.Run(t, app, d)

	h.Call("state", "set", map[string]any{"key": "page.demo", "value": map[string]any{"n": 1}}).AssertOK(t)
	if got := stateGet(t, h, "page.demo"); string(got) != `{"n":1}` {
		t.Fatalf("get page.demo = %s, want {\"n\":1}", got)
	}

	// A set without a value stores JSON null, distinct from absent.
	h.Call("state", "set", map[string]any{"key": "page.nil"}).AssertOK(t)
	if got := stateGet(t, h, "page.nil"); string(got) != "null" {
		t.Fatalf("get page.nil = %s, want null", got)
	}

	// keys lists page keys, filtered by prefix.
	var keys struct {
		Keys []string `json:"keys"`
	}
	h.Call("state", "keys", map[string]any{"prefix": "demo"}).MustResult(t, &keys)
	if len(keys.Keys) != 1 || keys.Keys[0] != "page.demo" {
		t.Fatalf("keys(demo) = %v, want [page.demo]", keys.Keys)
	}

	// delete removes it; get answers null afterwards.
	h.Call("state", "delete", map[string]any{"key": "page.demo"}).AssertOK(t)
	if got := stateGet(t, h, "page.demo"); string(got) != "null" {
		t.Fatalf("get after delete = %s, want null", got)
	}
	h.Call("state", "keys", map[string]any{"prefix": ""}).MustResult(t, &keys)
	if len(keys.Keys) != 1 || keys.Keys[0] != "page.nil" {
		t.Fatalf("keys() after delete = %v, want [page.nil]", keys.Keys)
	}
}

func TestStateKeysConfinedToPagePrefix(t *testing.T) {
	app, d := wsApp(t, desktop.Config{Title: "S"}, t.TempDir(), nil)
	h := desktoptest.Run(t, app, d)

	// The battery's own keys and everything else outside page. are
	// refused on every method; "page" without the dot too.
	for _, key := range []string{"windows", "settings", "main", "page", "Page.demo", "page.Demo", "other.x"} {
		h.Call("state", "set", map[string]any{"key": key, "value": 1}).AssertCode(t, desktop.CodeInvalidInput)
		h.Call("state", "get", map[string]any{"key": key}).AssertCode(t, desktop.CodeInvalidInput)
		h.Call("state", "delete", map[string]any{"key": key}).AssertCode(t, desktop.CodeInvalidInput)
	}

	// keys never surfaces a non-page key.
	h.Call("state", "set", map[string]any{"key": "page.demo", "value": 1}).AssertOK(t)
	var keys struct {
		Keys []string `json:"keys"`
	}
	h.Call("state", "keys", map[string]any{"prefix": ""}).MustResult(t, &keys)
	if len(keys.Keys) != 1 || keys.Keys[0] != "page.demo" {
		t.Fatalf("keys() = %v, want only page.demo", keys.Keys)
	}
}

func TestStateCapsValueSize(t *testing.T) {
	app, d := wsApp(t, desktop.Config{Title: "S"}, t.TempDir(), nil)
	h := desktoptest.Run(t, app, d)

	// The cap is measured on the COMPACT JSON of the value: {"t":"aaa"}
	// is 8 bytes of wrapper plus the string (the windows.post rule).
	atCap := strings.Repeat("a", (64<<10)-len(`{"t":""}`))
	h.Call("state", "set", map[string]any{"key": "page.atcap", "value": map[string]any{"t": atCap}}).AssertOK(t)
	overCap := strings.Repeat("a", (64<<10)-len(`{"t":""}`)+1)
	h.Call("state", "set", map[string]any{"key": "page.over", "value": map[string]any{"t": overCap}}).
		AssertCode(t, desktop.CodeInvalidInput)
	if got := stateGet(t, h, "page.over"); string(got) != "null" {
		t.Fatalf("an oversize value was stored: %s", got)
	}
}

func TestStateStrangerRefused(t *testing.T) {
	app, d := wsApp(t, desktop.Config{Title: "S"}, t.TempDir(), nil)
	h := desktoptest.Run(t, app, d)

	req, err := http.NewRequest(http.MethodPost, h.URL("/__gofastr/desktop/call/state/get"),
		strings.NewReader(`{"key":"page.demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.Stranger().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stranger state call: %d, want 403", resp.StatusCode)
	}
}

func TestStateChangedReachesSecondWindow(t *testing.T) {
	app, d := wsApp(t, desktop.Config{Title: "S"}, t.TempDir(), nil)
	h := desktoptest.Run(t, app, d)
	w2 := openPath(t, h, "/two")

	h.Call("state", "set", map[string]any{"key": "page.sync", "value": true}).AssertOK(t)
	ev := h.WaitEvent("state_changed")
	var p struct {
		Key string `json:"key"`
	}
	if err := ev.Unmarshal(&p); err != nil || p.Key != "page.sync" {
		t.Fatalf("state_changed payload = %s (err %v)", ev.Payload, err)
	}
	got := eventsNamed(h.Window(w2).Events(), "state_changed")
	if len(got) != 1 || string(got[0].Payload) != `{"key":"page.sync"}` {
		t.Fatalf("w2 state_changed events = %+v", got)
	}

	// delete announces itself too.
	h.Call("state", "delete", map[string]any{"key": "page.sync"}).AssertOK(t)
	ev = h.WaitEvent("state_changed")
	if err := ev.Unmarshal(&p); err != nil || p.Key != "page.sync" {
		t.Fatalf("delete state_changed payload = %s (err %v)", ev.Payload, err)
	}
}

func TestStateSurvivesSecondBattery(t *testing.T) {
	dir := t.TempDir()
	app, d := wsApp(t, desktop.Config{Title: "S"}, dir, nil)
	h := desktoptest.Run(t, app, d)
	h.Call("state", "set", map[string]any{"key": "page.keep", "value": map[string]any{"n": 7}}).AssertOK(t)
	// Quit before the 500 ms debounce: the quit flush must carry it.
	if err := h.Quit(); err != nil {
		t.Fatalf("first run: %v", err)
	}

	fi, err := os.Stat(filepath.Join(dir, "windowstate.test", "state.json"))
	if err != nil {
		t.Fatalf("state.json after quit: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("state.json mode = %v, want 0600", fi.Mode().Perm())
	}

	app2, d2 := wsApp(t, desktop.Config{Title: "S"}, dir, nil)
	h2 := desktoptest.Run(t, app2, d2)
	if got := stateGet(t, h2, "page.keep"); string(got) != `{"n":7}` {
		t.Fatalf("get page.keep after relaunch = %s, want {\"n\":7}", got)
	}
}

func TestStateAccessorSharesTheStores(t *testing.T) {
	app, d := wsApp(t, desktop.Config{Title: "S"}, t.TempDir(), nil)
	if d.State() != nil {
		t.Fatal("State() before Run must be nil")
	}
	h := desktoptest.Run(t, app, d)
	if d.State() == nil {
		t.Fatal("State() nil after Run")
	}
	// The Go side and the page side see one store.
	if err := d.State().Set("page.go", 5); err != nil {
		t.Fatal(err)
	}
	if got := stateGet(t, h, "page.go"); string(got) != "5" {
		t.Fatalf("get page.go = %s, want 5", got)
	}
}

func TestStateInManifestAndBridge(t *testing.T) {
	app, d := wsApp(t, desktop.Config{Title: "S"}, t.TempDir(), nil)
	h := desktoptest.Run(t, app, d)

	m := h.Manifest()
	var cap *desktop.CapabilityInfo
	for i := range m.Capabilities {
		if m.Capabilities[i].Name == "state" {
			cap = &m.Capabilities[i]
		}
	}
	if cap == nil {
		var names []string
		for _, c := range m.Capabilities {
			names = append(names, c.Name)
		}
		t.Fatalf("manifest capabilities = %v, want a state entry", names)
	}
	var methods []string
	for _, mm := range cap.Methods {
		methods = append(methods, mm.Name)
		if mm.Permission != "" {
			t.Fatalf("state.%s is gated (%q); it must be ungated like window.setPath", mm.Name, mm.Permission)
		}
	}
	if strings.Join(methods, ",") != "delete,get,keys,set" {
		t.Fatalf("state methods = %v, want [delete get keys set]", methods)
	}

	// The generated bridge carries the typed namespace.
	js := h.Get("/__gofastr/desktop/bridge.js").AssertStatus(t, http.StatusOK).Body
	for _, want := range []string{
		`D["state"]`,
		`"get": (input) => D.call("state", "get", input)`,
		`"set": (input) => D.call("state", "set", input)`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("generated bridge missing %q:\n%s", want, js)
		}
	}
}
