package crud

import "github.com/DonaldMurillo/gofastr/framework/internal/mcpname"

// MCPToolNames returns the names RegisterEntityMCPTools registers for
// ch's entity, in registration order: the five CRUD tools, then one per
// routable move under its own key. A namespace on the handler
// (MCPNamespace) names them "<ns>.<entity>.<action>" instead of the flat
// "<entity>_<action>". nil answers nil.
func MCPToolNames(ch *CrudHandler) []string {
	if ch == nil || ch.Entity == nil {
		return nil
	}
	ent := ch.Entity.GetName()
	name := func(action string) string {
		return mcpname.EntityTool(ch.MCPNamespace, ent, action)
	}
	names := []string{
		name("list"),
		name("get"),
		name("create"),
		name("update"),
		name("delete"),
	}
	for _, t := range RoutableTransitions(ch.Entity.Config.States) {
		names = append(names, name(t.Key))
	}
	return names
}
