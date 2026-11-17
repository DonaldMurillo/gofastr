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
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Extensions is the code an app registers next to its screens: field
// kinds, view funcs, record tabs and actions.
type Extensions struct {
	// Kinds are named field renderers; Display.Fields[f].Input picks one.
	Kinds map[string]Kind
	// Entities holds per-entity extensions keyed by entity name.
	Entities map[string]Extension
	// Jobs runs bulk actions over more than InRequestCap records; nil
	// refuses such a selection. New refuses Jobs on a Host that is not a BulkHost.
	Jobs JobRunner
	// FilesURL is the same-origin path stored files are served under
	// ("/uploads/"); a relative Image or File value is drawn at FilesURL +
	// key, and not at all when FilesURL is empty.
	FilesURL string
}

// Kind draws one field kind: Input on forms, Cell in list cells and cards,
// Detail read-only (Cell when nil). It never changes what the API stores.
type Kind struct {
	Input  func(InputContext) render.HTML
	Cell   func(CellContext) render.HTML
	Detail func(CellContext) render.HTML
	// Text is the value as plain text for slots that hold no markup (a
	// row link, a card title, a chip); nil prints the type's own text.
	Text func(CellContext) string
}

// InputContext is what a Kind's Input receives: the field, its control
// wiring, input name, current value as text, and the resolved label and help.
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

// CellContext is what a Kind's Cell and Detail receive; Row and Value are
// the read after its hooks.
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
	// Side are panels in the record's side column on the Edit tab.
	Side []SidePanel
	// Actions are record header buttons and, with Bulk, list bulk actions.
	Actions []Action
	// List and Record replace the body of the list or record screen; the
	// route and gates stay the framework's.
	List   func(ListContext) (component.Component, error)
	Record func(RecordContext) (component.Component, error)
}

// ViewFunc is a view whose filter depends on the caller, tenant or clock.
// Show, when set, hides the view; a hidden default view falls back to All.
type ViewFunc struct {
	Filter func(ctx context.Context) (*filter.Predicate, error)
	Show   func(ctx context.Context) bool
}

func asCaller(ctx context.Context) context.Context {
	return crud.WithoutElevation(ctx)
}

// Tab is an extra record tab; the zero Label shows Key.
type Tab struct {
	Key   string
	Label i18n.Phrase
	Build func(TabContext) (component.Component, error)
}

// SidePanel is a panel in a record's side column, inside the record's
// form; a failing Build fails that panel alone. The zero Title shows Key.
type SidePanel struct {
	Key   string
	Title i18n.Phrase
	Build func(RecordContext) (component.Component, error)
}

// Record is the record a tab, action or replaced screen is drawn for.
type Record struct {
	ID     string
	Values map[string]any
}

// TabContext is what a Tab's Build receives.
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

// Action is a record header button, and with Bulk a list bulk action.
// Permission, when set, is held exactly (no Wildcard) about each record on
// top of update access. Variant defaults to ui.ButtonSecondary; the zero
// Label shows Key.
type Action struct {
	Key        string
	Label      i18n.Phrase
	Variant    ui.ButtonVariant
	Permission string
	// Bulk offers the action on the list's selection too.
	Bulk bool
	Run  func(ctx context.Context, ac ActionContext) error
}

// ActionContext is what an Action's Run receives: the resolved selection
// and a CRUD handle scoped to the caller.
type ActionContext struct {
	Entity string
	IDs    []string
	Crud   *crud.CrudHandler
	// Run names the bulk run, the same on every retry of a queued job; a
	// queued record can run twice, so key outside effects on Run and id.
	Run string
}

// InRequestCap is the most records a bulk action runs inside one request.
const InRequestCap = 100

// EveryMatchCap is the most records an "every match" selection resolves.
const EveryMatchCap = 10000

// JobRunner runs a bulk action over more than InRequestCap records: its
// worker calls UI.RunBulkJob with job.ID until it returns nil.
type JobRunner interface {
	// Enqueue schedules job without running it inline; a job may be
	// enqueued twice.
	Enqueue(ctx context.Context, job BulkJob) error
	// Principal rebuilds the creator's context (user, fresh roles, and
	// job.Tenant) before every chunk; an error stops the run.
	Principal(ctx context.Context, job BulkJob) (context.Context, error)
}

