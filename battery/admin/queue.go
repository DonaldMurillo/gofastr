package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// queueStatuses are the states the Jobs page filters by: DBQueue's
// pending, claimed (running), failed (terminal) and done (kept under
// queue.WithDoneRetention). Any other ?status= reads as All, so request
// text never reaches ListJobs.
var queueStatuses = []struct {
	value   string
	label   i18nui.Key
	variant ui.StatusVariant
}{
	{"pending", i18nui.KeyAdminQueuePending, ui.StatusNeutral},
	{"claimed", i18nui.KeyAdminQueueClaimed, ui.StatusInfo},
	{"failed", i18nui.KeyAdminQueueFailed, ui.StatusDanger},
	{"done", i18nui.KeyAdminQueueDone, ui.StatusSuccess},
}

// queueStatus reads ?status= as one of queueStatuses, or "".
func queueStatus(r *http.Request) string {
	if r == nil {
		return ""
	}
	s := r.URL.Query().Get("status")
	for _, st := range queueStatuses {
		if st.value == s {
			return s
		}
	}
	return ""
}

// replayable returns the queue's replay capability, if it has one.
func (b *Battery) replayable() (queue.Replayable, bool) {
	rq, ok := b.cfg.Queue.(queue.Replayable)
	return rq, ok
}

// queuePageParam is the Jobs page's page number in its query.
const queuePageParam = "p"

// maxReplayAll bounds the jobs one Replay all click re-queues, so the
// request's work has an end however many jobs have failed.
const maxReplayAll = 10_000

// renderQueue draws the Jobs page: the status filter, then a page of
// jobs, newest first, with a pager when they run past one page. The
// failed view of a replayable queue replays one job or every failed job.
func (b *Battery) renderQueue(ctx context.Context, _ map[string]string) render.HTML {
	r := appui.RequestFromContext(ctx)
	status := queueStatus(r)
	limit := b.cfg.QueueListLimit
	pageNo := 1
	carry := url.Values{}
	if status != "" {
		carry.Set("status", status)
	}
	if r != nil {
		q := r.URL.Query()
		limit = parseLimit(q.Get("limit"), b.cfg.QueueListLimit)
		if q.Has("limit") {
			carry.Set("limit", strconv.Itoa(limit))
		}
		if n, err := strconv.Atoi(q.Get(queuePageParam)); err == nil && n > 1 {
			pageNo = n
		}
	}
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.T(ctx, i18nui.KeyAdminQueue),
		Subtitle: i18nui.T(ctx, i18nui.KeyAdminQueueSub),
	})
	stats, err := b.cfg.Queue.Stats(ctx)
	if err != nil {
		// Stats can come back partly filled beside the error; those
		// counts are not trustworthy, so the chips show none and the
		// page draws no pager.
		b.logger().Warn("admin: queue stats", "error", err)
		stats = nil
	}
	pages := 1
	if stats != nil {
		pages = pageCount(queueTotal(stats, status), limit)
	}
	// The page is clamped before it scales by the page size, so a huge
	// ?p= cannot wrap the offset.
	pageNo = min(pageNo, pages)
	q := url.Values{}
	for k, v := range carry {
		q[k] = v
	}
	if pageNo > 1 {
		q.Set(queuePageParam, strconv.Itoa(pageNo))
	}
	page := b.cfg.PathPrefix + "/queue"
	if len(q) > 0 {
		page += "?" + q.Encode()
	}
	jobs, err := b.cfg.Queue.ListJobs(ctx, status, limit, (pageNo-1)*limit)
	if err != nil {
		// Driver text can carry DSNs and hosts: it goes to the log only.
		b.logger().Error("admin: list jobs", "error", err)
		return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, header,
			ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(i18nui.T(ctx, i18nui.KeyAdminQueueLoadFailed))))
	}
	_, canReplay := b.replayable()
	if failed := stats["failed"]; canReplay && failed > 0 {
		header = ui.PageHeader(ui.PageHeaderConfig{
			Title:    i18nui.T(ctx, i18nui.KeyAdminQueue),
			Subtitle: i18nui.T(ctx, i18nui.KeyAdminQueueSub),
			Actions: b.opForm(ctx, opSpec{
				path: b.cfg.PathPrefix + "/queue/_replay_all", page: page,
				label:   i18nui.TVars(ctx, i18nui.KeyAdminReplayAll, map[string]string{"n": strconv.Itoa(failed)}),
				variant: ui.ButtonSecondary,
				confirm: i18nui.T(ctx, i18nui.KeyAdminReplayAllConfirm),
			}),
		})
	}
	var pager *ui.PaginationConfig
	if pages > 1 {
		pager = &ui.PaginationConfig{
			Page:      pageNo,
			Pages:     pages,
			Path:      b.cfg.PathPrefix + "/queue",
			Query:     carry,
			PageParam: queuePageParam,
			Ctx:       ctx,
		}
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		header,
		resultNotice(ctx),
		b.queueFilter(ctx, status, stats),
		b.jobsTable(ctx, jobs, status, page, canReplay, 2, pager),
	)
}

