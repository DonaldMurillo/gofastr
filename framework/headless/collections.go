package headless

import (
	"fmt"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The collections: a tag input whose values are chips the reader can
// see and remove, and a repeater whose rows are ordinary form fields
// with named submit controls. Both own a live status the server's
// words land in — the module that binds their hooks (headless-
// collections) announces chip operations through sentences that
// travel as attributes, never its own prose, and after an island swap
// it puts focus back where the operation happened.

// TagInput parts.
const (
	PartTagInputList  Part = "tag-input-list"
	PartTagInputTag   Part = "tag-input-tag"
	PartTagInputZone  Part = "tag-input-zone"
	PartTagInputField Part = "tag-input-field"
	PartTagInputAdd   Part = "tag-input-add"
)

// TagInputProps configures a free-form tag field.
type TagInputProps struct {
	// Name is the form-field name. Required: every committed value
	// submits under it, as the standard repeated-key pattern.
	Name string
	// Label is the visible label. Required.
	Label string
	// Values are the committed tags, rendered as chips and as hidden
	// inputs — the list on screen and the values in the form are the
	// same set, which is the whole point of rendering the chips at
	// all: what a reader sees remove is what a submit stops carrying.
	Values []string
	// Placeholder is the draft input's placeholder. A placeholder is
	// not a name; the label is.
	Placeholder string
	// MaxLength caps one tag's length, in characters. Zero is no cap;
	// negative is refused. The cap applies to the rendered values
	// too, so a server value longer than the cap cannot submit
	// unchanged while a typed one is refused.
	MaxLength int
	// Help is the rule the values obey.
	Help string
	// Disabled takes the chips' remove controls, the draft input and
	// the add control out.
	Disabled bool

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on every part. Strings carry the add
	// control's name, the remove controls' names, and the two
	// sentences the status says after a chip operation.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// TagInput renders the chips, the draft input and the add control.
//
// The chip list is a real list (role="list" of items) rather than a
// strip of hidden inputs the runtime converts on arrival: the values
// are visible on the first paint, each with its own named remove
// control, and the hidden inputs beside them are what the form
// submits. Enter and comma commit the draft; Backspace on an empty
// draft removes the last chip; the add control commits it for a
// reader whose keyboard has no handy Enter (a tablet's on-screen
// one) — all bound by the module, none of it required for the values
// already committed to be real.
func TagInput(p TagInputProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: TagInput requires Name — a control with no name submits nothing")
	}
	if p.Label == "" {
		panic("headless: TagInput requires Label — the field and its add control are named from it")
	}
	if p.MaxLength < 0 {
		panic("headless: TagInput MaxLength " + strconv.Itoa(p.MaxLength) + " is negative — a cap below zero caps nothing")
	}
	id := orDefault(p.ID, p.Name)
	w := p.Strings.Resolve()
	b := p.Parts.Box(s)

	chips := make([]render.HTML, 0, len(p.Values))
	for _, v := range p.Values {
		// The values are request-shaped: a CR or LF in a posted tag is
		// scrubbed the way the table's carried query is, not refused.
		v = scrubControlBytes(v)
		if p.MaxLength > 0 {
			if r := []rune(v); len(r) > p.MaxLength {
				v = string(r[:p.MaxLength])
			}
		}
		chips = append(chips, b.El("li", PartTagInputTag, html.Attrs{
			"data-hui-tag-input-remove": "",
			"aria-label":                fmt.Sprintf(w.RemoveLabelled, v),
		},
			render.Text(v),
			// The remove control keeps its type=button: it edits the
			// set, it does not submit the form.
			b.El("button", PartDismiss, html.Attrs{
				"type":       "button",
				"aria-label": fmt.Sprintf(w.RemoveLabelled, v),
			}, render.Text("×")),
			// The value rides inside its chip, the shape the module
			// builds too: removing the list item removes the value
			// from the form, whoever rendered the chip.
			b.El("input", PartControl, html.Attrs{
				"type":  "hidden",
				"name":  p.Name,
				"value": v,
			}),
		))
	}

	field := html.Attrs{
		"type":                     "text",
		"id":                       id,
		"data-hui-tag-input-field": "",
		"aria-label":               p.Label,
		"autocomplete":             "off",
	}
	attrsSet(field, "placeholder", p.Placeholder)
	Flag(field, "disabled", p.Disabled)
	if p.MaxLength > 0 {
		field["maxlength"] = strconv.Itoa(p.MaxLength)
	}

	add := html.Attrs{
		"type":                   "button",
		"aria-label":             fmt.Sprintf(w.TagInputAdd, p.Label),
		"data-hui-tag-input-add": "",
	}
	Flag(add, "disabled", p.Disabled)

	// The chips, the committed values and the draft draw one bordered
	// zone in the sheet: the field reads as one control with its
	// values in it, which is what a reader sees it as.
	zone := []render.HTML{b.El("ul", PartTagInputList, html.Attrs{
		"role":                    "list",
		"data-hui-tag-input-list": "",
	}, chips...)}
	zone = append(zone,
		b.El("input", PartTagInputField, field),
		b.El("button", PartTagInputAdd, add, render.Text("+")))

	kids := []render.HTML{
		b.El("label", PartLabel, Attrs(map[string]string{"for": id}), render.Text(p.Label)),
		b.El("div", PartTagInputZone, nil, zone...),
	}
	kids = append(kids,
		// The status is empty and wired: the sentences travel as
		// attributes from Strings, the module writes one after a chip
		// operation, and a translated page announces in its own
		// language.
		b.El("span", PartStatus, html.Attrs{
			"role":                       "status",
			"data-hui-tag-input-status":  "",
			"data-hui-tag-input-added":   w.TagInputAdded,
			"data-hui-tag-input-removed": w.TagInputRemoved,
		}),
	)
	if p.Help != "" {
		kids = append(kids, b.El("p", PartHint, nil, render.Text(p.Help)))
	}

	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label"), Attrs(map[string]string{
		"role":               "group",
		"aria-label":         p.Label,
		"id":                 p.ID,
		"data-hui-tag-input": p.Name,
		// The remove-label template travels so a chip the module
		// builds carries the same accessible name the server gave the
		// chips it rendered; the cap travels so a runtime commit
		// cannot be longer than a typed one.
		"data-hui-tag-input-remove-label": w.RemoveLabelled,
	}))
	if p.MaxLength > 0 {
		own["data-hui-tag-input-maxlength"] = strconv.Itoa(p.MaxLength)
	}
	return b.El("div", PartRoot, own, kids...)
}

