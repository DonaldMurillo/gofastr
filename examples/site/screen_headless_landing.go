package main

// =============================================================================
// /examples/headless/{theme}/landing — the theme-layer showcase.
//
// One screen implementation parameterised by the theme segment. The themes
// are boot-registered scoped overrides (style.RegisterThemeOverride at
// package init; hashing is lazy so a package-level var is safe), and every
// route renders its content inside ui.Themed(ref, …) so the page's own
// chrome keeps the site theme while the landing content answers to the
// route's theme: its palette, its dark palette, and its component options
// (density, button treatment, button radius).
//
// The fixtures are proofs, not decoration (DESIGN-foundation.md "The
// showcase"; EVAL 2026-09-16 §6): every button variant and size, the same
// palette under two option sets, an A → B → A nest, explicit scheme
// controls, bare headless beside styled ui, and a cold LoadAuto insertion
// whose stylesheet the runtime must fetch on arrival. Button is LoadAlways
// and proves nothing about demand loading; ui.Callout is LoadAuto and
// appears nowhere else on the initial page, so its sheet is genuinely cold.
//
// Everything composes framework/ui, core-ui/html and framework/headless:
// zero CSS and zero hand-rolled structural markup live in this file.
// =============================================================================

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// ── The registered themes ──────────────────────────────────────────

// landingFrameworkTheme is the framework's default look on the site's
// palette: comfortable density, filled treatment, round radius. It carries
// a dark palette (createTheme keeps the framework default's complete dark
// map; the dark primary is aligned with the site's amber so the accent
// survives the scheme flip).
func landingFrameworkTheme() style.Theme {
	t := createTheme()
	// danger keeps the site's light red rather than the framework's
	// mid #B91C1C for a reason of its own: this palette is dark-first
	// (near-black background, see createTheme), and the outline
	// treatment the tight twin renders paints the danger colour AS
	// text on those surfaces, where #B91C1C only reaches ~3.1:1 while
	// this red clears AA (it is the value the site's own stylesheets
	// ship, see styles.go). The filled pair needs no help anymore: the
	// compiler paints --color-danger-fg there, so the dark amber
	// primary-fg no longer leaks onto the danger fill.
	t.Colors.Danger = style.Color{Name: "danger", Value: "oklch(0.72 0.16 25)"}
	// The ink that light red is built for: the palette's near-black
	// (the same --on-accent value primary-fg carries). The framework's
	// default white danger-fg is tuned for its mid #B91C1C, not this
	// red — the token exists precisely so a palette owns both halves
	// of the pair.
	t.Colors.DangerFg = style.Color{Name: "danger-fg", Value: "oklch(0.14 0.005 75)"}
	t.DarkColors["primary"] = "#F2B14D"
	t.DarkColors["primary-fg"] = "#161310"
	t.DarkColors["accent"] = "#F2B14D"
	return t
}

// landingDenseTheme is the dense look: compact density, outline treatment,
// square radius, a distinct primary (teal, contrast-safe in both schemes)
// and its own teal-tinted dark palette, distinct from the site root's
// zinc dark so the scoped dark block is observable against the root's.
func landingDenseTheme() style.Theme {
	return theme.Default(theme.Overrides{
		Primary:   "#0F766E",
		PrimaryFg: "#FFFFFF",
		Accent:    "#0F766E",
		DarkColors: map[string]string{
			"primary":       "#5EEAD4",
			"primary-fg":    "#042F2E",
			"accent":        "#5EEAD4",
			"background":    "#0C1A19",
			"surface":       "#122422",
			"surface-soft":  "#1A2F2D",
			"border":        "#27403E",
			"border-strong": "#3E5B58",
			"text":          "#E6F5F3",
			"text-muted":    "#A7C4C1",
			"text-subtle":   "#7FA3A0",
		},
		Components: landingTightOptions,
	})
}

// landingTightOptions and landingRelaxedOptions are the two complete
// option sets the showcase flips between. Every registered theme carries a
// complete set — that is what makes option variables nest.
var (
	landingTightOptions = theme.ComponentOptions{
		Density: theme.Compact,
		Button:  theme.ButtonOptions{Treatment: theme.Outline, Radius: theme.Square},
	}
	landingRelaxedOptions = theme.ComponentOptions{
		Density: theme.Comfortable,
		Button:  theme.ButtonOptions{Treatment: theme.Filled, Radius: theme.Round},
	}
)