// queueTotal is how many jobs the status view lists: that status's
// count, or every count for All.
func queueTotal(stats queue.JobStats, status string) int {
	if status != "" {
		return stats[status]
	}
	total := 0
	for _, n := range stats {
		total += n
	}
	return total
}

// queueFilter is the status filter: a GET form that navigates to
// ?status=<value>, with each status's count beside it and the sum beside
// All. A nil stats shows no counts.
func (b *Battery) queueFilter(ctx context.Context, current string, stats queue.JobStats) render.HTML {
	all := i18nui.T(ctx, i18nui.KeyAdminQueueAll)
	if stats != nil {
		all = fmt.Sprintf("%s (%d)", all, queueTotal(stats, ""))
	}
	opts := []ui.FacetOption{{Label: all, Value: ""}}
	for _, st := range queueStatuses {
		label := i18nui.T(ctx, st.label)
		if stats != nil {
			label = fmt.Sprintf("%s (%d)", label, stats[st.value])
		}
		opts = append(opts, ui.FacetOption{Label: label, Value: st.value})
	}
	return ui.FilterToolbar(ui.FilterToolbarConfig{
		Action:    b.cfg.PathPrefix + "/queue",
		HideReset: true,
		Ctx:       ctx,
		Facets: []ui.Facet{{
			Name:    "status",
			Label:   i18nui.T(ctx, i18nui.KeyAdminQueueStatus),
			Kind:    ui.FacetPills,
			Value:   current,
			Options: opts,
		}},
	})
}

// jobStatusBadge names a job's state; a status the page does not know
// (another backend's) reads as itself.
func jobStatusBadge(ctx context.Context, status string) render.HTML {
	for _, st := range queueStatuses {
		if st.value == status {
			return ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.T(ctx, st.label), Variant: st.variant})
		}
	}
	if status == "" {
		return render.Text("—")
	}
	return ui.StatusBadge(ui.StatusBadgeConfig{Label: status, Variant: ui.StatusNeutral})
}

