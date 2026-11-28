package appspec

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	fwdsl "github.com/DonaldMurillo/gofastr/framework/dsl"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	fwfilter "github.com/DonaldMurillo/gofastr/framework/filter"
	fwmigrate "github.com/DonaldMurillo/gofastr/framework/migrate"
)

func validateAppSpecBlock(screenName string, entities map[string]framework.EntityDeclaration, block AppSpecBlock) error {
	kind := block.Kind
	if kind == "" {
		kind = block.Type
	}
	if block.Bulk && strings.ToLower(strings.TrimSpace(kind)) != "entity_list" {
		return fmt.Errorf("app spec: screen %q bulk applies only to an entity_list block, not %q", screenName, kind)
	}
	if block.Filter != "" && strings.ToLower(strings.TrimSpace(kind)) != "entity_list" {
		return fmt.Errorf("app spec: screen %q filter applies only to an entity_list block, not %q", screenName, kind)
	}
	requiredProp := ""
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "label":
		if len(block.Children) == 0 {
			requiredProp = "for"
		}
	case "fieldset":
		requiredProp = "legend"
	case "image", "img":
		requiredProp = "src"
	}
	if requiredProp != "" {
		value, _ := block.Props[requiredProp].(string)
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("app spec: screen %q %s block %s is required", screenName, kind, requiredProp)
		}
	}
	// Every catalog kind's text props, whichever arm below judges the
	// rest of the block: section and the charts reach no catalog check
	// of their own. Only kind: names a catalog section; type: section is
	// the plain tag.
	if block.Kind != "" && AppSpecCatalogKind(kind) {
		if err := validateCatalogText(screenName, strings.ToLower(strings.TrimSpace(kind)), block); err != nil {
			return err
		}
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "raw":
		// noderender has no raw kind (the IR is untrusted; a raw
		// passthrough would be an XSS sink), so the block would render
		// as an escaped "unknown kind" comment and vanish from the page.
		return fmt.Errorf("app spec: screen %q raw is not a render kind. Use a supported block kind such as paragraph or markdown", screenName)
	case "", "text", "p", "paragraph", "section", "div", "article", "main", "header", "footer", "nav", "aside", "span", "strong", "em", "code", "pre", "small", "blockquote", "button", "input", "label", "form", "select", "option", "textarea", "fieldset", "image", "img", "list", "ul", "ol", "li", "table", "thead", "tbody", "tr", "th", "td":
	case "heading", "h1", "h2", "h3", "h4", "h5", "h6":
		level := block.Level
		if level == 0 {
			level = 1
		}
		if level < 1 || level > 6 {
			return fmt.Errorf("app spec: screen %q heading level must be 1-6", screenName)
		}
	case "link", "a":
		text := block.Text
		if text == "" {
			text, _ = block.Props["text"].(string)
		}
		label, _ := block.Props["aria-label"].(string)
		labelledBy, _ := block.Props["aria-labelledby"].(string)
		if AppSpecBlockUsesNodeRenderer(block) && strings.TrimSpace(text) == "" && len(block.Children) == 0 && strings.TrimSpace(label) == "" && strings.TrimSpace(labelledBy) == "" {
			return fmt.Errorf("app spec: screen %q link block text or children are required unless aria-label or aria-labelledby supplies an accessible name", screenName)
		}
		href := block.Href
		if href == "" {
			href, _ = block.Props["href"].(string)
		}
		if href == "" {
			return fmt.Errorf("app spec: screen %q link block href is required", screenName)
		}
		// The renderer swaps an unsafe href for "#"; refuse it here so the
		// author hears about it instead of shipping a dead link.
		if !urlsafe.OK(href, urlsafe.Anchor) {
			return fmt.Errorf("app spec: screen %q link href %q is not a safe link: use an http(s), mailto, fragment or relative URL", screenName, href)
		}
	case "entity_list":
		if block.Entity == "" {
			return fmt.Errorf("app spec: screen %q entity_list block entity is required", screenName)
		}
		decl, ok := entities[block.Entity]
		if !ok {
			return fmt.Errorf("app spec: screen %q entity_list targets unknown entity %q", screenName, block.Entity)
		}
		if !EntityDeclarationCRUDEnabled(decl) {
			return fmt.Errorf("app spec: screen %q entity_list target %q must enable crud", screenName, block.Entity)
		}
		if len(block.Fields) == 0 {
			return fmt.Errorf("app spec: screen %q entity_list block fields are required", screenName)
		}
		// Each entry resolves to one declared column, the one the generator
		// emits into .Columns(...). entityui draws a column once and only
		// for a visible, non-omitted field, and fails the whole list
		// otherwise, so a hidden, omitted or repeated one is refused here.
		listed := map[string]bool{}
		for _, field := range block.Fields {
			col, ok := EntityListColumn(decl, field)
			if !ok {
				return fmt.Errorf("app spec: screen %q entity_list field %q is not defined on entity %q", screenName, field, block.Entity)
			}
			if col.Hidden {
				return fmt.Errorf("app spec: screen %q entity_list field %q is hidden on entity %q: a list cannot show it", screenName, field, block.Entity)
			}
			if decl.Display != nil && decl.Display.Fields[col.Name].Omit {
				return fmt.Errorf("app spec: screen %q entity_list field %q is omitted by entity %q's display.fields: a list cannot show it", screenName, field, block.Entity)
			}
			if listed[col.Name] {
				return fmt.Errorf("app spec: screen %q entity_list field %q is listed twice", screenName, field)
			}
			listed[col.Name] = true
		}
		if err := validateEntityListFilter(screenName, decl, block.Filter); err != nil {
			return err
		}
		if block.Limit < 0 {
			return fmt.Errorf("app spec: screen %q entity_list limit must be >= 0", screenName)
		}
		// mode picks how rows are drawn; the list builder accepts exactly
		// these presentations, so refuse the rest here, naming the two.
		switch strings.ToLower(strings.TrimSpace(block.Mode)) {
		case "", "table", "cards":
		default:
			return fmt.Errorf("app spec: screen %q entity_list mode %q is not a presentation; use table or cards", screenName, block.Mode)
		}
	case "entity_form":
		if block.Entity == "" {
			return fmt.Errorf("app spec: screen %q entity_form block entity is required", screenName)
		}
		decl, ok := entities[block.Entity]
		if !ok {
			return fmt.Errorf("app spec: screen %q entity_form targets unknown entity %q", screenName, block.Entity)
		}
		if !EntityDeclarationCRUDEnabled(decl) {
			return fmt.Errorf("app spec: screen %q entity_form target %q must enable crud", screenName, block.Entity)
		}
		mode := strings.ToLower(strings.TrimSpace(block.Mode))
		if mode != "" && mode != "create" && mode != "edit" {
			return fmt.Errorf("app spec: screen %q entity_form mode must be \"create\" or \"edit\"", screenName)
		}
		// The generator draws a control for a fields: entry only when it
		// names a declared field exactly and the form draws that field, so
		// an entry it would drop without a word is refused here.
		listed := map[string]bool{}
		for _, entry := range block.Fields {
			idx := slices.IndexFunc(decl.Fields, func(f framework.FieldDeclaration) bool { return f.Name == entry })
			if idx < 0 {
				return fmt.Errorf("app spec: screen %q entity_form field %q is not defined on entity %q", screenName, entry, block.Entity)
			}
			field := decl.Fields[idx]
			switch CreateFormFieldOmission(field) {
			case CreateFormFieldHidden:
				return fmt.Errorf("app spec: screen %q entity_form field %q is hidden on entity %q: a form cannot draw it", screenName, entry, block.Entity)
			case CreateFormFieldReadOnly:
				if mode != "edit" {
					return fmt.Errorf("app spec: screen %q entity_form field %q is read_only or auto_generate on entity %q: a create form cannot draw it", screenName, entry, block.Entity)
				}
			case CreateFormFieldServerFilled:
				if mode != "edit" {
					return fmt.Errorf("app spec: screen %q entity_form field %q is a column the server fills: a create form cannot draw it", screenName, entry)
				}
			}
			if listed[entry] {
				return fmt.Errorf("app spec: screen %q entity_form field %q is listed twice", screenName, entry)
			}
			listed[entry] = true
		}
	case "entity_detail":
		if block.Entity == "" {
			return fmt.Errorf("app spec: screen %q entity_detail block entity is required", screenName)
		}
		decl, ok := entities[block.Entity]
		if !ok {
			return fmt.Errorf("app spec: screen %q entity_detail targets unknown entity %q", screenName, block.Entity)
		}
		if !EntityDeclarationCRUDEnabled(decl) {
			return fmt.Errorf("app spec: screen %q entity_detail target %q must enable crud", screenName, block.Entity)
		}
		// The record page draws from the entity's own display: block, so
		// a text: or fields: here would be accepted and then never render.
		if block.Text != "" || len(block.Fields) > 0 {
			return fmt.Errorf("app spec: screen %q entity_detail takes no text or fields: the record page draws the entity's display: block, so name the record with display.title_fields and lay out its fields with display.form", screenName)
		}
	case "entity_create":
		return fmt.Errorf("app spec: screen %q uses entity_create, which is no longer a block kind: the list's create: synthesizes the create screen at <list>/create", screenName)
	case "entity_edit":
		return fmt.Errorf("app spec: screen %q uses entity_edit, which is no longer a block kind: entity_detail's record page holds the edit form", screenName)
	case "login_form", "signup_form":
		// Static HTML auth form posting to the auth battery.
		if v, ok := block.Props["action"]; ok {
			action, isStr := v.(string)
			if !isStr {
				return fmt.Errorf("app spec: screen %q %s action must be a string", screenName, kind)
			}
			// Same predicate the renderer applies (ui.Form runs Action
			// through the html.Form primitive's setURLAttr guard, which
			// degrades an unsafe scheme to a form that goes nowhere).
			// Safe, but silent: the author asked for a form and got one
			// that does not post. Reject here instead, the way every
			// sibling problem in this validator does, so validate and
			// render cannot disagree about what a safe form action is.
			if !urlsafe.OK(action, urlsafe.Anchor) {
				return fmt.Errorf("app spec: screen %q %s action %q is not a safe form action: use an http(s) or root-relative URL", screenName, kind, action)
			}
		}
		// The footer href becomes an anchor on the login/signup screen. The
		// emitter routes it through ui.Link, which degrades an unsafe scheme to
		// a dead link. Safe, but silent: the author asked for a link and got
		// one that goes nowhere. Reject here instead, the way every sibling
		// problem in this validator does. urlsafe.Anchor is the SAME predicate
		// the renderer applies (http(s), relative, fragment, mailto, tel), so
		// validate and render cannot disagree about what is safe.
		for _, key := range []string{"register_href", "login_href", "forgot_href"} {
			href, _ := block.Props[key].(string)
			if href == "" {
				continue
			}
			if !urlsafe.OK(href, urlsafe.Anchor) {
				return fmt.Errorf("app spec: screen %q %s %s %q is not a safe link: use an http(s), root-relative, fragment, mailto or tel URL", screenName, kind, key, href)
			}
		}
	case "bar_chart", "pie_chart", "line_chart":
		// Charts render from grouped entity data; without a valid source
		// the block would silently vanish from the page. Reject upfront.
		src, ok := block.Props["source"].(map[string]any)
		if !ok {
			return fmt.Errorf("app spec: screen %q %s requires source: {entity: ..., group_by: ...}", screenName, kind)
		}
		srcEntity, _ := src["entity"].(string)
		groupBy, _ := src["group_by"].(string)
		if srcEntity == "" || groupBy == "" {
			return fmt.Errorf("app spec: screen %q %s source needs both entity and group_by", screenName, kind)
		}
		// The generated chart and its boot check (entityui.CheckChart)
		// take the name as written, so a route-style "/customers" is an
		// unknown entity there: refuse it here, naming the spelling.
		if t := strings.Trim(srcEntity, "/"); t != srcEntity {
			return fmt.Errorf("app spec: screen %q %s source entity %q is a route, not an entity name: write %q", screenName, kind, srcEntity, t)
		}
		decl, known := entities[srcEntity]
		if !known {
			return fmt.Errorf("app spec: screen %q %s source targets unknown entity %q", screenName, kind, srcEntity)
		}
		if !EntityDeclarationCRUDEnabled(decl) {
			return fmt.Errorf("app spec: screen %q %s source entity %q must enable crud (the chart reads its rows via the CRUD handler)", screenName, kind, srcEntity)
		}
		// group_by is the chart's LABEL, not an aggregate: groupCounts
		// prints each distinct stored value as a bar or slice label. Pointed at a masked column that renders the stored
		// value verbatim on the page while the API masks it: the full value
		// set, not the one-bit oracle the stat_card filter guard below closes.
		// Masking groupCounts instead would collapse every row into a single
		// "****" bucket, so the bar belongs here.
		if err := requireGroupableColumn(screenName, kind, "group_by", groupBy,
			"the chart renders each distinct stored value as a bar or slice label", decl); err != nil {
			return err
		}
		// entityui.CheckChart refuses at boot a column the database does
		// not group: a json field, and a many-to-many relation, which is
		// a link table rather than a column. The chart drew nothing.
		if col, _ := appSpecColumn(decl, groupBy); col != nil {
			if t := strings.ToLower(strings.TrimSpace(col.Type)); t == "json" || (t == "relation" && col.Many) {
				return fmt.Errorf("app spec: screen %q %s group_by %q is a %s field, which the chart cannot group by", screenName, kind, groupBy, appSpecGroupKind(col))
			}
		}
		// Then the boot check itself, over the fields the generated entity
		// registers, so a chart the load passes never panics at boot.
		fields, err := AppSpecEntityFields(decl)
		if err != nil {
			return fmt.Errorf("app spec: entity %q: %w", srcEntity, err)
		}
		if err := entityui.CheckChartFields(srcEntity, fields, groupBy); err != nil {
			return fmt.Errorf("app spec: screen %q %s %q: %w", screenName, kind, AppSpecBlockName(block, "title", srcEntity), err)
		}
	case "stat_card":
		if err := validateCatalogRequired(screenName, "stat_card", block); err != nil {
			return err
		}
		// A stat_card with a source binds to live entity data; without a
		// registered CRUD handler statValue would render a silent "—".
		// Reject an unknown or crud-disabled source upfront. (A static
		// value: stat_card has no source and is fine.)
		if src, ok := block.Props["source"].(map[string]any); ok {
			srcEntity, _ := src["entity"].(string)
			if srcEntity == "" {
				return fmt.Errorf("app spec: screen %q stat_card source needs entity", screenName)
			}
			// As for a chart: entityui.CheckStat takes the name as written.
			if t := strings.Trim(srcEntity, "/"); t != srcEntity {
				return fmt.Errorf("app spec: screen %q stat_card source entity %q is a route, not an entity name: write %q", screenName, srcEntity, t)
			}
			decl, known := entities[srcEntity]
			if !known {
				return fmt.Errorf("app spec: screen %q stat_card source targets unknown entity %q", screenName, srcEntity)
			}
			if !EntityDeclarationCRUDEnabled(decl) {
				return fmt.Errorf("app spec: screen %q stat_card source entity %q must enable crud", screenName, srcEntity)
			}
			// source.filter is split on "=" and handed to CountAll as a raw
			// ParsedFilter, bypassing ParseFilters, the same unvalidated
			// route entity_list facets take. A Hidden or NoQuery column there
			// turns the rendered count into a one-bit oracle, so bar it here,
			// where entity_list's filters: are barred.
			// src["filter"], not block.Props["filter"]: the filter lives in
			// the nested source map, which is what statValue reads. Checking
			// the outer props meant this guard never fired.
			if f, ok := src["filter"].(string); ok && f != "" {
				col := strings.TrimSpace(strings.SplitN(f, "=", 2)[0])
				for i := range decl.Fields {
					if decl.Fields[i].Name != col {
						continue
					}
					if decl.Fields[i].Hidden {
						return fmt.Errorf("app spec: screen %q stat_card filter column %q is hidden (the count would disclose its values)", screenName, col)
					}
					if decl.Fields[i].NoQuery {
						return fmt.Errorf("app spec: screen %q stat_card filter column %q is no_query (the count would match on the stored value)", screenName, col)
					}
					break
				}
				if err := validateStatCardFilterValues(screenName, AppSpecBlockName(block, "label", srcEntity), decl, f); err != nil {
					return err
				}
			}
			// agg is StatValue's exact spelling: count (or empty) or sum. sum
			// renders the total of a numeric column. At one row that total IS
			// the stored value, so the masked-column bar applies here too.
			agg, _ := src["agg"].(string)
			// The generated app runs entityui.CheckStat on the card at
			// boot, which refuses a count given a field or a format and a
			// sum formatted other than "money": StatValue ignores those
			// props, so the card would draw a plain count where a sum or
			// money was asked for. Refuse the same shapes here.
			format := AppSpecProp(block, "format")
			switch agg {
			case "", "count":
				if f, _ := src["field"].(string); f != "" {
					return fmt.Errorf("app spec: screen %q stat_card counts rows and takes no source.field (got %q); set agg: sum to total it", screenName, f)
				}
				if format != "" {
					return fmt.Errorf("app spec: screen %q stat_card counts rows and takes no format (got %q)", screenName, format)
				}
			case "sum":
				if format != "" && format != "money" {
					return fmt.Errorf("app spec: screen %q stat_card format %q is not money (or empty)", screenName, format)
				}
				f, _ := src["field"].(string)
				if err := requireGroupableColumn(screenName, "stat_card", "source.field", f,
					"the card renders the total of the stored values, which at one row is the value itself", decl); err != nil {
					return err
				}
				if col, _ := appSpecColumn(decl, f); col == nil || !appSpecNumericType(col.Type) {
					return fmt.Errorf("app spec: screen %q stat_card sums source.field %q, which is not an int, float or decimal field", screenName, f)
				}
			default:
				return fmt.Errorf("app spec: screen %q stat_card source agg %q is not count or sum", screenName, agg)
			}
			// Then the boot check itself, over the fields the generated
			// entity registers, so a card the load passes never panics at
			// boot. The checks above refuse the common mistakes in the
			// spec's own words first.
			fields, err := AppSpecEntityFields(decl)
			if err != nil {
				return fmt.Errorf("app spec: entity %q: %w", srcEntity, err)
			}
			srcField, _ := src["field"].(string)
			filter, _ := src["filter"].(string)
			if err := entityui.CheckStatFields(srcEntity, fields, agg, srcField, filter, format); err != nil {
				return fmt.Errorf("app spec: screen %q stat_card %q: %w", screenName, AppSpecBlockName(block, "label", srcEntity), err)
			}
		}
	case "stack", "cluster", "grid", "stat_grid":
		if err := validateAppSpecLayoutBlock(screenName, kind, block.Props); err != nil {
			return err
		}
	case "page_header", "hero", "card", "stat_row",
		"link_button", "callout",
		"markdown", "pricing", "divider":
		// framework/ui catalog blocks. Props are validated leniently (the
		// generator reads only the props each component understands),
		// except what a component panics without at render.
		if err := validateCatalogRequired(screenName, strings.ToLower(strings.TrimSpace(kind)), block); err != nil {
			return err
		}
	case "action_button", "custom_form", "text_field", "number_field",
		"checkbox", "switch", "select_field", "radio_group", "data_table",
		"item_list", "nav_links":
		if err := validateAppSpecControlBlock(screenName, strings.ToLower(strings.TrimSpace(kind)), block); err != nil {
			return err
		}
	default:
		return fmt.Errorf("app spec: screen %q has unsupported block type %q", screenName, kind)
	}
	// A node-tree block hands its whole subtree to the node renderer, which
	// draws only node kinds: a catalog block anywhere under it would render
	// as an "unknown kind" comment, so it is refused here.
	if !AppSpecCatalogKind(block.Kind) && AppSpecBlockUsesNodeRenderer(block) {
		if inner := appSpecCatalogUnderNode(block.Children); inner != "" {
			return fmt.Errorf("app spec: screen %q %s block cannot hold a %s block: node blocks draw only node kinds; put it in a stack, cluster, card or section", screenName, kind, inner)
		}
	}
	for _, child := range block.Children {
		if err := validateAppSpecBlock(screenName, entities, child); err != nil {
			return err
		}
	}
	return nil
}

