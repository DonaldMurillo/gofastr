package journal

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/kiln/world"
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
	s.replaying = true
	defer func() { s.replaying = false }()
	for i, e := range entries {
		if err := Apply(s, e); err != nil {
			return nil, fmt.Errorf("journal: apply entry %d (id=%s kind=%s op=%s): %w", i+1, e.ID, e.Kind, e.Op, err)
		}
	}
	return s, nil
}

// Apply applies a single Entry to a Session. Exposed so the live mutator
// (kiln/live.Live.Apply) can share the same dispatch table used by replay.
func Apply(s *Session, e Entry) error {
	switch e.Kind {
	case KindWorldEdit:
		return applyWorldEdit(s, e)
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
		// A plan with no id is not a plan, and storing one arms the only
		// trick that gets past spendPlan's empty-id refusal: propose and
		// approve s.Plans[""] listing a target, then emit the destructive
		// entry with plan_id OMITTED, which decodes to "". Without
		// spendPlan's first branch that lookup HITS an approved plan
		// listing the target and the deletion is authorized. That branch
		// holds today, so this is the second lock on the same door — but
		// it removes the state that makes the door interesting.
		if strings.TrimSpace(p.PlanID) == "" {
			return fmt.Errorf("plan_proposed: empty plan_id")
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
//
// It enforces the same semantic guards kiln/protocol enforces on the live
// path, the multi-tenant refusal and plan-gating of destructive ops. Those
// used to live only in protocol.go, so a hand-authored .kiln.session.jsonl
// replayed at boot (kiln/live.New) or by `kiln freeze` installed world state
// the HTTP API refuses. The log is the authorization record; a guard that
// only one of the two readers applies is not a guard.
func applyWorldEdit(s *Session, e Entry) error {
	w := s.World
	switch e.Op {
	case OpSetAppConfig:
		var p SetAppConfigPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		// Same normalization the live tool applies, in the same place
		// the live tool applies it. Replay used to install the config
		// verbatim, so a journal could carry a nameless app or an
		// api_prefix of "/api/" or "" — states the API refuses, and the
		// prefix decides where entity CRUD mounts.
		//
		// Prev tells an authoritative set from a derived edit. set_theme
		// re-emits the world's existing config with one field changed,
		// and a fresh world has no name yet, so requiring a name there
		// would refuse an edit the live API produces. A derived edit
		// still may not BLANK a name the world already had.
		cfg := p.Config
		switch {
		case p.Prev == nil:
			if err := world.ValidateAppConfig(&cfg); err != nil {
				return fmt.Errorf("set_app_config: %w", err)
			}
		default:
			if strings.TrimSpace(cfg.Name) == "" && strings.TrimSpace(p.Prev.Name) != "" {
				return fmt.Errorf("set_app_config: cannot clear the app name (was %q)", p.Prev.Name)
			}
			world.NormalizeAPIPrefix(&cfg)
		}
		w.App = cfg
		return nil

	case OpSetScaffold:
		var p SetScaffoldPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if err := world.ValidateScaffold(p.Nav, p.Endpoints, map[string][]world.NamedStub{
			"middleware": p.Middleware,
			"plugins":    p.Plugins,
			"helpers":    p.Helpers,
		}); err != nil {
			return fmt.Errorf("set_scaffold: %w", err)
		}
		w.Nav = p.Nav
		w.Endpoints = p.Endpoints
		w.MiddlewareStubs = p.Middleware
		w.Plugins = p.Plugins
		w.Helpers = p.Helpers
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
		if p.Entity.MultiTenant {
			return fmt.Errorf("add_entity: entity %q sets multi_tenant, which kiln cannot honour (it cannot choose the app-specific tenant resolver): use owner_field, or add tenant middleware in owned Go after freeze", p.Entity.Name)
		}
		if err := validateEntityEndpoints("add_entity", p.Entity); err != nil {
			return err
		}
		if _, exists := w.Entities[p.Entity.Name]; exists {
			return fmt.Errorf("add_entity: %q already exists", p.Entity.Name)
		}
		// add_page's collision check, from the other side: App.Mount
		// panics on it either way round.
		if page := world.EntityCollidesWithPage(w, p.Entity); page != "" {
			return fmt.Errorf("add_entity %q: CRUD mount collides with page %q", p.Entity.Name, page)
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
		if p.Entity.MultiTenant {
			return fmt.Errorf("update_entity: entity %q sets multi_tenant, which kiln cannot honour (it cannot choose the app-specific tenant resolver): use owner_field, or add tenant middleware in owned Go after freeze", p.Entity.Name)
		}
		if err := validateEntityEndpoints("update_entity", p.Entity); err != nil {
			return err
		}
		if _, exists := w.Entities[p.Entity.Name]; !exists {
			return fmt.Errorf("update_entity: %q not found", p.Entity.Name)
		}
		if page := world.EntityCollidesWithPage(w, p.Entity); page != "" {
			return fmt.Errorf("update_entity %q: CRUD mount collides with page %q", p.Entity.Name, page)
		}
		// Wholesale replacement is destructive (every omitted field is
		// dropped, owner_field/cross_owner_read flip to the new value),
		// so it spends a plan target on replay exactly like the live
		// path's requirePlan. The log is the authorization record.
		target := PlanTarget{Op: "update_entity", Name: p.Entity.Name}
		if err := s.spendPlan(p.PlanID, target); err != nil {
			return err
		}
		w.Entities[p.Entity.Name] = p.Entity
		// A field the replacement omits leaves the seeds with it, as
		// delete_field does; "id" is the one undeclared column a seed
		// may set.
		pruneSeedColumns(w, p.Entity.Name, func(col string) bool {
			return col == "id" || slices.ContainsFunc(p.Entity.Fields, func(f world.Field) bool { return f.Name == col })
		})
		return nil

	case OpDeleteEntity:
		var p DeleteEntityPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if err := deleteSpendingPlan(s, p.PlanID, "delete_entity", p.Name, w.Entities); err != nil {
			return err
		}
		// The entity's seeds go in the same edit: the live side effect
		// drops the table they fill, and a seed naming an absent entity
		// is one freeze refuses, so leaving them made every world that
		// deleted a seeded entity impossible to graduate. Undo cuts this
		// entry from the journal, so replay brings both back. There is
		// no delete_seed tool, so refusing the delete while seeds exist
		// would leave the entity undeletable.
		kept := make([]*world.Seed, 0, len(w.Seeds))
		for _, sd := range w.Seeds {
			if sd == nil || sd.Entity != p.Name {
				kept = append(kept, sd)
			}
		}
		w.Seeds = kept
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
		i := slices.IndexFunc(ent.Fields, func(f world.Field) bool { return f.Name == p.Field })
		if i < 0 {
			return fmt.Errorf("delete_field: %s.%s not found", p.Entity, p.Field)
		}
		// Spend only once the edit is certain to apply: a failed delete
		// must not consume the approval it never used.
		target := PlanTarget{Op: "delete_field", Name: p.Entity + "." + p.Field}
		if err := s.spendPlan(p.PlanID, target); err != nil {
			return err
		}
		ent.Fields = append(ent.Fields[:i], ent.Fields[i+1:]...)
		// The seeds lose the column in the same edit, as delete_entity
		// takes the entity's seeds: undo cuts this entry, so replay
		// brings the column back to both. Refusing the delete instead
		// would leave a seeded field undeletable, with no delete_seed.
		pruneSeedColumns(w, p.Entity, func(col string) bool { return col != p.Field })
		return nil

	case OpAddPage:
		var p AddPagePayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if p.Page == nil {
			return fmt.Errorf("add_page: nil page")
		}
		// The live tool refuses it ("missing page or page.path"); an
		// empty path reaches this dispatch only through a hand-authored
		// journal, and its uihost screen registration is undefined.
		if p.Page.Path == "" {
			return fmt.Errorf("add_page: missing page.path")
		}
		if err := world.ValidatePageActions(p.Page); err != nil {
			return fmt.Errorf("add_page %q: %w", p.Page.Path, err)
		}
		if err := world.ValidatePageTree(p.Page.Tree); err != nil {
			return fmt.Errorf("add_page %q: %w", p.Page.Path, err)
		}
		if _, exists := w.Pages[p.Page.Path]; exists {
			return fmt.Errorf("add_page: %q already exists", p.Page.Path)
		}
		// App.Mount panics when a page path equals an entity's CRUD
		// mount, and a panic at boot takes the process rather than
		// refusing the edit. Refuse it here instead, where the answer is
		// survivable and the journal stays replayable.
		if ent := world.PageCollidesWithEntity(w, p.Page.Path); ent != "" {
			return fmt.Errorf("add_page %q: collides with entity %q's CRUD mount", p.Page.Path, ent)
		}
		w.Pages[p.Page.Path] = p.Page
		return nil

	case OpDeletePage:
		var p DeletePagePayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		return deleteSpendingPlan(s, p.PlanID, "delete_page", p.Path, w.Pages)

	case OpUpdatePageElement:
		var p UpdatePageElementPayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		// UpdatePageElement always journals a non-nil New; new:null
		// stores a nil page under the path, which both readers then
		// silently skip — the journal would record an applied edit while
		// the page vanishes from the preview and the graduation artifact.
		if p.New == nil {
			return fmt.Errorf("update_page_element %q: nil page", p.Path)
		}
		if err := world.ValidatePageActions(p.New); err != nil {
			return fmt.Errorf("update_page_element %q: %w", p.Path, err)
		}
		if err := world.ValidatePageTree(p.New.Tree); err != nil {
			return fmt.Errorf("update_page_element %q: %w", p.Path, err)
		}
		cur, exists := w.Pages[p.Path]
		if !exists {
			if p.Prev != nil {
				// Built against a page that has since been deleted.
				return &PageConflictError{Path: p.Path, Want: p.Prev.Version, Have: -1}
			}
			return fmt.Errorf("update_page_element: page %q not found", p.Path)
		}
		// The protocol reads the page, patches a clone and journals the
		// whole page, with no lock held in between. Two patches built
		// from the same version both used to land, the second replacing
		// the first's edit. Every released build journals Prev, and a
		// sequential log always matches it; a nil Prev (hand-authored)
		// keeps the old unconditional swap. Replay skips the check: a
		// log written before it may hold two patches from one version,
		// and it loads as it ran, the later one winning.
		if p.Prev != nil && !s.replaying && cur.Version != p.Prev.Version {
			return &PageConflictError{Path: p.Path, Want: p.Prev.Version, Have: cur.Version}
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
		// The shared kind set, not the per-surface one: released builds
		// journaled a hook with respond_json, and a session holding one
		// must still load. The live tool refuses new ones (world.ValidateHook).
		if err := world.ValidateAction(p.Hook.Action); err != nil {
			return fmt.Errorf("add_hook %q: %w", p.Hook.ID, err)
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
		i := slices.IndexFunc(w.Hooks, func(h *world.Hook) bool { return h.ID == p.ID })
		if i < 0 {
			return fmt.Errorf("delete_hook: id %q not found", p.ID)
		}
		if err := s.spendPlan(p.PlanID, PlanTarget{Op: "delete_hook", Name: p.ID}); err != nil {
			return err
		}
		w.Hooks = append(w.Hooks[:i], w.Hooks[i+1:]...)
		return nil

	case OpAddRoute:
		var p AddRoutePayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		if p.Route == nil {
			return fmt.Errorf("add_route: nil route")
		}
		if err := world.ValidateRoute(p.Route); err != nil {
			return fmt.Errorf("add_route: %w", err)
		}
		// Shared kind set for the same reason as add_hook; the live tool
		// checks routes per surface.
		if err := world.ValidateAction(p.Route.Action); err != nil {
			return fmt.Errorf("add_route %s %s: %w", p.Route.Method, p.Route.Path, err)
		}
		existing := make([]string, 0, len(w.Routes))
		for _, r := range w.Routes {
			if r.Method == p.Route.Method && r.Path == p.Route.Path {
				return fmt.Errorf("add_route: %s %s already exists", p.Route.Method, p.Route.Path)
			}
			existing = append(existing, r.Method+" "+r.Path)
		}
		// Two routes the mux cannot hold together panic the rebuild; the
		// live tool refuses them, and no journal the runtime accepted can
		// hold such a pair.
		if other := world.OverlappingPattern(existing, p.Route.Method+" "+p.Route.Path); other != "" {
			return fmt.Errorf("add_route: %s %s overlaps %s", p.Route.Method, p.Route.Path, other)
		}
		w.Routes = append(w.Routes, p.Route)
		return nil

	case OpDeleteRoute:
		var p DeleteRoutePayload
		if err := e.Decode(&p); err != nil {
			return err
		}
		i := slices.IndexFunc(w.Routes, func(r *world.Route) bool { return r.Method == p.Method && r.Path == p.Path })
		if i < 0 {
			return fmt.Errorf("delete_route: %s %s not found", p.Method, p.Path)
		}
		if err := s.spendPlan(p.PlanID, PlanTarget{Op: "delete_route", Name: p.Method + " " + p.Path}); err != nil {
			return err
		}
		w.Routes = append(w.Routes[:i], w.Routes[i+1:]...)
		return nil

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

// PageConflictError is the refusal of an update_page_element entry built
// against a page version the world no longer holds. Have is -1 when the
// page has been deleted since. It is typed so the tool layer can answer
// "conflict" with its refetch hint rather than "internal".
type PageConflictError struct {
	Path string
	Want int
	Have int
}

func (e *PageConflictError) Error() string {
	if e.Have < 0 {
		return fmt.Sprintf("update_page_element: page %q was deleted after version %d was read", e.Path, e.Want)
	}
	return fmt.Sprintf("update_page_element: page %q is at version %d, the patch was built from version %d", e.Path, e.Have, e.Want)
}

// validateEntityEndpoints rejects an entity whose custom endpoints carry an
// action kind kiln/effect cannot run. It mirrors the per-op action screams
// (hooks, routes, page trees): entity endpoints are part of the world IR and
// graduate to owned-Go stubs via freeze, so an unsupported action is caught
// here rather than landing as a stub that never had an implementation.
func validateEntityEndpoints(op string, ent *world.Entity) error {
	for _, ep := range ent.Endpoints {
		if err := world.ValidateAction(ep.Action); err != nil {
			return fmt.Errorf("%s: endpoint %s %s: %w", op, ep.Method, ep.Path, err)
		}
	}
	return nil
}

// spendPlan is the single authorization check for a destructive world edit:
// the named plan must exist, be approved, not be rejected, list this exact
// target, and not already have been spent on it. On success the (plan,
// target) pair is marked consumed so one approval authorizes one deletion.
//
// It mirrors kiln/protocol's requirePlan + consumeTarget, but derives
// consumption from the log instead of a per-process map, that map was never
// rebuilt on replay, so every restart re-armed every consumed plan.
func (s *Session) spendPlan(planID string, target PlanTarget) error {
	if planID == "" {
		return fmt.Errorf("%s %q: no plan_id: destructive edits require an approved plan naming this target", target.Op, target.Name)
	}
	plan, ok := s.Plans[planID]
	if !ok {
		return fmt.Errorf("%s %q: plan %q not found", target.Op, target.Name, planID)
	}
	if plan.Rejected {
		return fmt.Errorf("%s %q: plan %q was rejected", target.Op, target.Name, planID)
	}
	if !plan.Approved {
		return fmt.Errorf("%s %q: plan %q is not approved", target.Op, target.Name, planID)
	}
	listed := slices.Contains(plan.Targets, target)
	if !listed {
		return fmt.Errorf("%s %q: plan %q does not list this target", target.Op, target.Name, planID)
	}
	key := string(target.Op) + ":" + target.Name
	if s.Consumed[planID][key] {
		return fmt.Errorf("%s %q: plan %q was already spent on this target", target.Op, target.Name, planID)
	}
	if s.Consumed == nil {
		s.Consumed = map[string]map[string]bool{}
	}
	if s.Consumed[planID] == nil {
		s.Consumed[planID] = map[string]bool{}
	}
	s.Consumed[planID][key] = true
	return nil
}

// deleteSpendingPlan deletes name from m, refusing when absent, and
// spends the plan target only once the delete is certain to apply. The
// spend used to come first, so a delete of an absent name failed and
// still marked the approval consumed in the in-memory session, with no
// journal entry recording the spend. It replaces the duplicated delete
// blocks of the OpDeleteEntity and OpDeletePage cases, which differed
// only in op, map, and name field.
func deleteSpendingPlan[V any](s *Session, planID, op, name string, m map[string]V) error {
	if _, exists := m[name]; !exists {
		return fmt.Errorf("%s: %q not found", op, name)
	}
	if err := s.spendPlan(planID, PlanTarget{Op: op, Name: name}); err != nil {
		return err
	}
	delete(m, name)
	return nil
}

// pruneSeedColumns drops the columns keep refuses from the entity's seed
// rows and weights, and the rows left with no column. A seed row naming a
// column the entity no longer has fails its INSERT, so every rederive
// (boot, undo, reset) skipped the whole seed, and the generator refuses
// the frozen spec. Changed seeds are copies in a fresh slice: the decoded
// Seed may be shared with a session snapshot.
func pruneSeedColumns(w *world.World, entity string, keep func(col string) bool) {
	seeds := slices.Clone(w.Seeds)
	for i, sd := range seeds {
		if sd == nil || sd.Entity != entity {
			continue
		}
		cp := *sd
		cp.Rows = nil
		for _, row := range sd.Rows {
			kept := map[string]any{}
			for col, v := range row {
				if keep(col) {
					kept[col] = v
				}
			}
			if len(kept) > 0 {
				cp.Rows = append(cp.Rows, kept)
			}
		}
		if sd.Weights != nil {
			cp.Weights = map[string]map[string]int{}
			for col, wt := range sd.Weights {
				if keep(col) {
					cp.Weights[col] = wt
				}
			}
		}
		seeds[i] = &cp
	}
	w.Seeds = seeds
}
