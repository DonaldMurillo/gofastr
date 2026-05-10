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

// MountPanel registers the kiln chat panel as a core-ui/widget on r.
// The widget is a framework-managed FloatingPanel; kiln contributes:
//   - slot HTML for header / log / input (rendered server-side from session)
//   - SSE bindings that push fresh log HTML into the chat_html signal
//   - RPC handlers for chat send, reset, approve/reject, agent control
//
// All bespoke kiln widget plumbing (vanilla JS DOM, hand-rolled CSS,
// per-route HTTP handlers) is replaced by this single declaration.
func MountPanel(r *router.Router, l *live.Live, tools *protocol.Tools) {
	pe := &panelEnv{live: l, tools: tools}

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
		// Page-affecting world edits: full reload so the now-rendered
		// page reflects the new world. Filtered by op so add_entity
		// (which doesn't change page rendering) doesn't trigger reloads.
		SSEReload("/.kiln/events", "world_edit", "op", "add_page").
		SSEReload("/.kiln/events", "world_edit", "op", "delete_page").
		SSEReload("/.kiln/events", "world_edit", "op", "add_route").
		SSEReload("/.kiln/events", "world_edit", "op", "delete_route").
		// RPC: chat send, reset, approve/reject, undo.
		RPCWithSignal("POST", "/kiln/panel/send", http.HandlerFunc(pe.serveSend), "chat_html").
		RPCWithSignal("POST", "/kiln/panel/reset", http.HandlerFunc(pe.serveReset), "chat_html").
		RPCWithSignal("POST", "/kiln/panel/approve_plan", http.HandlerFunc(pe.serveApprove), "chat_html").
		RPCWithSignal("POST", "/kiln/panel/reject_plan", http.HandlerFunc(pe.serveReject), "chat_html").
		RPC("POST", "/kiln/panel/undo", http.HandlerFunc(pe.serveUndo)).
		Build()

	def.ExtraCSS = widgetCSS // appends panel content CSS after framework chrome
	widget.Mount(r, &def)
	widget.MountRuntime(r) // idempotent — the framework runtime URL goes here
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
		b.WriteString(`<div class="kiln-log-wrap" data-fui-signal="chat_html" data-fui-signal-mode="html">`)
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
	live  *live.Live
	tools *protocol.Tools
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
		`<button type="button" class="kiln-panel-config" title="Agent settings" data-fui-action="config">⚙</button>` +
		`<button type="button" id="kiln-reset" class="kiln-panel-reset" title="Reset session" data-fui-rpc="/kiln/panel/reset" data-fui-rpc-signal="chat_html">↺</button>` +
		`<button type="button" class="kiln-panel-close" data-fui-action="close" aria-label="Close">×</button>` +
		`</div>`
}

func (pe *panelEnv) inputHTML() string {
	return `<form class="kiln-form" data-fui-rpc="/kiln/panel/send" data-fui-rpc-signal="chat_html">` +
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
				`data-fui-rpc="/kiln/panel/approve_plan" data-fui-rpc-signal="chat_html" `+
				`data-fui-rpc-body='{"plan_id":"%s"}'>Approve</button>`+
				`<button type="button" class="kiln-plan-btn kiln-plan-btn-reject" `+
				`data-plan-action="reject" data-plan-id="%s" `+
				`data-fui-rpc="/kiln/panel/reject_plan" data-fui-rpc-signal="chat_html" `+
				`data-fui-rpc-body='{"plan_id":"%s"}'>Reject</button>`+
				`</div>`,
			escAttr(p.PlanID), escAttr(p.PlanID), escAttr(p.PlanID), escAttr(p.PlanID))
	}
	b.WriteString(`</li>`)
}

// --- RPC handlers ----------------------------------------------------

func (pe *panelEnv) serveSend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Text) == "" {
		http.Error(w, "empty", http.StatusBadRequest)
		return
	}
	pe.tools.Chat(r.Context(), protocol.ChatArgs{Role: "user", Text: body.Text})
	pe.respondHTML(w, pe.logHTMLForCurrent())
}

func (pe *panelEnv) serveReset(w http.ResponseWriter, r *http.Request) {
	pe.tools.ResetSession(r.Context(), protocol.ResetSessionArgs{})
	pe.respondHTML(w, pe.logHTMLForCurrent())
}

func (pe *panelEnv) serveApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID string `json:"plan_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	pe.tools.ApprovePlan(r.Context(), protocol.ApprovePlanArgs{PlanID: body.PlanID})
	pe.respondHTML(w, pe.logHTMLForCurrent())
}

func (pe *panelEnv) serveReject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID string `json:"plan_id"`
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	pe.tools.RejectPlan(r.Context(), protocol.RejectPlanArgs{PlanID: body.PlanID, Reason: body.Reason})
	pe.respondHTML(w, pe.logHTMLForCurrent())
}

func (pe *panelEnv) serveUndo(w http.ResponseWriter, _ *http.Request) {
	pe.tools.Undo(context.Background(), protocol.UndoArgs{})
	w.WriteHeader(http.StatusOK)
}

// respondHTML writes the html string as the JSON response body so the
// fui runtime's data-fui-rpc-signal binding (which json-decodes) puts
// the html into the named signal.
func (pe *panelEnv) respondHTML(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(html)
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
