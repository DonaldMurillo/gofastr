package framework

import (
	"database/sql"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

type entityKey struct {
	name    string
	version string
}

// Registry stores and retrieves Entity definitions keyed by (name, version).
// It is safe for concurrent use.
type Registry struct {
	mu         sync.RWMutex
	entities   map[entityKey]*entity.Entity
	db         *sql.DB
	handlerFor func(*entity.Entity) (*crud.CrudHandler, error)
}

// NewRegistry creates a new empty entity registry.
func NewRegistry() *Registry {
	return &Registry{
		entities: make(map[entityKey]*entity.Entity),
	}
}

// Register adds an entity under (Config.Name, Version). It errors on a nil
// or unnamed entity, a config Validate refuses, a (name, version) already
// held, and a declaration that disagrees with another on the same table.
func (r *Registry) Register(ent *entity.Entity) error {
	if ent == nil {
		return fmt.Errorf("registry: entity must not be nil")
	}
	if ent.Config.Name == "" {
		return fmt.Errorf("registry: entity name must not be empty")
	}
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
	if err := r.checkVersionCompat(key, ent); err != nil {
		return err
	}
	if err := r.checkTransitionTable(ent); err != nil {
		return err
	}
	if err := r.checkColumnConflicts(key, ent); err != nil {
		return err
	}
	if ent.DB == nil && r.db != nil {
		ent.DB = r.db
	}

	r.entities[key] = ent
	return nil
}

func (r *Registry) size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entities)
}

