package uihost

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// policyEmbedFixture builds an embed fixture whose /reports screen carries the
// given policy, so a granted subject who does not satisfy it still hits the
// policy on the content render. It stands in for a screen gated with
// decide.RequireRole("analyst") over a subject who is not an analyst.
func policyEmbedFixture(t *testing.T, policy app.Policy) embedFixture {
	t.Helper()
	application := app.NewApp("Embed Policy Test")
	application.SetDefaultLayout(app.NewLayout("main"))
	scr := app.NewScreen("/reports", &embedSubjectComp{}).WithTitle("Reports")
	if policy != nil {
		scr = scr.WithPolicy(policy)
	}
	application.RegisterScreen(scr, nil)

	cfg := fembed.Config{
		Surfaces: []fembed.Surface{{
			Name:    "reports",
			Path:    "/reports",
			Origins: []string{embedTestOrigin},
			Scopes:  []string{"read"},
		}},
		BurnStore: fembed.NewMemoryBurnStore(),
		Resolve: func(_ context.Context, subject string) (any, error) {
			return subject, nil
		},
	}
	eh, err := fembed.New(cfg)
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
	return embedFixture{host: New(application, WithEmbed(eh)), embed: eh, surface: "reports"}
}

// A screen policy that blocks or redirects the granted viewer must stop the
// embed content handler from rendering that viewer's content into the frame.
//
// This is the one place an app's own authorization decision meets the embed
// path: the surface is granted and the subject resolves, so identity and scope
// checks all pass — and then the screen's policy says "not this viewer". If the
// handler ignores that decision and falls through to res.HTML, content the
// policy already denied is written into the frame with a 200. DecisionRedirect
// is how "complete onboarding" / "subscription lapsed" gates are expressed, and
// following it from inside a frame on a stranger's site would render a
// different screen than the surface declares.
func TestEmbedContentHonorsScreenPolicy(t *testing.T) {
	cases := []struct {
		name     string
		decision app.Decision
		// wantStatus is what the handler must answer when it honours the
		// decision. A fall-through render would answer 200.
		wantStatus int
	}{
		{
			name:       "block",
			decision:   app.Decision{Kind: app.DecisionBlock, Status: http.StatusForbidden},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "redirect",
			decision:   app.Decision{Kind: app.DecisionRedirect, URL: "/onboarding"},
			wantStatus: http.StatusForbidden,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := app.PolicyFunc(func(_ context.Context) app.Decision { return tc.decision })
			f := policyEmbedFixture(t, policy)
			grant := f.grantFor(t, "reports")

			rec := f.do(t, http.MethodGet, "/__gofastr/embed/reports/content", "", func(req *http.Request) {
				req.Header.Set(embedGrantHeader, grant)
			})

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d — the handler rendered despite the screen policy", rec.Code, tc.wantStatus)
			}
			if strings.Contains(rec.Body.String(), "viewer:user-7") {
				t.Fatalf("the policy-denied content reached the frame:\n%s", rec.Body.String())
			}
		})
	}
}
