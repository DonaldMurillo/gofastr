package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofastr/gofastr/core/router"
)

func TestAgentHTTPGetReturnsCurrentAndAvailable(t *testing.T) {
	store := NewAdapterStore(Adapter{}) // none
	r := router.New()
	mountAgentRoutes(r, store, nil)

	req := httptest.NewRequest(http.MethodGet, "/kiln/agent", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cur, _ := got["current"].(map[string]any)
	if cur["name"] != "none" {
		t.Errorf("current.name = %v, want none", cur["name"])
	}
	avail, _ := got["available"].([]any)
	if len(avail) != len(adapters) {
		t.Errorf("available count = %d, want %d", len(avail), len(adapters))
	}
	if got["in_flight"] != false {
		t.Errorf("in_flight = %v, want false", got["in_flight"])
	}
}

func TestAgentHTTPSetAcceptsCustomAndCancelsInflight(t *testing.T) {
	store := NewAdapterStore(Adapter{})
	r := router.New()
	mountAgentRoutes(r, store, nil)

	// Simulate an in-flight turn so we can verify cancellation fires.
	cancelled := make(chan error, 1)
	cancel := func(cause error) { cancelled <- cause }
	store.SetTurnCancel(cancel)
	if !store.InFlight() {
		t.Fatal("expected in_flight=true after SetTurnCancel")
	}

	body := bytes.NewBufferString(`{"name":"custom","custom":"echo hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/kiln/agent", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["ok"] != true {
		t.Errorf("ok = %v, want true: %s", got["ok"], rec.Body.String())
	}

	select {
	case cause := <-cancelled:
		if !errors.Is(cause, errAgentSwitched) {
			t.Errorf("cancel cause = %v, want errAgentSwitched", cause)
		}
	default:
		t.Fatal("Set() should have cancelled the in-flight turn")
	}

	// Store now points at the custom adapter.
	if got, _ := store.Get().BuildArgs("ping"), 0; got == nil {
		t.Errorf("custom adapter not active: BuildArgs returned nil")
	}
	if store.InFlight() {
		t.Errorf("in_flight should be false after Set")
	}
}

func TestAgentHTTPSetUnknownReturnsError(t *testing.T) {
	store := NewAdapterStore(Adapter{})
	r := router.New()
	mountAgentRoutes(r, store, nil)

	body := bytes.NewBufferString(`{"name":"nonexistent-binary-xyz"}`)
	req := httptest.NewRequest(http.MethodPost, "/kiln/agent", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["ok"] == true {
		t.Errorf("expected ok=false for unknown adapter, got %v", got)
	}
}
