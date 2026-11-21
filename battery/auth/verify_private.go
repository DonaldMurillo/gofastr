package auth

import (
	"log/slog"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// VerifyAuthEntitiesPrivate logs a WARN for every version of the named auth entities registered with CRUD or MCP on, and for a name not registered.
func VerifyAuthEntitiesPrivate(reg entity.Registry, usersName, sessionsName string, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	for _, name := range []string{usersName, sessionsName} {
		if name == "" {
			continue
		}
		versions := versionsNamed(reg, name)
		if len(versions) == 0 {
			logger.Warn("auth: VerifyAuthEntitiesPrivate called for entity not registered yet: call AFTER app.Entity(...) (or after app.InitPlugins) so the check sees the actual config",
				"entity", name)
			continue
		}
		for _, ent := range versions {
			crudOn := ent.Config.Exposure.CRUD == nil || (ent.Config.Exposure.CRUD != nil && *ent.Config.Exposure.CRUD)
			mcpOn := ent.Config.Exposure.MCP
			if !crudOn && !mcpOn {
				continue
			}
			attrs := []any{
				"entity", name,
				"crud_enabled", crudOn,
				"mcp_enabled", mcpOn,
				"fix", "app.Entity(\"" + name + "\", auth.UserEntityConfig())",
			}
			if ent.Version != "" {
				attrs = append(attrs, "version", ent.Version)
			}
			logger.Warn("auth entity exposed via auto-CRUD/MCP: switch to auth.UserEntityConfig() / auth.SessionEntityConfig() to make it private by default",
				attrs...)
		}
	}
}

func versionsNamed(reg entity.Registry, name string) []*entity.Entity {
	var out []*entity.Entity
	for _, ent := range reg.AllSorted() {
		if ent != nil && ent.Config.Name == name {
			out = append(out, ent)
		}
	}
	return out
}
