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
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Metric is one figure in the dashboard's strip: a count or a sum over an
// exposed entity.
type Metric struct {
	// Label names the figure; a Detail's follows its value ("3 trialing").
	Label i18n.Phrase

	// Entity is an exposed entity's name. A Detail's defaults to its parent's.
	Entity string

	// Agg is "count" (or "") or "sum".
	Agg string

	// Field is the Int, Float or Decimal field a sum totals.
	Field string

	// Where narrows the rows in the list's query DSL. Empty takes every row.
	Where string

	// Format "money" prints a sum as currency.
	Format string

	// View is a key of the entity's Display.Views the figure links to. Empty
	// links to the whole list.
	View string

	// Icon is a registered kit icon name, or "".
	Icon string

	// Detail is a second, smaller figure under the value; it takes no View,
	// Icon or Detail of its own.
	Detail *Metric

	// Tone colours a Detail's line: ui.TrendUp good, ui.TrendDown bad, empty
	// or ui.TrendFlat muted.
	Tone ui.TrendDirection
}

const maxFigures = 6

func (b *Battery) checkMetric(m Metric, parent string) error {
	if m.Label.IsZero() {
		return errors.New("a metric needs a Label")
	}
	switch m.Tone {
	case "", ui.TrendUp, ui.TrendDown, ui.TrendFlat:
	default:
		return fmt.Errorf("%q has the tone %q; use ui.TrendUp, ui.TrendDown or ui.TrendFlat", m.Label.English(), m.Tone)
	}
	if parent == "" && m.Tone != "" {
		return fmt.Errorf("%q has a Tone, which colours a Detail's line: set it on the Detail", m.Label)
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
		return fmt.Errorf("%q counts %q, which the admin does not expose", m.Label.English(), m.Entity)
	}
	if err := b.ui.CheckStat(m.Entity, m.Agg, m.Field, m.Where, m.Format); err != nil {
		return fmt.Errorf("%q: %w", m.Label.English(), err)
	}
	if m.View != "" && !slices.ContainsFunc(listViews(e), func(v entity.ListView) bool { return v.Key == m.View }) {
		return fmt.Errorf("%q links to view %q, which %s does not declare", m.Label.English(), m.View, m.Entity)
	}
	if err := checkIcon(m.Icon, "metric "+m.Label.English()); err != nil {
		return err
	}
	if m.Detail != nil {
		return b.checkMetric(*m.Detail, m.Entity)
	}
	return nil
}

func listViews(e *entity.Entity) []entity.ListView {
	if d := e.Config.Display; d != nil {
		return d.Views
	}
	return nil
}

func (b *Battery) metricStrip(ctx context.Context) render.HTML {
	cells := make([]render.HTML, 0, len(b.cfg.Metrics)+1)
	for i, m := range b.cfg.Metrics {
		cells = append(cells, polled(b.cfg.PathPrefix+"/_metric/"+strconv.Itoa(i), b.metricStat(ctx, m)))
	}
	if b.cfg.Queue != nil && platformCaller(ctx) {
		cells = append(cells, polled(b.cfg.PathPrefix+"/_metric/jobs", b.jobsFigure(ctx)))
	}
	if len(cells) == 0 {
		return ""
	}
	return ui.StatStrip(ui.StatStripConfig{Label: i18nui.AdminMetrics.Text(ctx), Cells: cells})
}

func polled(src string, figure render.HTML) render.HTML {
	return html.Div(html.DivConfig{ExtraAttrs: html.Attrs{
		"data-cui-poll":     countPoll,
		"data-cui-poll-src": src,
	}}, figure)
}

func (b *Battery) metricStat(ctx context.Context, m Metric) render.HTML {
	cctx, cancel := context.WithTimeout(ctx, countDeadline)
	defer cancel()
	e, _ := b.exposedNamed(m.Entity)
	href := b.entityBase(e)
	if m.View != "" {
		href += "?view=" + url.QueryEscape(m.View)
	}
	trend, tone := "", ui.TrendFlat
	if d := m.Detail; d != nil {
		name := d.Entity
		if name == "" {
			name = m.Entity
		}
		trend = b.ui.StatValue(cctx, name, d.Agg, d.Field, d.Where, d.Format) + " " + d.Label.Text(ctx)
		if d.Tone != "" {
			tone = d.Tone
		}
	}
	return ui.StatCard(ui.StatCardConfig{
		Label:     m.Label.Text(ctx),
		Value:     b.ui.StatValue(cctx, m.Entity, m.Agg, m.Field, m.Where, m.Format),
		Trend:     trend,
		Direction: tone,
		Href:      href,
		Icon:      m.Icon,
		Plain:     true,
	})
}

func (b *Battery) jobsFigure(ctx context.Context) render.HTML {
	value, trend := "—", ""
	if stats, err := b.cfg.Queue.Stats(ctx); err == nil {
		value = i18n.Number(ctx, stats["failed"])
		if stats["failed"] > 0 {
			trend = i18nui.AdminNeedsReplay.Text(ctx)
		}
	} else {
		b.logger().Error("admin: queue stats", "error", err)
	}
	return ui.StatCard(ui.StatCardConfig{
		Label:     i18nui.AdminFailedJobs.Text(ctx),
		Value:     value,
		Trend:     trend,
		Direction: ui.TrendDown,
		Href:      b.cfg.PathPrefix + "/queue?status=failed",
		Plain:     true,
	})
}

func (b *Battery) mountMetrics(r *router.Router) {
	r.Get(b.cfg.PathPrefix+"/_metric/{i}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := appui.WithRequest(b.elevate(r.Context()), r)
		var figure render.HTML
		if r.PathValue("i") == "jobs" && b.cfg.Queue != nil {
			if !platformCaller(ctx) {
				i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
				return
			}
			figure = b.jobsFigure(ctx)
		} else {
			i, err := strconv.Atoi(r.PathValue("i"))
			if err != nil || i < 0 || i >= len(b.cfg.Metrics) {
				http.NotFound(w, r)
				return
			}
			figure = b.metricStat(ctx, b.cfg.Metrics[i])
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(figure))
	}))
}
