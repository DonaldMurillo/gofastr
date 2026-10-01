package main

// Every screen and fill component. Screens render the primary slot of
// their layout; the fills target the help center's crumbs, toc and
// pager outlets. Real copy everywhere — this is a product surface.

import (
	"context"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/examples/acme-site/helpdocs"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// --- landing -------------------------------------------------------------

// HomeScreen is the marketing landing: the hero with a real artifact,
// two honest sections, and a way into the help center.
type HomeScreen struct{}

func (s *HomeScreen) Render() render.HTML {
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage},
		ui.Stack(ui.StackConfig{Gap: ui.Gap2XL},
			ui.Hero(ui.HeroConfig{
				Eyebrow:  "Acme Tracker 2.4",
				Title:    "Issue tracking that stays out of your way",
				Subtitle: "Projects, issues and activity on one calm surface. Every issue has a URL you can share. Moving between issues keeps the list and its filters in place.",
				Media: ui.TerminalBlock(ui.TerminalBlockConfig{Label: "acme — billing"},
					ui.TerminalOut("$ acme issues --project billing --status open\n"),
					render.Text("BIL-42  Retry card updates         High\n"),
					render.Text("BIL-31  Webhook retries stop at 3  Medium\n"),
					render.Text("BIL-12  Statement PDF font         Low\n"),
					ui.TerminalOK("3 open · filtered from 12 in 84 ms\n"),
				),
			}),
			ui.Section(ui.SectionConfig{
				Eyebrow:     "01 / the product",
				Heading:     "Three surfaces, one product",
				Description: "The sidebar is the map, the issue pane is the work, the activity is the memory. Nothing else to learn.",
			},
				ui.Grid(ui.GridConfig{Min: "16rem"},
					ui.Card(ui.CardConfig{Heading: "Projects", HeadingLevel: 3},
						render.Text("One card per project on the Overview: the open count, the opened-per-week trend, the health badge. Open one and its layer arrives whole.")),
					ui.Card(ui.CardConfig{Heading: "Issues", HeadingLevel: 3},
						render.Text("A list beside its detail, one URL per issue, and a filter you can apply to the list. Back works, because the layers are real.")),
					ui.Card(ui.CardConfig{Heading: "Activity", HeadingLevel: 3},
						render.Text("Every assignment, status change and comment, per issue and across projects. On slow connections it streams in after the issue paints.")),
				),
			),
			ui.Section(ui.SectionConfig{
				Eyebrow:     "02 / the numbers that matter",
				Heading:     "Fast where it matters",
				Description: "Server-rendered pages you can read before a single module loads, and an interactive layer that pays only for what the page uses.",
			},
				ui.MetricBand(ui.MetricBandConfig{Items: []ui.MetricBandItem{
					{Label: "Issue page", Value: "SSR-first", Hint: "readable with JavaScript off"},
					{Label: "Activity", Value: "streams in", Hint: "its own request beside the page"},
					{Label: "Navigation", Value: "no reloads", Hint: "one layout, swapped in place"},
				}}),
			),
			ui.Section(ui.SectionConfig{
				Eyebrow:     "03 / start here",
				Heading:     "The help center is the product tour",
				Description: "Seven short articles, written the way the tracker works: projects, issues, filters, notifications, shortcuts and what archiving keeps.",
			},
				ui.LinkButton(ui.LinkButtonConfig{Label: "Browse the help center", Href: "/help"}),
			),
		),
	)
}

// --- pricing -------------------------------------------------------------

// PricingScreen is the plans page: three tiers and the band of what
// every plan carries.
type PricingScreen struct{}

func (s *PricingScreen) Render() render.HTML {
	cards := make([]render.HTML, 0, len(plans))
	for _, p := range plans {
		cards = append(cards, ui.PricingCard(ui.PricingCardConfig{
			Name: p.Name, Price: p.Price, Period: p.Period,
			Description: p.Description, Features: p.Features,
			CTALabel: p.CTALabel, CTAHref: p.CTAHref,
			Featured:     p.Featured,
			HeadingLevel: 2, // directly under the page's h1
		}))
	}
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadEnd},
		ui.Stack(ui.StackConfig{Gap: ui.Gap2XL},
			ui.PageHeader(ui.PageHeaderConfig{
				Eyebrow:  "Pricing",
				Title:    "Pay for the team, not the seat-count ceremony",
				Subtitle: "Three plans, no feature roulette: everything that makes the tracker pleasant to live in is in Team, and Team is the price of two coffees.",
			}),
			ui.Grid(ui.GridConfig{Min: "18rem"}, cards...),
			ui.Section(ui.SectionConfig{
				Eyebrow:     "every plan",
				Heading:     "What every plan includes",
				Description: "The parts of the product that are not features, just how it is built.",
			},
				ui.Grid(ui.GridConfig{Min: "14rem"},
					ui.FactBox(ui.FactBoxConfig{Label: "URLs", Value: "Every issue, its own"}),
					ui.FactBox(ui.FactBoxConfig{Label: "No reloads", Value: "Navigation swaps layouts"}),
					ui.FactBox(ui.FactBoxConfig{Label: "JavaScript off", Value: "The site still reads"}),
					ui.FactBox(ui.FactBoxConfig{Label: "Dark scheme", Value: "One toggle, everywhere"}),
				),
			),
		),
	)
}

