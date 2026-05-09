package live

import (
	"fmt"

	"github.com/gofastr/gofastr/kiln/journal"
)

// summarizeEntry builds a one-line, human-readable digest of e for the
// SSE Event.Summary field. The widget shows it in the synthetic system
// row alongside the op name (e.g. "✦ add_entity name=posts fields=3"),
// so users see what changed without expanding raw JSON.
//
// Returns "" for kinds/ops with no useful one-liner — the widget just
// shows the bare op name in that case.
func summarizeEntry(e journal.Entry) string {
	if e.Kind != journal.KindWorldEdit {
		return ""
	}
	switch e.Op {
	case journal.OpAddEntity:
		var p journal.AddEntityPayload
		if err := e.Decode(&p); err == nil && p.Entity != nil {
			return fmt.Sprintf("name=%s fields=%d", p.Entity.Name, len(p.Entity.Fields))
		}
	case journal.OpUpdateEntity:
		var p journal.UpdateEntityPayload
		if err := e.Decode(&p); err == nil && p.Entity != nil {
			return fmt.Sprintf("name=%s fields=%d", p.Entity.Name, len(p.Entity.Fields))
		}
	case journal.OpDeleteEntity:
		var p journal.DeleteEntityPayload
		if err := e.Decode(&p); err == nil {
			return "name=" + p.Name
		}
	case journal.OpAddField:
		var p journal.AddFieldPayload
		if err := e.Decode(&p); err == nil {
			return fmt.Sprintf("%s.%s:%s", p.Entity, p.Field.Name, p.Field.Type)
		}
	case journal.OpDeleteField:
		var p journal.DeleteFieldPayload
		if err := e.Decode(&p); err == nil {
			return p.Entity + "." + p.Field
		}
	case journal.OpAddPage:
		var p journal.AddPagePayload
		if err := e.Decode(&p); err == nil && p.Page != nil {
			return "path=" + p.Page.Path
		}
	case journal.OpDeletePage:
		var p journal.DeletePagePayload
		if err := e.Decode(&p); err == nil {
			return "path=" + p.Path
		}
	case journal.OpAddHook:
		var p journal.AddHookPayload
		if err := e.Decode(&p); err == nil && p.Hook != nil {
			return fmt.Sprintf("id=%s %s/%s", p.Hook.ID, p.Hook.Entity, p.Hook.When)
		}
	case journal.OpDeleteHook:
		var p journal.DeleteHookPayload
		if err := e.Decode(&p); err == nil {
			return "id=" + p.ID
		}
	case journal.OpAddRoute:
		var p journal.AddRoutePayload
		if err := e.Decode(&p); err == nil && p.Route != nil {
			return fmt.Sprintf("%s %s", p.Route.Method, p.Route.Path)
		}
	case journal.OpDeleteRoute:
		var p journal.DeleteRoutePayload
		if err := e.Decode(&p); err == nil {
			return fmt.Sprintf("%s %s", p.Method, p.Path)
		}
	case journal.OpAddSeed:
		var p journal.AddSeedPayload
		if err := e.Decode(&p); err == nil && p.Seed != nil {
			return fmt.Sprintf("entity=%s rows=%d", p.Seed.Entity, len(p.Seed.Rows))
		}
	}
	return ""
}
