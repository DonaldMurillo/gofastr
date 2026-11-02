package appgen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/framework"
)

// renderEntityColumns emits one entity's typed column constants and include
// names. Codegen-only; the framework itself defines NewStringColumn etc. so
// the generator just instantiates.
func renderEntityColumns(decl framework.EntityDeclaration) string {
	struct_ := appspec.ToCamelCase(decl.Name)
	var sb strings.Builder
	sb.WriteString("// ====== " + struct_ + " column references ======\n\n")
	sb.WriteString("var (\n")
	// An entity keyed on the generated "id" gets its column here: Define
	// adds the field, so Fields does not declare it. A declared key's
	// columns are ordinary fields below.
	keyIsID := appspec.DeclKeyIsID(decl)
	if keyIsID {
		ctor := "framework.NewUUIDColumn"
		if intID(decl) {
			ctor = "framework.NewIntColumn"
		}
		sb.WriteString(fmt.Sprintf("\t%sID = %s(\"id\")\n", struct_, ctor))
	}
	for _, field := range decl.Fields {
		if keyIsID && field.Name == "id" {
			continue
		}
		// A typed column exists to be put in a WHERE or ORDER BY. Emitting
		// one for a NoQuery field hands the developer a compiling, running
		// way to query the very column the HTTP surface refuses. TypedQuery
		// applies conditions straight to the builder without re-checking the
		// schema. Leave it out; the field is still readable on the struct.
		if field.NoQuery {
			continue
		}
		ctor := columnConstructor(field.Type)
		if typ := enumValueType(struct_, field); typ != "" {
			ctor = "framework.NewEnumColumn[" + typ + "]"
		}
		sb.WriteString(fmt.Sprintf("\t%s%s = %s(%q)\n",
			struct_, appspec.ToCamelCase(field.Name), ctor, field.Name))
	}
	sb.WriteString(")\n\n")

	// Include name constants per relation.
	if len(decl.Relations) > 0 {
		sb.WriteString("// " + struct_ + " include names: pass to framework.TypedQuery.Include or repo.Get(..., includes...).\n")
		sb.WriteString("const (\n")
		for _, rel := range decl.Relations {
			sb.WriteString(fmt.Sprintf("\t%sIncl%s = %q\n",
				struct_, appspec.ToCamelCase(rel.Name), rel.Name))
		}
		sb.WriteString(")\n\n")
	}
	if len(declKeyCols(decl)) > 1 {
		fmt.Fprintf(&sb, `// %[1]sID is the record id of the %[2]s row keyed on (%[3]s): the id
// BatchUpdate and BatchDelete take and On%[1]sDeleted passes.
func %[1]sID(%[4]s) string {
	return framework.JoinKey([]string{%[5]s})
}

`, struct_, goCommentSafe(decl.Name), goCommentSafe(keyColNames(decl)), goKeyParams(decl), keyArgs(decl))
	}
	return sb.String()
}

// columnConstructor maps a schema field type to the framework constructor
// that produces a typed column for it.
func columnConstructor(value string) string {
	switch strings.ToLower(value) {
	case "int", "integer":
		return "framework.NewIntColumn"
	case "float", "number", "decimal":
		return "framework.NewFloatColumn"
	case "bool", "boolean":
		return "framework.NewBoolColumn"
	case "timestamp", "datetime", "date":
		return "framework.NewTimestampColumn"
	case "uuid", "relation":
		return "framework.NewUUIDColumn"
	default:
		// String, Text, Enum, JSON, Image, File all get StringColumn: they're
		// all stored as TEXT and queried by string equality / LIKE.
		return "framework.NewStringColumn"
	}
}

