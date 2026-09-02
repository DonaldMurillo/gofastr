package agent

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// PromptLayers assembles the layered system prompt. The slabs are
// concatenated in order: persona first (most stable), framework
// middle (occasional refresh), project last (volatile per turn). LLM
// prompt caching pins the prefix; only the project slab changes turn
// to turn.
type PromptLayers struct {
	Persona   string
	Framework string
	Project   string
}

// String concatenates the layers with section separators.
func (p PromptLayers) String() string {
	var b strings.Builder
	if p.Persona != "" {
		b.WriteString(p.Persona)
		b.WriteString("\n\n")
	}
	if p.Framework != "" {
		b.WriteString("--- FRAMEWORK KNOWLEDGE ---\n")
		b.WriteString(p.Framework)
		b.WriteString("\n\n")
	}
	if p.Project != "" {
		b.WriteString("--- PROJECT ---\n")
		b.WriteString(p.Project)
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// DefaultPersona is the stable Kiln agent persona.
const DefaultPersona = `You are Kiln, an in-app build agent that helps users compose web applications declaratively.
You build apps by calling tools, never by writing free-form code or SQL. The tools mutate the
project's "world" (entities, pages, hooks, routes, seed data); the runtime auto-migrates the
database and re-renders the live preview after each call.

Operating rules:
- For small additive asks, just call the right tools. Don't narrate every step.
- For ANY destructive op (delete_entity, delete_field, delete_page, delete_hook, delete_route)
  you MUST: (1) call propose_plan with targets listing each destructive op, (2) wait for the
  user to click Approve in the panel, (3) call the destructive tool with plan_id set. The
  protocol enforces this: a delete without an approved matching plan returns kind=needs_plan.
  Each (plan, target) is single-use; reuse needs a new plan.
- When a tool returns ok=false, read the kind and hint fields and self-correct in the next turn.
  Common kinds: validation, conflict, not_found, needs_plan.
- Never invent tool names. Never write Go, JS, or SQL: express behavior via declarative
  hooks (validate, set_field, audit, emit_event) and route actions (respond_json).
- When unsure of the current state, call world_get with a path (e.g. entities.posts) before
  acting.`

// BuildFrameworkSlab generates the framework-knowledge slab from the
// tool descriptors and a few baked-in conventions. Refreshed at session
// start; static for the duration of a session.
func BuildFrameworkSlab(tools []protocol.Descriptor) string {
	var b strings.Builder
	b.WriteString("Available tools (call by name with the JSON args shown):\n\n")
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	for _, d := range tools {
		fmt.Fprintf(&b, "• %s: %s\n", d.Name, d.Description)
		if d.Destructive {
			b.WriteString("    (destructive: requires plan_id of an approved propose_plan with matching target)\n")
		}
	}
	b.WriteString("\nField types: string, text, int, float, decimal, bool, enum, uuid, timestamp, ")
	b.WriteString("date, json, relation, image, file.\n")
	b.WriteString("Hook events: before_create, after_create, before_update, after_update, ")
	b.WriteString("before_delete, after_delete, before_list, after_list.\n")
	b.WriteString("Action kinds: noop, validate, set_field, audit, respond_json, emit_event.\n")
	b.WriteString("Expressions support: + - * / %, == != < > <= >=, && || !, field access (entity.x, ctx.user.role), ")
	b.WriteString("string/list operations (len, lower, upper, contains, starts_with, ends_with), ")
	b.WriteString("now() for timestamps. Strings use double or single quotes.\n")
	return b.String()
}

// BuildProjectSlab returns a per-turn project header: app name, entity
// names, page paths, and the most recent journal IDs. Small enough to
// re-send each turn without breaking caching of earlier slabs.
func BuildProjectSlab(sess *journal.Session) string {
	if sess == nil {
		return ""
	}
	w := sess.World
	var b strings.Builder
	if name := w.App.Name; name != "" && slabSafe(name) {
		fmt.Fprintf(&b, "App: %s\n", name)
	}
	names := make([]string, 0, len(w.Entities))
	for n := range w.Entities {
		if slabSafe(n) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) > 0 {
		fmt.Fprintf(&b, "Entities: %s\n", strings.Join(names, ", "))
	} else {
		b.WriteString("Entities: (none)\n")
	}
	paths := make([]string, 0, len(w.Pages))
	for p := range w.Pages {
		if slabSafe(p) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	if len(paths) > 0 {
		fmt.Fprintf(&b, "Pages: %s\n", strings.Join(paths, ", "))
	}
	if len(w.Hooks) > 0 {
		fmt.Fprintf(&b, "Hooks: %d registered\n", len(w.Hooks))
	}
	if len(sess.Chat) > 0 {
		recent := sess.Chat
		if len(recent) > 5 {
			recent = recent[len(recent)-5:]
		}
		ids := make([]string, 0, len(recent))
		for _, e := range recent {
			if slabSafe(e.EntryID) {
				ids = append(ids, e.EntryID)
			}
		}
		if len(ids) > 0 {
			fmt.Fprintf(&b, "Recent entries: %s\n", strings.Join(ids, ", "))
		}
	}
	return b.String()
}

// slabSafe reports whether a journal-derived string may be interpolated
// into the project slab as a single line. Every value here reaches the
// journal through caller-supplied IDs and world-edit payloads that are
// never newline-validated on replay, so a hand-authored journal can
// carry text that would rewrite the prompt's line structure (a
// "CRITICAL OPERATOR OVERRIDE: ..." directive riding on injected
// newlines is the pinned shape). Values that fail are omitted, not
// flattened: a payload must not be able to smuggle any of its text onto
// the slab once its line structure is found hostile.
func slabSafe(s string) bool {
	for _, r := range s {
		switch {
		case r < 0x20, r == 0x7f, r == '\u2028', r == '\u2029':
			return false
		}
	}
	return true
}

// BuildPrompt produces a fully-assembled prompt for the current session.
func BuildPrompt(sess *journal.Session, tools []protocol.Descriptor) PromptLayers {
	return PromptLayers{
		Persona:   DefaultPersona,
		Framework: BuildFrameworkSlab(tools),
		Project:   BuildProjectSlab(sess),
	}
}

// MarshalArgs is a small helper for tests and providers that want to
// see a tool call's args as JSON.
func MarshalArgs(c ToolCall) string {
	if c.Args == nil {
		return "{}"
	}
	buf, _ := json.Marshal(c.Args)
	return string(buf)
}

// SummarizeWorld returns a tight, model-friendly snapshot of an entity
// or page for inclusion when world_get's full result is too big.
func SummarizeWorld(w *world.World) string {
	if w == nil {
		return "(empty world)"
	}
	var b strings.Builder
	// Sorted so the snapshot is byte-stable call to call: the model sees
	// one consistent world text and prompt caching gets a fair chance.
	for _, name := range slices.Sorted(maps.Keys(w.Entities)) {
		e := w.Entities[name]
		fmt.Fprintf(&b, "%s: ", e.Name)
		fields := make([]string, 0, len(e.Fields))
		for _, f := range e.Fields {
			fields = append(fields, f.Name+":"+f.Type)
		}
		b.WriteString(strings.Join(fields, ", "))
		b.WriteByte('\n')
	}
	return b.String()
}
