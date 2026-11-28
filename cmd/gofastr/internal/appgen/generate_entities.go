package appgen

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
)

// RenderGeneratedProject emits the entities package for a set of entity
// declarations. Output is ONE FILE PER ENTITY plus thin fixed seams:
//
//   - entities/register.go: the RegisterAll seam. It is byte-identical for
//     every project regardless of entity count; each entity file appends
//     itself to registrars in init(), so adding an entity means adding one
//     new file and never editing an existing one.
//   - entities/events.go: the event-helper seam (onEntityEvent,
//     onEntityDeleted, extractEntityRecord), also byte-identical regardless
//     of entity count; each entity file's On<Camel>* wrappers delegate to it.
//   - entities/<snake_name>.go: everything for one entity: model struct,
//     column constants, typed repo, event wrappers, and its registration func.
//   - client/client.go: a standalone typed HTTP client (separate package).
//
// Declaration order is carried by the registrar.order field so RegisterAll
// registers entities in app spec order and gofastr pack can recover it.
func RenderGeneratedProject(decls []framework.EntityDeclaration) ([]GeneratedFile, error) {
	return renderGeneratedProjectWithOrder(decls, 0)
}

// renderGeneratedProjectWithOrder is the additive-generation variant: the
// order assigned to each entity is orderOffset+i, so entities added via --add
// to a project that already has N entities (orders 0..N-1) continue at N, N+1,
// … keeping RegisterAll's declaration order and pack recovery coherent.
func renderGeneratedProjectWithOrder(decls []framework.EntityDeclaration, orderOffset int) ([]GeneratedFile, error) {
	// Validate up front so a bad entity fails fast, with its name in the
	// error, before any file is emitted.
	for _, decl := range decls {
		if _, err := decl.Config(); err != nil {
			return nil, err
		}
		if len(decl.Endpoints) > 0 {
			return nil, fmt.Errorf("entity %q: code generation does not support endpoints: endpoints require Go handlers and must be wired in code (use app.Entity with EntityConfig.Endpoints)", decl.Name)
		}
	}
	// renderClient emits move methods and their doc comments straight from
	// these declarations, so its input must have passed the states boot check
	// (the reserved-name and grammar refusals live there, one implementation
	// in framework/entity): the Config() call above checks shape, not states,
	// and a hand-edited declaration could carry a key or an enum value no
	// emitter may render.
	for _, decl := range decls {
		if err := validateDeclarationStates(decl); err != nil {
			return nil, fmt.Errorf("entity %q: %w", decl.Name, err)
		}
	}
	// The client is the client generator's derived output, stamped as
	// such here, so the first copy matches every later rebuild.
	clientSrc, err := renderAppClient(decls)
	if err != nil {
		return nil, err
	}
	files := []GeneratedFile{
		{Name: "register.go", Content: RenderRegisterSeam()},
		// Event helpers are entity-independent: one fixed seam file instead
		// of a copy of each body per entity file. Additive generation writes
		// it only when absent (the skip-existing partition).
		{Name: "events.go", Content: renderEventHelpers()},
		// Generated client lives in its own package so consumers can import
		// it without dragging the server-side schema/framework deps along.
		{Name: "client/client.go", Content: clientSrc},
	}
	// Preserve the caller's entity order (the app spec's authored order) so
	// scaffolds are reproducible byte-for-byte and gofastr pack round-trips.
	for i, decl := range decls {
		content, err := renderEntityFile(decl, orderOffset+i, decls)
		if err != nil {
			return nil, fmt.Errorf("entity %q: %w", decl.Name, err)
		}
		files = append(files, GeneratedFile{Name: entityFileName(decl.Name), Content: content})
	}
	if err := refuseDuplicateDecls(files); err != nil {
		return nil, err
	}
	return files, nil
}

// RenderRegisterSeam emits entities/register.go, the fixed registration seam.
// It carries no entity-specific text, so its bytes are identical whether the
// project declares one entity or a hundred.
func RenderRegisterSeam() string {
	return `package entities

import (
	"sort"

	"github.com/DonaldMurillo/gofastr/framework"
)

// registrar pairs an entity's registration func with its declaration order.
// Each entity file appends one registrar in init(); RegisterAll runs them in
// declaration order, so the package behaves identically regardless of the
// lexical order Go runs each file's init() in. The order field is also how
// gofastr pack recovers the app spec's authored entity order from source.
type registrar struct {
	order int
	fn    func(app *framework.App)
}

var registrars []registrar

func boolPtr(v bool) *bool        { return &v }
func floatPtr(v float64) *float64 { return &v }

// RegisterAll registers every generated entity declaration with app, in
// declaration order. This file never holds an entity name: add an entity by
// dropping in a new entities/<name>.go that appends to registrars in init().
func RegisterAll(app *framework.App) {
	sort.SliceStable(registrars, func(i, j int) bool {
		return registrars[i].order < registrars[j].order
	})
	for _, r := range registrars {
		r.fn(app)
	}
}
`
}

