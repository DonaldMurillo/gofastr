package uinodev1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Validate decodes, type-checks, and bounds-checks a ui.node.v1 JSON tree.
// It returns the typed [Tree] on success, or a descriptive error describing
// the first failure encountered. On any failure the whole tree is rejected
// — Validate never returns a partial tree and never truncates content.
//
// All whole-tree caps ([Limits]) are enforced: input bytes are bounded
// before any decode allocation; depth, node count, and per-node children
// are counted during the single recursive walk; per-prop strings and total
// text are accumulated during the same walk; URL props and ActionRef
// values are checked per-component. See [Limits] for the default cap
// values and how to override them.
//
// data MUST be a single JSON object (the tree root). null, arrays,
// scalars, empty input, or input with trailing garbage after the root
// object are all rejected.
func Validate(data []byte, lim Limits) (*Tree, error) {
	lim = lim.withDefaults()

	// 1. Hard cap on raw input — defeats memory bombs before any parse.
	if len(data) > lim.MaxInputBytes {
		return nil, errCapExceeded("input bytes", len(data), lim.MaxInputBytes)
	}

	// 2. Trim surrounding whitespace (a polite allowance — JSON spec
	//    permits it). Reject empty input.
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, errEmpty()
	}
	if trimmed[0] != '{' {
		return nil, errRootNotObject()
	}

	// 3. Reject duplicate JSON keys anywhere in the document. Go's
	//    encoding/json silently uses the last value on duplicate keys,
	//    which is a smuggling vector: an attacker could write
	//    `{"component":"heading","component":"script",...}` and rely on
	//    Go picking the second. We reject that here, before any decode.
	if err := rejectDuplicateKeys(trimmed); err != nil {
		return nil, err
	}

	// 4. Single recursive walk: decode + caps + per-component validation.
	//    The walk enforces depth/nodes/children/text as it goes and
	//    returns the first failure.
	w := &walker{lim: lim}
	root, err := w.decodeNode(trimmed, 1)
	if err != nil {
		return nil, err
	}
	// 5. Reject trailing bytes after the root object. decodeNode consumes
	//    exactly one JSON value via json.Decoder; this is belt-and-
	//    suspenders against malformed framing.
	if hasTrailingBytes(trimmed, root.consumed) {
		return nil, errTrailingJSON()
	}

	return &Tree{Root: root.node}, nil
}

// decodedNode carries a node plus the byte length its JSON value consumed,
// so Validate can detect trailing garbage.
type decodedNode struct {
	node     Node
	consumed int
}

// walker carries the limits and the running counters for one Validate call.
// It is not safe for concurrent reuse — each Validate call mints its own.
type walker struct {
	lim       Limits
	nodes     int
	totalText int
}