// withLandingOptions returns a copy of t carrying the given complete
// option set, tokens untouched. The option-only twins below are built with
// it: same palette, different options — the fixture that isolates the
// option compiler from the palette.
func withLandingOptions(t style.Theme, o theme.ComponentOptions) style.Theme {
	t.Components = o.Complete().Flattened()
	return t
}

// The registered refs. Registration must precede the first render (the
// host freezes app.css); package init is that guarantee, and hashing is
// lazy so init-time registration cannot clash with framework/ui's own
// compiler registration.
var (
	landingRefFramework = style.RegisterThemeOverride(landingFrameworkTheme())
	landingRefDense     = style.RegisterThemeOverride(landingDenseTheme())
	// Option-only twins: each route's palette under the other option set.
	landingRefFrameworkTight = style.RegisterThemeOverride(withLandingOptions(landingFrameworkTheme(), landingTightOptions))
	landingRefDenseRelaxed   = style.RegisterThemeOverride(withLandingOptions(landingDenseTheme(), landingRelaxedOptions))
)

// landingRoute is one theme segment of the showcase.
type landingRoute struct {
	Segment string // URL segment: "default" | "dense"
	Name    string
	// Ref is the route's theme; Twin is the same palette under the other
	// option set; Other is the other route's theme (the B of A → B → A).
	Ref, Twin, Other style.ThemeRef
}

var landingRoutes = []landingRoute{
	{
		Segment: "default",
		Name:    "Framework default",
		Ref:     landingRefFramework,
		Twin:    landingRefFrameworkTight,
		Other:   landingRefDense,
	},
	{
		Segment: "dense",
		Name:    "Dense",
		Ref:     landingRefDense,
		Twin:    landingRefDenseRelaxed,
		Other:   landingRefFramework,
	},
}

// landingRouteFor resolves a URL segment to its route. Unknown segments
// are absent so Load can 404 them.
func landingRouteFor(segment string) (landingRoute, bool) {
	for _, r := range landingRoutes {
		if r.Segment == segment {
			return r, true
		}
	}
	return landingRoute{}, false
}

// landingRoutePath is the canonical URL of one route.
func landingRoutePath(segment string) string {
	return "/examples/headless/" + segment + "/landing"
}

// ── The screen ─────────────────────────────────────────────────────
// HeadlessLandingScreen is /examples/headless/:theme/landing.
type HeadlessLandingScreen struct {
	Route landingRoute
	// Subscribe carries the newsletter round trip's no-script state,
	// read from the landing route's query (see Load): the 303 the
	// native POST answers leaves the answer in the URL, and the page
	// renders its newsletter region from it.
	Subscribe landingSubscribeState
}

func (s *HeadlessLandingScreen) ScreenTitle() string {
	return "Headless landing · " + s.Route.Name + " theme"
}

func (s *HeadlessLandingScreen) ScreenDescription() string {
	return "The theme layer and the Button rebuild on a real page: two registered themes, option fixtures, nesting, and a cold LoadAuto insertion."
}

