package headless

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

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
	// Name is the form-field name every value submits under. Required.
	Name string
	// Label is the visible label. Required.
	Label string
	// Values are the committed tags, each a chip holding its hidden input.
	Values      []string
	Placeholder string
	// MaxLength caps one tag's length in characters, rendered values included. Zero is no cap.
	MaxLength int
	// Help is the rule the values obey.
	Help string
	// Disabled disables the remove controls, the draft input and the add control.
	Disabled bool

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings

	// Ctx scopes the draft field's id derived from Name when ID is empty.
	Ctx context.Context
}

// TagInput renders the chips, the draft input and the add control.
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
	var fieldID string
	if p.ID != "" {
		render.ReserveID(p.Ctx, p.ID, "-field", "-field-hint")
		fieldID = p.ID + "-field"
	} else {
		fieldID = render.UniqueID(p.Ctx, p.Name, "-hint")
	}
	hintID := ""
	if p.Help != "" {
		hintID = fieldID + "-hint"
	}
	w := p.Strings.Resolve()
	b := p.Parts.Box(s)

	chips := make([]render.HTML, 0, len(p.Values))
	for _, v := range p.Values {
		v = scrubControlBytes(v)
		if p.MaxLength > 0 {
			if r := []rune(v); len(r) > p.MaxLength {
				v = string(r[:p.MaxLength])
			}
		}
		chips = append(chips, b.El("li", PartTagInputTag, html.Attrs{
			"data-hui-tag-input-remove": "",
			"aria-label":                w.fill(w.RemoveLabelled, "label", v),
		},
			render.Text(v),
			b.El("button", PartDismiss, html.Attrs{
				"type":       "button",
				"aria-label": w.fill(w.RemoveLabelled, "label", v),
			}, render.Text("×")),
			b.El("input", PartControl, html.Attrs{
				"type":  "hidden",
				"name":  p.Name,
				"value": v,
			}),
		))
	}

	field := html.Attrs{
		"type":                     "text",
		"id":                       fieldID,
		"data-hui-tag-input-field": "",
		"aria-label":               p.Label,
		"autocomplete":             "off",
	}
	attrsSet(field, "placeholder", p.Placeholder)
	attrsSet(field, "aria-describedby", hintID)
	Flag(field, "disabled", p.Disabled)
	if p.MaxLength > 0 {
		field["maxlength"] = strconv.Itoa(p.MaxLength)
	}

	add := html.Attrs{
		"type":                   "button",
		"aria-label":             w.fill(w.TagInputAdd, "label", p.Label),
		"data-hui-tag-input-add": "",
	}
	Flag(add, "disabled", p.Disabled)

	zone := []render.HTML{b.El("ul", PartTagInputList, html.Attrs{
		"role":                    "list",
		"data-hui-tag-input-list": "",
	}, chips...)}
	zone = append(zone,
		b.El("input", PartTagInputField, field),
		b.El("button", PartTagInputAdd, add, render.Text("+")))

	kids := []render.HTML{
		b.El("label", PartLabel, Internal(Attrs(map[string]string{"for": fieldID})), render.Text(p.Label)),
		b.El("div", PartTagInputZone, Internal(nil), zone...),
	}
	kids = append(kids,
		b.El("span", PartStatus, Internal(html.Attrs{
			"role":                       "status",
			"data-hui-tag-input-status":  "",
			"data-hui-tag-input-added":   w.TagInputAdded,
			"data-hui-tag-input-removed": w.TagInputRemoved,
		})),
	)
	if p.Help != "" {
		kids = append(kids, b.El("p", PartHint, Internal(html.Attrs{"id": hintID}), render.Text(p.Help)))
	}

	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label"), Attrs(map[string]string{
		"role":                            "group",
		"aria-label":                      p.Label,
		"id":                              p.ID,
		"data-hui-tag-input":              p.Name,
		"data-hui-tag-input-remove-label": w.RemoveLabelled,
	}))
	if p.MaxLength > 0 {
		own["data-hui-tag-input-maxlength"] = strconv.Itoa(p.MaxLength)
	}
	return b.El("div", PartRoot, own, kids...)
}

// Repeater parts.
const (
	PartRepeaterItems  Part = "repeater-items"
	PartRepeaterItem   Part = "repeater-item"
	PartRepeaterFields Part = "repeater-fields"
	PartRepeaterAdd    Part = "repeater-add"
)

// RepeaterItem is one repeated row of caller fields.
type RepeaterItem struct {
	Fields []render.HTML
}