// renderEntityEvents emits one entity's typed event wrappers
// (On<Camel>Created/Updated/Deleted) and its record extractor. The bodies
// they delegate to live once per package in entities/events.go
// (renderEventHelpers): onEntityEvent, onEntityDeleted, and the generic
// extractEntityRecord, which formerly were duplicated per entity file. Every
// exported name and signature is unchanged, and each wrapper still returns
// the cancel func from EventBus.Subscribe so callers can unsubscribe.
func renderEntityEvents(decl framework.EntityDeclaration) string {
	struct_ := appspec.ToCamelCase(decl.Name)
	return fmt.Sprintf(`// On%sCreated subscribes to entity.created events scoped to %q.
// Returns a cancel func; call it to remove the handler.
func On%sCreated(app *framework.App, fn func(ctx context.Context, row *%s) error) func() {
	return onEntityEvent[%s](app, %q, framework.EntityCreated, fn)
}

// On%sUpdated subscribes to entity.updated events scoped to %q.
func On%sUpdated(app *framework.App, fn func(ctx context.Context, row *%s) error) func() {
	return onEntityEvent[%s](app, %q, framework.EntityUpdated, fn)
}

// On%sDeleted subscribes to entity.deleted events scoped to %q. Callback
// receives the deleted row's id only: by the time the event fires the row
// has been removed (or soft-deleted).
func On%sDeleted(app *framework.App, fn func(ctx context.Context, id %s) error) func() {
	%s
}

// extract%sRecord unmarshals an event payload's "record" field into a
// *%s, returning ok=false if the event is for a different entity or
// the payload shape doesn't match.
func extract%sRecord(ev framework.Event, entityName string) (*%s, bool) {
	return extractEntityRecord[%s](ev, entityName)
}

`,
		struct_, decl.Name, struct_, struct_, struct_, decl.Name,
		struct_, decl.Name, struct_, struct_, struct_, decl.Name,
		struct_, decl.Name, struct_, goIDType(decl), deletedBody(decl),
		struct_, struct_, struct_, struct_, struct_,
	)
}

// deletedBody is On<Entity>Deleted's body. An entity keyed on a string
// "id" hands off to the shared onEntityDeleted, which reads the payload's
// "id"; an integer id reads it in its own body and parses it, since the
// callback takes an int. A declared key's payload carries each key column
// under its wire key instead, so the body reads the record id from those.
func deletedBody(decl framework.EntityDeclaration) string {
	if intID(decl) {
		// The payload's id is the string key; the callback takes the
		// int the row's ID holds.
		return fmt.Sprintf(`return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != %q {
			return nil
		}
		record, _ := data["record"].(map[string]any)
		key, ok := framework.RowKey(record, []string{"id"}, nil)
		if !ok {
			return nil
		}
		id, err := strconv.Atoi(key)
		if err != nil {
			return nil
		}
		return fn(ctx, id)
	})`, decl.Name)
	}
	if appspec.DeclKeyIsID(decl) {
		return fmt.Sprintf("return onEntityDeleted(app, %q, fn)", decl.Name)
	}
	cols := declKeyCols(decl)
	names := make([]string, len(cols))
	wire := make([]string, len(cols))
	for i, k := range cols {
		names[i] = k.Name
		wire[i] = fmt.Sprintf("%q: %q", k.Name, appspec.WireKey(k.Name))
	}
	return fmt.Sprintf(`wire := map[string]string{%s}
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != %q {
			return nil
		}
		record, _ := data["record"].(map[string]any)
		id, ok := framework.RowKey(record, []string{%s}, func(col string) string { return wire[col] })
		if !ok {
			return nil
		}
		return fn(ctx, id)
	})`, strings.Join(wire, ", "), decl.Name, stringSliceLiteral(names))
}

