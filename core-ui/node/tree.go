package node

import (
	"crypto/rand"
	"encoding/hex"
	"maps"
	"reflect"
)

// AssignNodeIDs gives every node without an ID one from idGen.
func AssignNodeIDs(n *Node, idGen func() string) {
	if n == nil {
		return
	}
	if n.ID == "" {
		n.ID = idGen()
	}
	for i := range n.Children {
		AssignNodeIDs(&n.Children[i], idGen)
	}
}

// NewElementID returns a fresh element ID, "el_" and 12 hex characters.
func NewElementID() string {
	var buf [6]byte
	rand.Read(buf[:]) // never returns an error since Go 1.24
	return "el_" + hex.EncodeToString(buf[:])
}

// FindNodeByID returns the node with the given ID, its parent and its
// index in the parent; nil and -1 for the root.
func FindNodeByID(root *Node, id string) (target *Node, parent *Node, index int, ok bool) {
	if root == nil || id == "" {
		return nil, nil, -1, false
	}
	if root.ID == id {
		return root, nil, -1, true
	}
	return findNodeByIDRec(root, id)
}

func findNodeByIDRec(parent *Node, id string) (*Node, *Node, int, bool) {
	for i := range parent.Children {
		child := &parent.Children[i]
		if child.ID == id {
			return child, parent, i, true
		}
		if t, p, idx, ok := findNodeByIDRec(child, id); ok {
			return t, p, idx, true
		}
	}
	return nil, nil, -1, false
}

// CloneNode returns a deep copy of n.
func CloneNode(n Node) Node {
	out := Node{
		ID:   n.ID,
		Kind: n.Kind,
	}
	if n.Props != nil {
		out.Props = cloneJSONMap(n.Props)
	}
	if n.Bindings != nil {
		out.Bindings = make(map[string]string, len(n.Bindings))
		maps.Copy(out.Bindings, n.Bindings)
	}
	if n.Actions != nil {
		out.Actions = make(map[string]Action, len(n.Actions))
		for name, action := range n.Actions {
			if action.Params != nil {
				action.Params = cloneJSONMap(action.Params)
			}
			out.Actions[name] = action
		}
	}
	if len(n.Children) > 0 {
		out.Children = make([]Node, len(n.Children))
		for i := range n.Children {
			out.Children[i] = CloneNode(n.Children[i])
		}
	}
	return out
}

func cloneJSONMap(values map[string]any) map[string]any {
	return cloneJSONValue(reflect.ValueOf(values)).Interface().(map[string]any)
}

func cloneJSONValue(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Interface:
		if !value.IsNil() {
			out := reflect.New(value.Type()).Elem()
			out.Set(cloneJSONValue(value.Elem()))
			return out
		}
	case reflect.Map:
		if !value.IsNil() {
			out := reflect.MakeMapWithSize(value.Type(), value.Len())
			for item := value.MapRange(); item.Next(); {
				out.SetMapIndex(item.Key(), cloneJSONValue(item.Value()))
			}
			return out
		}
	case reflect.Slice:
		if !value.IsNil() {
			out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
			for i := range value.Len() {
				out.Index(i).Set(cloneJSONValue(value.Index(i)))
			}
			return out
		}
	}
	return value
}
