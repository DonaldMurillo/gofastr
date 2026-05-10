package protocol

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofastr/gofastr/kiln/journal"
	"github.com/gofastr/gofastr/kiln/live"
	"github.com/gofastr/gofastr/kiln/world"
)

// Result is the structured response every tool returns. The shape is the
// same as the on-the-wire payload exposed by the MCP / ACP transports.
type Result struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	Kind   string `json:"kind,omitempty"` // "validation" | "conflict" | "not_found" | "needs_plan" | ...
	Hint   string `json:"hint,omitempty"`
}

// Tools is the agent-facing tool surface. It is safe for concurrent use.
type Tools struct {
	live *live.Live

	mu      sync.Mutex
	counter int64

	// consumed tracks plan targets that have already been used. Once a
	// destructive op succeeds against a plan, that (planID, opKey) pair
	// is recorded here so the same plan can't authorize the same delete
	// twice. Per-process, in-memory — not journaled.
	consumed map[string]map[string]bool
}

// New constructs Tools bound to a Live runtime.
func New(l *live.Live) *Tools {
	return &Tools{
		live:     l,
		consumed: map[string]map[string]bool{},
	}
}

// Live returns the bound runtime, useful for transports that need the
// current session, journal, or SSE bus.
func (t *Tools) Live() *live.Live { return t.live }

// nextEntryID returns a monotonic ID with a per-process random suffix.
func (t *Tools) nextEntryID() string {
	n := atomic.AddInt64(&t.counter, 1)
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), n)
}

// --- Args types -------------------------------------------------------

type WorldGetArgs struct {
	Path string `json:"path,omitempty"`
}

type SetAppConfigArgs struct {
	Config world.AppConfig `json:"config"`
}

type AddEntityArgs struct {
	Entity *world.Entity `json:"entity"`
}

type UpdateEntityArgs struct {
	Entity *world.Entity `json:"entity"`
}

type DeleteEntityArgs struct {
	Name string `json:"name"`
	// PlanID names an approved plan whose Targets list contains
	// {op:"delete_entity",name:<Name>}. Required — destructive ops do not
	// run without a plan-approved authorization.
	PlanID string `json:"plan_id,omitempty"`
}

type AddFieldArgs struct {
	Entity string      `json:"entity"`
	Field  world.Field `json:"field"`
}

type DeleteFieldArgs struct {
	Entity string `json:"entity"`
	Field  string `json:"field"`
	PlanID string `json:"plan_id,omitempty"`
}

type AddPageArgs struct {
	Page *world.Page `json:"page"`
}

type DeletePageArgs struct {
	Path   string `json:"path"`
	PlanID string `json:"plan_id,omitempty"`
}

type AddHookArgs struct {
	Hook *world.Hook `json:"hook"`
}

type DeleteHookArgs struct {
	ID     string `json:"id"`
	PlanID string `json:"plan_id,omitempty"`
}

type AddRouteArgs struct {
	Route *world.Route `json:"route"`
}

type DeleteRouteArgs struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	PlanID string `json:"plan_id,omitempty"`
}

type AddSeedArgs struct {
	Seed *world.Seed `json:"seed"`
}

type ProposePlanArgs struct {
	PlanID  string               `json:"plan_id"`
	Steps   []string             `json:"steps"`
	Reason  string               `json:"reason,omitempty"`
	Targets []journal.PlanTarget `json:"targets,omitempty"`
}

type ApprovePlanArgs struct {
	PlanID   string `json:"plan_id"`
	Modified bool   `json:"modified,omitempty"`
}

type RejectPlanArgs struct {
	PlanID string `json:"plan_id"`
	Reason string `json:"reason,omitempty"`
}

type UndoArgs struct{}

type ResetSessionArgs struct{}

// SetThemeArgs replaces world.App.Theme. Keys are token names from
// core-ui/widget/theme (e.g. "page-bg", "page-primary", "page-accent");
// values are CSS color literals. Empty Theme map clears overrides
// (revert to framework defaults).
type SetThemeArgs struct {
	Theme map[string]string `json:"theme"`
}

type ChatArgs struct {
	Role string `json:"role"` // "user" | "assistant"
	Text string `json:"text"`
}

// --- Tool methods -----------------------------------------------------