// renderEventHelpers emits entities/events.go, the fixed event-helper seam
// for the generated entities package. It holds the bodies that
// renderEntityEvents formerly emitted per entity: the record-lifecycle
// subscription (on<Camel>Event, now onEntityEvent), the deleted-subscription
// body (formerly inlined in every On<Camel>Deleted, now onEntityDeleted),
// and the record extractor (extract<Camel>Record, now the generic
// extractEntityRecord). Like register.go it carries no entity-specific text,
// so its bytes are identical for every project and additive generation
// writes it only when absent.
func renderEventHelpers() string {
	return `package entities

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework"
)

// onEntityEvent subscribes to one of the record lifecycle events for entity
// (entity.created / entity.updated), unmarshalling the payload's record into
// a *T before invoking fn. Shared body behind every generated On<Camel>Created
// and On<Camel>Updated wrapper.
func onEntityEvent[T any](app *framework.App, entity, eventType string, fn func(ctx context.Context, row *T) error) func() {
	return app.Events().Subscribe(eventType, func(ctx context.Context, ev framework.Event) error {
		row, ok := extractEntityRecord[T](ev, entity)
		if !ok {
			return nil
		}
		return fn(ctx, row)
	})
}

// onEntityDeleted subscribes to entity.deleted events for entity. By the time
// the event fires the row has been removed (or soft-deleted), so the callback
// receives the deleted row's id only. Shared body behind every generated
// On<Camel>Deleted wrapper.
func onEntityDeleted(app *framework.App, entity string, fn func(ctx context.Context, id string) error) func() {
	return app.Events().Subscribe(framework.EntityDeleted, func(ctx context.Context, ev framework.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok || data["entity"] != entity {
			return nil
		}
		record, _ := data["record"].(map[string]any)
		id, _ := record["id"].(string)
		if id == "" {
			return nil
		}
		return fn(ctx, id)
	})
}

// extractEntityRecord unmarshals an event payload's "record" field for entity
// into a *T, returning ok=false if the event is for a different entity or the
// payload shape doesn't match. Shared body behind every generated
// extract<Camel>Record wrapper.
func extractEntityRecord[T any](ev framework.Event, entity string) (*T, bool) {
	data, ok := ev.Data.(map[string]any)
	if !ok || data["entity"] != entity {
		return nil, false
	}
	record, ok := data["record"].(map[string]any)
	if !ok {
		return nil, false
	}
	var v T
	if err := framework.UnmarshalEntity(record, &v); err != nil {
		return nil, false
	}
	return &v, true
}
`
}

// enumValueType names the Go type an enum field's values take in the
// entities package, <Entity><Field>Value, or "" for a field that is not an
// enum with declared values.
func enumValueType(struct_ string, field framework.FieldDeclaration) string {
	if !strings.EqualFold(field.Type, "enum") || len(field.Values) == 0 {
		return ""
	}
	return struct_ + appspec.ToCamelCase(field.Name) + "Value"
}

// entityFieldGoType is a field's Go type in the entities package: the enum
// value type for an enum with declared values, the plain mapping otherwise.
func entityFieldGoType(struct_ string, field framework.FieldDeclaration) string {
	if typ := enumValueType(struct_, field); typ != "" {
		return typ
	}
	return goTypeForField(field.Type)
}

// entityMoveKeys lists every move key on the entity, System moves included:
// the repo is Go code, the one caller a System move admits.
func entityMoveKeys(decl framework.EntityDeclaration) []string {
	if decl.States == nil {
		return nil
	}
	keys := make([]string, 0, len(decl.States.Transitions))
	for _, t := range decl.States.Transitions {
		keys = append(keys, t.Key)
	}
	return keys
}

type identConst struct{ name, value string }

// entityFileNames counts the top-level names one entity file declares
// besides its enum and move constants: the model, the write shapes and
// their nested cascade types, the repo and its constructor, the column vars and include constants, the
// value and move types, and the event and registration funcs.
func entityFileNames(decl framework.EntityDeclaration) map[string]int {
	struct_ := appspec.ToCamelCase(decl.Name)
	names := map[string]int{}
	for _, suffix := range []string{"", "Input", "Patch", "Repo", "ID"} {
		names[struct_+suffix]++
	}
	for _, name := range []string{"New" + struct_ + "Repo", "On" + struct_ + "Created",
		"On" + struct_ + "Updated", "On" + struct_ + "Deleted", "extract" + struct_ + "Record",
		"register" + struct_} {
		names[name]++
	}
	for _, field := range decl.Fields {
		// renderEntityColumns emits no column var for the generated id or a
		// NoQuery field.
		if !(appspec.DeclKeyIsID(decl) && field.Name == "id") && !field.NoQuery {
			names[struct_+appspec.ToCamelCase(field.Name)]++
		}
		if typ := enumValueType(struct_, field); typ != "" {
			names[typ]++
		}
	}
	for _, rel := range decl.Relations {
		names[struct_+"Incl"+appspec.ToCamelCase(rel.Name)]++
	}
	for _, rel := range cascadeRelations(decl) {
		names[struct_+appspec.ToCamelCase(rel.Name)+"Nested"]++
	}
	if len(entityMoveKeys(decl)) > 0 {
		names[struct_+"Move"]++
	}
	return names
}