// BulkJob is one queued bulk run; its ids live in the snapshot store.
type BulkJob struct {
	ID     string
	Entity string
	// Action is the action key: "delete", "set:<field>:<value>", "move:<key>" or "run:<key>".
	Action string
	Count  int
	// Creator is the confirming user's id and Tenant their tenant ("" for none).
	Creator string
	Tenant  string
	// FilterHash is a SHA-256 of the scope and list query, for the audit trail.
	FilterHash string
	// Key is a SHA-256 of creator, tenant, entity, action and ids; a
	// second confirm of a queued Key answers that job.
	Key string
	// Status is "queued", "done" or "stopped"; Store.Job fills it.
	Status string
	// Done, Skipped and Failed are a finished job's tally.
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

// BulkStore keeps queued bulk runs and their ids. One runner at a time
// holds a job's lease (Claim); Settle and Finish write only for it.
type BulkStore interface {
	// Create writes the job and its ids in one transaction, or returns the
	// queued job with the same Key and writes nothing.
	Create(ctx context.Context, job BulkJob, ids []string) (BulkJob, error)
	// Enqueued records that the JobRunner accepted the job.
	Enqueued(ctx context.Context, id string) error
	// Job reads one job; an unknown id is an error.
	Job(ctx context.Context, id string) (BulkJob, error)
	// Claim gives or renews runner's lease until until; false when the job
	// is not queued or another lease is live at now (expiry == now is expired).
	Claim(ctx context.Context, id, runner string, now, until time.Time) (bool, error)
	// Pending returns up to limit ids not yet settled, in a stable order.
	Pending(ctx context.Context, id string, limit int) ([]string, error)
	// Settle records each id's outcome for the lease holder, else
	// ErrBulkLeaseLost; a settled id leaves Pending.
	Settle(ctx context.Context, id, runner string, outcomes map[string]string) error
	// Tally counts the job's settled ids by outcome across every Settle.
	Tally(ctx context.Context, id string) (map[string]int, error)
	// Finish sets the final status, keeps the tally and deletes the ids,
	// for the lease holder only (else ErrBulkLeaseLost).
	Finish(ctx context.Context, id, runner, status string, at time.Time) error
	// Unenqueued lists queued jobs created before before that are unmarked or whose lease expired.
	Unenqueued(ctx context.Context, before time.Time) ([]BulkJob, error)
	// Prune deletes jobs finished before before and reports how many.
	Prune(ctx context.Context, before time.Time) (int, error)
}

// ErrBulkLeaseLost answers a Settle or Finish from a runner that lost the lease.
var ErrBulkLeaseLost = errors.New("entityui: bulk job lease lost")

// ErrBulkJobBusy is RunBulkJob's answer while another runner holds the lease.
var ErrBulkJobBusy = errors.New("entityui: bulk job held by another runner")

// BulkHost is what a Host implements to back bulk actions: the snapshot
// store and the audit rows. Without it runs stay in the request, unaudited.
type BulkHost interface {
	// BulkStore returns the snapshot store, nil when the host keeps none.
	BulkStore() BulkStore
	// AuditEvent writes one audit row, or nothing when there is no audit log.
	AuditEvent(ctx context.Context, entity, op, recordID string, detail map[string]any) error
}

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
	for _, e := range reg.AllSorted() {
		if err := x.checkViews(e); err != nil {
			return err
		}
	}
	return nil
}

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

// CheckEntity runs New's checks on an entity registered after New: every
// Input names a kind and every view has a Where (or a Filter).
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
	for _, p := range x.Side {
		switch {
		case !entity.ValidKey(p.Key):
			return fmt.Errorf("entityui: entity %q: side panel key %q is not a key", name, p.Key)
		case seen[p.Key]:
			return fmt.Errorf("entityui: entity %q: duplicate side panel %q", name, p.Key)
		case p.Build == nil:
			return fmt.Errorf("entityui: entity %q: side panel %q has no Build", name, p.Key)
		}
		seen[p.Key] = true
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

func (x Extensions) clone() Extensions {
	out := Extensions{Jobs: x.Jobs, FilesURL: x.FilesURL, Kinds: maps.Clone(x.Kinds), Entities: map[string]Extension{}}
	for k, v := range x.Entities {
		v.Views = maps.Clone(v.Views)
		v.Tabs = slices.Clone(v.Tabs)
		v.Side = slices.Clone(v.Side)
		v.Actions = slices.Clone(v.Actions)
		out.Entities[k] = v
	}
	return out
}
