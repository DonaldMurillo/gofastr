package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Metric is one figure in the strip at the top of the dashboard: a
// count or a sum over an exposed entity, computed by the database in the
// admin's read, the way the entity cards count.
type Metric struct {
	// Label names the figure.
	Label string

	// Entity is an exposed entity's name. A Detail's defaults to its
	// parent's.
	Entity string

	// Agg is "count" (or "") or "sum".
	Agg string

	// Field is the Int, Float or Decimal field a sum totals.
	Field string

	// Where narrows the rows, in the list's query DSL:
	// `status = "active"`. Empty takes every row.
	Where string

	// Format "money" prints a sum as currency.
	Format string

	// View is a key of the entity's Display.Views: the figure links to
	// that list view. Empty links to the whole list.
	View string

	// Icon is a registered kit icon name, or "".
	Icon string

	// Detail is a second, smaller figure under the value, its Label after
	// the number: "$1,240.00 outstanding" beside a count of past-due
	// invoices. A Detail takes no View, Icon or Detail of its own.
	Detail *Metric
}

// checkMetric refuses a metric that could only ever draw "—". parent is
// the entity a Detail inherits; "" for a top-level metric.
func (b *Battery) checkMetric(m Metric, parent string) error {
	if m.Label == "" {
		return errors.New("a metric needs a Label")
	}
	if parent != "" {
		if m.View != "" || m.Icon != "" || m.Detail != nil {
			return fmt.Errorf("the detail %q takes no View, Icon or Detail", m.Label)
		}
		if m.Entity == "" {
			m.Entity = parent
		}
	}
	e, ok := b.exposedNamed(m.Entity)
	if !ok {
		return fmt.Errorf("%q counts %q, which the admin does not expose", m.Label, m.Entity)
	}
	if err := b.ui.CheckStat(m.Entity, m.Agg, m.Field, m.Where, m.Format); err != nil {
		return fmt.Errorf("%q: %w", m.Label, err)
	}
	if m.View != "" && !slices.ContainsFunc(listViews(e), func(v entity.ListView) bool { return v.Key == m.View }) {
		return fmt.Errorf("%q links to view %q, which %s does not declare", m.Label, m.View, m.Entity)
	}
	if err := checkIcon(m.Icon, "metric "+m.Label); err != nil {
		return err
	}
	if m.Detail != nil {
		return b.checkMetric(*m.Detail, m.Entity)
	}
	return nil
}

// listViews is the entity's declared list views.
func listViews(e *entity.Entity) []entity.ListView {
	if d := e.Config.Display; d != nil {
		return d.Views
	}
	return nil
}

// metricStrip is the strip: each figure inside the element that polls
// it.
func (b *Battery) metricStrip(ctx context.Context) render.HTML {
	if len(b.cfg.Metrics) == 0 {
		return ""
	}
	cells := make([]render.HTML, len(b.cfg.Metrics))
	for i, m := range b.cfg.Metrics {
		cells[i] = html.Div(html.DivConfig{ExtraAttrs: html.Attrs{
			"data-cui-poll":     countPoll,
			"data-cui-poll-src": b.cfg.PathPrefix + "/_metric/" + strconv.Itoa(i),
		}}, b.metricStat(ctx, m))
	}
	return ui.Grid(ui.GridConfig{Min: "10rem"}, cells...)
}

// metricStat is one figure's card, read under countDeadline.
func (b *Battery) metricStat(ctx context.Context, m Metric) render.HTML {
	cctx, cancel := context.WithTimeout(ctx, countDeadline)
	defer cancel()
	e, _ := b.exposedNamed(m.Entity)
	href := b.entityBase(e)
	if m.View != "" {
		href += "?view=" + url.QueryEscape(m.View)
	}
	trend := ""
	if d := m.Detail; d != nil {
		name := d.Entity
		if name == "" {
			name = m.Entity
		}
		trend = b.ui.StatValue(cctx, name, d.Agg, d.Field, d.Where, d.Format) + " " + d.Label
	}
	return ui.StatCard(ui.StatCardConfig{
		Label: m.Label,
		Value: b.ui.StatValue(cctx, m.Entity, m.Agg, m.Field, m.Where, m.Format),
		Trend: trend,
		Href:  href,
		Icon:  m.Icon,
	})
}

// mountMetrics mounts GET <prefix>/_metric/<index>, the fragment a
// metric polls, read in the admin's scope like the entity counts.
func (b *Battery) mountMetrics(r *router.Router) {
	r.Get(b.cfg.PathPrefix+"/_metric/{i}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, err := strconv.Atoi(r.PathValue("i"))
		if err != nil || i < 0 || i >= len(b.cfg.Metrics) {
			http.NotFound(w, r)
			return
		}
		ctx := b.elevate(r.Context())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(b.metricStat(appui.WithRequest(ctx, r), b.cfg.Metrics[i])))
	}))
}