// validateEntityListFilter guards an entity_list's pinned filter
// (`filter: field=value`, stat_card's spelling): exactly one `=`, both
// sides non-empty bare words, the field defined on the entity (its camel
// JSON spelling resolves to the column) and neither hidden nor no_query.
// The pin lands on entityui's ListBuilder.Where, which pins field =
// value on every read inside the caller's scope — the same one-bit
// oracle the stat_card filter guard closes, so the same columns are
// barred here. So is a column whose type takes no equality filter (a
// json field): crud refuses the pin on every read, whatever the value.
func validateEntityListFilter(screenName string, decl framework.EntityDeclaration, filter string) error {
	if filter == "" {
		return nil
	}
	if strings.Contains(filter, " ") || strings.Count(filter, "=") != 1 {
		return fmt.Errorf("app spec: screen %q entity_list filter %q is not the pinned spelling; use field=value", screenName, filter)
	}
	field, value, _ := strings.Cut(filter, "=")
	if field == "" || value == "" || !statFilterWord.MatchString(field) || !statFilterWord.MatchString(value) {
		return fmt.Errorf("app spec: screen %q entity_list filter %q is not the pinned spelling; use field=value", screenName, filter)
	}
	if f, ok := EntityListPinColumn(decl, field); ok {
		if f.Hidden {
			return fmt.Errorf("app spec: screen %q entity_list filter column %q is hidden (the pinned list would disclose its values)", screenName, f.Name)
		}
		if f.NoQuery {
			return fmt.Errorf("app spec: screen %q entity_list filter column %q is no_query (the pinned list would match on the stored value)", screenName, f.Name)
		}
		sf, err := f.Field()
		if err != nil {
			return fmt.Errorf("app spec: screen %q entity_list filter column %q: %w", screenName, f.Name, err)
		}
		if !fwfilter.OpSuitsType(fwfilter.OpEq, sf.Type) {
			return fmt.Errorf("app spec: screen %q entity_list filter column %q is a %s field, which no filter can match on", screenName, f.Name, f.Type)
		}
		return validateFilterValue(screenName, "entity_list filter", f, value, true)
	}
	return fmt.Errorf("app spec: screen %q entity_list filter column %q is not defined on entity %q", screenName, field, decl.Name)
}

