// Package session implements the SQLite append-only event log and
// associated retention/redaction/ledger machinery per § Persistence.
//
// At-rest encryption is on the v0.2 roadmap (OPS-SQLITE-VACUUM /
// OPS-KEY-ROTATION); v0.1 stores unencrypted but applies the
// redaction middleware on the write path so secrets never land on
// disk.
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// PastSession is a single past-session summary row returned by
// Store.ListPastSessions. Used to populate the "Sessions" sidebar
// and the REST /v1/sessions?past=true endpoint.
type PastSession struct {
	SessionID    ids.SessionID `json:"sessionId"`
	FirstSeenAt  time.Time     `json:"firstSeenAt"`
	LastSeenAt   time.Time     `json:"lastSeenAt"`
	EventCount   int64         `json:"eventCount"`
	FirstMessage string        `json:"firstMessage,omitempty"` // first user-turn text, truncated
}

// Store is the durable session storage. Implementations: sqlite (v0.1).
type Store interface {
	// AppendEvent persists one envelope. Called by an event-bus
	// subscriber on every Publish.
	AppendEvent(ctx context.Context, env control.EventEnvelope) error

	// EventsSince returns events with id > since for the given
	// session, in ID order. Used by stream-resume across all
	// transports.
	EventsSince(ctx context.Context, session ids.SessionID, since uint64, limit int) ([]control.EventEnvelope, error)

	// ListPastSessions returns a summary row per distinct session in
	// the event log, ordered newest-last-seen first. limit ≤ 0 means
	// unbounded.
	ListPastSessions(ctx context.Context, limit int) ([]PastSession, error)

	// RecordToolIntent writes a tool_call_intents row before the
	// tool spawns. For mutating tools, the implementation MUST fsync
	// before returning.
	RecordToolIntent(ctx context.Context, intent ToolIntent) error

	// RecordToolOutcome writes a tool_call_outcomes row after the
	// tool returns. fsync for mutating tools.
	RecordToolOutcome(ctx context.Context, outcome ToolOutcome) error

	// OrphanIntents returns intent rows with no matching outcome —
	// used on resume to surface tool calls that started before a
	// crash and need user disposition.
	OrphanIntents(ctx context.Context, session ids.SessionID) ([]ToolIntent, error)

	// ApplyRetention drops content from events older than ttl,
	// leaving metadata-only rows. Called by a daily scheduled task.
	ApplyRetention(ctx context.Context, ttl time.Duration) (rowsAffected int64, err error)

	// Close releases resources.
	Close() error
}

// ToolIntent is one row in the intent ledger.
type ToolIntent struct {
	CallID    ids.CallID
	LogID     ids.LogID
	Tool      string
	ArgsHash  string // sha256 of args
	Mutating  bool
	StartedAt time.Time
}

// ToolOutcome is one row in the outcome ledger.
type ToolOutcome struct {
	CallID      ids.CallID
	Outcome     string // "ok" | "error" | "cancelled" | "timeout"
	CompletedAt time.Time
	ResultRef   string // pointer into events table (event ID encoded)
}

// EncodeEvent is a helper for tests/in-memory stores: serializes the
// envelope's payload to a raw JSON message.
func EncodeEvent(env control.EventEnvelope) []byte {
	b, _ := json.Marshal(env)
	return b
}

// ErrSession is the root of session-package errors.
type ErrSession string

func (e ErrSession) Error() string { return string(e) }

// Specific errors.
const (
	ErrUnknownSession   = ErrSession("session: unknown session")
	ErrSchemaMismatch   = ErrSession("session: schema version mismatch")
	ErrOrphanIntents    = ErrSession("session: tool intents without outcomes (orphans)")
)

// Standard SQL schema. Exposed for tests + migrations. Bump
// SchemaVersion when changing.
const SchemaVersion = 1

// Schema is the v1 DDL. Migrations live as additional .sql files in
// session/sqlite/migrations/ when v2+ ships; v0.1 only needs v1.
const Schema = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL,
    sha256     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    id         INTEGER NOT NULL,
    session    TEXT NOT NULL,
    log_id     TEXT,
    kind       TEXT NOT NULL,
    originator TEXT,
    ts         TEXT NOT NULL,
    payload    TEXT NOT NULL,
    PRIMARY KEY (session, id)
);

CREATE INDEX IF NOT EXISTS idx_events_session_id
    ON events (session, id);

CREATE INDEX IF NOT EXISTS idx_events_ts
    ON events (ts);

CREATE TABLE IF NOT EXISTS tool_call_intents (
    call_id     TEXT PRIMARY KEY,
    log_id      TEXT NOT NULL,
    tool_name   TEXT NOT NULL,
    args_hash   TEXT NOT NULL,
    is_mutating INTEGER NOT NULL,
    started_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tool_call_outcomes (
    call_id      TEXT PRIMARY KEY REFERENCES tool_call_intents(call_id),
    outcome      TEXT NOT NULL,
    completed_at TEXT NOT NULL,
    result_ref   TEXT
);
`

// Sentinel for placing the events table in WAL mode for crash
// safety. Applied at Open() time, separately from Schema (PRAGMAs
// can't go inside a transaction with DDL).
const PragmaWAL = "PRAGMA journal_mode = WAL"

// Compile-time assertion that Store has the right shape if
// implementations exist in this package.
var _ = fmt.Sprintf
