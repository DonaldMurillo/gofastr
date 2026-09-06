package main

// screens.go renders every page from framework/ui components: no CSS
// and no hand-rolled structural markup live here. Live values render
// server-side in BOTH states (a hidden local video, a muted/live pill
// pair per tile); static/app.js toggles `hidden`, sets textContent and
// srcObject, and clones the two <template> elements, so the design
// system owns every pixel and the script only moves state.
//
// The room page carries one config carrier, #call-root, whose
// data-call-* attributes tell app.js the room name and the signaler
// path. That is the whole page-to-script contract: CSP-safe (no
// inline JS), and nothing a page doesn't declare reaches the script.

import (
	"context"

	"github.com/DonaldMurillo/gofastr/battery/rtc"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// callRoot renders the #call-root config carrier around the room page
// body. app.js refuses to boot without it.
func callRoot(attrs map[string]string, children ...render.HTML) render.HTML {
	data := map[string]string{"call-root": ""}
	for k, v := range attrs {
		data["call-"+k] = v
	}
	return html.Div(html.DivConfig{ID: "call-root", ExtraAttrs: html.DataAttrs(data)}, children...)
}

// videoTag renders a <video> element through the html primitive,
// hidden until a stream is attached (app.js clears the attribute when
// it sets srcObject). A leaf semantic tag, not layout: the design
// system has no media component, and none is needed — the element
// ships no styling of its own.
func videoTag(id string, attrs html.Attrs) render.HTML {
	all := html.Attrs{"autoplay": "", "playsinline": "", "hidden": ""}
	for k, v := range attrs {
		all[k] = v
	}
	return html.Video(html.VideoConfig{ID: id, ExtraAttrs: all})
}

// pillPair renders two StatusPills where exactly one is visible: the
// one matching the state at render time, so a reload shows the truth
// before hydration; app.js flips the hidden attribute as state
// changes. The pair inside the remote-tile template carries data
// attributes instead of ids: every clone would duplicate an id.
func pillPair(idBase string, off, on string, state bool) render.HTML {
	offAttrs := html.Attrs{"data-call-live": ""}
	onAttrs := html.Attrs{"data-call-live": ""}
	if state {
		offAttrs["hidden"] = ""
	} else {
		onAttrs["hidden"] = ""
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
		ui.StatusPill(ui.StatusPillConfig{ID: idBase + "-off", Dot: true, Label: off, ExtraAttrs: offAttrs}),
		ui.StatusPill(ui.StatusPillConfig{ID: idBase + "-on", Dot: true, Label: on, ExtraAttrs: onAttrs}),
	)
}

// tilePillPair is pillPair for the tile template, keyed by data
// attribute instead of id (clones must not duplicate ids).
func tilePillPair(key string, off, on string, state bool) render.HTML {
	offAttrs := html.Attrs{"data-call-pill": key + "-off", "data-call-live": ""}
	onAttrs := html.Attrs{"data-call-pill": key + "-on", "data-call-live": ""}
	if state {
		offAttrs["hidden"] = ""
	} else {
		onAttrs["hidden"] = ""
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
		ui.StatusPill(ui.StatusPillConfig{Dot: true, Label: off, ExtraAttrs: offAttrs}),
		ui.StatusPill(ui.StatusPillConfig{Dot: true, Label: on, ExtraAttrs: onAttrs}),
	)
}

// LobbyScreen is the public page: a name, a room, one form.
type LobbyScreen struct{ component.ContextOnly }

func (s *LobbyScreen) ScreenTitle() string { return "Join a call" }

func (s *LobbyScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow:  "GoFastr example",
			Title:    "Join a call",
			Subtitle: "Pick a name and a room. Anyone who enters the same room name joins the same call; no account, no invite.",
		}),
		ui.Form(ui.FormConfig{
			Action:      "/join",
			Method:      "POST",
			SubmitLabel: "Join room",
			Ctx:         ctx,
		},
			ui.TextField(ui.TextFieldConfig{
				Name: "name", Label: "Your name", ID: "call-name-input",
				Required: true, MaxLength: 40, Placeholder: "Ann",
			}),
			ui.TextField(ui.TextFieldConfig{
				Name: "room", Label: "Room", ID: "call-room-input",
				Required: true, MaxLength: 40, Placeholder: "standup",
				ExtraAttrs: html.Attrs{"pattern": "[a-z0-9-]+",
					"title": "lowercase letters, digits, and hyphens"},
			}),
		),
		html.Paragraph(html.TextConfig{},
			render.Text("The room is whatever you and the people you call agree on: it is a name, not a reservation. The server relays the WebRTC handshake and never sees or hears the call.")),
	)
}

// RoomScreen is the call: local tile, one remote-tile template, chat.
// roomGuard has already verified the cookie and the room name.
type RoomScreen struct {
	component.ContextOnly
	room string
}

func (s *RoomScreen) ScreenTitle() string { return "Call room" }

func (s *RoomScreen) SetParams(m map[string]string) { s.room = m["room"] }

