package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestCatalogItemHTTPStatus(t *testing.T) {
	for _, tt := range []struct {
		id     string
		status int
		body   string
	}{
		{"no-such-item", http.StatusNotFound, "No such product"},
		{"kettle", http.StatusOK, "Pour-over kettle"},
	} {
		t.Run(tt.id, func(t *testing.T) {
			rec := serve(t, http.MethodGet, "/examples/catalog/"+tt.id)
			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
			if !strings.Contains(rec.Body.String(), tt.body) {
				t.Errorf("body missing %q", tt.body)
			}
		})
	}
}