// EntityListPinColumn resolves an entity_list pin's field to the
// declared column: its name, or its camel JSON spelling. The load
// checks the pin through it and the generator emits the column it
// returns in .Where(column, value), so the two cannot resolve
// differently. The pin binds through ListBuilder.Where, which takes
// the column, so the camel spelling is resolved here, never at runtime.
func EntityListPinColumn(decl framework.EntityDeclaration, field string) (framework.FieldDeclaration, bool) {
	for _, f := range decl.Fields {
		if f.Name == field || ToCamelJSON(f.Name) == field {
			return f, true
		}
	}
	return framework.FieldDeclaration{}, false
}

// EntityListColumn resolves one entry of an entity_list's `fields:` to
// the declared column it names: by its name or its camel JSON spelling
// (EntityListPinColumn, the pin's resolution), or the generated "id" of
// an entity keyed on it. The load checks each entry through it and the
// generator emits the column it returns in .Columns(...), because
// entityui's list resolves columns by declared name: the camel spelling
// emitted as written failed the whole list.
func EntityListColumn(decl framework.EntityDeclaration, field string) (framework.FieldDeclaration, bool) {
	if f, ok := EntityListPinColumn(decl, field); ok {
		return f, true
	}
	if field == "id" && DeclKeyIsID(decl) {
		return framework.FieldDeclaration{Name: "id"}, true
	}
	return framework.FieldDeclaration{}, false
}

