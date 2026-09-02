package main

// screens.go renders every page from framework/ui components: no CSS
// and no hand-rolled structural markup live here. Live values (the
// current instruction, presence, media state) render BOTH states
// server-side; static/app.js toggles `hidden` and sets textContent, so
// the design system owns every pixel and the script only moves state.
//
// Each session page carries one config carrier, #assist-root, whose
// data-assist-* attributes tell app.js the session id, the role, and
// that page's WebSocket endpoint. That is the whole page-to-script
// contract: CSP-safe (no inline JS), and nothing a page doesn't
// declare reaches the script.

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// assistRoot renders the #assist-root config carrier around the page
// body. app.js refuses to boot without it.
func assistRoot(attrs map[string]string, children ...render.HTML) render.HTML {
	data := map[string]string{"assist-root": ""}
	for k, v := range attrs {
		data["assist-"+k] = v
	}
	return html.Div(html.DivConfig{ID: "assist-root", ExtraAttrs: html.DataAttrs(data)}, children...)
}

// videoTag renders a <video> element, hidden until a stream is
// attached (app.js clears the attribute when it sets srcObject). A leaf
// semantic tag, not layout: the design system has no media component,
// and none is needed — the element ships no styling of its own.
func videoTag(id string) render.HTML {
	return render.Tag("video", map[string]string{
		"id": id, "autoplay": "", "muted": "", "playsinline": "", "hidden": "",
	})
}

// pillPair renders two StatusPills where exactly one is visible: the
// one matching the state at render time, so a reload shows the truth
// before hydration; app.js flips the hidden attribute as state changes.
func pillPair(id string, off, on string, state bool) render.HTML {
	offAttrs := html.Attrs{"data-assist-live": ""}
	onAttrs := html.Attrs{"data-assist-live": ""}
	if state {
		offAttrs["hidden"] = ""
	} else {
		onAttrs["hidden"] = ""
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
		ui.StatusPill(ui.StatusPillConfig{ID: id + "-off", Dot: true, Label: off, ExtraAttrs: offAttrs}),
		ui.StatusPill(ui.StatusPillConfig{ID: id + "-on", Dot: true, Label: on, ExtraAttrs: onAttrs}),
	)
}

// LandingScreen is the public page: no role, no tools, no bridge.
type LandingScreen struct{}

func (s *LandingScreen) ScreenTitle() string { return "Remote assist" }
func (s *LandingScreen) Render() render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow:  "GoFastr example",
			Title:    "Remote assist",
			Subtitle: "An operator shares their camera with support. Support guides them with instructions an in-browser AI agent can also issue.",
		}),
		ui.Grid(ui.GridConfig{Min: "18rem"},
			ui.Card(ui.CardConfig{Heading: "For support", HeadingLevel: 2,
				Description: "Sign in, create a short-lived session, and get a one-time link for the operator."},
				ui.LinkButton(ui.LinkButtonConfig{Label: "Support sign in", Href: "/support/login"}),
			),
			ui.Card(ui.CardConfig{Heading: "For the operator", HeadingLevel: 2,
				Description: "Open the one-time link Support sent you. The link trades for a role cookie and opens your side of the session."},
				ui.StatusPill(ui.StatusPillConfig{Label: "Join links come from support", Dot: true}),
			),
			ui.Card(ui.CardConfig{Heading: "What the example shows", HeadingLevel: 2},
				ui.Stack(ui.StackConfig{Gap: ui.GapSM},
					html.Paragraph(html.TextConfig{}, render.Text("Browser WebMCP tools scoped to the support console.")),
					html.Paragraph(html.TextConfig{}, render.Text("WebRTC video that never touches the server; only signaling does.")),
					html.Paragraph(html.TextConfig{}, render.Text("One typed command behind the manual button and the AI tools.")),
				),
			),
		),
	)
}

// SupportLoginScreen mints the support role cookie. A real deployment
// replaces this with battery/auth; the example keeps the surface so
// the role boundary stays visible and testable.
type SupportLoginScreen struct{ component.ContextOnly }

func (s *SupportLoginScreen) ScreenTitle() string { return "Support sign in" }

func (s *SupportLoginScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.AuthCard(ui.AuthCardConfig{
		Title: "Support sign in",
		Body: ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			ui.Form(ui.FormConfig{
				Action:      "/support/login",
				Method:      "POST",
				SubmitLabel: "Enter the console",
				Ctx:         ctx,
			}, ui.FormField(ui.FormFieldConfig{
				Label: "Sign-in key", For: "assist-support-key", Required: true,
				Input: ui.PasswordInput(ui.PasswordInputConfig{
					Name: "key", ID: "assist-support-key", Required: true,
					Autocomplete: "current-password", Ctx: ctx,
				}),
			})),
			html.Paragraph(html.TextConfig{},
				render.Text("Demo sign-in: the key is ASSIST_SUPPORT_KEY, or the value printed in the server log at start. A real app puts its own login here.")),
		),
	})
}

