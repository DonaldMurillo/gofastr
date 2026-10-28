package auth

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// The auth battery owns physical tables that live OUTSIDE the framework entity
// registry (created with raw DDL in entity_store.go). Registering them here
// from init(), mirroring framework/agentsinv, means any app that imports
// battery/auth has its auth tables included in App.ExportData, so a data
// dump/restore is complete. The framework centralizes all raw read/write
// behind one SafeIdent-guarded path; this registration is purely declarative.
//
// The exporters mirror the erasers in erase.go, table for table, so a
// restore keeps everything an erasure would remove:
//
//   - auth_users: the user table backing EntityUserStore (id, email,
//     password_hash, roles, password_set, email_verified, name).
//   - auth_sessions: the session table backing EntitySessionStore.
//   - auth_twofa: TOTP enrolment backing EntityTwoFAStore. Without it a
//     restored 2FA account signs in with its password alone.
//   - users_oauth_links: the "<user table>_oauth_links" table. Without it
//     an account that only ever signed in through a provider is locked out.
//   - auth_passkeys: the passkey table backing SQLPasskeyStore. A restore
//     without it would make every user enrol their passkeys again.
//
// magic_link_tokens is left out on purpose. Its rows are short-lived
// credentials (login links, reset links, verification and passkey
// challenges); a restored row would be a live link minted for a past
// session, and losing them costs a user one more email.
//
// init() registers the canonical table names. The battery's documented
// wiring names its tables users/sessions, and a host may pick any name, so
// AuthManager.Init re-registers the same Names against the tables the
// configured stores actually use (resolveExportTables, beside
// resolveEraseTables). A host on its own store implementations registers
// its tables itself. A user table that is ALSO a registry entity is
// already covered by the registry walk; collectSources dedups by table so
// it is never exported twice.

func init() { registerCanonicalExporters() }

// registerCanonicalExporters is the init()-time registration under the
// canonical table spellings. Tests that assert the registry reset it and
// call this to pin the canonical shape.
func registerCanonicalExporters() {
	// The link table's canonical name is the erase plane's: the
	// "<user table>_oauth_links" convention under the documented "users".
	registerUserExporters("auth_users", "users_oauth_links", DefaultUserFieldMap())
	registerSessionExporter("auth_sessions")
	registerTwoFAExporter("auth_twofa")
	registerPasskeyExporter("auth_passkeys")
}

// resolveExportTables re-registers the exporters against the table names
// the CONFIGURED stores use. AuthManager.Init calls it after plugins, with
// resolveEraseTables; the registry is last-writer-wins per Name, so the
// live wiring wins. Stores that are not the battery's own SQL
// implementations leave the canonical registrations in place.
func resolveExportTables(mgr *AuthManager) {
	if us, ok := mgr.UserStore().(*EntityUserStore); ok {
		registerUserExporters(us.table, us.oauthLinksTable(), us.fieldMap)
	}
	if ss, ok := mgr.SessionStore().(*EntitySessionStore); ok {
		registerSessionExporter(ss.table)
	}
	if p, ok := mgr.Plugin("twofa"); ok {
		if tp, ok := p.(*TwoFAPlugin); ok {
			if es, ok := tp.store.(*EntityTwoFAStore); ok {
				registerTwoFAExporter(es.table)
			}
		}
	}
	if p, ok := mgr.Plugin("passkey"); ok {
		if pp, ok := p.(*PasskeyPlugin); ok {
			if ps, ok := pp.store.(*SQLPasskeyStore); ok {
				registerPasskeyExporter(ps.table)
			}
		}
	}
}

// registerUserExporters registers the user table and its OAuth link table.
func registerUserExporters(table, linksTable string, fm UserFieldMap) {
	datexport.Register(datexport.DataExporter{
		Name: "auth_users", Source: "auth", Table: table, PrimaryKey: fm.ID,
		Columns: []string{fm.ID, fm.Email, fm.PasswordHash, fm.Roles, fm.PasswordSet, fm.EmailVerified, fm.Name},
	})
	datexport.Register(datexport.DataExporter{
		Name: "users_oauth_links", Source: "auth", Table: linksTable,
		PrimaryKey: "provider,provider_id",
		Columns:    []string{"provider", "provider_id", "user_id", "email", "name", "avatar_url", "created_at"},
	})
}

func registerSessionExporter(table string) {
	datexport.Register(datexport.DataExporter{
		Name: "auth_sessions", Source: "auth", Table: table, PrimaryKey: "id",
		Columns: []string{"id", "token", "user_id", "created_at", "expires_at", "two_factor_verified", "pending_two_factor"},
	})
}

func registerTwoFAExporter(table string) {
	datexport.Register(datexport.DataExporter{
		Name: "auth_twofa", Source: "auth", Table: table, PrimaryKey: "user_id",
		Columns: []string{"user_id", "enabled", "secret", "backup_codes", "verified", "last_used_step", "version"},
	})
}

func registerPasskeyExporter(table string) {
	datexport.Register(datexport.DataExporter{
		Name: "auth_passkeys", Source: "auth", Table: table, PrimaryKey: "id",
		Columns: strings.Split(passkeyColumns, ", "),
	})
}
