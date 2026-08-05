package auth

import "github.com/DonaldMurillo/gofastr/framework/datexport"

// The auth battery's right-to-be-forgotten registrations — the erase-plane
// mirror of export.go. Registering from init() means any app that imports
// battery/auth has its auth tables reached by App.EraseUserData, so a user
// erasure is complete. The framework centralizes all raw write behind one
// SafeIdent-guarded path; these registrations are purely declarative.
//
// Two erasers are registered under the canonical table names:
//
//   - auth_sessions: EraseDelete by user_id. A user's sessions are pure
//     credential state — they are hard-deleted, revoking every active session
//     as part of the erasure.
//   - auth_users: EraseDelete by id (the primary key IS the user id). The auth
//     battery's tables are created with raw DDL and carry NO foreign-key
//     constraints, so hard-deleting the user row cannot cascade-fail; an
//     anonymizing tombstone would leave a login-oracle row behind, so
//     hard-delete is both safe and the stronger erasure. (Other auth tables —
//     twofa, oauth_links, apitokens — are NOT registered here: they are not in
//     the export registry either, and an app that wants them erased registers
//     its own eraser for the actual table name, exactly as it must for export.)
//
// The audit trail is deliberately NOT registered as an eraser. It is the
// framework's table (audit_log), host-configurable via AuditConfig.Table, and
// App.EraseUserData anonymizes it built-in: actor_id = userID → "[erased]",
// rows and record_id retained. See framework/docs/content/data-export.md →
// "Data erasure".
//
// The table names are host-configured (commonly "users"/"auth_users" for the
// user table). These registrations cover the canonical names; a host that
// renamed either must datexport.RegisterEraser the actual name, or the
// canonical entry is skipped with a note at erase time and that table is
// excluded from the erasure.

func init() {
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "auth_sessions", Source: "auth", Table: "auth_sessions",
		Column: "user_id", Mode: datexport.EraseDelete,
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "auth_users", Source: "auth", Table: "auth_users",
		Column: "id", Mode: datexport.EraseDelete,
	})
}
