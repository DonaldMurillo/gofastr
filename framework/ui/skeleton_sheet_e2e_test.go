package ui_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

func TestSkeletonSheetHidesAnnouncements(t *testing.T) {
	entry, ok := registry.Lookup("ui-skeleton-presets")
	if !ok {
		t.Fatal("preset stylesheet not registered")
	}
	body := render.Join(ui.SkeletonCard(ui.SkeletonCardConfig{}), ui.SkeletonRow(ui.SkeletonRowConfig{}), ui.SkeletonAvatar(ui.SkeletonAvatarConfig{}), ui.SkeletonTimeline(ui.SkeletonTimelineConfig{Rows: 2}), ui.SkeletonLine(ui.SkeletonLineConfig{}))
	server := themeTestPageWithHead(t, "<style>"+entry.CSSFor(theme.Default())+"</style>", string(body))
	ctx := moduleTestCtxURL(t, server.URL)
	var problems []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const roots = [...document.querySelectorAll('[data-cui-comp="ui-skeleton-presets"]')];
		const problems = [];
		if (roots.length !== 5) problems.push('missing preset');
		for (const root of roots) {
			const announcements = root.querySelectorAll('.fui-visually-hidden');
			if (announcements.length !== 1 || root.getAttribute('role') !== 'status' || root.hasAttribute('aria-hidden')) { problems.push('lost accessible announcement'); continue; }
			const announcement = announcements[0];
			const style = getComputedStyle(announcement);
			const box = announcement.getBoundingClientRect();
			if (style.position !== 'absolute' || box.width > 1 || box.height > 1 || style.display === 'none' || style.visibility === 'hidden' || announcement.hasAttribute('aria-hidden') || !announcement.textContent.trim()) problems.push(root.className + ': visible or inaccessible announcement');
		}
		return problems;
	})()`, &problems)); err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("standalone stylesheet: %v", problems)
	}
}
