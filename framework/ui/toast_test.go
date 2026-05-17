package ui_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func TestAddToastSetsJSONArrayHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	ui.AddToast(rec, ui.ToastTrigger{
		Variant: ui.StatusSuccess,
		Title:   "Saved",
		Body:    "Your changes are persisted.",
		TTL:     5000,
	})
	h := rec.Header().Get("X-Gofastr-Toast")
	if h == "" {
		t.Fatal("expected X-Gofastr-Toast header to be set")
	}
	var arr []ui.ToastTrigger
	if err := json.Unmarshal([]byte(h), &arr); err != nil {
		t.Fatalf("header should be a JSON array: %v\ngot: %s", err, h)
	}
	if len(arr) != 1 || arr[0].Title != "Saved" || arr[0].Variant != ui.StatusSuccess || arr[0].TTL != 5000 {
		t.Errorf("payload mismatch: %+v", arr)
	}
}

func TestAddToastAccumulatesMultipleCalls(t *testing.T) {
	rec := httptest.NewRecorder()
	ui.AddToast(rec, ui.ToastTrigger{Title: "First", Variant: ui.StatusInfo})
	ui.AddToast(rec, ui.ToastTrigger{Title: "Second", Variant: ui.StatusDanger})
	ui.AddToast(rec, ui.ToastTrigger{Title: "Third"})
	h := rec.Header().Get("X-Gofastr-Toast")
	var arr []ui.ToastTrigger
	if err := json.Unmarshal([]byte(h), &arr); err != nil {
		t.Fatalf("invalid JSON: %v\nheader: %s", err, h)
	}
	if len(arr) != 3 {
		t.Fatalf("want 3 triggers, got %d: %+v", len(arr), arr)
	}
	if arr[0].Title != "First" || arr[1].Title != "Second" || arr[2].Title != "Third" {
		t.Errorf("order or content wrong: %+v", arr)
	}
	if arr[1].Variant != ui.StatusDanger {
		t.Errorf("variant on second toast: %s", arr[1].Variant)
	}
	if arr[2].Variant != ui.StatusInfo {
		t.Errorf("default variant should be info, got %s", arr[2].Variant)
	}
}

func TestAddToastIgnoresEmptyTitle(t *testing.T) {
	rec := httptest.NewRecorder()
	ui.AddToast(rec, ui.ToastTrigger{Title: ""})
	if rec.Header().Get("X-Gofastr-Toast") != "" {
		t.Error("empty-title trigger should not emit a header")
	}
}

func TestAddToastSuccessSetsHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	ui.AddToastSuccess(rec, "Saved", "All good.", 3000)
	h := rec.Header().Get("X-Gofastr-Toast")
	if !strings.Contains(h, `"variant":"success"`) ||
		!strings.Contains(h, `"title":"Saved"`) ||
		!strings.Contains(h, `"ttl":3000`) {
		t.Errorf("AddToastSuccess payload: %s", h)
	}
}

func TestAddToastErrorIsPersistent(t *testing.T) {
	rec := httptest.NewRecorder()
	ui.AddToastError(rec, "Upload failed", "Retry?")
	h := rec.Header().Get("X-Gofastr-Toast")
	if !strings.Contains(h, `"variant":"danger"`) {
		t.Errorf("expected danger variant: %s", h)
	}
	if strings.Contains(h, `"ttl"`) {
		t.Errorf("error toasts should be persistent (no ttl emitted): %s", h)
	}
}

func TestToastSlotRendersEmptyContainer(t *testing.T) {
	html := string(ui.ToastSlot("site-toasts").Render())
	for _, want := range []string{
		`data-fui-comp="ui-toast-stack"`,
		`data-fui-toast-stack="site-toasts"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("ToastSlot HTML missing %q\n--\n%s", want, html)
		}
	}
}
