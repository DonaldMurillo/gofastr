package journal

import (
	"fmt"

	"github.com/gofastr/gofastr/kiln/world"
)

// Replay reads every entry from j and applies it to a fresh Session.
// Replay is deterministic: equal entry sequences yield equal Sessions.
func Replay(j Journal) (*Session, error) {
	entries, err := j.Read()
	if err != nil {
		return nil, fmt.Errorf("journal: read for replay: %w", err)
	}
	return ReplayEntries(entries)
}

// ReplayEntries applies a slice of entries to a fresh Session. Useful when
// the caller already has the entries in memory.
func ReplayEntries(entries []Entry) (*Session, error) {
	s := NewSession()
	for i, e := range entries {
		if err := Apply(s, e); err != nil {
			return nil, fmt.Errorf("journal: apply entry %d (id=%s kind=%s op=%s): %w", i+1, e.ID, e.Kind, e.Op, err)
		}
	}
	return s, nil
}

// Apply applies a single Entry to a Session. Exposed so the live mutator
// (Phase 2) can share the same dispatch table used by replay.
func Apply(s *Session, e Entry) error {
	switch e.Kind {
	case KindWorldEdit:
		return applyWorldEdit(s.World, e)
	case KindChatUser, KindChatAssistant:
		var p ChatMessagePayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		s.Chat = append(s.Chat, ChatEvent{
			EntryID:   e.ID,
			Timestamp: e.Timestamp,
			Kind:      e.Kind,
			Message:   &p,
		})
		return nil
	case KindToolCall:
		var p ToolCallPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		s.Chat = append(s.Chat, ChatEvent{
			EntryID:   e.ID,
			Timestamp: e.Timestamp,
			Kind:      e.Kind,
			Call:      &p,
		})
		return nil
	case KindToolResult:
		var p ToolResultPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		s.Chat = append(s.Chat, ChatEvent{
			EntryID:   e.ID,
			Timestamp: e.Timestamp,
			Kind:      e.Kind,
			Result:    &p,
		})
		return nil
	case KindPlanProposed:
		var p PlanProposedPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		s.Plans[p.PlanID] = &Plan{
			PlanID:     p.PlanID,
			ProposedAt: e.Timestamp,
			Steps:      append([]string(nil), p.Steps...),
			Reason:     p.Reason,
			Targets:    append([]PlanTarget(nil), p.Targets...),
		}
		return nil
	case KindPlanApproved:
		var p PlanApprovedPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		plan, ok := s.Plans[p.PlanID]
		if !ok {
			return fmt.Errorf("plan_approved references unknown plan_id %q", p.PlanID)
		}
		if plan.Rejected {
			return fmt.Errorf("plan_approved on already-rejected plan %q", p.PlanID)
		}
		plan.Approved = true
		plan.ApprovedAt = e.Timestamp
		plan.Modified = p.Modified
		return nil
	case KindPlanRejected:
		var p PlanRejectedPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		plan, ok := s.Plans[p.PlanID]
		if !ok {
			return fmt.Errorf("plan_rejected references unknown plan_id %q", p.PlanID)
		}
		if plan.Approved {
			return fmt.Errorf("plan_rejected on already-approved plan %q", p.PlanID)
		}
		plan.Rejected = true
		plan.RejectedAt = e.Timestamp
		plan.RejectReason = p.Reason
		return nil
	default:
		return fmt.Errorf("unknown entry kind %q", e.Kind)
	}
}