// identConsts derives one constant name per value: prefix plus the value
// camel-cased. A value whose name is not an ASCII Go identifier, or whose
// name taken counts more than once, gets no constant; it comes back in
// unnamed so the emitter can say so. taken holds every name in the file,
// these candidates included, so "past_due" next to "past-due", or "value"
// on status against the type PostsStatusValue, both lose their constant.
// The value itself only ever reaches a quoted literal.
func identConsts(prefix string, values []string, taken map[string]int) (consts []identConst, unnamed []string) {
	for _, v := range values {
		camel := appspec.ToCamelCase(v)
		if camel == "" || taken[prefix+camel] > 1 || !project.IsGoIdentifier(prefix+camel) {
			unnamed = append(unnamed, v)
			continue
		}
		consts = append(consts, identConst{name: prefix + camel, value: v})
	}
	return consts, unnamed
}

// writeConstBlock emits typ's constants, and a comment naming the values
// identConsts could not name, which stay reachable as typ("<value>").
func writeConstBlock(sb *strings.Builder, typ string, consts []identConst, unnamed []string) {
	if len(consts) > 0 {
		sb.WriteString("const (\n")
		for _, c := range consts {
			fmt.Fprintf(sb, "\t%s %s = %q\n", c.name, typ, c.value)
		}
		sb.WriteString(")\n\n")
	}
	if len(unnamed) > 0 {
		quoted := make([]string, len(unnamed))
		for i, v := range unnamed {
			quoted[i] = strconv.Quote(v)
		}
		fmt.Fprintf(sb, "// No constant for %s: the value has no identifier form. Write %s(<value>).\n\n",
			goCommentSafe(strings.Join(quoted, ", ")), typ)
	}
}

// renderEntityEnums emits, per enum field with declared values, a named
// string type and one constant per value, and for an entity with moves,
// <Entity>Move and one constant per move key. The model, the write shapes
// and the enum column all take the named type, so PostsStatus.Eq(
// PostsStatusLive) compiles and a plain string variable does not.
func renderEntityEnums(decl framework.EntityDeclaration) string {
	struct_ := appspec.ToCamelCase(decl.Name)
	keys := entityMoveKeys(decl)
	// Count every candidate constant before naming any, so a value loses
	// its constant when any other name in the file, constants included,
	// repeats it.
	taken := entityFileNames(decl)
	count := func(prefix string, values []string) {
		for _, v := range values {
			if camel := appspec.ToCamelCase(v); camel != "" {
				taken[prefix+camel]++
			}
		}
	}
	for _, field := range decl.Fields {
		if enumValueType(struct_, field) != "" {
			count(struct_+appspec.ToCamelCase(field.Name), field.Values)
		}
	}
	count(struct_+"Move", keys)

	var sb strings.Builder
	for _, field := range decl.Fields {
		typ := enumValueType(struct_, field)
		if typ == "" {
			continue
		}
		fmt.Fprintf(&sb, "// %s is one value of the %s field %s.\ntype %s string\n\n",
			typ, goCommentSafe(decl.Name), goCommentSafe(field.Name), typ)
		consts, unnamed := identConsts(struct_+appspec.ToCamelCase(field.Name), field.Values, taken)
		writeConstBlock(&sb, typ, consts, unnamed)
	}
	if len(keys) > 0 {
		typ := struct_ + "Move"
		fmt.Fprintf(&sb, "// %s names one of the %s moves; pass it to %sRepo.Transition.\ntype %s string\n\n",
			typ, goCommentSafe(decl.Name), struct_, typ)
		consts, unnamed := identConsts(typ, keys, taken)
		writeConstBlock(&sb, typ, consts, unnamed)
	}
	return sb.String()
}

