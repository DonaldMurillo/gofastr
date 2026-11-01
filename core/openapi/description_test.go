package openapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

func TestSetDescription(t *testing.T) {
	s := NewSpec("Orders", "2.0.0")
	s.SetDescription("Order intake for the warehouse.")
	info := s.Build()["info"].(map[string]any)
	if info["description"] != "Order intake for the warehouse." || info["title"] != "Orders" {
		t.Fatalf("info = %v", info)
	}
	s.SetDescription("")
	if _, ok := s.Build()["info"].(map[string]any)["description"]; ok {
		t.Fatal("SetDescription(\"\") must remove the description")
	}
}

// The gated handler serves a per-request view; the description the host
// set on the spec must survive into it.
func TestRequestViewKeepsDescription(t *testing.T) {
	s := NewSpec("Orders", "2.0.0")
	s.SetDescription("Order intake.")
	s.RequestView = func(*http.Request) *Spec { return NewSpec("Orders", "2.0.0") }
	r := httptest.NewRequest("GET", "/openapi.json", nil)
	r = r.WithContext(handler.SetUser(r.Context(), "u1"))
	rec := httptest.NewRecorder()
	Handler(s).ServeHTTP(rec, r)
	var doc struct {
		Info map[string]any `json:"info"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Info["description"] != "Order intake." {
		t.Fatalf("served info = %v; want the description carried into the view", doc.Info)
	}
}
