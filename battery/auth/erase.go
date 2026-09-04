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

func init() { registerCanonicalErasers() }

// registerCanonicalErasers is the init()-time registration under the
// canonical table spellings (auth_users / auth_sessions / auth_twofa /
// users_oauth_links / magic_link_tokens), the erase-plane mirror of
// export.go's registrations. A manager Init'd with the battery's own SQL
// stores under other names re-registers the same Names against the live
// tables (see resolveEraseTables); tests that assert the registry reset
// it and call this to pin the canonical shape.
func registerCanonicalErasers() {
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

// resolveEraseTables re-registers the erasers (and the IdentityEmail
// resolver) against the table names the CONFIGURED stores actually use.
// The init() registrations above target the canonical auth_* spellings;
// the battery's documented wiring (auth.md, agents.md) names the tables
// users/sessions, and a host may pick any name. AuthManager.Init calls
// this after plugins, and the registry is last-writer-wins per Name, so
// the live wiring wins: an erasure under NewEntityUserStore(db, "users")
// reaches "users"/"users_oauth_links"/the resolved IdentityEmail, not the
// skipped auth_* entries. Stores that are not the battery's own SQL
// implementations leave the canonical registrations in place.
func resolveEraseTables(mgr *AuthManager) {
	if us, ok := mgr.UserStore().(*EntityUserStore); ok {
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "auth_users", Source: "auth", Table: us.table,
			Column: us.fieldMap.ID, Mode: datexport.EraseDelete,
		})
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "users_oauth_links", Source: "auth", Table: us.oauthLinksTable(),
			Column: "user_id", Mode: datexport.EraseDelete,
		})
		datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
			Table: us.table, IDColumn: us.fieldMap.ID, ValueColumn: us.fieldMap.Email,
		})
	}
	if ss, ok := mgr.SessionStore().(*EntitySessionStore); ok {
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "auth_sessions", Source: "auth", Table: ss.table,
			Column: "user_id", Mode: datexport.EraseDelete,
		})
	}
	if p, ok := mgr.Plugin("twofa"); ok {
		if tp, ok := p.(*TwoFAPlugin); ok {
			if es, ok := tp.store.(*EntityTwoFAStore); ok {
				datexport.RegisterEraser(datexport.DataEraser{
					Name: "auth_twofa", Source: "auth", Table: es.table,
					Column: "user_id", Mode: datexport.EraseDelete,
				})
			}
		}
	}
	if p, ok := mgr.Plugin("magic-link"); ok {
		if mp, ok := p.(*MagicLinkPlugin); ok {
			if ms, ok := mp.tokenStore.(*SQLMagicLinkTokenStore); ok {
				datexport.RegisterEraser(datexport.DataEraser{
					Name: "magic_link_tokens", Source: "auth", Table: ms.table,
					Column: "email", Mode: datexport.EraseDelete,
					Identity: datexport.IdentityEmail,
				})
			}
		}
	}
}
