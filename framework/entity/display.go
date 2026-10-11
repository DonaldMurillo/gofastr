package entity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// DisplayConfig holds the screen hints for one entity: the names, columns,
// views, form layout and nav placement the admin and generated screens read.
// It is plain data; nil (the default) means every screen falls back to the
// schema itself. It never changes what the API accepts or returns: a field's
// own Hidden, ReadOnly and NoQuery flags keep their meaning and screens honour
// them first.
//
// Where the entity already says something (search fields, page limits, who may
// write), Display reads it rather than repeating it. Strings that hold a query
// (a view's Where and Sort, a field's ShowWhen) are not parsed here: this
// package cannot import the query DSL, so App.Entity and GroupEntity parse
// them when the app registers the entity.
type DisplayConfig struct {
	Singular string `json:"singular,omitempty"`
	Plural   string `json:"plural,omitempty"`
	// TitleFields name a record in lists, drawers, pickers and
	// breadcrumbs: their values joined with " · ". A Relation field
	// contributes the related record's own title, so a subscription
	// titled by customer_id and plan_id reads "Ada Lovelace · Pro". The
	// first carries the list's record link.
	TitleFields []string `json:"title_fields,omitempty"`
	Description string   `json:"description,omitempty"`
	Columns     []string `json:"columns,omitempty"`

	// Nav places the entity in the sidebar. Hide drops it from nav and the
	// dashboard; it never changes what the admin exposes.
	Nav    *EntityNav  `json:"nav,omitempty"`
	Views  []ListView  `json:"views,omitempty"`
	Facets []string    `json:"facets,omitempty"`
	Form   *EntityForm `json:"form,omitempty"`
	Card   *CardFields `json:"card,omitempty"`

	// Fields holds per-field screen hints keyed by field name.
	Fields map[string]FieldDisplay `json:"fields,omitempty"`

	// PageSizes lists the choices the page-size menu offers. Each entry must
	// be positive and within Pagination.MaxListLimit when that is set.
	PageSizes []int `json:"page_sizes,omitempty"`

	// NoDuplicate and NoBulk turn off the Duplicate row action, or every
	// bulk action, for this entity. The No prefix matches NoQuery.
	NoDuplicate bool `json:"no_duplicate,omitempty"`
	NoBulk      bool `json:"no_bulk,omitempty"`
}

// EntityNav places an entity (or an admin page) in the sidebar: under a
// group, with an icon, in an order. Group is a key — a lowercase ASCII
// slug, never `all` or `deleted` — translated as nav.groups.<group>. Hide
// drops the entity from nav and the dashboard; it never changes what the
// admin exposes. An entity's nav row shows how many records the viewer
// can read, recounted on every navigation; HideCount drops the count
// for a table too large to count per click. Pages ignore it.
type EntityNav struct {
	Group     string `json:"group,omitempty"`
	Icon      string `json:"icon,omitempty"`
	Order     int    `json:"order,omitempty"`
	Hide      bool   `json:"hide,omitempty"`
	HideCount bool   `json:"hide_count,omitempty"`
}

// ListView is a named starting point for a list: a DSL Where and Sort, an
// optional As for how rows are drawn ("table", "cards"), and Default for the
// one view that opens when the URL names none. A view whose filter depends
// on the caller names no Where; its func is registered under the view's key
// next to the record screens, and an unregistered Where-less view fails
// there, at app start.
type ListView struct {
	Key     string `json:"key"`
	Label   string `json:"label,omitempty"`
	Where   string `json:"where,omitempty"`
	Sort    string `json:"sort,omitempty"`
	As      string `json:"as,omitempty"`
	Default bool   `json:"default,omitempty"`
}

// EntityForm says where fields sit on the record: a main column and a narrow
// side column. Fields the form leaves out are appended at the end of Main in
// schema order (minus the auto-generated system fields), so adding a field
// never makes it vanish. No form at all means one column in schema order.
type EntityForm struct {
	Main []FormItem `json:"main,omitempty"`
	Side []FormItem `json:"side,omitempty"`
}

// FormItem is one row of the form: exactly one of Field (one field at the
// column's full width), Row (one to three fields side by side) or Section (a
// heading over its Items, optionally starting Collapsed with a Help line).
// In YAML a field item is a bare string; sections nest at most two deep.
type FormItem struct {
	Field     string     `json:"-"`
	Row       []string   `json:"row,omitempty"`
	Section   string     `json:"section,omitempty"`
	Help      string     `json:"help,omitempty"`
	Collapsed bool       `json:"collapsed,omitempty"`
	Items     []FormItem `json:"items,omitempty"`
}

