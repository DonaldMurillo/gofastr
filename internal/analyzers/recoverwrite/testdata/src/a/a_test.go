package a

import (
	"net/http"
	"testing"
)

// A test's recover asserts the panic; it answers no client.
func TestPanics(t *testing.T) {
	var w http.ResponseWriter
	defer func() {
		if recover() != nil {
			http.Error(w, "x", http.StatusInternalServerError)
		}
	}()
}
