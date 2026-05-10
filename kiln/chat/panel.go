package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/core/router"
	"github.com/gofastr/gofastr/core-ui/widget"
	"github.com/gofastr/gofastr/core-ui/widget/preset"
	"github.com/gofastr/gofastr/kiln/journal"
	"github.com/gofastr/gofastr/kiln/live"
	"github.com/gofastr/gofastr/kiln/protocol"
)

// AgentStateFn returns the JSON-shaped agent state consumed by the
// gear modal. Shape: { current: {name, display}, available: [{name,
// display, installed}, ...], in_flight: bool }. Concretely supplied
// by cmd/kiln (which owns the AdapterStore); kept as a callback so
// the chat package doesn't depend on cmd/kiln. Pass nil to render an
// empty modal (development / tests without an adapter registry).
type AgentStateFn func() any

// MountPanel registers the kiln chat panel as a core-ui/widget on r.
// The widget is a framework-managed FloatingPanel; kiln contributes:
//   - slot HTML for header / log / input (rendered server-side from session)
//   - SSE bindings that push fresh log HTML into the chat_html signal
//   - RPC handlers for chat send, reset, approve/reject, agent control
//
// agentState is read by the gear modal's agent_list_html signal at
// mount time so the user sees the actual adapter list (claude-code,
// pi, codex, ...) instead of a Loading… placeholder. nil → no list.
//
// All bespoke kiln widget plumbing (vanilla JS DOM, hand-rolled CSS,
// per-route HTTP handlers) is replaced by this single declaration.
func MountPanel(r *router.Router, l *live.Live, tools *protocol.Tools, agentState AgentStateFn) {
	pe := &panelEnv{live: l, tools: tools, agentState: agentState}

	// Build slot HTML once per Mount call. SSE events drive signal updates
	// that re-render the chat log via data-fui-signal="chat_html" (HTML mode).
	def := preset.FloatingPanel("kiln-panel").
		Slot("header", htmlComp{html: pe.headerHTML()}).
		Slot("log", htmlComp{html: pe.logHTMLForCurrent()}).
		Slot("input", htmlComp{html: pe.inputHTML()}).
		Skeleton(pe.skeleton).
		Signal("chat_html", widget.SignalFunc(func() (any, error) {
			return pe.logHTMLForCurrent(), nil
		})).
		Signal("agent", widget.SignalFunc(func() (any, error) {
			return pe.agentLabel(), nil
		})).
		Signal("chat_status", widget.SignalFunc(func() (any, error) {
			return pe.statusText(), nil
		})).
		Signal("world_snapshot", widget.SignalFunc(func() (any, error) {
			return pe.worldSnapshotText(), nil
		})).
		// Every world / chat event triggers a chat_html refresh —
		// SSERefetch re-pulls the rendered HTML from /state instead
		// of using the SSE payload (which is just metadata).
		SSERefetch("/.kiln/events", "chat_user", "chat_html").
		SSERefetch("/.kiln/events", "chat_assistant", "chat_html").
		SSERefetch("/.kiln/events", "world_edit", "chat_html").
		SSERefetch("/.kiln/events", "tool_call", "chat_html").
		SSERefetch("/.kiln/events", "tool_result", "chat_html").
		SSERefetch("/.kiln/events", "plan_proposed", "chat_html").
		SSERefetch("/.kiln/events", "plan_approved", "chat_html").
		SSERefetch("/.kiln/events", "plan_rejected", "chat_html").
		// session_reset (synthetic) clears the panel after the user
		// hits ↺. Without this the chat list shows stale items until
		// the next event lands. agent_turn_started/ended drive the
		// in-flight indicator in the header.
		SSERefetch("/.kiln/events", "session_reset", "chat_html").
		SSERefetch("/.kiln/events", "agent_turn_started", "chat_status").
		SSERefetch("/.kiln/events", "agent_turn_ended", "chat_status").
		SSERefetch("/.kiln/events", "chat_assistant", "chat_status").
		// Tool-call landing increments the in-flight tool counter
		// shown in the header — refresh chat_status on each one.
		SSERefetch("/.kiln/events", "tool_call", "chat_status").
		// Agent picker → header chip update.
		SSERefetch("/.kiln/events", "agent_changed", "agent").
		// World-snapshot pill: live count of entities/pages/routes/hooks.
		// Refresh on any world_edit so the pill keeps pace with the agent.
		SSERefetch("/.kiln/events", "world_edit", "world_snapshot").
		SSERefetch("/.kiln/events", "session_reset", "world_snapshot").
		// Page-affecting world edits: full reload so the now-rendered
		// page reflects the new world. Filtered by op so add_entity
		// (which doesn't change page rendering) doesn't trigger reloads.
		SSEReload("/.kiln/events", "world_edit", "op", "add_page").
		SSEReload("/.kiln/events", "world_edit", "op", "delete_page").
		SSEReload("/.kiln/events", "world_edit", "op", "add_route").
		SSEReload("/.kiln/events", "world_edit", "op", "delete_route").
		// RPC: chat send, reset, approve/reject, undo.
		// SSE refetch owns log updates; the synchronous RPC response is
		// just an ack. Binding chat_html to the response was a footgun:
		// on error (or even on success) the JSON ack got stringified
		// into the log innerHTML.
		RPC("POST", "/kiln/panel/send", http.HandlerFunc(pe.serveSend)).
		RPC("POST", "/kiln/panel/reset", http.HandlerFunc(pe.serveReset)).
		RPC("POST", "/kiln/panel/approve_plan", http.HandlerFunc(pe.serveApprove)).
		RPC("POST", "/kiln/panel/reject_plan", http.HandlerFunc(pe.serveReject)).
		RPC("POST", "/kiln/panel/undo", http.HandlerFunc(pe.serveUndo)).
		Build()

	def.ExtraCSS = widgetCSS // appends panel content CSS after framework chrome
	widget.Mount(r, &def)

	// Hidden Modal: agent-settings, opened by the gear button via
	// data-fui-open="kiln-agent-settings". Loads the same panel CSS
	// (widgetCSS) so .kiln-modal-card, .kiln-modal-title, .kiln-button
	// and friends are styled. Without this the modal renders as
	// transparent floating text — looks like the gear "doesn't work".
	settings := preset.Modal("kiln-agent-settings").
		Hidden().
		Slot("body", htmlComp{html: pe.agentSettingsHTML()}).
		// Server-rendered HTML for the adapter list. The runtime
		// hydrates [data-fui-signal="agent_list_html"][mode="html"]
		// on modal mount, replacing the Loading… placeholder.
		Signal("agent_list_html", widget.SignalFunc(func() (any, error) {
			return pe.agentListHTML(), nil
		})).
		Build()
	settings.ExtraCSS = widgetCSS
	widget.Mount(r, &settings)

	// Hidden Modal: reset confirmation, opened by the ↺ button via
	// data-fui-open="kiln-reset-confirm". Reset is destructive
	// (truncates journal + drops DB schema) so a single misclick
	// shouldn't lose work — the modal forces an explicit Confirm.
	resetConfirm := preset.Modal("kiln-reset-confirm").
		Hidden().
		Slot("body", htmlComp{html: pe.resetConfirmHTML()}).
		Build()
	resetConfirm.ExtraCSS = widgetCSS
	widget.Mount(r, &resetConfirm)

	widget.MountRuntime(r) // idempotent — the framework runtime URL goes here
}