// CardFields names the fields a card shows when a list is drawn as cards.
type CardFields struct {
	Title    string   `json:"title,omitempty"`
	Subtitle string   `json:"subtitle,omitempty"`
	Badge    string   `json:"badge,omitempty"`
	Meta     []string `json:"meta,omitempty"`
}

// FieldDisplay is the per-field screen hint: how a field is labelled and
// drawn, never what the API accepts.
type FieldDisplay struct {
	Label       string `json:"label,omitempty"`
	Help        string `json:"help,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`

	// Locked draws the field read-only on screens. The screens' save
	// path drops a Locked key before the write; the API may still write
	// it. Refused at registration on a Required field with no Default:
	// the value never submits on create, so no form could create the
	// record.
	Locked bool `json:"locked,omitempty"`

	// Omit leaves the field out of forms and columns on purpose. Unlike
	// Hidden, the API still returns it. Refused at registration on a
	// Required field with no Default: no form could create the record.
	Omit bool `json:"omit,omitempty"`

	// ShowWhen shows the field only while `field = value` or
	// `field in [...]` holds on an editable Enum or Bool field. Parsed
	// with the query DSL when App.Entity registers the entity; any
	// other shape is refused there. A hidden region's controls are
	// disabled, so they never submit — refused at registration on a
	// Required field with no Default, like Omit.
	ShowWhen string `json:"show_when,omitempty"`

	// Input picks a different input and cell for the same storage: a
	// built-in kind (email, url, color, markdown, code) or one the app
	// registers in entityui.Extensions.Kinds. A key here; whether the
	// kind exists is checked when entityui.New builds the app's UI.
	Input string `json:"input,omitempty"`
}

// UnmarshalJSON decodes the display block strictly: an unknown key is an
// error, not a silent ignore, matching how the blueprint decoder refuses
// unknown keys in every other entity group.
func (d *DisplayConfig) UnmarshalJSON(data []byte) error {
	type plain DisplayConfig // sheds UnmarshalJSON so this does not recurse
	var p plain
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return err
	}
	*d = DisplayConfig(p)
	return nil
}

// MarshalJSON emits the same three shapes UnmarshalJSON reads: a field item
// as a bare string, a row or section as an object, so a decoded form
// round-trips.
func (fi FormItem) MarshalJSON() ([]byte, error) {
	if fi.Field != "" {
		return json.Marshal(fi.Field)
	}
	type plain FormItem // sheds MarshalJSON; tags carry the object shape
	return json.Marshal(plain(fi))
}

// UnmarshalJSON accepts the three form-item shapes: a bare string (one
// field), {"row": [...]}, or {"section": key, "help": ..., "collapsed": ...,
// "items": [...]} where items takes the same three shapes again. Any other
// key is refused.
func (fi *FormItem) UnmarshalJSON(data []byte) error {
	var field string
	if err := json.Unmarshal(data, &field); err == nil {
		if field == "" {
			return fmt.Errorf("form item: a bare string must name a field")
		}
		fi.Field = field
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("form item must be a field name string or an object: %w", err)
	}
	for key := range raw {
		switch key {
		case "row", "section", "help", "collapsed", "items":
		default:
			return fmt.Errorf("form item: unknown key %q (a bare string names a field; an object takes row, section, help, collapsed, items)", key)
		}
	}
	if rawRow, ok := raw["row"]; ok {
		if err := json.Unmarshal(rawRow, &fi.Row); err != nil {
			return fmt.Errorf("form item row: %w", err)
		}
	}
	if rawSection, ok := raw["section"]; ok {
		if err := json.Unmarshal(rawSection, &fi.Section); err != nil {
			return fmt.Errorf("form item section: %w", err)
		}
	}
	if rawHelp, ok := raw["help"]; ok {
		if err := json.Unmarshal(rawHelp, &fi.Help); err != nil {
			return fmt.Errorf("form item help: %w", err)
		}
	}
	if rawCollapsed, ok := raw["collapsed"]; ok {
		if err := json.Unmarshal(rawCollapsed, &fi.Collapsed); err != nil {
			return fmt.Errorf("form item collapsed: %w", err)
		}
	}
	if rawItems, ok := raw["items"]; ok {
		if err := json.Unmarshal(rawItems, &fi.Items); err != nil {
			return fmt.Errorf("form item items: %w", err)
		}
	}
	return nil
}

