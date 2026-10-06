package entityui

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// Extensions is the code an app registers next to its screens: field
// kinds, view funcs, record tabs and actions. The entity says what to
// show; Extensions draws or acts. The admin and app pages share one value.
type Extensions struct {
	// Kinds are named field renderers; Display.Fields[f].Input picks one.
	Kinds map[string]Kind
	// Entities holds per-entity extensions keyed by entity name.
	Entities map[string]Extension
	// Jobs runs bulk actions over more records than one request may
	// touch (InRequestCap). nil refuses such a selection, naming the cap.
	// A queued run needs the Host to keep snapshots (BulkHost); New
	// refuses Jobs on a Host that does not.
	Jobs JobRunner
}

// Kind draws one field kind: Input on forms, Cell in list cells and cards,
// Detail read-only (Cell when nil). A kind changes how a value is drawn
// and entered, never what the API stores.
type Kind struct {
	Input  func(InputContext) render.HTML
	Cell   func(CellContext) render.HTML
	Detail func(CellContext) render.HTML
}

// InputContext is what a Kind's Input receives: the field, the control
// wiring the form field built (label association, description chain,
// invalid state), the input name and the current value as text.
type InputContext struct {
	Ctx         context.Context
	Entity      string
	Field       schema.Field
	Control     headless.FieldControl
	Name        string
	Value       string
	Placeholder string
}

// CellContext is what a Kind's Cell and Detail receive.
type CellContext struct {
	Ctx    context.Context
	Entity string
	Field  schema.Field
	Value  any
	Row    map[string]any
}

// Extension is one entity's extensions.
type Extension struct {
	// Views binds a view func to a Display.Views key.
	Views map[string]ViewFunc
	// Tabs are record tabs drawn after the built-in ones.
	Tabs []Tab
	// Actions are record header buttons and, with Bulk, list bulk actions.
	Actions []Action
	// List and Record replace the body of the entity's list or record
	// screen. The route, gates and the drawer stack stay the framework's.
	List   func(ListContext) (component.Component, error)
	Record func(RecordContext) (component.Component, error)
}

// ViewFunc is a view whose filter depends on who is looking, the tenant
// or the clock. Filter's predicate passes filter.ValidatePredicate before
// it reaches SQL, every time. Show, when set, hides the view from callers
// it does not apply to; a hidden default view falls back to All.
type ViewFunc struct {
	Filter func(ctx context.Context) (*filter.Predicate, error)
	Show   func(ctx context.Context) bool
}

// Tab is an extra record tab.
type Tab struct {
	Key   string
	Label string
	Build func(TabContext) (component.Component, error)
}

// Record is the record a tab, action or replaced screen is drawn for, as
// the read hooks left it.
type Record struct {
	ID     string
	Values map[string]any
}

// TabContext is what a Tab's Build receives. UI is the same *UI the screen
// was drawn with; there is no package-level one.
type TabContext struct {
	Ctx    context.Context
	UI     *UI
	Entity string
	Record Record
}

// ListContext is what an Extension.List receives.
type ListContext struct {
	Ctx    context.Context
	UI     *UI
	Entity string
}

// RecordContext is what an Extension.Record receives.
type RecordContext struct {
	Ctx    context.Context
	UI     *UI
	Entity string
	Record Record
}

// Action is a record or bulk action. Permission, when set, is checked
// about each record on top of the entity's update access, with
// access.CanResourceExact: a Wildcard grant does not satisfy it.
type Action struct {
	Key        string
	Label      string
	Variant    string
	Permission string
	// Bulk offers the action on the list's selection as well as on the
	// record.
	Bulk bool
	Run  func(ctx context.Context, ac ActionContext) error
}

// ActionContext is what an Action's Run receives: the resolved selection
// and a CRUD handle scoped to the caller. Code that reaches past the
// handle does so outside the framework's guarantees.
type ActionContext struct {
	Entity string
	IDs    []string
	Crud   *crud.CrudHandler
}