// renderEntityWriteShapes emits the repo's two write payloads.
// <Entity>Input is the create payload: a required field with no default
// is a value and always sent; every other field is a pointer whose nil
// takes the column default. <Entity>Patch is the update payload, every
// field a pointer: nil leaves the field as stored, a pointer to a zero
// value writes it where the CRUD path keeps "" (dropEmptyFormValues keeps
// it only for string and text). The model cannot be the write shape: a
// value-typed field tagged omitempty marshals false, 0 and "" away, so a
// create could not override a non-zero default and an update could never
// clear a field.
//
// Hidden fields stay in both: the repo is server code, and WithServerWrites
// lets it write them. writeShapeDrops removes what the CRUD path never
// takes from a payload, the same set the HTTP client's shapes drop.
func renderEntityWriteShapes(decl framework.EntityDeclaration, decls []framework.EntityDeclaration) string {
	struct_ := appspec.ToCamelCase(decl.Name)
	dropCreate, dropUpdate := writeShapeDrops(decl)
	var sb strings.Builder
	fmt.Fprintf(&sb, "// %sInput is the create payload for %sRepo. A required field with no\n"+
		"// default is always sent; a nil field takes its default, and new(false)\n"+
		"// stores false.\n", struct_, struct_)
	fmt.Fprintf(&sb, "type %sInput struct {\n", struct_)
	for _, field := range decl.Fields {
		if dropCreate(field) {
			continue
		}
		typ, tag := entityFieldGoType(struct_, field), appspec.WireKey(field.Name)
		if inputTakesValue(field) {
			fmt.Fprintf(&sb, "\t%s %s `json:\"%s\"`\n", appspec.ToCamelCase(field.Name), typ, tag)
		} else {
			fmt.Fprintf(&sb, "\t%s *%s `json:\"%s,omitempty\"`\n", appspec.ToCamelCase(field.Name), typ, tag)
		}
	}
	for _, rel := range cascadeRelations(decl) {
		fmt.Fprintf(&sb, "\t%s %s%sInput `json:\"%s,omitempty\"`\n", appspec.ToCamelCase(rel.Name),
			cascadeArity(rel), appspec.ToCamelCase(rel.Entity), appspec.WireKey(rel.Name))
	}
	sb.WriteString("}\n\n")
	fmt.Fprintf(&sb, "// %sPatch is the update payload for %sRepo: a nil field is left as\n"+
		"// stored, and a pointer to false, 0 or \"\" writes it to a bool, int,\n"+
		"// float, string or text field. \"\" for any other type is a blank: it\n"+
		"// clears an optional field and fails validation on a required one.\n", struct_, struct_)
	fmt.Fprintf(&sb, "type %sPatch struct {\n", struct_)
	for _, field := range decl.Fields {
		if dropUpdate(field) {
			continue
		}
		fmt.Fprintf(&sb, "\t%s *%s `json:\"%s,omitempty\"`\n",
			appspec.ToCamelCase(field.Name), entityFieldGoType(struct_, field), appspec.WireKey(field.Name))
	}
	for _, rel := range cascadeRelations(decl) {
		fmt.Fprintf(&sb, "\t%s %s%s%sNested `json:\"%s,omitempty\"`\n", appspec.ToCamelCase(rel.Name),
			cascadeArity(rel), struct_, appspec.ToCamelCase(rel.Name), appspec.WireKey(rel.Name))
	}
	sb.WriteString("}\n\n")
	for _, rel := range cascadeRelations(decl) {
		nested := struct_ + appspec.ToCamelCase(rel.Name) + "Nested"
		// The child's own key column, not always id: the patch drops key
		// columns, so this field is the only one that can address it.
		key, keyType, keyTag := nestedKey(rel, decls)
		empty := "an empty " + key + " creates one"
		if rel.Type == framework.RelHasOne {
			empty = "an empty " + key + " updates the existing child, or creates one"
		}
		if keyType != "string" {
			empty = strings.Replace(empty, "an empty", "a zero", 1)
		}
		fmt.Fprintf(&sb, "// %s is the %s child in a %sPatch: a set %s updates that\n"+
			"// child, and %s.\ntype %s struct {\n\t%s %s `json:\"%s,omitempty\"`\n\t%sPatch\n}\n\n",
			nested, goCommentSafe(rel.Name), struct_, key, empty, nested, key, keyType, keyTag, appspec.ToCamelCase(rel.Entity))
	}
	return sb.String()
}

// cascadeRelations lists the relations whose children the write payloads
// carry: cascade_write on a has_one, has_many or belongs_to, the three the
// CRUD path writes through (crud/cascade_write.go). The model struct
// carried them before the payloads replaced it as the write shape.
func cascadeRelations(decl framework.EntityDeclaration) []framework.Relation {
	var out []framework.Relation
	for _, rel := range decl.Relations {
		if rel.CascadeWrite && rel.Type != framework.RelManyToMany {
			out = append(out, rel)
		}
	}
	return out
}