func (s *HeadlessLandingScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// SetParams resolves the theme segment. An unknown segment leaves the
// route zero so Load rejects it.
func (s *HeadlessLandingScreen) SetParams(p map[string]string) {
	if r, ok := landingRouteFor(p["theme"]); ok {
		s.Route = r
	}
}

// Load rejects unknown theme segments so the site's 404 screen answers
// them (a panic would 500; a wrong theme would silently lie), and reads
// the newsletter round trip's query: the no-script POST is answered with
// a 303 back here carrying the outcome — blank, invalid or ok — and
// the page renders its region from it. The outcome travels, never the
// address: a submitted value in a query string lands in the reader's
// history and in any referrer a later click sends, and this page needs
// the message, not the value. The key is render state, not a route —
// it never reaches StaticPaths.
func (s *HeadlessLandingScreen) Load(ctx context.Context) error {
	if _, ok := landingRouteFor(s.Route.Segment); !ok {
		return errors.New("headless landing: unknown theme " + s.Route.Segment)
	}
	switch o := landingSubscribeOutcome(app.QueryFromContext(ctx).Get("subscribe")); o {
	case landingSubscribeOK:
		s.Subscribe = landingSubscribeState{Done: true}
	case landingSubscribeBlank, landingSubscribeInvalid:
		s.Subscribe = landingSubscribeState{Error: landingSubscribeMessage(o)}
	}
	return nil
}

// StaticPaths enumerates one page per registered theme so the static
// export, the sitemap, llm.md, and the coverage gate all see both routes.
func (s *HeadlessLandingScreen) StaticPaths(ctx context.Context) []map[string]string {
	out := make([]map[string]string, 0, len(landingRoutes))
	for _, r := range landingRoutes {
		out = append(out, map[string]string{"theme": r.Segment})
	}
	return out
}
func (s *HeadlessLandingScreen) Render() render.HTML {
	return s.render(context.Background())
}

// RenderCtx renders with the request's context, so the headless
// components the page renders directly resolve their words through
// ui.StringsFor(ctx) — the Strings bridge. The site installs no
// translator, so today that means the English defaults; a site built
// with WithI18n would hear the same fixtures in the reader's locale
// with nothing in this file changing. The static export renders
// through RenderCtx too, with the build's context (no translator, so
// English today; a per-locale export would flow through it with no
// change here); Render keeps the background ctx for the callers that
// take a plain Component, llm.md among them.
func (s *HeadlessLandingScreen) RenderCtx(ctx context.Context) render.HTML {
	return s.render(ctx)
}

func (s *HeadlessLandingScreen) render(ctx context.Context) render.HTML {
	r := s.Route
	return ui.Themed(r.Ref, container(
		landingHero(r),
		landingContentSection(),
		landingNewsletterSection(r, s.Subscribe),
		landingVariantsSection(),
		landingOptionsSection(r),
		landingNestingSection(r),
		landingSchemeSection(),
		landingBareSection(ctx),
		landingLateSection(),
	))
}

// ── Hero ───────────────────────────────────────────────────────────

var (
	landingHeroImageOnce sync.Once
	landingHeroImageVal  struct{ fallback, placeholder string }
)

// landingHeroImage draws the hero visual in-process (no committed binary)
// and encodes it plus a BlurHash placeholder, the same shape the gallery's
// pipeline demo uses: the image goes through the site's image pipeline as
// data URIs.
func landingHeroImage() (fallback, placeholder string) {
	landingHeroImageOnce.Do(func() {
		w, h := 960, 540
		canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
		block := func(x0, y0, x1, y1 int, c color.NRGBA) {
			draw.Draw(canvas, image.Rect(x0, y0, x1, y1), &image.Uniform{C: c}, image.Point{}, draw.Src)
		}
		var (
			ink    = color.NRGBA{R: 0x1F, G: 0x1D, B: 0x1A, A: 0xFF}
			amber  = color.NRGBA{R: 0xF2, G: 0xB1, B: 0x4D, A: 0xFF}
			teal   = color.NRGBA{R: 0x14, G: 0xB8, B: 0xA6, A: 0xFF}
			slate  = color.NRGBA{R: 0x3A, G: 0x37, B: 0x33, A: 0xFF}
			paper  = color.NRGBA{R: 0xFA, G: 0xFA, B: 0xF9, A: 0xFF}
			indigo = color.NRGBA{R: 0x6E, G: 0x7C, B: 0xF4, A: 0xFF}
		)
		block(0, 0, w, h, ink)
		block(0, 0, w, h/7, amber)
		block(w/24, h/4, w/2, h-h/8, slate)              // chart panel
		block(w/2+w/24, h/4, w-w/24, h/2, teal)          // card one
		block(w/2+w/24, h/2+h/24, w-w/24, h-h/8, indigo) // card two
		block(w/24, h-h/10, w/3, h-h/10+h/48, paper)     // footer rule
		src := fwimage.FromImage(canvas, fwimage.FormatPNG)
		full, err := src.JPEG(fwimage.JPEGOptions{Quality: 78}).DataURL()
		if err != nil {
			return
		}
		hash, err := src.BlurHash(4, 3)
		if err != nil {
			landingHeroImageVal.fallback = full
			return
		}
		ph, err := fwimage.BlurHashDataURL(hash, fwimage.BlurHashRenderConfig{})
		if err != nil {
			landingHeroImageVal.fallback = full
			return
		}
		landingHeroImageVal.fallback = full
		landingHeroImageVal.placeholder = ph
	})
	return landingHeroImageVal.fallback, landingHeroImageVal.placeholder
}

func landingHero(r landingRoute) render.HTML {
	fallback, placeholder := landingHeroImage()
	return ui.Hero(ui.HeroConfig{
		Eyebrow:   "framework/ui on framework/headless",
		Title:     "One page, two themes, zero bespoke CSS",
		Subtitle:  "Every control below reads its look from the route's registered theme: palette tokens for colour, component options for density, treatment and radius. Same markup, same classes, different theme boundary.",
		AriaLabel: "Headless landing, " + r.Name + " theme",
		Actions: []render.HTML{
			ui.LinkButton(ui.LinkButtonConfig{Label: "Get started", Href: "/get-started"}),
			ui.LinkButton(ui.LinkButtonConfig{Label: "How theming works", Href: "/docs/theming", Variant: ui.ButtonSecondary}),
		},
		Media: ui.PipelineImage(ui.PipelineImageConfig{
			Fallback:    fallback,
			Placeholder: placeholder,
			Alt:         "A generated dashboard mockup standing in for a product shot, drawn through the framework's image pipeline.",
			Width:       960,
			Height:      540,
			Aspect:      ui.ImageAspect16x9,
			Rounded:     true,
		}),
	})
}

// ── Long content ───────────────────────────────────────────────────

const landingContentMarkdown = `This page is the living proof for two documents: [headless
components](/docs/ui-headless) own structure, and [theming](/docs/theming)
owns the look. Nothing between the hero and this sentence carries a
site-authored style; every value resolves through a theme variable.

### Why two layers

A component's structure — its tags, roles, label wiring — changes with the
design system's semantics, not with its palette. Splitting the two means a
re-skin is a theme edit, and an accessibility fix never re-opens a colour
decision. The [Button rebuild](/docs/ui-new-components) is the first
component to ship on that split.

### What the fixtures below prove

- **Options, not palette.** The "same palette" fixture changes only the
  option set; the colours are the page's own.
- **Nesting by inheritance.** Theme boundaries declare the option
  variables, component rules consume them, so A → B → A ends on A without
  a single descendant rule.
- **Demand loading.** The late fragment's stylesheet is not on the page
  until the runtime sees its marker.`

func landingContentSection() render.HTML {
	return ui.Section(ui.SectionConfig{
		ID:          "hl-content",
		Heading:     "The page under the theme",
		Description: "Long-form content, headings and links, all reading theme tokens.",
	}, ui.Markdown(ui.MarkdownConfig{Source: landingContentMarkdown}))
}

// ── Newsletter (island with a no-script round trip) ────────────────

const (
	landingSubscribePath   = "/__site/headless/subscribe"
	landingLatePath        = "/__site/headless/late"
	landingSubscribeSignal = "hl-subscribe"
)

// landingSubscribeState is the newsletter form's render state.
type landingSubscribeState struct {
	Email string
	Error string
	Done  bool
}

// landingSubscribeOutcome is what the server decided about a submitted
// address. The no-script redirect carries this, not the address.
type landingSubscribeOutcome string

const (
	landingSubscribeOK      landingSubscribeOutcome = "ok"
	landingSubscribeBlank   landingSubscribeOutcome = "blank"
	landingSubscribeInvalid landingSubscribeOutcome = "invalid"
)

// landingValidateEmail applies the server-side check both paths share.
// The form is novalidate on purpose: the server owns validation so the
// island and the no-script round trip answer identically.
func landingValidateEmail(email string) landingSubscribeOutcome {
	email = strings.TrimSpace(email)
	if email == "" {
		return landingSubscribeBlank
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return landingSubscribeInvalid
	}
	return landingSubscribeOK
}

// landingSubscribeMessage is the sentence an outcome renders; empty for
// a valid address. It lives beside the outcome so the no-script answer
// and the island answer say the same words from the same place.
func landingSubscribeMessage(o landingSubscribeOutcome) string {
	switch o {
	case landingSubscribeBlank:
		return "Enter an email address."
	case landingSubscribeInvalid:
		return "That address does not parse as an email."
	}
	return ""
}

// falsePtr is the *bool for Callout's Landmark field (inline callouts are
// not complementary landmarks).
func falsePtr() *bool {
	b := false
	return &b
}

// landingSubscribeErrorSummary renders the focusable error summary: a
// danger callout, role="alert" by variant, tabindex="-1" so the headless
// behaviour module can move focus to it after a failed island submit.
func landingSubscribeErrorSummary(msg string) render.HTML {
	return ui.Callout(ui.CalloutConfig{
		Variant:    ui.StatusDanger,
		ID:         "hl-subscribe-summary",
		Title:      "Check the address",
		Landmark:   falsePtr(),
		ExtraAttrs: html.Attrs{"tabindex": "-1"},
	}, render.Text(msg))
}

// renderLandingSubscribe renders the form (or, after a success, the
// success callout). The SSR page, the query-rendered no-script answer
// and every island response go through this one function, so the round
// trip is stateless: the answer is the re-rendered region.
func renderLandingSubscribe(r landingRoute, state landingSubscribeState) render.HTML {
	if state.Done {
		// The no-script success redirect carries no email (only the
		// island round trip still holds it in memory), so the copy has
		// a shape that does not name one it was not given.
		detail := "A confirmation would go to " + state.Email + ". This demo keeps no list: the round trip is the point."
		if state.Email == "" {
			detail = "You are on the list. This demo keeps no list: the round trip is the point."
		}
		return ui.Callout(ui.CalloutConfig{
			Variant:  ui.StatusSuccess,
			ID:       "hl-subscribe-done",
			Title:    "Subscribed",
			Landmark: falsePtr(),
		}, render.Text(detail))
	}
	extra := html.Attrs{"novalidate": ""}
	var summary render.HTML
	if state.Error != "" {
		// The hook the headless behaviour module keys on: a form inside
		// it moves focus to its [role="alert"][tabindex="-1"] summary.
		extra["data-hui-form-errors"] = ""
		summary = landingSubscribeErrorSummary(state.Error)
	}
	form := ui.Form(ui.FormConfig{
		Action:      landingSubscribePath,
		Method:      "POST",
		SubmitLabel: "Subscribe",
		Summary:     "Enter a valid address to subscribe.",
		// The island wiring rides the form itself: with the runtime on
		// the page a submit is an RPC whose 200 body is this region,
		// re-rendered, and the typed value survives in it; without the
		// runtime the same POST navigates and the handler answers 303
		// back to this page, whose query carries the outcome alone.
		ExtraAttrs: html.MergeAttrs(extra,
			interactive.Post(landingSubscribePath).
				OnSuccess(interactive.SetSignal(landingSubscribeSignal)).Attrs()),
	},
		summary,
		ui.TextField(ui.TextFieldConfig{
			Name:         "email",
			Label:        "Email address",
			ID:           "hl-subscribe-email",
			Value:        state.Email,
			Placeholder:  "you@example.com",
			AutoComplete: "email",
			Required:     true,
			Help:         "One field, server-side validation, island round trip with script, plain POST without.",
			Error:        state.Error,
		}),
		// The no-script round trip needs the theme segment server-side;
		// the island response ignores it (the scope is already on the
		// page).
		html.Input(html.InputConfig{Type: "hidden", Name: "theme", Value: r.Segment}),
	)
	return form
}

// landingSubscribeRegion is the signal-bound region the island response
// replaces. It renders the current state on first paint.
func landingSubscribeRegion(r landingRoute, state landingSubscribeState) render.HTML {
	return interactive.BindHTML(
		html.Div(html.DivConfig{ID: "hl-newsletter"}, renderLandingSubscribe(r, state)),
		landingSubscribeSignal)
}

func landingNewsletterSection(r landingRoute, state landingSubscribeState) render.HTML {
	return ui.Section(ui.SectionConfig{
		ID:          "hl-newsletter-section",
		Heading:     "A form with a real round trip",
		Description: "Server-validated. With the runtime: an island swap and focus lands on the summary. Without script: the same POST redirects back and this page re-renders the answer.",
	}, landingSubscribeRegion(r, state))
}

// serveHeadlessSubscribe answers both the island RPC (JSON body → 200
// with the re-rendered region; the errors ARE the answer) and the
// no-script native POST (urlencoded body → 303 See Other back to the
// landing route, whose query carries the answer: subscribe=invalid plus
// the typed email, or subscribe=ok). The redirect keeps the answer on a
// real site page — chrome, stylesheets and all — instead of a
// hand-rolled document. Mounted in setupServer.
func serveHeadlessSubscribe(w http.ResponseWriter, r *http.Request) {
	var email, segment string
	island := false
	switch {
	case strings.HasPrefix(r.Header.Get("Content-Type"), "application/json"):
		island = true
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		var body struct {
			Email string `json:"email"`
			Theme string `json:"theme"`
		}
		if err := handler.DecodeStrict(r.Body, &body); err != nil {
			// DecodeStrict wraps the read error, so the cap's
			// *http.MaxBytesError is visible here and answers 413.
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			// Constant on purpose: the parse error text is request
			// data and never belongs in the response.
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		email, segment = body.Email, body.Theme
	default:
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := r.ParseForm(); err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		email, segment = r.PostForm.Get("email"), r.PostForm.Get("theme")
	}

	route, ok := landingRouteFor(segment)
	if !ok {
		// The route 404s an unknown segment because a wrong theme would
		// silently lie; the handler holds the same line for its carry.
		http.Error(w, "unknown theme", http.StatusBadRequest)
		return
	}

	state := landingSubscribeState{Email: strings.TrimSpace(email)}
	outcome := landingValidateEmail(email)
	state.Error = landingSubscribeMessage(outcome)
	state.Done = outcome == landingSubscribeOK

	if island {
		render.RespondHTML(w, renderLandingSubscribe(route, state))
		return
	}
	// Post-redirect-get: the outcome lives in the landing route's query
	// and the page renders its region from it, so a refresh or a back
	// button never re-POSTs. The typed address does not travel — see
	// Load — so a no-script reader retypes an address the server
	// refused, which is the price of keeping it out of history.
	q := url.Values{"subscribe": {string(outcome)}}
	http.Redirect(w, r, landingRoutePath(route.Segment)+"?"+q.Encode(), http.StatusSeeOther)
}

// ── Fixture a: every variant and size ──────────────────────────────

func landingVariantsSection() render.HTML {
	variants := []struct {
		name    string
		variant ui.ButtonVariant
	}{
		{"Primary", ui.ButtonPrimary},
		{"Secondary", ui.ButtonSecondary},
		{"Danger", ui.ButtonDanger},
		{"Ghost", ui.ButtonGhost},
	}
	sizes := []struct {
		name string
		size ui.ButtonSize
	}{
		{"default", ui.ButtonSizeDefault},
		{"small", ui.ButtonSizeSmall},
		{"large", ui.ButtonSizeLarge},
	}
	rows := make([]render.HTML, 0, len(variants)+1)
	for _, v := range variants {
		buttons := make([]render.HTML, 0, len(sizes))
		for _, s := range sizes {
			id := ""
			if v.variant == ui.ButtonPrimary && s.size == ui.ButtonSizeDefault {
				id = "hl-variant-primary"
			}
			buttons = append(buttons, ui.Button(ui.ButtonConfig{
				Label: v.name + " · " + s.name, Variant: v.variant, Size: s.size, ID: id,
			}))
		}
		rows = append(rows, ui.Stack(ui.StackConfig{Gap: ui.GapSM},
			html.Heading(html.HeadingConfig{Level: 3}, render.Text(v.name)),
			ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM}, buttons...),
		))
	}
	rows = append(rows, ui.Stack(ui.StackConfig{Gap: ui.GapSM},
		html.Heading(html.HeadingConfig{Level: 3}, render.Text("States and links")),
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
			ui.Button(ui.ButtonConfig{Label: "Disabled", Variant: ui.ButtonPrimary, Disabled: true}),
			ui.LinkButton(ui.LinkButtonConfig{
				Label: "With an icon", Href: "/docs/theming", Variant: ui.ButtonSecondary,
				Icon: "hl-arrow",
			}),
			ui.LinkButton(ui.LinkButtonConfig{
				Label: "External ↗", Href: "https://github.com/DonaldMurillo/gofastr",
				Variant: ui.ButtonSecondary, External: true, Icon: "hl-arrow",
			}),
		),
	))
	return ui.Section(ui.SectionConfig{
		ID:          "hl-variants",
		Heading:     "Every variant, every size",
		Description: "Variant is what a button means; treatment is how the theme draws it. One disabled, one with an icon, one external link.",
	}, ui.Stack(ui.StackConfig{}, rows...))
}

