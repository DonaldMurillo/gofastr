package framework

import (
	"errors"
	"net/http"
	"testing"
)

func TestResponseCreationErrors(t *testing.T) {
	harness := TestHarness(t, NewApp(WithoutDefaultMiddleware()))
	responses := map[string]*TestResponse{
		"post":    harness.Post("/x", make(chan int)),
		"builder": harness.Request(http.MethodPut, "/x", nil).WithBody(func() {}).Execute(),
	}
	for name, response := range responses {
		t.Run(name, func(t *testing.T) {
			if response.err == nil {
				t.Fatal("request unexpectedly succeeded")
			}
			for helper, assert := range map[string]func(testing.TB){
				"header": func(tb testing.TB) { response.AssertHeader(tb, "X-Test", "value") },
				"body":   func(tb testing.TB) { response.AssertBodyContains(tb, "value") },
			} {
				t.Run(helper, func(t *testing.T) {
					var panicked any
					failed := cov_runFail(func(tb testing.TB) {
						defer func() { panicked = recover() }()
						assert(tb)
					})
					if panicked != nil || !failed {
						t.Fatalf("assertion failed=%v, panic=%v; want a clean test failure", failed, panicked)
					}
				})
			}
			t.Run("json", func(t *testing.T) {
				defer func() {
					if panicked := recover(); panicked != nil {
						t.Fatalf("JSON panicked: %v", panicked)
					}
				}()
				var decoded struct{}
				if err := response.JSON(&decoded); !errors.Is(err, response.err) {
					t.Fatalf("JSON error=%v, want creation error %v", err, response.err)
				}
			})
		})
	}
}
