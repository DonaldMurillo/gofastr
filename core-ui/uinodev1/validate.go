package uinodev1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

func checkPropItems(lim Limits, field string, count int) error {
	if count > lim.MaxPropItems {
		return errCapExceeded(field+" items", count, lim.MaxPropItems)
	}
	return nil
}

// Validate decodes and checks a ui.node.v1 tree, returning it typed or
// refusing the whole tree with the first failure.
func Validate(data []byte, lim Limits) (*Tree, error) {
	lim = lim.withDefaults()

	if len(data) > lim.MaxInputBytes {
		return nil, errCapExceeded("input bytes", len(data), lim.MaxInputBytes)
	}

	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, errEmpty()
	}
	if trimmed[0] != '{' {
		return nil, errRootNotObject()
	}

	if err := rejectDuplicateKeys(trimmed); err != nil {
		return nil, err
	}

	w := &walker{lim: lim}
	root, err := w.decodeNode(trimmed, 1)
	if err != nil {
		return nil, err
	}
	if hasTrailingBytes(trimmed, root.consumed) {
		return nil, errTrailingJSON()
	}

	return &Tree{Root: root.node}, nil
}

type decodedNode struct {
	node     Node
	consumed int
}

type walker struct {
	lim       Limits
	nodes     int
	totalText int
}

func (w *walker) decodeNode(data []byte, depth int) (decodedNode, error) {
	if depth > w.lim.MaxDepth {
		return decodedNode{}, errCapExceeded("depth", depth, w.lim.MaxDepth)
	}
	w.nodes++
	if w.nodes > w.lim.MaxNodes {
		return decodedNode{}, errCapExceeded("node count", w.nodes, w.lim.MaxNodes)
	}

	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return decodedNode{}, errNullNode()
	}

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

	consumed := int(dec.InputOffset())
	if consumed == 0 {
		consumed = len(data)
	}

	comp := Component(shadow.Component)
	propDecoder, ok := componentDecoders[comp]
	if !ok {
		return decodedNode{}, errUnknownComponent(shadow.Component)
	}

	props, err := decodeProps(propDecoder, shadow.Props)
	if err != nil {
		return decodedNode{}, errDecode(err)
	}

	if err := props.validate(w.lim); err != nil {
		return decodedNode{}, err
	}

	w.totalText += props.estimatedTextSize()
	if w.totalText > w.lim.MaxTotalText {
		return decodedNode{}, errCapExceeded("total text", w.totalText, w.lim.MaxTotalText)
	}

	if shadow.ActionRef != "" {
		if err := validateActionRef(shadow.ActionRef, w.lim); err != nil {
			return decodedNode{}, err
		}
		w.totalText += len(shadow.ActionRef)
		if w.totalText > w.lim.MaxTotalText {
			return decodedNode{}, errCapExceeded("total text", w.totalText, w.lim.MaxTotalText)
		}
	}

	if len(shadow.Children) > w.lim.MaxChildrenPerNode {
		return decodedNode{}, errCapExceeded("children per node", len(shadow.Children), w.lim.MaxChildrenPerNode)
	}

	if err := enforceComponentRules(comp, props, shadow.ActionRef, shadow.Children); err != nil {
		return decodedNode{}, err
	}

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

func decodeProps(decode func(json.RawMessage) (Props, error), raw json.RawMessage) (Props, error) {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	return decode(raw)
}

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

func defaultCapForChildren(Props) int { return DefaultMaxChildrenPerNode }

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

func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := walkDupCheck(dec); err != nil {
		return err
	}
	if dec.More() {
		return errTrailingJSON()
	}
	return nil
}

func walkDupCheck(dec *json.Decoder) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
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

func walkObjectDupCheck(dec *json.Decoder) error {
	seen := make(map[string]string)
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := t.(string)
		if !ok {
			return errors.New("uinodev1: decode: object key is not a string")
		}
		norm := asciicase.Lower(key)
		if prev, dup := seen[norm]; dup {
			if prev == key {
				return errDupKey(key)
			}
			return errFoldedKey(prev, key)
		}
		seen[norm] = key
		if err := walkDupCheck(dec); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	return nil
}