// renderEntityFile emits the single entities/<snake_name>.go for one entity:
// its model struct, column constants, typed repository, event helpers, and a
// registration func that self-registers via init(). decls are the
// project's entities, for the types of cascade children's ids.
func renderEntityFile(decl framework.EntityDeclaration, order int, decls []framework.EntityDeclaration) (string, error) {
	var sb strings.Builder
	strconvImport := ""
	if intID(decl) {
		// The repo and the deleted event turn an integer id into the
		// string key CrudHandler takes, and back.
		strconvImport = "\t\"strconv\"\n"
	}
	sb.WriteString(`package entities

import (
	"context"
	"database/sql"
` + strconvImport + `
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

`)
	sb.WriteString(renderEntityModel(decl))
	sb.WriteString(renderEntityEnums(decl))
	sb.WriteString(renderEntityWriteShapes(decl, decls))
	sb.WriteString(renderEntityColumns(decl))
	sb.WriteString(renderEntityRepo(decl))
	sb.WriteString(renderEntityEvents(decl))
	registration, err := renderEntityRegistrationFunc(decl, order)
	if err != nil {
		return "", err
	}
	sb.WriteString(registration)
	return sb.String(), nil
}

// renderEntityRegistrationFunc wraps an entity's app.Entity(...) call in a
// register<Camel> func and self-registers it via init() so register.go never
// needs editing when an entity is added.
func renderEntityRegistrationFunc(decl framework.EntityDeclaration, order int) (string, error) {
	structName := appspec.ToCamelCase(decl.Name)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("// register%s registers the %q entity with app.\n", structName, decl.Name))
	sb.WriteString(fmt.Sprintf("func register%s(app *framework.App) {\n", structName))
	body, err := RenderEntityRegistration(decl)
	if err != nil {
		return "", err
	}
	sb.WriteString(body)
	sb.WriteString("}\n\n")
	sb.WriteString(fmt.Sprintf("func init() {\n\tregistrars = append(registrars, registrar{order: %d, fn: register%s})\n}\n", order, structName))
	return sb.String(), nil
}