// validateFilterValue refuses a filter literal its column cannot hold.
// The entity_list pin and the stat_card filter both bind through crud's
// filter coercion on every read, so a value the column's type cannot
// hold (yes on a bool) fails every load: a 400 on the list, "—" on the
// card. With member set (an equality, inequality or in comparison) an
// enum value outside the field's values is refused too: it matches no
// row, or every row.
func validateFilterValue(screenName, what string, f fwentity.FieldDeclaration, value string, member bool) error {
	sf, err := f.Field()
	if err != nil {
		return fmt.Errorf("app spec: screen %q %s column %q: %w", screenName, what, f.Name, err)
	}
	if _, err := fwfilter.CoerceValue(f.Name, sf.Type, value); err != nil {
		return fmt.Errorf("app spec: screen %q %s value %q does not fit %s column %q", screenName, what, value, f.Type, f.Name)
	}
	if member && len(sf.Values) > 0 && !slices.Contains(sf.Values, value) {
		return fmt.Errorf("app spec: screen %q %s value %q is not one of enum column %q's values %v", screenName, what, value, f.Name, sf.Values)
	}
	return nil
}

// validateStatCardFilterValues parses a stat_card's source.filter (the
// DSL predicate entityui.StatValue parses on every render) over the
// fields the generated entity registers, the parse entityui.CheckStat
// runs at boot: an unknown column, a camel spelling (no generated field
// sets a WireName), an operator the parser does not know and a literal
// its column's type cannot hold are refused, naming the card and the
// parse error. Then each =, != and in comparison on an enum column must
// name one of the column's values: it matches no row, or every row,
// otherwise.
func validateStatCardFilterValues(screenName, card string, decl framework.EntityDeclaration, filter string) error {
	fields, err := AppSpecEntityFields(decl)
	if err != nil {
		return fmt.Errorf("app spec: entity %q: %w", decl.Name, err)
	}
	pred, err := fwdsl.ParsePredicate(filter, fields)
	if err != nil {
		return fmt.Errorf("app spec: screen %q stat_card %q filter %q: %w", screenName, card, filter, err)
	}
	if pred == nil {
		return nil
	}
	byName := make(map[string]fwentity.FieldDeclaration, len(decl.Fields))
	for _, f := range decl.Fields {
		byName[f.Name] = f
	}
	var walk func(p *fwfilter.Predicate) error
	walk = func(p *fwfilter.Predicate) error {
		for i := range p.Children {
			if err := walk(&p.Children[i]); err != nil {
				return err
			}
		}
		// A column Define adds (id, the timestamps) is no enum, and the
		// parse already held its literal to its type.
		f, ok := byName[p.Field]
		if len(p.Children) > 0 || !ok {
			return nil
		}
		switch p.Op {
		case fwfilter.OpIn:
			for _, v := range p.Values {
				if err := validateFilterValue(screenName, "stat_card filter", f, v, true); err != nil {
					return err
				}
			}
		case fwfilter.OpEq, fwfilter.OpNe:
			return validateFilterValue(screenName, "stat_card filter", f, p.Value, true)
		}
		return nil
	}
	return walk(pred)
}

