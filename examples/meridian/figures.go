package main

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
)

// Meridian draws some stats and charts with hand-written readers (the
// admin Revenue page, the customer Billing panel) instead of generated
// blocks, so the generated app's boot checks never see them. Each
// surface keeps its figures in one list that its render reads and
// checkFigures judges once at boot: a figure that could never compute (a
// filter or group_by naming a column the entity lacks) stops the start,
// naming the surface and the figure, instead of drawing "—" or an empty
// chart forever. The Overview screen keeps its generated shape, which
// gofastr pack reads back, and checks its cards in mountDashboardScreen
// the way a generated mount does.

// figureStat is one stat a surface draws through entityui's StatValue,
// or its CountInt (a count: agg and format empty).
type figureStat struct {
	label                             i18n.Phrase
	icon                              string
	entity, agg, field, where, format string
}

// value is the stat as StatValue draws it for the caller.
func (s figureStat) value(ctx context.Context, u *entityui.UI) string {
	return u.StatValue(ctx, s.entity, s.agg, s.field, s.where, s.format)
}

// figureChart is one chart a surface draws through entityui's GroupBars.
type figureChart struct {
	title           i18n.Phrase
	entity, groupBy string
}

// checkFigures reports the first of surface's figures (surface reads
// `screen "dashboard"` or `admin page "revenue"`) that entityui's
// CheckStat or CheckChart refuses, named by its English label.
func checkFigures(u *entityui.UI, surface string, stats []figureStat, charts []figureChart) error {
	for _, s := range stats {
		if err := u.CheckStat(s.entity, s.agg, s.field, s.where, s.format); err != nil {
			return fmt.Errorf("%s stat %q: %w", surface, s.label.English(), err)
		}
	}
	for _, c := range charts {
		if err := u.CheckChart(c.entity, c.groupBy); err != nil {
			return fmt.Errorf("%s chart %q: %w", surface, c.title.English(), err)
		}
	}
	return nil
}