func RenderEntityRegistration(decl framework.EntityDeclaration) (string, error) {
	var sb strings.Builder
	structName := appspec.ToCamelCase(decl.Name)
	sb.WriteString(fmt.Sprintf("\tapp.Entity(%q, framework.EntityConfig{\n", decl.Name))
	if decl.Table != "" {
		sb.WriteString(fmt.Sprintf("\t\tTable: %q,\n", decl.Table))
	}
	if len(decl.PrimaryKey) > 0 {
		sb.WriteString("\t\tPrimaryKey: []string{" + stringSliceLiteral(decl.PrimaryKey) + "},\n")
	}
	sb.WriteString("\t\tFields: []schema.Field{\n")
	for _, field := range decl.Fields {
		literal, err := renderFieldLiteral(field)
		if err != nil {
			return "", fmt.Errorf("field %q: %w", field.Name, err)
		}
		sb.WriteString("\t\t\t" + literal + ",\n")
	}
	sb.WriteString("\t\t},\n")
	if len(decl.Relations) > 0 {
		sb.WriteString("\t\tRelations: []framework.Relation{\n")
		for _, rel := range decl.Relations {
			sb.WriteString("\t\t\t" + renderRelationLiteral(rel) + ",\n")
		}
		sb.WriteString("\t\t},\n")
	}
	if scopeDeclHasContent(decl.Scope) {
		sb.WriteString("\t\tScope: &framework.ScopeConfig{\n")
		if decl.Scope.SoftDelete {
			sb.WriteString("\t\t\tSoftDelete: true,\n")
		}
		if decl.Scope.MultiTenant {
			sb.WriteString("\t\t\tMultiTenant: true,\n")
		}
		if decl.Scope.TenantField != "" {
			sb.WriteString(fmt.Sprintf("\t\t\tTenantField: %q,\n", decl.Scope.TenantField))
		}
		if decl.Scope.OwnerField != "" {
			sb.WriteString(fmt.Sprintf("\t\t\tOwnerField: %q,\n", decl.Scope.OwnerField))
		}
		if decl.Scope.CrossOwnerRead != "" {
			sb.WriteString(fmt.Sprintf("\t\t\tCrossOwnerRead: %q,\n", decl.Scope.CrossOwnerRead))
		}
		sb.WriteString("\t\t},\n")
	}
	if len(decl.SearchFields) > 0 {
		sb.WriteString("\t\tSearchFields: []string{")
		for i, sf := range decl.SearchFields {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%q", sf))
		}
		sb.WriteString("},\n")
	}
	if exposureDeclHasContent(decl.Exposure) {
		sb.WriteString("\t\tExposure: &framework.ExposureConfig{\n")
		if decl.Exposure.CRUD != nil {
			sb.WriteString(fmt.Sprintf("\t\t\tCRUD: boolPtr(%t),\n", *decl.Exposure.CRUD))
		}
		if decl.Exposure.MCP {
			sb.WriteString("\t\t\tMCP: true,\n")
		}
		if decl.Exposure.Public {
			sb.WriteString("\t\t\tPublic: true,\n")
		}
		if literal := renderAccessLiteral(decl.Exposure.Access); literal != "" {
			sb.WriteString("\t\t\tAccess: " + literal + ",\n")
		}
		if literal := renderReadScopeLiteral(decl.Exposure.ReadScope); literal != "" {
			sb.WriteString("\t\t\tReadScope: " + literal + ",\n")
		}
		sb.WriteString("\t\t},\n")
	}
	if paginationDeclHasContent(decl.Pagination) {
		sb.WriteString("\t\tPagination: &framework.PaginationConfig{\n")
		if decl.Pagination.CursorField != "" {
			sb.WriteString(fmt.Sprintf("\t\t\tCursorField: %q,\n", decl.Pagination.CursorField))
		}
		if len(decl.Pagination.CursorFields) > 0 {
			sb.WriteString("\t\t\tCursorFields: []string{")
			for i, field := range decl.Pagination.CursorFields {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%q", field))
			}
			sb.WriteString("},\n")
		}
		if decl.Pagination.MaxListLimit != 0 {
			sb.WriteString(fmt.Sprintf("\t\t\tMaxListLimit: %d,\n", decl.Pagination.MaxListLimit))
		}
		sb.WriteString("\t\t},\n")
	}
	if len(decl.Indices) > 0 {
		sb.WriteString("\t\tIndices: []framework.Index{\n")
		for _, idx := range decl.Indices {
			sb.WriteString("\t\t\t" + renderIndexLiteral(idx) + ",\n")
		}
		sb.WriteString("\t\t},\n")
	}
	if literal := renderStatesLiteral(decl.States); literal != "" {
		sb.WriteString("\t\tStates: " + literal + ",\n")
	}
	if literal := renderDisplayLiteral(decl.Display); literal != "" {
		sb.WriteString("\t\tDisplay: " + literal + ",\n")
	}
	if len(decl.Properties) > 0 {
		literal, err := renderGoLiteral(decl.Properties)
		if err != nil {
			return "", fmt.Errorf("properties: %w", err)
		}
		sb.WriteString("\t\tProperties: " + literal + ",\n")
	}
	sb.WriteString("\t}")
	if decl.Timestamps != nil {
		sb.WriteString(fmt.Sprintf(".WithTimestamps(%t)", *decl.Timestamps))
	}
	sb.WriteString(")\n")
	sb.WriteString(fmt.Sprintf("\t_ = %s{}\n", structName))
	return sb.String(), nil
}

