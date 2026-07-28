// Package main demonstrates embeddable surfaces: a GoFastr app handing out a
// piece of itself to a website it does not control.
//
// Two servers run in one process, deliberately on different ports, because
// everything interesting about this feature only exists across an origin
// boundary — the session cookie that is never sent, the CSP frame-ancestors
// allowlist, the postMessage handshake that carries the nonce.
//
//	:8087  the app          — owns the data, mints the nonce, serves the frame
//	:8088  "acme.example"   — a customer's site, which just pastes a <script>
//
// Run it:
//
//	go run ./examples/embed-demo
//
// Then open http://localhost:8088. The reports panel on that page is served by
// the app on :8087, rendered as a specific user, themed with the customer's
// brand colour.
//
// Read framework/docs/content/embed.md for the design; this file is the
// smallest wiring that exercises it end to end.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

const (
	appAddr      = ":8087"
	customerAddr = ":8088"
	appOrigin    = "http://localhost" + appAddr
	// The customer's origin, exactly as it must appear in the allowlist.
	// Exact origins only — http://localhost:8088 does not cover
	// http://127.0.0.1:8088, and that is the point.
	customerOrigin = "http://localhost" + customerAddr
)

// reportsScreen is the embeddable surface: a small dashboard built entirely
// from design-system components, so it inherits the app's theme (or the
// customer's brand override) with no CSS of its own.
type reportsScreen struct{}

func (s *reportsScreen) ScreenTitle() string { return "Reports" }

func (s *reportsScreen) Render() render.HTML { return s.RenderCtx(context.Background()) }

func (s *reportsScreen) RenderCtx(ctx context.Context) render.HTML {
	viewer := "a guest"
	if u, ok := handler.GetUser(ctx); ok {
		if s, ok := u.(string); ok {
			viewer = s
		}
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    "This week",
			Subtitle: "Signed in as " + viewer,
		}),
		ui.Grid(ui.GridConfig{Min: "12rem", Gap: ui.GapMD},
			ui.StatCard(ui.StatCardConfig{Label: "Orders", Value: "1,284", Trend: "+8% vs. last week", Direction: ui.TrendUp}),
			ui.StatCard(ui.StatCardConfig{Label: "Revenue", Value: "$48,120", Trend: "+3% vs. last week", Direction: ui.TrendUp}),
			ui.StatCard(ui.StatCardConfig{Label: "Refunds", Value: "17", Trend: "-2% vs. last week", Direction: ui.TrendDown}),
		),
		ui.Card(ui.CardConfig{
			Heading:     "Top channel",
			Description: "Organic search, 41% of orders",
			// A primary-variant control, so the customer's brand token is
			// visible in the render rather than merely configured.
			Footer: ui.Button(ui.ButtonConfig{Label: "Open full report", Variant: ui.ButtonPrimary}),
		}),
	)
}

