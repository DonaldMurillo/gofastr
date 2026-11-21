package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/internal/listfilter"
)

var queueStatuses = []struct {
	value   string
	label   i18n.Phrase
	variant ui.StatusVariant
}{
	{"pending", i18nui.AdminQueuePending, ui.StatusNeutral},
	{"claimed", i18nui.AdminQueueClaimed, ui.StatusInfo},
	{"failed", i18nui.AdminQueueFailed, ui.StatusDanger},
	{"done", i18nui.AdminQueueDone, ui.StatusSuccess},
}

func queueStatus(q url.Values) (status string, unknown bool) {
	s := q.Get("status")
	for _, st := range queueStatuses {
		if st.value == s {
			return s, false
		}
	}
	return "", s != ""
}

func (b *Battery) replayable() (queue.Replayable, bool) {
	rq, ok := b.cfg.Queue.(queue.Replayable)
	return rq, ok
}

const maxReplayAll = 10_000

func (b *Battery) renderQueue(ctx context.Context, _ map[string]string) render.HTML {
	dropped := false
	var query url.Values
	if r := appui.RequestFromContext(ctx); r != nil {
		query = r.URL.Query()
		dropped = listfilter.DroppedNarrowing(r.URL.RawQuery, func(name string) bool { return name == "status" })
	}
	status, unknownStatus := queueStatus(query)
	pageNo, limit := decodePage(query, b.cfg.QueueListLimit, opsMaxLimit)
	carry := url.Values{}
	if status != "" {
		carry.Set("status", status)
	}
	if query.Has("limit") {
		carry.Set("limit", strconv.Itoa(limit))
	}
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.AdminQueue.Text(ctx),
		Subtitle: i18nui.AdminQueueSub.Text(ctx),
	})
	stats, err := b.cfg.Queue.Stats(ctx)
	if err != nil {
		b.logger().Warn("admin: queue stats", "error", err)
		stats = nil
	}
	pages := 1
	if stats != nil {
		pages = pageCount(queueTotal(stats, status), limit)
	}
	pageNo = min(pageNo, pages)
	q := url.Values{}
	for k, v := range carry {
		q[k] = v
	}
	if pageNo > 1 {
		q.Set(pageParam, strconv.Itoa(pageNo))
	}
	page := b.cfg.PathPrefix + "/queue"
	if len(q) > 0 {
		page += "?" + q.Encode()
	}
	jobs, err := b.cfg.Queue.ListJobs(ctx, status, limit, (pageNo-1)*limit)
	if err != nil {
		return b.loadFailed(ctx, "admin: list jobs", i18nui.AdminQueueLoadFailed, err, []render.HTML{header})
	}
	_, canReplay := b.replayable()
	if failed := stats["failed"]; canReplay && failed > 0 {
		header = ui.PageHeader(ui.PageHeaderConfig{
			Title:    i18nui.AdminQueue.Text(ctx),
			Subtitle: i18nui.AdminQueueSub.Text(ctx),
			Actions: b.opForm(ctx, opSpec{
				path: b.cfg.PathPrefix + "/queue/_replay_all", page: page,
				label:   i18nui.AdminReplayAll.Text(ctx, map[string]any{"n": failed}),
				variant: ui.ButtonSecondary,
				confirm: i18nui.AdminReplayAllConfirm.Text(ctx),
			}),
		})
	}
	pager := pagePager(ctx, b.cfg.PathPrefix+"/queue", carry, pageNo, pages)
	var warning render.HTML
	if unknownStatus || dropped {
		warning = listfilter.Warning(ctx)
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		header,
		resultNotice(ctx),
		warning,
		b.queueFilter(ctx, status, stats),
		b.jobsTable(ctx, jobs, status, page, canReplay, 2, pager, false),
	)
}

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

func (b *Battery) queueFilter(ctx context.Context, current string, stats queue.JobStats) render.HTML {
	href := func(status string) string {
		q := url.Values{}
		if status != "" {
			q.Set("status", status)
		}
		if r := appui.RequestFromContext(ctx); r != nil && r.URL.Query().Has("limit") {
			q.Set("limit", r.URL.Query().Get("limit"))
		}
		if len(q) == 0 {
			return b.cfg.PathPrefix + "/queue"
		}
		return b.cfg.PathPrefix + "/queue?" + q.Encode()
	}
	count := func(n int) string {
		if stats == nil {
			return ""
		}
		return i18n.Number(ctx, n)
	}
	items := []ui.SegmentLink{{Text: i18nui.AdminQueueAll.Text(ctx), Count: count(queueTotal(stats, "")), Href: href(""), Current: current == ""}}
	for _, st := range queueStatuses {
		items = append(items, ui.SegmentLink{Text: st.label.Text(ctx), Count: count(stats[st.value]), Href: href(st.value), Current: current == st.value})
	}
	return ui.SegmentedLinks(ui.SegmentedLinksConfig{Label: i18nui.AdminQueueStatus.Text(ctx), Items: items, Ctx: ctx})
}