// RepeaterProps configures a repeated field group.
type RepeaterProps struct {
	// Name is the group's name and the default controls' names. Required.
	Name string
	// Label names the group and the items list. Optional.
	Label string
	Items []RepeaterItem
	// MinItems disables removal at the floor; MaxItems disables addition at the ceiling (zero is unlimited).
	MinItems int
	MaxItems int
	// AddLabel and RemoveLabel override the controls' visible text.
	AddLabel    string
	RemoveLabel string
	// AddName (default Name_add), AddValue (default 1) and RemoveName (default Name_remove, value the 0-based row index) name the submit controls.
	AddName    string
	AddValue   string
	RemoveName string
	// Action is the same-origin endpoint add and remove go to in place; it requires Island.
	Action string
	// Island is where an add or remove result lands.
	Island Island
	// Status is the server's sentence in the live region.
	Status string

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

// Repeater renders the rows with their named add and remove submit controls.
func Repeater(p RepeaterProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: Repeater requires Name — the group's identity and its controls' names derive from it")
	}
	p.AddName = orDefault(p.AddName, p.Name+"_add")
	p.RemoveName = orDefault(p.RemoveName, p.Name+"_remove")
	p.AddValue = orDefault(p.AddValue, "1")
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

	opQuery := func(op string, index int) string {
		q := "?op=" + op
		if index >= 0 {
			q += "&index=" + strconv.Itoa(index)
		}
		return q
	}
	ops := Island{Endpoint: p.Action, Signal: p.Island.Signal}

	anyFields := false
	for _, item := range p.Items {
		if len(item.Fields) > 0 {
			anyFields = true
			break
		}
	}
	rows := make([]render.HTML, 0, len(p.Items))
	for i, item := range p.Items {
		remove := html.Attrs{
			"type":                     "submit",
			"data-hui-repeater-action": "remove",
			"aria-label":               w.fill(w.RepeaterRemove, "n", i+1),
		}
		if p.RemoveName != "" {
			remove["name"] = p.RemoveName
			remove["value"] = strconv.Itoa(i)
		}
		if p.Action != "" {
			for k, v := range ops.attrs(opQuery("remove", i), "POST") {
				remove[k] = v
			}
		}
		atFloor := len(p.Items) <= p.MinItems
		Flag(remove, "disabled", atFloor)
		itemOwn := html.Attrs{
			"data-hui-repeater-item":  "",
			"data-hui-repeater-index": strconv.Itoa(i),
		}
		if len(item.Fields) == 0 {
			itemOwn = Internal(itemOwn)
		}
		row := b.El("li", PartRepeaterItem, itemOwn,
			b.El("div", PartRepeaterFields, nil, item.Fields...),
			b.El("div", PartActions, Internal(nil),
				b.El("button", PartDismiss, remove,
					render.Text(orDefault(p.RemoveLabel, w.fill(w.RepeaterRemove, "n", i+1))))),
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
	if p.Action != "" {
		for k, v := range ops.attrs(opQuery("add", -1), "POST") {
			add[k] = v
		}
	}
	atCeiling := p.MaxItems > 0 && len(p.Items) >= p.MaxItems
	Flag(add, "disabled", atCeiling)

	kids := []render.HTML{
		b.El("button", PartDefaultAction, Internal(html.Attrs{
			"type":                      "submit",
			"hidden":                    "",
			"tabindex":                  "-1",
			"data-hui-repeater-default": "",
		})),
	}
	if p.Label != "" {
		kids = append(kids, b.El("span", PartLabel, Internal(nil), render.Text(p.Label)))
	}
	kids = append(kids,
		func() render.HTML {
			items := Attrs(map[string]string{"id": id + "-items", "role": "list"})
			attrsSet(items, "aria-label", p.Label)
			if !anyFields {
				items = Internal(items)
			}
			return b.El("ul", PartRepeaterItems, items, rows...)
		}(),
		b.El("button", PartRepeaterAdd, Internal(add),
			render.Text(orDefault(p.AddLabel, w.RepeaterAdd))),
		b.El("span", PartStatus, Internal(html.Attrs{
			"role":                     "status",
			"data-hui-repeater-status": "",
		}), render.Text(p.Status)),
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
		Anatomy: []Part{PartRoot, PartDefaultAction, PartLabel, PartRepeaterItems, PartRepeaterItem,
			PartRepeaterFields, PartActions, PartDismiss, PartRepeaterAdd, PartStatus},
		Hooks: []string{"data-hui-repeater", "data-hui-repeater-default", "data-hui-repeater-item",
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