// AppSpecEntityFields returns the fields the generated app's entity
// holds once it registers: the declaration's fields (the owner column
// the loader adds among them) and the system columns entity.Define
// injects that the declaration does not name, the id key, created_at
// and updated_at unless timestamps is false, and deleted_at under
// soft_delete. It runs the framework's own injector
// (EntityConfig.WithSystemFields), so the load and the boot see one
// field list. No generated field sets a WireName: a filter names the
// column.
func AppSpecEntityFields(decl framework.EntityDeclaration) ([]schema.Field, error) {
	cfg, err := decl.Config()
	if err != nil {
		return nil, err
	}
	return cfg.WithSystemFields().Fields, nil
}

// AppSpecBlockName is the name a generated screen's boot check gives a
// stat card or chart: its label or title prop, else the source entity.
func AppSpecBlockName(block AppSpecBlock, prop, entity string) string {
	if name := AppSpecProp(block, prop); name != "" {
		return name
	}
	return entity
}

// appSpecCatalogUnderNode returns the first kind among blocks the node
// renderer draws that it does not know: a catalog kind, an entity screen
// (entity_list, entity_form, entity_detail) or an auth form, each of which
// it would draw as an "unknown kind" comment. section is a node kind as
// well as a catalog one, so it renders either way.
func appSpecCatalogUnderNode(blocks []AppSpecBlock) string {
	for _, b := range blocks {
		if AppSpecCatalogKind(b.Kind) && !strings.EqualFold(strings.TrimSpace(b.Kind), "section") {
			return b.Kind
		}
		kind := b.Kind
		if kind == "" {
			kind = b.Type
		}
		switch strings.ToLower(strings.TrimSpace(kind)) {
		case "entity_list", "entity_form", "entity_detail", "login_form", "signup_form":
			return kind
		}
		if inner := appSpecCatalogUnderNode(b.Children); inner != "" {
			return inner
		}
	}
	return ""
}

