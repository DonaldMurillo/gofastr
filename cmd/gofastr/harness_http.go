//check-csp:ignore-file
// The harness web client is a dev-only operator surface, not a
// production browser app, so the framework's strict-CSP contract
// does not apply. The renderTUIShell helper inlines a tiny
// bootstrap <script> + <style> bundle so the harness runs without
// a build step or an extra HTTP round-trip; that's the explicit
// trade-off, hence the directive.
package main

// HTTP listener boot for `gofastr harness` (the -listen / -web flags).
// Mounts the REST + WS handlers on the same http.Server so external
// clients (browser, curl, the bundled web client) can attach to the
// same harness instance the TUI is already driving.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	xharness "github.com/DonaldMurillo/gofastr/framework/harness"
	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/rest"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/ws"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// startHTTPListener binds a TCP listener at bindAddr, mounts the REST
// API at /v1/* and the WS endpoint at /v1/ws, and returns the public
// URL plus a shutdown closure. Returns the actual bound address even
// when the caller asked for an ephemeral port (host:0).
//
// Auth: HMAC tokens are signed with a per-process secret derived from
// the machine key (or a fresh random secret on first boot). The token
// is printed to stderr once so the user can paste it into curl /
// browser; subsequent calls within the same process accept any token
// the same encoder produced.
func startHTTPListener(h *xharness.Harness, sess ids.SessionID, bindAddr string) (url string, token string, shutdown func(), err error) {
	secret := deriveListenerSecret()
	enc := auth.NewEncoder(secret)
	revs := auth.NewRevocationList()

	// Make sure the catalog exposes the running session so REST
	// callers can query it via /v1/sessions/<id>.
	h.Catalog.RegisterEngine(h.Mux.EngineFor(sess))

	restSrv := &rest.Server{
		Mux:          h.Mux,
		Catalog:      h.Catalog,
		Encoder:      enc,
		Revocations:  revs,
		Features:     []string{"rest", "ws"},
		SessionStore: h.Sessions, // enables ?past=true sidebar
	}
	wsHandler := &ws.Handler{
		Mux:         h.Mux,
		Encoder:     enc,
		Revocations: revs,
	}

	// Token is generated before mux so we can hand it to the SSR
	// chat page. The page embeds it in a meta tag so the inline JS
	// can use it for fetch + EventSource without round-tripping
	// through a separate /auth endpoint.
	token, _ = enc.Encode(auth.Claims{
		Ver:           auth.VerCurrent,
		JTI:           ids.NewJTI(),
		Sessions:      []ids.SessionID{sess},
		IdentityClass: control.IdentityHuman,
		ExpiresAt:     time.Now().Add(24 * time.Hour).Unix(),
	})

	mux := http.NewServeMux()
	mux.Handle("/v1/ws", wsHandler)
	mux.Handle("/v1/", restSrv.Handler())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		chatPage(w, r, sess, token)
	})

	ln, lerr := net.Listen("tcp", bindAddr)
	if lerr != nil {
		return "", "", nil, fmt.Errorf("listen %s: %w", bindAddr, lerr)
	}
	addr := ln.Addr().String()
	url = "http://" + addr
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()

	shutdown = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return url, token, shutdown, nil
}