func (pe *panelEnv) resetConfirmHTML() string {
	return `<div class="kiln-modal">` +
		`<h2 class="kiln-modal-title">Reset session?</h2>` +
		// Live count updates via the world_snapshot signal which
		// refreshes on every world_edit / session_reset.
		`<p class="kiln-modal-sub">Currently live: <strong data-fui-signal="world_snapshot">` + escHTML(pe.worldSnapshotText()) + `</strong>. Reset wipes the journal, drops the live DB schema, and clears the chat. Anything not frozen is gone.</p>` +
		`<p class="kiln-modal-tip">Snapshot first with <code>kiln freeze --diff</code> in your terminal — emits a review summary you can paste into a commit message before resetting.</p>` +
		`<div class="kiln-modal-actions">` +
		`<button type="button" class="kiln-modal-cancel" data-fui-action="close">Cancel <kbd class="kiln-kbd">Esc</kbd></button>` +
		`<button type="button" class="kiln-modal-apply kiln-modal-danger" data-fui-rpc="/kiln/panel/reset" data-fui-rpc-close>Reset</button>` +
		`</div>` +
		`</div>`
}

// agentSettingsHTML is the modal body. The list itself is server-
// rendered via the agent_list_html signal — the runtime hydrates
// the placeholder on widget mount.
//
// Uses kiln-modal-* classes that already have styled rules in
// kiln/chat/style.go (.kiln-modal — the card container, etc.).
func (pe *panelEnv) agentSettingsHTML() string {
	return `<div class="kiln-modal">` +
		`<h2 class="kiln-modal-title">Agent settings</h2>` +
		`<p class="kiln-modal-sub">Pick which CLI agent kiln spawns when you send a message.</p>` +
		`<div class="kiln-modal-body" id="kiln-agent-list" data-fui-signal="agent_list_html" data-fui-signal-mode="html">Loading…</div>` +
		`<div class="kiln-modal-actions">` +
		`<button type="button" class="kiln-modal-cancel" data-fui-action="close">Close <kbd class="kiln-kbd">Esc</kbd></button>` +
		`</div>` +
		`</div>`
}

