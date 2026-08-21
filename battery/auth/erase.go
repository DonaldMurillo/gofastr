package auth

import "github.com/DonaldMurillo/gofastr/framework/datexport"

// The auth battery's right-to-be-forgotten registrations, the erase-plane
// mirror of export.go. Registering from init() means any app that imports
// battery/auth has its auth tables reached by App.EraseUserData, so a user
// erasure is complete. The framework centralizes all raw write behind one
// SafeIdent-guarded path; these registrations are purely declarative.
//
// These erasers are registered under the canonical table names:
//
//   - auth_sessions: EraseDelete by user_id. A user's sessions are pure
//     credential state, they are hard-deleted, revoking every active session
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
//   - magic_link_tokens: EraseDelete by EMAIL. The token table is keyed by
//     email, not user id, so the eraser declares IdentityEmail and the
//     framework resolves the user's email from the user table at erase time
//     (see the IdentityEmail registration below) and binds it instead of the
//     user id. This closes the gap where a magic link minted before an erasure
//     and redeemed after it re-created the erased account.
//
// API tokens (auth_api_tokens) are named credentials, not per-user rows,
// the table has no user column, so a host that scopes tokens to users
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
// renamed either must datexport.RegisterEraser the actual name (and, for a
// renamed user table, re-register the IdentityEmail resolver against it), or
// the canonical entry is skipped with a note at erase time and that table is
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
	// IdentityEmail resolves the erased user's email from the user table
	// (auth_users.id → email) ONCE per erasure, before the tx. The
	// magic-link token table is keyed by email, so its eraser declares
	// IdentityEmail and the framework binds the resolved email instead of
	// the user id, closing the gap where an in-flight magic link survived
	// an erasure and re-created the account. The token table is created
	// lazily by NewSQLMagicLinkTokenStore; a host on the in-memory store has
	// no such table, so this eraser is a no-op (skipped at erase time).
	datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
		Table: "auth_users", IDColumn: "id", ValueColumn: "email",
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "magic_link_tokens", Source: "auth", Table: "magic_link_tokens",
		Column: "email", Mode: datexport.EraseDelete,
		Identity: datexport.IdentityEmail,
	})
}