// deriveListenerSecret returns 32 bytes derived from the machine key
// if set (via GOFASTR_HARNESS_MACHINE_KEY), else a fresh random secret
// so each process restart invalidates outstanding tokens.
func deriveListenerSecret() []byte {
	if mk, err := machineKeyFromEnv(); err == nil && len(mk) > 0 {
		h := sha256.Sum256(append([]byte("harness-http:"), mk...))
		return h[:]
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return b
}

// shortSessionLabel produces a status-bar-friendly truncation of the
// session id. Robust against shorter-than-expected inputs (tests +
// future formats); never panics.
func shortSessionLabel(s string) string {
	if len(s) <= 14 {
		return s
	}
	return s[:14] + "…"
}

// chatPage is the SSR chat UI served at /. Renders an empty
// scrollback + input box; client-side JS opens an SSE stream and
// POSTs input so the browser experience mirrors the TUI.
func chatPage(w http.ResponseWriter, r *http.Request, sess ids.SessionID, token string) {
	if r.URL.Path != "/" {
		// The /endpoints sub-page is a fallback developer reference.
		if r.URL.Path == "/endpoints" {
			render.RespondHTML(w, renderLanding(r.Host))
			return
		}
		http.NotFound(w, r)
		return
	}
	render.RespondHTML(w, renderChat(r.Host, string(sess), token))
}

// renderChat builds the chat document. The token + session land in
// meta tags rather than a query string so they aren't logged in
// referrers or browser history.
//
// Layout (web is not the TUI — it has affordances the TUI can't have):
//
//	┌──────────────────────────── status bar ──────────────────┐
//	│ harness · session · model · $cost                api docs │
//	├──────────────┬────────────────────────────────────────────┤
//	│              │                                            │
//	│  Sessions    │  message bubbles                           │
//	│  ● this      │  ● collapsible tool cards                  │
//	│    other     │  ● live markdown                           │
//	│              │  ● spinner during thinking                 │
//	│  /help       │  ● copy buttons on hover                   │
//	│              │                                            │
//	├──────────────┴────────────────────────────────────────────┤
//	│ permission modal overlay (when triggered)                 │
//	├───────────────────────────────────────────────────────────┤
//	│ > input box                                               │
//	└───────────────────────────────────────────────────────────┘
func renderChat(host, sessionID, token string) render.HTML {
	statusBar := html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "status-bar"}},
		render.Tag("span", map[string]string{"class": "status-name"},
			render.Text("gofastr harness")),
		render.Tag("span", map[string]string{"class": "status-sep"}, render.Text("·")),
		render.Tag("span", map[string]string{"id": "status-session"},
			render.Text("session "+shortSessionLabel(sessionID))),
		render.Tag("span", map[string]string{"class": "status-sep"}, render.Text("·")),
		render.Tag("span", map[string]string{"id": "status-model"}, render.Text("…")),
		render.Tag("span", map[string]string{"class": "status-sep"}, render.Text("·")),
		render.Tag("span", map[string]string{"id": "status-cost"}, render.Text("$0.0000")),
		render.Tag("span", map[string]string{"class": "status-spacer"}, render.Text("")),
		render.Tag("a", map[string]string{"href": "/endpoints", "class": "status-link"},
			render.Text("api docs ↗")),
	)
	sidebar := html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "sidebar"}},
		render.Tag("h3", nil, render.Text("Sessions")),
		html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "session-list"}},
			render.Tag("div", map[string]string{"class": "sidebar-empty"}, render.Text("loading…")),
		),
		render.Tag("h3", nil, render.Text("Quick commands")),
		html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "quick-cmds"}},
			render.Tag("button", map[string]string{"class": "qc", "data-cmd": "/help"}, render.Text("/help")),
			render.Tag("button", map[string]string{"class": "qc", "data-cmd": "/clear"}, render.Text("/clear")),
			render.Tag("button", map[string]string{"class": "qc", "data-cmd": "/cost"}, render.Text("/cost")),
			render.Tag("button", map[string]string{"class": "qc", "data-cmd": "/profile"}, render.Text("/profile")),
		),
		render.Tag("h3", nil, render.Text("Plan")),
		html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "task-panel"}},
			render.Tag("div", map[string]string{"class": "sidebar-empty"}, render.Text("no plan yet")),
		),
	)
	main := html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "main"}},
		html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "scrollback"}},
			html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"class": "welcome"}},
				render.Tag("strong", nil, render.Text("gofastr harness")),
				render.Tag("br", nil),
				render.Text("Type a message and press Enter. This page is one of multiple clients attached to the same engine — open the TUI in another window and you'll see messages and tool calls sync both ways."),
			),
		),
		render.Tag("form", map[string]string{"id": "input-form"},
			render.Tag("div", map[string]string{"class": "input-box"},
				render.Tag("span", map[string]string{"class": "prompt"}, render.Text(">")),
				render.VoidTag("input", map[string]string{
					"id":           "input",
					"type":         "text",
					"autocomplete": "off",
					"spellcheck":   "false",
					"placeholder":  "ask anything…",
					"autofocus":    "autofocus",
				}),
				render.Tag("button", map[string]string{"id": "send", "type": "submit"},
					render.Text("send")),
			),
		),
	)
	permissionModal := html.Div(html.DivConfig{ExtraAttrs: html.Attrs{
		"id": "permission-modal", "class": "modal hidden", "aria-hidden": "true",
	}},
		html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"class": "modal-panel"}},
			render.Tag("h3", nil, render.Text("Permission requested")),
			render.Tag("div", map[string]string{"id": "perm-tool", "class": "perm-tool"}, render.Text("")),
			render.Tag("pre", map[string]string{"id": "perm-args", "class": "perm-args"}, render.Text("")),
			html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"class": "modal-buttons"}},
				render.Tag("button", map[string]string{"id": "perm-yes", "class": "btn btn-ok"}, render.Text("Allow once")),
				render.Tag("button", map[string]string{"id": "perm-tool-btn", "class": "btn"}, render.Text("Allow tool")),
				render.Tag("button", map[string]string{"id": "perm-session", "class": "btn"}, render.Text("Allow session")),
				render.Tag("button", map[string]string{"id": "perm-always", "class": "btn btn-always"}, render.Text("Allow always")),
				render.Tag("button", map[string]string{"id": "perm-no", "class": "btn btn-deny"}, render.Text("Deny")),
			),
		),
	)

	body := html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "app"}},
		statusBar,
		html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"id": "layout"}},
			sidebar,
			main,
		),
		permissionModal,
	)

	return render.Join(
		render.Raw("<!doctype html>"),
		render.Tag("html", map[string]string{"lang": "en"},
			render.Tag("head", nil,
				render.VoidTag("meta", map[string]string{"charset": "utf-8"}),
				render.VoidTag("meta", map[string]string{
					"name": "viewport", "content": "width=device-width,initial-scale=1",
				}),
				render.VoidTag("meta", map[string]string{
					"name": "harness-session", "content": sessionID,
				}),
				render.VoidTag("meta", map[string]string{
					"name": "harness-token", "content": token,
				}),
				render.Tag("title", nil, render.Text("gofastr harness — "+host)),
				render.Tag("style", nil, render.Raw(chatCSS)),
			),
			render.Tag("body", nil,
				body,
				render.Tag("script", nil, render.Raw(chatJS)),
			),
		),
	)
}

