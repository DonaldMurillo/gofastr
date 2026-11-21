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

// DisplayConfig holds an entity's screen hints. Nil means every screen falls
// back to the schema; it never changes what the API accepts or returns.
type DisplayConfig struct {
	Singular string `json:"singular,omitempty"`
	Plural   string `json:"plural,omitempty"`
	// TitleFields name a record, values joined with " · "; a Relation contributes the related record's title.
	TitleFields []string `json:"title_fields,omitempty"`
	Description string   `json:"description,omitempty"`
	Columns     []string `json:"columns,omitempty"`

	// Nav places the entity in the sidebar.
	Nav    *EntityNav  `json:"nav,omitempty"`
	Views  []ListView  `json:"views,omitempty"`
	Facets []string    `json:"facets,omitempty"`
	Form   *EntityForm `json:"form,omitempty"`
	Card   *CardFields `json:"card,omitempty"`

	// Fields holds per-field screen hints keyed by field name.
	Fields map[string]FieldDisplay `json:"fields,omitempty"`

	// PageSizes lists the page-size menu's choices, each positive and within Pagination.MaxListLimit when set.
	PageSizes []int `json:"page_sizes,omitempty"`

	// NoDuplicate and NoBulk turn off the Duplicate row action or every bulk action.
	NoDuplicate bool `json:"no_duplicate,omitempty"`
	NoBulk      bool `json:"no_bulk,omitempty"`
}

// EntityNav places an entity or admin page in the sidebar. Group is a display
// key, translated as nav.groups.<group>. Hide drops it from nav and the
// dashboard; HideCount drops the record count (pages ignore it).
type EntityNav struct {
	Group     string `json:"group,omitempty"`
	Icon      string `json:"icon,omitempty"`
	Order     int    `json:"order,omitempty"`
	Hide      bool   `json:"hide,omitempty"`
	HideCount bool   `json:"hide_count,omitempty"`
}

// ListView is a named list starting point: a DSL Where and Sort, As ("table" or
// "cards"), and Default for the view opened when the URL names none.
type ListView struct {
	Key     string `json:"key"`
	Label   string `json:"label,omitempty"`
	Where   string `json:"where,omitempty"`
	Sort    string `json:"sort,omitempty"`
	As      string `json:"as,omitempty"`
	Default bool   `json:"default,omitempty"`
}

// EntityForm places fields in a main and a side column; unplaced fields append to Main.
type EntityForm struct {
	Main []FormItem `json:"main,omitempty"`
	Side []FormItem `json:"side,omitempty"`
}

// FormItem is one form row: exactly one of Field, Row (one to three fields) or
// Section (a heading over Items). In YAML a field item is a bare string.
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

// FieldDisplay is a per-field screen hint: how a field is labelled and drawn.
type FieldDisplay struct {
	Label       string `json:"label,omitempty"`
	Help        string `json:"help,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`

	// Locked draws the field read-only; screens drop it before the write, the API may still write it.
	Locked bool `json:"locked,omitempty"`

	// Omit leaves the field out of forms and columns; the API still returns it.
	Omit bool `json:"omit,omitempty"`

	// ShowWhen shows the field only while `field = value` or `field in [...]` holds on an editable Enum or Bool field.
	ShowWhen string `json:"show_when,omitempty"`

	// Input picks a built-in or app-registered input kind for the same storage.
	Input string `json:"input,omitempty"`
}

// decodeStrictJSON decodes data into dst with unknown fields refused, so a
// config's UnmarshalJSON keeps its local aliasing and assignment boundary.
func decodeStrictJSON(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// UnmarshalJSON decodes strictly: an unknown key is an error.
func (d *DisplayConfig) UnmarshalJSON(data []byte) error {
	type plain DisplayConfig
	var p plain
	if err := decodeStrictJSON(data, &p); err != nil {
		return err
	}
	*d = DisplayConfig(p)
	return nil
}

// MarshalJSON writes a field item as a bare string and a row or section as an object, so a decoded form round-trips.
func (fi FormItem) MarshalJSON() ([]byte, error) {
	if fi.Field != "" {
		return json.Marshal(fi.Field)
	}
	type plain FormItem
	return json.Marshal(plain(fi))
}

// UnmarshalJSON accepts a bare field-name string, {"row": [...]} or a section object; any other key is refused.
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
	for _, key := range slices.Sorted(maps.Keys(raw)) {
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

var displayKeyGrammar = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidKey reports whether s is a display key: a lowercase ASCII slug led by a letter.
func ValidKey(s string) bool { return displayKeyGrammar.MatchString(s) }

var reservedDisplayKeys = map[string]bool{"all": true, "deleted": true}

func (d *DisplayConfig) validate(name string, fields []schema.Field, pagination *PaginationConfig) error {
	byName := make(map[string]*schema.Field, len(fields))
	for i := range fields {
		byName[fields[i].Name] = &fields[i]
	}
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
	checkQueryable := func(what, field string) error {
		if err := checkField(what, field); err != nil {
			return err
		}
		if byName[field].NoQuery {
			return fmt.Errorf("entity %q: display %s names field %q, which is NoQuery", name, what, field)
		}
		return nil
	}
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