func (s *RoomScreen) RenderCtx(ctx context.Context) render.HTML {
	name := ""
	if r := app.RequestFromContext(ctx); r != nil {
		if c, err := r.Cookie(callNameCookie); err == nil {
			name = c.Value
		}
	}

	return callRoot(map[string]string{
		"room": s.room,
		"ws":   rtc.DefaultPath,
	},
		ui.Stack(ui.StackConfig{Gap: ui.GapLG},
			ui.PageHeader(ui.PageHeaderConfig{
				Eyebrow:  "Room " + s.room,
				Title:    "Video call",
				Subtitle: "Share your camera to talk. Media flows peer to peer; the server only relays the handshake.",
			}),
			ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
				// Share and the chat controls start disabled: app.js
				// enables them once the room is hydrated, so nothing can
				// acquire a camera with no room to publish to.
				ui.Button(ui.ButtonConfig{Label: "Share camera", ID: "call-share", Type: "button",
					ExtraAttrs: html.Attrs{"disabled": ""}}),
				// Mute needs an audio track to toggle, so it waits for
				// the share; app.js enables it.
				ui.Button(ui.ButtonConfig{Label: "Mute", ID: "call-mute", Type: "button",
					ExtraAttrs: html.Attrs{"disabled": ""}}),
				// Leave is a plain link. app.js is a document-scoped
				// script, so the runtime loads the lobby as a real
				// document (register_script.go: a navigation that
				// crosses the scope edge never partial-swaps), and the
				// document teardown closes the socket and the tracks.
				// No script, no location.href.
				ui.LinkButton(ui.LinkButtonConfig{Label: "Leave", Href: "/", ID: "call-leave", Variant: ui.ButtonSecondary}),
			),
			// The one place a refusal or a failed camera is said out
			// loud: app.js fills the text and flips hidden. Rendered
			// empty and hidden so the page carries no stale message.
			ui.Callout(ui.CalloutConfig{ID: "call-notice", Variant: ui.StatusWarning, ExtraAttrs: html.Attrs{"hidden": ""}},
				html.Span(html.TextConfig{ID: "call-notice-text"}),
			),
			// The grid holds the server-rendered local tile plus one
			// cloned tile per remote peer; app.js appends and removes.
			ui.Grid(ui.GridConfig{Min: "18rem", ID: "call-grid"}, localTile(name)),
			chatSection(ctx, s.room),
			// The remote tile every peer gets: cloned, never built by
			// hand. The video is not muted (you want to hear them);
			// the browser's autoplay policy is satisfied by the user
			// gestures that led here.
			html.Template(html.TemplateConfig{ID: "call-tile"},
				ui.Card(ui.CardConfig{ExtraAttrs: html.Attrs{"data-call-tile": ""},
					Header: html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"data-call-name": ""}},
						render.Text("?")),
				},
					ui.Stack(ui.StackConfig{Gap: ui.GapMD},
						videoTag("", html.Attrs{"data-call-video": ""}),
						tilePillPair("muted", "audio live", "muted", false),
					),
				),
			),
			// One chat line: cloned per message, filled by name and
			// text, so a message row is a server-rendered shape too.
			html.Template(html.TemplateConfig{ID: "call-chat-line"},
				html.ListItem(html.ListItemConfig{},
					html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"data-call-chat-name": ""}}, render.Text("?")),
					render.Text(": "),
					html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"data-call-chat-text": ""}}, render.Text("?")),
				),
			),
		),
	)
}

// localTile is your own video: muted (yours, or you would hear
// yourself), hidden until you share.
func localTile(name string) render.HTML {
	return ui.Card(ui.CardConfig{Heading: "You", HeadingLevel: 2,
		Description: "Your camera and microphone, shared peer to peer with everyone in the room."},
		ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			videoTag("call-local", html.Attrs{"muted": ""}),
			html.Paragraph(html.TextConfig{ID: "call-local-name"}, render.Text(name)),
		),
	)
}

// chatSection renders the message list and the send form. Chat has
// no server endpoint: it travels peer to peer over a negotiated data
// channel, and app.js intercepts the submit. The controls render
// disabled so nothing can submit natively before app.js owns the form
// (a native submit would put the message in the URL and the access
// log); the form is POST so even a stray submit never serializes into a
// query string.
func chatSection(ctx context.Context, room string) render.HTML {
	return ui.Section(ui.SectionConfig{Heading: "Chat", Ctx: ctx,
		Description: "Messages travel on a negotiated data channel, peer to peer. The server relays neither chat nor media."},
		html.UnorderedList(html.ListConfig{ID: "call-chat"}),
		ui.Form(ui.FormConfig{
			Action:     "/room/" + room, // required by ui.Form; the room route takes no POST, so a stray submit is a 405, never a query string
			Method:     "POST",
			ID:         "call-chat-form",
			HideSubmit: true,
			Ctx:        ctx,
		},
			ui.TextField(ui.TextFieldConfig{
				Name: "message", Label: "Message", ID: "call-chat-input",
				Required: true, MaxLength: 500, Placeholder: "Hello", Disabled: true,
			}),
			ui.Button(ui.ButtonConfig{Label: "Send", ID: "call-chat-send", Type: "submit",
				ExtraAttrs: html.Attrs{"disabled": ""}}),
		),
	)
}