// renderDisplayLiteral emits the Display group of an entity registration:
// every YAML-settable field, strings quoted, only non-zero fields, map keys
// sorted so the emitted Go is byte-stable. Empty string when the declaration
// carries no display config.
func renderDisplayLiteral(d *fwentity.DisplayConfig) string {
	if d == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("&framework.DisplayConfig{")
	first := true
	str := func(name, v string) {
		if v == "" {
			return
		}
		if !first {
			sb.WriteString(", ")
		}
		first = false
		fmt.Fprintf(&sb, "%s: %q", name, v)
	}
	str("Singular", d.Singular)
	str("Plural", d.Plural)
	if len(d.TitleFields) > 0 {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString("TitleFields: []string{" + stringSliceLiteral(d.TitleFields) + "}")
	}
	str("Description", d.Description)
	if len(d.Columns) > 0 {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString("Columns: []string{" + stringSliceLiteral(d.Columns) + "}")
	}
	if d.Nav != nil && *d.Nav != (fwentity.EntityNav{}) {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString("Nav: &framework.EntityNav{")
		var parts []string
		if d.Nav.Group != "" {
			parts = append(parts, fmt.Sprintf("Group: %q", d.Nav.Group))
		}
		if d.Nav.Icon != "" {
			parts = append(parts, fmt.Sprintf("Icon: %q", d.Nav.Icon))
		}
		if d.Nav.Order != 0 {
			parts = append(parts, fmt.Sprintf("Order: %d", d.Nav.Order))
		}
		if d.Nav.Hide {
			parts = append(parts, "Hide: true")
		}
		if d.Nav.HideCount {
			parts = append(parts, "HideCount: true")
		}
		sb.WriteString(strings.Join(parts, ", "))
		sb.WriteString("}")
	}
	if len(d.Views) > 0 {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		var parts []string
		for _, v := range d.Views {
			var fields []string
			fields = append(fields, fmt.Sprintf("Key: %q", v.Key))
			if v.Label != "" {
				fields = append(fields, fmt.Sprintf("Label: %q", v.Label))
			}
			if v.Where != "" {
				fields = append(fields, fmt.Sprintf("Where: %q", v.Where))
			}
			if v.Sort != "" {
				fields = append(fields, fmt.Sprintf("Sort: %q", v.Sort))
			}
			if v.As != "" {
				fields = append(fields, fmt.Sprintf("As: %q", v.As))
			}
			if v.Default {
				fields = append(fields, "Default: true")
			}
			parts = append(parts, "{"+strings.Join(fields, ", ")+"}")
		}
		sb.WriteString("Views: []framework.ListView{" + strings.Join(parts, ", ") + "}")
	}
	if len(d.Facets) > 0 {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString("Facets: []string{" + stringSliceLiteral(d.Facets) + "}")
	}
	if d.Form != nil && (len(d.Form.Main) > 0 || len(d.Form.Side) > 0) {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString("Form: &framework.EntityForm{")
		var parts []string
		if len(d.Form.Main) > 0 {
			parts = append(parts, "Main: "+formItemsLiteral(d.Form.Main))
		}
		if len(d.Form.Side) > 0 {
			parts = append(parts, "Side: "+formItemsLiteral(d.Form.Side))
		}
		sb.WriteString(strings.Join(parts, ", "))
		sb.WriteString("}")
	}
	if d.Card != nil && (d.Card.Title != "" || d.Card.Subtitle != "" || d.Card.Badge != "" || len(d.Card.Meta) > 0) {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		var parts []string
		if d.Card.Title != "" {
			parts = append(parts, fmt.Sprintf("Title: %q", d.Card.Title))
		}
		if d.Card.Subtitle != "" {
			parts = append(parts, fmt.Sprintf("Subtitle: %q", d.Card.Subtitle))
		}
		if d.Card.Badge != "" {
			parts = append(parts, fmt.Sprintf("Badge: %q", d.Card.Badge))
		}
		if len(d.Card.Meta) > 0 {
			parts = append(parts, "Meta: []string{"+stringSliceLiteral(d.Card.Meta)+"}")
		}
		sb.WriteString("Card: &framework.CardFields{" + strings.Join(parts, ", ") + "}")
	}
	if len(d.Fields) > 0 {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		var parts []string
		for _, name := range slices.Sorted(maps.Keys(d.Fields)) {
			h := d.Fields[name]
			var fields []string
			if h.Label != "" {
				fields = append(fields, fmt.Sprintf("Label: %q", h.Label))
			}
			if h.Help != "" {
				fields = append(fields, fmt.Sprintf("Help: %q", h.Help))
			}
			if h.Placeholder != "" {
				fields = append(fields, fmt.Sprintf("Placeholder: %q", h.Placeholder))
			}
			if h.Locked {
				fields = append(fields, "Locked: true")
			}
			if h.Omit {
				fields = append(fields, "Omit: true")
			}
			if h.ShowWhen != "" {
				fields = append(fields, fmt.Sprintf("ShowWhen: %q", h.ShowWhen))
			}
			if h.Input != "" {
				fields = append(fields, fmt.Sprintf("Input: %q", h.Input))
			}
			parts = append(parts, fmt.Sprintf("%q: {%s}", name, strings.Join(fields, ", ")))
		}
		sb.WriteString("Fields: map[string]framework.FieldDisplay{" + strings.Join(parts, ", ") + "}")
	}
	if len(d.PageSizes) > 0 {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		fmt.Fprintf(&sb, "PageSizes: []int{%s}", intsLiteral(d.PageSizes))
	}
	if d.NoDuplicate {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString("NoDuplicate: true")
	}
	if d.NoBulk {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString("NoBulk: true")
	}
	sb.WriteString("}")
	return sb.String()
}