// ── Fixture b: same palette, two option sets ───────────────────────

// landingOptionPanel labels one side of the comparison.
func landingOptionPanel(label, note string, body ...render.HTML) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapSM},
		html.Heading(html.HeadingConfig{Level: 3}, render.Text(label)),
		html.Paragraph(html.TextConfig{}, render.Text(note)),
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM}, body...),
	)
}

func landingOptionsSection(r landingRoute) render.HTML {
	return ui.Section(ui.SectionConfig{
		ID:          "hl-options",
		Heading:     "Same palette, two option sets",
		Description: "The right panel is a nested ui.Themed whose theme keeps this page's tokens and changes only the options. Colour stays; density, treatment and radius flip.",
	},
		ui.Grid(ui.GridConfig{Min: "18rem"},
			landingOptionPanel("This page's theme", "Density, treatment and radius as registered for the route.",
				ui.Button(ui.ButtonConfig{Label: "Primary", Variant: ui.ButtonPrimary}),
				ui.Button(ui.ButtonConfig{Label: "Danger", Variant: ui.ButtonDanger}),
			),
			// A theme wrapper paints its own background and colour and
			// nothing else; the Card inside it gives the nested scope
			// its edge and padding, so the boundary reads as a panel.
			ui.Themed(r.Twin, ui.Card(ui.CardConfig{},
				landingOptionPanel("The other option set", "Same palette, the flipped option set: the option compiler's work isolated from the tokens.",
					ui.Button(ui.ButtonConfig{Label: "Primary", Variant: ui.ButtonPrimary}),
					ui.Button(ui.ButtonConfig{Label: "Danger", Variant: ui.ButtonDanger}),
				),
			)),
		),
	)
}