// agentListHTML renders the adapter list as HTML. Each row is a label
// containing a radio + name + description + installed flag. The
// "current" adapter is checked. Apply happens via the /kiln/panel/agent_select
// RPC bound on the form (see serveAgentSelect).
func (pe *panelEnv) agentListHTML() string {
	if pe.agentState == nil {
		return `<p class="kiln-modal-sub">No agent registry mounted.</p>`
	}
	state, _ := pe.agentState().(map[string]any)
	if state == nil {
		return `<p class="kiln-modal-sub">No agent registry mounted.</p>`
	}
	curName := ""
	if cur, ok := state["current"].(map[string]any); ok {
		curName, _ = cur["name"].(string)
	}
	available, _ := state["available"].([]map[string]any)

	var b strings.Builder

	// If zero adapters are installed, prepend an install hint —
	// otherwise the modal looks like a list of broken options.
	anyInstalled := false
	for _, a := range available {
		if installed, _ := a["installed"].(bool); installed {
			anyInstalled = true
			break
		}
	}
	if !anyInstalled && len(available) > 0 {
		b.WriteString(`<p class="kiln-modal-tip">No agent CLIs detected on PATH. Install one to enable chat-driven builds — e.g. ` +
			`<code>brew install pi</code>, ` +
			`<code>npm i -g @anthropic-ai/claude-code</code>, ` +
			`or <code>npm i -g @openai/codex</code>.</p>`)
	}

	// Form posts directly to /kiln/agent (registered by cmd/kiln).
	// data-fui-rpc-close dismisses the modal on a successful 2xx so
	// the user gets visible feedback that Apply landed. Re-open to
	// see the new "current" mark; the panel header chip still needs
	// SSE wiring (separate task) to update without a modal re-open.
	b.WriteString(`<form class="kiln-adapter-list" data-fui-rpc="/kiln/agent" data-fui-rpc-close>`)

	// "none" sentinel — always present, always installed.
	writeAdapterRow(&b, "none", "(no agent — chat goes to journal but nothing runs)", true, curName == "none" || curName == "")

	for _, a := range available {
		name, _ := a["name"].(string)
		display, _ := a["display"].(string)
		installed, _ := a["installed"].(bool)
		writeAdapterRow(&b, name, display, installed, curName == name)
	}

	b.WriteString(`<div class="kiln-modal-actions">`)
	b.WriteString(`<button type="submit" class="kiln-modal-apply">Apply</button>`)
	b.WriteString(`</div>`)
	b.WriteString(`</form>`)
	return b.String()
}

func writeAdapterRow(b *strings.Builder, name, display string, installed, isCurrent bool) {
	rowClass := "kiln-adapter-row"
	if !installed {
		rowClass += " kiln-adapter-row-disabled"
	}
	fmt.Fprintf(b, `<label class="%s" data-installed="%t">`, rowClass, installed)
	checked := ""
	if isCurrent {
		checked = ` checked`
	}
	disabled := ""
	if !installed {
		disabled = ` disabled`
	}
	fmt.Fprintf(b,
		`<input type="radio" name="name" value="%s" class="kiln-adapter-radio"%s%s>`,
		escAttr(name), checked, disabled,
	)
	suffix := ""
	if !installed {
		suffix = " — not installed"
	}
	fmt.Fprintf(b,
		`<div class="kiln-adapter-label"><div class="kiln-adapter-name">%s</div><div class="kiln-adapter-display">%s%s</div></div>`,
		escHTML(name), escHTML(display), escHTML(suffix),
	)
	b.WriteString(`</label>`)
}

// skeleton renders the kiln panel chrome with the floating-panel
// classes the existing CSS already targets (.kiln-widget,
// .kiln-panel.kiln-open, etc.). The fui-* classes are added alongside
// so the framework's positioning + bootstrap behavior also applies.
func (pe *panelEnv) skeleton(slots map[string]render.HTML) render.HTML {
	var b strings.Builder
	b.WriteString(`<div class="fui-widget fui-pos-bottom-right kiln-widget kiln-corner-bottom-right" data-fui-widget="kiln-panel">`)
	b.WriteString(`<section class="kiln-panel kiln-open" role="dialog" aria-label="Kiln agent">`)
	if h, ok := slots["header"]; ok {
		b.WriteString(string(h))
	}
	if l, ok := slots["log"]; ok {
		// data-fui-scroll-bottom-on-update=".kiln-log" pushes the inner
		// scrollable list to bottom after each chat_html refresh; the
		// wrap itself has overflow:hidden, so the scroll is on .kiln-log.
		// aria-live="polite" announces new content to screen readers.
		b.WriteString(`<div class="kiln-log-wrap" role="log" aria-live="polite" aria-relevant="additions text" data-fui-signal="chat_html" data-fui-signal-mode="html" data-fui-scroll-bottom-on-update=".kiln-log">`)
		b.WriteString(string(l))
		b.WriteString(`</div>`)
	}
	if inp, ok := slots["input"]; ok {
		b.WriteString(string(inp))
	}
	b.WriteString(`</section></div>`)
	return render.HTML(b.String())
}

// panelEnv carries the live + tools refs into RPC handlers and HTML
// rendering. One per widget mount.
type panelEnv struct {
	live       *live.Live
	tools      *protocol.Tools
	agentState AgentStateFn
}

// htmlComp is a render.HTML-valued Component for slot composition.
type htmlComp struct{ html string }

func (h htmlComp) Render() render.HTML { return render.HTML(h.html) }

// --- HTML rendering --------------------------------------------------

