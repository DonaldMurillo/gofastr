package framework

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestResolveSetupEnv_Off(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "off")
	mode, err := resolveSetupEnv()
	if err != nil {
		t.Fatalf("resolveSetupEnv: %v", err)
	}
	if mode != setupOff {
		t.Fatalf("expected setupOff, got %d", mode)
	}
}

func TestResolveSetupEnv_Force(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "force")
	mode, err := resolveSetupEnv()
	if err != nil {
		t.Fatalf("resolveSetupEnv: %v", err)
	}
	if mode != setupForce {
		t.Fatalf("expected setupForce, got %d", mode)
	}
}

func TestResolveSetupEnv_Auto(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	mode, err := resolveSetupEnv()
	if err != nil {
		t.Fatalf("resolveSetupEnv: %v", err)
	}
	if mode != setupAuto {
		t.Fatalf("expected setupAuto, got %d", mode)
	}
}

func TestResolveSetupEnv_Invalid(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "yes")
	_, err := resolveSetupEnv()
	if err == nil {
		t.Fatal("expected error for invalid GOFASTR_SETUP")
	}
	if !strings.Contains(err.Error(), "yes") {
		t.Fatalf("error must name the bad value, got: %s", err.Error())
	}
}

func TestResolveSetupEnv_CaseInsensitive(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "OFF")
	mode, err := resolveSetupEnv()
	if err != nil {
		t.Fatalf("resolveSetupEnv: %v", err)
	}
	if mode != setupOff {
		t.Fatalf("expected setupOff for 'OFF', got %d", mode)
	}
}

// TestWithSetup_NilPanics verifies WithSetup panics on nil.
func TestWithSetup_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for WithSetup(nil)")
		}
	}()
	_ = WithSetup(nil)
}

// TestWithSetup_StoresRunner verifies the runner is stored on the App.
func TestWithSetup_StoresRunner(t *testing.T) {
	mock := &mockSetupRunner{}
	app := NewApp(WithSetup(mock))
	if app.setup == nil {
		t.Fatal("expected setup runner to be set")
	}
}

// ─── mock SetupRunner for framework-level tests ──────────────────────

type mockSetupRunner struct {
	incomplete    bool
	incompleteErr error
	canHeadless   bool
	headlessErr   error
	runStepsErr   error
	setupURL      string
}

func (m *mockSetupRunner) Incomplete(_ context.Context) (bool, error) {
	return m.incomplete, m.incompleteErr
}

func (m *mockSetupRunner) CanRunHeadless(_ context.Context) (bool, error) {
	return m.canHeadless, m.headlessErr
}

func (m *mockSetupRunner) RunSteps(_ context.Context) error {
	return m.runStepsErr
}

func (m *mockSetupRunner) Handler(_ func(), _, _ http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func (m *mockSetupRunner) SetupURL(_ string) string {
	return m.setupURL
}
