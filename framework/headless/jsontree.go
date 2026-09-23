package headless

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The JSON tree: an arbitrary value as nested native details, so the
// whole surface collapses and expands with no script. Deterministic
// by construction — object keys render sorted — because a value whose
// bytes change per render defeats every cache and golden the stack
// has. No module: the browser owns the disclosure.

// JSONTreeParts: the primitive has no interactive parts beyond the
// details the browser owns, so the anatomy is textual (the value, the
// node, the summary, the list, the key) — and, under the value, one
// part per JSON scalar kind, so a class map can colour a string
// without colouring a number. Every one renders the same span the
// shared PartText did; only the name a class map can target differs.
const (
	PartJSONColon Part = "json-colon"
	PartJSONType  Part = "json-type"
	PartJSONCount Part = "json-count"
	PartJSONStr   Part = "json-str"
	PartJSONNum   Part = "json-num"
	PartJSONBool  Part = "json-bool"
	PartJSONNull  Part = "json-null"
	PartJSONEmpty Part = "json-empty"
)

// JSONTreeProps configures the tree.
type JSONTreeProps struct {
	// Value is the data to render. Required non-nil; values that do
	// not marshal to JSON are refused (a channel, a func, a cycle).
	Value any
	// OpenDepth is the recursion depth that renders open by default.
	// 0 means only the root is open; -1 means everything is open.
	OpenDepth int
	// MaxStringLen truncates long strings with the truncation mark.
	// 0 means no limit.
	MaxStringLen int

	ID         string
	ExtraAttrs html.Attrs

	// Strings overrides the words (object, array, null, empty,
	// truncation). Nil takes the defaults.
	Strings *Strings

	// Parts: attrs on the root, nodes, summaries, lists, keys.
	Parts Parts
}

// JSONTree renders the value as a collapsible tree.
func JSONTree(p JSONTreeProps, s Classes) render.HTML {
	w := p.Strings.Resolve()
	raw, err := json.Marshal(p.Value)
	if err != nil {
		panic("headless: JSONTree cannot marshal Value — " + err.Error())
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		panic("headless: JSONTree cannot re-parse marshalled JSON — " + err.Error())
	}
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{
		"id": p.ID,
	}))
	return b.El("div", PartRoot, own, jsonNode(b, parsed, 0, p, w))
}

// jsonNode renders one value at one depth.
func jsonNode(b Box, v any, depth int, p JSONTreeProps, w *Strings) render.HTML {
	switch t := v.(type) {
	case nil:
		return b.El("span", PartJSONNull, nil, render.Text(w.JSONNull))
	case bool:
		word := w.JSONFalse
		if t {
			word = w.JSONTrue
		}
		return b.El("span", PartJSONBool, nil, render.Text(word))
	case float64:
		s := strconv.FormatFloat(t, 'f', -1, 64)
		return b.El("span", PartJSONNum, nil, render.Text(s))
	case string:
		s := t
		if p.MaxStringLen > 0 && len(s) > p.MaxStringLen {
			s = s[:p.MaxStringLen] + w.JSONTruncated
		}
		return b.El("span", PartJSONStr, nil, render.Text(`"`+s+`"`))
	case []any:
		return jsonNodeArray(b, t, depth, p, w)
	case map[string]any:
		return jsonNodeObject(b, t, depth, p, w)
	}
	panic("headless: JSONTree walked a value that is not JSON")
}

func jsonOpenAttr(p JSONTreeProps, depth int) html.Attrs {
	if p.OpenDepth < 0 || depth <= p.OpenDepth {
		return html.Attrs{"open": ""}
	}
	return nil
}
func jsonNodeArray(b Box, arr []any, depth int, p JSONTreeProps, w *Strings) render.HTML {
	if len(arr) == 0 {
		return b.El("span", PartJSONEmpty, nil, render.Text(w.JSONEmptyArray))
	}
	items := make([]render.HTML, 0, len(arr))
	for i, item := range arr {
		key := b.El("span", PartLabel, nil, render.Text(strconv.Itoa(i)))
		colon := b.El("span", PartJSONColon, nil, render.Text(":"))
		items = append(items, b.El("li", PartText, nil, key, colon,
			jsonNode(b, item, depth+1, p, w)))
	}
	list := b.El("ol", PartBody, nil, items...)
	count := b.El("span", PartJSONCount, nil, render.Text("("+strconv.Itoa(len(arr))+")"))
	typ := b.El("span", PartJSONType, nil, render.Text(w.JSONArray))
	summary := b.El("summary", PartTitle, nil, typ, count)
	return b.El("details", PartControl, jsonOpenAttr(p, depth), summary, list)
}
func jsonNodeObject(b Box, obj map[string]any, depth int, p JSONTreeProps, w *Strings) render.HTML {
	if len(obj) == 0 {
		return b.El("span", PartJSONEmpty, nil, render.Text(w.JSONEmptyObject))
	}
	// Sorted keys: a map's range order is random, and a tree whose
	// bytes change per render defeats every cache and golden.
	items := make([]render.HTML, 0, len(obj))
	for _, k := range slices.Sorted(maps.Keys(obj)) {
		key := b.El("span", PartLabel, nil, render.Text(`"`+k+`"`))
		colon := b.El("span", PartJSONColon, nil, render.Text(":"))
		items = append(items, b.El("li", PartText, nil, key, colon,
			jsonNode(b, obj[k], depth+1, p, w)))
	}
	list := b.El("ul", PartBody, nil, items...)
	count := b.El("span", PartJSONCount, nil, render.Text("("+strconv.Itoa(len(obj))+")"))
	typ := b.El("span", PartJSONType, nil, render.Text(w.JSONObject))
	summary := b.El("summary", PartTitle, nil, typ, count)
	return b.El("details", PartControl, jsonOpenAttr(p, depth), summary, list)
}

func init() {
	Register(Spec{
		Name: "JSONTree",
		Anatomy: []Part{PartRoot, PartControl, PartTitle, PartBody,
			PartLabel, PartText, PartJSONColon, PartJSONType, PartJSONCount,
			PartJSONStr, PartJSONNum, PartJSONBool, PartJSONNull, PartJSONEmpty},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return JSONTree(JSONTreeProps{Value: map[string]any{"a": 1}, Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "an object tree",
				Why:  "every collection is a native details, so the tree collapses and expands with no script",
				HTML: JSONTree(JSONTreeProps{Value: map[string]any{
					"name": "gofastr",
					"tags": []any{"fast", "ssr"},
					"meta": map[string]any{"stars": 42},
				}}, s),
			}, {
				Name: "the scalar and empty words",
				Why:  "an empty collection renders its word, not a details with nothing under it, and the null/boolean literals are words a translation owns",
				HTML: JSONTree(JSONTreeProps{Value: map[string]any{
					"nothing":   nil,
					"on":        true,
					"off":       false,
					"emptyList": []any{},
					"emptyMap":  map[string]any{},
					"long":      "a string long enough to truncate",
				}, MaxStringLen: 8}, s),
			}}
		},
	})
}