// formItemsLiteral emits a []framework.FormItem literal: a field item as
// {Field: "x"}, a row as {Row: [...]}, a section with its nested items.
func formItemsLiteral(items []fwentity.FormItem) string {
	var parts []string
	for _, it := range items {
		switch {
		case it.Field != "":
			parts = append(parts, fmt.Sprintf("{Field: %q}", it.Field))
		case len(it.Row) > 0:
			parts = append(parts, "{Row: []string{"+stringSliceLiteral(it.Row)+"}}")
		case it.Section != "":
			var fields []string
			fields = append(fields, fmt.Sprintf("Section: %q", it.Section))
			if it.Help != "" {
				fields = append(fields, fmt.Sprintf("Help: %q", it.Help))
			}
			if it.Collapsed {
				fields = append(fields, "Collapsed: true")
			}
			if len(it.Items) > 0 {
				fields = append(fields, "Items: "+formItemsLiteral(it.Items))
			}
			parts = append(parts, "{"+strings.Join(fields, ", ")+"}")
		}
	}
	return "[]framework.FormItem{" + strings.Join(parts, ", ") + "}"
}

// renderStatesLiteral emits the States group of an entity registration, the
// same only-non-zero-fields rule as Display. Empty string when the
// declaration declares no states.
func renderStatesLiteral(s *fwentity.StatesConfig) string {
	if s == nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "&framework.StatesConfig{Field: %q", s.Field)
	if len(s.Initial) > 0 {
		sb.WriteString(", Initial: []string{" + stringSliceLiteral(s.Initial) + "}")
	}
	if len(s.Transitions) > 0 {
		var parts []string
		for _, tr := range s.Transitions {
			var fields []string
			fields = append(fields, fmt.Sprintf("Key: %q", tr.Key))
			if tr.Label != "" {
				fields = append(fields, fmt.Sprintf("Label: %q", tr.Label))
			}
			if len(tr.From) > 0 {
				fields = append(fields, "From: []string{"+stringSliceLiteral(tr.From)+"}")
			}
			if tr.To != "" {
				fields = append(fields, fmt.Sprintf("To: %q", tr.To))
			}
			if tr.Stamp != "" {
				fields = append(fields, fmt.Sprintf("Stamp: %q", tr.Stamp))
			}
			if tr.Variant != "" {
				fields = append(fields, fmt.Sprintf("Variant: %q", tr.Variant))
			}
			if tr.Permission != "" {
				fields = append(fields, fmt.Sprintf("Permission: %q", tr.Permission))
			}
			if tr.System {
				fields = append(fields, "System: true")
			}
			parts = append(parts, "{"+strings.Join(fields, ", ")+"}")
		}
		sb.WriteString(", Transitions: []framework.Transition{" + strings.Join(parts, ", ") + "}")
	}
	if s.Advisory {
		sb.WriteString(", Advisory: true")
	}
	sb.WriteString("}")
	return sb.String()
}

// stringSliceLiteral emits {"a", "b"} for a string slice, in order.
func stringSliceLiteral(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return strings.Join(quoted, ", ")
}

func intsLiteral(values []int) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ", ")
}

// scopeDeclHasContent, exposureDeclHasContent, and paginationDeclHasContent
// gate group-literal emission: a nil or all-zero group must emit nothing, so
// the generated Go packs back to the same declaration the YAML parsed to
// (a materialized-but-empty group would round-trip as non-nil where the
// author wrote nothing).
func scopeDeclHasContent(s *fwentity.ScopeDeclaration) bool {
	return s != nil && *s != (fwentity.ScopeDeclaration{})
}

func exposureDeclHasContent(e *fwentity.ExposureDeclaration) bool {
	return e != nil && (e.CRUD != nil || e.MCP || e.Public || e.Access != nil || readScopeDeclHasContent(e.ReadScope))
}

func paginationDeclHasContent(p *fwentity.PaginationDeclaration) bool {
	return p != nil && (p.CursorField != "" || len(p.CursorFields) > 0 || p.MaxListLimit != 0)
}

// readScopeDeclHasContent is the ReadScope half of exposureDeclHasContent: an
// all-zero ReadScope must emit nothing, or an app spec that declares
// `read_scope: {}` would round-trip as a non-nil scope where the author wrote
// a no-op — and pack would re-emit it forever after.
func readScopeDeclHasContent(rs *fwentity.ReadScopeDeclaration) bool {
	return rs != nil && (rs.Unrestricted != "" || len(rs.Filter) > 0)
}

