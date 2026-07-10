package setup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// helperBuildRunner creates a Runner with the given steps and a simple
// Complete predicate that returns the provided *bool.
func helperBuildRunner(t *testing.T, steps []Step, completeVal *bool) *Runner {
	t.Helper()
	return New(Config{
		Steps: steps,
		Complete: func(_ context.Context) (bool, error) {
			return *completeVal, nil
		},
	})
}

func TestAllFieldsFromEnv_AllPresent(t *testing.T) {
	t.Setenv("TEST_EMAIL", "a@b.c")
	t.Setenv("TEST_NAME", "alice")
	steps := []Step{
		{Name: "s1", Fields: []Field{
			{Name: "email", EnvVar: "TEST_EMAIL"},
			{Name: "name", EnvVar: "TEST_NAME"},
		}},
	}
	if !allFieldsFromEnv(steps) {
		t.Fatal("expected headless=true when all env present")
	}
}

func TestAllFieldsFromEnv_PartialMissing(t *testing.T) {
	t.Setenv("TEST_EMAIL", "a@b.c")
	steps := []Step{
		{Name: "s1", Fields: []Field{
			{Name: "email", EnvVar: "TEST_EMAIL"},
			{Name: "name", EnvVar: "TEST_MISSING_NAME"},
		}},
	}
	if allFieldsFromEnv(steps) {
		t.Fatal("expected headless=false when an env var is missing")
	}
}

func TestAllFieldsFromEnv_NoEnvVarField(t *testing.T) {
	steps := []Step{
		{Name: "s1", Fields: []Field{
			{Name: "manual", EnvVar: ""},
		}},
	}
	if allFieldsFromEnv(steps) {
		t.Fatal("expected headless=false when a field has no EnvVar")
	}
}

func TestCanRunHeadless_AllPresent(t *testing.T) {
	t.Setenv("TEST_HEADLESS_E", "x")
	t.Setenv("TEST_HEADLESS_P", "y")
	done := false
	r := helperBuildRunner(t, []Step{
		{Name: "s1", Fields: []Field{
			{Name: "e", EnvVar: "TEST_HEADLESS_E"},
			{Name: "p", EnvVar: "TEST_HEADLESS_P"},
		}},
	}, &done)
	can, err := r.CanRunHeadless(context.Background())
	if err != nil {
		t.Fatalf("CanRunHeadless: %v", err)
	}
	if !can {
		t.Fatal("expected CanRunHeadless=true")
	}
}

func TestCanRunHeadless_PartialMissing(t *testing.T) {
	t.Setenv("TEST_PARTIAL_E", "x")
	done := false
	r := helperBuildRunner(t, []Step{
		{Name: "s1", Fields: []Field{
			{Name: "e", EnvVar: "TEST_PARTIAL_E"},
			{Name: "p", EnvVar: "TEST_PARTIAL_MISSING"},
		}},
	}, &done)
	can, err := r.CanRunHeadless(context.Background())
	if err != nil {
		t.Fatalf("CanRunHeadless: %v", err)
	}
	if can {
		t.Fatal("expected CanRunHeadless=false when env partial")
	}
}

func TestRunSteps_StepErrorNamesStep(t *testing.T) {
	wantErr := errors.New("boom")
	done := false
	r := helperBuildRunner(t, []Step{
		{Name: "good", Fields: []Field{{Name: "x", EnvVar: "X"}}, Run: func(_ context.Context, _ map[string]string) error { return nil }},
		{Name: "bad", Fields: []Field{{Name: "y", EnvVar: "Y"}}, Run: func(_ context.Context, _ map[string]string) error { return wantErr }},
	}, &done)

	t.Setenv("X", "1")
	t.Setenv("Y", "2")
	err := r.RunSteps(context.Background())
	if err == nil {
		t.Fatal("expected error from RunSteps")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped error, got: %v", err)
	}
	// Error must name the step.
	if !contains(err.Error(), "bad") {
		t.Fatalf("error must name step 'bad', got: %s", err.Error())
	}
}