// ── Fixture c: A → B → A nesting ───────────────────────────────────

// landingNestLevel is one labelled boundary with a primary button.
func landingNestLevel(label, note string) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapSM},
		html.Heading(html.HeadingConfig{Level: 3}, render.Text(label)),
		html.Paragraph(html.TextConfig{}, render.Text(note)),
		ui.Button(ui.ButtonConfig{Label: label, Variant: ui.ButtonPrimary}),
	)
}

func landingNestingSection(r landingRoute) render.HTML {
	page := r.Name
	other := "Dense"
	if r.Segment == "dense" {
		other = "Framework default"
	}
	// Each nested boundary sits in a Card: the theme wrapper paints
	// background and colour only, the Card gives the scope its edge
	// and padding so the nesting reads as boxes within boxes.
	inner := ui.Themed(r.Ref, ui.Card(ui.CardConfig{},
		landingNestLevel(page+" again", "The innermost scope redeclares the complete option set, so it wins by proximity: A → B → A ends on A."),
	))
	middle := ui.Themed(r.Other, ui.Card(ui.CardConfig{},
		ui.Stack(ui.StackConfig{Gap: ui.GapSM},
			landingNestLevel(other, "The other registered theme, nested inside the page's scope."),
			inner,
		),
	))
	return ui.Section(ui.SectionConfig{
		ID:          "hl-nesting",
		Heading:     "Nesting A → B → A",
		Description: "Three primary buttons. Each boundary declares the full option set; read the computed values and the innermost A matches the outer A.",
	},
		ui.Stack(ui.StackConfig{Gap: ui.GapSM},
			landingNestLevel(page, "The page's theme: the outer A."),
			middle,
		),
	)
}