// jobsTable draws jobs, with pager under them when it is set: the job,
// its type, status, attempts, when it last changed and the error its
// last failed attempt returned. listed is the status the jobs were
// listed under ("" for All). With replay, each failed row replays its
// job and the answer returns to page.
func (b *Battery) jobsTable(ctx context.Context, jobs []queue.Job, listed, page string, replay bool, emptyLevel int, pager *ui.PaginationConfig) render.HTML {
	cols := []ui.Column{
		{Key: "id", Header: i18nui.T(ctx, i18nui.KeyAdminColJob), Phone: ui.PhoneSubtitle},
		{Key: "type", Header: i18nui.T(ctx, i18nui.KeyAdminColType), Phone: ui.PhoneTitle},
		{Key: "status", Header: i18nui.T(ctx, i18nui.KeyAdminQueueStatus), Phone: ui.PhoneMeta},
		{Key: "attempts", Header: i18nui.T(ctx, i18nui.KeyAdminColAttempts), Align: "end"},
		{Key: "updated", Header: i18nui.T(ctx, i18nui.KeyAdminColUpdated), Phone: ui.PhoneDetail},
		{Key: "error", Header: i18nui.T(ctx, i18nui.KeyAdminColLastError), Truncate: true},
	}
	if replay {
		cols = append(cols, ui.Column{Key: "actions", Header: i18nui.T(ctx, i18nui.KeyAdminColActions), Align: "end", Fit: true, Phone: ui.PhoneEnd})
	}
	rows := make([]ui.Row, len(jobs))
	for i, j := range jobs {
		status := j.Status
		if status == "" {
			status = listed
		}
		updated := j.UpdatedAt
		if updated.IsZero() {
			updated = j.CreatedAt
		}
		lastErr := render.HTML(render.Text("—"))
		if j.LastError != "" {
			lastErr = ui.InlineCodeDanger(j.LastError)
		}
		cells := map[string]render.HTML{
			"id":       ui.ShortID(ui.ShortIDConfig{Value: j.ID, Ctx: ctx}),
			"type":     render.Text(j.Type),
			"status":   jobStatusBadge(ctx, status),
			"attempts": render.Text(fmt.Sprintf("%d / %d", j.Attempts, j.MaxAttempts)),
			"updated":  agoCell(ctx, time.Now(), updated),
			"error":    lastErr,
		}
		if replay && status == "failed" {
			cells["actions"] = b.opForm(ctx, opSpec{
				path: b.cfg.PathPrefix + "/queue/_replay/" + url.PathEscape(j.ID), page: page,
				label: i18nui.T(ctx, i18nui.KeyAdminReplay), variant: ui.ButtonSecondary, small: true,
			})
		}
		rows[i] = ui.Row{ID: j.ID, Cells: cells}
	}
	return ui.DataTable(ui.DataTableConfig{
		Columns:       cols,
		Rows:          rows,
		Caption:       i18nui.T(ctx, i18nui.KeyAdminQueue),
		CaptionHidden: true,
		Responsive:    ui.ResponsiveRows,
		Pagination:    pager,
		Ctx:           ctx,
		Empty: ui.EmptyStateConfig{
			Title:        i18nui.T(ctx, i18nui.KeyAdminQueueEmpty),
			Description:  i18nui.T(ctx, i18nui.KeyAdminQueueEmptyDesc),
			HeadingLevel: emptyLevel,
		},
	})
}

// handleReplay re-queues one failed job and audits who did it.
func (b *Battery) handleReplay(w http.ResponseWriter, r *http.Request) {
	page := b.cfg.PathPrefix + "/queue?status=failed"
	if _, ok := b.readOps(w, r, page); !ok {
		return
	}
	rq, ok := b.replayable()
	if !ok {
		b.refuse(w, r, page, http.StatusNotImplemented, "replay-unsupported")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
		return
	}
	if err := rq.Replay(r.Context(), id); err != nil {
		b.logger().Error("admin: replay job", "job", textsafe.ScrubControlBytes(id), "error", textsafe.ScrubControlBytes(err.Error()))
		b.refuse(w, r, page, http.StatusInternalServerError, "failed")
		return
	}
	b.appendAudit(r.Context(), "queue", "replay", id, adminActorID(r.Context()), nil)
	b.done(w, r, page, "replayed")
}

