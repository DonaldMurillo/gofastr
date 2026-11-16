package headless

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// JSONTree parts: one per JSON scalar kind under the value, so a class map can colour each.
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
	Value        any // required; a value that does not marshal to JSON panics
	OpenDepth    int // the deepest level rendered open; 0 opens the root only, -1 everything
	MaxStringLen int // truncates strings at this many runes; 0 means no limit

	ID         string
	ExtraAttrs html.Attrs

	Strings *Strings // the words; nil takes the defaults

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
	if err := jsonexact.Unmarshal(raw, &parsed); err != nil {
		panic("headless: JSONTree cannot re-parse marshalled JSON — " + err.Error())
	}
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{
		"id": p.ID,
	}))
	return b.El("div", PartRoot, own, jsonNode(b, parsed, 0, p, w))
}

// jsonNode renders one value; only depth 0 carries the internal mark.
func jsonNode(b Box, v any, depth int, p JSONTreeProps, w *Strings) render.HTML {
	var own html.Attrs
	if depth == 0 {
		own = Internal(nil)
	}
	switch t := v.(type) {
	case nil:
		return b.El("span", PartJSONNull, own, render.Text(w.JSONNull))
	case bool:
		word := w.JSONFalse
		if t {
			word = w.JSONTrue
		}
		return b.El("span", PartJSONBool, own, render.Text(word))
	case float64:
		s := strconv.FormatFloat(t, 'f', -1, 64)
		//gofastr:allow(rawtext) a JSON viewer shows the document's own digits, the source a reader copies
		return b.El("span", PartJSONNum, own, render.Text(s))
	case json.Number:
		//gofastr:allow(rawtext) a JSON viewer shows the document's own digits, the source a reader copies
		return b.El("span", PartJSONNum, own, render.Text(t.String()))
	case string:
		s := t
		if p.MaxStringLen > 0 {
			if r := []rune(s); len(r) > p.MaxStringLen {
				s = string(r[:p.MaxStringLen]) + w.JSONTruncated
			}
		}
		return b.El("span", PartJSONStr, own, render.Text(`"`+s+`"`))
	case []any:
		return jsonNodeArray(b, t, depth, p, w, own)
	case map[string]any:
		return jsonNodeObject(b, t, depth, p, w, own)
	}
	panic("headless: JSONTree walked a value that is not JSON")
}

func jsonOpenAttr(p JSONTreeProps, depth int) html.Attrs {
	if p.OpenDepth < 0 || depth <= p.OpenDepth {
		return html.Attrs{"open": ""}
	}
	return nil
}
func jsonNodeArray(b Box, arr []any, depth int, p JSONTreeProps, w *Strings, own html.Attrs) render.HTML {
	if len(arr) == 0 {
		return b.El("span", PartJSONEmpty, own, render.Text(w.JSONEmptyArray))
	}
	items := make([]render.HTML, 0, len(arr))
	for i, item := range arr {
		key := b.El("span", PartLabel, nil, render.Text(w.Number(i)))
		colon := b.El("span", PartJSONColon, nil, render.Text(":"))
		items = append(items, b.El("li", PartText, nil, key, colon,
			jsonNode(b, item, depth+1, p, w)))
	}
	list := b.El("ol", PartBody, nil, items...)
	count := b.El("span", PartJSONCount, nil, render.Text("("+w.Number(len(arr))+")"))
	typ := b.El("span", PartJSONType, nil, render.Text(w.JSONArray))
	summary := b.El("summary", PartTitle, nil, typ, count)
	return b.El("details", PartControl, Merge(jsonOpenAttr(p, depth), own), summary, list)
}
func jsonNodeObject(b Box, obj map[string]any, depth int, p JSONTreeProps, w *Strings, own html.Attrs) render.HTML {
	if len(obj) == 0 {
		return b.El("span", PartJSONEmpty, own, render.Text(w.JSONEmptyObject))
	}
	items := make([]render.HTML, 0, len(obj))
	for _, k := range slices.Sorted(maps.Keys(obj)) {
		key := b.El("span", PartLabel, nil, render.Text(`"`+k+`"`))
		colon := b.El("span", PartJSONColon, nil, render.Text(":"))
		items = append(items, b.El("li", PartText, nil, key, colon,
			jsonNode(b, obj[k], depth+1, p, w)))
	}
	list := b.El("ul", PartBody, nil, items...)
	count := b.El("span", PartJSONCount, nil, render.Text("("+w.Number(len(obj))+")"))
	typ := b.El("span", PartJSONType, nil, render.Text(w.JSONObject))
	summary := b.El("summary", PartTitle, nil, typ, count)
	return b.El("details", PartControl, Merge(jsonOpenAttr(p, depth), own), summary, list)
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
