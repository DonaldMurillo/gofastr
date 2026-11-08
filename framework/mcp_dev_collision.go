package framework

import (
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func entityMCPToolNames(ent *entity.Entity, m registrationMount) []string {
	actions := []string{"list", "get", "create", "update", "delete"}
	for _, move := range crud.RoutableTransitions(ent.Config.States) {
		actions = append(actions, move.Key)
	}
	names := make([]string, 0, len(actions))
	for _, action := range actions {
		names = append(names, m.entityTool(ent.GetName(), action))
	}
	return names
}

// Keep provenance for dev-only reads too: those intentionally survive the
// bind withdrawal, so core/mcp's mutating-tool marker cannot identify them.
func (a *App) rememberDevEntityMCPTools(ent *entity.Entity, m registrationMount) {
	if ent.Config.Exposure.MCP {
		return
	}
	for _, name := range entityMCPToolNames(ent, m) {
		a.devEntityMCPTools.Store(name, true)
	}
}

func (a *App) skipDevEntityMCPCollision(name string) bool {
	if _, devOnly := a.devEntityMCPTools.Load(name); !devOnly || !a.MCP.HasTool(name) {
		return false
	}
	a.Logger().Warn("MCP built-in tool skipped: name belongs to a dev-implied entity tool", "tool", name)
	return true
}