func (pe *panelEnv) headerHTML() string {
	return `<div class="kiln-panel-head">` +
		`<span class="kiln-panel-conn" title="SSE connection status" aria-label="SSE connection status"></span>` +
		`<span class="kiln-panel-title">Kiln</span>` +
		`<span class="kiln-panel-page">/</span>` +
		// When there's no agent wired, render the chip as a button
		// that opens the gear modal directly — the user has a one-
		// click path to fix it from where they noticed the problem.
		// kiln-panel-agent-none CSS already styles it distinctly.
		(func() string {
			label := pe.agentLabel()
			if label == "no agent" {
				return `<button type="button" class="kiln-panel-agent kiln-panel-agent-none" data-fui-signal="agent" data-fui-flash-on-update data-fui-open="kiln-agent-settings" title="Pick an agent">` + escHTML(label) + `</button>`
			}
			return `<span class="kiln-panel-agent" data-fui-signal="agent" data-fui-flash-on-update>` + escHTML(label) + `</span>`
		})() +
		`<a class="kiln-panel-snapshot" data-fui-signal="world_snapshot" data-fui-flash-on-update href="/kiln/world" target="_blank" rel="noopener" title="Open world IR (JSON)">` + escHTML(pe.worldSnapshotText()) + `</a>` +
		`<span class="kiln-panel-status" data-fui-signal="chat_status" data-fui-signal-mode="html"></span>` +
		`<button type="button" class="kiln-panel-stop" title="Cancel running turn" data-fui-rpc="/kiln/agent/cancel" data-fui-rpc-method="POST">■</button>` +
		`<button type="button" class="kiln-panel-config" title="Agent settings" data-fui-open="kiln-agent-settings">⚙</button>` +
		`<button type="button" id="kiln-reset" class="kiln-panel-reset" title="Reset session" data-fui-open="kiln-reset-confirm">↺</button>` +
		`<button type="button" class="kiln-panel-close" data-fui-action="close" aria-label="Close">×</button>` +
		`</div>`
}

