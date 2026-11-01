package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// A sub-agent's events keep their sub-agent tag through the store, so
// the log and the export can tell them from the session's own.
func TestSubAgentTagRoundTrips(t *testing.T) {
	s := newStore(t)
	sess := ids.NewSessionID()
	env, err := control.EncodeEvent(1, control.TextDelta{Text: "x"}, sess, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	env.SubAgent = "client_sub"
	if err := s.AppendEvent(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	got, err := s.EventsSince(context.Background(), sess, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SubAgent != "client_sub" {
		t.Fatalf("got %+v, want the sub-agent tag kept", got)
	}
}

// A database made before the sub-agent column opens and gains it.
func TestOpenAddsSubAgentColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE events (
    id INTEGER NOT NULL, session TEXT NOT NULL, log_id TEXT, kind TEXT NOT NULL,
    originator TEXT, ts TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY (session, id))`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sess := ids.NewSessionID()
	env, _ := control.EncodeEvent(1, control.TextDelta{Text: "x"}, sess, "", time.Now())
	env.SubAgent = "client_sub"
	if err := s.AppendEvent(context.Background(), env); err != nil {
		t.Fatalf("append on an old database: %v", err)
	}
}
