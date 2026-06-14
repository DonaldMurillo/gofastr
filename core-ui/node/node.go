// Package node is the JSON-clean UI element tree — the serializable
// description of a screen that renders to HTML via core-ui/noderender.
//
// It is a first-party UI primitive. Both the blueprint codegen
// (cmd/gofastr) and Kiln's World IR (kiln/world) compose it; neither owns
// it. The package is deliberately dependency-free (stdlib only) so any
// layer can describe a node tree without dragging in a renderer or the
// Kiln authoring engine.
package node

// Node is a single element in a UI tree. The Kind discriminates between
// built-in elements ("div", "button", "heading", …) and named components
// ("component:<name>"). Props feed element configuration; Bindings express
// signal-driven values via expressions; Actions wire events to declarative
// effects.
//
// ID is a stable per-element handle. Authoring tools (e.g. Kiln) reference
// it to address the exact element they want to mutate, rather than
// positional tree paths (which break when siblings shift) or selector
// queries (which can be ambiguous). The renderer ignores ID — it's pure
// metadata.
type Node struct {
	ID       string            `json:"_id,omitempty"`
	Kind     string            `json:"kind"`
	Props    map[string]any    `json:"props,omitempty"`
	Bindings map[string]string `json:"bindings,omitempty"`
	Actions  map[string]Action `json:"actions,omitempty"`
	Children []Node            `json:"children,omitempty"`
}

// Action is the canonical declarative effect type. The Kind selects from a
// closed verb catalog; Params is verb-specific. Treating actions as data —
// never Go source — is what lets consumers (journals, codegen, MCP tool
// surfaces) round-trip them losslessly.
type Action struct {
	Kind   string         `json:"kind"`
	Params map[string]any `json:"params,omitempty"`
}

// Known Action kinds. Evaluators validate Kind+Params shape; serialization
// only requires that they round-trip through JSON unchanged.
const (
	ActionNoop         = "noop"
	ActionSetField     = "set_field"     // params: {field, value}
	ActionValidate     = "validate"      // params: {expression, message}
	ActionAudit        = "audit"         // params: {channel, message}
	ActionCreateEntity = "create_entity" // params: {entity, data}
	ActionRespondJSON  = "respond_json"  // params: {status, body}
	ActionRespondQuery = "respond_query" // params: {query}
	ActionEmitEvent    = "emit_event"    // params: {topic, data}
)