func (t *Tools) WorldGet(_ context.Context, args WorldGetArgs) Result {
	sess := t.live.Session()
	if args.Path == "" {
		return ok(sess.World)
	}
	switch args.Path {
	case "_chat":
		return ok(sess.Chat)
	case "_plans":
		return ok(sess.Plans)
	}
	if strings.HasPrefix(args.Path, "entities.") {
		name := strings.TrimPrefix(args.Path, "entities.")
		ent, ok2 := sess.World.Entities[name]
		if !ok2 {
			return notFound("entity %q not found", name)
		}
		return ok(ent)
	}
	if strings.HasPrefix(args.Path, "pages.") {
		path := strings.TrimPrefix(args.Path, "pages.")
		page, ok2 := sess.World.Pages[path]
		if !ok2 {
			return notFound("page %q not found", path)
		}
		return ok(page)
	}
	return invalid("unknown path %q", args.Path)
}

func (t *Tools) SetAppConfig(_ context.Context, args SetAppConfigArgs) Result {
	prev := t.live.Session().World.App
	return t.applyEdit(journal.OpSetAppConfig, journal.SetAppConfigPayload{Config: args.Config, Prev: &prev})
}

func (t *Tools) AddEntity(_ context.Context, args AddEntityArgs) Result {
	if args.Entity == nil || args.Entity.Name == "" {
		return invalid("missing entity or entity.name")
	}
	if _, exists := t.live.Session().World.Entities[args.Entity.Name]; exists {
		return conflict("entity %q already exists", args.Entity.Name).withHint("use update_entity to modify")
	}
	// Reject if a page already lives at /<entity>; both would register
	// GET /<entity> on the router.
	if _, hasPage := t.live.Session().World.Pages["/"+args.Entity.Name]; hasPage {
		return conflict("entity %q would collide with existing page at /%s",
			args.Entity.Name, args.Entity.Name).
			withHint("rename the entity (e.g. add a suffix) or delete the page first")
	}
	return t.applyEdit(journal.OpAddEntity, journal.AddEntityPayload{Entity: args.Entity})
}

func (t *Tools) UpdateEntity(_ context.Context, args UpdateEntityArgs) Result {
	if args.Entity == nil || args.Entity.Name == "" {
		return invalid("missing entity or entity.name")
	}
	prev, exists := t.live.Session().World.Entities[args.Entity.Name]
	if !exists {
		return notFound("entity %q not found", args.Entity.Name)
	}
	return t.applyEdit(journal.OpUpdateEntity, journal.UpdateEntityPayload{Entity: args.Entity, Prev: prev})
}

func (t *Tools) DeleteEntity(_ context.Context, args DeleteEntityArgs) Result {
	prev, exists := t.live.Session().World.Entities[args.Name]
	if !exists {
		return notFound("entity %q not found", args.Name)
	}
	target := journal.PlanTarget{Op: "delete_entity", Name: args.Name}
	if r := t.requirePlan(args.PlanID, target); !r.OK {
		return r
	}
	res := t.applyEdit(journal.OpDeleteEntity, journal.DeleteEntityPayload{Name: args.Name, Prev: prev})
	if res.OK {
		t.consumeTarget(args.PlanID, target)
	}
	return res
}

func (t *Tools) AddField(_ context.Context, args AddFieldArgs) Result {
	if args.Field.Name == "" {
		return invalid("missing field.name")
	}
	ent, exists := t.live.Session().World.Entities[args.Entity]
	if !exists {
		return notFound("entity %q not found", args.Entity)
	}
	for _, f := range ent.Fields {
		if f.Name == args.Field.Name {
			return conflict("%s.%s already exists", args.Entity, args.Field.Name)
		}
	}
	return t.applyEdit(journal.OpAddField, journal.AddFieldPayload{Entity: args.Entity, Field: args.Field})
}

func (t *Tools) DeleteField(_ context.Context, args DeleteFieldArgs) Result {
	ent, exists := t.live.Session().World.Entities[args.Entity]
	if !exists {
		return notFound("entity %q not found", args.Entity)
	}
	var prev *world.Field
	for i := range ent.Fields {
		if ent.Fields[i].Name == args.Field {
			cp := ent.Fields[i]
			prev = &cp
			break
		}
	}
	if prev == nil {
		return notFound("field %s.%s not found", args.Entity, args.Field)
	}
	target := journal.PlanTarget{Op: "delete_field", Name: args.Entity + "." + args.Field}
	if r := t.requirePlan(args.PlanID, target); !r.OK {
		return r
	}
	res := t.applyEdit(journal.OpDeleteField, journal.DeleteFieldPayload{Entity: args.Entity, Field: args.Field, Prev: prev})
	if res.OK {
		t.consumeTarget(args.PlanID, target)
	}
	return res
}