// decodeNode decodes a single JSON object into a Node, enforcing every cap
// at the earliest possible point. depth is 1 at the root.
//
// The decode strategy is two-phase per node:
//  1. Decode into a shadow struct using json.Decoder.DisallowUnknownFields,
//     capturing props as a json.RawMessage so we don't materialize
//     attacker-shaped prop structures before we know the component.
//  2. Look up the component's typed prop decoder, then decode props into
//     the right struct with DisallowUnknownFields (rejects data-fui-* and
//     every other unknown prop key).
//
// Children are decoded as []json.RawMessage, then recursed on, so the
// depth/node-count caps can abort the walk without ever decoding a
// child's props.
func (w *walker) decodeNode(data []byte, depth int) (decodedNode, error) {
	if depth > w.lim.MaxDepth {
		return decodedNode{}, errCapExceeded("depth", depth, w.lim.MaxDepth)
	}
	w.nodes++
	if w.nodes > w.lim.MaxNodes {
		return decodedNode{}, errCapExceeded("node count", w.nodes, w.lim.MaxNodes)
	}

	// Null nodes are explicitly rejected. A bare "null" decodes cleanly
	// into a zero-valued struct, which would silently pass component
	// checks as an empty component — we must catch it.
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return decodedNode{}, errNullNode()
	}

	// Shadow decode with DisallowUnknownFields so any key outside
	// {component, props, children, action_ref} rejects the whole tree.
	// That makes Bindings/Actions/_id/etc. unrepresentable at the node
	// level — they are not in the shadow struct, so DisallowUnknownFields
	// fires.
	var shadow struct {
		Component string            `json:"component"`
		Props     json.RawMessage   `json:"props"`
		Children  []json.RawMessage `json:"children"`
		ActionRef string            `json:"action_ref"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&shadow); err != nil {
		return decodedNode{}, errDecode(err)
	}

	// Track how many bytes this object consumed, for trailing-byte
	// detection at the top level. For non-root depths we don't need it.
	consumed := int(dec.InputOffset())
	if consumed == 0 {
		consumed = len(data)
	}

	comp := Component(shadow.Component)
	propDecoder, ok := componentDecoders[comp]
	if !ok {
		return decodedNode{}, errUnknownComponent(shadow.Component)
	}

	// Decode props into the typed struct, or use a zero-value if absent.
	// DisallowUnknownFields here is the load-bearing control: a key like
	// "data-fui-rpc" or "onclick" is not a field of any prop struct, so
	// the decoder rejects it. This is the core repair for the noderender
	// extraAttrs denylist breach (design §9).
	props, err := decodeProps(propDecoder, shadow.Props)
	if err != nil {
		return decodedNode{}, errDecode(err)
	}

	// Per-component invariants (URLs, ranges, enum values, string caps).
	if err := props.validate(w.lim); err != nil {
		return decodedNode{}, err
	}

	// Total-text cap: accumulate the node's text contribution and reject
	// on overflow. We check BEFORE recursing into children so a fat leaf
	// fails fast.
	w.totalText += props.estimatedTextSize()
	if w.totalText > w.lim.MaxTotalText {
		return decodedNode{}, errCapExceeded("total text", w.totalText, w.lim.MaxTotalText)
	}

	// Action reference shape + placement.
	if shadow.ActionRef != "" {
		if err := validateActionRef(shadow.ActionRef, w.lim); err != nil {
			return decodedNode{}, err
		}
		w.totalText += len(shadow.ActionRef)
		if w.totalText > w.lim.MaxTotalText {
			return decodedNode{}, errCapExceeded("total text", w.totalText, w.lim.MaxTotalText)
		}
	}

	// Per-node child cap — checked before recursion so a children bomb
	// never allocates child structs.
	if len(shadow.Children) > w.lim.MaxChildrenPerNode {
		return decodedNode{}, errCapExceeded("children per node", len(shadow.Children), w.lim.MaxChildrenPerNode)
	}

	// Per-component child policy + interactive-component rules.
	if err := enforceComponentRules(comp, props, shadow.ActionRef, shadow.Children); err != nil {
		return decodedNode{}, err
	}

	// Recurse on children.
	children := make([]Node, len(shadow.Children))
	for i, raw := range shadow.Children {
		child, err := w.decodeNode(raw, depth+1)
		if err != nil {
			return decodedNode{}, err
		}
		children[i] = child.node
	}

	return decodedNode{
		node: Node{
			Component: comp,
			Props:     props,
			Children:  children,
			ActionRef: shadow.ActionRef,
		},
		consumed: consumed,
	}, nil
}

// decodeProps dispatches to the component's typed prop decoder. If raw is
// empty (the "props" key was absent), it decodes "{}" so a component
// whose props are all optional still gets a valid zero-value struct.
func decodeProps(decode func(json.RawMessage) (Props, error), raw json.RawMessage) (Props, error) {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	return decode(raw)
}

// validateActionRef checks the shape of an ActionRef string. Resolution
// against the descriptor's installed routes is the host renderer's job.
func validateActionRef(s string, lim Limits) error {
	if s == "" {
		return errActionRefShape("empty action_ref")
	}
	if len(s) > lim.MaxActionRefLen {
		return errActionRefShape(fmt.Sprintf("action_ref length %d exceeds max %d", len(s), lim.MaxActionRefLen))
	}
	for i := range s {
		b := s[i]
		if b <= 0x20 || b == 0x7F {
			return errActionRefShape("action_ref contains whitespace or control bytes")
		}
	}
	return nil
}

// enforceComponentRules applies the per-component constraints that the
// prop struct cannot express on its own:
//   - child policy (text/data/interactive components reject children),
//   - interactive-component rules (button needs an action_ref; link needs
//     exactly one of to/action_ref; non-interactive components reject
//     action_ref).
//
// The per-node child COUNT cap (Limits.MaxChildrenPerNode) is enforced
// in decodeNode before recursion — this helper only handles policy +
// interactive rules.
func enforceComponentRules(comp Component, props Props, actionRef string, children []json.RawMessage) error {
	if props.childPolicy() == childPolicyNone && len(children) > 0 {
		return errChildPolicy(comp)
	}
	switch comp {
	case CompButton:
		if actionRef == "" {
			return errButtonNeedsActionRef()
		}
	case CompLink:
		to := props.(LinkProps).To
		if (to == "" && actionRef == "") || (to != "" && actionRef != "") {
			return errLinkNeedsToOrActionRef()
		}
	default:
		if actionRef != "" {
			return errActionRefOnWrongComponent(comp)
		}
	}
	return nil
}

// defaultCapForChildren is a placeholder that returns the per-node child
// cap lookup; the actual cap is enforced in decodeNode via walker.lim.
// (Kept as a named helper so the rule is discoverable.)
func defaultCapForChildren(Props) int { return DefaultMaxChildrenPerNode }

// hasTrailingBytes reports whether trimmed has non-whitespace bytes
// beyond the first consumed JSON value.
func hasTrailingBytes(trimmed []byte, consumed int) bool {
	if consumed <= 0 || consumed >= len(trimmed) {
		return false
	}
	for i := consumed; i < len(trimmed); i++ {
		b := trimmed[i]
		if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
			return true
		}
	}
	return false
}

// --- duplicate-key detection --------------------------------------------

// rejectDuplicateKeys walks the entire JSON document and rejects any
// object that contains a duplicate key. Go's encoding/json silently
// uses the LAST value on duplicates, which is a smuggling vector
// (e.g. {"component":"heading","component":"script",...}); this pass
// makes duplicates a hard error before any structural decode runs.
func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := walkDupCheck(dec); err != nil {
		return err
	}
	// Reject any trailing non-whitespace after the single root value.
	if dec.More() {
		return errTrailingJSON()
	}
	return nil
}

// walkDupCheck descends into one JSON value (object, array, or scalar).
func walkDupCheck(dec *json.Decoder) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		// Scalar (string/number/bool/null) — nothing to recurse into.
		return nil
	}
	switch delim {
	case '{':
		return walkObjectDupCheck(dec)
	case '[':
		for dec.More() {
			if err := walkDupCheck(dec); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
	}
	return nil
}

// walkObjectDupCheck reads an object body (after the opening '{') and
// rejects duplicate keys.
func walkObjectDupCheck(dec *json.Decoder) error {
	seen := make(map[string]struct{})
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := t.(string)
		if !ok {
			return errors.New("uinodev1: decode: object key is not a string")
		}
		if _, dup := seen[key]; dup {
			return errDupKey(key)
		}
		seen[key] = struct{}{}
		if err := walkDupCheck(dec); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	return nil
}