// Get returns the unversioned entity registered under name, else its only
// version. It errors when name is unknown or has several versions and no
// unversioned one; use GetVersioned for those.
func (r *Registry) Get(name string) (*entity.Entity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.entities[entityKey{name: name, version: ""}]; ok {
		return e, nil
	}

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

// GetVersioned returns the entity registered under name at version, the
// group prefix it mounted at ("" for App.Entity), or an error.
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

// All returns one entity per name: the unversioned one, else the lowest
// version. AllSorted returns every version in a stable order.
func (r *Registry) All() map[string]*entity.Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()
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

func pickRepresentative(group []*entity.Entity) *entity.Entity {
	for _, e := range group {
		if e.Version == "" {
			return e
		}
	}
	sort.Slice(group, func(i, j int) bool {
		return group[i].Version < group[j].Version
	})
	return group[0]
}

// AllSorted returns every registered entity, every version included,
// ordered by (name, version).
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

// SetDB sets the database on the registry and every registered entity.
func (r *Registry) SetDB(db *sql.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.db = db
	for _, e := range r.entities {
		e.DB = db
	}
}

// CrudHandlerFor returns App.CrudHandlerForEntity(ent) for the App that
// owns this registry, or nil, nil for one no App owns (NewRegistry).
func (r *Registry) CrudHandlerFor(ent *entity.Entity) (*crud.CrudHandler, error) {
	if r.handlerFor == nil {
		return nil, nil
	}
	return r.handlerFor(ent)
}

// checkColumnConflicts runs under r.mu.
func (r *Registry) checkColumnConflicts(newKey entityKey, newEnt *entity.Entity) error {
	if newEnt.Config.Unmanaged {
		return nil
	}
	newTable := asciicase.Lower(newEnt.GetTable())
	for k, existing := range r.entities {
		if k == newKey {
			continue
		}
		if existing.Config.Unmanaged || asciicase.Lower(existing.GetTable()) != newTable {
			continue
		}
		if err := conflictingColumns(existing, newEnt); err != nil {
			return err
		}
		if err := conflictingMandatoryFields(existing, newEnt); err != nil {
			return err
		}
		if err := conflictingIndices(existing, newEnt); err != nil {
			return err
		}
		if err := conflictingRelations(existing, newEnt); err != nil {
			return err
		}
	}
	return nil
}

func conflictingColumns(a, b *entity.Entity) error {
	if ka, kb := a.KeyColumns(), b.KeyColumns(); !slices.EqualFunc(ka, kb, strings.EqualFold) {
		return fmt.Errorf(
			"registry: entity %q (table %q): %s is keyed on (%s) but %s on (%s); two versions share one table so they must declare the same primary key",
			a.Config.Name, a.Config.Table,
			versionLabel(a.Version), strings.Join(ka, ", "), versionLabel(b.Version), strings.Join(kb, ", "),
		)
	}
	byName := make(map[string]schema.Field, len(a.Config.Fields))
	for _, f := range a.Config.Fields {
		byName[strings.ToLower(f.Name)] = f
	}
	for _, fb := range b.Config.Fields {
		fa, ok := byName[strings.ToLower(fb.Name)]
		if !ok {
			continue
		}
		aKey, bKey := slices.Contains(a.KeyColumns(), fa.Name), slices.Contains(b.KeyColumns(), fb.Name)
		if columnSchemaEqual(fa, aKey, fb, bKey) {
			continue
		}
		return fmt.Errorf(
			"registry: entity %q (table %q): column %q is declared with an incompatible schema across versions: "+
				"%s declares %s, %s declares %s; two versions share one table so the column definitions must match exactly "+
				"(type, nullability, uniqueness, default, auto/PK). Make both versions identical or rename the column in one version",
			a.Config.Name, a.Config.Table, fb.Name,
			versionLabel(a.Version), describeColumnSchema(fa, aKey),
			versionLabel(b.Version), describeColumnSchema(fb, bKey),
		)
	}
	return nil
}

func columnSchemaEqual(a schema.Field, aKey bool, b schema.Field, bKey bool) bool {
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
	if aKey != bKey {
		return false
	}
	if !reflect.DeepEqual(a.Default, b.Default) {
		return false
	}
	return true
}

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

func versionLabel(v string) string {
	if v == "" {
		return "version \"\" (unversioned)"
	}
	return fmt.Sprintf("version %q", v)
}

func managedLabel(unmanaged bool) string {
	if unmanaged {
		return "unmanaged (framework emits no DDL)"
	}
	return "managed (framework emits DDL)"
}

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

// checkVersionCompat runs under r.mu.
func (r *Registry) checkVersionCompat(newKey entityKey, newEnt *entity.Entity) error {
	for k, existing := range r.entities {
		if k.name != newKey.name || k == newKey {
			continue
		}
		if existing.GetTable() != newEnt.GetTable() {
			return fmt.Errorf(
				"registry: entity %q is registered with conflicting tables across versions: "+
					"%s uses table %q, %s uses table %q; versions of one entity share one table. "+
					"Use distinct entity names if you need distinct tables",
				newEnt.Config.Name,
				versionLabel(existing.Version), existing.GetTable(),
				versionLabel(newEnt.Version), newEnt.GetTable())
		}
		if existing.Config.Unmanaged != newEnt.Config.Unmanaged {
			return fmt.Errorf(
				"registry: entity %q (table %q) mixes managed and unmanaged versions: "+
					"%s is %s, %s is %s; versions of one entity must agree on who owns the DDL",
				newEnt.Config.Name, newEnt.GetTable(),
				versionLabel(existing.Version), managedLabel(existing.Config.Unmanaged),
				versionLabel(newEnt.Version), managedLabel(newEnt.Config.Unmanaged))
		}
		if err := checkRowIsolationCompat(existing, newEnt); err != nil {
			return err
		}
		if existing.Config.Seed != nil && newEnt.Config.Seed != nil {
			return fmt.Errorf(
				"registry: entity %q declares a Seed in multiple versions: "+
					"%s and %s; at most one version may seed a shared table",
				newEnt.Config.Name,
				versionLabel(existing.Version), versionLabel(newEnt.Version))
		}
	}
	return nil
}

func (r *Registry) checkTransitionTable(newEnt *entity.Entity) error {
	if err := checkTransitionForeignKeys(newEnt, newEnt); err != nil {
		return err
	}
	for _, existing := range r.entities {
		if !strings.EqualFold(existing.GetTable(), newEnt.GetTable()) {
			continue
		}
		for _, p := range [][2]*entity.Entity{{existing, newEnt}, {newEnt, existing}} {
			if err := checkTransitionForeignKeys(p[0], p[1]); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkTransitionForeignKeys(rels, moves *entity.Entity) error {
	s := moves.Config.States
	if s == nil {
		return nil
	}
	for _, rel := range rels.Config.Relations {
		fks := rel.ForeignKey
		if rel.Type != entity.RelManyToOne || len(fks) < 2 {
			continue
		}
		for _, c := range fks {
			if strings.EqualFold(s.Field, c) || slices.ContainsFunc(s.Transitions, func(t entity.Transition) bool { return strings.EqualFold(t.Stamp, c) }) {
				return fmt.Errorf(
					"registry: entity %q (%s): the transitions of %q (%s) write %q, one column of relation %q's foreign key (%s) on table %q",
					rels.Config.Name, versionLabel(rels.Version), moves.Config.Name, versionLabel(moves.Version),
					c, rel.Name, strings.Join(fks, ", "), rels.GetTable())
			}
		}
	}
	return nil
}

func checkRowIsolationCompat(existing, newEnt *entity.Entity) error {
	mismatch := func(setting, existingVal, newVal string) error {
		return fmt.Errorf(
			"registry: entity %q (table %q) mixes %s across versions: "+
				"%s is %s, %s is %s; versions of one entity share one table, so they must agree "+
				"on which rows a request may see. Use distinct entity names if the versions really "+
				"need different row scoping",
			newEnt.Config.Name, newEnt.GetTable(), setting,
			versionLabel(existing.Version), existingVal,
			versionLabel(newEnt.Version), newVal)
	}
	if existing.Config.Scope.MultiTenant != newEnt.Config.Scope.MultiTenant {
		return mismatch("tenant scoping",
			multiTenantLabel(existing.Config.Scope.MultiTenant),
			multiTenantLabel(newEnt.Config.Scope.MultiTenant))
	}
	if existing.Config.Scope.MultiTenant &&
		existing.Config.TenantColumn() != newEnt.Config.TenantColumn() {
		return mismatch("tenant column",
			existing.Config.TenantColumn(),
			newEnt.Config.TenantColumn())
	}
	if existing.Config.Scope.OwnerField != newEnt.Config.Scope.OwnerField {
		return mismatch("owner scoping",
			ownerFieldLabel(existing.Config.Scope.OwnerField),
			ownerFieldLabel(newEnt.Config.Scope.OwnerField))
	}
	if existing.Config.Scope.SoftDelete != newEnt.Config.Scope.SoftDelete {
		return mismatch("delete semantics",
			softDeleteLabel(existing.Config.Scope.SoftDelete),
			softDeleteLabel(newEnt.Config.Scope.SoftDelete))
	}
	if existing.Config.Exposure.Public != newEnt.Config.Exposure.Public {
		return mismatch("public access",
			publicLabel(existing.Config.Exposure.Public),
			publicLabel(newEnt.Config.Exposure.Public))
	}
	if existing.Config.Exposure.Access != newEnt.Config.Exposure.Access {
		return mismatch("access control",
			accessLabel(existing.Config.Exposure.Access),
			accessLabel(newEnt.Config.Exposure.Access))
	}
	if existing.Config.Scope.CrossOwnerRead != newEnt.Config.Scope.CrossOwnerRead {
		return mismatch("cross-owner read",
			crossOwnerReadLabel(existing.Config.Scope.CrossOwnerRead),
			crossOwnerReadLabel(newEnt.Config.Scope.CrossOwnerRead))
	}
	return nil
}

func publicLabel(public bool) string {
	if public {
		return "public (no session required)"
	}
	return "session-required"
}

func accessLabel(a entity.AccessControl) string {
	if !a.Declared() {
		return "un-gated"
	}
	parts := make([]string, 0, 4)
	for _, p := range []struct{ op, perm string }{
		{"read", a.Read}, {"create", a.Create},
		{"update", a.Update}, {"delete", a.Delete},
	} {
		if p.perm != "" {
			parts = append(parts, p.op+"="+p.perm)
		}
	}
	return strings.Join(parts, " ")
}

func crossOwnerReadLabel(perm string) string {
	if perm == "" {
		return "owner-scoped"
	}
	return "lifted by " + perm
}

func multiTenantLabel(on bool) string {
	if on {
		return "MultiTenant (rows scoped to the request's tenant)"
	}
	return "not MultiTenant (rows unscoped)"
}

func ownerFieldLabel(field string) string {
	if field == "" {
		return "no OwnerField (rows unscoped)"
	}
	return fmt.Sprintf("OwnerField %q (rows scoped to their owner)", field)
}

func softDeleteLabel(on bool) string {
	if on {
		return "SoftDelete (deletes stamp deleted_at)"
	}
	return "not SoftDelete (deletes remove the row)"
}

func conflictingMandatoryFields(a, b *entity.Entity) error {
	if err := findExclusiveMandatory(a, b); err != nil {
		return err
	}
	return findExclusiveMandatory(b, a)
}

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
			"registry: entity %q (table %q): column %q is %s in %s but absent in %s: "+
				"a NOT NULL column with no default cannot be supplied by the other version's writes. "+
				"Give it a default, make it auto-generated, or declare it in both versions",
			have.Config.Name, have.Config.Table, f.Name,
			describeColumnSchema(f, slices.Contains(have.KeyColumns(), f.Name)),
			versionLabel(have.Version), versionLabel(lack.Version))
	}
	return nil
}

func isMandatoryColumn(f schema.Field) bool {
	return f.Required && f.AutoGenerate == schema.AutoNone && f.Default == nil
}

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
				"registry: entity %q (table %q): index %q is declared with incompatible definitions across versions: "+
					"%s declares %s, %s declares %s; a named index on a shared table must be identical "+
					"(columns, expression, uniqueness, ordering)",
				b.Config.Name, b.Config.Table, n,
				versionLabel(a.Version), describeIndex(prev),
				versionLabel(b.Version), describeIndex(idx))
		}
	}
	return nil
}