// renderLanding is the old endpoints-reference page, kept at
// /endpoints as a developer reference.
func renderLanding(host string) render.HTML {
	endpoints := []struct{ method, path, desc, auth string }{
		{"GET", "/v1/handshake", "wire-protocol version + features", ""},
		{"GET", "/v1/health", "subsystem status", ""},
		{"GET", "/v1/sessions", "list active sessions", "token"},
		{"POST", "/v1/sessions/<id>/input", "send turn input", "token"},
		{"GET", "/v1/sessions/<id>/events", "SSE event stream", "token"},
		{"WS", "/v1/ws?session=<id>", "bidirectional command/event frames", "token"},
	}
	rows := make([]render.HTML, 0, len(endpoints))
	for _, e := range endpoints {
		methodSpan := render.Tag("code",
			map[string]string{"class": "method method-" + strings.ToLower(e.method)},
			render.Text(e.method))
		pathSpan := render.Tag("code", nil, render.Text(e.path))
		desc := render.Text(" — " + e.desc)
		var authMark render.HTML
		if e.auth != "" {
			authMark = render.Tag("small", nil, render.Text("  ("+e.auth+" required)"))
		}
		rows = append(rows, html.ListItem(html.ListItemConfig{},
			methodSpan, render.Text(" "), pathSpan, desc, authMark))
	}

	body := html.Div(html.DivConfig{},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("gofastr harness")),
		html.Paragraph(html.TextConfig{},
			render.Text("Control plane is live at "),
			render.Tag("code", nil, render.Text("http://"+host+"/v1/")),
			render.Text(". This sidecar is rendered by the gofastr framework itself — no hand-written HTML, no JS framework. The full interactive web client is on the roadmap; for now the REST + WS endpoints below are the primary surface.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Endpoints")),
		html.UnorderedList(html.ListConfig{}, rows...),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Curl example")),
		render.Tag("pre", nil,
			render.Text("curl -H \"Authorization: Bearer $TOKEN\" http://"+host+"/v1/sessions")),
		html.Paragraph(html.TextConfig{},
			render.Tag("small", nil,
				render.Text("The session token was printed to the harness stderr on boot."))),
	)

	return render.Join(
		render.Raw("<!doctype html>"),
		render.Tag("html", map[string]string{"lang": "en"},
			render.Tag("head", nil,
				render.VoidTag("meta", map[string]string{"charset": "utf-8"}),
				render.Tag("title", nil, render.Text("gofastr harness")),
				render.Tag("style", nil, render.Raw(landingCSS)),
			),
			render.Tag("body", nil, body),
		),
	)
}