// handleReplayAll re-queues every failed job, up to maxReplayAll,
// auditing each one. It reads the failed jobs a page at a time first,
// then replays them, so the replays do not move the pages it is still
// reading. A failure stops the run; the jobs replayed before it stay
// replayed and audited.
func (b *Battery) handleReplayAll(w http.ResponseWriter, r *http.Request) {
	page := b.cfg.PathPrefix + "/queue?status=failed"
	if _, ok := b.readOps(w, r, page); !ok {
		return
	}
	rq, ok := b.replayable()
	if !ok {
		b.refuse(w, r, page, http.StatusNotImplemented, "replay-unsupported")
		return
	}
	ids, err := b.failedJobIDs(r.Context())
	if err != nil {
		b.logger().Error("admin: list failed jobs", "error", err)
		b.refuse(w, r, page, http.StatusInternalServerError, "queue-load-failed")
		return
	}
	actor := adminActorID(r.Context())
	for _, id := range ids {
		if err := rq.Replay(r.Context(), id); err != nil {
			b.logger().Error("admin: replay job", "job", id, "error", err)
			b.refuse(w, r, page, http.StatusInternalServerError, "failed")
			return
		}
		b.appendAudit(r.Context(), "queue", "replay", id, actor, nil)
	}
	b.done(w, r, page, "replayed-all")
}

// failedJobIDs reads the failed jobs' ids a page of QueueListLimit at a
// time, newest first, up to maxReplayAll. A page that adds no new id
// ends the read, so a backend that repeats a page cannot keep it going.
func (b *Battery) failedJobIDs(ctx context.Context) ([]string, error) {
	limit := b.cfg.QueueListLimit
	seen := map[string]bool{}
	var ids []string
	for offset := 0; len(ids) < maxReplayAll; offset += limit {
		jobs, err := b.cfg.Queue.ListJobs(ctx, "failed", limit, offset)
		if err != nil {
			return nil, err
		}
		fresh := 0
		for _, j := range jobs {
			if !seen[j.ID] && len(ids) < maxReplayAll {
				seen[j.ID] = true
				ids = append(ids, j.ID)
				fresh++
			}
		}
		if len(jobs) < limit || fresh == 0 {
			break
		}
	}
	return ids, nil
}

// opSpec is one ops action drawn as a form: a form RPC with the runtime
// (the answer's toast, then page re-fetched), a plain POST without it.
type opSpec struct {
	path, page string
	label      string
	ariaLabel  string
	variant    ui.ButtonVariant
	small      bool
	confirm    string
	fields     map[string]string // hidden fields, sorted by name
	body       []render.HTML     // visible fields before the button
	stacked    bool              // fields over the button, for a panel
	wide       bool              // full width, the button in the form's own actions row
}

// opForm draws an opSpec.
func (b *Battery) opForm(ctx context.Context, op opSpec) render.HTML {
	rpc := interactive.Post(op.path)
	if op.confirm != "" {
		// The dialog's accept button is the operation's own label and
		// takes its variant's danger, so a disable asks in red.
		rpc = rpc.WithConfirmDialog(interactive.Confirm{
			Message: op.confirm,
			Accept:  op.label,
			Danger:  op.variant == ui.ButtonDanger,
		})
	}
	rpc = rpc.OnSuccess(interactive.Navigate(op.page))
	children := make([]render.HTML, 0, len(op.fields)+len(op.body)+1)
	for _, name := range sortedKeys(op.fields) {
		children = append(children, html.Input(html.InputConfig{Type: "hidden", Name: name, Value: op.fields[name]}))
	}
	children = append(children, op.body...)
	if op.wide {
		return ui.Form(ui.FormConfig{
			Action:      op.path,
			Method:      "POST",
			Ctx:         ctx,
			Wide:        true,
			SubmitLabel: op.label,
			ExtraAttrs:  rpc.Attrs(),
		}, ui.Stack(ui.StackConfig{Gap: ui.GapMD}, children...))
	}
	btn := ui.ButtonConfig{Label: op.label, AriaLabel: op.ariaLabel, Variant: op.variant, Type: "submit"}
	if op.small {
		btn.Size = ui.ButtonSizeSmall
	}
	children = append(children, ui.Button(btn))
	layout := ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignEnd}, children...)
	if op.stacked {
		layout = ui.Stack(ui.StackConfig{Gap: ui.GapMD}, children...)
	}
	return ui.Form(ui.FormConfig{
		Action:     op.path,
		Method:     "POST",
		Ctx:        ctx,
		HideSubmit: true,
		ExtraAttrs: rpc.Attrs(),
	}, layout)
}