func validateAppSpecLayoutBlock(screenName, kind string, props map[string]any) error {
	allowed := func(value string, values ...string) bool {
		if value == "" {
			return true
		}
		for _, candidate := range values {
			if value == candidate {
				return true
			}
		}
		return false
	}
	stringProp := func(name string) (string, error) {
		value, ok := props[name]
		if !ok {
			return "", nil
		}
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("app spec: screen %q %s prop %q must be a string", screenName, kind, name)
		}
		return strings.ToLower(strings.TrimSpace(text)), nil
	}
	gap, err := stringProp("gap")
	if err != nil {
		return err
	}
	if !allowed(gap, "none", "xs", "sm", "md", "lg", "xl", "2xl") {
		return fmt.Errorf("app spec: screen %q %s gap %q is not a design token", screenName, kind, gap)
	}
	if kind == "stack" || kind == "cluster" {
		align, err := stringProp("align")
		if err != nil {
			return err
		}
		if !allowed(align, "start", "center", "end", "baseline", "stretch") {
			return fmt.Errorf("app spec: screen %q %s align %q is unsupported", screenName, kind, align)
		}
		justify, err := stringProp("justify")
		if err != nil {
			return err
		}
		if !allowed(justify, "start", "center", "end", "between", "around") {
			return fmt.Errorf("app spec: screen %q %s justify %q is unsupported", screenName, kind, justify)
		}
	}
	if kind == "cluster" {
		if value, ok := props["no_wrap"]; ok {
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("app spec: screen %q cluster prop %q must be a boolean", screenName, "no_wrap")
			}
		}
	}
	if kind == "grid" || kind == "stat_grid" {
		if _, err := stringProp("min"); err != nil {
			return err
		}
	}
	return nil
}

