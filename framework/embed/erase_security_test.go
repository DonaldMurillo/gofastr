package embed_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// burnGrant burns a fresh nonce for subject and returns its id and grant.
func burnGrant(t *testing.T, s *embed.SQLBurnStore, subject string) (string, string) {
	t.Helper()
	key := []byte("erase-test-key-0123456789abcdef!") // not-a-secret: test fixture
	now := time.Now()
	tok, err := embed.MintNonce(key, "dashboard", subject, "https://acme.com", nil, time.Minute, now)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	n, err := embed.VerifyNonce(key, tok, now)
	if err != nil {
		t.Fatalf("VerifyNonce: %v", err)
	}
	grant, err := embed.MintGrant(key, n, time.Hour, now.Add(time.Hour), now)
	if err != nil {
		t.Fatalf("MintGrant: %v", err)
	}
	if got, replay, err := s.Burn(context.Background(), n.ID, grant, now.Add(time.Hour)); err != nil || replay || got != grant {
		t.Fatalf("Burn: got=%q replay=%v err=%v", got, replay, err)
	}
	return n.ID, grant
}

// An erasure blanks the grant a burn row keeps for the user: the grant's
// claims name the subject. The row stays, so the nonce is still spent,
// and another subject's grant is untouched.
func eraseScrubsGrant(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	s, err := embed.NewSQLBurnStore(db)
	if err != nil {
		t.Fatalf("NewSQLBurnStore: %v", err)
	}
	erasedID, erasedGrant := burnGrant(t, s, "u-erased")
	keptID, keptGrant := burnGrant(t, s, "u-kept")

	app := framework.NewApp(framework.WithDB(db))
	if _, err := app.EraseUserData(ctx, "u-erased"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM `+embed.DefaultBurnTable+` WHERE grant_token = $1 OR subject = $2`, erasedGrant, "u-erased").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("%d burn row(s) still carry the erased subject's grant", n)
	}
	if got, replay, err := s.Burn(ctx, erasedID, "fresh", time.Now().Add(time.Hour)); err != nil || got != "" || !replay {
		t.Errorf("erased nonce re-burn: got=%q replay=%v err=%v; the nonce must stay spent and hand back nothing", got, replay, err)
	}
	if got, replay, err := s.Burn(ctx, keptID, "fresh", time.Now().Add(time.Hour)); err != nil || got != keptGrant || !replay {
		t.Errorf("kept nonce re-burn: got=%q replay=%v err=%v; another subject's grant was touched", got, replay, err)
	}
}

func TestEraseScrubsBurnedGrant(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:"+t.TempDir()+"/burn.db?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	eraseScrubsGrant(t, db)
}

func TestEraseScrubsBurnedGrantPG(t *testing.T) {
	eraseScrubsGrant(t, pgtest.DB(t))
}

// A burn table an older build created has no subject column. Opening the
// store adds it, the old row keeps its nonce spent, and new burns record
// their subject.
func TestBurnTableGainsSubjectColumn(t *testing.T) {
	for name, open := range map[string]func(t *testing.T) *sql.DB{
		"sqlite": func(t *testing.T) *sql.DB {
			db, err := sql.Open("sqlite3", "file:"+t.TempDir()+"/burn.db?_busy_timeout=5000")
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			return db
		},
		"postgres": pgtest.DB,
	} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			ctx := context.Background()
			if _, err := db.Exec(`CREATE TABLE ` + embed.DefaultBurnTable + ` (nonce_id TEXT PRIMARY KEY, grant_token TEXT NOT NULL, expires_at TIMESTAMP NOT NULL)`); err != nil {
				t.Fatalf("create the older table: %v", err)
			}
			if _, err := db.Exec(`INSERT INTO `+embed.DefaultBurnTable+` (nonce_id, grant_token, expires_at) VALUES ($1, $2, $3)`, "old", "old-grant", time.Now().Add(time.Hour).UTC()); err != nil {
				t.Fatalf("insert the older row: %v", err)
			}
			s, err := embed.NewSQLBurnStore(db)
			if err != nil {
				t.Fatalf("NewSQLBurnStore over the older table: %v", err)
			}
			if _, err := embed.NewSQLBurnStore(db); err != nil {
				t.Fatalf("second NewSQLBurnStore: %v", err)
			}
			if got, replay, err := s.Burn(ctx, "old", "fresh", time.Now().Add(time.Hour)); err != nil || got != "old-grant" || !replay {
				t.Errorf("old nonce re-burn: got=%q replay=%v err=%v", got, replay, err)
			}
			id, _ := burnGrant(t, s, "u-new")
			var subject string
			if err := db.QueryRow(`SELECT subject FROM `+embed.DefaultBurnTable+` WHERE nonce_id = $1`, id).Scan(&subject); err != nil || subject != "u-new" {
				t.Errorf("new burn subject = %q (err %v), want u-new", subject, err)
			}
		})
	}
}
