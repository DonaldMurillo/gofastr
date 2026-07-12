package main

// ── Presence demo (additive — /examples/presence) ───────────────────
//
// A self-contained live-presence roster. It composes the v0.19
// ui.AvatarGroup + AvatarConfig.Status (the presence dot) with the
// island manager's presence roster API and the SSE presence topic.
//
// How it works end-to-end:
//   - The page is linked as /examples/presence?presence=presence-demo.
//     handlePage threads the ?presence= value into the SSE <meta> tag,
//     so the client's EventSource opens /__gofastr/sse?session=X&presence=presence-demo.
//   - handleSSE reads the SERVER-DERIVED identity (from the auth ctx
//     user; anonymous → session pseudo-identity) and joins the connection
//     onto the "presence-demo" topic.
//   - The roster region below is a data-island slot. On any join/leave
//     the manager fires OnPresenceChange (wired in setupServer), which
//     re-renders the AvatarGroup and pushes it to every session on the
//     topic — so every open tab updates live.
//
// To see two viewers: open /examples/presence?presence=presence-demo in
// TWO browsers (or one normal + one private window — they get distinct
// sessions). Each appears in the other's roster; close one and it drops.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const (
	// presenceDemoTopic is the opaque presence topic this page joins. Any
	// SSE connection carrying ?presence=presence-demo is a roster member.
	presenceDemoTopic = "presence-demo"
	// presenceRosterIslandID is the data-island slot the runtime swaps on
	// a roster push. Matches the island id used by the OnPresenceChange
	// callback in setupServer.
	presenceRosterIslandID = "presence-roster-" + presenceDemoTopic
)

// renderPresenceRoster renders the live roster as an AvatarGroup with
// online status dots. Shared by the SSR path and the live-push callback so
// the initial paint and every update produce identical markup.
func renderPresenceRoster(members []island.PresenceMember) render.HTML {
	if len(members) == 0 {
		return html.Paragraph(html.TextConfig{Class: "presence-empty"},
			render.Text("No one is here yet — open this page in a second browser to see a viewer appear live."))
	}
	avatars := make([]ui.AvatarConfig, len(members))
	for i, m := range members {
		avatars[i] = ui.AvatarConfig{
			Name:   m.DisplayName,
			Status: ui.AvatarOnline,
		}
	}
	return ui.AvatarGroup(ui.AvatarGroupConfig{Avatars: avatars})
}

// PresenceScreen is the /examples/presence page — a live avatar roster of
// who's currently viewing it.
type PresenceScreen struct{}

func (s *PresenceScreen) ScreenTitle() string { return "Live Presence" }
func (s *PresenceScreen) ScreenDescription() string {
	return "Live viewer roster via SSE presence topics"
}
func (s *PresenceScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *PresenceScreen) Render() render.HTML {
	var members []island.PresenceMember
	if siteIslands != nil {
		members = siteIslands.PresenceRoster(presenceDemoTopic)
	}

	return html.Main(html.MainConfig{Class: "presence-demo-page"},
		container(
			ui.PageHeader(ui.PageHeaderConfig{
				Eyebrow: "Example · Live presence",
				Title:   "Who's viewing this page, right now",
				Subtitle: "A live roster built on the SSE presence topic. Each open connection joins the " +
					"\"presence-demo\" topic; the server pushes an updated avatar stack to every viewer when " +
					"someone joins or leaves.",
			}),
			// The live roster island. On a roster change the runtime swaps this
			// slot's innerHTML with a fresh AvatarGroup (see OnPresenceChange in
			// setupServer). role=status so screen readers announce changes.
			html.Div(html.DivConfig{
				Class:      "presence-roster-slot",
				ExtraAttrs: html.Attrs{"data-island": presenceRosterIslandID},
				Role:       "status",
				AriaLabel:  "Currently viewing this page",
			}, renderPresenceRoster(members)),
			html.Div(html.DivConfig{Class: "presence-notes"},
				html.Heading(html.HeadingConfig{Level: 2}, render.Text("Try it")),
				html.UnorderedList(html.ListConfig{},
					html.ListItem(html.ListItemConfig{}, render.Text("Open this URL in a second browser (or a private window) — a second avatar appears within a second.")),
					html.ListItem(html.ListItemConfig{}, render.Text("Close the second tab — its avatar drops from the roster.")),
					html.ListItem(html.ListItemConfig{}, render.Text("Two tabs of the SAME browser share one session, so they show as one viewer (correct — it's the same person).")),
				),
				html.Heading(html.HeadingConfig{Level: 2}, render.Text("How identity works")),
				html.Paragraph(html.TextConfig{},
					render.Text("The name under each avatar is SERVER-DERIVED. On an authenticated app it comes from the request-context user (battery/auth); this demo site has no auth, so each browser gets a stable pseudo-identity synthesized from its session id. A client can name a topic but can never claim another user's identity.")),
				html.Paragraph(html.TextConfig{},
					render.Text("Single-replica: the roster reflects only connections on THIS server. Cross-replica roster aggregation is future work.")),
			),
		),
	)
}
