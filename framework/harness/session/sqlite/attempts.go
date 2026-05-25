package sqlite

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ProviderAttempt records one provider HTTP call. Used by the cost
// reconciliation job to compare local estimates against provider-side
// usage APIs (OpenRouter /credits, ZAI /billing/usage, etc.).
type ProviderAttempt struct {
	ID                int64
	Session           string
	Turn              int
	Provider          string
	Model             string
	RequestID         string // upstream request ID when surfaced
	StartedAt         time.Time
	EndedAt           time.Time
	TerminatedReason  string  // "complete" | "eof" | "timeout" | "cancel" | "http_error"
	InputTokens       int
	OutputTokens      int
	CacheReadTokens   int
	CacheWriteTokens  int
	EstimatedUSD      float64
}

// migration: provider_attempts table.
const attemptsSchema = `
CREATE TABLE IF NOT EXISTS provider_attempts (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    session            TEXT NOT NULL,
    turn               INTEGER NOT NULL,
    provider           TEXT NOT NULL,
    model              TEXT NOT NULL,
    request_id         TEXT,
    started_at         TEXT NOT NULL,
    ended_at           TEXT NOT NULL,
    terminated_reason  TEXT NOT NULL,
    input_tokens       INTEGER NOT NULL,
    output_tokens      INTEGER NOT NULL,
    cache_read_tokens  INTEGER NOT NULL,
    cache_write_tokens INTEGER NOT NULL,
    estimated_usd      REAL NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_attempts_session_turn ON provider_attempts (session, turn);
CREATE INDEX IF NOT EXISTS idx_attempts_request_id  ON provider_attempts (request_id);
CREATE INDEX IF NOT EXISTS idx_attempts_started_at  ON provider_attempts (started_at);

CREATE TABLE IF NOT EXISTS reconciliations (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    provider         TEXT NOT NULL,
    period_start     TEXT NOT NULL,
    period_end       TEXT NOT NULL,
    upstream_usd     REAL NOT NULL,
    local_usd        REAL NOT NULL,
    variance_usd     REAL NOT NULL,
    ran_at           TEXT NOT NULL
);
`

// EnsureAttemptsSchema applies the provider_attempts migration. Safe
// to call repeatedly.
func (s *Store) EnsureAttemptsSchema(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, attemptsSchema)
	return err
}

// RecordAttempt inserts a provider_attempts row, returning the
// assigned ID.
func (s *Store) RecordAttempt(ctx context.Context, a ProviderAttempt) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx, `
        INSERT INTO provider_attempts(
            session, turn, provider, model, request_id, started_at, ended_at,
            terminated_reason, input_tokens, output_tokens,
            cache_read_tokens, cache_write_tokens, estimated_usd)
        VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Session, a.Turn, a.Provider, a.Model, a.RequestID,
		a.StartedAt.UTC().Format(time.RFC3339Nano),
		a.EndedAt.UTC().Format(time.RFC3339Nano),
		a.TerminatedReason,
		a.InputTokens, a.OutputTokens,
		a.CacheReadTokens, a.CacheWriteTokens,
		a.EstimatedUSD,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AttemptsForTurn returns all attempts for one turn. Useful when
// computing the true cost of a turn that retried.
func (s *Store) AttemptsForTurn(ctx context.Context, session string, turn int) ([]ProviderAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, session, turn, provider, model, request_id,
               started_at, ended_at, terminated_reason,
               input_tokens, output_tokens,
               cache_read_tokens, cache_write_tokens, estimated_usd
          FROM provider_attempts
         WHERE session = ? AND turn = ?
         ORDER BY started_at ASC`,
		session, turn,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderAttempt
	for rows.Next() {
		var a ProviderAttempt
		var startedAt, endedAt string
		if err := rows.Scan(&a.ID, &a.Session, &a.Turn, &a.Provider, &a.Model, &a.RequestID,
			&startedAt, &endedAt, &a.TerminatedReason,
			&a.InputTokens, &a.OutputTokens,
			&a.CacheReadTokens, &a.CacheWriteTokens, &a.EstimatedUSD); err != nil {
			return nil, err
		}
		a.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
		a.EndedAt, _ = time.Parse(time.RFC3339Nano, endedAt)
		out = append(out, a)
	}
	return out, rows.Err()
}

// Reconciliation is one reconciliation pass against an upstream
// provider's billing API.
type Reconciliation struct {
	ID           int64
	Provider     string
	PeriodStart  time.Time
	PeriodEnd    time.Time
	UpstreamUSD  float64
	LocalUSD     float64
	VarianceUSD  float64
	RanAt        time.Time
}

// SaveReconciliation persists a reconciliation result.
func (s *Store) SaveReconciliation(ctx context.Context, r Reconciliation) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.VarianceUSD = r.UpstreamUSD - r.LocalUSD
	res, err := s.db.ExecContext(ctx, `
        INSERT INTO reconciliations(provider, period_start, period_end,
            upstream_usd, local_usd, variance_usd, ran_at)
        VALUES(?, ?, ?, ?, ?, ?, ?)`,
		r.Provider,
		r.PeriodStart.UTC().Format(time.RFC3339Nano),
		r.PeriodEnd.UTC().Format(time.RFC3339Nano),
		r.UpstreamUSD, r.LocalUSD, r.VarianceUSD,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// LocalCostInPeriod returns the local-estimated USD for one provider
// across the time window. Used by the reconciliation job.
func (s *Store) LocalCostInPeriod(ctx context.Context, provider string, start, end time.Time) (float64, error) {
	var usd float64
	row := s.db.QueryRowContext(ctx, `
        SELECT COALESCE(SUM(estimated_usd), 0) FROM provider_attempts
         WHERE provider = ? AND started_at >= ? AND ended_at <= ?`,
		provider,
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
	)
	if err := row.Scan(&usd); err != nil {
		return 0, err
	}
	return usd, nil
}

// UpstreamUsageFetcher is the abstraction the reconciliation job uses
// to pull billing data from a provider. Each Provider package can
// implement this if its upstream offers a usage API.
type UpstreamUsageFetcher interface {
	// Name returns the provider name (matches ProviderAttempt.Provider).
	Name() string
	// FetchUsageUSD returns the upstream-reported USD spent in the
	// given period.
	FetchUsageUSD(ctx context.Context, start, end time.Time) (float64, error)
}

// ReconcileNow runs one reconciliation pass for the given provider.
// Returns the diff. Callers schedule this daily.
func (s *Store) ReconcileNow(ctx context.Context, fetcher UpstreamUsageFetcher, period time.Duration) (Reconciliation, error) {
	end := time.Now().UTC()
	start := end.Add(-period)
	upstream, err := fetcher.FetchUsageUSD(ctx, start, end)
	if err != nil {
		return Reconciliation{}, fmt.Errorf("reconcile: fetch upstream: %w", err)
	}
	local, err := s.LocalCostInPeriod(ctx, fetcher.Name(), start, end)
	if err != nil {
		return Reconciliation{}, err
	}
	r := Reconciliation{
		Provider:     fetcher.Name(),
		PeriodStart:  start,
		PeriodEnd:    end,
		UpstreamUSD:  upstream,
		LocalUSD:     local,
		VarianceUSD:  upstream - local,
	}
	if _, err := s.SaveReconciliation(ctx, r); err != nil {
		return Reconciliation{}, err
	}
	return r, nil
}

// ErrNoReconciliations sentinel.
var ErrNoReconciliations = errors.New("sqlite: no reconciliations recorded")
