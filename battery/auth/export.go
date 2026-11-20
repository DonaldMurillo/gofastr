package auth

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)


func init() { registerCanonicalExporters() }

func registerCanonicalExporters() {
	registerUserExporters("auth_users", "users_oauth_links", DefaultUserFieldMap())
	registerSessionExporter("auth_sessions")
	registerTwoFAExporter("auth_twofa")
	registerPasskeyExporter("auth_passkeys")
}

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
