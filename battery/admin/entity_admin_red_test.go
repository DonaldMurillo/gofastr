//go:build red

package admin

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// Property: when the save path cannot recompute the masked-field set it fails CLOSED (every editable column treated as masked), exactly as the read path's documented contract does.
// Surfaces: battery/admin/entity_admin.go:maskedFieldsForID, battery/admin/entity_admin.go:entitySave
// Finding: maskedFieldsForID returns nil when its GetOne fails (entity_admin.go:536-539) and entitySave passes that nil into formToJSON (:570), so a blank masked write-only bool is emitted as false and flips the stored column even though the UPDATE itself commits; the read path fails closed on the same condition (entity_admin.go:501-509) and entity_edit_masked_test.go::TestEditFormFailsClosedWhenTheHookErrors pins that leg only.
// Fix direction: mirror maskedFields' fail-closed contract in maskedFieldsForID — on a read failure mark every editable column masked instead of returning nil.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// redFailReads is the fault switch for redFaultConnector: while set, every
// SELECT on the wrapped connection fails. That is the transient read failure
// maskedFieldsForID must survive, while writes (INSERT / UPDATE ... RETURNING)
// still commit — the asymmetry the finding needs. Connection-wrapper fault
// injection is established repo practice (framework/crud/cov_faultdriver_test.go,
// framework/migrate/sqlite_faildriver_test.go); those live in package-local
// test files, so a minimal copy lives here.
var redFailReads bool

// redFaultConnector opens the repo's registered "sqlite3" driver (imported by
// admin_test.go) through a wrapper, without a global sql.Register.
type redFaultConnector struct {
	inner driver.Driver
	dsn   string
}

func (c redFaultConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.inner.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return redFaultConn{Conn: conn}, nil
}

func (c redFaultConnector) Driver() driver.Driver { return c.inner }

// redFaultConn embeds the real conn (Prepare/Close/Begin pass through) and
// fails SELECTs while redFailReads is set; everything else flows through.
type redFaultConn struct{ driver.Conn }

func (c redFaultConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if redFailReads && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "SELECT") {
		return nil, errors.New("red test: transient read failure")
	}
	if q, ok := c.Conn.(driver.QueryerContext); ok {
		return q.QueryContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func TestEntitySaveRedMaskRecomputeFailClosed(t *testing.T) {
	base, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open base: %v", err)
	}
	defer base.Close()
	db := sql.OpenDB(redFaultConnector{inner: base.Driver(), dsn: ":memory:"})
	db.SetMaxOpenConns(1)
	defer db.Close()

	// is_admin is the masked write-only column (hook redacts it on reads);
	// enabled is an ordinary bool so the blank-bool emission has somewhere
	// honest to land besides the column under test.
	cfg := entity.EntityConfig{
		Table: "accts",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "is_admin", Type: schema.Bool},
			{Name: "enabled", Type: schema.Bool},
		},
	}.WithTimestamps(false)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"accts": cfg})
	app.HookRegistry("accts").RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		p.Result["isAdmin"] = false // the mask
		return nil
	})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"accts"}}, testUser{"u1"})

	postForm(h, "/admin/e/accts/_create", url.Values{
		"name": {"root"}, "is_admin": {"on"}, "enabled": {"on"},
	})
	id := firstID(t, db, "accts")
	var isAdmin bool
	if err := db.QueryRow(`SELECT is_admin FROM accts WHERE id=?`, id).Scan(&isAdmin); err != nil {
		t.Fatal(err)
	}
	if !isAdmin {
		t.Fatalf("precondition: stored is_admin must be true before the edit")
	}

	// Fail reads for the save: the maskedFieldsForID recompute GetOne errors
	// while the subsequent UPDATE (a write statement) still commits.
	redFailReads = true
	rr := postForm(h, "/admin/e/accts/_update/"+id, url.Values{
		"name": {"root2"}, "is_admin": {""}, "enabled": {""},
	})
	redFailReads = false

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("update status = %d, want 303 (the write must commit for the finding to bite): %s",
			rr.Code, rr.Body.String())
	}
	var name string
	if err := db.QueryRow(`SELECT name, is_admin FROM accts WHERE id=?`, id).Scan(&name, &isAdmin); err != nil {
		t.Fatal(err)
	}
	if name != "root2" {
		t.Fatalf("the edit did not apply (name=%q); the fixture no longer exercises the finding", name)
	}
	if !isAdmin {
		t.Errorf("SECURITY: [admin] the masked-field recompute failed and entitySave failed OPEN: " +
			"a blank masked is_admin was emitted as false and flipped the stored column, where the " +
			"read path's contract treats every editable column as masked when the set cannot be " +
			"recomputed")
	}
}