// Returns "" when access is nil or every operation is un-gated, so the
// generated registration stays byte-identical for app specs without RBAC.
func renderAccessLiteral(access *fwentity.AccessDeclaration) string {
	if access == nil {
		return ""
	}
	var parts []string
	for _, op := range []struct{ key, perm string }{
		{"Read", access.Read}, {"Create", access.Create}, {"Update", access.Update}, {"Delete", access.Delete},
	} {
		switch op.perm {
		case "":
		case fwentity.Anyone:
			parts = append(parts, op.key+": framework.Anyone")
		default:
			parts = append(parts, fmt.Sprintf("%s: %q", op.key, op.perm))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "framework.AccessControl{" + strings.Join(parts, ", ") + "}"
}

// renderReadScopeLiteral renders an entity's read_scope declaration as a
// framework.ReadScopeConfig composite literal for the generated register.go.
// Returns "" when the scope is nil or all-zero, so an app spec without one
// stays byte-identical.
func renderReadScopeLiteral(rs *fwentity.ReadScopeDeclaration) string {
	if !readScopeDeclHasContent(rs) {
		return ""
	}
	var parts []string
	if rs.Unrestricted != "" {
		parts = append(parts, fmt.Sprintf("Unrestricted: %q", rs.Unrestricted))
	}
	if len(rs.Filter) > 0 {
		var preds []string
		for _, p := range rs.Filter {
			var pv []string
			pv = append(pv, fmt.Sprintf("Field: %q", p.Field))
			if p.Op != "" {
				pv = append(pv, fmt.Sprintf("Op: %q", p.Op))
			}
			if p.Value != "" {
				pv = append(pv, fmt.Sprintf("Value: %q", p.Value))
			}
			if len(p.Values) > 0 {
				vals := make([]string, 0, len(p.Values))
				for _, v := range p.Values {
					vals = append(vals, fmt.Sprintf("%q", v))
				}
				pv = append(pv, "Values: []string{"+strings.Join(vals, ", ")+"}")
			}
			preds = append(preds, "framework.RowPredicate{"+strings.Join(pv, ", ")+"}")
		}
		parts = append(parts, "Filter: []framework.RowPredicate{"+strings.Join(preds, ", ")+"}")
	}
	return "&framework.ReadScopeConfig{" + strings.Join(parts, ", ") + "}"
}

func renderIndexLiteral(idx framework.Index) string {
	var parts []string
	if idx.Name != "" {
		parts = append(parts, fmt.Sprintf("Name: %q", idx.Name))
	}
	if len(idx.Columns) > 0 {
		quoted := make([]string, len(idx.Columns))
		for i, column := range idx.Columns {
			quoted[i] = fmt.Sprintf("%q", column)
		}
		parts = append(parts, "Columns: []string{"+strings.Join(quoted, ", ")+"}")
	}
	if idx.Unique {
		parts = append(parts, "Unique: true")
	}
	// Expression replaces Columns for function/constant indices (see
	// framework/entity Index). Dropping it silently downgraded the declaration:
	// a `{Name: …, Expression: "LOWER(email)", Unique: true}` came back out as
	// `{Name: …, Unique: true}`, an index with no key at all, so the UNIQUE
	// constraint the developer declared simply did not exist in the generated
	// app. Case-insensitive uniqueness is the usual reason to reach for one, so
	// the failure mode was duplicate rows differing only by case.
	if idx.Expression != "" {
		parts = append(parts, fmt.Sprintf("Expression: %q", idx.Expression))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func renderFieldLiteral(field framework.FieldDeclaration) (string, error) {
	typeConst, err := schemaConstName(field.Type)
	if err != nil {
		return "", err
	}
	autoConst, err := autoGenerateConstName(field.AutoGenerate)
	if err != nil {
		return "", err
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("Name: %q", field.Name))
	parts = append(parts, fmt.Sprintf("Type: schema.%s", typeConst))
	if field.Required {
		parts = append(parts, "Required: true")
	}
	if field.Unique {
		parts = append(parts, "Unique: true")
	}
	if field.ReadOnly {
		parts = append(parts, "ReadOnly: true")
	}
	if field.Hidden {
		parts = append(parts, "Hidden: true")
	}
	if field.NoQuery {
		parts = append(parts, "NoQuery: true")
	}
	if field.Default != nil {
		literal, err := renderGoLiteral(field.Default)
		if err != nil {
			return "", fmt.Errorf("default value: %w", err)
		}
		parts = append(parts, "Default: "+literal)
	}
	if autoConst != "AutoNone" {
		parts = append(parts, "AutoGenerate: schema."+autoConst)
	}
	if field.Max != nil {
		parts = append(parts, fmt.Sprintf("Max: floatPtr(%g)", *field.Max))
	}
	if field.Min != nil {
		parts = append(parts, fmt.Sprintf("Min: floatPtr(%g)", *field.Min))
	}
	if field.Pattern != "" {
		parts = append(parts, fmt.Sprintf("Pattern: %q", field.Pattern))
	}
	if len(field.Values) > 0 {
		quoted := make([]string, len(field.Values))
		for i, value := range field.Values {
			quoted[i] = fmt.Sprintf("%q", value)
		}
		parts = append(parts, "Values: []string{"+strings.Join(quoted, ", ")+"}")
	}
	if field.To != "" {
		parts = append(parts, fmt.Sprintf("To: %q", field.To))
	}
	if field.Many {
		parts = append(parts, "Many: true")
	}
	return "{" + strings.Join(parts, ", ") + "}", nil
}

func renderRelationLiteral(rel framework.Relation) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("Type: framework.%s", relationTypeConst(rel.Type)))
	parts = append(parts, fmt.Sprintf("Name: %q", rel.Name))
	parts = append(parts, fmt.Sprintf("Entity: %q", rel.Entity))
	if len(rel.ForeignKey) > 0 {
		quoted := make([]string, len(rel.ForeignKey))
		for i, c := range rel.ForeignKey {
			quoted[i] = fmt.Sprintf("%q", c)
		}
		parts = append(parts, "ForeignKey: []string{"+strings.Join(quoted, ", ")+"}")
	}
	if rel.Through != "" {
		parts = append(parts, fmt.Sprintf("Through: %q", rel.Through))
	}
	if rel.LocalKey != "" {
		parts = append(parts, fmt.Sprintf("LocalKey: %q", rel.LocalKey))
	}
	if rel.ForeignKeyTarget != "" {
		parts = append(parts, fmt.Sprintf("ForeignKeyTarget: %q", rel.ForeignKeyTarget))
	}
	if rel.OnDelete != "" {
		parts = append(parts, fmt.Sprintf("OnDelete: framework.%s", onDeleteConstName(rel.OnDelete)))
	}
	if rel.CascadeWrite {
		parts = append(parts, "CascadeWrite: true")
	}
	if rel.Hidden {
		parts = append(parts, "Hidden: true")
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func onDeleteConstName(action framework.OnDeleteAction) string {
	switch action {
	case framework.OnDeleteCascade:
		return "OnDeleteCascade"
	case framework.OnDeleteSetNull:
		return "OnDeleteSetNull"
	case framework.OnDeleteRestrict:
		return "OnDeleteRestrict"
	case framework.OnDeleteNoAction:
		return "OnDeleteNoAction"
	default:
		return "OnDeleteNoAction"
	}
}

// RelationKindFor returns t's row, or the belongs_to row for a kind the
// table has not learned — the default every former switch's fallback arm
// produced, so unknown kinds keep reading as belongs_to instead of failing
// pack on generated code.
func RelationKindFor(t framework.RelationType) appspec.RelationKind {
	for _, k := range appspec.RelationKinds {
		if k.Typ == t {
			return k
		}
	}
	return appspec.RelationKind{Typ: framework.RelManyToOne, ConstName: "RelManyToOne", Yaml: "belongs_to"}
}

// relationTypeConst returns the framework const name for t, for emitting
// `Type: framework.<Name>` in generated relation literals.
func relationTypeConst(t framework.RelationType) string {
	return RelationKindFor(t).ConstName
}

// goExpr is Go source renderGoLiteral writes as is: a phrase read
// inside a literal the app spec otherwise spells out.
type goExpr string

func renderGoLiteral(value any) (string, error) {
	switch v := value.(type) {
	case goExpr:
		return string(v), nil
	case nil:
		return "nil", nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	case string:
		return fmt.Sprintf("%q", v), nil
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v)), nil
		}
		return fmt.Sprintf("%g", v), nil
	case int, int32, int64:
		return fmt.Sprintf("%d", v), nil
	case json.Number:
		// A spec decoded with UseNumber: an int64 keeps every digit; any
		// other number takes the float64 spelling above.
		if i, err := v.Int64(); err == nil {
			return fmt.Sprintf("%d", i), nil
		}
		f, err := v.Float64()
		if err != nil {
			return "", fmt.Errorf("default literal %q: %w", v.String(), err)
		}
		return renderGoLiteral(f)
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			literal, err := renderGoLiteral(v[key])
			if err != nil {
				return "", err
			}
			parts = append(parts, fmt.Sprintf("%q: %s", key, literal))
		}
		return "map[string]any{" + strings.Join(parts, ", ") + "}", nil
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			literal, err := renderGoLiteral(item)
			if err != nil {
				return "", err
			}
			parts = append(parts, literal)
		}
		return "[]any{" + strings.Join(parts, ", ") + "}", nil
	case []string:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = fmt.Sprintf("%q", item)
		}
		return "[]string{" + strings.Join(parts, ", ") + "}", nil
	default:
		return "", fmt.Errorf("unsupported default literal of type %T", value)
	}
}

