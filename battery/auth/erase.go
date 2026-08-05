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
//     hard-delete is both safe and the stronger erasure.
//   - auth_twofa: EraseDelete by user_id. TOTP secrets and backup codes are
//     credential material; leaving them behind survives the erasure.
//   - users_oauth_links: EraseDelete by user_id, the canonical name for the
//     "<user table>_oauth_links" convention. A surviving link re-attaches the
//     erased account on the next provider sign-in.
//
// KNOWN GAP — magic_link_tokens is keyed by EMAIL, not user id, so the
// declarative eraser (which receives only the user id) cannot reach it. A
// magic link minted before an erasure and redeemed after it creates a fresh
// account for that address. Hosts that use magic links should expire
// outstanding tokens for the address alongside the erasure. Tracked
// separately; do not assume erasure revokes an in-flight magic link.
//
// API tokens (auth_api_tokens) are named credentials, not per-user rows —
// the table has no user column — so a host that scopes tokens to users
// registers its own eraser for the actual column.
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
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "auth_twofa", Source: "auth", Table: "auth_twofa",
		Column: "user_id", Mode: datexport.EraseDelete,
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "users_oauth_links", Source: "auth", Table: "users_oauth_links",
		Column: "user_id", Mode: datexport.EraseDelete,
	})
}
