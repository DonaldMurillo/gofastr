package auth

import (
	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
)


func init() { registerCanonicalErasers() }

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
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "auth_passkeys", Source: "auth", Table: "auth_passkeys",
		Column: "user_id", Mode: datexport.EraseDelete,
	})
	datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
		Table: "auth_users", IDColumn: "id", ValueColumn: "email",
		Canonicalize: canonicalEmailOrEmpty(nil),
	})
	for _, p := range []tokenPurpose{purposeMagicLink, purposeEmailCode, purposeReset, purposeVerify, purposePasskeyRegister} {
		registerTokenEraser("magic_link_tokens", p)
	}
}

func resolveEraseTables(mgr *AuthManager) {
	registerMemoryStoreEraser(mgr)
	if us, ok := mgr.UserStore().(*EntityUserStore); ok {
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "auth_users", Source: "auth", Table: us.table,
			Column: us.fieldMap.ID, Mode: datexport.EraseDelete,
		})
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "users_oauth_links", Source: "auth", Table: us.oauthLinksTable(),
			Column: "user_id", Mode: datexport.EraseDelete,
		})
	}
	if us, ok := mgr.UserStore().(*EntityUserStore); ok {
		datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
			Table: us.table, IDColumn: us.fieldMap.ID, ValueColumn: us.fieldMap.Email,
			Canonicalize: canonicalEmailOrEmpty(mgr.config.CanonicalizeEmail),
		})
	} else if r, ok := datexport.ResolveIdentity(datexport.IdentityEmail); ok &&
		(r.Canonicalize == nil || (r.Table == "auth_users" && r.IDColumn == "id" && r.ValueColumn == "email")) {
		r.Canonicalize = canonicalEmailOrEmpty(mgr.config.CanonicalizeEmail)
		datexport.RegisterIdentityResolver(datexport.IdentityEmail, r)
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
	if cfg := mgr.Config().LoginRateLimitPerAccount; cfg != nil {
		if rs, ok := cfg.Store.(*SQLRateLimitStore); ok {
			scope := cfg.Scope
			if scope == "" {
				scope = loginAccountScope
			}
			prefix := ratelimit.NamespaceScope(scope) + "|account:"
			for _, e := range []struct{ name, table string }{
				{"auth_login_limits", rs.table},
				{"auth_login_limit_attempts", rs.table + "_attempts"},
			} {
				datexport.RegisterEraser(datexport.DataEraser{
					Name: e.name, Source: "auth", Table: e.table, Column: "rl_key",
					Mode: datexport.EraseDelete, Identity: datexport.IdentityEmail,
					ValuePrefix: prefix,
				})
			}
		}
	}
	if p, ok := mgr.Plugin("magic-link"); ok {
		if mp, ok := p.(*MagicLinkPlugin); ok {
			if ms, ok := mp.tokenStore.(*SQLMagicLinkTokenStore); ok {
				registerTokenEraser(ms.table, purposeMagicLink)
			}
		}
	}
	if p, ok := mgr.Plugin("email-code"); ok {
		if ep, ok := p.(*EmailCodePlugin); ok {
			if ms, ok := ep.tokenStore.(*SQLMagicLinkTokenStore); ok {
				registerTokenEraser(ms.table, purposeEmailCode)
			}
		}
	}
	if p, ok := mgr.Plugin("password-reset"); ok {
		if rp, ok := p.(*PasswordResetPlugin); ok {
			if ms, ok := rp.store.(*SQLMagicLinkTokenStore); ok {
				registerTokenEraser(ms.table, purposeReset)
			}
		}
	}
	if p, ok := mgr.Plugin("passkey"); ok {
		if pp, ok := p.(*PasskeyPlugin); ok {
			if ps, ok := pp.store.(*SQLPasskeyStore); ok {
				datexport.RegisterEraser(datexport.DataEraser{
					Name: "auth_passkeys", Source: "auth", Table: ps.table,
					Column: "user_id", Mode: datexport.EraseDelete,
				})
			}
			if ms, ok := pp.challenges.(*SQLMagicLinkTokenStore); ok {
				registerTokenEraser(ms.table, purposePasskeyRegister)
			}
		}
	}
	if p, ok := mgr.Plugin("email-verification"); ok {
		if vp, ok := p.(*EmailVerificationPlugin); ok {
			if ms, ok := vp.store.(*SQLMagicLinkTokenStore); ok {
				registerTokenEraser(ms.table, purposeVerify)
			}
		}
	}
}

func canonicalEmailOrEmpty(canonicalize func(string) (string, error)) func(string) string {
	return func(email string) string {
		c, err := canonicalizeEmailWith(canonicalize, email)
		if err != nil {
			return ""
		}
		return c
	}
}

func registerTokenEraser(table string, p tokenPurpose) {
	e := datexport.DataEraser{
		Name: "magic_link_tokens", Source: "auth", Table: table,
		Column: "email", Mode: datexport.EraseDelete,
		ValuePrefix: string(p) + ":",
	}
	if p != purposeMagicLink {
		e.Name = "magic_link_tokens_" + string(p)
	}
	if p == purposeMagicLink || p == purposeEmailCode {
		e.Identity = datexport.IdentityEmail
	}
	datexport.RegisterEraser(e)
}
