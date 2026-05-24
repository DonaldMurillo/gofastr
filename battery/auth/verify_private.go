package auth

import (
	"log/slog"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// VerifyAuthEntitiesPrivate checks the named auth entities in `reg` and
// emits a WARN log if any are registered with CRUD enabled. The auth
// tables (users, sessions) hold password hashes and session tokens —
// exposing them via auto-CRUD is a footgun. The auto-private helpers
// UserEntityConfig() / SessionEntityConfig() set CRUD=false / MCP=false;
// hosts that still wire the bare UserEntityFields() / SessionEntityFields()
// fall back to the dangerous default.
//
// Call once at startup after registering entities:
//
//	app.Entity("users",    auth.UserEntityConfig())
//	app.Entity("sessions", auth.SessionEntityConfig())
//	auth.VerifyAuthEntitiesPrivate(app.Registry, "users", "sessions", nil)
//
// usersName / sessionsName are the entity names — pass "" to skip
// either. logger=nil uses slog.Default. Unknown entity names are
// silently skipped (the entity may not be registered yet, or a
// different name was used).
func VerifyAuthEntitiesPrivate(reg entity.Registry, usersName, sessionsName string, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	for _, name := range []string{usersName, sessionsName} {
		if name == "" {
			continue
		}
		ent, err := reg.Get(name)
		if err != nil || ent == nil {
			// Don't silently skip — the host explicitly passed this
			// name and expects an audit. A missing entity is almost
			// always "VerifyAuthEntitiesPrivate called before
			// app.Entity(...)" — the wrong order produces false
			// confidence. Log so the operator sees it.
			logger.Warn("auth: VerifyAuthEntitiesPrivate called for entity not registered yet — call AFTER app.Entity(...) (or after app.InitPlugins) so the check sees the actual config",
				"entity", name)
			continue
		}
		// CRUD=nil means "auto" (defaults to true when DB is set).
		// CRUD=&true is explicit on.
		// CRUD=&false is the safe state.
		crudOn := ent.Config.CRUD == nil || (ent.Config.CRUD != nil && *ent.Config.CRUD)
		mcpOn := ent.Config.MCP
		if !crudOn && !mcpOn {
			continue
		}
		logger.Warn("auth entity exposed via auto-CRUD/MCP — switch to auth.UserEntityConfig() / auth.SessionEntityConfig() to make it private by default",
			"entity", name,
			"crud_enabled", crudOn,
			"mcp_enabled", mcpOn,
			"fix", "app.Entity(\""+name+"\", auth.UserEntityConfig())")
	}
}
