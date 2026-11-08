package framework

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// The App sizes its default idempotency store's in-flight TTL from the
// request budget plus slack, and refuses a store whose TTL the budget
// outlives — the WriteTimeout-against-RequestTimeout shape (#659,
// fifth decision).

func idemPrincipal(*http.Request) string { return "user-1" }

// shortClaimStore reports an in-flight TTL shorter than the budget.
type shortClaimStore struct{}

func (shortClaimStore) Begin(context.Context, string, string) (*middleware.IdempotentResponse, bool, error) {
	return nil, false, nil
}
func (shortClaimStore) Finish(context.Context, string, string, *middleware.IdempotentResponse) error {
	return nil
}
func (shortClaimStore) InFlightTTL() time.Duration { return 30 * time.Second }

// opaqueClaimStore implements IdempotencyStore only: no InFlightTTL.
type opaqueClaimStore struct{}

func (opaqueClaimStore) Begin(context.Context, string, string) (*middleware.IdempotentResponse, bool, error) {
	return nil, false, nil
}
func (opaqueClaimStore) Finish(context.Context, string, string, *middleware.IdempotentResponse) error {
	return nil
}
func TestAppSizesDefaultIdempotencyInFlightTTL(t *testing.T) {
	app := NewApp(
		WithConfig(AppConfig{RequestTimeout: 90 * time.Second}),
		WithIdempotency(middleware.IdempotencyConfig{Principal: idemPrincipal}),
	)
	ts, ok := app.idempotency.Store.(middleware.InFlightTTLStore)
	if !ok {
		t.Fatal("the App's default store does not report its in-flight TTL")
	}
	if got := ts.InFlightTTL(); got != 90*time.Second+idempotencyInFlightSlack {
		t.Fatalf("default store in-flight TTL = %v, want the 90s budget plus %v slack", got, idempotencyInFlightSlack)
	}

	// An unset RequestTimeout still enforces the 30s default in the
	// default chain, so the claim is sized past THAT; only a disabled
	// deadline leaves the store's own default in place.
	app = NewApp(
		WithConfig(AppConfig{}),
		WithIdempotency(middleware.IdempotencyConfig{Principal: idemPrincipal}),
	)
	if got := app.idempotency.Store.(middleware.InFlightTTLStore).InFlightTTL(); got != defaultRequestTimeout+idempotencyInFlightSlack {
		t.Fatalf("in-flight TTL with no RequestTimeout = %v, want the enforced %v default plus %v slack", got, defaultRequestTimeout, idempotencyInFlightSlack)
	}
	app = NewApp(
		WithConfig(AppConfig{DisableRequestTimeout: true}),
		WithIdempotency(middleware.IdempotencyConfig{Principal: idemPrincipal}),
	)
	if got := app.idempotency.Store.(middleware.InFlightTTLStore).InFlightTTL(); got != 30*time.Second {
		t.Fatalf("in-flight TTL with DisableRequestTimeout = %v, want the store's own 30s default", got)
	}
}

func TestAppRefusesIdempotencyClaimOutlivedByRequestTimeout(t *testing.T) {
	app := NewApp(
		WithConfig(AppConfig{RequestTimeout: time.Minute}),
		WithIdempotency(middleware.IdempotencyConfig{
			Principal: idemPrincipal,
			Store:     shortClaimStore{},
		}),
	)
	err := app.Start("127.0.0.1:0")
	if err == nil {
		app.Shutdown(context.Background())
		t.Fatal("Start accepted an in-flight TTL the request budget outlives")
	}
	for _, want := range []string{"30s", "1m0s"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Start error does not name both values (want %q): %v", want, err)
		}
	}
	if !strings.Contains(err.Error(), "in-flight TTL") {
		t.Fatalf("Start error is not the idempotency TTL refusal: %v", err)
	}
}

// The default chain still enforces its 30s deadline when
// RequestTimeout is unset, so a 30s claim is refused there too — the
// check must read the enforced budget, not the configured field.
func TestAppRefusesShortClaimAgainstDefaultBudget(t *testing.T) {
	app := NewApp(
		WithIdempotency(middleware.IdempotencyConfig{
			Principal: idemPrincipal,
			Store:     shortClaimStore{},
		}),
	)
	if err := app.Start("127.0.0.1:0"); err == nil {
		app.Shutdown(context.Background())
		t.Fatal("Start accepted a 30s in-flight TTL against the enforced 30s default deadline")
	}
}

func TestAppWarnsOnUncheckedIdempotencyInFlightTTL(t *testing.T) {
	run := func(cfg AppConfig, store middleware.IdempotencyStore) string {
		var logs strings.Builder
		app := NewApp(
			WithConfig(cfg),
			WithLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))),
			WithIdempotency(middleware.IdempotencyConfig{
				Principal: idemPrincipal,
				Store:     store,
			}),
		)
		_, stop := startOnRandomPort(t, app)
		stop()
		return logs.String()
	}
	// A store without the method: nothing to check, say so.
	if logs := run(AppConfig{RequestTimeout: time.Minute}, opaqueClaimStore{}); !strings.Contains(logs, "does not report") {
		t.Fatalf("no warning for a store without InFlightTTL: %q", logs)
	}
	// DisableRequestTimeout: no budget to check against.
	if logs := run(AppConfig{DisableRequestTimeout: true}, shortClaimStore{}); !strings.Contains(logs, "DisableRequestTimeout") {
		t.Fatalf("no warning with DisableRequestTimeout: %q", logs)
	}
}
