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

	// Redactors run over every event payload before AppendEvent stores it.
	Redactors []Redactor

	encMode EncryptionMode
	encKey  []byte

	mu sync.Mutex
}

// Redactor replaces matched substrings with redaction markers.
type Redactor interface {
	Redact(text string) string
}

// MemberRedactor is a Redactor that also sees the member name a JSON string value belongs to.
type MemberRedactor interface {
	Redactor
	RedactMember(key, value string) string
}

// Open opens or creates the owner-only SQLite session log at path and applies the schema.
func Open(path string) (*Store, error) {
	if err := fileperm.MkdirPrivate(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := fileperm.SeedOwnerOnly(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_journal=WAL&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
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

// AppendEvent stores one envelope after running the Redactors over its payload.
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

// EventsSince returns up to limit (default 10,000) events of sess with id > since, in id order.
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

// ListPastSessions returns one summary row per session, newest last-seen first, with the first root TurnStarted text.
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
	for i := range out {
		var payload string
		err := s.db.QueryRowContext(ctx, `
            SELECT payload FROM events
             WHERE session = ? AND kind = 'TurnStarted' AND COALESCE(sub_agent, '') = ''
          ORDER BY id ASC LIMIT 1`,
			string(out[i].SessionID),
		).Scan(&payload)
		if err != nil {
			continue
		}
		if text := extractFirstText(payload); text != "" {
			if len(text) > 80 {
				text = text[:80] + "…"
			}
			out[i].FirstMessage = text
		}
	}
	return out, nil
}

func extractFirstText(payload string) string {
	const marker = `"text":"`
	i := strings.Index(payload, marker)
	if i < 0 {
		return ""
	}
	start := i + len(marker)
	var out []byte
	for j := start; j < len(payload); j++ {
		c := payload[j]
		if c == '\\' && j+1 < len(payload) {
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

// RecordToolIntent upserts an intent row; a mutating intent also checkpoints the WAL.
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
		_, _ = s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(FULL)`)
	}
	return err
}

// RecordToolOutcome upserts an outcome row and checkpoints the WAL.
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
		_, _ = s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(FULL)`)
	}
	return err
}

// OrphanIntents returns every intent with no outcome row, in any session.
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

// ApplyRetention replaces the payload of events older than ttl with "«ttl-expired»".
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

// Close closes the database.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// HashArgs returns the SHA-256 hex of tool arguments.
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

// Redact replaces every match of the on-write secret patterns.
func (DefaultRedactor) Redact(text string) string {
	for _, sub := range redactionSubs {
		text = sub.regex.ReplaceAllString(text, sub.repl)
	}
	return text
}

// RedactMember replaces value when key is a secret-suffixed variable name such as ZAI_API_KEY.
func (DefaultRedactor) RedactMember(key, value string) string {
	out, _ := redactSecretMember(key, value)
	return out
}

var _ MemberRedactor = DefaultRedactor{}

var _ session.Store = (*Store)(nil)

// SnapshotPath returns the path the store was opened with.
func (s *Store) SnapshotPath() string { return s.path }

// DefaultPath returns ~/.local/share/gofastr/harness/sessions.db.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "gofastr", "harness", "sessions.db")
}