// --- changelog -------------------------------------------------------------

// ChangelogScreen is the release notes: one section per release, newest
// first. The first section's id is the anchor the announcement bar
// links to.
type ChangelogScreen struct{}

func (s *ChangelogScreen) Render() render.HTML {
	sections := make([]render.HTML, 0, len(releases))
	for _, r := range releases {
		items := make([]render.HTML, 0, len(r.Items))
		for _, it := range r.Items {
			items = append(items, html.ListItem(html.ListItemConfig{}, render.Text(it)))
		}
		sections = append(sections, ui.Section(ui.SectionConfig{
			Compact: true,
			ID:      r.ID,
			Eyebrow: "Released " + r.Date,
			Heading: "Acme Tracker " + r.Version,
		},
			html.UnorderedList(html.ListConfig{}, items...),
		))
	}
	// One reading column for the whole page, so the header and the
	// releases share a left edge.
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerNarrow, Pad: ui.ContainerPadEnd},
		ui.Stack(ui.StackConfig{Gap: ui.Gap2XL},
			ui.PageHeader(ui.PageHeaderConfig{
				Eyebrow:  "Changelog",
				Title:    "What shipped, when",
				Subtitle: "Every release of Acme Tracker, newest first, in the words of the people who shipped it.",
			}),
			ui.Stack(ui.StackConfig{Gap: ui.Gap2XL}, sections...),
		),
	)
}

// --- the help center ---------------------------------------------------------

// HelpIndexScreen is the help center's front page: every article as a
// card with its one-line summary.
type HelpIndexScreen struct{}

func (s *HelpIndexScreen) Render() render.HTML {
	cards := make([]render.HTML, 0, len(helpArticles))
	for _, a := range helpArticles {
		cards = append(cards, ui.Card(ui.CardConfig{
			Heading: a.Title, HeadingLevel: 2, Description: a.Summary,
			Footer: ui.LinkButton(ui.LinkButtonConfig{
				Label: "Read", Href: "/help/" + a.Slug, Variant: ui.ButtonGhost, Size: ui.ButtonSizeSmall,
			}),
		}))
	}
	// The help page (helpdocs) places the column; the index only stacks.
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow:  "Help center",
			Title:    "Seven short articles, the whole product",
			Subtitle: "Written the way the tracker works. If an article is wrong, the article is what we fix.",
		}),
		ui.Grid(ui.GridConfig{Min: "16rem"}, cards...),
	)
}

// HelpArticleScreen renders one article: its title as the page's h1 and
// its blocks. Headings carry the ids the toc outlet links to.
type HelpArticleScreen struct {
	art HelpArticle
}

func (s *HelpArticleScreen) SetParams(m map[string]string) {}

func (s *HelpArticleScreen) Load(ctx context.Context) error {
	a, err := resolvedArticle.Get(ctx)
	if err != nil {
		return err
	}
	s.art = a
	return nil
}

// ScreenTitle names the tab after the article.
func (s *HelpArticleScreen) ScreenTitle() string {
	return s.art.Title + " — Acme Tracker help"
}

func (s *HelpArticleScreen) Render() render.HTML {
	body := make([]render.HTML, 0, len(s.art.Blocks)+1)
	body = append(body, ui.PageHeader(ui.PageHeaderConfig{Title: s.art.Title, Compact: true, Class: helpdocs.Style.Title()}))
	for _, b := range s.art.Blocks {
		switch {
		case b.heading != "":
			level := b.level
			if level == 0 {
				level = 2
			}
			body = append(body, html.Heading(html.HeadingConfig{
				Level:      level,
				ExtraAttrs: html.Attrs{"id": slugify(b.heading)},
			}, render.Text(b.heading)))
		case b.para != "":
			body = append(body, html.Paragraph(html.TextConfig{}, render.Text(b.para)))
		case b.bullets != nil:
			items := make([]render.HTML, 0, len(b.bullets))
			for _, it := range b.bullets {
				items = append(items, html.ListItem(html.ListItemConfig{}, render.Text(it)))
			}
			body = append(body, html.UnorderedList(html.ListConfig{}, items...))
		case b.note != "":
			body = append(body, ui.Callout(ui.CalloutConfig{Title: "Good to know", Variant: ui.StatusInfo},
				render.Text(b.note)))
		case b.shortcuts != nil:
			rows := make([]render.HTML, 0, len(b.shortcuts))
			for _, sc := range b.shortcuts {
				rows = append(rows, ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD},
					ui.ShortcutHint(ui.ShortcutHintConfig{Chord: sc[0]}),
					html.Span(html.TextConfig{}, render.Text(sc[1])),
				))
			}
			body = append(body, ui.Stack(ui.StackConfig{Gap: ui.GapMD}, rows...))
		}
	}
	return render.Join(body...)
}