func validateAppSpecActions(screenName string, blocks []AppSpecBlock) error {
	names := map[string]bool{}
	var walk func(AppSpecBlock) error
	walk = func(block AppSpecBlock) error {
		events := map[string]bool{}
		for i, action := range block.Actions {
			name := strings.TrimSpace(action.Name)
			event := AppSpecActionEvent(action)
			if name == "" {
				name = event
			}
			if !isGoFastrActionEvent(event) {
				return fmt.Errorf("app spec: screen %q action %q event %q is not supported", screenName, name, event)
			}
			if events[event] {
				return fmt.Errorf("app spec: screen %q duplicate event %q on one block", screenName, event)
			}
			events[event] = true
			if event == "click" && len(block.Actions) > 1 && i != 0 {
				return fmt.Errorf("app spec: screen %q click action must be first when combined with other actions", screenName)
			}
			if names[name] {
				return fmt.Errorf("app spec: screen %q duplicate action %q", screenName, name)
			}
			names[name] = true
			if action.ClientJS == "" {
				return fmt.Errorf("app spec: screen %q action %q client_js is required", screenName, name)
			}
		}
		for _, child := range block.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, block := range blocks {
		if err := walk(block); err != nil {
			return err
		}
	}
	return nil
}

func isGoFastrActionEvent(event string) bool {
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "click", "input", "change", "submit":
		return true
	default:
		return false
	}
}

// appSpecGroupKind names a field a chart cannot group by, for the
// refusal: "json" or "many-to-many relation".
func appSpecGroupKind(col *framework.FieldDeclaration) string {
	if col.Many {
		return "many-to-many relation"
	}
	return "json"
}

// appSpecNumericType reports a declared field type a sum can total.
func appSpecNumericType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "int", "integer", "float", "number", "decimal":
		return true
	}
	return false
}

// requireGroupableColumn refuses a screen column whose stored values would be
// rendered or matched on: a chart's group_by label, a summed stat_card field.
// Hidden and no_query are both barred, for the reasons stated at each call
// site.
func requireGroupableColumn(screenName, kind, key, col, why string, decl framework.EntityDeclaration) error {
	found, ok := appSpecColumn(decl, col)
	if !ok {
		return fmt.Errorf("app spec: screen %q %s %s %q is not defined on entity %q", screenName, kind, key, col, decl.Name)
	}
	if found == nil {
		return nil // framework-managed column; carries no flags
	}
	if found.Hidden {
		return fmt.Errorf("app spec: screen %q %s %s %q is hidden (it is not in the projection, so every row would group under the same empty value)", screenName, kind, key, col)
	}
	if found.NoQuery {
		return fmt.Errorf("app spec: screen %q %s %s %q is no_query (%s)", screenName, kind, key, col, why)
	}
	return nil
}