// applyWorldEdit dispatches by Op.
func applyWorldEdit(w *world.World, e Entry) error {
	switch e.Op {
	case OpSetAppConfig:
		var p SetAppConfigPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		w.App = p.Config
		return nil

	case OpAddEntity:
		var p AddEntityPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if p.Entity == nil {
			return fmt.Errorf("add_entity: nil entity")
		}
		if p.Entity.Name == "" {
			return fmt.Errorf("add_entity: entity has empty name")
		}
		if _, exists := w.Entities[p.Entity.Name]; exists {
			return fmt.Errorf("add_entity: %q already exists", p.Entity.Name)
		}
		w.Entities[p.Entity.Name] = p.Entity
		return nil

	case OpUpdateEntity:
		var p UpdateEntityPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if p.Entity == nil {
			return fmt.Errorf("update_entity: nil entity")
		}
		if _, exists := w.Entities[p.Entity.Name]; !exists {
			return fmt.Errorf("update_entity: %q not found", p.Entity.Name)
		}
		w.Entities[p.Entity.Name] = p.Entity
		return nil

	case OpDeleteEntity:
		var p DeleteEntityPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if _, exists := w.Entities[p.Name]; !exists {
			return fmt.Errorf("delete_entity: %q not found", p.Name)
		}
		delete(w.Entities, p.Name)
		return nil

	case OpAddField:
		var p AddFieldPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		ent, ok := w.Entities[p.Entity]
		if !ok {
			return fmt.Errorf("add_field: entity %q not found", p.Entity)
		}
		for _, f := range ent.Fields {
			if f.Name == p.Field.Name {
				return fmt.Errorf("add_field: %s.%s already exists", p.Entity, p.Field.Name)
			}
		}
		ent.Fields = append(ent.Fields, p.Field)
		return nil

	case OpDeleteField:
		var p DeleteFieldPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		ent, ok := w.Entities[p.Entity]
		if !ok {
			return fmt.Errorf("delete_field: entity %q not found", p.Entity)
		}
		for i, f := range ent.Fields {
			if f.Name == p.Field {
				ent.Fields = append(ent.Fields[:i], ent.Fields[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("delete_field: %s.%s not found", p.Entity, p.Field)

	case OpAddPage:
		var p AddPagePayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if p.Page == nil {
			return fmt.Errorf("add_page: nil page")
		}
		if p.Page.Path == "" {
			return fmt.Errorf("add_page: empty path")
		}
		if _, exists := w.Pages[p.Page.Path]; exists {
			return fmt.Errorf("add_page: %q already exists", p.Page.Path)
		}
		w.Pages[p.Page.Path] = p.Page
		return nil

	case OpDeletePage:
		var p DeletePagePayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if _, exists := w.Pages[p.Path]; !exists {
			return fmt.Errorf("delete_page: %q not found", p.Path)
		}
		delete(w.Pages, p.Path)
		return nil

	case OpUpdatePageElement:
		var p UpdatePageElementPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if p.New == nil {
			return fmt.Errorf("update_page_element: nil new page")
		}
		if _, exists := w.Pages[p.Path]; !exists {
			return fmt.Errorf("update_page_element: page %q not found", p.Path)
		}
		w.Pages[p.Path] = p.New
		return nil

	case OpAddHook:
		var p AddHookPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if p.Hook == nil || p.Hook.ID == "" {
			return fmt.Errorf("add_hook: nil or unidentified hook")
		}
		for _, h := range w.Hooks {
			if h.ID == p.Hook.ID {
				return fmt.Errorf("add_hook: id %q already exists", p.Hook.ID)
			}
		}
		w.Hooks = append(w.Hooks, p.Hook)
		return nil

	case OpDeleteHook:
		var p DeleteHookPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		for i, h := range w.Hooks {
			if h.ID == p.ID {
				w.Hooks = append(w.Hooks[:i], w.Hooks[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("delete_hook: id %q not found", p.ID)

	case OpAddRoute:
		var p AddRoutePayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if p.Route == nil {
			return fmt.Errorf("add_route: nil route")
		}
		for _, r := range w.Routes {
			if r.Method == p.Route.Method && r.Path == p.Route.Path {
				return fmt.Errorf("add_route: %s %s already exists", p.Route.Method, p.Route.Path)
			}
		}
		w.Routes = append(w.Routes, p.Route)
		return nil

	case OpDeleteRoute:
		var p DeleteRoutePayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		for i, r := range w.Routes {
			if r.Method == p.Method && r.Path == p.Path {
				w.Routes = append(w.Routes[:i], w.Routes[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("delete_route: %s %s not found", p.Method, p.Path)

	case OpAddSeed:
		var p AddSeedPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if p.Seed == nil {
			return fmt.Errorf("add_seed: nil seed")
		}
		w.Seeds = append(w.Seeds, p.Seed)
		return nil

	case OpAddMiddleware:
		var p AddMiddlewarePayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if p.Middleware == nil {
			return fmt.Errorf("add_middleware: nil middleware")
		}
		w.Middleware = append(w.Middleware, p.Middleware)
		return nil

	default:
		return fmt.Errorf("unknown world edit op %q", e.Op)
	}
}