// --- the help center's fills ---------------------------------------------------

// IndexCrumbs fills the index's crumbs outlet: Home, then here.
type IndexCrumbs struct{}

func (c *IndexCrumbs) Render() render.HTML {
	return ui.Breadcrumbs(ui.BreadcrumbsConfig{Label: "Breadcrumb"},
		ui.Crumb{Text: "Home", Href: "/"},
		ui.Crumb{Text: "Help center", Current: true},
	)
}

// ArticleCrumbs fills an article's crumbs outlet: Home, Help center,
// then the article.
type ArticleCrumbs struct {
	title string
}

func (c *ArticleCrumbs) SetParams(m map[string]string) {}

func (c *ArticleCrumbs) Load(ctx context.Context) error {
	a, err := resolvedArticle.Get(ctx)
	if err != nil {
		return err
	}
	c.title = a.Title
	return nil
}

func (c *ArticleCrumbs) Render() render.HTML {
	return ui.Breadcrumbs(ui.BreadcrumbsConfig{Label: "Breadcrumb"},
		ui.Crumb{Text: "Home", Href: "/"},
		ui.Crumb{Text: "Help center", Href: "/help"},
		ui.Crumb{Text: c.title, Current: true},
	)
}

// ArticleToc fills the toc outlet with the article's own headings. An
// article with no headings declines (ErrNoFill): the outlet renders
// empty and helpdocs collapses the TOC column.
type ArticleToc struct {
	items []ui.TOCItem
}

func (t *ArticleToc) SetParams(m map[string]string) {}

func (t *ArticleToc) Load(ctx context.Context) error {
	a, err := resolvedArticle.Get(ctx)
	if err != nil {
		return err
	}
	for _, b := range a.Blocks {
		if b.heading == "" {
			continue
		}
		level := b.level
		if level == 0 {
			level = 2
		}
		t.items = append(t.items, ui.TOCItem{ID: slugify(b.heading), Label: b.heading, Level: level})
	}
	if len(t.items) == 0 {
		return uiapp.ErrNoFill
	}
	return nil
}

func (t *ArticleToc) Render() render.HTML {
	return ui.TableOfContents(ui.TOCConfig{
		Items:  t.items,
		Target: "article",
		Sticky: true,
	})
}

// ArticlePager fills the pager outlet: the previous article (the index
// for the first) and the next (nothing for the last — helpdocs.Pager
// omits the card).
type ArticlePager struct {
	prevHref, prevLabel, nextHref, nextLabel string
}

func (p *ArticlePager) SetParams(m map[string]string) {}

func (p *ArticlePager) Load(ctx context.Context) error {
	a, err := resolvedArticle.Get(ctx)
	if err != nil {
		return err
	}
	i := 0
	for j, cand := range helpArticles {
		if cand.Slug == a.Slug {
			i = j
			break
		}
	}
	if i > 0 {
		p.prevHref = "/help/" + helpArticles[i-1].Slug
		p.prevLabel = helpArticles[i-1].Title
	} else {
		p.prevHref = "/help"
		p.prevLabel = "Help center"
	}
	if i+1 < len(helpArticles) {
		p.nextHref = "/help/" + helpArticles[i+1].Slug
		p.nextLabel = helpArticles[i+1].Title
	}
	return nil
}

func (p *ArticlePager) Render() render.HTML {
	return helpdocs.Pager(helpdocs.PagerConfig{
		PrevHref: p.prevHref, PrevLabel: p.prevLabel,
		NextHref: p.nextHref, NextLabel: p.nextLabel,
	})
}

// --- helpers -----------------------------------------------------------------

// slugify turns a heading into its fragment id: lowercase, words joined
// with '-', anything else dropped.
func slugify(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, byte(r))
		case r >= 'A' && r <= 'Z':
			out = append(out, byte(r-'A'+'a'))
		case r == ' ', r == '-', r == '_', r == '.', r == '/', r == '\'':
			if len(out) > 0 && out[len(out)-1] != '-' {
				out = append(out, '-')
			}
		}
	}
	// Trim a trailing joiner.
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}
