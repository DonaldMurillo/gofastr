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
			// Filled in by the host (cmd/kiln) via SSE binding to "agent_changed".
			return "", nil
		})).
		Signal("chat_status", widget.SignalFunc(func() (any, error) {
			return pe.statusText(), nil
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

	widget.MountRuntime(r) // idempotent — the framework runtime URL goes here
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
		`<button type="button" class="kiln-modal-cancel" data-fui-action="close">Close</button>` +
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
		b.WriteString(`<div class="kiln-log-wrap" data-fui-signal="chat_html" data-fui-signal-mode="html" data-fui-scroll-bottom-on-update=".kiln-log">`)
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
		`<span class="kiln-panel-title">Kiln</span>` +
		`<span class="kiln-panel-page">/</span>` +
		`<span class="kiln-panel-agent" data-fui-signal="agent">no agent</span>` +
		`<span class="kiln-panel-status" data-fui-signal="chat_status" data-fui-signal-mode="html"></span>` +
		`<button type="button" class="kiln-panel-config" title="Agent settings" data-fui-open="kiln-agent-settings">⚙</button>` +
		`<button type="button" id="kiln-reset" class="kiln-panel-reset" title="Reset session" data-fui-rpc="/kiln/panel/reset" >↺</button>` +
		`<button type="button" class="kiln-panel-close" data-fui-action="close" aria-label="Close">×</button>` +
		`</div>`
}

// statusText returns the in-flight indicator HTML. Empty when no agent
// turn is running; an animated dots row otherwise. Read from the
// AgentStateFn so cmd/kiln owns the truth without panel knowing about
// AdapterStore.
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
	return `<span class="kiln-thinking" role="status" aria-live="polite">agent thinking<span class="kiln-thinking-dots">…</span></span>`
}

func (pe *panelEnv) inputHTML() string {
	return `<form class="kiln-form" data-fui-rpc="/kiln/panel/send" data-fui-rpc-reset>` +
		`<textarea class="kiln-input" name="text" placeholder="Tell the agent what to build…" rows="2" autocomplete="off"></textarea>` +
		`<button class="kiln-send" type="submit">Send</button>` +
		`</form>`
}

// logHTMLForCurrent returns the chat log + plan cards + world_edit
// rows as HTML, ready for innerHTML insertion via the chat_html
// signal. Walks the journal so world_edits surface as synthetic
// system rows (".kiln-msg-tool") even when no tool_call envelope
// was journaled (in-process tools.X() calls fire world_edit only).
func (pe *panelEnv) logHTMLForCurrent() string {
	sess := pe.live.Session()
	var b strings.Builder
	b.WriteString(`<ol class="kiln-log">`)

	type item struct {
		ts    time.Time
		kind  string // "chat" | "plan" | "world_edit"
		chat  *journal.ChatEvent
		plan  *journal.Plan
		op    journal.Op
	}
	items := make([]item, 0, len(sess.Chat)+len(sess.Plans))
	for i := range sess.Chat {
		e := sess.Chat[i]
		items = append(items, item{ts: e.Timestamp, kind: "chat", chat: &e})
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

	for _, it := range items {
		switch it.kind {
		case "chat":
			renderChatEvent(&b, it.chat)
		case "plan":
			renderPlanCard(&b, it.plan)
		case "world_edit":
			fmt.Fprintf(&b, `<li class="kiln-msg kiln-msg-tool">✦ %s</li>`, escHTML(string(it.op)))
		}
	}
	b.WriteString(`</ol>`)
	return b.String()
}

func renderChatEvent(b *strings.Builder, e *journal.ChatEvent) {
	if e.Message != nil {
		role := "user"
		if e.Kind == journal.KindChatAssistant {
			role = "assistant"
		}
		fmt.Fprintf(b, `<li class="kiln-msg kiln-msg-%s">%s</li>`, role, escHTML(stripPagePrefix(e.Message.Text)))
		return
	}
	if e.Call != nil {
		fmt.Fprintf(b, `<li class="kiln-msg kiln-msg-tool">→ %s %s</li>`,
			escHTML(e.Call.Name), escHTML(summarizeArgs(e.Call.Args)))
		return
	}
	if e.Result != nil {
		cls := "kiln-msg-tool"
		if !e.Result.OK {
			cls = "kiln-msg-tool-error"
		}
		txt := "← ok"
		if !e.Result.OK {
			txt = "← " + e.Result.Kind + ": " + e.Result.Error
		}
		fmt.Fprintf(b, `<li class="kiln-msg %s">%s</li>`, cls, escHTML(txt))
		return
	}
}

func renderPlanCard(b *strings.Builder, p *journal.Plan) {
	b.WriteString(`<li class="kiln-msg kiln-msg-plan" data-plan-id="` + escAttr(p.PlanID) + `">`)
	fmt.Fprintf(b, `<div class="kiln-plan-head"><span class="kiln-plan-title">Plan: %s</span>`, escHTML(p.PlanID))
	if p.Reason != "" {
		fmt.Fprintf(b, `<span class="kiln-plan-reason">%s</span>`, escHTML(p.Reason))
	}
	b.WriteString(`</div>`)
	if len(p.Steps) > 0 {
		b.WriteString(`<ol class="kiln-plan-steps">`)
		for _, s := range p.Steps {
			fmt.Fprintf(b, `<li>%s</li>`, escHTML(s))
		}
		b.WriteString(`</ol>`)
	}
	if len(p.Targets) > 0 {
		b.WriteString(`<div class="kiln-plan-targets"><span class="kiln-plan-targets-label">Will run: </span>`)
		parts := make([]string, 0, len(p.Targets))
		for _, t := range p.Targets {
			parts = append(parts, t.Op+" "+t.Name)
		}
		b.WriteString(escHTML(strings.Join(parts, ", ")))
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
		fmt.Fprintf(b,
			`<div class="kiln-plan-actions">`+
				`<button type="button" class="kiln-plan-btn kiln-plan-btn-approve" `+
				`data-plan-action="approve" data-plan-id="%s" `+
				`data-fui-rpc="/kiln/panel/approve_plan"  `+
				`data-fui-rpc-body='{"plan_id":"%s"}'>Approve</button>`+
				`<button type="button" class="kiln-plan-btn kiln-plan-btn-reject" `+
				`data-plan-action="reject" data-plan-id="%s" `+
				`data-fui-rpc="/kiln/panel/reject_plan"  `+
				`data-fui-rpc-body='{"plan_id":"%s"}'>Reject</button>`+
				`</div>`,
			escAttr(p.PlanID), escAttr(p.PlanID), escAttr(p.PlanID), escAttr(p.PlanID))
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
	if !strings.HasPrefix(s, "[page=") {
		return s
	}
	if i := strings.Index(s, "] "); i > 0 {
		return s[i+2:]
	}
	return s
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