// worldSnapshotText returns a glanceable summary of the live world:
// "3 entities · 1 page · 2 routes · 5 hooks". Singular forms collapse
// (1 entity, not 1 entities). Empty world returns "empty world" so
// the pill is never blank.
func (pe *panelEnv) worldSnapshotText() string {
	w := pe.live.Session().World
	if w == nil {
		return "empty world"
	}
	parts := []string{}
	if n := len(w.Entities); n > 0 {
		base := pluralize(n, "entity", "entities")
		if n <= 4 {
			names := make([]string, 0, n)
			for k := range w.Entities {
				names = append(names, k)
			}
			sortStrings(names)
			base += " (" + strings.Join(names, ", ") + ")"
		}
		parts = append(parts, base)
	}
	if n := len(w.Pages); n > 0 {
		parts = append(parts, pluralize(n, "page", "pages"))
	}
	if n := len(w.Routes); n > 0 {
		parts = append(parts, pluralize(n, "route", "routes"))
	}
	if n := len(w.Hooks); n > 0 {
		parts = append(parts, pluralize(n, "hook", "hooks"))
	}
	if len(parts) == 0 {
		return "empty world"
	}
	return strings.Join(parts, " · ")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// agentLabel returns the current adapter name for the header chip:
// the adapter Name (e.g. "claude-code", "pi", "custom"), or "no
// agent" when none is wired. Read from the AgentStateFn so the
// truth lives in cmd/kiln (AdapterStore) without panel knowing.
func (pe *panelEnv) agentLabel() string {
	if pe.agentState == nil {
		return "no agent"
	}
	state, _ := pe.agentState().(map[string]any)
	if state == nil {
		return "no agent"
	}
	cur, _ := state["current"].(map[string]any)
	if cur == nil {
		return "no agent"
	}
	name, _ := cur["name"].(string)
	if name == "" || name == "none" {
		return "no agent"
	}
	return name
}

// statusText returns the in-flight indicator HTML. Empty when no agent
// turn is running; an animated dots row otherwise, with a per-turn
// tool counter so the user sees real progress (e.g. 'agent thinking ·
// 5 tools'). Read from the AgentStateFn so cmd/kiln owns the truth
// without panel knowing about AdapterStore.
func (pe *panelEnv) statusText() string {
	if pe.agentState == nil {
		return ""
	}
	state, _ := pe.agentState().(map[string]any)
	if state == nil {
		return ""
	}
	inFlight, _ := state["in_flight"].(bool)
	if !inFlight {
		return ""
	}
	calls, pending := pe.toolCountsSinceLastUserMessage()
	suffix := ""
	// Live elapsed-time tick driven by data-fui-tick-elapsed (runtime
	// rewrites every 200ms). Anchored to the last user message
	// timestamp; turn-start is non-journaled (Notify only) so this is
	// the closest stable anchor with a real timestamp.
	if start := pe.lastUserMessageMillis(); start > 0 {
		suffix += fmt.Sprintf(` · <span data-fui-tick-elapsed="%d">…</span>`, start)
	}
	if calls > 0 {
		suffix += ` · ` + pluralize(calls, "tool", "tools")
		if pending > 0 {
			done := calls - pending
			suffix += fmt.Sprintf(` (%d done · %d running)`, done, pending)
		}
	}
	return `<span class="kiln-thinking" role="status" aria-live="polite">agent thinking` + suffix + `<span class="kiln-thinking-dots">…</span></span>`
}

func (pe *panelEnv) lastUserMessageMillis() int64 {
	chat := pe.live.Session().Chat
	for i := len(chat) - 1; i >= 0; i-- {
		if chat[i].Kind == journal.KindChatUser {
			return chat[i].Timestamp.UnixMilli()
		}
	}
	return 0
}

// quickstartExamples picks 3 prompts tailored to the current world
// state. Empty world → first-time onboarding suggestions. Once an
// entity exists → suggest building on it (page, hook, relation).
// Returns nil when the world is rich enough that suggestions would
// just be noise (any chat history triggers an empty list upstream).
func (pe *panelEnv) quickstartExamples() []string {
	w := pe.live.Session().World
	if w == nil || len(w.Entities) == 0 {
		return []string{
			"add an entity called notes with title (string) and body (text)",
			"build me a small blog: posts and authors with a one-to-many relation",
			"add a page at /dashboard listing all entities with row counts",
		}
	}
	// Pick the first entity (sorted) to anchor world-aware suggestions.
	names := make([]string, 0, len(w.Entities))
	for k := range w.Entities {
		names = append(names, k)
	}
	sortStrings(names)
	first := names[0]

	suggestions := []string{
		fmt.Sprintf("add a page at /%s listing all rows with edit links", first),
		fmt.Sprintf("add a before_create hook on %s that validates required fields", first),
	}
	if len(w.Entities) > 1 {
		suggestions = append(suggestions,
			fmt.Sprintf("add a relation between %s and %s", names[0], names[1]),
		)
	} else {
		suggestions = append(suggestions,
			fmt.Sprintf("add a related entity to %s, like comments or tags", first),
		)
	}
	return suggestions
}

// toolCountsSinceLastUserMessage returns (totalCalls, pendingCalls)
// in the current turn. Pending = tool_call without a matching
// tool_result yet. Drives the 'N tools (M done · K running)' split
// in the in-flight indicator.
func (pe *panelEnv) toolCountsSinceLastUserMessage() (calls, pending int) {
	chat := pe.live.Session().Chat
	lastUser := -1
	for i := len(chat) - 1; i >= 0; i-- {
		if chat[i].Kind == journal.KindChatUser {
			lastUser = i
			break
		}
	}
	resolved := map[string]bool{}
	for i := lastUser + 1; i < len(chat); i++ {
		if chat[i].Kind == journal.KindToolResult && chat[i].Result != nil {
			resolved[chat[i].Result.CallID] = true
		}
	}
	for i := lastUser + 1; i < len(chat); i++ {
		if chat[i].Kind == journal.KindToolCall && chat[i].Call != nil {
			calls++
			if !resolved[chat[i].Call.CallID] {
				pending++
			}
		}
	}
	return
}

func (pe *panelEnv) inputHTML() string {
	return `<form class="kiln-form" data-fui-rpc="/kiln/panel/send" data-fui-rpc-reset data-fui-disable-when-invalid data-fui-submit-on-enter>` +
		`<textarea class="kiln-input" name="text" placeholder="Tell the agent what to build…  (⌘K to focus · Enter to send · Esc to clear)" rows="2" autocomplete="off" required data-fui-autogrow data-fui-shortcut-focus="Mod+k" data-fui-clear-on-esc></textarea>` +
		`<button class="kiln-send" type="submit">Send <kbd class="kiln-kbd">⏎</kbd></button>` +
		`</form>`
}

// logHTMLForCurrent returns the chat log + plan cards + world_edit
// rows as HTML, ready for innerHTML insertion via the chat_html
// signal. Walks the journal so world_edits surface as synthetic
// system rows (".kiln-msg-tool") even when no tool_call envelope
// was journaled (in-process tools.X() calls fire world_edit only).
//
// First-run UX: when nothing has happened yet (empty chat + empty
// world + no plans) we render a quick-start tray with example
// prompts so users have a click-path forward instead of an empty
// box. The buttons set the textarea value via a tiny on-page hook
// (data-fui-fill-input) and focus it.
func (pe *panelEnv) logHTMLForCurrent() string {
	sess := pe.live.Session()
	var b strings.Builder

	if len(sess.Chat) == 0 && len(sess.Plans) == 0 {
		examples := pe.quickstartExamples()
		if len(examples) > 0 {
			b.WriteString(`<div class="kiln-quickstart">`)
			b.WriteString(`<div class="kiln-quickstart-label">try one of these:</div>`)
			for _, ex := range examples {
				fmt.Fprintf(&b, `<button type="button" class="kiln-quickstart-btn" data-fui-fill-input=".kiln-input">%s</button>`, escHTML(ex))
			}
			b.WriteString(`</div>`)
		}
	}

	b.WriteString(`<ol class="kiln-log">`)

	type item struct {
		ts    time.Time
		kind  string // "chat" | "plan" | "world_edit"
		chat  *journal.ChatEvent
		plan  *journal.Plan
		op    journal.Op
	}
	items := make([]item, 0, len(sess.Chat)+len(sess.Plans))
	// Index tool_results by their call_id so each tool_call row can
	// annotate elapsed time and the matching tool_result row can
	// echo the tool name.
	resultByCall := map[string]*journal.ChatEvent{}
	callByID := map[string]*journal.ChatEvent{}
	for i := range sess.Chat {
		e := sess.Chat[i]
		items = append(items, item{ts: e.Timestamp, kind: "chat", chat: &e})
		if e.Call != nil {
			callByID[e.Call.CallID] = &e
		}
		if e.Result != nil {
			resultByCall[e.Result.CallID] = &e
		}
	}
	for _, p := range sess.Plans {
		items = append(items, item{ts: p.ProposedAt, kind: "plan", plan: p})
	}
	// Pull world_edit entries from the journal so add_entity / add_page
	// etc. show as "✦ <op>" rows even when no tool_call envelope exists.
	if entries, err := pe.live.Journal().Read(); err == nil {
		for _, e := range entries {
			if e.Kind == journal.KindWorldEdit {
				items = append(items, item{ts: e.Timestamp, kind: "world_edit", op: e.Op})
			}
		}
	}
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].ts.Before(items[j-1].ts); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}

	// Find the most recent unresolved plan id; its Approve/Reject
	// buttons get Y/N keyboard shortcuts.
	var latestUnresolvedID string
	var latestUnresolvedAt time.Time
	for _, it := range items {
		if it.kind != "plan" || it.plan == nil {
			continue
		}
		if it.plan.Approved || it.plan.Rejected {
			continue
		}
		if it.plan.ProposedAt.After(latestUnresolvedAt) {
			latestUnresolvedAt = it.plan.ProposedAt
			latestUnresolvedID = it.plan.PlanID
		}
	}

	prevWasUser := false
	for i, it := range items {
		// Insert a turn divider when a new user message starts a
		// new turn (skip the very first row to avoid a leading line).
		if i > 0 && it.kind == "chat" && it.chat != nil && it.chat.Kind == journal.KindChatUser && !prevWasUser {
			b.WriteString(`<li class="kiln-turn-divider" aria-hidden="true"></li>`)
		}
		switch it.kind {
		case "chat":
			renderChatEvent(&b, it.chat, resultByCall, callByID)
		case "plan":
			renderPlanCard(&b, it.plan, it.plan.PlanID == latestUnresolvedID)
		case "world_edit":
			fmt.Fprintf(&b, `<li class="kiln-msg kiln-msg-tool">✦ %s</li>`, escHTML(string(it.op)))
		}
		prevWasUser = it.kind == "chat" && it.chat != nil && it.chat.Kind == journal.KindChatUser
	}
	b.WriteString(`</ol>`)
	return b.String()
}

