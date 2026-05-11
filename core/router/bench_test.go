package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkRouter_Lookup measures the cost of matching a request to a
// handler for varying route-table sizes. The comparison axis is N
// registered routes; the hit is always the last registered route (worst
// case for a linear matcher, neutral for a tree).
//
// Comparison: a baseline `http.ServeMux` benchmark sits alongside so the
// overhead vs the stdlib is visible.
func BenchmarkRouter_Lookup(b *testing.B) {
	noop := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	sizes := []int{1, 10, 100, 1000}

	for _, n := range sizes {
		n := n
		b.Run(fmt.Sprintf("gofastr-static-N=%d", n), func(b *testing.B) {
			r := New()
			for i := 0; i < n; i++ {
				r.GetFunc(fmt.Sprintf("/route-%d", i), noop)
			}
			target := fmt.Sprintf("/route-%d", n-1)
			req := httptest.NewRequest(http.MethodGet, target, nil)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
			}
		})

		b.Run(fmt.Sprintf("gofastr-param-N=%d", n), func(b *testing.B) {
			r := New()
			for i := 0; i < n; i++ {
				r.GetFunc(fmt.Sprintf("/users/{id}/route-%d", i), noop)
			}
			target := fmt.Sprintf("/users/42/route-%d", n-1)
			req := httptest.NewRequest(http.MethodGet, target, nil)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
			}
		})

		b.Run(fmt.Sprintf("servemux-static-N=%d", n), func(b *testing.B) {
			mux := http.NewServeMux()
			for i := 0; i < n; i++ {
				mux.HandleFunc(fmt.Sprintf("/route-%d", i), noop)
			}
			target := fmt.Sprintf("/route-%d", n-1)
			req := httptest.NewRequest(http.MethodGet, target, nil)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
			}
		})
	}
}
