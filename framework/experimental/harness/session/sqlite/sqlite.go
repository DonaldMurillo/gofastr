// Package sqlite is the SQLite-backed implementation of session.Store.
//
// Uses GoFastr's pure-Go SQLite adapter. Per the architecture doc,
// at-rest encryption is on the v0.2 roadmap; v0.1
// stores plaintext and relies on the redaction middleware on the
// write path to keep secrets out of the file.
package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/internal/jsonredact"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/session"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Store is the SQLite-backed session.Store.
type Store struct {
	db   *sql.DB
	path string

	// Redactors run on every event payload before it lands in the
	// `events.payload` column. The default redactor uses common
	// secret regexes; profiles can swap in tighter sets.
	Redactors []Redactor

	// Encryption state when opened via OpenEncrypted. Zero values
	// mean plaintext.
	encMode EncryptionMode
	encKey  []byte

	mu sync.Mutex
}

// Redactor replaces matched substrings with redaction markers.
type Redactor interface {
	Redact(text string) string
}

// MemberRedactor is a Redactor that also judges a JSON string by the
// object member it is the value of: key is the member name, or "" for
// an object key or an array element. DefaultRedactor implements it, so
// {"ZAI_API_KEY": "<v>"} loses its value although neither string alone
// matches a pattern.
type MemberRedactor interface {
	Redactor
	RedactMember(key, value string) string
}

// Open opens (or creates) the SQLite session log at path. Creates
// the parent directory and applies the v1 schema on first use.
func Open(path string) (*Store, error) {
	if err := fileperm.MkdirPrivate(filepath.Dir(path)); err != nil {
		return nil, err
	}
	// Seed the file 0600 BEFORE the driver opens it: the event log
	// carries full plaintext session transcripts, the SQLite driver
	// creates a missing database (and its -wal sidecar, which inherits
	// the main db's mode) 0666&~umask, and a pre-existing weaker file
	// must be tightened too. OpenCostLedger seeds its ledger the same
	// way.
	if err := fileperm.SeedOwnerOnly(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_journal=WAL&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	// WAL mode (idempotent; sqlite3 driver honors the URL flag, but
	// double-check for clarity).
	if _, err := db.Exec(session.PragmaWAL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: enable WAL: %w", err)
	}
	if _, err := db.Exec(session.Schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: apply schema: %w", err)
	}
	if err := addSubAgentColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: add events.sub_agent: %w", err)
	}
	// Record the schema migration if it's not already there.
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at, sha256) VALUES (?, ?, ?)`,
		session.SchemaVersion, time.Now().UTC().Format(time.RFC3339),
		hashStr("session-schema-v1"),
	); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db, path: path}
	s.Redactors = []Redactor{DefaultRedactor{}}
	return s, nil
}

// AppendEvent persists one envelope with redactors applied.
func (s *Store) AppendEvent(ctx context.Context, env control.EventEnvelope) error {
	payload := s.redactPayload(env.Payload)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO events(id, session, log_id, kind, originator, ts, payload, sub_agent)
        VALUES(?, ?, '', ?, ?, ?, ?, ?)`,
		env.ID, string(env.Session), env.Kind,
		string(env.Originator),
		env.TS.UTC().Format(time.RFC3339Nano),
		payload, env.SubAgent,
	)
	return err
}

// redactPayload runs the redactors over each decoded string of a JSON
// payload, so a pattern that reaches a closing quote or an escape cannot
// leave the stored event unparseable. A payload that is not JSON gets
// the redactors on its raw text.
func (s *Store) redactPayload(raw []byte) string {
	apply := func(text string) string {
		for _, r := range s.Redactors {
			text = r.Redact(text)
		}
		return text
	}
	member := func(key, text string) string {
		for _, r := range s.Redactors {
			if mr, ok := r.(MemberRedactor); ok && key != "" {
				text = mr.RedactMember(key, text)
			}
		}
		return apply(text)
	}
	if out, ok := jsonredact.Members(raw, member); ok {
		return string(out)
	}
	return apply(string(raw))
}