// ── Fixture d: explicit scheme controls ────────────────────────────

func landingSchemeSection() render.HTML {
	return ui.Section(ui.SectionConfig{
		ID:          "hl-scheme",
		Heading:     "Light, dark, system",
		Description: "The pill forces a scheme independent of the OS setting; Auto follows it. Scoped dark palettes flip with the document, not the wrapper.",
	},
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
			ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeTogglePill, ID: "hl-scheme-toggle"}),
		),
	)
}

// ── Fixture e: bare headless beside styled ui ──────────────────────

func landingBareSection(ctx context.Context) render.HTML {
	return ui.Section(ui.SectionConfig{
		ID:          "hl-bare",
		Heading:     "Bare headless, styled ui",
		Description: "The left button is headless.Button with a nil Classes: structure, roles, no classes, no stylesheet. The right one is ui.Button on the same page. The banner beneath is headless.SystemBanner with its Strings resolved from the request through ui.StringsFor — English words here, the reader's locale the moment the site installs a translator.",
	},
		ui.Grid(ui.GridConfig{Min: "18rem"},
			ui.Stack(ui.StackConfig{Gap: ui.GapSM},
				html.Heading(html.HeadingConfig{Level: 3}, render.Text("headless.Button, nil Classes")),
				headless.Button(headless.ButtonProps{
					Label: "Bare headless button",
					Type:  "button",
					ID:    "hl-bare-button",
				}, nil),
			),
			ui.Stack(ui.StackConfig{Gap: ui.GapSM},
				html.Heading(html.HeadingConfig{Level: 3}, render.Text("ui.Button")),
				ui.Button(ui.ButtonConfig{Label: "Styled ui button", Variant: ui.ButtonPrimary, ID: "hl-styled-button"}),
			),
			ui.Stack(ui.StackConfig{Gap: ui.GapSM},
				html.Heading(html.HeadingConfig{Level: 3}, render.Text("headless.SystemBanner, ui.StringsFor(ctx)")),
				// The B0 seam proof: the tone word before the title and
				// the dismiss control's name are Strings fields, filled
				// from the request's locale by the bridge. With no
				// translator installed they are the English defaults,
				// which the SSR test pins on the rendered page.
				headless.SystemBanner(headless.SystemBannerProps{
					ID:      "hl-bare-banner",
					Tone:    "info",
					Title:   "Strings from the request",
					Text:    "The tone word and the dismiss name are headless.Strings fields; ui.StringsFor resolves them per request.",
					Shown:   true,
					Strings: ui.StringsFor(ctx),
				}, nil),
			),
		),
	)
}