// InRequestCap is the most records a bulk action runs inside one request,
// the CRUD batch endpoint's cap. A larger selection needs Extensions.Jobs.
const InRequestCap = 100

// EveryMatchCap is the most records an "every match" selection resolves.
const EveryMatchCap = 10000

// JobRunner runs a bulk action over more than InRequestCap records,
// outside the request. entityui writes the confirmed selection to the
// Host's snapshot store and hands Enqueue the job; the runner's worker
// then calls UI.RunBulkJob with job.ID, as often as it retries. The admin
// backs it with battery/queue.
type JobRunner interface {
	// Enqueue schedules job. It must not run the job inline.
	Enqueue(ctx context.Context, job BulkJob) error
	// Principal rebuilds the creator's request context as of now from
	// job.Creator and job.Tenant: the user, their current roles and the
	// tenant, read fresh, the way a request from them would carry them.
	// RunBulkJob calls it before every chunk; an error stops the run.
	Principal(ctx context.Context, job BulkJob) (context.Context, error)
}

// BulkJob is one queued bulk run. Its selection lives in the snapshot
// store under ID; no ids ride in the job itself.
type BulkJob struct {
	ID     string
	Entity string
	// Action is the bulk bar's action key: "delete", "set:<field>:<value>",
	// "move:<key>" or "run:<key>".
	Action string
	Count  int
	// Creator is the confirming user's id (handler.GetUser's GetID) and
	// Tenant their tenant, "" for none.
	Creator string
	Tenant  string
	// FilterHash is a SHA-256 of the scope and list query the selection
	// came from, for the audit trail.
	FilterHash string
	// Status is "queued", "done" or "stopped"; Store.Job fills it.
	Status string
}

// Bulk job statuses.
const (
	BulkQueued  = "queued"
	BulkDone    = "done"
	BulkStopped = "stopped"
)

// Outcomes a snapshot row settles to.
const (
	BulkRowDone    = "done"
	BulkRowSkipped = "skipped"
	BulkRowFailed  = "failed"
)

// BulkStore keeps queued bulk runs: the job and the ids its selection
// resolved to at confirm, each settled once it has run.
type BulkStore interface {
	// Create writes the job and its ids in one transaction.
	Create(ctx context.Context, job BulkJob, ids []string) error
	// Job reads one job; an unknown id is an error.
	Job(ctx context.Context, id string) (BulkJob, error)
	// Pending returns up to limit ids not yet settled, in a stable order.
	Pending(ctx context.Context, id string, limit int) ([]string, error)
	// Settle records each id's outcome (BulkRowDone, BulkRowSkipped,
	// BulkRowFailed). A settled id never comes back from Pending.
	Settle(ctx context.Context, id string, outcomes map[string]string) error
	// Tally counts the job's settled ids by outcome, across every call
	// that settled any, so a resumed run's summary covers the whole job.
	Tally(ctx context.Context, id string) (map[string]int, error)
	// Finish sets the job's status.
	Finish(ctx context.Context, id, status string) error
}

// BulkHost is what a Host implements to back bulk actions: the snapshot
// store queued runs walk, and the audit rows every run writes. The host
// App.EntityUI builds implements it; without it, bulk runs still work
// inside the request and write no summary row, and Extensions.Jobs is
// refused.
type BulkHost interface {
	// BulkStore returns the snapshot store, nil when the host keeps none.
	BulkStore() BulkStore
	// AuditEvent writes one audit row; a host with no audit log answers
	// nil and writes nothing.
	AuditEvent(ctx context.Context, entity, op, recordID string, detail map[string]any) error
}