// displayKeyGrammar is the shape every key in Display (a view, a form
// section, a nav group) must follow: a lowercase ASCII slug starting with a
// letter, no dots, so it is safe in a URL, a translation key and a binding.
var displayKeyGrammar = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidKey reports whether s follows the Display key grammar: a
// lowercase ASCII slug starting with a letter. Code that names screen
// parts with keys checks them here. States move keys follow the stricter
// transitionKeyGrammar; see ValidateStates.
func ValidKey(s string) bool { return displayKeyGrammar.MatchString(s) }

// reservedDisplayKeys are taken by the screens themselves: "all" is the
// unfiltered view every list has, "deleted" is the soft-delete view.
var reservedDisplayKeys = map[string]bool{"all": true, "deleted": true}

// validate checks every name Display holds against the entity's fields.
// Registration refuses the entity when it fails, so a typo or a stale name
// after a rename fails the app at boot, naming the entity and the offender.
func (d *DisplayConfig) validate(name string, fields []schema.Field, pagination *PaginationConfig) error {

	byName := make(map[string]*schema.Field, len(fields))
	for i := range fields {
		byName[fields[i].Name] = &fields[i]
	}
	// checkField refuses an unknown or Hidden field. Hidden is refused
	// anywhere a name would be SHOWN (a column, a title, a facet, a form
	// slot): a hidden column is either a leak or a blank cell, never what
	// the author meant.
	checkField := func(what, field string) error {
		f, ok := byName[field]
		if !ok {
			return fmt.Errorf("entity %q: display %s names field %q, which the entity does not declare", name, what, field)
		}
		if f.Hidden {
			return fmt.Errorf("entity %q: display %s names field %q, which is Hidden", name, what, field)
		}
		return nil
	}
	// checkQueryable refuses a field no query may name: a facet filters on
	// it and a sort orders by it, and NoQuery keeps it out of both.
	checkQueryable := func(what, field string) error {
		if err := checkField(what, field); err != nil {
			return err
		}
		if byName[field].NoQuery {
			return fmt.Errorf("entity %q: display %s names field %q, which is NoQuery", name, what, field)
		}
		return nil
	}
	// checkKey enforces the key grammar and the reserved words for one of
	// the entity's keys (a view, a form section, a nav group).
	checkKey := func(what, key string) error {
		if !displayKeyGrammar.MatchString(key) {
			return fmt.Errorf("entity %q: display %s key %q must be a lowercase slug matching ^[a-z][a-z0-9_]*$ with no dots", name, what, key)
		}
		if reservedDisplayKeys[key] {
			return fmt.Errorf("entity %q: display %s key %q is reserved (all and deleted belong to the screens)", name, what, key)
		}
		return nil
	}

	seenTitles := make(map[string]bool, len(d.TitleFields))
	for i, tf := range d.TitleFields {
		if err := checkField(fmt.Sprintf("title_fields[%d]", i), tf); err != nil {
			return err
		}
		if seenTitles[tf] {
			return fmt.Errorf("entity %q: display title_fields list %q more than once", name, tf)
		}
		seenTitles[tf] = true
	}
	seenColumns := make(map[string]bool, len(d.Columns))
	for i, col := range d.Columns {
		if err := checkField(fmt.Sprintf("columns[%d]", i), col); err != nil {
			return err
		}
		if seenColumns[col] {
			return fmt.Errorf("entity %q: display columns list %q more than once", name, col)
		}
		seenColumns[col] = true
	}
	// A facet is a one-click filter over a small value set, so only Enum,
	// Bool and Relation fields can be one.
	seenFacets := make(map[string]bool, len(d.Facets))
	for i, facet := range d.Facets {
		if err := checkQueryable(fmt.Sprintf("facets[%d]", i), facet); err != nil {
			return err
		}
		if seenFacets[facet] {
			return fmt.Errorf("entity %q: display facets list %q more than once", name, facet)
		}
		seenFacets[facet] = true
		switch byName[facet].Type {
		case schema.Enum, schema.Bool, schema.Relation:
		default:
			return fmt.Errorf("entity %q: display facets[%d] %q must be an Enum, Bool or Relation field, not %s", name, i, facet, schema.FieldTypeLabel(byName[facet].Type, false))
		}
	}
	if d.Card != nil {
		// A fixed order, so two bad names always report the same one.
		for _, cf := range []struct{ what, field string }{
			{"card title", d.Card.Title},
			{"card subtitle", d.Card.Subtitle},
			{"card badge", d.Card.Badge},
		} {
			if cf.field == "" {
				continue
			}
			if err := checkField(cf.what, cf.field); err != nil {
				return err
			}
		}
		for i, meta := range d.Card.Meta {
			if err := checkField(fmt.Sprintf("card.meta[%d]", i), meta); err != nil {
				return err
			}
		}
	}
	for _, field := range slices.Sorted(maps.Keys(d.Fields)) {
		if err := checkField(fmt.Sprintf("fields[%s]", field), field); err != nil {
			return err
		}
		fd := d.Fields[field]
		f := byName[field]
		if fd.Input != "" {
			if err := checkKey(fmt.Sprintf("fields[%s].input", field), fd.Input); err != nil {
				return err
			}
		}
		// A Required field with no supplied value (a Default or an
		// auto-generation) needs the form to submit it. Three hints
		// take that away: Omit leaves it off the form entirely;
		// ShowWhen hides its region while the condition does not hold,
		// and when.js disables a hidden region's controls, so they
		// never submit; Locked draws it read-only and the screens'
		// save path drops a Locked key before the write. Under any of
		// the three, no screen could create the record.
		if f.Required && f.Default == nil && f.AutoGenerate == schema.AutoNone {
			what := ""
			switch {
			case fd.Omit:
				what = "omits"
			case fd.Locked:
				what = "locks"
			case fd.ShowWhen != "":
				what = "hides behind show_when on"
			}
			if what != "" {
				return fmt.Errorf("entity %q: display fields[%s] %s a Required field with no Default; no form could create the record", name, field, what)
			}
		}
	}
	if d.Nav != nil && d.Nav.Group != "" {
		if err := checkKey("nav group", d.Nav.Group); err != nil {
			return err
		}
	}
	seenViewKeys := map[string]bool{}
	defaultViews := 0
	for i, view := range d.Views {
		if err := checkKey(fmt.Sprintf("views[%d]", i), view.Key); err != nil {
			return err
		}
		if seenViewKeys[view.Key] {
			return fmt.Errorf("entity %q: display declares view %q more than once", name, view.Key)
		}
		seenViewKeys[view.Key] = true
		if view.Default {
			defaultViews++
			if defaultViews > 1 {
				return fmt.Errorf("entity %q: display declares more than one default view; at most one view may set Default", name)
			}
		}
		// Where and Sort are DSL expressions: parsed when the app
		// registers the entity (framework/display_check.go), the same
		// grammar ?where= and ?sort= parse, so one grammar answers for
		// both. As names how rows are drawn; the two shapes the screens
		// know are table (the default) and cards.
		switch view.As {
		case "", "table", "cards":
		default:
			return fmt.Errorf("entity %q: display view %q as %q must be \"table\" or \"cards\"", name, view.Key, view.As)
		}
	}
	if err := d.Form.validate(name, checkField, checkKey); err != nil {
		return err
	}
	seenSizes := make(map[int]bool, len(d.PageSizes))
	for i, size := range d.PageSizes {
		if size <= 0 {
			return fmt.Errorf("entity %q: display page_sizes[%d] is %d; every entry must be positive", name, i, size)
		}
		if seenSizes[size] {
			return fmt.Errorf("entity %q: display page_sizes list %d more than once", name, size)
		}
		seenSizes[size] = true
		if pagination != nil && pagination.MaxListLimit > 0 && size > pagination.MaxListLimit {
			return fmt.Errorf("entity %q: display page_sizes[%d] is %d, above Pagination.MaxListLimit %d", name, i, size, pagination.MaxListLimit)
		}
	}
	return nil
}