const chatCSS = `
:root {
  --bg: #0d1117; --bg-soft: #161b22; --fg: #e6edf3; --dim: #8b949e;
  --accent: #58a6ff; --user: #1f6feb; --assistant: #6e40c9;
  --tool: #f0b72f; --ok: #56d364; --err: #ff7b72;
  --frame: #30363d; --frame-soft: #21262d;
  --bubble-user: #1f3057; --bubble-assistant: #21262d;
  --diff-add: #033a16; --diff-del: #5a1d1d;
}
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; background: var(--bg); color: var(--fg);
  font: 14px/1.5 -apple-system, system-ui, "Segoe UI", Roboto, sans-serif;
  height: 100%; }
code, pre, .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
button { background: var(--frame-soft); border: 1px solid var(--frame); color: var(--fg);
  font: inherit; padding: .35rem .8rem; border-radius: 4px; cursor: pointer; }
button:hover { background: var(--frame); border-color: var(--accent); }
button:active { transform: translateY(1px); }

#app { display: flex; flex-direction: column; height: 100vh; }
#status-bar { display: flex; align-items: center; gap: .5rem; padding: .6rem 1rem;
  border-bottom: 1px solid var(--frame); color: var(--dim); font-size: 12px;
  background: var(--bg-soft); }
.status-name { color: var(--fg); font-weight: 600; }
.status-sep { opacity: .5; }
#status-cost { color: var(--ok); font-variant-numeric: tabular-nums; }
.status-spacer { flex: 1; }
.status-link { color: var(--dim); text-decoration: none; }
.status-link:hover { color: var(--accent); }

#layout { display: flex; flex: 1; overflow: hidden; }
#sidebar { width: 14rem; min-width: 14rem; padding: 1rem; border-right: 1px solid var(--frame);
  overflow-y: auto; background: var(--bg-soft); font-size: 13px; }
#sidebar h3 { font-size: 11px; text-transform: uppercase; letter-spacing: .1em;
  color: var(--dim); margin: 1rem 0 .5rem; font-weight: 600; }
#sidebar h3:first-child { margin-top: 0; }
#session-list { display: flex; flex-direction: column; gap: .15rem; }
.session-item { padding: .35rem .5rem; border-radius: 4px; cursor: default;
  font-family: ui-monospace, monospace; font-size: 11px; color: var(--dim);
  display: flex; align-items: center; gap: .5rem; }
.session-item:hover { background: var(--frame-soft); }
.session-item.active { background: var(--frame-soft); color: var(--fg); }
.session-item .dot { width: 6px; height: 6px; border-radius: 50%; background: var(--ok);
  flex-shrink: 0; }
.session-item.active .dot { background: var(--accent); box-shadow: 0 0 6px var(--accent); }
.sidebar-empty { color: var(--dim); font-style: italic; padding: .35rem .5rem; }
#quick-cmds { display: flex; flex-wrap: wrap; gap: .25rem; }
.qc { font-size: 11px; padding: .2rem .5rem; font-family: ui-monospace, monospace; }

#task-panel { display: flex; flex-direction: column; gap: .25rem; }
.task-item { display: flex; gap: .4rem; align-items: flex-start; padding: .3rem .4rem;
  border-radius: 4px; font-size: 12px; line-height: 1.35; }
.task-item .mark { flex-shrink: 0; font-family: ui-monospace, monospace;
  color: var(--dim); width: 1em; text-align: center; }
.task-item[data-status="completed"]   { color: var(--dim); text-decoration: line-through; }
.task-item[data-status="completed"] .mark { color: var(--ok); }
.task-item[data-status="in_progress"] { color: var(--fg); background: var(--frame-soft); }
.task-item[data-status="in_progress"] .mark { color: var(--accent); }
.task-item[data-status="pending"]     { color: var(--dim); }

#main { flex: 1; display: flex; flex-direction: column; overflow: hidden;
  max-width: 56rem; margin: 0 auto; width: 100%; padding: 0 1rem; }
#scrollback { flex: 1; overflow-y: auto; overflow-x: hidden; padding: 1rem 0;
  min-width: 0; }
.welcome { color: var(--dim); margin-bottom: 1rem; padding: .75rem 1rem;
  border-left: 3px solid var(--accent); background: var(--bg-soft); border-radius: 0 4px 4px 0; }
.welcome strong { color: var(--fg); display: inline-block; margin-bottom: .25rem; }

.bubble { display: flex; gap: .6rem; margin: .8rem 0; position: relative; }
.bubble .avatar { width: 24px; height: 24px; border-radius: 50%; flex-shrink: 0;
  font-size: 12px; display: flex; align-items: center; justify-content: center;
  font-weight: 600; }
.bubble.user .avatar { background: var(--user); color: white; }
.bubble.assistant .avatar { background: var(--assistant); color: white; }
.bubble .body { flex: 1; min-width: 0; padding: .5rem .8rem; border-radius: 8px;
  background: var(--bubble-assistant);
  overflow-wrap: anywhere; word-break: break-word;
  max-width: 100%; overflow-x: hidden; }
.bubble .body pre { overflow-x: auto; max-width: 100%; }
.bubble.user .body { background: var(--bubble-user); }
.bubble .body p { margin: .25rem 0; }
.bubble .body code { background: rgba(0,0,0,.3); padding: .05rem .3rem; border-radius: 3px; font-size: .9em; }
.bubble .body pre { background: rgba(0,0,0,.4); padding: .6rem; border-radius: 4px;
  overflow-x: auto; margin: .5rem 0; }
.bubble .body pre code { background: transparent; padding: 0; }
.bubble .body h1, .bubble .body h2, .bubble .body h3 { margin: .8rem 0 .3rem; font-size: 1em; }
.bubble .body ul { margin: .25rem 0; padding-left: 1.2rem; }
.bubble .body a { color: var(--accent); }
.bubble .copy-btn { position: absolute; top: 4px; right: 4px; padding: .1rem .4rem;
  font-size: 10px; opacity: 0; transition: opacity .15s; }
.bubble:hover .copy-btn { opacity: 1; }
.bubble .copy-btn.copied { color: var(--ok); }

.thinking-row { display: flex; align-items: center; gap: .5rem; padding: .5rem .8rem;
  color: var(--dim); font-style: italic; font-size: 13px; margin: .5rem 0;
  font-family: ui-monospace, monospace; }
.spinner { display: inline-block; width: 12px; height: 12px; border: 2px solid var(--frame);
  border-top-color: var(--accent); border-radius: 50%; animation: spin .8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.thinking-row.collapsed { font-style: normal; }
.thinking-row.collapsed .spinner { display: none; }

.tool-card { border: 1px solid var(--frame); border-radius: 6px; margin: .6rem 0;
  background: var(--bg-soft); overflow: hidden; max-width: 100%; min-width: 0; }
.tool-card .header { display: flex; align-items: center; gap: .5rem;
  padding: .5rem .8rem; cursor: pointer; user-select: none;
  font-family: ui-monospace, monospace; font-size: 13px; }
.tool-card .header:hover { background: var(--frame-soft); }
.tool-card .header .glyph { color: var(--tool); }
.tool-card .header .name { font-weight: 600; }
.tool-card .header .args { color: var(--dim); overflow: hidden;
  text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0; }
.tool-card .header .chevron { color: var(--dim); transition: transform .15s; }
.tool-card.open .header .chevron { transform: rotate(90deg); }
.tool-card .preview { padding: .35rem .8rem .5rem 2rem; color: var(--dim);
  font-family: ui-monospace, monospace; font-size: 12px; white-space: pre;
  overflow: hidden; text-overflow: ellipsis; }
.tool-card .preview .line { display: block; overflow: hidden; text-overflow: ellipsis;
  white-space: nowrap; }
.tool-card .preview .more { color: var(--accent); font-style: italic; font-size: 11px;
  margin-top: .15rem; }
.tool-card.open .preview { display: none; }
.tool-card .body { display: none; padding: .5rem .8rem; border-top: 1px solid var(--frame);
  background: var(--bg); font-family: ui-monospace, monospace; font-size: 12px;
  white-space: pre-wrap;
  /* Force aggressive wrapping so even unbreakable strings (URLs, long
     JSON keys, base64 blobs) stay inside the card. overflow-wrap:
     anywhere lets the renderer break in the middle of a "word" when
     there's no other choice. word-break:break-word is the older
     fallback. Without these, a single long token causes the tool
     card to overflow horizontally and push the page width past the
     viewport — which is what made it impossible to scroll past
     the card. */
  overflow-wrap: anywhere; word-break: break-word;
  max-width: 100%; overflow-x: hidden; }
.tool-card.open .body { display: block; }
.tool-card .preview .line { max-width: 100%; }
.tool-card .body.err { color: var(--err); }
.tool-card .body .diff-add { color: var(--ok); background: var(--diff-add); display: block; }
.tool-card .body .diff-del { color: var(--err); background: var(--diff-del); display: block; }
.tool-card .body .diff-hunk { color: var(--accent); display: block; }

.row.error { color: var(--err); padding: .4rem .8rem; background: rgba(255,123,114,.1);
  border-left: 3px solid var(--err); border-radius: 0 4px 4px 0; margin: .5rem 0;
  font-family: ui-monospace, monospace; font-size: 12px; }
.row.separator { color: var(--frame); margin: 1rem 0; text-align: center; font-size: 10px; }

#input-form { padding: .5rem 0 1rem 0; }
.input-box { display: flex; align-items: center; border: 1px solid var(--frame);
  border-radius: 8px; padding: .5rem .8rem; background: var(--bg-soft);
  transition: border-color .15s; }
.input-box:focus-within { border-color: var(--accent);
  box-shadow: 0 0 0 2px rgba(88,166,255,.15); }
.prompt { color: var(--dim); margin-right: .5rem; font-weight: 600; font-family: ui-monospace, monospace; }
#input { flex: 1; background: transparent; border: 0; outline: 0; color: var(--fg);
  font: inherit; }
#input::placeholder { color: var(--dim); }
#send { padding: .3rem .8rem; font-size: 12px; }

.modal { position: fixed; inset: 0; display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,.5); z-index: 100; backdrop-filter: blur(2px); }
.modal.hidden { display: none; }
.modal-panel { background: var(--bg-soft); border: 1px solid var(--frame); border-radius: 8px;
  padding: 1.5rem; max-width: 32rem; width: 90%; }
.modal-panel h3 { margin: 0 0 .5rem; }
.perm-tool { color: var(--tool); font-family: ui-monospace, monospace; margin-bottom: .5rem; }
.perm-args { background: var(--bg); padding: .6rem; border-radius: 4px; overflow-x: auto;
  font-size: 12px; max-height: 12rem; }
.modal-buttons { display: flex; gap: .5rem; flex-wrap: wrap; justify-content: flex-end;
  margin-top: 1rem; }
.btn-ok { background: var(--ok); color: black; border-color: var(--ok); }
.btn-ok:hover { background: var(--ok); filter: brightness(1.1); }
.btn-deny { background: var(--err); color: black; border-color: var(--err); }
.btn-deny:hover { background: var(--err); filter: brightness(1.1); }
.btn-always { background: var(--accent); color: black; border-color: var(--accent); }
.btn-always:hover { background: var(--accent); filter: brightness(1.1); }
`

