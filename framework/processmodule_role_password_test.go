package framework

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
)

// TestModuleSchemaRoleStmtsResetPasswordOnReprovision pins the invariant that
// a re-provisioned module role ends up with the password the coordinator is
// about to authenticate with.
//
// The bug this replaces was a production defect, not a test artifact. Roles in
// Postgres are CLUSTER-scoped, so a role created by an earlier deploy still
// exists on the next one. Provisioning creates it inside
//
//	DO $$ BEGIN CREATE ROLE … PASSWORD '<new>' …;
//	EXCEPTION WHEN duplicate_object THEN null; END $$
//
// which is idempotent for existence and silently a no-op for the PASSWORD: on
// the second deploy the CREATE raises duplicate_object, the handler swallows
// it, and the role keeps its ORIGINAL password. The following ALTER ROLE
// re-asserted every privilege flag but not the password, so the coordinator
// then connected as the role with a freshly generated secret and got
// `password authentication failed for user "module_demo_role" (28P01)` —
// every process-module migration failing from the second deploy onward.
//
// It stayed invisible because CI gave each test process a throwaway Postgres
// container, where the role never pre-existed. It surfaced the moment all
// tests began sharing one server.
//
// Asserted at the statement level rather than against a live Postgres so the
// check runs in the ordinary unit lane — no Docker, no skip. The end-to-end
// proof is TestCoord_PG_ModuleSchemaIsolation, which needs a real server.
func TestModuleSchemaRoleStmtsResetPasswordOnReprovision(t *testing.T) {
	const pw = "deadbeefcafe0123456789ab"
	stmts := moduleSchemaRoleStmts("module_demo", "module_demo_role", pw)

	createAt, alterAt := -1, -1
	for i, s := range stmts {
		if !strings.Contains(s, pw) {
			continue
		}
		switch {
		// The CREATE lives inside a DO $$ … $$ block whose exception handler
		// makes it a no-op for a role that already exists.
		case strings.Contains(s, "CREATE ROLE"):
			createAt = i
		case strings.Contains(s, "ALTER ROLE"):
			alterAt = i
		}
	}

	if createAt < 0 {
		t.Error("no CREATE ROLE statement carries the password — a first-time provision would create a role nobody can log in as")
	}
	if alterAt < 0 {
		t.Error("the password appears ONLY in the swallowed CREATE ROLE. " +
			"A role that already exists (any redeploy, since roles are cluster-scoped) keeps its old password, " +
			"and the coordinator's very next step — connecting as that role — fails with 28P01. " +
			"Re-assert it with ALTER ROLE … PASSWORD alongside the privilege flags.")
	}
	// Order matters, and only the slice order encodes it:
	// execModuleSchemaRoleStmts runs these sequentially, so an ALTER placed
	// before the CREATE would error on a role that does not exist yet and
	// break FIRST-time provisioning — the opposite of the bug above, and
	// invisible to a test that merely checks both statements are present.
	if createAt >= 0 && alterAt >= 0 && createAt >= alterAt {
		t.Errorf("CREATE ROLE is at index %d and ALTER ROLE at %d — the CREATE must come first, or first-time provisioning alters a role that does not exist yet", createAt, alterAt)
	}
}

// The privilege flags must survive a re-provision too: the whole point of the
// role is that it cannot reach outside its own schema, and a role left over
// from an older release may have been granted more.
func TestModuleSchemaRoleStmtsReassertPrivilegeFence(t *testing.T) {
	stmts := moduleSchemaRoleStmts("module_demo", "module_demo_role", "pw")
	joinedAlters := ""
	for _, s := range stmts {
		if strings.Contains(s, "ALTER ROLE") {
			joinedAlters += s + "\n"
		}
	}
	for _, flag := range []string{"NOSUPERUSER", "NOCREATEDB", "NOCREATEROLE", "NOINHERIT"} {
		if !strings.Contains(joinedAlters, flag) {
			t.Errorf("no ALTER ROLE re-asserts %s — a pre-existing role keeps whatever rights it had", flag)
		}
	}
	// The fence itself is a REVOKE, not an ALTER; assert it is still emitted.
	fenced := false
	for _, s := range stmts {
		if strings.Contains(s, "REVOKE ALL ON SCHEMA public") {
			fenced = true
		}
	}
	if !fenced {
		t.Error("the public-schema REVOKE is missing — that statement is the actual privilege fence")
	}
}

// The wrap in execModuleSchemaRoleStmts quotes the failing statement's first
// line. The ALTER ROLE that re-asserts the password is a single line, so
// without redaction an exec failure hands the live credential to whatever
// logs the returned error. Driven through a driver that fails every exec so
// each statement's wrapped error is observed, not just the first one's.
func TestModuleSchemaRoleExecErrDoesNotLeakPassword(t *testing.T) {
	const pw = "deadbeefcafe0123456789ab"
	db := sql.OpenDB(failConnector{})
	defer db.Close()
	for _, s := range moduleSchemaRoleStmts("module_demo", "module_demo_role", pw) {
		err := execModuleSchemaRoleStmts(context.Background(), db, []string{s})
		if err == nil {
			t.Fatal("fail-driver exec unexpectedly succeeded")
		}
		if strings.Contains(err.Error(), pw) {
			t.Errorf("exec error leaks the role password: %v", err)
		}
	}
}

// failConnector hands out no connections, so every ExecContext errors and the
// statement-quoting wrap path runs for the statement under test.
type failConnector struct{}

func (failConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("provision exec refused by test driver")
}
func (failConnector) Driver() driver.Driver { return nil }