func main() {
	secret := os.Getenv("GOFASTR_SECRET")
	if secret == "" {
		// Embeds require a real secret — a per-boot key would invalidate every
		// outstanding nonce on restart and would never verify on a second
		// replica. A fixed literal is fine HERE and nowhere else: this file is
		// a demo that runs on one machine for one session.
		secret = "embed-demo-secret-not-for-production" // not-a-secret: demo fixture
	}

	application := app.NewApp("Embed demo")
	// The embed layout is chrome-less: no site header, no nav, no footer. The
	// customer's page already has all of those.
	reports := app.NewScreen("/reports", &reportsScreen{})
	application.RegisterScreen(reports, app.EmbedLayout())

	embeds, err := fembed.New(fembed.Config{
		Surfaces: []fembed.Surface{{
			Name:    "reports",
			Screen:  reports,
			Origins: []string{customerOrigin},
			// Declare what an embed of this surface may do. A grant carries the
			// subject's FULL authority unless something narrows it, so a surface
			// that declares no scopes gives RequireScope nothing to gate on.
			Scopes: []string{"reports:read"},
			// And where it may go. A grant reaches this surface's own Path
			// subtree and /__gofastr/* by default; anything else — the API route
			// a form posts to, for instance — is listed here or answers 403.
			// This demo's surface only renders, so it needs nothing extra.
			Reach: nil,
			Theme: fembed.ThemeConfig{AllowTokens: []string{"color-primary"}},
		}},
		// One process, so the in-memory store is correct here. Anything running
		// more than one replica needs NewSQLBurnStore, or the same nonce is
		// spendable once per replica.
		BurnStore: fembed.NewMemoryBurnStore(),
		Resolve: func(_ context.Context, subject string) (any, error) {
			// A real app looks the subject up in its user store. The demo has
			// one user, so the id is the identity.
			return subject, nil
		},
	})
	if err != nil {
		log.Fatalf("embed.New: %v", err)
	}
	// framework.App.Mount does this from the app secret. This demo runs the
	// host standalone, so it derives the keys itself.
	embeds.SetKeys(demoKey(secret, "nonce"), demoKey(secret, "grant"))

	site := uihost.New(application, uihost.WithEmbed(embeds))

	// The embed ROUTES verify the grant themselves, so first paint works
	// without this. Everything the surface does afterwards does not: an island
	// RPC, a form post, a poll all target ordinary app routes that know nothing
	// about embeds, and without this middleware they run anonymously — the
	// panel paints as its viewer and then acts as nobody.
	//
	// It goes OUTERMOST, before any authentication middleware, because it
	// discards the ambient credentials (Cookie, Authorization, X-API-Key) so
	// nothing can compete with the grant it just verified.
	//
	// A real app would also gate what an embed may reach:
	//
	//	reports := fwApp.Group("/reports")
	//	reports.Use(embeds.RequireScope("reports:read"))
	//
	// The grant is delegated authority sitting in someone else's page — it
	// should reach the surface's own routes and nothing more.
	var appHandler http.Handler = embeds.Middleware()(site)

	go func() {
		fmt.Printf("app        → %s\n", appOrigin)
		if err := http.ListenAndServe(appAddr, appHandler); err != nil {
			log.Fatalf("app server: %v", err)
		}
	}()

	fmt.Printf("customer   → %s   ← open this one\n", customerOrigin)
	if err := http.ListenAndServe(customerAddr, customerSite(embeds)); err != nil {
		log.Fatalf("customer server: %v", err)
	}
}

// customerSite stands in for a website the app does not control. Its only
// GoFastr dependency is the <script> tag; everything else is its own HTML.
//
// The nonce is minted per page load, for the viewer this page is being rendered
// for. That is the whole discipline: a nonce baked into a template would be
// spent by the first visitor and every visitor after them would arrive as the
// same identity.
func customerSite(embeds *fembed.Host) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		nonce, err := embeds.MintNonce(r.Context(), "reports", "avery@acme.example", customerOrigin, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// The customer's brand colour. Not a secret — it is a colour — so it
		// rides in the frame URL, which lets the frame link the right
		// stylesheet in its first response instead of swapping it after paint.
		brand, _ := json.Marshal(map[string]string{"color-primary": "#0f766e"})
		theme := base64.RawURLEncoding.EncodeToString(brand)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, customerPage, appOrigin, nonce, theme)
	})
	return mux
}

// customerPage is hand-written HTML on purpose: it is what a customer's page
// looks like, not a GoFastr surface. The design-system rules that govern this
// repo's own UI stop at the frame boundary.
const customerPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Acme — Dashboard</title>
<style>
  body { font: 16px/1.5 system-ui, sans-serif; margin: 0; background: #f8fafc; color: #0f172a; }
  header { padding: 24px 32px; background: #0f766e; color: #fff; }
  main { max-width: 880px; margin: 32px auto; padding: 0 24px; }
  h1 { margin: 0; font-size: 20px; }
  h2 { font-size: 15px; text-transform: uppercase; letter-spacing: .06em; color: #64748b; }
  .panel { background: #fff; border: 1px solid #e2e8f0; border-radius: 12px; overflow: hidden; }
</style>
</head>
<body>
<header><h1>Acme Supply Co.</h1></header>
<main>
  <p>Everything on this page is Acme's, except the panel below — that is served
     by a GoFastr app on a different origin and rendered for this viewer.</p>
  <h2>Reports</h2>
  <div class="panel" id="reports"></div>
</main>
<script src="%s/__gofastr/embed.js"
        data-surface="reports"
        data-token="%s"
        data-theme="%s"
        data-target="#reports"></script>
</body>
</html>
`

// demoKey derives a per-purpose key without the framework's HKDF helper, which
// is unexported. Real apps set WithSecret / GOFASTR_SECRET and
// framework.App.Mount derives both keys properly.
func demoKey(secret, purpose string) []byte {
	sum := sha256.Sum256([]byte(purpose + "\x00" + secret))
	return sum[:]
}
