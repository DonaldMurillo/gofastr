package framework

import (
	"net/http"
	"testing"
)

// A server wrapper stands in front of the setup wizard, which Start
// serves in the router's place and router middleware never sees.
func TestWrapServerCoversSetupWizard(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware(), WithSetup(&mockSetupRunnerSwappable{incomplete: true}))
	app.WrapServer(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Pass") == "" {
				http.Error(w, "gated", http.StatusTeapot)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	addr, stop := startOnRandomPort(t, app)
	defer stop()

	resp, err := http.Get("http://" + addr + "/setup")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("setup wizard answered %d without the wrapper; want 418", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/setup", nil)
	req.Header.Set("X-Pass", "1")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wrapped wizard answered %d; want 200", resp.StatusCode)
	}
}

// The first wrapper registered is the outermost.
func TestWrapServerOrder(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware())
	var order []string
	mark := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	app.WrapServer(mark("outer")).WrapServer(mark("inner"))
	addr, stop := startOnRandomPort(t, app)
	defer stop()
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(order) != 2 || order[0] != "outer" || order[1] != "inner" {
		t.Fatalf("wrapper order = %v; want [outer inner]", order)
	}
}

// A wrapper added once the server exists could never run, so it panics
// instead of being accepted and ignored.
func TestWrapServerAfterStartPanics(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware())
	_, stop := startOnRandomPort(t, app)
	defer stop()
	defer func() {
		if recover() == nil {
			t.Fatal("WrapServer after Start did not panic")
		}
	}()
	app.WrapServer(func(h http.Handler) http.Handler { return h })
}