func renderChatEvent(b *strings.Builder, e *journal.ChatEvent, resultByCall, callByID map[string]*journal.ChatEvent) {
	if e.Message != nil {
		role := "user"
		if e.Kind == journal.KindChatAssistant {
			role = "assistant"
		}
		page, body := splitPagePrefix(e.Message.Text)
		var pageChip string
		if page != "" {
			pageChip = fmt.Sprintf(`<span class="kiln-msg-page">%s</span>`, escHTML(page))
		}
		fmt.Fprintf(b, `<li class="kiln-msg kiln-msg-%s" title="%s">%s%s</li>`,
			role, escAttr(formatRowTime(e.Timestamp)), pageChip, escHTML(body))
		return
	}
	if e.Call != nil {
		// Pair with the matching tool_result for elapsed time + status.
		// Pending (no result yet) is shown as "(running…)" so users
		// see the in-flight state instead of a silent "→".
		var suffix string
		if r, ok := resultByCall[e.Call.CallID]; ok {
			d := r.Timestamp.Sub(e.Timestamp)
			suffix = ` <span class="kiln-msg-tool-elapsed">(` + escHTML(formatElapsed(d)) + `)</span>`
		} else {
			// Live ticker — runtime rewrites text every 200ms relative
			// to the call timestamp so pending tools surface their age.
			suffix = fmt.Sprintf(` <span class="kiln-msg-tool-elapsed kiln-msg-tool-pending">(running… <span data-fui-tick-elapsed="%d">…</span>)</span>`,
				e.Timestamp.UnixMilli())
		}
		fmt.Fprintf(b, `<li class="kiln-msg kiln-msg-tool" data-call-id="%s" data-tool="%s" title="%s">%s %s %s%s</li>`,
			escAttr(e.Call.CallID), escAttr(e.Call.Name), escAttr(formatRowTime(e.Timestamp)),
			escHTML(toolIcon(e.Call.Name)), escHTML(e.Call.Name),
			escHTML(summarizeArgs(e.Call.Args)), suffix)
		return
	}
	if e.Result != nil {
		cls := "kiln-msg-tool"
		if !e.Result.OK {
			cls = "kiln-msg-tool-error"
		}
		// Echo the tool name on the result row so a long log is
		// readable without scrolling up to find the matching call.
		var name string
		if c, ok := callByID[e.Result.CallID]; ok && c.Call != nil {
			name = c.Call.Name
		}
		var txt string
		if e.Result.OK {
			if name != "" {
				txt = "← ok · " + name
			} else {
				txt = "← ok"
			}
		} else {
			// Errors: distinct ✗ prefix so a long log scans for failures
			// without reading every word; tool name + kind + message;
			// hint appended if the protocol surfaced one (e.g.
			// 'add a propose_plan first' for destructive ops).
			if name != "" {
				txt = "✗ " + name + " · " + e.Result.Kind + ": " + e.Result.Error
			} else {
				txt = "✗ " + e.Result.Kind + ": " + e.Result.Error
			}
			if e.Result.Hint != "" {
				txt += " — " + e.Result.Hint
			}
		}
		fmt.Fprintf(b, `<li class="kiln-msg %s" data-call-id="%s" title="%s">%s</li>`,
			cls, escAttr(e.Result.CallID), escAttr(formatRowTime(e.Timestamp)), escHTML(txt))
		return
	}
}