// SupportHomeScreen creates sessions.
type SupportHomeScreen struct{ component.ContextOnly }

func (s *SupportHomeScreen) ScreenTitle() string { return "Assist sessions" }

func (s *SupportHomeScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    "Assist sessions",
			Subtitle: "Each session lives 10 minutes. The join link works once.",
		}),
		ui.Card(ui.CardConfig{Heading: "New session", HeadingLevel: 2},
			ui.Form(ui.FormConfig{
				Action:      "/support/sessions",
				Method:      "POST",
				SubmitLabel: "Create session",
				Ctx:         ctx,
			}),
		),
	)
}

// SupportConsoleScreen is the capability-bearing page: the WebMCP
// bridge document scope covers /support, the manual forms and the AI
// tools share one command path, and the operator's camera arrives
// peer-to-peer in #assist-remote.
type SupportConsoleScreen struct {
	component.ContextOnly
	id string
}

func (s *SupportConsoleScreen) ScreenTitle() string { return "Support console" }

func (s *SupportConsoleScreen) SetParams(m map[string]string) { s.id = m["id"] }

func (s *SupportConsoleScreen) RenderCtx(ctx context.Context) render.HTML {
	r := app.RequestFromContext(ctx)
	snap := assist.snapshotOf(s.id, roleSupport)
	joinURL := ""
	if sess := assist.lookup(s.id); sess != nil {
		joinURL = joinLink(r, sess)
	}

	return assistRoot(map[string]string{
		"session": s.id,
		"role":    string(roleSupport),
		"ws":      "/support/session/" + s.id + "/ws",
	},
		ui.Stack(ui.StackConfig{Gap: ui.GapLG},
			ui.PageHeader(ui.PageHeaderConfig{
				Eyebrow: "Session " + shortID(s.id),
				Title:   "Support console",
				Actions: ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
					pillPair("assist-pill-op", "operator away", "operator online", snap.OperatorOnline),
					pillPair("assist-pill-media", "no video", "video live", snap.MediaUp),
					pillPair("assist-pill-ack", "not acknowledged", "acknowledged", snap.Acked),
				),
			}),
			ui.Grid(ui.GridConfig{Min: "22rem"},
				ui.Card(ui.CardConfig{Heading: "Operator camera", HeadingLevel: 2,
					Description: "Peer-to-peer WebRTC. The server relayed the handshake; the video never crosses it."},
					ui.Stack(ui.StackConfig{Gap: ui.GapMD},
						videoTag("assist-remote"),
						html.Paragraph(html.TextConfig{ID: "assist-media-note"},
							render.Text("Waiting for the operator to share a camera.")),
					),
				),
				ui.Stack(ui.StackConfig{Gap: ui.GapMD},
					ui.Card(ui.CardConfig{Heading: "Instruction", HeadingLevel: 2,
						Description: "One instruction at a time. The button and the AI tools issue the same command."},
						ui.Stack(ui.StackConfig{Gap: ui.GapMD},
							instructionCard(snap.Instruction, true),
							ui.Form(ui.FormConfig{
								Action:      "/support/session/" + s.id + "/instruction",
								Method:      "POST",
								SubmitLabel: "Send instruction",
								ID:          "assist-manual-form",
								Ctx:         ctx,
							}, ui.TextField(ui.TextFieldConfig{
								Name: "instruction", Label: "Instruction",
								ID: "assist-instruction-input", Required: true,
								MaxLength:   500,
								Placeholder: "Press the green restart button",
							})),
							ui.Form(ui.FormConfig{
								Action:      "/support/session/" + s.id + "/clear",
								Method:      "POST",
								SubmitLabel: "Clear instruction",
								ID:          "assist-clear-form",
								Ctx:         ctx,
							}),
						),
					),
					ui.Card(ui.CardConfig{Heading: "Invite the operator", HeadingLevel: 2,
						Description: "One-time link: the first open trades it for the operator cookie."},
						ui.Stack(ui.StackConfig{Gap: ui.GapSM},
							ui.TextField(ui.TextFieldConfig{
								Name: "join", Label: "Join link", ID: "assist-join-link",
								Value: joinURL, ExtraAttrs: html.Attrs{"readonly": ""},
							}),
							ui.CopyButton(ui.CopyButtonConfig{Target: "assist-join-link", Ctx: ctx}),
						),
					),
					ui.Card(ui.CardConfig{Heading: "Browser tools", HeadingLevel: 2,
						Description: "inspect_session, send_instruction, clear_instruction are registered on this document only."},
						html.Paragraph(html.TextConfig{ID: "assist-bridge"},
							render.Text("Checking bridge registration…")),
					),
				),
			),
		),
	)
}