// EventsSince returns events with id > since for a session.
func (s *Store) EventsSince(ctx context.Context, sess ids.SessionID, since uint64, limit int) ([]control.EventEnvelope, error) {
	if limit <= 0 {
		limit = 10_000
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, kind, originator, ts, payload, COALESCE(sub_agent, '')
          FROM events
         WHERE session = ? AND id > ?
         ORDER BY id ASC
         LIMIT ?`,
		string(sess), since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []control.EventEnvelope
	for rows.Next() {
		var (
			id         uint64
			kind       string
			originator string
			tsStr      string
			payload    string
			subAgent   string
		)
		if err := rows.Scan(&id, &kind, &originator, &tsStr, &payload, &subAgent); err != nil {
			return nil, err
		}
		ts, _ := time.Parse(time.RFC3339Nano, tsStr)
		out = append(out, control.EventEnvelope{
			ID:         id,
			Kind:       kind,
			Session:    sess,
			Originator: ids.ClientID(originator),
			TS:         ts,
			Payload:    json.RawMessage(payload),
			SubAgent:   subAgent,
		})
	}
	return out, rows.Err()
}

// addSubAgentColumn adds events.sub_agent to a database created before
// sub-agent events were persisted. CREATE TABLE IF NOT EXISTS leaves an
// existing table as it was.
func addSubAgentColumn(db *sql.DB) error {
	rows, err := db.Query(`SELECT name FROM pragma_table_info('events')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == "sub_agent" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_ = rows.Close()
	_, err = db.Exec(`ALTER TABLE events ADD COLUMN sub_agent TEXT`)
	return err
}

// ListPastSessions aggregates the events table into per-session
// summaries. Newest-last-seen first. Uses two passes, one for the
// counts/timestamps (fast), one for the first user-message text per
// session (slower; bounded by `limit`).
func (s *Store) ListPastSessions(ctx context.Context, limit int, only []ids.SessionID) ([]session.PastSession, error) {
	stmt := `SELECT session, MIN(ts), MAX(ts), COUNT(*) FROM events`
	var args []any
	if len(only) > 0 {
		stmt += ` WHERE session IN (?` + strings.Repeat(`, ?`, len(only)-1) + `)`
		for _, id := range only {
			args = append(args, string(id))
		}
	}
	stmt += ` GROUP BY session ORDER BY MAX(ts) DESC`
	if limit > 0 {
		stmt += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []session.PastSession
	for rows.Next() {
		var (
			sess    string
			firstTs string
			lastTs  string
			count   int64
		)
		if err := rows.Scan(&sess, &firstTs, &lastTs, &count); err != nil {
			return nil, err
		}
		first, _ := time.Parse(time.RFC3339Nano, firstTs)
		last, _ := time.Parse(time.RFC3339Nano, lastTs)
		out = append(out, session.PastSession{
			SessionID:   ids.SessionID(sess),
			FirstSeenAt: first,
			LastSeenAt:  last,
			EventCount:  count,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Best-effort: enrich with first user-input text per session.
	for i := range out {
		var payload string
		err := s.db.QueryRowContext(ctx, `
            SELECT payload FROM events
             WHERE session = ? AND kind = 'TurnStarted' AND COALESCE(sub_agent, '') = ''
          ORDER BY id ASC LIMIT 1`,
			string(out[i].SessionID),
		).Scan(&payload)
		if err != nil {
			continue // missing TurnStarted is fine, sessions log other events too
		}
		// TurnStarted payload contains {"content":[{"type":"text","text":"..."}]}
		// Cheap extraction without unmarshaling the full schema.
		if text := extractFirstText(payload); text != "" {
			if len(text) > 80 {
				text = text[:80] + "…"
			}
			out[i].FirstMessage = text
		}
	}
	return out, nil
}

// extractFirstText pulls the first "text":"..." value out of a
// TurnStarted payload without paying for a full JSON unmarshal of
// the ContentBlock schema. Tolerant of escaping.
func extractFirstText(payload string) string {
	const marker = `"text":"`
	i := strings.Index(payload, marker)
	if i < 0 {
		return ""
	}
	start := i + len(marker)
	// Walk to the matching unescaped closing quote.
	var out []byte
	for j := start; j < len(payload); j++ {
		c := payload[j]
		if c == '\\' && j+1 < len(payload) {
			// Skip escape pair, decode common ones.
			next := payload[j+1]
			switch next {
			case 'n':
				out = append(out, '\n')
			case 't':
				out = append(out, '\t')
			case '"':
				out = append(out, '"')
			case '\\':
				out = append(out, '\\')
			default:
				out = append(out, next)
			}
			j++
			continue
		}
		if c == '"' {
			return string(out)
		}
		out = append(out, c)
	}
	return string(out)
}

// RecordToolIntent writes an intent row.
func (s *Store) RecordToolIntent(ctx context.Context, intent session.ToolIntent) error {
	mut := 0
	if intent.Mutating {
		mut = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, `
        INSERT OR REPLACE INTO tool_call_intents(call_id, log_id, tool_name, args_hash, is_mutating, started_at)
        VALUES(?, ?, ?, ?, ?, ?)`,
		string(intent.CallID), string(intent.LogID),
		intent.Tool, intent.ArgsHash, mut,
		intent.StartedAt.UTC().Format(time.RFC3339Nano),
	)
	if err == nil && intent.Mutating {
		// Best-effort fsync via a WAL checkpoint after the write.
		_, _ = s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(FULL)`)
	}
	return err
}

// RecordToolOutcome writes an outcome row.
func (s *Store) RecordToolOutcome(ctx context.Context, outcome session.ToolOutcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO tool_call_outcomes(call_id, outcome, completed_at, result_ref)
        VALUES(?, ?, ?, ?)
        ON CONFLICT(call_id) DO UPDATE SET
            outcome      = excluded.outcome,
            completed_at = excluded.completed_at,
            result_ref   = excluded.result_ref`,
		string(outcome.CallID), outcome.Outcome,
		outcome.CompletedAt.UTC().Format(time.RFC3339Nano),
		outcome.ResultRef,
	)
	if err == nil {
		// best-effort: the row is durable in WAL; a checkpoint only advances
		// the main database file and can safely retry later.
		_, _ = s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(FULL)`)
	}
	return err
}

// OrphanIntents returns intents without matching outcomes. v0.1 returns
// all intents for the given LogID; the higher layer narrows by session.
func (s *Store) OrphanIntents(ctx context.Context, sess ids.SessionID) ([]session.ToolIntent, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT i.call_id, i.log_id, i.tool_name, i.args_hash, i.is_mutating, i.started_at
          FROM tool_call_intents i
          LEFT JOIN tool_call_outcomes o USING (call_id)
         WHERE o.call_id IS NULL`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []session.ToolIntent
	for rows.Next() {
		var (
			callID, logID, name, hash, ts string
			mut                           int
		)
		if err := rows.Scan(&callID, &logID, &name, &hash, &mut, &ts); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339Nano, ts)
		out = append(out, session.ToolIntent{
			CallID:    ids.CallID(callID),
			LogID:     ids.LogID(logID),
			Tool:      name,
			ArgsHash:  hash,
			Mutating:  mut == 1,
			StartedAt: t,
		})
	}
	return out, rows.Err()
}

// ApplyRetention drops the payload column for events older than ttl,
// leaving metadata-only rows.
func (s *Store) ApplyRetention(ctx context.Context, ttl time.Duration) (int64, error) {
	cutoff := time.Now().Add(-ttl).UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
        UPDATE events
           SET payload = '"«ttl-expired»"'
         WHERE ts < ? AND payload != '"«ttl-expired»"'`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Close closes the underlying DB.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// HashArgs returns the SHA-256 hex of tool arguments, used to fill
// tool_call_intents.args_hash.
func HashArgs(args []byte) string {
	sum := sha256.Sum256(args)
	return hex.EncodeToString(sum[:])
}

func hashStr(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// DefaultRedactor replaces common secret patterns with «redacted:KIND».
type DefaultRedactor struct{}

// Redact applies a list of canned regex replacements. The list is
// intentionally tight in v0.1, false positives are worse than false
// negatives here because the regex runs on *every event payload*.
// Roadmap OBS-EXPORT-BUNDLE adds a deeper-pass detector for export.
func (DefaultRedactor) Redact(text string) string {
	for _, sub := range redactionSubs {
		text = sub.regex.ReplaceAllString(text, sub.repl)
	}
	return text
}

// RedactMember replaces a JSON member's value when the member name is a
// secret-suffixed variable name (ZAI_API_KEY, GOFASTR_HARNESS_PASSPHRASE).
func (DefaultRedactor) RedactMember(key, value string) string {
	out, _ := redactSecretMember(key, value)
	return out
}

var _ MemberRedactor = DefaultRedactor{}

// Compile-time assertion that Store satisfies session.Store.
var _ session.Store = (*Store)(nil)

// SnapshotPath returns the on-disk path the store was opened with.
// Useful for diagnostics + the /health endpoint.
func (s *Store) SnapshotPath() string { return s.path }

// makeAbsPath helps callers build the default location.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "gofastr", "harness", "sessions.db")
}

// Helper to keep imports honest in test contexts.
var _ = strings.Builder{}
