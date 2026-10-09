package entityui

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
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
	// FilesURL is the same-origin path the app serves stored files
	// under ("/uploads/" for upload.ServeHandler on /uploads/{key...}).
	// A relative Image or File value is a storage key: the screens draw
	// it at FilesURL + key, and draw nothing for one when FilesURL is
	// empty. With file storage set on the app (framework.WithFileStorage),
	// forms draw Image and File fields as uploads.
	FilesURL string
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
// invalid state), the input name, the current value as text, and the
// label and help the form resolved for the field (the catalog entry,
// else the Display hint, else the humanized name), so a kind draws the
// same words a built-in field would.
type InputContext struct {
	Ctx         context.Context
	Entity      string
	Field       schema.Field
	Control     headless.FieldControl
	Name        string
	Value       string
	Placeholder string
	Label       string
	Help        string
}

// CellContext is what a Kind's Cell and Detail receive. Row and Value
// come from the read after its hooks, so a masked column stays masked; a
// relation whose target the caller may not read draws muted and never
// reaches the callback.
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

// asCaller is the context an app's extension code runs under: the
// caller's own, with a back office's elevation (crud.WithElevation)
// removed. The admin vouches for its own reads and writes, not for an
// action's Run, a tab's Build, a view func or a field kind, so those
// see only what the caller's roles allow.
func asCaller(ctx context.Context) context.Context {
	return crud.WithoutElevation(ctx)
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

// Action is a record or bulk action: a button in the record's header,
// posting to the entity's bulk route with the record scope. Permission,
// when set, is checked about each record on top of the entity's update
// access, with access.CanResourceExact: a Wildcard grant does not
// satisfy it. Variant is the button's (ui.ButtonSecondary when empty);
// New refuses one no Button knows.
type Action struct {
	Key        string
	Label      string
	Variant    ui.ButtonVariant
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
	// Run names the bulk run: the queued job's id, the same on every
	// retry, or a fresh id for a run inside the request. A queued run
	// hands each record to Run at least once: a worker that dies after
	// Run returns but before the outcomes are saved runs those records
	// again under the same Run. An action with effects outside the
	// database (mail, a payment, a webhook) keys them on Run and the
	// record id so the second delivery does nothing.
	Run string
}

// InRequestCap is the most records a bulk action runs inside one request,
// the CRUD batch endpoint's cap. A larger selection needs Extensions.Jobs.
const InRequestCap = 100

// EveryMatchCap is the most records an "every match" selection resolves.
const EveryMatchCap = 10000

// JobRunner runs a bulk action over more than InRequestCap records,
// outside the request. entityui writes the confirmed selection to the
// Host's snapshot store and hands Enqueue the job; the runner's worker
// then calls UI.RunBulkJob with job.ID, as often as it retries, until it
// returns nil. The admin backs it with battery/queue.
type JobRunner interface {
	// Enqueue schedules job. It must not run the job inline. Enqueuing a
	// job twice is safe: RunBulkJob runs a job under a lease and answers
	// nil once it has finished, so UI.ResumeBulkJobs can hand over again
	// a job whose first Enqueue is not known to have happened.
	Enqueue(ctx context.Context, job BulkJob) error
	// Principal rebuilds the creator's request context from job.Creator
	// and job.Tenant: the user and their current roles, read fresh, and
	// the run's tenant, fixed at confirm because the snapshot's records
	// belong to it. RunBulkJob calls it before every chunk; an error
	// stops the run, so a runner that tracks tenant membership refuses a
	// creator who has left job.Tenant.
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
	// Key identifies the confirmed run: a SHA-256 of the creator, tenant,
	// entity, action and the ids. While a job with a Key is queued, a
	// second confirm of the same run answers that job instead of queuing
	// another.
	Key string
	// Status is "queued", "done" or "stopped"; Store.Job fills it.
	Status string
	// Done, Skipped and Failed are a finished job's tally; Store.Job
	// fills them once Finish has run.
	Done, Skipped, Failed int
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
// resolved to at confirm, each settled once it has run. One runner at a
// time holds a job's lease (Claim); Settle and Finish write only for the
// runner holding it, so two workers handed the same job never both
// record it.
type BulkStore interface {
	// Create writes the job and its ids in one transaction and returns
	// it. When a queued job with the same Key is held it writes nothing
	// and returns that job instead.
	Create(ctx context.Context, job BulkJob, ids []string) (BulkJob, error)
	// Enqueued records that the JobRunner accepted the job.
	Enqueued(ctx context.Context, id string) error
	// Job reads one job; an unknown id is an error.
	Job(ctx context.Context, id string) (BulkJob, error)
	// Claim gives runner the job's lease until until, or renews it when
	// runner holds it. It answers false when the job is not queued or
	// another runner's lease is still live at now.
	Claim(ctx context.Context, id, runner string, now, until time.Time) (bool, error)
	// Pending returns up to limit ids not yet settled, in a stable order.
	Pending(ctx context.Context, id string, limit int) ([]string, error)
	// Settle records each id's outcome (BulkRowDone, BulkRowSkipped,
	// BulkRowFailed) while runner holds the lease, and answers
	// ErrBulkLeaseLost when it does not. A settled id never comes back
	// from Pending.
	Settle(ctx context.Context, id, runner string, outcomes map[string]string) error
	// Tally counts the job's settled ids by outcome, across every call
	// that settled any, so a resumed run's summary covers the whole job.
	Tally(ctx context.Context, id string) (map[string]int, error)
	// Finish, while runner holds the lease, sets the job's final status
	// (BulkDone or BulkStopped) at at, keeps its tally on the job and
	// deletes its ids. It answers ErrBulkLeaseLost when runner does not
	// hold the lease.
	Finish(ctx context.Context, id, runner, status string, at time.Time) error
	// Unenqueued lists the queued jobs created before before that no
	// Enqueued call has marked.
	Unenqueued(ctx context.Context, before time.Time) ([]BulkJob, error)
	// Prune deletes the jobs that finished before before, and reports
	// how many.
	Prune(ctx context.Context, before time.Time) (int, error)
}

// ErrBulkLeaseLost answers a Settle or Finish from a runner that no
// longer holds the job's lease. RunBulkJob returns it and the JobRunner
// retries; the lease's holder finishes the run.
var ErrBulkLeaseLost = errors.New("entityui: bulk job lease lost")

// ErrBulkJobBusy is RunBulkJob's answer while another runner holds the
// job's live lease. The JobRunner retries later; once the job has
// finished, RunBulkJob answers nil.
var ErrBulkJobBusy = errors.New("entityui: bulk job held by another runner")

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
	if x.FilesURL != "" && (!strings.HasPrefix(x.FilesURL, "/") || strings.HasPrefix(x.FilesURL, "//") || !strings.HasSuffix(x.FilesURL, "/")) {
		return fmt.Errorf("entityui: FilesURL %q must be a same-origin path that starts and ends with /", x.FilesURL)
	}
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
		if err := x.checkInputs(e); err != nil {
			return err
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
		if err := x.checkViews(e); err != nil {
			return err
		}
	}
	return nil
}

// checkInputs refuses a FieldDisplay.Input on e naming no kind, and a
// built-in kind on a field type it does not fit (money on a String).
func (x Extensions) checkInputs(e *entity.Entity) error {
	d := e.Config.Display
	if d == nil {
		return nil
	}
	for _, f := range slices.Sorted(maps.Keys(d.Fields)) {
		in := d.Fields[f].Input
		if in == "" {
			continue
		}
		if _, ok := x.Kinds[in]; ok {
			continue
		}
		fits, ok := builtinKinds[in]
		if !ok {
			return fmt.Errorf("entityui: entity %q field %q: input %q names no kind; register it in Extensions.Kinds", e.GetName(), f, in)
		}
		for _, sf := range e.Config.Fields {
			if sf.Name == f && !slices.Contains(fits, sf.Type) {
				return fmt.Errorf("entityui: entity %q field %q: input %q does not fit the field's type", e.GetName(), f, in)
			}
		}
	}
	return nil
}

// checkViews refuses a view on e with neither a Where nor a registered
// Filter.
func (x Extensions) checkViews(e *entity.Entity) error {
	d := e.Config.Display
	if d == nil {
		return nil
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
	return nil
}

// CheckEntity runs New's checks on an entity registered after it: every
// FieldDisplay.Input names a kind, and every view has a Where or a
// registered Filter. Extensions.Entities can only name entities that
// existed at New, so a later entity's views all need a Where. The host
// calls it before registering the entity and refuses the entity on an
// error.
func (u *UI) CheckEntity(e *entity.Entity) error {
	if err := u.ext.checkInputs(e); err != nil {
		return err
	}
	return u.ext.checkViews(e)
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
		_, knownVariant := ui.ParseButtonVariant(string(a.Variant))
		switch {
		case !entity.ValidKey(a.Key):
			return fmt.Errorf("entityui: entity %q: action key %q is not a key", name, a.Key)
		case seen[a.Key]:
			return fmt.Errorf("entityui: entity %q: duplicate action %q", name, a.Key)
		case a.Run == nil:
			return fmt.Errorf("entityui: entity %q: action %q has no Run", name, a.Key)
		case a.Variant != "" && !knownVariant:
			return fmt.Errorf("entityui: entity %q: action %q: unknown button variant %q", name, a.Key, a.Variant)
		}
		seen[a.Key] = true
	}
	return nil
}

// clone copies the maps and slices New keeps, so the caller changing its
// value later changes nothing New checked.
func (x Extensions) clone() Extensions {
	out := Extensions{Jobs: x.Jobs, FilesURL: x.FilesURL, Kinds: maps.Clone(x.Kinds), Entities: map[string]Extension{}}
	for k, v := range x.Entities {
		v.Views = maps.Clone(v.Views)
		v.Tabs = slices.Clone(v.Tabs)
		v.Actions = slices.Clone(v.Actions)
		out.Entities[k] = v
	}
	return out
}