// ── Fixture f: cold LoadAuto insertion ─────────────────────────────

func landingLateSection() render.HTML {
	button := ui.Button(ui.ButtonConfig{
		Label: "Load the late fragment",
		ID:    "hl-late-button",
		ExtraAttrs: interactive.Get(landingLatePath).
			OnSuccess(interactive.SetSignal(landingLateSignal)).Attrs(),
	})
	region := interactive.BindHTML(
		html.Div(html.DivConfig{ID: "hl-late-region"},
			html.Paragraph(html.TextConfig{}, render.Text("Nothing loaded yet. The fragment's component stylesheet is not on this page.")),
		),
		landingLateSignal,
	)
	return ui.Section(ui.SectionConfig{
		ID:          "hl-late",
		Heading:     "A cold LoadAuto insertion",
		Description: "Button is LoadAlways and proves nothing about demand loading. The fragment below arrives with a ui.Callout, whose sheet is LoadAuto and absent here: the runtime must fetch it when the marker appears.",
	},
		ui.Stack(ui.StackConfig{Gap: ui.GapSM}, button, region),
	)
}

const landingLateSignal = "hl-late"

// serveHeadlessLate answers the fixture-f fetch with a component whose
// stylesheet is LoadAuto (ui-callout) and appears nowhere else on the
// landing page's first paint, so its sheet is genuinely cold. Mounted in
// setupServer.
func serveHeadlessLate(w http.ResponseWriter, _ *http.Request) {
	render.RespondHTML(w, ui.Callout(ui.CalloutConfig{
		Variant:  ui.StatusInfo,
		ID:       "hl-late-fragment",
		Title:    "Late fragment",
		Landmark: falsePtr(),
	}, render.Text("This callout arrived after a click, and its stylesheet was fetched on arrival: the marker the runtime scans for is what loaded it, not the page.")))
}

// init registers the single icon the fixtures use. The icon registry is
// process-wide; package init keeps it ahead of any render.
func init() {
	ui.RegisterIcon("hl-arrow", `<path d="M5 12h14M13 6l6 6-6 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>`)
}
