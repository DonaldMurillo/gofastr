package framework

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/featureflag"
)

// Flags and SetFlagStore install the App's evaluator as the process-wide
// default, so with two Apps in one process featureflag.Bool(ctx, ...) in
// the first App's handler read the second App's store (2026-10-27 S34).
// The App puts its own evaluator on every request context, and an App
// with no evaluator yet stamps none, so neither reads the other's.
func TestFlagsFollowTheRequestsApp(t *testing.T) {
	storeA := featureflag.NewMemoryStore()
	if err := storeA.Set(featureflag.Flag{Key: "beta", Enabled: true, Rollout: 100}); err != nil {
		t.Fatal(err)
	}
	a := NewApp().SetFlagStore(storeA)
	b := NewApp().SetFlagStore(featureflag.NewMemoryStore())
	bare := NewApp()
	t.Cleanup(func() { featureflag.SetDefault(nil) })
	for _, tc := range []struct {
		name string
		app  *App
		want bool
	}{{"first app", a, true}, {"second app", b, false}, {"app without flags", bare, false}} {
		t.Run(tc.name, func(t *testing.T) {
			// The process default names a's evaluator for the bare app's
			// case, so a fallback to it would read true.
			featureflag.SetDefault(a.Flags())
			var got, saw bool
			tc.app.Router().Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got, saw = featureflag.Bool(r.Context(), "beta"), true
			}))
			if tc.app == a {
				featureflag.SetDefault(b.Flags())
			}
			tc.app.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/probe", nil))
			if !saw || got != tc.want {
				t.Fatalf("featureflag.Bool = %v (ran %v), want %v", got, saw, tc.want)
			}
		})
	}
}
