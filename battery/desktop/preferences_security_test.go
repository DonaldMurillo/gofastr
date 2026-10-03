package desktop_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// The null-value property from the 2026-09-06 red-probe round, on both
// write surfaces that reach the settings entry: JSON null is a shape
// no rendered control produces (encoding/json unmarshals it into any
// type with a nil error and leaves the zero value behind), so
// coercePreference refuses it like every other non-shape instead of
// letting it zero a stored preference. The in-process Preferences.Set
// path shares coercePreference and is covered by the same guard.

// prefSetRawSec calls preferences.set with one raw JSON value for key.
func prefSetRawSec(t *testing.T, h *desktoptest.Harness, key string, raw json.RawMessage) *desktoptest.CallResult {
	t.Helper()
	return h.Call("preferences", "set", map[string]any{
		"values": map[string]json.RawMessage{key: raw},
	})
}

// formSetRawSec posts one raw JSON value for key through the form route.
func formSetRawSec(t *testing.T, h *desktoptest.Harness, key string, raw json.RawMessage) *desktoptest.Response {
	t.Helper()
	body, err := json.Marshal(map[string]json.RawMessage{key: raw})
	if err != nil {
		t.Fatal(err)
	}
	return postRaw(t, h, "/__gofastr/desktop/preferences", string(body))
}

// postRaw sends a raw JSON body from the window's session client.
func postRaw(t *testing.T, h *desktoptest.Harness, path, body string) *desktoptest.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.URL(path), http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Body = http.NoBody
	if body != "" {
		req.Body = io.NopCloser(strings.NewReader(body))
		req.ContentLength = int64(len(body))
	}
	req.Header.Set("Content-Type", "application/json")
	return h.Do(req)
}

// TestPrefNullRefused: null must not quietly store the zero value.
func TestPrefNullRefused(t *testing.T) {
	app, d := prefsApp(t)
	h := desktoptest.Run(t, app, d)

	// Bool: default true; null must not quietly store false.
	prefSetRawSec(t, h, "notify_on_done", json.RawMessage(`null`)).AssertCode(t, desktop.CodeInvalidInput)
	if !d.Preferences().Bool("notify_on_done") {
		t.Fatal(`null zeroed notify_on_done (default true) through the capability`)
	}
	// String: default ""; null must be a refusal, not a stored "".
	prefSetRawSec(t, h, "export_folder", json.RawMessage(`null`)).AssertCode(t, desktop.CodeInvalidInput)
	res := formSetRawSec(t, h, "notify_on_done", json.RawMessage(`null`))
	if res.Status != http.StatusBadRequest {
		t.Fatalf("form notify_on_done=null = %d (%s), want 400", res.Status, res.Body)
	}
	if !d.Preferences().Bool("notify_on_done") {
		t.Fatal(`null zeroed notify_on_done (default true) through the form route`)
	}
	// Int: with a Min bound the range catches the zero; the refusal
	// must still be the coercion's, not an accident of the bound.
	prefSetRawSec(t, h, "work_minutes", json.RawMessage(`null`)).AssertCode(t, desktop.CodeInvalidInput)
	if res := formSetRawSec(t, h, "work_minutes", json.RawMessage(`null`)); res.Status != http.StatusBadRequest ||
		!strings.Contains(res.Body, "null") {
		t.Fatalf("form work_minutes=null = %d (%s), want 400 naming null", res.Status, res.Body)
	}
}