func renderEntityModel(decl framework.EntityDeclaration) string {
	structName := appspec.ToCamelCase(decl.Name)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("type %s struct {\n", structName))
	keyIsID := appspec.DeclKeyIsID(decl)
	if keyIsID {
		sb.WriteString("\tID " + goIDType(decl) + " `json:\"id\"`\n")
	}
	for _, field := range decl.Fields {
		if keyIsID && field.Name == "id" {
			continue
		}
		typ := entityFieldGoType(structName, field)
		// Optional scalars read as pointers: null on the wire stays nil,
		// addressable, instead of collapsing into an authored-looking zero.
		if !readFieldTakesValue(decl, field) {
			typ = "*" + typ
		}
		sb.WriteString(fmt.Sprintf("\t%s %s `json:\"%s,omitempty\"`\n", appspec.ToCamelCase(field.Name), typ, appspec.WireKey(field.Name)))
	}
	// Relation fields: populated by TypedQuery.Include() and Repo.Get(...,
	// includes...). Singular relations (HasOne/BelongsTo) get *Target;
	// collections (HasMany/ManyToMany) get []*Target. Pointer so the absent
	// case marshals as null (matches the framework's nil-for-missing
	// contract).
	for _, rel := range decl.Relations {
		targetStruct := appspec.ToCamelCase(rel.Entity)
		fieldName := appspec.ToCamelCase(rel.Name)
		jsonTag := appspec.WireKey(rel.Name)
		switch rel.Type {
		case framework.RelHasOne, framework.RelManyToOne:
			sb.WriteString(fmt.Sprintf("\t%s *%s `json:\"%s,omitempty\"`\n", fieldName, targetStruct, jsonTag))
		case framework.RelHasMany, framework.RelManyToMany:
			sb.WriteString(fmt.Sprintf("\t%s []*%s `json:\"%s,omitempty\"`\n", fieldName, targetStruct, jsonTag))
		}
	}
	sb.WriteString("}\n\n")
	return sb.String()
}