// cascadeArity is the Go type prefix for a cascade relation's children:
// a slice for has_many, a pointer for a single child.
func cascadeArity(rel framework.Relation) string {
	if rel.Type == framework.RelHasMany {
		return "[]"
	}
	return "*"
}

// renderEntityRepo emits one entity's typed repository, wrapping a
// CrudHandler so generated callers get Create/Get/Update/Delete + Query +
// WithTx without re-implementing CRUD plumbing. Writes take the shapes
// renderEntityWriteShapes emits, never the model; an entity with moves
// also gets Transition.
func renderEntityRepo(decl framework.EntityDeclaration) string {
	repo := fmt.Sprintf(`// %[1]sRepo is the typed repository for %[2]s rows.
// Event helpers: On%[1]sCreated/On%[1]sUpdated/On%[1]sDeleted in this package.
type %[1]sRepo struct {
	handler *framework.CrudHandler
}

// New%[1]sRepo wires a typed repo against the App's %[2]q entity. Panics if the
// entity hasn't been registered yet.
func New%[1]sRepo(app *framework.App) *%[1]sRepo {
	entity, err := app.Registry.Get(%[2]q)
	if err != nil {
		panic("entities: %[2]s not registered: " + err.Error())
	}
	h := framework.NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry(%[2]q)
	h.Storage = app.Storage
	h.Events = app.Events()
	h.Registry = app.Registry
	return &%[1]sRepo{handler: h}
}

// Handler returns the underlying CrudHandler: useful for advanced wiring or
// to feed the typed-query primitives directly.
func (r *%[1]sRepo) Handler() *framework.CrudHandler { return r.handler }

// WithTx returns a tx-bound copy of the repo. Calls within a hook can use
// framework.TxFromContext(ctx) and pass the result here to chain typed CRUD
// atomically with the parent operation.
func (r *%[1]sRepo) WithTx(tx *sql.Tx) *%[1]sRepo {
	h := *r.handler
	h.DB = tx
	return &%[1]sRepo{handler: &h}
}

// row decodes one CRUD result.
func (r *%[1]sRepo) row(out map[string]any) (*%[1]s, error) {
	var row %[1]s
	if err := framework.UnmarshalEntity(out, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// rows decodes a batch result, keeping its order.
func (r *%[1]sRepo) rows(outs []map[string]any) ([]*%[1]s, error) {
	rows := make([]*%[1]s, len(outs))
	for i, out := range outs {
		row, err := r.row(out)
		if err != nil {
			return nil, err
		}
		rows[i] = row
	}
	return rows, nil
}

// Create persists in and returns the stored row, server-generated fields
// included.
func (r *%[1]sRepo) Create(ctx context.Context, in %[1]sInput) (*%[1]s, error) {
	body, err := framework.MarshalEntity(in)
	if err != nil {
		return nil, err
	}
	out, err := r.handler.CreateOne(ctx, body)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Get fetches a row by %[5]s with optional eager-loaded includes.
func (r *%[1]sRepo) Get(ctx context.Context, %[3]s, includes ...string) (*%[1]s, error) {
	out, err := r.handler.GetOne(ctx, %[4]s, includes)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Update writes patch's non-nil fields onto the row with this %[5]s and
// returns the row as stored afterwards.
func (r *%[1]sRepo) Update(ctx context.Context, %[3]s, patch %[1]sPatch) (*%[1]s, error) {
	body, err := framework.MarshalEntity(patch)
	if err != nil {
		return nil, err
	}
	out, err := r.handler.UpdateOne(ctx, %[4]s, body)
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

// Delete removes the row by %[5]s (or soft-deletes if SoftDelete is enabled on
// the entity).
func (r *%[1]sRepo) Delete(ctx context.Context, %[3]s) error {
	return r.handler.DeleteOne(ctx, %[4]s)
}

// Query starts a typed query for chaining Where/Order/Limit/Include and
// finishing with Find/First/Count.
func (r *%[1]sRepo) Query() *framework.TypedQuery[%[1]s] {
	return framework.NewTypedQuery[%[1]s](r.handler)
}

// Exists reports whether a row with the given %[5]s is present (and not soft-
// deleted for SoftDelete entities). Tenant scope still applies.
func (r *%[1]sRepo) Exists(ctx context.Context, %[3]s) (bool, error) {
	%[6]s
}

// Count returns the total number of rows visible under the current tenant
// and soft-delete scope. Chain through Query() for filtered counts.
func (r *%[1]sRepo) Count(ctx context.Context) (int, error) {
	return r.Query().Count(ctx)
}

// FirstOrCreate returns the first row matching match, or creates in and
// returns the stored row when none does.
func (r *%[1]sRepo) FirstOrCreate(ctx context.Context, in %[1]sInput, match framework.Condition) (*%[1]s, error) {
	existing, err := r.Query().Where(match).First(ctx)
	if err == nil {
		return existing, nil
	}
	if !framework.IsNotFound(err) {
		return nil, err
	}
	return r.Create(ctx, in)
}

// BatchCreate inserts every input in one transaction; on any per-item error
// the entire batch rolls back. Returned rows are in input order.
func (r *%[1]sRepo) BatchCreate(ctx context.Context, ins []%[1]sInput) ([]*%[1]s, error) {
	bodies := make([]map[string]any, len(ins))
	for i, in := range ins {
		b, err := framework.MarshalEntity(in)
		if err != nil {
			return nil, err
		}
		bodies[i] = b
	}
	results, err := r.handler.BatchCreateMany(ctx, bodies)
	if err != nil {
		return nil, err
	}
	return r.rows(results)
}

// BatchUpdate writes patches[i] onto the row ids[i], all in one transaction.
func (r *%[1]sRepo) BatchUpdate(ctx context.Context, ids %[7]s, patches []%[1]sPatch) ([]*%[1]s, error) {
	%[8]sbodies := make([]map[string]any, len(patches))
	for i, patch := range patches {
		b, err := framework.MarshalEntity(patch)
		if err != nil {
			return nil, err
		}
		bodies[i] = b
	}
	results, err := r.handler.BatchUpdateMany(ctx, %[9]s, bodies)
	if err != nil {
		return nil, err
	}
	return r.rows(results)
}

// BatchDelete deletes every id atomically.
func (r *%[1]sRepo) BatchDelete(ctx context.Context, ids %[7]s) error {
	%[8]s_, err := r.handler.BatchDeleteMany(ctx, %[9]s)
	return err
}

`, appspec.ToCamelCase(decl.Name), decl.Name, goKeyParams(decl), typedKeyExpr(decl), keyDoc(decl), existsBody(decl),
		goIDsType(decl), keyStrings(decl), keysVar(decl))
	if len(entityMoveKeys(decl)) == 0 {
		return repo
	}
	return repo + fmt.Sprintf(`// Transition runs move on the row with this %[4]s and returns the row as
// stored afterwards: the one way an enforced state field changes. It runs
// as CrudHandler.RunTransition does, so a System move skips the permission
// check and a row outside the move's From answers a conflict.
func (r *%[1]sRepo) Transition(ctx context.Context, %[2]s, move %[1]sMove) (*%[1]s, error) {
	out, err := r.handler.RunTransition(ctx, %[3]s, string(move))
	if err != nil {
		return nil, err
	}
	return r.row(out)
}

`, appspec.ToCamelCase(decl.Name), goKeyParams(decl), typedKeyExpr(decl), keyDoc(decl))
}

// typedKeyExpr is the Go expression for a record's id inside a repo
// method, the string key CrudHandler takes: the one key parameter, an
// integer id in decimal, or <Entity>ID over a composite key's.
func typedKeyExpr(decl framework.EntityDeclaration) string {
	if intID(decl) {
		return "strconv.Itoa(id)"
	}
	if len(declKeyCols(decl)) == 1 {
		return declKeyCols(decl)[0].Param
	}
	return appspec.ToCamelCase(decl.Name) + "ID(" + keyArgs(decl) + ")"
}

// existsBody is the repo's Exists body. The generated id, string or
// integer, has a typed column to query; a declared key's values arrive as
// strings whatever the columns' types, so Exists reads the record the way
// Get does.
func existsBody(decl framework.EntityDeclaration) string {
	if appspec.DeclKeyIsID(decl) {
		return fmt.Sprintf("return r.Query().Where(%sID.Eq(id)).Exists(ctx)", appspec.ToCamelCase(decl.Name))
	}
	return fmt.Sprintf(`_, err := r.handler.GetOne(ctx, %s, nil)
	if framework.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err`, typedKeyExpr(decl))
}
