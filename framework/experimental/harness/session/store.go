package session

//gofastr:allow-file(GOFASTR1415) the harness event log is the operator's own SQLite file, never the app database App.EraseUserData and App.ExportData open

import (
	"context"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// PastSession is one summary row returned by Store.ListPastSessions.
type PastSession struct {
	SessionID    ids.SessionID `json:"sessionId"`
	FirstSeenAt  time.Time     `json:"firstSeenAt"`
	LastSeenAt   time.Time     `json:"lastSeenAt"`
	EventCount   int64         `json:"eventCount"`
	FirstMessage string        `json:"firstMessage,omitempty"`
}

// Store is the durable session event log and tool-call ledger.
type Store interface {
	// AppendEvent persists one envelope.
	AppendEvent(ctx context.Context, env control.EventEnvelope) error

	// EventsSince returns up to limit events of session with id > since, in id order.
	EventsSince(ctx context.Context, session ids.SessionID, since uint64, limit int) ([]control.EventEnvelope, error)

	// ListPastSessions returns one row per session, newest last-seen first; limit <= 0 is unbounded and a non-empty only filters before the limit.
	ListPastSessions(ctx context.Context, limit int, only []ids.SessionID) ([]PastSession, error)

	// RecordToolIntent writes the intent row for a tool call before it runs.
	RecordToolIntent(ctx context.Context, intent ToolIntent) error

	// RecordToolOutcome writes the outcome row for a tool call after it returns.
	RecordToolOutcome(ctx context.Context, outcome ToolOutcome) error

	// OrphanIntents returns intent rows that have no outcome row.
	OrphanIntents(ctx context.Context, session ids.SessionID) ([]ToolIntent, error)

	// ApplyRetention replaces the payload of events older than ttl, keeping their metadata.
	ApplyRetention(ctx context.Context, ttl time.Duration) (rowsAffected int64, err error)

	Close() error
}

// ToolIntent is one row in the intent ledger.
type ToolIntent struct {
	CallID    ids.CallID
	LogID     ids.LogID
	Tool      string
	ArgsHash  string
	Mutating  bool
	StartedAt time.Time
}

// ToolOutcome is one row in the outcome ledger.
type ToolOutcome struct {
	CallID      ids.CallID
	Outcome     string
	CompletedAt time.Time
	ResultRef   string
}

// ErrSession is the root of session-package errors.
type ErrSession string

func (e ErrSession) Error() string { return string(e) }

const (
	ErrUnknownSession = ErrSession("session: unknown session")
	ErrSchemaMismatch = ErrSession("session: schema version mismatch")
	ErrOrphanIntents  = ErrSession("session: tool intents without outcomes (orphans)")
)

// SchemaVersion is the version of Schema recorded in schema_migrations.
const SchemaVersion = 1

// Schema is the v1 DDL of the event log and tool-call ledger.
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
    sub_agent  TEXT,
    PRIMARY KEY (session, id)
);

CREATE INDEX IF NOT EXISTS idx_events_session_id
    ON events (session, id);

CREATE INDEX IF NOT EXISTS idx_events_ts
    ON events (ts);

CREATE TABLE IF NOT EXISTS tool_call_intents (
    call_id     TEXT PRIMARY KEY NOT NULL,
    log_id      TEXT NOT NULL,
    tool_name   TEXT NOT NULL,
    args_hash   TEXT NOT NULL,
    is_mutating INTEGER NOT NULL,
    started_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tool_call_outcomes (
    call_id      TEXT PRIMARY KEY NOT NULL REFERENCES tool_call_intents(call_id),
    outcome      TEXT NOT NULL,
    completed_at TEXT NOT NULL,
    result_ref   TEXT
);
`

// PragmaWAL puts the database in WAL journal mode.
const PragmaWAL = "PRAGMA journal_mode = WAL"
