package framework

import (
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// entityKey is the registry's primary identity for an entity: its config
// name paired with the API version it is mounted under. Entities registered
// via App.Entity carry Version == "" and keep the historical single-version
// behaviour. Entities registered via App.GroupEntity carry the group's full
// prefix (e.g. "/api/v1"), so the same entity name can coexist under
// different versions without colliding.
type entityKey struct {
	name    string
	version string
}

// Registry stores and retrieves Entity definitions keyed by (name, version).
// It is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	entities map[entityKey]*entity.Entity
	db       *sql.DB
}

// NewRegistry creates a new empty entity registry.
func NewRegistry() *Registry {
	return &Registry{
		entities: make(map[entityKey]*entity.Entity),
	}
}

// Register adds an Entity to the registry.
//
// The key is (Config.Name, Version). The same name may be registered under
// multiple Versions (e.g. "posts" at "/api/v1" and "/api/v2"); registering
// the same (name, version) pair twice is an error. An entity with an empty
// Version — the App.Entity path — keeps the historical behaviour: at most
// one per name.
func (r *Registry) Register(ent *entity.Entity) error {
	if ent == nil {
		return fmt.Errorf("registry: entity must not be nil")
	}
	if ent.Config.Name == "" {
		return fmt.Errorf("registry: entity name must not be empty")
	}

	// Validate the full config at registration time so misconfigs surface at
	// app.Entity() with an actionable message, rather than as an opaque SQL
	// error several phases later (migrate / first request).
	if err := ent.Validate(); err != nil {
		return fmt.Errorf("registry: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := entityKey{name: ent.Config.Name, version: ent.Version}
	if _, exists := r.entities[key]; exists {
		if ent.Version == "" {
			return fmt.Errorf("registry: entity %q already registered", ent.Config.Name)
		}
		return fmt.Errorf("registry: entity %q already registered at version %q", ent.Config.Name, ent.Version)
	}
	// Same-name structural compatibility: two versions of one entity name
	// share one physical table, so they MUST agree on which table it is
	// (F5), whether the framework manages its DDL (F10), and at most one
	// may declare a Seed (F11). These are pairwise, incremental, and fire
	// at registration so the author sees the conflict at app.Entity — not
	// at boot when the union consumer runs.
	if err := r.checkVersionCompat(key, ent); err != nil {
		return err
	}

	// Cross-version column compatibility. Two versions of the same entity
	// name share one DB table (the table is derived from the name, or set
	// to the same value explicitly). A column both versions declare MUST
	// have a physically compatible definition, or the DDL emitted at boot
	// would depend on which version migrate happened to see first — a
	// silent correctness trap that surfaces as a runtime SQL error long
	// after boot succeeded. Catch it here, at registration, with a message
	// that names both versions so the author can find the conflict. The
	// same loop also rejects a mandatory (NOT NULL, no-default) column
	// exclusive to one version (F6), conflicting named indices (F8), and
	// conflicting foreign keys on the same column (F9).
	if err := r.checkColumnConflicts(key, ent); err != nil {
		return err
	}

	// Propagate registry-level DB if the entity doesn't have one
	if ent.DB == nil && r.db != nil {
		ent.DB = r.db
	}

	r.entities[key] = ent
	return nil
}

// Get retrieves an Entity by name, resolving version ambiguity safely.
//
// Resolution order:
//  1. If an unversioned entity (Version == "") is registered under name, it
//     is returned. This is the App.Entity path and the historical behaviour.
//  2. Otherwise, if exactly one versioned entity is registered under name,
//     it is returned (the common "one version so far" case).
//  3. If multiple versions exist and none is unversioned, Get returns an
//     error — picking one silently would be a correctness trap. Callers that
//     need a specific version must use GetVersioned.
func (r *Registry) Get(name string) (*entity.Entity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Fast path: unversioned entity (the historical App.Entity case).
	if e, ok := r.entities[entityKey{name: name, version: ""}]; ok {
		return e, nil
	}

	// Collect every versioned entity under this name.
	var matches []*entity.Entity
	for k, e := range r.entities {
		if k.name == name && k.version != "" {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("registry: entity %q not found", name)
	case 1:
		return matches[0], nil
	default:
		versions := make([]string, 0, len(matches))
		for _, e := range matches {
			versions = append(versions, e.Version)
		}
		sort.Strings(versions)
		return nil, fmt.Errorf("registry: entity %q has %d versions (%s); use Registry.GetVersioned to pick one", name, len(matches), strings.Join(versions, ", "))
	}
}

// GetVersioned retrieves the specific version of an entity registered under
// the given name. version is the route group prefix the entity was mounted at
// (e.g. "/api/v1"); pass "" to fetch the unversioned (App.Entity) entity.
// Returns an error if no entity matches both name and version.
func (r *Registry) GetVersioned(name, version string) (*entity.Entity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entities[entityKey{name: name, version: version}]
	if !ok {
		if version == "" {
			return nil, fmt.Errorf("registry: entity %q has no unversioned registration", name)
		}
		return nil, fmt.Errorf("registry: entity %q not registered at version %q", name, version)
	}
	return e, nil
}

// All returns a map of all registered entities keyed by name, with one entry
// per name. When a name has an unversioned entity it wins; otherwise the sole
// version is returned; when multiple versions exist and none is unversioned,
// the lexicographically smallest version is included (deterministic) and
// callers that need every version should use AllSorted. Map iteration order
// is randomised by Go; use AllSorted() for stable iteration in code paths
// that emit order-sensitive output.
func (r *Registry) All() map[string]*entity.Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Group every entity by name to pick one representative per name.
	byName := make(map[string][]*entity.Entity, len(r.entities))
	for k, e := range r.entities {
		byName[k.name] = append(byName[k.name], e)
	}
	out := make(map[string]*entity.Entity, len(byName))
	for name, group := range byName {
		out[name] = pickRepresentative(group)
	}
	return out
}

// pickRepresentative chooses one entity from a same-name group: the
// unversioned one if present, else the sole version, else the lex-first
// version. Deterministic.
func pickRepresentative(group []*entity.Entity) *entity.Entity {
	// Prefer unversioned.
	for _, e := range group {
		if e.Version == "" {
			return e
		}
	}
	// Sort the rest by version for deterministic selection.
	sort.Slice(group, func(i, j int) bool {
		return group[i].Version < group[j].Version
	})
	return group[0]
}

// AllSorted returns every registered entity in alphabetical order by
// (name, version). Unlike All, this includes every version of every entity,
// so it is the right choice when callers must see all mount points (startup
// banners, OpenAPI, route enumeration). Use this whenever the iteration
// order or the full version set matters for bytes-on-the-wire.
func (r *Registry) AllSorted() []*entity.Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*entity.Entity, 0, len(r.entities))
	for _, e := range r.entities {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Config.Name != out[j].Config.Name {
			return out[i].Config.Name < out[j].Config.Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}

// SetDB sets the database connection on the registry and propagates it
// to all registered entities.
func (r *Registry) SetDB(db *sql.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.db = db
	for _, e := range r.entities {
		e.DB = db
	}
}

// checkColumnConflicts verifies that every column the new entity declares is
// physically compatible with the same-named column on every other registered
// entity that targets the SAME TABLE. The caller already holds r.mu.
//
// Keyed on table, not name, because the table is what makes a conflict matter:
// two entities sharing a table both emit DDL for it, so an incompatible column
// leaves the surviving schema dependent on map iteration order. That is true
// whether they differ by version (same name, /api/v1 vs /api/v2) or by name
// ("posts" and "postsLegacy" both on table "posts" — the shape people reached
// for before the registry became version-aware, and still reachable today).
//
// Unmanaged entities are exempt on both sides: migrate never emits DDL for a
// view, FTS virtual table, or external table, so it cannot contribute to a
// divergence. Requiring their columns to match a managed sibling's would
// reject legitimate read models over an existing table.
func (r *Registry) checkColumnConflicts(newKey entityKey, newEnt *entity.Entity) error {
	if newEnt.Config.Unmanaged {
		return nil
	}
	newTable := newEnt.GetTable()
	for k, existing := range r.entities {
		if k == newKey {
			continue // same (name, version) — already rejected by the duplicate-key check
		}
		if existing.Config.Unmanaged || existing.GetTable() != newTable {
			continue
		}
		if err := conflictingColumns(existing, newEnt); err != nil {
			return err
		}
		// F6: a mandatory (NOT NULL, no default, no auto-gen) column exclusive
		// to one entity breaks the other's writes — the shared table gains a
		// column the other version can never supply.
		if err := conflictingMandatoryFields(existing, newEnt); err != nil {
			return err
		}
		// F8: a named index on the shared table must be identical across
		// every version that declares it (columns, expression, uniqueness).
		if err := conflictingIndices(existing, newEnt); err != nil {
			return err
		}
		// F9: a foreign-key column must reference the same target table and
		// key with the same relation type across every version.
		if err := conflictingRelations(existing, newEnt); err != nil {
			return err
		}
	}
	return nil
}

// conflictingColumns compares two versions of one entity and returns an
// error describing the first same-named column whose physical schema
// definition diverges. Returns nil when every overlapping column is
// compatible, or when the two entities share no column names.
//
// Two versions of one entity share one DB table, so a column both
// declare MUST produce identical DDL — a divergence would make the
// emitted schema depend on which version migrate saw first. The
// comparison covers exactly the field attributes that reach the DDL:
//
//   - the rendered SQL TYPE  (derived from migrate.SQLType — catches Type,
//     RawType, AND String.Max which selects VARCHAR(n); the single source
//     of truth for the column's physical type, so it can never drift from
//     the emitted DDL the way a hand-listed attribute set would)
//   - Unique                      (UNIQUE constraint)
//   - Required                    (NOT NULL, when not auto-generated)
//   - AutoGenerate                (DEFAULT clause + NOT NULL interaction)
//   - Default                     (DEFAULT value)
//   - primary-key-ness            (PRIMARY KEY constraint)
//
// Deliberately NOT compared — these are wire/validation concerns that
// never reach the DDL, exactly like the Exclude/Rename projections
// apiversions.ApplyToEntityConfig produces:
//
//   - Hidden         (response visibility)
//   - WireName       (JSON key override)
//   - ReadOnly       (request-body acceptance)
//   - Min/Pattern/Values (validation rules that don't change the column type)
//   - To/Many        (relation metadata, not column-level DDL)
//   - field ordering (declaration order is irrelevant to the schema)
func conflictingColumns(a, b *entity.Entity) error {
	// Index a's fields by lowercased name (Postgres folds unquoted identifiers
	// to lowercase, so a mixed-case column name is physically the same column
	// in both versions even if the declarations differ in case).
	byName := make(map[string]schema.Field, len(a.Config.Fields))
	for _, f := range a.Config.Fields {
		byName[strings.ToLower(f.Name)] = f
	}
	for _, fb := range b.Config.Fields {
		fa, ok := byName[strings.ToLower(fb.Name)]
		if !ok {
			continue // column only b declares — additive, handled by the union
		}
		if columnSchemaEqual(fa, a.PrimaryKey, fb, b.PrimaryKey) {
			continue
		}
		return fmt.Errorf(
			"registry: entity %q (table %q): column %q is declared with an incompatible schema across versions — "+
				"%s declares %s, %s declares %s; two versions share one table so the column definitions must match exactly "+
				"(type, nullability, uniqueness, default, auto/PK). Make both versions identical or rename the column in one version",
			a.Config.Name, a.Config.Table, fb.Name,
			versionLabel(a.Version), describeColumnSchema(fa, fa.Name == a.PrimaryKey),
			versionLabel(b.Version), describeColumnSchema(fb, fb.Name == b.PrimaryKey),
		)
	}
	return nil
}

// columnSchemaEqual reports whether two same-named fields produce identical
// physical DDL. The rendered SQL TYPE is the single source of truth: it is
// derived from migrate.SQLType — the same function migration emits — so the
// comparison catches Type, RawType, AND String.Max (VARCHAR(n)) divergences,
// and can never drift from the DDL the way a hand-listed attribute set would.
// Both dialects are compared as a belt-and-braces guard against a future
// dialect-specific type rendering.
func columnSchemaEqual(a schema.Field, aPK string, b schema.Field, bPK string) bool {
	if migrate.SQLType(a, migrate.DialectPostgres) != migrate.SQLType(b, migrate.DialectPostgres) {
		return false
	}
	if migrate.SQLType(a, migrate.DialectSQLite) != migrate.SQLType(b, migrate.DialectSQLite) {
		return false
	}
	if a.Unique != b.Unique {
		return false
	}
	if a.Required != b.Required {
		return false
	}
	if a.AutoGenerate != b.AutoGenerate {
		return false
	}
	if (a.Name == aPK) != (b.Name == bPK) {
		return false
	}
	if !reflect.DeepEqual(a.Default, b.Default) {
		return false
	}
	return true
}

// describeColumnSchema renders a human-readable summary of the physical
// DDL attributes the conflict check compares, for the error message. The
// type is rendered via migrate.SQLType so the diagnostic shows the real
// physical type — including a VARCHAR(n) length — matching what
// columnSchemaEqual compares.
func describeColumnSchema(f schema.Field, isPK bool) string {
	var b strings.Builder
	b.WriteString(migrate.SQLType(f, migrate.DialectPostgres))
	if isPK {
		b.WriteString(" PRIMARY KEY")
	}
	if f.Unique {
		b.WriteString(" UNIQUE")
	}
	if f.Required && f.AutoGenerate == schema.AutoNone {
		b.WriteString(" NOT NULL")
	}
	if f.AutoGenerate != schema.AutoNone {
		fmt.Fprintf(&b, " auto=%s", autoGenName(f.AutoGenerate))
	}
	if f.Default != nil {
		fmt.Fprintf(&b, " default=%v", f.Default)
	}
	return b.String()
}

// versionLabel renders a version string for error messages, making the
// unversioned (App.Entity) case explicit.
func versionLabel(v string) string {
	if v == "" {
		return "version \"\" (unversioned)"
	}
	return fmt.Sprintf("version %q", v)
}

// managedLabel renders the managed/unmanaged posture for error messages.
func managedLabel(unmanaged bool) string {
	if unmanaged {
		return "unmanaged (framework emits no DDL)"
	}
	return "managed (framework emits DDL)"
}

// autoGenName renders a schema.AutoGenerate value for diagnostics.
func autoGenName(a schema.AutoGenerate) string {
	switch a {
	case schema.AutoNone:
		return "none"
	case schema.AutoUUID:
		return "uuid"
	case schema.AutoTimestamp:
		return "timestamp"
	case schema.AutoIncrement:
		return "increment"
	}
	return fmt.Sprintf("AutoGenerate(%d)", int(a))
}

// checkVersionCompat enforces the SAME-NAME structural invariants that make
// the multi-version union sound: two versions of one entity name share one
// physical table, so they MUST agree on which table (F5), whether the
// framework manages its DDL (F10), and at most one may declare a Seed (F11).
// The caller already holds r.mu. This is distinct from checkColumnConflicts,
// which is table-keyed and compares column/index/FK definitions; these are
// name-keyed and compare the entity-level shape.
func (r *Registry) checkVersionCompat(newKey entityKey, newEnt *entity.Entity) error {
	for k, existing := range r.entities {
		if k.name != newKey.name || k == newKey {
			continue
		}
		// F5: same name → same physical table. The union design merges versions
		// by name into one table; differing tables would silently drop every
		// non-representative version's table.
		if existing.GetTable() != newEnt.GetTable() {
			return fmt.Errorf(
				"registry: entity %q is registered with conflicting tables across versions — "+
					"%s uses table %q, %s uses table %q; versions of one entity share one table. "+
					"Use distinct entity names if you need distinct tables",
				newEnt.Config.Name,
				versionLabel(existing.Version), existing.GetTable(),
				versionLabel(newEnt.Version), newEnt.GetTable())
		}
		// F10: same name → same managed/unmanaged posture. An unmanaged
		// representative suppresses migration for the whole union, so a managed
		// version's columns would never be created.
		if existing.Config.Unmanaged != newEnt.Config.Unmanaged {
			return fmt.Errorf(
				"registry: entity %q (table %q) mixes managed and unmanaged versions — "+
					"%s is %s, %s is %s; versions of one entity must agree on who owns the DDL",
				newEnt.Config.Name, newEnt.GetTable(),
				versionLabel(existing.Version), managedLabel(existing.Config.Unmanaged),
				versionLabel(newEnt.Version), managedLabel(newEnt.Config.Unmanaged))
		}
		// F11: at most one version may declare a Seed. Two Seeds cannot be
		// compared for equality (they are funcs), so the second is ambiguous.
		if existing.Config.Seed != nil && newEnt.Config.Seed != nil {
			return fmt.Errorf(
				"registry: entity %q declares a Seed in multiple versions — "+
					"%s and %s; at most one version may seed a shared table",
				newEnt.Config.Name,
				versionLabel(existing.Version), versionLabel(newEnt.Version))
		}
	}
	return nil
}

// conflictingMandatoryFields rejects a column that is physically mandatory
// (Required, no Default, no AutoGenerate) in one entity but ABSENT in the
// other. Two entities sharing a table both write to it; a NOT NULL column
// with no default that one version cannot supply turns every complete,
// valid request through that version into a database constraint violation.
// The check is symmetric — both directions are examined (F6).
func conflictingMandatoryFields(a, b *entity.Entity) error {
	if err := findExclusiveMandatory(a, b); err != nil {
		return err
	}
	return findExclusiveMandatory(b, a)
}

// findExclusiveMandatory reports the first mandatory field declared by `have`
// whose lowercased name is absent from `lack`.
func findExclusiveMandatory(have, lack *entity.Entity) error {
	lackNames := make(map[string]bool, len(lack.Config.Fields))
	for _, f := range lack.Config.Fields {
		lackNames[strings.ToLower(f.Name)] = true
	}
	for _, f := range have.Config.Fields {
		if !isMandatoryColumn(f) {
			continue
		}
		if lackNames[strings.ToLower(f.Name)] {
			continue
		}
		return fmt.Errorf(
			"registry: entity %q (table %q): column %q is %s in %s but absent in %s — "+
				"a NOT NULL column with no default cannot be supplied by the other version's writes. "+
				"Give it a default, make it auto-generated, or declare it in both versions",
			have.Config.Name, have.Config.Table, f.Name,
			describeColumnSchema(f, f.Name == have.PrimaryKey),
			versionLabel(have.Version), versionLabel(lack.Version))
	}
	return nil
}

// isMandatoryColumn reports whether a field produces a NOT NULL column with
// no DEFAULT on any dialect — the column shape that breaks inserts which
// omit it. Required alone is not enough: an auto-generation path (AutoUUID,
// AutoTimestamp, AutoIncrement) or an explicit Default supplies the value, so
// the column is safe to omit from a write.
func isMandatoryColumn(f schema.Field) bool {
	return f.Required && f.AutoGenerate == schema.AutoNone && f.Default == nil
}

// conflictingIndices rejects a named index that two entities sharing a table
// declare with physically different definitions — different columns,
// expression, uniqueness, or column ordering. The merge keeps the first
// declaration; a silent divergence would violate the other version's declared
// invariant (e.g. v2 declares the index UNIQUE while v1 does not) (F8).
func conflictingIndices(a, b *entity.Entity) error {
	byName := make(map[string]entity.Index, len(a.Config.Indices))
	for _, idx := range a.Config.Indices {
		if n := indexDedupName(idx, a.Config.Table); n != "" {
			byName[n] = idx
		}
	}
	for _, idx := range b.Config.Indices {
		n := indexDedupName(idx, b.Config.Table)
		if n == "" {
			continue
		}
		prev, ok := byName[n]
		if !ok {
			continue
		}
		if !indexSchemaEqual(prev, idx) {
			return fmt.Errorf(
				"registry: entity %q (table %q): index %q is declared with incompatible definitions across versions — "+
					"%s declares %s, %s declares %s; a named index on a shared table must be identical "+
					"(columns, expression, uniqueness, ordering)",
				b.Config.Name, b.Config.Table, n,
				versionLabel(a.Version), describeIndex(prev),
				versionLabel(b.Version), describeIndex(idx))
		}
	}
	return nil
}

// indexDedupName returns the key the merge uses to deduplicate indices: the
// explicit Name, or the synthesised "idx_<table>_<cols>" slug for an unnamed
// index (mirroring indexDDL in migrate.go).
func indexDedupName(idx entity.Index, table string) string {
	if idx.Name != "" {
		return idx.Name
	}
	if len(idx.Columns) > 0 {
		return "idx_" + table + "_" + strings.Join(idx.Columns, "_")
	}
	return ""
}

// indexSchemaEqual reports whether two indices produce identical DDL.
func indexSchemaEqual(a, b entity.Index) bool {
	if a.Unique != b.Unique {
		return false
	}
	if a.Expression != b.Expression {
		return false
	}
	if len(a.Columns) != len(b.Columns) {
		return false
	}
	for i := range a.Columns {
		if a.Columns[i] != b.Columns[i] {
			return false
		}
	}
	return true
}

// describeIndex renders an index for the conflict error message.
func describeIndex(idx entity.Index) string {
	if idx.Expression != "" {
		unique := ""
		if idx.Unique {
			unique = "UNIQUE "
		}
		return fmt.Sprintf("%sINDEX(%s)", unique, idx.Expression)
	}
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}
	return fmt.Sprintf("%sINDEX(%s)", unique, strings.Join(idx.Columns, ", "))
}

// conflictingRelations rejects a foreign-key column (or a same-named
// logical relation) that two entities sharing a table declare with
// different physical targets — different relation type, target entity,
// target key, or pivot table. The merge keeps the first relation; a silent
// divergence would make the DDL reference the wrong table (e.g. v1 points
// owner_id at users, v2 at teams) (F9).
func conflictingRelations(a, b *entity.Entity) error {
	byKey := make(map[string]entity.Relation, len(a.Config.Relations))
	for _, rel := range a.Config.Relations {
		if k := relationDedupKey(rel); k != "" {
			byKey[k] = rel
		}
	}
	for _, rel := range b.Config.Relations {
		k := relationDedupKey(rel)
		if k == "" {
			continue
		}
		prev, ok := byKey[k]
		if !ok {
			continue
		}
		if !relationSchemaEqual(prev, rel) {
			return fmt.Errorf(
				"registry: entity %q (table %q): relation on %q is declared with incompatible definitions across versions — "+
					"%s declares %s, %s declares %s; a foreign-key column on a shared table must reference the same target "+
					"(relation type, target table, target key)",
				b.Config.Name, b.Config.Table, relationDisplayColumn(rel),
				versionLabel(a.Version), describeRelation(prev),
				versionLabel(b.Version), describeRelation(rel))
		}
	}
	return nil
}

// relationDedupKey returns the key the merge uses to deduplicate relations:
// the ForeignKey column (the physical column), or the logical Name for
// HasMany/HasOne relations that carry no FK column on this side.
func relationDedupKey(rel entity.Relation) string {
	if rel.ForeignKey != "" {
		return rel.ForeignKey
	}
	return rel.Name
}

// relationDisplayColumn returns the column to name in the error message.
func relationDisplayColumn(rel entity.Relation) string {
	if rel.ForeignKey != "" {
		return rel.ForeignKey
	}
	return rel.Name
}

// relationSchemaEqual reports whether two relations sharing a dedup key
// produce identical physical FK DDL (or, for non-FK relations, the same
// logical target).
func relationSchemaEqual(a, b entity.Relation) bool {
	return a.Type == b.Type &&
		a.Entity == b.Entity &&
		a.ForeignKeyTarget == b.ForeignKeyTarget &&
		a.Through == b.Through &&
		a.LocalKey == b.LocalKey
}

// describeRelation renders a relation for the conflict error message.
func describeRelation(rel entity.Relation) string {
	target := rel.Entity
	if rel.ForeignKeyTarget != "" {
		target = fmt.Sprintf("%s(%s)", rel.Entity, rel.ForeignKeyTarget)
	}
	col := relationDisplayColumn(rel)
	return fmt.Sprintf("%s %s→%s", relTypeLabel(rel.Type), col, target)
}

// relTypeLabel renders a relation type for diagnostics.
func relTypeLabel(t entity.RelationType) string {
	switch t {
	case entity.RelHasOne:
		return "HasOne"
	case entity.RelHasMany:
		return "HasMany"
	case entity.RelManyToOne:
		return "BelongsTo"
	case entity.RelManyToMany:
		return "ManyToMany"
	}
	return fmt.Sprintf("RelationType(%d)", int(t))
}
