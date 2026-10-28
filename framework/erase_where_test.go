package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// An eraser's Where equalities narrow its match: a service account that
// shares the erased user's id keeps its rows, in both modes, and the
// report counts only the user's rows.
func TestEraseWhereNarrowsTheMatch(t *testing.T) {
	app, db := newEraseTestApp(t)
	for _, q := range []string{
		`CREATE TABLE tokens (id TEXT, owner_kind TEXT, owner_id TEXT)`,
		`INSERT INTO tokens VALUES ('t1','user','u1'), ('t2','service','u1'), ('t3','user','u2')`,
		`CREATE TABLE memos (id TEXT, owner_kind TEXT, owner_id TEXT, body TEXT)`,
		`INSERT INTO memos VALUES ('m1','user','u1','mine'), ('m2','service','u1','svc')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	user := []datexport.Equals{{Column: "owner_kind", Value: "user"}}
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "tokens", Source: "test", Table: "tokens",
		Column: "owner_id", Where: user, Mode: datexport.EraseDelete,
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "memos", Source: "test", Table: "memos", Column: "owner_id", Where: user,
		Mode: datexport.EraseAnonymize, ScrubColumns: []string{"body"},
	})
	t.Cleanup(func() {
		datexport.UnregisterEraser("tokens")
		datexport.UnregisterEraser("memos")
	})

	rep, err := app.EraseUserData(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Batteries {
		if (r.Name == "tokens" || r.Name == "memos") && r.RowsAffected != 1 {
			t.Errorf("%s matched %d row(s), want 1", r.Name, r.RowsAffected)
		}
	}
	var ids []string
	rows, err := db.Query(`SELECT id FROM tokens ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if len(ids) != 2 || ids[0] != "t2" || ids[1] != "t3" {
		t.Errorf("tokens left = %v, want [t2 t3]", ids)
	}
	var body, owner string
	if err := db.QueryRow(`SELECT body, owner_id FROM memos WHERE id = 'm2'`).Scan(&body, &owner); err != nil {
		t.Fatal(err)
	}
	if body != "svc" || owner != "u1" {
		t.Errorf("the service account's memo was scrubbed: body %q, owner %q", body, owner)
	}
	if err := db.QueryRow(`SELECT body FROM memos WHERE id = 'm1'`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body == "mine" {
		t.Error("the user's memo was not scrubbed")
	}
}

// A Where column the table lacks fails the erasure: SQLite would read the
// unknown quoted name as a string literal and match nothing.
func TestEraseWhereRequiresTheColumn(t *testing.T) {
	app, db := newEraseTestApp(t)
	if _, err := db.Exec(`CREATE TABLE tokens (id TEXT, owner_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "tokens", Source: "test", Table: "tokens", Column: "owner_id",
		Where: []datexport.Equals{{Column: "owner_kind", Value: "user"}}, Mode: datexport.EraseDelete,
	})
	t.Cleanup(func() { datexport.UnregisterEraser("tokens") })
	if _, err := app.EraseUserData(context.Background(), "u1"); err == nil {
		t.Fatal("an eraser naming a missing Where column ran")
	}
}
