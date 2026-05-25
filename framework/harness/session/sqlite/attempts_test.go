package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordAttemptAndQuery(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.EnsureAttemptsSchema(ctx); err != nil {
		t.Fatal(err)
	}

	a := ProviderAttempt{
		Session:           "sess_a",
		Turn:              3,
		Provider:          "openrouter",
		Model:             "claude",
		RequestID:         "req-123",
		StartedAt:         time.Now().Add(-time.Second),
		EndedAt:           time.Now(),
		TerminatedReason:  "complete",
		InputTokens:       100,
		OutputTokens:      40,
		EstimatedUSD:      0.01,
	}
	id, err := s.RecordAttempt(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Error("RecordAttempt returned ID 0")
	}
	got, err := s.AttemptsForTurn(ctx, "sess_a", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].EstimatedUSD != 0.01 {
		t.Errorf("got = %+v", got)
	}
}

type stubFetcher struct{ usd float64 }

func (s stubFetcher) Name() string { return "openrouter" }
func (s stubFetcher) FetchUsageUSD(_ context.Context, _, _ time.Time) (float64, error) {
	return s.usd, nil
}

func TestReconcileNow(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "x.db"))
	defer s.Close()
	ctx := context.Background()
	_ = s.EnsureAttemptsSchema(ctx)

	// Record a local estimate.
	_, _ = s.RecordAttempt(ctx, ProviderAttempt{
		Session: "sess", Turn: 1, Provider: "openrouter",
		Model: "claude", StartedAt: time.Now().Add(-time.Hour),
		EndedAt: time.Now().Add(-30 * time.Minute), TerminatedReason: "complete",
		EstimatedUSD: 0.05,
	})

	r, err := s.ReconcileNow(ctx, stubFetcher{usd: 0.07}, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if r.UpstreamUSD != 0.07 || r.LocalUSD < 0.04 || r.LocalUSD > 0.06 {
		t.Errorf("reconciliation = %+v", r)
	}
	if r.VarianceUSD < 0.01 || r.VarianceUSD > 0.03 {
		t.Errorf("variance = %f", r.VarianceUSD)
	}
}