const chatJS = `
(() => {
  const SESSION = document.querySelector('meta[name="harness-session"]').content;
  const TOKEN   = document.querySelector('meta[name="harness-token"]').content;
  const BASE    = location.protocol + '//' + location.host;
  const sb = document.getElementById('scrollback');
  const form = document.getElementById('input-form');
  const input = document.getElementById('input');
  const statusCost = document.getElementById('status-cost');
  const statusModel = document.getElementById('status-model');
  const sessionList = document.getElementById('session-list');
  const permModal = document.getElementById('permission-modal');
  const permTool = document.getElementById('perm-tool');
  const permArgs = document.getElementById('perm-args');

  let assistantBubble = null; // currently-growing assistant bubble body
  let assistantText = '';     // raw text so we can re-render markdown
  let thinkingRow = null;     // currently-growing thinking row
  let thinkingStart = 0;
  let totalCost = 0;
  let pendingPermissionCallId = null;
  const toolCardsByCallId = new Map(); // callId → tool-card DOM

  // ----- HTML escape + tiny markdown renderer ---------------------
  function esc(s) {
    return String(s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;')
      .replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }
  function renderMarkdown(text) {
    if (!text) return '';
    let s = esc(text);
    // Fenced code blocks ` + "`" + `` + "`" + `` + "`" + ` ... ` + "`" + `` + "`" + `` + "`" + `
    s = s.replace(/` + "`" + `` + "`" + `` + "`" + `([\s\S]*?)` + "`" + `` + "`" + `` + "`" + `/g,
      (_, code) => '<pre><code>' + code.replace(/^\n/, '') + '</code></pre>');
    // Inline code
    s = s.replace(/` + "`" + `([^` + "`" + `\n]+?)` + "`" + `/g, '<code>$1</code>');
    // Bold + italic
    s = s.replace(/\*\*([^\*\n]+)\*\*/g, '<strong>$1</strong>');
    s = s.replace(/(^|[^\*])\*([^\*\n]+)\*/g, '$1<em>$2</em>');
    // Headings (### text)
    s = s.replace(/^(#{1,6})\s+(.+)$/gm,
      (_, hashes, text) => '<h' + hashes.length + '>' + text + '</h' + hashes.length + '>');
    // Bullet lists
    s = s.replace(/((?:^[-*]\s.+\n?)+)/gm, (block) => {
      const items = block.trim().split(/\n/).map(l => '<li>' + l.replace(/^[-*]\s+/, '') + '</li>');
      return '<ul>' + items.join('') + '</ul>';
    });
    // Auto-link bare URLs
    s = s.replace(/(https?:\/\/[^\s<]+)/g,
      (url) => '<a href="' + url + '" target="_blank" rel="noopener">' + url + '</a>');
    // Paragraphs: wrap remaining bare lines
    s = s.split(/\n\n+/).map(p =>
      /^<(h\d|ul|pre|p|blockquote)/.test(p) ? p : ('<p>' + p.replace(/\n/g, '<br>') + '</p>')
    ).join('');
    return s;
  }

  function makeCopyBtn(getText) {
    const btn = document.createElement('button');
    btn.className = 'copy-btn';
    btn.textContent = 'copy';
    btn.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(getText());
        btn.textContent = 'copied';
        btn.classList.add('copied');
        setTimeout(() => { btn.textContent = 'copy'; btn.classList.remove('copied'); }, 1500);
      } catch { btn.textContent = 'failed'; }
    });
    return btn;
  }

  // ----- Bubble + tool card builders ------------------------------
  function appendUserBubble(text) {
    const wrap = document.createElement('div');
    wrap.className = 'bubble user';
    const av = document.createElement('div'); av.className = 'avatar'; av.textContent = '→';
    const body = document.createElement('div'); body.className = 'body';
    body.innerHTML = renderMarkdown(text);
    wrap.appendChild(av); wrap.appendChild(body);
    wrap.appendChild(makeCopyBtn(() => text));
    sb.appendChild(wrap);
    autoscroll();
    return body;
  }

  function ensureAssistantBubble() {
    if (assistantBubble) return assistantBubble;
    const wrap = document.createElement('div');
    wrap.className = 'bubble assistant';
    const av = document.createElement('div'); av.className = 'avatar'; av.textContent = '←';
    const body = document.createElement('div'); body.className = 'body';
    wrap.appendChild(av); wrap.appendChild(body);
    wrap.appendChild(makeCopyBtn(() => assistantText));
    sb.appendChild(wrap);
    assistantBubble = body;
    assistantText = '';
    autoscroll();
    return body;
  }

  function ensureThinkingRow() {
    if (thinkingRow) return thinkingRow;
    const row = document.createElement('div');
    row.className = 'thinking-row';
    const sp = document.createElement('span'); sp.className = 'spinner';
    const tx = document.createElement('span'); tx.className = 'tx';
    tx.textContent = 'thinking…';
    row.appendChild(sp); row.appendChild(tx);
    sb.appendChild(row);
    thinkingRow = row;
    thinkingStart = Date.now();
    autoscroll();
    return row;
  }

  function collapseThinking() {
    if (!thinkingRow) return;
    const secs = Math.max(1, Math.round((Date.now() - thinkingStart) / 1000));
    thinkingRow.classList.add('collapsed');
    const tx = thinkingRow.querySelector('.tx');
    tx.textContent = '… cogitated for ' + secs + 's';
    thinkingRow = null;
  }

  function detectDiff(text) {
    // Heuristic: contains a hunk header or alternating +/- lines.
    return /^@@/m.test(text) || /^\+\+\+/m.test(text) ||
      (/^[-+]/m.test(text) && text.split('\n').filter(l => /^[+-]/.test(l)).length >= 2);
  }
  function renderDiff(text) {
    return text.split('\n').map(line => {
      if (line.startsWith('@@')) return '<span class="diff-hunk">' + esc(line) + '</span>';
      if (line.startsWith('+') && !line.startsWith('+++')) return '<span class="diff-add">' + esc(line) + '</span>';
      if (line.startsWith('-') && !line.startsWith('---')) return '<span class="diff-del">' + esc(line) + '</span>';
      return esc(line);
    }).join('\n');
  }

  function appendToolCard(callId, name, args) {
    const card = document.createElement('div');
    card.className = 'tool-card';
    const header = document.createElement('div'); header.className = 'header';
    const argsStr = JSON.stringify(args || {});
    header.innerHTML =
      '<span class="glyph">●</span>' +
      '<span class="name">' + esc(name || '?') + '</span>' +
      '<span class="args">' + esc(argsStr) + '</span>' +
      '<span class="chevron">▸</span>';
    header.addEventListener('click', () => card.classList.toggle('open'));
    const preview = document.createElement('div'); preview.className = 'preview';
    preview.innerHTML = '<span class="line">running…</span>';
    const body = document.createElement('div'); body.className = 'body';
    body.textContent = '';
    card.appendChild(header);
    card.appendChild(preview);
    card.appendChild(body);
    sb.appendChild(card);
    if (callId) toolCardsByCallId.set(callId, card);
    autoscroll();
    return card;
  }

  const PREVIEW_LINES = 3;

  function updateToolCardResult(callId, text, isError) {
    const card = toolCardsByCallId.get(callId);
    if (!card) return;
    const body = card.querySelector('.body');
    const preview = card.querySelector('.preview');
    body.classList.toggle('err', !!isError);
    const safe = text || '(no output)';
    // Full body (used when user expands).
    if (detectDiff(safe)) {
      body.innerHTML = renderDiff(safe);
    } else {
      body.textContent = safe;
    }
    // Preview = first PREVIEW_LINES non-empty source lines, one per row.
    const lines = safe.split('\n');
    preview.innerHTML = '';
    const shown = lines.slice(0, PREVIEW_LINES);
    for (const line of shown) {
      const span = document.createElement('span');
      span.className = 'line';
      span.textContent = line || ' ';
      preview.appendChild(span);
    }
    if (lines.length > PREVIEW_LINES) {
      const more = document.createElement('span');
      more.className = 'more';
      more.textContent = '… +' + (lines.length - PREVIEW_LINES) + ' more lines (click to expand)';
      preview.appendChild(more);
    }
    // Stay COLLAPSED by default — user expands by clicking the
    // header. Avoids the previous behavior where every tool card
    // popped open and ate the viewport.
  }

  function appendError(msg) {
    const row = document.createElement('div');
    row.className = 'row error';
    row.textContent = '[error] ' + msg;
    sb.appendChild(row);
    autoscroll();
  }

  function appendSeparator() {
    const row = document.createElement('div');
    row.className = 'row separator';
    row.textContent = '────── new turn ──────';
    sb.appendChild(row);
    autoscroll();
  }

  function autoscroll() {
    sb.scrollTop = sb.scrollHeight;
  }

  // ----- Cost tracker ---------------------------------------------
  function bumpCost(usd) {
    totalCost += usd || 0;
    statusCost.textContent = '$' + totalCost.toFixed(4);
  }

  // ----- Permission modal -----------------------------------------
  function openPermissionModal(tool, args, callId) {
    permTool.textContent = tool || '?';
    permArgs.textContent = JSON.stringify(args || {}, null, 2);
    pendingPermissionCallId = callId;
    permModal.classList.remove('hidden');
  }
  function closePermissionModal() {
    permModal.classList.add('hidden');
    pendingPermissionCallId = null;
  }
  async function answerPermission(decision, scope) {
    const callId = pendingPermissionCallId;
    closePermissionModal();
    if (!callId) return;
    try {
      await fetch(BASE + '/v1/sessions/' + SESSION + '/permission', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Harness-Token': TOKEN },
        body: JSON.stringify({
          sessionId: SESSION, callId,
          decision, scope: scope || 'once',
        }),
      });
    } catch (err) {
      appendError('permission send failed: ' + err.message);
    }
  }
  document.getElementById('perm-yes').addEventListener('click', () => answerPermission('allow', 'once'));
  document.getElementById('perm-tool-btn').addEventListener('click', () => answerPermission('allow', 'tool'));
  document.getElementById('perm-session').addEventListener('click', () => answerPermission('allow', 'session'));
  document.getElementById('perm-always').addEventListener('click', () => answerPermission('allow', 'always'));
  document.getElementById('perm-no').addEventListener('click', () => answerPermission('deny', 'once'));
  permModal.addEventListener('click', (e) => { if (e.target === permModal) closePermissionModal(); });

  // ----- Session sidebar ------------------------------------------
  async function refreshSessions() {
    try {
      const res = await fetch(BASE + '/v1/sessions',
        { headers: { 'X-Harness-Token': TOKEN } });
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const list = await res.json();
      sessionList.innerHTML = '';
      if (!Array.isArray(list) || list.length === 0) {
        sessionList.innerHTML = '<div class="sidebar-empty">no sessions</div>';
        return;
      }
      for (const s of list) {
        const item = document.createElement('div');
        const id = s.sessionID || s.session || s.id || '';
        item.className = 'session-item' + (id === SESSION ? ' active' : '');
        item.innerHTML = '<span class="dot"></span><span>' + esc(id.slice(0, 16)) +
          (id.length > 16 ? '…' : '') + '</span>';
        item.title = (s.model || '') + ' • ' + (s.profile || '');
        sessionList.appendChild(item);
      }
      if (list.length > 0 && list[0].model) statusModel.textContent = list[0].model;
    } catch (err) {
      sessionList.innerHTML = '<div class="sidebar-empty">load failed</div>';
    }
  }
  refreshSessions();
  setInterval(refreshSessions, 5000);

  // ----- Task panel ----------------------------------------------
  const taskPanel = document.getElementById('task-panel');
  async function refreshTasks() {
    try {
      const res = await fetch(BASE + '/v1/sessions/' + SESSION + '/tasks', {
        headers: { 'X-Harness-Token': TOKEN },
      });
      if (!res.ok) return;
      const body = await res.json();
      const tasks = body.tasks || [];
      taskPanel.innerHTML = '';
      if (tasks.length === 0) {
        taskPanel.innerHTML = '<div class="sidebar-empty">no plan yet</div>';
        return;
      }
      for (const t of tasks) {
        const row = document.createElement('div');
        row.className = 'task-item';
        row.dataset.status = t.status || 'pending';
        const mark = document.createElement('span');
        mark.className = 'mark';
        mark.textContent = t.status === 'completed' ? '✓' :
          t.status === 'in_progress' ? '▸' : '○';
        const body = document.createElement('span');
        body.textContent = t.activeForm && t.status === 'in_progress'
          ? t.activeForm
          : t.content;
        row.appendChild(mark);
        row.appendChild(body);
        taskPanel.appendChild(row);
      }
    } catch {
      /* silently ignore — UI just won't update until next tick */
    }
  }
  refreshTasks();
  setInterval(refreshTasks, 2000);

  // Quick command buttons
  document.querySelectorAll('.qc').forEach(btn => {
    btn.addEventListener('click', () => {
      input.value = btn.dataset.cmd;
      input.focus();
    });
  });

  // ----- SSE event dispatch ---------------------------------------
  function handleEvent(ev) {
    const data = JSON.parse(ev.data);
    const kind = data.kind;
    const payload = data.payload || {};
    switch (kind) {
      case 'TurnStarted':
        collapseThinking();
        assistantBubble = null;
        for (const b of (payload.content || [])) {
          if (b.type === 'text' && b.text) appendUserBubble(b.text);
        }
        ensureThinkingRow();
        break;
      case 'TextDelta':
        collapseThinking();
        const body = ensureAssistantBubble();
        assistantText += payload.text || '';
        body.innerHTML = renderMarkdown(assistantText);
        autoscroll();
        break;
      case 'ThinkingDelta':
        ensureThinkingRow();
        const block = payload.block;
        let text = '';
        try { text = typeof block === 'string' ? JSON.parse(block) : (block || ''); }
        catch { text = String(block || ''); }
        const tx = thinkingRow.querySelector('.tx');
        tx.textContent = 'thinking… ' + (text.slice(-80) || '');
        autoscroll();
        break;
      case 'ToolCallStarted':
        collapseThinking();
        assistantBubble = null;
        appendToolCard(payload.callId, payload.tool, payload.args);
        break;
      case 'ToolResult':
        assistantBubble = null;
        const blocks = payload.content || [];
        let resultText = '';
        for (const b of blocks) if (b.type === 'text') { resultText = b.text || ''; break; }
        if (toolCardsByCallId.has(payload.callId)) {
          updateToolCardResult(payload.callId, resultText, payload.isError);
        } else {
          const card = appendToolCard(payload.callId, '(result)', {});
          updateToolCardResult(payload.callId, resultText, payload.isError);
        }
        break;
      case 'TurnEnded':
        collapseThinking();
        assistantBubble = null;
        appendSeparator();
        break;
      case 'Error':
        appendError(payload.message || 'unknown');
        break;
      case 'PermissionRequested':
        openPermissionModal(payload.tool, payload.args, payload.callId);
        break;
      case 'CostIncremented':
        bumpCost(payload.usd);
        break;
      case 'ModelChanged':
        statusModel.textContent = payload.model || '…';
        break;
    }
  }

  // ----- SSE wiring -----------------------------------------------
  const es = new EventSource(
    BASE + '/v1/sessions/' + SESSION + '/events?token=' + encodeURIComponent(TOKEN));
  const KINDS = [
    'TextDelta', 'ThinkingDelta',
    'ToolCallStarted', 'ToolCallProgress', 'ToolResult',
    'TurnStarted', 'TurnEnded', 'Cancelled',
    'PermissionRequested', 'PermissionDecided',
    'Error', 'CostIncremented', 'CompactionTriggered',
    'ModelChanged', 'SessionCreated', 'SessionResumed',
  ];
  for (const k of KINDS) es.addEventListener(k, handleEvent);
  es.onmessage = handleEvent;
  es.onerror = () => { appendError('event stream disconnected'); };

  // ----- Form submit ----------------------------------------------
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const line = input.value;
    if (!line.trim()) return;
    input.value = '';
    try {
      const res = await fetch(BASE + '/v1/sessions/' + SESSION + '/input', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Harness-Token': TOKEN,
        },
        body: JSON.stringify({
          sessionId: SESSION,
          content: [{ type: 'text', text: line }],
        }),
      });
      if (!res.ok) {
        const t = await res.text();
        appendError('send failed: HTTP ' + res.status + ' ' + t);
      }
    } catch (err) {
      appendError('send failed: ' + err.message);
    }
  });

  // Esc closes permission modal as deny (safe default).
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !permModal.classList.contains('hidden')) {
      answerPermission('deny', 'once');
    }
  });
})();
`

const landingCSS = `
body { font: 14px/1.5 -apple-system, system-ui, sans-serif; max-width: 48rem;
       margin: 2rem auto; padding: 0 1rem; color: #222; }
h1 { font-size: 1.4rem; margin-top: 0; }
h2 { font-size: 1.05rem; margin-top: 1.6rem; }
code { background: #f4f4f4; padding: .12rem .3rem; border-radius: 3px;
       font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
pre { background: #f4f4f4; padding: .6rem .8rem; border-radius: 4px; overflow: auto; }
small { color: #666; }
.method { font-weight: 600; padding: .05rem .35rem; border-radius: 2px;
          background: #e7eef7; color: #1f3a5f; }
.method-post { background: #e9f5ec; color: #1d5a30; }
.method-ws   { background: #fbe9f0; color: #6c1d3e; }
ul { padding-left: 1.1rem; }
`
