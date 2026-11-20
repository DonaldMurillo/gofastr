// Package node is the JSON-clean UI element tree core-ui/noderender renders.
package node

// Node is one element of a UI tree; ID is a handle the renderer ignores.
type Node struct {
	ID       string            `json:"_id,omitempty"`
	Kind     string            `json:"kind"`
	Props    map[string]any    `json:"props,omitempty"`
	Bindings map[string]string `json:"bindings,omitempty"`
	Actions  map[string]Action `json:"actions,omitempty"`
	Children []Node            `json:"children,omitempty"`
}

// Action is a declarative effect: a Kind from the verb catalog and its Params.
type Action struct {
	Kind   string         `json:"kind"`
	Params map[string]any `json:"params,omitempty"`
}

// Known Action kinds.
const (
	ActionNoop        = "noop"
	ActionSetField    = "set_field"    // params: {field, value}
	ActionValidate    = "validate"     // params: {expression, message}
	ActionAudit       = "audit"        // params: {channel, message}
	ActionRespondJSON = "respond_json" // params: {status, body}
	ActionEmitEvent   = "emit_event"   // params: {topic, data}
)