// validate walks the form: each item is exactly one of Field, Row or Section;
// a row holds one to three distinct fields; only a section carries Items,
// Help and Collapsed; sections nest at most two deep; a field appears once
// across Main and Side; section keys follow the key grammar and are unique.
func (f *EntityForm) validate(name string, checkField func(what, field string) error, checkKey func(what, key string) error) error {
	if f == nil {
		return nil
	}
	seenSections := map[string]bool{}
	seenFields := map[string]bool{}
	place := func(where, field string) error {
		if err := checkField("form "+where, field); err != nil {
			return err
		}
		if seenFields[field] {
			return fmt.Errorf("entity %q: display form places field %q more than once across Main and Side", name, field)
		}
		seenFields[field] = true
		return nil
	}
	var walk func(items []FormItem, path string, depth int) error
	walk = func(items []FormItem, path string, depth int) error {
		for i, it := range items {
			where := fmt.Sprintf("%s[%d]", path, i)
			shapes := 0
			if it.Field != "" {
				shapes++
			}
			if len(it.Row) > 0 {
				shapes++
			}
			if it.Section != "" {
				shapes++
			}
			if shapes != 1 {
				return fmt.Errorf("entity %q: display form item %s sets %d of Field, Row and Section; exactly one is required", name, where, shapes)
			}
			if it.Section == "" && (len(it.Items) > 0 || it.Help != "" || it.Collapsed) {
				return fmt.Errorf("entity %q: display form item %s is not a section; only a section carries Items, Help or Collapsed", name, where)
			}
			switch {
			case it.Field != "":
				if err := place(where, it.Field); err != nil {
					return err
				}
			case len(it.Row) > 0:
				if len(it.Row) > 3 {
					return fmt.Errorf("entity %q: display form row %s holds %d fields; a row holds one to three", name, where, len(it.Row))
				}
				inRow := map[string]bool{}
				for _, field := range it.Row {
					if inRow[field] {
						return fmt.Errorf("entity %q: display form row %s lists field %q twice", name, where, field)
					}
					inRow[field] = true
					if err := place(where+" row", field); err != nil {
						return err
					}
				}
			default:
				if err := checkKey("form section", it.Section); err != nil {
					return err
				}
				if seenSections[it.Section] {
					return fmt.Errorf("entity %q: display form declares section %q more than once", name, it.Section)
				}
				seenSections[it.Section] = true
				if len(it.Items) == 0 {
					return fmt.Errorf("entity %q: display form section %q holds no items", name, it.Section)
				}
				if depth > 2 {
					return fmt.Errorf("entity %q: display form section %q nests %d deep; sections nest at most two", name, it.Section, depth)
				}
				if err := walk(it.Items, where+".items", depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(f.Main, "main", 1); err != nil {
		return err
	}
	return walk(f.Side, "side", 1)
}

// copyDisplayConfig returns a deep copy: every slice, map and nested pointer
// is rebuilt, so a caller that edits its value after Define changes nothing
// the app checked or serves. A nil config stays nil (nil means every
// default, unlike Scope and its siblings which Define always populates).
func copyDisplayConfig(d *DisplayConfig) *DisplayConfig {
	if d == nil {
		return nil
	}
	out := &DisplayConfig{
		Singular:    d.Singular,
		Plural:      d.Plural,
		TitleFields: slices.Clone(d.TitleFields),
		Description: d.Description,
		Columns:     slices.Clone(d.Columns),
		Facets:      slices.Clone(d.Facets),
		PageSizes:   slices.Clone(d.PageSizes),
		NoDuplicate: d.NoDuplicate,
		NoBulk:      d.NoBulk,
	}
	if d.Nav != nil {
		nav := *d.Nav
		out.Nav = &nav
	}
	if len(d.Views) > 0 {
		out.Views = slices.Clone(d.Views)
	}
	if d.Card != nil {
		card := *d.Card
		card.Meta = slices.Clone(d.Card.Meta)
		out.Card = &card
	}
	if d.Form != nil {
		out.Form = &EntityForm{
			Main: copyFormItems(d.Form.Main),
			Side: copyFormItems(d.Form.Side),
		}
	}
	if len(d.Fields) > 0 {
		out.Fields = make(map[string]FieldDisplay, len(d.Fields))
		maps.Copy(out.Fields, d.Fields)
	}
	return out
}

func copyFormItems(items []FormItem) []FormItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]FormItem, len(items))
	for i, it := range items {
		out[i] = FormItem{
			Field:     it.Field,
			Row:       slices.Clone(it.Row),
			Section:   it.Section,
			Help:      it.Help,
			Collapsed: it.Collapsed,
			Items:     copyFormItems(it.Items),
		}
	}
	return out
}