func indexDedupName(idx entity.Index, table string) string {
	if idx.Name != "" {
		return idx.Name
	}
	if len(idx.Columns) > 0 {
		return entity.DefaultIndexName(table, idx.Columns)
	}
	return ""
}

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
				"registry: entity %q (table %q): relation on %q is declared with incompatible definitions across versions: "+
					"%s declares %s, %s declares %s; a foreign-key column on a shared table must reference the same target "+
					"(relation type, target table, target key)",
				b.Config.Name, b.Config.Table, relationDisplayColumn(rel),
				versionLabel(a.Version), describeRelation(prev),
				versionLabel(b.Version), describeRelation(rel))
		}
	}
	return nil
}

func relationDedupKey(rel entity.Relation) string {
	if fks := rel.ForeignKey; len(fks) > 0 {
		return strings.Join(fks, ",")
	}
	return rel.Name
}

func relationDisplayColumn(rel entity.Relation) string {
	if fks := rel.ForeignKey; len(fks) > 0 {
		return strings.Join(fks, ", ")
	}
	return rel.Name
}

func relationSchemaEqual(a, b entity.Relation) bool {
	return a.Type == b.Type &&
		a.Entity == b.Entity &&
		a.ForeignKeyTarget == b.ForeignKeyTarget &&
		a.Through == b.Through &&
		a.LocalKey == b.LocalKey
}

func describeRelation(rel entity.Relation) string {
	target := rel.Entity
	if rel.ForeignKeyTarget != "" {
		target = fmt.Sprintf("%s(%s)", rel.Entity, rel.ForeignKeyTarget)
	}
	col := relationDisplayColumn(rel)
	return fmt.Sprintf("%s %s→%s", relTypeLabel(rel.Type), col, target)
}

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