// formatRowTime renders a timestamp the way users want to read it
// when hovering: 'Mon 15:04:05.123' for recent rows, dropping the
// weekday once it's irrelevant. Local time so it matches the wall
// clock in the user's terminal.
func formatRowTime(t time.Time) string {
	return t.Local().Format("Mon 15:04:05.000")
}

// toolIcon returns a category-distinguishing prefix for a tool name
// so a long log scans by shape, not by reading every word. Mutating
// vs read vs plan vs meta show different glyphs; unknown tools fall
// back to the neutral "→".
func toolIcon(name string) string {
	switch name {
	case "add_entity", "update_entity", "delete_entity",
		"add_field", "delete_field":
		return "▢" // entity-shape
	case "add_page", "delete_page":
		return "◇" // page-shape
	case "add_hook", "delete_hook":
		return "⌖" // hook-shape
	case "add_route", "delete_route":
		return "⚙" // route-shape
	case "add_seed":
		return "✿" // seed-shape
	case "propose_plan", "approve_plan", "reject_plan":
		return "❘" // plan-shape
	case "world_get":
		return "◈" // read-only
	case "set_app_config", "set_theme":
		return "✦" // app-meta
	case "undo", "reset_session":
		return "↶" // history meta
	case "chat":
		return "💬"
	}
	return "→"
}