func (t *Tools) AddPage(_ context.Context, args AddPageArgs) Result {
	if args.Page == nil || args.Page.Path == "" {
		return invalid("missing page or page.path")
	}
	if args.Page.Tree.Kind == "" {
		return invalid("page.tree.kind must be set (e.g. \"div\") — the tree is the root element and nothing renders without a kind").
			withHint(`minimal page: {"path":"/x","tree":{"kind":"div","children":[{"kind":"heading","props":{"level":1,"text":"Hello"}}]}}`)
	}
	if _, exists := t.live.Session().World.Pages[args.Page.Path]; exists {
		return conflict("page %q already exists", args.Page.Path)
	}
	// Reject paths that already belong to an entity's CRUD list endpoint.
	// Both register `GET /<path>` so the underlying router would panic.
	for _, ent := range t.live.Session().World.Entities {
		if "/"+ent.Name == args.Page.Path {
			return conflict("page path %q collides with entity %q's CRUD list endpoint at GET %q",
				args.Page.Path, ent.Name, args.Page.Path).
				withHint(fmt.Sprintf("pick a different path like %q, %q, or %q",
					args.Page.Path+"/list", "/view"+args.Page.Path, args.Page.Path+"-page"))
		}
	}
	return t.applyEdit(journal.OpAddPage, journal.AddPagePayload{Page: args.Page})
}

func (t *Tools) DeletePage(_ context.Context, args DeletePageArgs) Result {
	prev, exists := t.live.Session().World.Pages[args.Path]
	if !exists {
		return notFound("page %q not found", args.Path)
	}
	target := journal.PlanTarget{Op: "delete_page", Name: args.Path}
	if r := t.requirePlan(args.PlanID, target); !r.OK {
		return r
	}
	res := t.applyEdit(journal.OpDeletePage, journal.DeletePagePayload{Path: args.Path, Prev: prev})
	if res.OK {
		t.consumeTarget(args.PlanID, target)
	}
	return res
}

func (t *Tools) AddHook(_ context.Context, args AddHookArgs) Result {
	if args.Hook == nil || args.Hook.ID == "" {
		return invalid("missing hook or hook.id")
	}
	for _, h := range t.live.Session().World.Hooks {
		if h.ID == args.Hook.ID {
			return conflict("hook %q already exists", args.Hook.ID)
		}
	}
	return t.applyEdit(journal.OpAddHook, journal.AddHookPayload{Hook: args.Hook})
}

func (t *Tools) DeleteHook(_ context.Context, args DeleteHookArgs) Result {
	var prev *world.Hook
	for _, h := range t.live.Session().World.Hooks {
		if h.ID == args.ID {
			prev = h
			break
		}
	}
	if prev == nil {
		return notFound("hook %q not found", args.ID)
	}
	target := journal.PlanTarget{Op: "delete_hook", Name: args.ID}
	if r := t.requirePlan(args.PlanID, target); !r.OK {
		return r
	}
	res := t.applyEdit(journal.OpDeleteHook, journal.DeleteHookPayload{ID: args.ID, Prev: prev})
	if res.OK {
		t.consumeTarget(args.PlanID, target)
	}
	return res
}

func (t *Tools) AddRoute(_ context.Context, args AddRouteArgs) Result {
	if args.Route == nil || args.Route.Method == "" || args.Route.Path == "" {
		return invalid("route.method and route.path required")
	}
	for _, r := range t.live.Session().World.Routes {
		if r.Method == args.Route.Method && r.Path == args.Route.Path {
			return conflict("route %s %s already exists", args.Route.Method, args.Route.Path)
		}
	}
	return t.applyEdit(journal.OpAddRoute, journal.AddRoutePayload{Route: args.Route})
}