// OperatorScreen is the operator's side: camera sender, instruction
// receiver, acknowledger. No support tools exist in this document.
type OperatorScreen struct {
	component.ContextOnly
	id string
}

func (s *OperatorScreen) ScreenTitle() string { return "Assist session" }

func (s *OperatorScreen) SetParams(m map[string]string) { s.id = m["id"] }

func (s *OperatorScreen) RenderCtx(ctx context.Context) render.HTML {
	snap := assist.snapshotOf(s.id, roleOperator)
	return assistRoot(map[string]string{
		"session": s.id,
		"role":    string(roleOperator),
		"ws":      "/session/" + s.id + "/ws",
	},
		ui.Stack(ui.StackConfig{Gap: ui.GapLG},
			ui.PageHeader(ui.PageHeaderConfig{
				Eyebrow:  "Session " + shortID(s.id),
				Title:    "Your assist session",
				Subtitle: "Share your camera when you are ready. Support sees what you see and sends instructions here.",
				Actions:  pillPair("assist-pill-media", "camera off", "camera live", snap.MediaUp),
			}),
			ui.Grid(ui.GridConfig{Min: "22rem"},
				ui.Card(ui.CardConfig{Heading: "Your camera", HeadingLevel: 2,
					Description: "Video and no microphone: the stream is send-only to Support, peer-to-peer."},
					ui.Stack(ui.StackConfig{Gap: ui.GapMD},
						ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
							ui.Button(ui.ButtonConfig{
								Label: "Share camera", ID: "assist-share", Type: "button",
								ExtraAttrs: html.Attrs{"data-assist-live": ""},
							}),
						),
						videoTag("assist-local"),
					),
				),
				ui.Card(ui.CardConfig{Heading: "Instruction from support", HeadingLevel: 2},
					ui.Stack(ui.StackConfig{Gap: ui.GapMD},
						instructionCard(snap.Instruction, false),
						ui.Form(ui.FormConfig{
							Action:      "/session/" + s.id + "/ack",
							Method:      "POST",
							SubmitLabel: "Mark as shown",
							ID:          "assist-ack-form",
							Ctx:         ctx,
						}),
						pillPair("assist-pill-ack", "not acknowledged", "acknowledged", snap.Acked),
					),
				),
			),
		),
	)
}

// instructionCard renders the current instruction slot. The text
// element keeps its own id so app.js can replace its content without
// touching component internals.
func instructionCard(text string, withInvocation bool) render.HTML {
	body := []render.HTML{
		html.Paragraph(html.TextConfig{ID: "assist-instruction-text"},
			render.Text(instructionText(text))),
	}
	if withInvocation {
		body = append(body,
			html.Paragraph(html.TextConfig{ID: "assist-invocation", Class: ""},
				render.Text(invocationText(""))))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapSM}, body...)
}

func instructionText(text string) string {
	if strings.TrimSpace(text) == "" {
		return "No instruction yet."
	}
	return text
}

func invocationText(inv string) string {
	if inv == "" {
		return "Last command: manual or page button."
	}
	return "Last command: agent invocation " + shortID(inv) + "."
}

// JoinScreen is the one-time link's landing page. Rendering it spends
// nothing: link previewers and mail scanners fetch it and see a button.
// The button's same-origin POST performs the exchange (handleJoin).
type JoinScreen struct {
	component.ContextOnly
	token string
}

func (s *JoinScreen) ScreenTitle() string { return "Join assist session" }

func (s *JoinScreen) SetParams(m map[string]string) { s.token = m["token"] }

func (s *JoinScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.AuthCard(ui.AuthCardConfig{
		Title: "Join the assist session",
		Body: ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			html.Paragraph(html.TextConfig{},
				render.Text("Support sent you this link. Joining opens your side of the session, where you can share your camera and read instructions. The link works once.")),
			ui.Form(ui.FormConfig{
				Action:      "/join/" + s.token,
				Method:      "POST",
				SubmitLabel: "Join the session",
				Ctx:         ctx,
			}),
		),
	})
}

// SessionGoneScreen is the one recovery screen for every dead path: an
// expired session, an unknown id, a spent join link, a missing role
// cookie. Callers never learn which one they hit.
type SessionGoneScreen struct{}

func (s *SessionGoneScreen) ScreenTitle() string { return "Session ended" }

func (s *SessionGoneScreen) Render() render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    "This assist session has ended",
			Subtitle: "Ask Support for a new link, or start another session from the console.",
		}),
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
			ui.LinkButton(ui.LinkButtonConfig{Label: "Back to the overview", Href: "/"}),
		),
	)
}

// joinLink builds the absolute one-time join URL for the request's
// scheme and host.
func joinLink(r *http.Request, s *assistSession) string {
	if r == nil {
		return "/join/" + s.joinToken
	}
	scheme := "https"
	if r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s/join/%s", scheme, r.Host, s.joinToken)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
