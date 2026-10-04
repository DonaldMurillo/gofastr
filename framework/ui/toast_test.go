package ui_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
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
		`data-cui-comp="ui-toast-stack"`,
		`data-cui-toast-stack="site-toasts"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("ToastSlot HTML missing %q\n--\n%s", want, html)
		}
	}
}

// The row a runtime toast is cloned from is this package's: the
// template carries the notification classes, the stack item class and
// the owned-style marker, and the slot ships it inside every stack so
// the headless-feedback module never names a class.
func TestToastTemplateCarriesTheKitSkin(t *testing.T) {
	tpl, ok := registry.Template(context.Background(), preset.ToastTemplate)
	if !ok {
		t.Fatal("framework/ui registers no toast row template")
	}
	for _, want := range []string{
		`<template data-hui-toast-dismiss-label="Dismiss: %s"`,
		`data-hui-toast-template=""`,
		`data-hui-toast-glyph-success="✓"`,
		`data-hui-toast-variant-danger="fui-notification--danger"`,
		`data-hui-toast-tone-warning="Warning"`,
		`class="fui-toast-stack__item" data-cui-internal="" data-hui-toast-item=""`,
		`class="fui-notification" data-cui-comp="ui-notification" data-hui-toast=""`,
		`class="fui-notification__title" data-cui-internal="" data-hui-toast-title=""`,
		`class="fui-notification__dismiss" data-cui-internal="" data-hui-toast-dismiss="" type="button">×</button>`,
	} {
		if !strings.Contains(string(tpl), want) {
			t.Errorf("toast template missing %q\n--\n%s", want, tpl)
		}
	}
	slot := string(ui.ToastSlot("site-toasts").Render())
	if !strings.Contains(slot, string(tpl)) {
		t.Errorf("ToastSlot does not ship the registered template:\n%s", slot)
	}
	if !strings.HasPrefix(slot, `<div data-cui-comp="ui-toast-stack" data-cui-toast-stack="site-toasts">`) {
		t.Errorf("the stack container carries more than the kernel name and the style id; the sheet keys on data-cui-comp:\n%s", slot)
	}
}
