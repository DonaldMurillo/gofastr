package app_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// leakyScreen reproduces the oatmeal-renders-as-Coffee bug: Load reads
// a route param, sets s.food if found, returns an error if not — and
// crucially DOES NOT reset s.food on entry. With the old single-instance
// model, a request for an unknown slug would inherit s.food from the
// previous successful render.
type leakyScreen struct {
	params map[string]string
	food   string
	loaded bool
}

func (s *leakyScreen) SetParams(p map[string]string) { s.params = p }
func (s *leakyScreen) Load(ctx context.Context) error {
	s.loaded = true
	slug := s.params["slug"]
	switch slug {
	case "coffee":
		s.food = "Coffee"
		return nil
	case "oatmeal":
		// Simulates "not found" — intentionally does NOT reset s.food.
		return nil
	default:
		return nil
	}
}
func (s *leakyScreen) Render() render.HTML {
	return render.HTML("<p>" + s.food + "</p>")
}

// TestScreen_PerRequestInstance_NoStateLeak pins the fix for the
// screen-state-leak footgun (V3 feedback P0 #2). Two sequential
// requests with different params must NOT see each other's mutations.
//
// Without per-request instancing, request 2 inherits s.food from
// request 1 because Load forgot to reset it.
func TestScreen_PerRequestInstance_NoStateLeak(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(app.NewScreen("/foods/:slug", &leakyScreen{}), nil)

	// Request 1: /foods/coffee — sets s.food = "Coffee".
	res1, err := a.RenderPageResult(context.Background(), "/foods/coffee")
	if err != nil {
		t.Fatalf("req1 err: %v", err)
	}
	if got := string(res1.HTML); !contains(got, "<p>Coffee</p>") {
		t.Fatalf("req1: want 'Coffee' rendered, got: %s", got)
	}

	// Request 2: /foods/oatmeal — Load doesn't set s.food. With state leak,
	// the page still renders "Coffee" (the previous request's value).
	// With per-request instancing, s.food starts zero ⇒ empty paragraph.
	res2, err := a.RenderPageResult(context.Background(), "/foods/oatmeal")
	if err != nil {
		t.Fatalf("req2 err: %v", err)
	}
	if got := string(res2.HTML); contains(got, "<p>Coffee</p>") {
		t.Fatalf("req2: STATE LEAK — oatmeal page rendered previous request's Coffee: %s", got)
	}
	if got := string(res2.HTML); !contains(got, "<p></p>") {
		t.Fatalf("req2: want empty paragraph (fresh zero-valued instance), got: %s", got)
	}
}

// TestScreen_PerRequestInstance_ParallelIsolation goes one step further:
// concurrent renders with different params must each see their OWN
// param + load result. With a shared instance, parallel SetParams calls
// race and one request can render the other request's data.
func TestScreen_PerRequestInstance_ParallelIsolation(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(app.NewScreen("/foods/:slug", &echoSlugScreen{}), nil)

	const N = 50
	var wg sync.WaitGroup
	var mismatches int32
	for i := 0; i < N; i++ {
		slug := fmt.Sprintf("food-%d", i)
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			res, err := a.RenderPageResult(context.Background(), "/foods/"+s)
			if err != nil {
				t.Errorf("err for %s: %v", s, err)
				return
			}
			want := "<p>" + s + "</p>"
			if !contains(string(res.HTML), want) {
				atomic.AddInt32(&mismatches, 1)
			}
		}(slug)
	}
	wg.Wait()
	if m := atomic.LoadInt32(&mismatches); m > 0 {
		t.Fatalf("%d/%d parallel renders saw a slug other than the one they requested", m, N)
	}
}

// echoSlugScreen renders whatever slug Load received. With a shared
// instance under concurrency, the rendered slug can drift from the
// requested one because SetParams + Load + Render are interleaved.
type echoSlugScreen struct {
	params map[string]string
	slug   string
}

func (s *echoSlugScreen) SetParams(p map[string]string)  { s.params = p }
func (s *echoSlugScreen) Load(ctx context.Context) error { s.slug = s.params["slug"]; return nil }
func (s *echoSlugScreen) Render() render.HTML            { return render.HTML("<p>" + s.slug + "</p>") }

// TestScreen_PerRequestInstance_PreservesConstructionFields guards a
// regression risk in the per-request-instance fix: shallow-copy from
// the registered template means fields populated at construction
// (e.g. embedded constants, configured values) MUST survive into the
// per-request copy. Otherwise we trade a state-leak bug for a
// "fields go missing" bug.
func TestScreen_PerRequestInstance_PreservesConstructionFields(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(app.NewScreen("/cfg", &configuredScreen{Greeting: "hello"}), nil)

	res, err := a.RenderPageResult(context.Background(), "/cfg")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !contains(string(res.HTML), "hello") {
		t.Fatalf("construction-time field lost: %s", string(res.HTML))
	}
}

type configuredScreen struct {
	Greeting string
}

func (s *configuredScreen) Render() render.HTML { return render.HTML("<p>" + s.Greeting + "</p>") }

// TestScreen_PerRequestInstance_RenderPartialAlsoIsolated pins that
// the partial-render path (used for client-side nav swaps) shares the
// same per-request guarantee — otherwise the bug returns for SPA
// navigation even if full-page renders are fixed.
func TestScreen_PerRequestInstance_RenderPartialAlsoIsolated(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(app.NewScreen("/foods/:slug", &leakyScreen{}), nil)

	_, err := a.RenderPartialResult(context.Background(), "/foods/coffee")
	if err != nil {
		t.Fatalf("partial req1: %v", err)
	}
	res2, err := a.RenderPartialResult(context.Background(), "/foods/oatmeal")
	if err != nil {
		t.Fatalf("partial req2: %v", err)
	}
	if contains(string(res2.HTML), "Coffee") {
		t.Fatalf("partial: STATE LEAK — oatmeal saw previous Coffee: %s", string(res2.HTML))
	}
}
