package widget

import (
	"encoding/json"
	"math"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// A signal whose value has no JSON form (NaN) failed the whole encode
// after the headers went, so /state served an empty 200 and every
// signal kept its SSR value (U57). The bad signal is omitted, as one
// whose Read fails is, and the others still arrive.
func TestStateOmitsUnencodableSignal(t *testing.T) {
	r := router.New()
	def := New("nan-state").
		Signal("bad", SignalFunc(func() (any, error) { return math.NaN(), nil })).
		Signal("good", SignalFunc(func() (any, error) { return 7, nil })).
		Build()
	Mount(r, &def)
	// Signals are read in map order, so repeat the request until "bad"
	// has been read before "good" with near certainty.
	for range 20 {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/core-ui/widget/nan-state/state", nil))
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("/state body %q is not JSON: %v", w.Body.String(), err)
		}
		if got["good"] != float64(7) {
			t.Fatalf("good = %v, want 7", got["good"])
		}
		if _, ok := got["bad"]; ok {
			t.Fatalf("bad was served: %v", got["bad"])
		}
	}
}

// Mount registers the widget's routes, so a second Mount of one name on
// one router is a route conflict, which panics; the doc used to call it
// idempotent (U56).
func TestMountTwicePanics(t *testing.T) {
	r := router.New()
	def := New("twice").Signal("n", SignalFunc(func() (any, error) { return 1, nil })).Build()
	Mount(r, &def)
	defer func() {
		if recover() == nil {
			t.Error("a second Mount on the same router did not panic")
		}
	}()
	again := def
	Mount(r, &again)
}