func TestRunSteps_ValidationErrorNamesField(t *testing.T) {
	done := false
	r := helperBuildRunner(t, []Step{
		{Name: "s1", Fields: []Field{
			{Name: "x", EnvVar: "VX", Validate: func(v string) error { return fmt.Errorf("too short") }},
		}, Run: func(_ context.Context, _ map[string]string) error { return nil }},
	}, &done)

	t.Setenv("VX", "short")
	err := r.RunSteps(context.Background())
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !contains(err.Error(), "too short") {
		t.Fatalf("error must include validation message, got: %s", err.Error())
	}
}

func TestRunSteps_AllSucceed(t *testing.T) {
	done := false
	var order []string
	var mu sync.Mutex
	r := helperBuildRunner(t, []Step{
		{Name: "first", Fields: []Field{{Name: "a", EnvVar: "EA"}}, Run: func(_ context.Context, _ map[string]string) error {
			mu.Lock()
			order = append(order, "first")
			mu.Unlock()
			return nil
		}},
		{Name: "second", Fields: []Field{{Name: "b", EnvVar: "EB"}}, Run: func(_ context.Context, _ map[string]string) error {
			mu.Lock()
			order = append(order, "second")
			mu.Unlock()
			return nil
		}},
	}, &done)

	t.Setenv("EA", "1")
	t.Setenv("EB", "2")
	if err := r.RunSteps(context.Background()); err != nil {
		t.Fatalf("RunSteps: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("expected [first second], got %v", order)
	}
}

// TestSecretNeverPrefilled renders an actual wizard page containing a
// Secret field whose EnvVar is set, then asserts the rendered HTML does
// NOT include the env value in a value= attribute for that field.
// Non-secret fields ARE pre-filled; only secrets are suppressed.
func TestSecretNeverPrefilled(t *testing.T) {
	t.Setenv("GOFASTR_SETUP_PW", "super-secret-value")
	t.Setenv("GOFASTR_SETUP_NAME", "prefilled-name")

	done := false
	r := New(Config{
		DisableToken: true,
		Complete:     func(_ context.Context) (bool, error) { return done, nil },
		Steps: []Step{
			{Name: "create", Fields: []Field{
				{Name: "NAME", Label: "Name", EnvVar: "GOFASTR_SETUP_NAME"},
				{Name: "PW", Label: "Password", EnvVar: "GOFASTR_SETUP_PW", Secret: true},
			}, Run: func(_ context.Context, _ map[string]string) error { return nil }},
		},
	})
	h := r.Handler(func() {}, nil, nil)

	w := doGet(h, "/setup")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /setup expected 200, got %d", w.Code)
	}
	body := w.Body.String()

	// The secret value must never appear anywhere in the rendered HTML.
	if strings.Contains(body, "super-secret-value") {
		t.Fatal("Secret field value must not appear in rendered HTML")
	}

	// Non-secret field IS pre-filled from env.
	if !strings.Contains(body, "prefilled-name") {
		t.Fatal("Non-secret field should be pre-filled from env")
	}

	// The password input should have type="password" and no value=.
	if !strings.Contains(body, `type="password"`) {
		t.Fatal("Secret field should render as type=password")
	}
}

// TestConcurrentStepExecution_OneRun verifies that two concurrent step
// submissions don't both execute the step — the mutex + Complete
// re-check must serialize them.
func TestConcurrentStepExecution_OneRun(t *testing.T) {
	var runCount int64
	// Complete returns false until the step has run; then true.
	completed := int32(0)

	cfg := Config{
		Steps: []Step{
			{
				Name:   "create",
				Fields: []Field{{Name: "x", EnvVar: "CONCURRENT_X"}},
				Run: func(_ context.Context, _ map[string]string) error {
					atomic.AddInt64(&runCount, 1)
					atomic.StoreInt32(&completed, 1)
					return nil
				},
			},
		},
		Complete: func(_ context.Context) (bool, error) {
			return atomic.LoadInt32(&completed) == 1, nil
		},
	}
	r := New(cfg)

	// Two concurrent calls to runStepSerialized for the same step.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.runStepSerialized(context.Background(), 0, cfg.Steps[0], map[string]string{"x": "v"})
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&runCount); got != 1 {
		t.Fatalf("expected step.Run called exactly once, got %d", got)
	}
}