// ─── Repeater ───────────────────────────────────────────────────────

// Repeater parts.
const (
	PartRepeaterItems  Part = "repeater-items"
	PartRepeaterItem   Part = "repeater-item"
	PartRepeaterFields Part = "repeater-fields"
	PartRepeaterAdd    Part = "repeater-add"
)

// RepeaterItem is one repeated row: the fields are the caller's, the
// remove control is the component's.
type RepeaterItem struct {
	Fields []render.HTML
}

// RepeaterProps configures a repeated field group.
type RepeaterProps struct {
	// Name is the group's name. Required; the submit controls' names
	// derive from it when the caller does not give their own.
	Name string
	// Label names the group for assistive technology and names the
	// items region. Optional: a repeater beside its own heading may
	// not need one, though two unnamed repeaters on a page are two
	// identical entries in a landmarks list.
	Label string
	// Items are the rendered rows, in order.
	Items []RepeaterItem
	// MinItems is the floor below which removal is refused (the
	// remove controls render disabled). MaxItems is the ceiling above
	// which addition is refused. Zero MaxItems is unlimited.
	MinItems int
	MaxItems int
	// AddLabel and RemoveLabel override the add control's visible
	// text and the remove controls'; the remove control's accessible
	// name always carries its 1-based position.
	AddLabel    string
	RemoveLabel string
	// AddName and AddValue name the add control as a submit control
	// (a FormRepeater's "<Name>_add=1"); RemoveName names the remove
	// controls, whose value is the row's 0-based index. Empty names
	// render plain buttons the surrounding form does not carry.
	AddName    string
	AddValue   string
	RemoveName string
	// Action is the same-origin URL the add and remove operations go
	// to when the result swaps in place; it is required together with
	// Island and useless without it — a plain form repeater needs no
	// Action, because the surrounding form's own action is the
	// no-script destination and the submit controls ride it.
	Action string
	// Island is where an add or remove result lands. Required when
	// Action is set, refused half-wired like every Island.
	Island Island
	// Status is the sentence the live region carries — the server's
	// own words about the operation that just happened, re-rendered
	// with the region after an island swap.
	Status string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on every part. Strings carry the add
	// control's text and the remove controls' names.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// Repeater renders the repeated rows with their add and remove controls.
//
// The rows are ordinary form fields and the controls are named submit
// buttons, so the no-script page is not a degraded page: add and
// remove submit the surrounding form with the operation in the
// request, and the server re-renders. With an Island the same buttons
// carry the framework's RPC contract beside their submit semantics,
// the region swaps in place, and the module restores focus to the
// row's first control (a removal) or the add control (an addition).
func Repeater(p RepeaterProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: Repeater requires Name — the group's identity and its controls' names derive from it")
	}
	if p.MinItems < 0 {
		panic("headless: Repeater MinItems " + strconv.Itoa(p.MinItems) + " is negative — a floor below zero is no floor")
	}
	if p.MaxItems < 0 {
		panic("headless: Repeater MaxItems " + strconv.Itoa(p.MaxItems) + " is negative — a ceiling below zero is no ceiling")
	}
	if p.MaxItems > 0 && p.MinItems > p.MaxItems {
		panic("headless: Repeater MinItems " + strconv.Itoa(p.MinItems) + " is above MaxItems " + strconv.Itoa(p.MaxItems) + " — a range that admits no items is a configuration error")
	}
	if p.MaxItems > 0 && len(p.Items) > p.MaxItems {
		panic("headless: Repeater carries " + strconv.Itoa(len(p.Items)) + " items above MaxItems " + strconv.Itoa(p.MaxItems) + " — the server's own props disagree")
	}
	if p.Action == "" && !p.Island.zero() {
		panic("headless: Repeater carries an Island with no Action — the Action is the endpoint the operations go to; without it the Island is a target with no request")
	}
	if p.Action != "" {
		checkSameOrigin("Repeater", "Action", p.Action)
		requireIsland("Repeater with Action", p.Island)
	}

	w := p.Strings.Resolve()
	b := p.Parts.Box(s)
	id := orDefault(p.ID, p.Name)

	// The operations ride the island's query, the same pairs the
	// plain submit carries: op for the operation, index for the row.
	// They travel as a bare query because Island.attrs merges the
	// href's pairs onto the endpoint's own — building the full URL
	// here would duplicate every pair the endpoint already had.
	opQuery := func(op string, index int) string {
		q := "?op=" + op
		if index >= 0 {
			q += "&index=" + strconv.Itoa(index)
		}
		return q
	}

	rows := make([]render.HTML, 0, len(p.Items))
	for i, item := range p.Items {
		remove := html.Attrs{
			"type":                     "submit",
			"data-hui-repeater-action": "remove",
			"aria-label":               fmt.Sprintf(w.RepeaterRemove, i+1),
		}
		if p.RemoveName != "" {
			remove["name"] = p.RemoveName
			remove["value"] = strconv.Itoa(i)
		}
		if p.Island.Endpoint != "" {
			for k, v := range p.Island.attrs(opQuery("remove", i), "POST") {
				remove[k] = v
			}
		}
		atFloor := len(p.Items) <= p.MinItems
		Flag(remove, "disabled", atFloor)
		row := b.El("li", PartRepeaterItem, html.Attrs{
			"data-hui-repeater-item":  "",
			"data-hui-repeater-index": strconv.Itoa(i),
		},
			b.El("div", PartRepeaterFields, nil, item.Fields...),
			b.El("div", PartActions, nil,
				b.El("button", PartDismiss, remove,
					render.Text(orDefault(p.RemoveLabel, fmt.Sprintf(w.RepeaterRemove, i+1))))),
		)
		rows = append(rows, row)
	}

	add := html.Attrs{
		"type":                     "submit",
		"data-hui-repeater-action": "add",
	}
	if p.AddName != "" {
		add["name"] = p.AddName
		add["value"] = orDefault(p.AddValue, "1")
	}
	if p.Island.Endpoint != "" {
		for k, v := range p.Island.attrs(opQuery("add", -1), "POST") {
			add[k] = v
		}
	}
	atCeiling := p.MaxItems > 0 && len(p.Items) >= p.MaxItems
	Flag(add, "disabled", atCeiling)

	kids := []render.HTML{}
	if p.Label != "" {
		kids = append(kids, b.El("span", PartLabel, nil, render.Text(p.Label)))
	}
	kids = append(kids,
		func() render.HTML {
			items := Attrs(map[string]string{"id": id + "-items", "role": "list"})
			attrsSet(items, "aria-label", p.Label)
			return b.El("ul", PartRepeaterItems, items, rows...)
		}(),
		b.El("button", PartRepeaterAdd, add,
			render.Text(orDefault(p.AddLabel, w.RepeaterAdd))),
		b.El("span", PartStatus, html.Attrs{
			"role":                     "status",
			"data-hui-repeater-status": "",
		}, render.Text(p.Status)),
	)

	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label"), Attrs(map[string]string{
		"role":              "group",
		"aria-label":        orDefault(p.Label, p.Name),
		"id":                p.ID,
		"data-hui-repeater": p.Name,
	}))
	return b.El("div", PartRoot, own, kids...)
}

