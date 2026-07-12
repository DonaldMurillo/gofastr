package auth

import "github.com/DonaldMurillo/gofastr/framework/datexport"

// The auth battery owns physical tables that live OUTSIDE the framework entity
// registry (created with raw DDL in entity_store.go). Registering them here
// from init() — mirroring framework/agentsinv — means any app that imports
// battery/auth has its auth tables included in App.ExportData, so a data
// dump/restore is complete. The framework centralizes all raw read/write
// behind one SafeIdent-guarded path; this registration is purely declarative.
//
// Two tables are registered under their canonical names:
//
//   - auth_users: the user table backing EntityUserStore
//     (id, email, password_hash, roles, password_set).
//   - auth_sessions: the session table backing EntitySessionStore
//     (id, token, user_id, created_at, expires_at,
//     two_factor_verified, pending_two_factor).
//
// The user table name is host-configured (commonly "users" or "auth_users"),
// and the session table name is host-configured too. These registrations cover
// the canonical names; a host that renamed either must datexport.Register the
// actual name, or the canonical entry is skipped with a note at export time
// and that table is excluded. A user table that is ALSO a registry entity is
// already covered by the registry walk; collectSources dedups by table so it
// is never exported twice.

func init() {
	datexport.Register(datexport.DataExporter{
		Name:       "auth_users",
		Source:     "auth",
		Table:      "auth_users",
		PrimaryKey: "id",
		Columns: []string{
			"id",
			"email",
			"password_hash",
			"roles",
			"password_set",
		},
	})
	datexport.Register(datexport.DataExporter{
		Name:       "auth_sessions",
		Source:     "auth",
		Table:      "auth_sessions",
		PrimaryKey: "id",
		Columns: []string{
			"id",
			"token",
			"user_id",
			"created_at",
			"expires_at",
			"two_factor_verified",
			"pending_two_factor",
		},
	})
}