func TestIncomplete_ReturnsTrueWhenNotDone(t *testing.T) {
	done := false
	r := helperBuildRunner(t, nil, &done)
	incomplete, err := r.Incomplete(context.Background())
	if err != nil {
		t.Fatalf("Incomplete: %v", err)
	}
	if !incomplete {
		t.Fatal("expected incomplete=true when done=false")
	}
}

func TestIncomplete_ReturnsFalseWhenDone(t *testing.T) {
	done := true
	r := helperBuildRunner(t, nil, &done)
	incomplete, err := r.Incomplete(context.Background())
	if err != nil {
		t.Fatalf("Incomplete: %v", err)
	}
	if incomplete {
		t.Fatal("expected incomplete=false when done=true")
	}
}

func TestNew_PanicsWithoutComplete(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Config.Complete is nil")
		}
	}()
	_ = New(Config{Steps: nil})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestConcurrentStepExecution_IntermediateStep verifies that concurrent
// POSTs on an INTERMEDIATE step (not the final step) execute it exactly
// once. The fix advances currentStep inside runStepSerialized's lock so
// there is no window between run and advance.
func TestConcurrentStepExecution_IntermediateStep(t *testing.T) {
	var runCount int64
	done := false

	r := New(Config{
		DisableToken: true,
		Complete:     func(_ context.Context) (bool, error) { return done, nil },
		Steps: []Step{
			{Name: "first", Fields: []Field{{Name: "A"}}, Run: func(_ context.Context, _ map[string]string) error {
				atomic.AddInt64(&runCount, 1)
				return nil
			}},
			{Name: "second", Fields: []Field{{Name: "B"}}, Run: func(_ context.Context, _ map[string]string) error {
				return nil
			}},
		},
	})
	h := r.Handler(func() {}, nil, nil)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			doPost(h, "/setup", "A=value", nil)
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&runCount); got != 1 {
		t.Fatalf("expected step1.Run called exactly once, got %d", got)
	}
}

// TestRender_NoHandRolledClasses verifies the rendered wizard HTML
// contains no invented setup-* class names — only framework/ui
// composition primitives.
func TestRender_NoHandRolledClasses(t *testing.T) {
	done := false
	r := New(Config{
		DisableToken: true,
		Complete:     func(_ context.Context) (bool, error) { return done, nil },
		Steps: []Step{
			{Name: "step1", Fields: []Field{{Name: "X", Label: "X"}}},
		},
	})
	h := r.Handler(func() {}, nil, nil)

	// Every class on every rendered page must come from the design system:
	// registered ui-* component classes (with their BEM-ish suffixes) or
	// runtime fui-* markers. An invented class — setup-*, ui-input,
	// anything the registry never styles — is hand-rolled markup
	// (Hard Rules 7/8).
	classRe := regexp.MustCompile(`class="([^"]*)"`)
	assertDesignSystemClasses := func(page, body string) {
		t.Helper()
		for _, m := range classRe.FindAllStringSubmatch(body, -1) {
			for _, cls := range strings.Fields(m[1]) {
				if strings.HasPrefix(cls, "ui-") || strings.HasPrefix(cls, "fui-") {
					continue
				}
				t.Errorf("%s: class %q is not a design-system class:\n%s", page, cls, body)
			}
		}
		// ui-input specifically: looks plausible, styled nowhere.
		if strings.Contains(body, `"ui-input"`) {
			t.Errorf("%s: ui-input is an invented class (inputs are styled via the form-field selector)", page)
		}
	}

	// Step page.
	w := doGet(h, "/setup")
	assertDesignSystemClasses("step page", w.Body.String())

	// Completion page.
	done = true
	w = doGet(h, "/setup")
	assertDesignSystemClasses("completion page", w.Body.String())
}
