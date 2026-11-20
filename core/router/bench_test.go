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
		// The gofastr axis: static literal routes and parameterized
		// routes, both hit on the last registered route.
		variants := []struct {
			name     string
			register func(*Router)
			target   string
		}{
			{
				name: "gofastr-static",
				register: func(r *Router) {
					for i := range n {
						r.GetFunc(fmt.Sprintf("/route-%d", i), noop)
					}
				},
				target: fmt.Sprintf("/route-%d", n-1),
			},
			{
				name: "gofastr-param",
				register: func(r *Router) {
					for i := range n {
						r.GetFunc(fmt.Sprintf("/users/{id}/route-%d", i), noop)
					}
				},
				target: fmt.Sprintf("/users/42/route-%d", n-1),
			},
		}
		for _, v := range variants {
			b.Run(fmt.Sprintf("%s-N=%d", v.name, n), func(b *testing.B) {
				r := New()
				v.register(r)
				req := httptest.NewRequest(http.MethodGet, v.target, nil)
				b.ResetTimer()
				for range b.N {
					rec := httptest.NewRecorder()
					r.ServeHTTP(rec, req)
				}
			})
		}

		// Baseline: stdlib ServeMux, static routes only.
		b.Run(fmt.Sprintf("servemux-static-N=%d", n), func(b *testing.B) {
			mux := http.NewServeMux()
			for i := range n {
				mux.HandleFunc(fmt.Sprintf("/route-%d", i), noop)
			}
			target := fmt.Sprintf("/route-%d", n-1)
			req := httptest.NewRequest(http.MethodGet, target, nil)
			b.ResetTimer()
			for range b.N {
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
			}
		})
	}
}