func init() {
	Register(Spec{
		Name: "TagInput",
		Anatomy: []Part{PartRoot, PartLabel, PartTagInputZone, PartTagInputList,
			PartTagInputTag, PartDismiss, PartControl, PartTagInputField,
			PartTagInputAdd, PartStatus, PartHint},
		Hooks: []string{"data-hui-tag-input", "data-hui-tag-input-list", "data-hui-tag-input-field",
			"data-hui-tag-input-add", "data-hui-tag-input-remove", "data-hui-tag-input-status",
			"data-hui-tag-input-added", "data-hui-tag-input-removed",
			"data-hui-tag-input-remove-label", "data-hui-tag-input-maxlength"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return TagInput(TagInputProps{Name: "tags", Label: "Tags", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "values, visible and submittable",
				Why:  "every committed value is a chip with its own named remove control AND a hidden input — what a reader sees removed is what a submit stops carrying, on the first paint, with no module at all",
				HTML: TagInput(TagInputProps{Name: "tags", Label: "Tags",
					Values: []string{"go", "css"}}, s),
			}, {
				Name: "an empty list with a draft field",
				Why:  "an empty tag field still renders its list and its status wiring, so the module that binds the field arrives to a page shaped like the one it expects",
				HTML: TagInput(TagInputProps{Name: "topics", Label: "Topics",
					Placeholder: "Type a topic"}, s),
			}, {
				Name: "a capped, helped field",
				Why:  "the cap rides both the draft input and the rendered values — a server value longer than the cap cannot submit unchanged while a typed one is refused — and the rule rides the field's own description",
				HTML: TagInput(TagInputProps{Name: "labels", Label: "Labels",
					Values: []string{"prod"}, MaxLength: 8,
					Help: "One word each, eight characters at most."}, s),
			}}
		},
	})

	Register(Spec{
		Name: "Repeater",
		Anatomy: []Part{PartRoot, PartLabel, PartRepeaterItems, PartRepeaterItem,
			PartRepeaterFields, PartActions, PartDismiss, PartRepeaterAdd, PartStatus},
		Hooks: []string{"data-hui-repeater", "data-hui-repeater-item",
			"data-hui-repeater-action", "data-hui-repeater-index", "data-hui-repeater-status"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Repeater(RepeaterProps{Name: "links", Items: []RepeaterItem{{}}, Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			fields := func() []render.HTML {
				return []render.HTML{Input(InputProps{Name: "links-label", AriaLabel: "Link label"}, s)}
			}
			return []Case{{
				Name: "named submit controls, no script",
				Why:  "the add and remove controls are named submit buttons riding the surrounding form — the no-script page adds and removes rows through ordinary POSTs, and each remove names its row",
				HTML: Repeater(RepeaterProps{Name: "links", Label: "Links",
					Items:      []RepeaterItem{{Fields: fields()}, {Fields: fields()}},
					AddName:    "links_add",
					RemoveName: "links_remove"}, s),
			}, {
				Name: "an island repeater",
				Why:  "the same buttons carry the framework's RPC contract beside their submit semantics — the href-shaped operation in the endpoint's query — so the region swaps in place and the module restores focus to the row or the add control",
				HTML: Repeater(RepeaterProps{Name: "guests", Label: "Guests",
					Items:  []RepeaterItem{{Fields: fields()}},
					Action: "/island/guests",
					Island: Island{Endpoint: "/island/guests", Signal: "guests"}}, s),
			}, {
				Name: "at the floor",
				Why:  "a min of one disables the single row's remove control rather than hiding it — the row is real, and the control says it cannot go",
				HTML: Repeater(RepeaterProps{Name: "owners", Label: "Owners",
					Items: []RepeaterItem{{Fields: fields()}}, MinItems: 1,
					AddName: "owners_add", RemoveName: "owners_remove",
					Status: "One owner is required."}, s),
			}}
		},
	})
}