func formatElapsed(d time.Duration) string {
	if d < time.Millisecond {
		return "<1ms"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < 10*time.Second {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// formatRelTime returns a brief relative-time string like '12s ago',
// '4m ago', '1h ago' — enough context for plan cards / chat rows
// to convey freshness without taking too much space.
func formatRelTime(t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		return "just now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func renderPlanCard(b *strings.Builder, p *journal.Plan, primary bool) {
	collapsed := p.Approved || p.Rejected
	cls := "kiln-msg kiln-msg-plan"
	if collapsed {
		cls += " kiln-msg-plan-collapsed"
	}
	b.WriteString(`<li class="` + cls + `" data-plan-id="` + escAttr(p.PlanID) + `">`)
	fmt.Fprintf(b, `<div class="kiln-plan-head"><span class="kiln-plan-title">Plan: %s</span>`, escHTML(p.PlanID))
	fmt.Fprintf(b, `<span class="kiln-plan-when" title="%s">proposed %s</span>`,
		escAttr(formatRowTime(p.ProposedAt)), escHTML(formatRelTime(p.ProposedAt)))
	if collapsed && len(p.Steps) > 0 {
		fmt.Fprintf(b, `<span class="kiln-plan-stepcount">(%s)</span>`, escHTML(pluralize(len(p.Steps), "step", "steps")))
	}
	if p.Reason != "" {
		fmt.Fprintf(b, `<span class="kiln-plan-reason">%s</span>`, escHTML(p.Reason))
	}
	b.WriteString(`</div>`)
	if !collapsed && len(p.Steps) > 0 {
		b.WriteString(`<ol class="kiln-plan-steps">`)
		for _, s := range p.Steps {
			fmt.Fprintf(b, `<li>%s</li>`, escHTML(s))
		}
		b.WriteString(`</ol>`)
	}
	if !collapsed && len(p.Targets) > 0 {
		b.WriteString(`<div class="kiln-plan-targets"><span class="kiln-plan-targets-label">Will run: </span>`)
		for i, t := range p.Targets {
			if i > 0 {
				b.WriteString(`, `)
			}
			cls := "kiln-plan-target"
			if strings.HasPrefix(t.Op, "delete_") {
				cls += " kiln-plan-target-destructive"
			}
			fmt.Fprintf(b, `<span class="%s">%s %s</span>`, cls, escHTML(t.Op), escHTML(t.Name))
		}
		b.WriteString(`</div>`)
	}
	switch {
	case p.Approved:
		b.WriteString(`<div class="kiln-plan-status kiln-plan-status-approved">✓ Approved</div>`)
	case p.Rejected:
		b.WriteString(`<div class="kiln-plan-status kiln-plan-status-rejected">✕ Rejected`)
		if p.RejectReason != "" {
			b.WriteString(`: ` + escHTML(p.RejectReason))
		}
		b.WriteString(`</div>`)
	default:
		// Approve / Reject / Modify. Modify pre-fills the input with
		// a refinement prompt so the user has a one-click path to
		// nudge the plan instead of binary accept/decline. The
		// most recent unresolved plan ('primary') gets Y/N keyboard
		// shortcuts on its Approve/Reject buttons + a kbd hint badge.
		approveExtra := ""
		rejectExtra := ""
		approveLabel := "Approve"
		rejectLabel := "Reject"
		if primary {
			approveExtra = ` data-fui-shortcut-click="y"`
			rejectExtra = ` data-fui-shortcut-click="n"`
			approveLabel = `Approve <kbd class="kiln-kbd">Y</kbd>`
			rejectLabel = `Reject <kbd class="kiln-kbd">N</kbd>`
		}
		fmt.Fprintf(b,
			`<div class="kiln-plan-actions">`+
				`<button type="button" class="kiln-plan-btn kiln-plan-btn-approve" `+
				`data-plan-action="approve" data-plan-id="%s" `+
				`data-fui-rpc="/kiln/panel/approve_plan"  `+
				`data-fui-rpc-body='{"plan_id":"%s"}'%s>%s</button>`+
				`<button type="button" class="kiln-plan-btn kiln-plan-btn-reject" `+
				`data-plan-action="reject" data-plan-id="%s" `+
				`data-fui-rpc="/kiln/panel/reject_plan"  `+
				`data-fui-rpc-body='{"plan_id":"%s"}'%s>%s</button>`+
				`<button type="button" class="kiln-plan-btn kiln-plan-btn-modify" `+
				`data-fui-fill-input=".kiln-input" `+
				`data-fui-fill-text="Refine plan %s: ">Modify…</button>`+
				`</div>`,
			escAttr(p.PlanID), escAttr(p.PlanID), approveExtra, approveLabel,
			escAttr(p.PlanID), escAttr(p.PlanID), rejectExtra, rejectLabel,
			escAttr(p.PlanID))
	}
	b.WriteString(`</li>`)
}

// --- RPC handlers ----------------------------------------------------

// All RPC handlers return a small JSON ack. The actual log update flows
// to the panel via the SSE refetch binding (chat_user / world_edit /
// plan_*) — that's the only path that should write to chat_html.

func (pe *panelEnv) serveSend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Text) == "" {
		// Don't 4xx on empty — that would surface as an RPC failure
		// in the runtime. Silent ack: nothing to do, no journal write.
		ack(w)
		return
	}
	pe.tools.Chat(r.Context(), protocol.ChatArgs{Role: "user", Text: body.Text})
	ack(w)
}

func (pe *panelEnv) serveReset(w http.ResponseWriter, r *http.Request) {
	pe.tools.ResetSession(r.Context(), protocol.ResetSessionArgs{})
	ack(w)
}

func (pe *panelEnv) serveApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID string `json:"plan_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	pe.tools.ApprovePlan(r.Context(), protocol.ApprovePlanArgs{PlanID: body.PlanID})
	ack(w)
}

func (pe *panelEnv) serveReject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID string `json:"plan_id"`
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	pe.tools.RejectPlan(r.Context(), protocol.RejectPlanArgs{PlanID: body.PlanID, Reason: body.Reason})
	ack(w)
}

func (pe *panelEnv) serveUndo(w http.ResponseWriter, _ *http.Request) {
	pe.tools.Undo(context.Background(), protocol.UndoArgs{})
	ack(w)
}

func ack(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// --- helpers ---------------------------------------------------------

func summarizeArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	if ent, ok := args["entity"].(map[string]any); ok {
		name, _ := ent["name"].(string)
		fields, _ := ent["fields"].([]any)
		return "name=" + name + " fields=" + itoa(len(fields))
	}
	if page, ok := args["page"].(map[string]any); ok {
		path, _ := page["path"].(string)
		return "path=" + path
	}
	if route, ok := args["route"].(map[string]any); ok {
		method, _ := route["method"].(string)
		path, _ := route["path"].(string)
		return method + " " + path
	}
	if hook, ok := args["hook"].(map[string]any); ok {
		id, _ := hook["id"].(string)
		entity, _ := hook["entity"].(string)
		when, _ := hook["when"].(string)
		return "id=" + id + " " + entity + "/" + when
	}
	if seed, ok := args["seed"].(map[string]any); ok {
		entity, _ := seed["entity"].(string)
		rows, _ := seed["rows"].([]any)
		return "entity=" + entity + " rows=" + itoa(len(rows))
	}
	// fallback: short JSON
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(args)
	s := strings.TrimSpace(buf.String())
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

func stripPagePrefix(s string) string {
	page, rest := splitPagePrefix(s)
	if page == "" {
		return s
	}
	return rest
}

// splitPagePrefix peels the "[page=/foo] " context header that the
// widget prepends so the panel can render the page as a chip alongside
// the message text. Returns ("", s) when no prefix is present.
func splitPagePrefix(s string) (page, rest string) {
	if !strings.HasPrefix(s, "[page=") {
		return "", s
	}
	end := strings.Index(s, "] ")
	if end <= 0 {
		return "", s
	}
	body := s[len("[page="):end]
	if i := strings.Index(body, " "); i >= 0 {
		// "[page=/x ?q=1]" — drop the query suffix from the chip text.
		body = body[:i]
	}
	return body, s[end+2:]
}

func escHTML(s string) string {
	r := strings.NewReplacer(`&`, `&amp;`, `<`, `&lt;`, `>`, `&gt;`)
	return r.Replace(s)
}

func escAttr(s string) string {
	r := strings.NewReplacer(`"`, `&quot;`, `&`, `&amp;`, `<`, `&lt;`, `>`, `&gt;`)
	return r.Replace(s)
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