func schemaConstName(value string) (string, error) {
	switch strings.ToLower(value) {
	case "", "string":
		return "String", nil
	case "text":
		return "Text", nil
	case "int", "integer":
		return "Int", nil
	case "float", "number":
		return "Float", nil
	case "decimal":
		return "Decimal", nil
	case "bool", "boolean":
		return "Bool", nil
	case "enum":
		return "Enum", nil
	case "uuid":
		return "UUID", nil
	case "timestamp", "datetime":
		return "Timestamp", nil
	case "date":
		return "Date", nil
	case "json":
		return "JSON", nil
	case "relation":
		return "Relation", nil
	case "image":
		return "Image", nil
	case "file":
		return "File", nil
	default:
		return "", fmt.Errorf("unknown field type %q", value)
	}
}

func autoGenerateConstName(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "none":
		return "AutoNone", nil
	case "uuid":
		return "AutoUUID", nil
	case "timestamp":
		return "AutoTimestamp", nil
	case "increment", "auto_increment":
		return "AutoIncrement", nil
	default:
		return "", fmt.Errorf("unknown auto_generate %q", value)
	}
}

// goTypeForField is a field's Go type in the generated entity model, write
// shapes and HTTP client. A JSON field holds any JSON value (crud stores
// arrays, numbers and strings as well as objects), so it is any: a
// map[string]any fails the decode of every row whose value is an array,
// and a list fails for the whole page.
func goTypeForField(value string) string {
	switch strings.ToLower(value) {
	case "int", "integer":
		return "int"
	case "float", "number":
		return "float64"
	case "bool", "boolean":
		return "bool"
	case "json":
		return "any"
	default:
		return "string"
	}
}
