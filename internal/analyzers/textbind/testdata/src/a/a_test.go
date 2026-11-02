package a

import (
	"net/http"
	"testing"
)

// A test drives the bind on purpose; _test.go files are skipped.
func TestBind(t *testing.T) {
	r, _ := http.NewRequest("GET", "/?q=a%00b", nil)
	db.QueryRowContext(r.Context(), "SELECT 1 WHERE $1 IS NOT NULL", r.URL.Query().Get("q"))
}
