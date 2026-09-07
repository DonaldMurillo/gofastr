package ui_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// Pins: AddToast drops an entry whose Title/Body/Stack carries a C0 or
// DEL byte before marshalling — per-entry drop mirroring
// InvalidateScreens, never the whole accumulated header.
// Property: outbound header values reject C0/DEL — screen_cache.go::InvalidateScreens
// documents AND implements the exact rule ("DEL survives JSON encoding un-escaped, and
// an invalid header value is silently dropped by Go's HTTP/2 writer"); AddToast lacks it.
// Surfaces: framework/ui/toast.go::AddToast (json.Marshal of Title/Body/Stack into the
// X-Gofastr-Toast header).
// Finding: AddToast marshals trigger titles verbatim. encoding/json does not escape
// DEL, so a \x7f-bearing title lands as a raw byte in the header value; the value is
// then an invalid header value that Go's HTTP/2 writer silently drops, losing the WHOLE
// accumulated header — every toast in the response, not just the poisoned one. C0 bytes
// (\x0b) round-trip \u-escaped on the wire but still decode back into the client-side
// toast text.
// Fix direction: drop the entry carrying a C0/DEL byte in Title/Body/Stack before
// marshalling (per-entry drop, mirroring InvalidateScreens), never the whole header.
func TestToastCtlBytesDropped(t *testing.T) {
	for _, tc := range []struct {
		name  string
		title string
		bad   byte
	}{
		{"DEL", "Bad \x7f title", 0x7f},
		{"VT", "Bad \x0b title", 0x0b},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ui.AddToast(rec, ui.ToastTrigger{Title: "Saved"})
			ui.AddToast(rec, ui.ToastTrigger{Title: tc.title})
			raw := rec.Header().Get("X-Gofastr-Toast")
			if raw == "" {
				t.Fatal("setup broken: X-Gofastr-Toast empty after two AddToast calls")
			}
			// Wire-level: the header value itself must be a valid header
			// value — no raw C0/DEL byte survives into it.
			if i := toastCtlRawByte(raw); i >= 0 {
				t.Errorf("SECURITY: [toast-ctl-bytes] header value carries a raw control byte 0x%02x — an invalid header value that Go's HTTP/2 writer silently drops, losing every toast in the response: %q", raw[i], raw)
			}
			// Parse-level: the accumulated value must still be the JSON
			// array shape the runtime scans for.
			var list []map[string]any
			if err := json.Unmarshal([]byte(raw), &list); err != nil {
				t.Errorf("SECURITY: [toast-ctl-bytes] accumulated header is not valid JSON: %v (raw: %q)", err, raw)
				return
			}
			saved := false
			for _, e := range list {
				title, _ := e["title"].(string)
				if title == "Saved" {
					saved = true
				}
				if strings.IndexByte(title, tc.bad) >= 0 {
					t.Errorf("SECURITY: [toast-ctl-bytes] control byte 0x%02x survived into toast title %q instead of that entry being dropped", tc.bad, title)
				}
			}
			// Per-entry drop like InvalidateScreens: the clean toast
			// must survive alongside the poisoned one.
			if !saved {
				t.Errorf("SECURITY: [toast-ctl-bytes] the clean toast was lost alongside the poisoned one (per-entry drop, like InvalidateScreens): %q", raw)
			}
		})
	}
}

// toastCtlRawByte returns the index of the first raw C0 control byte or DEL
// in s, or -1 when s is header-value clean (same rule as screen_cache.go's
// hasCtl, which lives in package ui and is not reachable from ui_test).
func toastCtlRawByte(s string) int {
	for i, b := range s {
		if b < 0x20 || b == 0x7f {
			return i
		}
	}
	return -1
}

// TestToastReparseDropsControlByteEntries pins the re-parse half of the
// rule: AddToast tolerates a header a previous (manual) caller set
// directly, and re-marshalling that value must not carry a control-byte
// entry into this response's whole accumulated header.
func TestToastReparseDropsControlByteEntries(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"array", `[{"variant":"info","title":"Bad \u000b title"}]`},
		{"single-object", `{"variant":"info","title":"Bad \u007f title"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			rec.Header().Set("X-Gofastr-Toast", tc.raw)
			ui.AddToast(rec, ui.ToastTrigger{Title: "Saved"})
			got := rec.Header().Get("X-Gofastr-Toast")
			if i := toastCtlRawByte(got); i >= 0 {
				t.Errorf("SECURITY: [toast-ctl-bytes] re-marshalled header carries a raw control byte 0x%02x from a manually-set value — every toast in the response is lost when the HTTP/2 writer drops the header: %q", got[i], got)
			}
			var list []map[string]any
			if err := json.Unmarshal([]byte(got), &list); err != nil {
				t.Fatalf("re-marshalled header is not a JSON array: %v (%q)", err, got)
			}
			if len(list) != 1 || list[0]["title"] != "Saved" {
				t.Errorf("SECURITY: [toast-ctl-bytes] poisoned manual entry survived the re-parse (want only the clean \"Saved\" toast): %q", got)
			}
		})
	}
}