func (t *Tools) DeleteRoute(_ context.Context, args DeleteRouteArgs) Result {
	var prev *world.Route
	for _, r := range t.live.Session().World.Routes {
		if r.Method == args.Method && r.Path == args.Path {
			prev = r
			break
		}
	}
	if prev == nil {
		return notFound("route %s %s not found", args.Method, args.Path)
	}
	target := journal.PlanTarget{Op: "delete_route", Name: args.Method + " " + args.Path}
	if r := t.requirePlan(args.PlanID, target); !r.OK {
		return r
	}
	res := t.applyEdit(journal.OpDeleteRoute, journal.DeleteRoutePayload{Method: args.Method, Path: args.Path, Prev: prev})
	if res.OK {
		t.consumeTarget(args.PlanID, target)
	}
	return res
}

func (t *Tools) AddSeed(_ context.Context, args AddSeedArgs) Result {
	if args.Seed == nil || args.Seed.Entity == "" {
		return invalid("seed.entity required")
	}
	return t.applyEdit(journal.OpAddSeed, journal.AddSeedPayload{Seed: args.Seed})
}

func (t *Tools) ProposePlan(_ context.Context, args ProposePlanArgs) Result {
	if args.PlanID == "" || len(args.Steps) == 0 {
		return invalid("plan_id and at least one step required")
	}
	if _, ok := t.live.Session().Plans[args.PlanID]; ok {
		return conflict("plan %q already exists", args.PlanID)
	}
	return t.applyEntry(journal.KindPlanProposed, "", journal.PlanProposedPayload{
		PlanID:  args.PlanID,
		Steps:   args.Steps,
		Reason:  args.Reason,
		Targets: args.Targets,
	})
}

func (t *Tools) ApprovePlan(_ context.Context, args ApprovePlanArgs) Result {
	plan, exists := t.live.Session().Plans[args.PlanID]
	if !exists {
		return notFound("plan %q not found", args.PlanID)
	}
	if plan.Rejected {
		return conflict("plan %q was rejected — propose a new plan", args.PlanID)
	}
	if plan.Approved {
		return ok(map[string]any{"plan_id": args.PlanID, "already": true})
	}
	return t.applyEntry(journal.KindPlanApproved, "", journal.PlanApprovedPayload{
		PlanID:   args.PlanID,
		Modified: args.Modified,
	})
}

func (t *Tools) RejectPlan(_ context.Context, args RejectPlanArgs) Result {
	plan, exists := t.live.Session().Plans[args.PlanID]
	if !exists {
		return notFound("plan %q not found", args.PlanID)
	}
	if plan.Approved {
		return conflict("plan %q already approved", args.PlanID)
	}
	if plan.Rejected {
		return ok(map[string]any{"plan_id": args.PlanID, "already": true})
	}
	return t.applyEntry(journal.KindPlanRejected, "", journal.PlanRejectedPayload{
		PlanID: args.PlanID,
		Reason: args.Reason,
	})
}

// SetTheme writes world.App.Theme via a SetAppConfig journal entry.
// Theme tokens flow into core-ui/widget/theme.PageCSS at render time;
// the next /kiln/theme.css fetch reflects the new palette and every
// page re-skins on the client's next stylesheet load.
func (t *Tools) SetTheme(_ context.Context, args SetThemeArgs) Result {
	prev := t.live.Session().World.App
	next := prev
	if args.Theme == nil {
		next.Theme = nil
	} else {
		next.Theme = map[string]string{}
		for k, v := range args.Theme {
			next.Theme[k] = v
		}
	}
	return t.applyEdit(journal.OpSetAppConfig, journal.SetAppConfigPayload{Config: next, Prev: &prev})
}

// ResetSession truncates the journal to zero entries and reloads the
// live runtime. The DB schema is rebuilt from scratch (empty world →
// empty migration set). In-memory plan-consumption tracking resets
// too. Used by the panel's "Reset" button when the user wants a clean
// slate without killing the process.
func (t *Tools) ResetSession(_ context.Context, _ ResetSessionArgs) Result {
	j := t.live.Journal()
	if err := j.TruncateAfter(0); err != nil {
		return failure("truncate: %v", err)
	}
	if err := t.live.Reload(); err != nil {
		return failure("reload: %v", err)
	}
	t.mu.Lock()
	t.consumed = map[string]map[string]bool{}
	t.mu.Unlock()
	return ok(nil)
}