func jobStatusBadge(ctx context.Context, status string) render.HTML {
	for _, st := range queueStatuses {
		if st.value == status {
			return ui.StatusBadge(ui.StatusBadgeConfig{Label: st.label.Text(ctx), Variant: st.variant})
		}
	}
	if status == "" {
		return render.Text("—")
	}
	return ui.StatusBadge(ui.StatusBadgeConfig{Label: status, Variant: ui.StatusNeutral})
}

func (b *Battery) jobsTable(ctx context.Context, jobs []queue.Job, listed, page string, replay bool, emptyLevel int, pager *ui.PaginationConfig, flush bool) render.HTML {
	cols := []ui.Column{
		{Key: "id", Header: i18nui.AdminColJob.Text(ctx), Phone: ui.PhoneSubtitle},
		{Key: "type", Header: i18nui.AdminColType.Text(ctx), Phone: ui.PhoneTitle},
		{Key: "status", Header: i18nui.AdminQueueStatus.Text(ctx), Phone: ui.PhoneMeta},
		{Key: "attempts", Header: i18nui.AdminColAttempts.Text(ctx), Align: "end"},
		{Key: "updated", Header: i18nui.AdminColUpdated.Text(ctx), Phone: ui.PhoneDetail},
		{Key: "error", Header: i18nui.AdminColLastError.Text(ctx), Truncate: true},
	}
	if replay {
		cols = append(cols, ui.Column{Key: "actions", Header: i18nui.AdminColActions.Text(ctx), Align: "end", Fit: true, Phone: ui.PhoneEnd})
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
			"attempts": render.Text(i18n.Number(ctx, j.Attempts) + " / " + i18n.Number(ctx, j.MaxAttempts)),
			"updated":  agoCell(ctx, time.Now(), updated),
			"error":    lastErr,
		}
		if replay && status == "failed" {
			cells["actions"] = b.opForm(ctx, opSpec{
				path: b.cfg.PathPrefix + "/queue/_replay/" + url.PathEscape(j.ID), page: page,
				label: i18nui.AdminReplay.Text(ctx), variant: ui.ButtonSecondary, small: true,
			})
		}
		rows[i] = ui.Row{ID: j.ID, Cells: cells}
	}
	return ui.DataTable(ui.DataTableConfig{
		Columns:       cols,
		Rows:          rows,
		Caption:       i18nui.AdminQueue.Text(ctx),
		CaptionHidden: true,
		Responsive:    ui.ResponsiveRows,
		Flush:         flush,
		Pagination:    pager,
		Ctx:           ctx,
		Empty: ui.EmptyStateConfig{
			Title:        i18nui.AdminQueueEmpty.Text(ctx),
			Description:  i18nui.AdminQueueEmptyDesc.Text(ctx),
			HeadingLevel: emptyLevel,
		},
	})
}

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
	if id == "" || !textsafe.Storable(id) {
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
		return
	}
	replayed, err := replayJob(r.Context(), rq, id)
	if err != nil {
		if errors.Is(err, queue.ErrDuplicateJob) {
			b.refuse(w, r, page, http.StatusConflict, "replay-duplicate")
			return
		}
		b.logger().Error("admin: replay job", "job", textsafe.ScrubControlBytes(id), "error", textsafe.ScrubControlBytes(err.Error()))
		b.refuse(w, r, page, http.StatusInternalServerError, "failed")
		return
	}
	if !replayed {
		b.refuse(w, r, page, http.StatusConflict, "replay-nothing")
		return
	}
	b.appendAudit(r.Context(), "queue", "replay", id, adminActorID(r.Context()), nil)
	b.done(w, r, page, "replayed")
}

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
	skipped := false
	queued := 0
	for _, id := range ids {
		replayed, err := replayJob(r.Context(), rq, id)
		if err != nil {
			if errors.Is(err, queue.ErrDuplicateJob) {
				skipped = true
				continue
			}
			b.logger().Error("admin: replay job", "job", id, "error", err)
			b.refuse(w, r, page, http.StatusInternalServerError, "failed")
			return
		}
		if replayed {
			queued++
			b.appendAudit(r.Context(), "queue", "replay", id, actor, nil)
		}
	}
	if queued == 0 {
		b.refuse(w, r, page, http.StatusConflict, "replay-all-nothing")
		return
	}
	if skipped {
		b.done(w, r, page, "replayed-some")
		return
	}
	b.done(w, r, page, "replayed-all")
}

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

func replayJob(ctx context.Context, rq queue.Replayable, id string) (bool, error) {
	if rr, ok := rq.(queue.ReplayReporter); ok {
		return rr.ReplayJob(ctx, id)
	}
	return true, rq.Replay(ctx, id)
}

type opSpec struct {
	path, page string
	label      string
	ariaLabel  string
	variant    ui.ButtonVariant
	small      bool
	confirm    string
	fields     map[string]string
	body       []render.HTML
	stacked    bool
	wide       bool
}

func (b *Battery) opForm(ctx context.Context, op opSpec) render.HTML {
	rpc := interactive.Post(op.path)
	if op.confirm != "" {
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
	btn := ui.ButtonConfig{Ctx: ctx, Label: op.label, AriaLabel: op.ariaLabel, Variant: op.variant, Type: "submit"}
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