// check is New's name check. See New for the refusals.
func (x Extensions) check(reg entity.Registry) error {
	for _, name := range slices.Sorted(maps.Keys(x.Kinds)) {
		k := x.Kinds[name]
		if !entity.ValidKey(name) {
			return fmt.Errorf("entityui: kind %q is not a key (lowercase ASCII slug)", name)
		}
		if k.Input == nil && k.Cell == nil {
			return fmt.Errorf("entityui: kind %q has neither Input nor Cell", name)
		}
	}
	for _, e := range reg.AllSorted() {
		d := e.Config.Display
		if d == nil {
			continue
		}
		for _, f := range slices.Sorted(maps.Keys(d.Fields)) {
			if in := d.Fields[f].Input; in != "" {
				if _, ok := x.Kinds[in]; !ok && !isBuiltinKind(in) {
					return fmt.Errorf("entityui: entity %q field %q: input %q names no kind; register it in Extensions.Kinds", e.GetName(), f, in)
				}
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(x.Entities)) {
		e, err := reg.Get(name)
		if err != nil {
			return fmt.Errorf("entityui: extension for unknown entity %q", name)
		}
		if err := x.Entities[name].check(e); err != nil {
			return err
		}
	}
	// Every Where-less view needs a registered Filter, on every entity,
	// extended or not.
	for _, e := range reg.AllSorted() {
		d := e.Config.Display
		if d == nil {
			continue
		}
		ext := x.Entities[e.GetName()]
		for _, v := range d.Views {
			if v.Where != "" {
				continue
			}
			if vf, ok := ext.Views[v.Key]; !ok || vf.Filter == nil {
				return fmt.Errorf("entityui: entity %q view %q has no where and no registered filter func", e.GetName(), v.Key)
			}
		}
	}
	return nil
}

func (x Extension) check(e *entity.Entity) error {
	name := e.GetName()
	declared := map[string]bool{}
	if d := e.Config.Display; d != nil {
		for _, v := range d.Views {
			declared[v.Key] = true
		}
	}
	for _, key := range slices.Sorted(maps.Keys(x.Views)) {
		if !declared[key] {
			return fmt.Errorf("entityui: entity %q: view func %q names no view in Display.Views", name, key)
		}
		if vf := x.Views[key]; vf.Filter == nil && vf.Show == nil {
			return fmt.Errorf("entityui: entity %q: view func %q sets neither Filter nor Show", name, key)
		}
	}
	seen := map[string]bool{}
	for _, t := range x.Tabs {
		switch {
		case !entity.ValidKey(t.Key):
			return fmt.Errorf("entityui: entity %q: tab key %q is not a key", name, t.Key)
		case isBuiltinTab(t.Key):
			return fmt.Errorf("entityui: entity %q: tab key %q is a built-in tab", name, t.Key)
		case seen[t.Key]:
			return fmt.Errorf("entityui: entity %q: duplicate tab %q", name, t.Key)
		case t.Build == nil:
			return fmt.Errorf("entityui: entity %q: tab %q has no Build", name, t.Key)
		}
		seen[t.Key] = true
	}
	seen = map[string]bool{}
	for _, a := range x.Actions {
		switch {
		case !entity.ValidKey(a.Key):
			return fmt.Errorf("entityui: entity %q: action key %q is not a key", name, a.Key)
		case seen[a.Key]:
			return fmt.Errorf("entityui: entity %q: duplicate action %q", name, a.Key)
		case a.Run == nil:
			return fmt.Errorf("entityui: entity %q: action %q has no Run", name, a.Key)
		}
		seen[a.Key] = true
	}
	return nil
}

// clone copies the maps and slices New keeps, so the caller changing its
// value later changes nothing New checked.
func (x Extensions) clone() Extensions {
	out := Extensions{Jobs: x.Jobs, Kinds: maps.Clone(x.Kinds), Entities: map[string]Extension{}}
	for k, v := range x.Entities {
		v.Views = maps.Clone(v.Views)
		v.Tabs = slices.Clone(v.Tabs)
		v.Actions = slices.Clone(v.Actions)
		out.Entities[k] = v
	}
	return out
}