// appSpecRelationKeys refuses at load the relation shapes
// entity.CheckRelationKeys refuses when the generated app boots: a
// foreign key needs one column per column of the key it references, in
// key order, and a key of several columns names each one as a declared
// field of the entity holding it. A many_to_many links two one-column
// keys; a composite end goes through a join entity instead.
func appSpecRelationKeys(decl framework.EntityDeclaration, rel framework.Relation, target framework.EntityDeclaration) error {
	keyOf := func(d framework.EntityDeclaration) []string {
		if DeclKeyIsID(d) {
			return []string{"id"}
		}
		return d.PrimaryKey
	}
	holder, ref := decl, target
	switch rel.Type {
	case fwentity.RelManyToMany:
		if len(keyOf(decl)) > 1 || len(keyOf(target)) > 1 {
			return fmt.Errorf("app spec: entity %q relation %q is many_to_many, and a many_to_many links two one-column keys: declare a join entity that belongs to both sides instead", decl.Name, rel.Name)
		}
		return nil
	case fwentity.RelHasOne, fwentity.RelHasMany:
		holder, ref = target, decl
	}
	fks, key := rel.ForeignKey, keyOf(ref)
	if len(fks) == 0 {
		return nil
	}
	if len(fks) != len(key) {
		return fmt.Errorf("app spec: entity %q relation %q names foreign key (%s), and %q is keyed on (%s): name one foreign-key column per key column, in key order", decl.Name, rel.Name, strings.Join(fks, ", "), ref.Name, strings.Join(key, ", "))
	}
	if len(fks) == 1 {
		return nil
	}
	for i, c := range fks {
		if slices.ContainsFunc(fks[:i], func(p string) bool { return strings.EqualFold(p, c) }) {
			return fmt.Errorf("app spec: entity %q relation %q names foreign-key column %q twice", decl.Name, rel.Name, c)
		}
		if !slices.ContainsFunc(holder.Fields, func(f framework.FieldDeclaration) bool { return f.Name == c }) {
			return fmt.Errorf("app spec: entity %q relation %q names foreign-key column %q, which is not a declared field of %q: declare each column of a multi-column foreign key", decl.Name, rel.Name, c, holder.Name)
		}
		if st := holder.States; st != nil && (st.Field == c || slices.ContainsFunc(st.Transitions, func(t fwentity.Transition) bool { return t.Stamp == c })) {
			return fmt.Errorf("app spec: entity %q relation %q names foreign-key column %q, which %q's transitions write: a move would change the reference with no check, so keep state fields out of multi-column foreign keys", decl.Name, rel.Name, c, holder.Name)
		}
	}
	// The migrator refuses an integer column against a text key column
	// and the reverse; refuse it here, before the app is generated.
	for i, c := range fks {
		f, _ := appSpecColumn(holder, c)
		k, _ := appSpecColumn(ref, key[i])
		if f == nil || k == nil {
			continue
		}
		if ft, kt := appSpecTypeFamily(f), appSpecTypeFamily(k); ft != "" && kt != "" && ft != kt {
			return fmt.Errorf("app spec: entity %q relation %q names foreign-key column %q, typed %s, for key column %q of %q, typed %s: type each column like the key column it references", decl.Name, rel.Name, c, f.Type, key[i], ref.Name, k.Type)
		}
	}
	return nil
}

// appSpecTypeFamily groups a declared field by the Postgres column the
// migrator gives it, as its foreign-key type check does: every integer
// width is one family, text of any length (uuid, enum, image and file
// among it) another, and any other type its own. A relation holds its
// target's key, and a type the loader refuses elsewhere has no column,
// so both are "" and the check skips them.
func appSpecTypeFamily(f *framework.FieldDeclaration) string {
	if appSpecIntType(f.Type) {
		return "integer"
	}
	sf, err := f.Field()
	if err != nil || sf.Type == schema.Relation {
		return ""
	}
	t := fwmigrate.SQLType(sf, fwmigrate.DialectPostgres)
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = t[:i]
	}
	switch strings.ToUpper(t) {
	case "SMALLINT", "INTEGER", "INT", "BIGINT", "SERIAL", "BIGSERIAL":
		return "integer"
	case "TEXT", "VARCHAR", "CHAR", "CHARACTER VARYING":
		return "text"
	}
	return strings.ToLower(t)
}

// validateCreateFormCoversRequired refuses a create entity_form whose
// fields: list leaves out a field the create handler requires. The
// generator draws a control only for the listed fields, and the handler
// answers 400 for a Required field with no Default that the body lacks
// (schema.ValidateAll), so such a form could never submit. Exempt are the
// fields the server fills before that check: the system columns, an
// auto-generated, Hidden or ReadOnly field, the owner column (stamped
// from the request; app specs refuse multi_tenant), the state field and
// its stamps (the first Initial value, or a move), and every field of an
// entity with a before_create hook, which may set it. An edit form sends
// a partial update, so it is not checked.
func validateCreateFormCoversRequired(screenName string, entities map[string]framework.EntityDeclaration, hooks []AppSpecHook, block AppSpecBlock) error {
	if !IsEntityFormBlock(block) || IsEditFormBlock(block) || len(block.Fields) == 0 {
		return nil
	}
	decl, ok := entities[block.Entity]
	if !ok {
		return nil // validateAppSpecBlock names the unknown entity
	}
	for _, hook := range hooks {
		if hook.Entity == decl.Name && hook.When == "before_create" {
			return nil
		}
	}
	serverSet := map[string]bool{"id": true, "created_at": true, "updated_at": true, "deleted_at": true}
	if decl.Scope != nil && decl.Scope.OwnerField != "" {
		serverSet[decl.Scope.OwnerField] = true
	}
	for _, col := range decl.States.Guarded() {
		serverSet[col] = true
	}
	var missing []string
	for _, field := range decl.Fields {
		if !field.Required || field.Default != nil || serverSet[field.Name] ||
			FieldAutoGenerated(field) || field.Hidden || field.ReadOnly ||
			slices.Contains(block.Fields, field.Name) {
			continue
		}
		missing = append(missing, strconv.Quote(field.Name))
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("app spec: screen %q entity_form creates %q but its fields: leave out required %s, which has no default: every submit would answer 400. List it in fields: (a relation draws a picker), give it a default, or drop fields: to draw every field", screenName, decl.Name, strings.Join(missing, ", "))
}