func (t *Tools) Undo(_ context.Context, _ UndoArgs) Result {
	j := t.live.Journal()
	n, err := j.Len()
	if err != nil {
		return failure("journal len: %v", err)
	}
	if n == 0 {
		return invalid("nothing to undo")
	}
	if err := j.TruncateAfter(n - 1); err != nil {
		return failure("truncate: %v", err)
	}
	if err := t.live.Reload(); err != nil {
		return failure("reload: %v", err)
	}
	return ok(nil)
}

func (t *Tools) Chat(_ context.Context, args ChatArgs) Result {
	if args.Text == "" {
		return invalid("text required")
	}
	kind := journal.KindChatUser
	if args.Role == "assistant" {
		kind = journal.KindChatAssistant
	}
	return t.applyEntry(kind, "", journal.ChatMessagePayload{Text: args.Text})
}

// --- helpers ----------------------------------------------------------

func (t *Tools) applyEdit(op journal.Op, payload any) Result {
	return t.applyEntry(journal.KindWorldEdit, op, payload)
}

func (t *Tools) applyEntry(kind journal.Kind, op journal.Op, payload any) Result {
	entry, err := journal.NewEntry(t.nextEntryID(), time.Now().UTC(), kind, op, payload)
	if err != nil {
		return failure("build entry: %v", err)
	}
	if err := t.live.Apply(entry); err != nil {
		return failure("%v", err)
	}
	return Result{OK: true, Result: map[string]any{"entry_id": entry.ID}}
}

// requirePlan is the destructive-op safety gate. It returns Result{OK:true}
// when the call may proceed (an approved, not-yet-consumed plan covers
// the target); otherwise Result{OK:false, Kind:"needs_plan"} with a hint
// telling the agent how to comply.
func (t *Tools) requirePlan(planID string, target journal.PlanTarget) Result {
	if planID == "" {
		return needsPlan(target, "no plan_id supplied — call propose_plan listing this destructive op in `targets`, then await user approval")
	}
	plan, exists := t.live.Session().Plans[planID]
	if !exists {
		return needsPlan(target, fmt.Sprintf("plan %q not found", planID))
	}
	if plan.Rejected {
		return needsPlan(target, fmt.Sprintf("plan %q was rejected — propose a new plan", planID))
	}
	if !plan.Approved {
		return needsPlan(target, fmt.Sprintf("plan %q is not yet approved by the user", planID))
	}
	matched := false
	for _, tg := range plan.Targets {
		if tg == target {
			matched = true
			break
		}
	}
	if !matched {
		return needsPlan(target, fmt.Sprintf("plan %q does not list this op in `targets` — propose a plan that includes %+v", planID, target))
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if used, ok := t.consumed[planID]; ok && used[target.Op+":"+target.Name] {
		return needsPlan(target, fmt.Sprintf("plan %q already consumed for this target — propose a new plan", planID))
	}
	return Result{OK: true}
}

// consumeTarget marks (planID, target) as used. Called only after the
// underlying applyEdit succeeded.
func (t *Tools) consumeTarget(planID string, target journal.PlanTarget) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.consumed[planID] == nil {
		t.consumed[planID] = map[string]bool{}
	}
	t.consumed[planID][target.Op+":"+target.Name] = true
}

// --- result constructors ----------------------------------------------

func ok(v any) Result {
	return Result{OK: true, Result: v}
}

func failure(format string, a ...any) Result {
	return Result{OK: false, Error: fmt.Sprintf(format, a...)}
}

func invalid(format string, a ...any) Result {
	r := failure(format, a...)
	r.Kind = "validation"
	return r
}

func notFound(format string, a ...any) Result {
	r := failure(format, a...)
	r.Kind = "not_found"
	return r
}

func conflict(format string, a ...any) Result {
	r := failure(format, a...)
	r.Kind = "conflict"
	return r
}

// needsPlan signals "you tried a destructive op without an approved
// plan covering it." The Result includes the target so the agent (and
// the panel) know what to propose; the Hint tells the agent the next
// step.
func needsPlan(target journal.PlanTarget, hint string) Result {
	return Result{
		OK:   false,
		Kind: "needs_plan",
		Result: map[string]any{
			"target": target,
		},
		Error: "destructive op blocked: needs an approved plan",
		Hint:  hint,
	}
}

func (r Result) withHint(h string) Result {
	r.Hint = h
	return r
}
