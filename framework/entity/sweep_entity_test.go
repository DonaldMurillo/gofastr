package entity

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestSweepEntityStateFieldIsNotTheKey(t *testing.T) {
	err := statesEntityErr(t, func(c *EntityConfig, _ *StatesConfig) {
		c.PrimaryKey = []string{"status"}
	})
	if err == nil || !strings.Contains(err.Error(), `states field "status" is the primary key`) {
		t.Fatalf("Validate = %v, want the primary-key refusal", err)
	}
}

func TestSweepEntityValidateDisplayReturnsDefinePanic(t *testing.T) {
	err := ValidateDisplay("memos", EntityConfig{
		Fields:       []schema.Field{{Name: "title", Type: schema.String}},
		SearchFields: []string{"nope"},
		Display:      &DisplayConfig{},
	})
	if err == nil || !strings.Contains(err.Error(), `SearchFields entry "nope"`) {
		t.Fatalf("ValidateDisplay = %v, want Define's refusal as the error", err)
	}
}

func TestSweepEntityGuardedHoldsFieldAndStamps(t *testing.T) {
	s := statesFixture()
	s.Transitions = append(s.Transitions, Transition{Key: "repay", From: []string{"void"}, To: "paid", Stamp: "paid_on"})
	if got, want := s.Guarded(), []string{"status", "paid_on"}; !slices.Equal(got, want) {
		t.Fatalf("Guarded() = %v, want %v", got, want)
	}
}

func TestSweepEntityViewAsIsTableOrCards(t *testing.T) {
	fields := []schema.Field{{Name: "title", Type: schema.String}}
	for as, ok := range map[string]bool{"": true, "table": true, "cards": true, "grid": false} {
		err := (&DisplayConfig{Views: []ListView{{Key: "open", As: as}}}).validate("memos", fields, nil)
		if (err == nil) != ok {
			t.Errorf("view as %q: err = %v, want accepted=%v", as, err, ok)
		}
	}
}

func sweepDefinePanic(t *testing.T, cfg EntityConfig) string {
	t.Helper()
	var msg string
	func() {
		defer func() {
			if r := recover(); r != nil {
				msg = r.(string)
			}
		}()
		Define("things", cfg)
	}()
	return msg
}

func TestSweepEntityUnsafeTenantFieldPanics(t *testing.T) {
	msg := sweepDefinePanic(t, EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Scope:  &ScopeConfig{MultiTenant: true, TenantField: "tenant; DROP"},
	})
	if !strings.Contains(msg, "TenantField") {
		t.Fatalf("Define panic = %q, want the TenantField refusal", msg)
	}
}

func TestSweepEntityNoQuerySearchFieldPanics(t *testing.T) {
	msg := sweepDefinePanic(t, EntityConfig{
		Fields:       []schema.Field{{Name: "secret", Type: schema.String, NoQuery: true}},
		SearchFields: []string{"secret"},
	})
	if !strings.Contains(msg, `SearchFields entry "secret" is NoQuery`) {
		t.Fatalf("Define panic = %q, want the NoQuery refusal", msg)
	}
}

func TestSweepEntityDeclarationRefusesHiddenCompositeKey(t *testing.T) {
	d := EntityDeclaration{
		Name:       "links",
		PrimaryKey: []string{"a", "b"},
		Fields:     []FieldDeclaration{{Name: "a", Type: "string"}, {Name: "b", Type: "string", Hidden: true}},
	}
	if _, err := d.Config(); err == nil || !strings.Contains(err.Error(), `composite key column "b" is Hidden or NoQuery`) {
		t.Fatalf("Config() = %v, want the composite-key refusal", err)
	}
}

func TestSweepEntityManyRelationDerivesNoBelongsTo(t *testing.T) {
	e := Define("posts", EntityConfig{Fields: []schema.Field{{Name: "tags", Type: schema.Relation, To: "tags", Many: true}}})
	if len(e.Config.Relations) != 0 {
		t.Fatalf("a Many relation field derived %v", e.Config.Relations)
	}
}

func TestSweepEntityDeclarationRefusesBadPrimaryKey(t *testing.T) {
	for name, pk := range map[string][]string{"undeclared": {"a", "nope"}, "empty": {}} {
		d := EntityDeclaration{Name: "links", PrimaryKey: pk, Fields: []FieldDeclaration{{Name: "a", Type: "string"}}}
		if _, err := d.Config(); err == nil {
			t.Errorf("%s primary key %v accepted", name, pk)
		}
	}
	d := EntityDeclaration{Name: "links", PrimaryKey: []string{"a", "nope"}, Fields: []FieldDeclaration{{Name: "a", Type: "string"}}}
	if _, err := d.Config(); err == nil || !strings.Contains(err.Error(), `primary key column "nope" is not a declared field`) {
		t.Errorf("Config() = %v, want the undeclared-column refusal", err)
	}
}

func TestSweepEntityRawTableWithNoKeyNamesNoRecord(t *testing.T) {
	e := &Entity{Config: EntityConfig{Name: "logs", PrimaryKey: []string{}}}
	if err := e.CheckRecordKey(); err == nil {
		t.Fatal("CheckRecordKey accepted a table with no key")
	}
}

func TestSweepEntityRawTypeKeysOnlyIntegers(t *testing.T) {
	if keyable(schema.Field{Name: "code", Type: schema.String, RawType: "INTEGER"}) {
		t.Fatal("a String column with a RawType is keyable")
	}
	if keyable(schema.Field{Name: "n", Type: schema.Int, RawType: "ＩＮＴ"}) {
		t.Fatal("a non-ASCII RawType is keyable")
	}
}

func TestSweepEntityInjectsHiddenScopeColumns(t *testing.T) {
	e := Define("notes", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Scope:  &ScopeConfig{MultiTenant: true, TenantField: "org_id", SoftDelete: true},
	})
	for _, col := range []string{"org_id", "deleted_at"} {
		i := slices.IndexFunc(e.Config.Fields, func(f schema.Field) bool { return f.Name == col })
		if i < 0 {
			t.Fatalf("Define injected no %q column: %v", col, e.Config.Fields)
		}
		if f := e.Config.Fields[i]; !f.Hidden || !f.ReadOnly {
			t.Errorf("injected %q is not Hidden and ReadOnly: %+v", col, f)
		}
	}
}

func TestSweepEntityDefineCopiesSubConfigs(t *testing.T) {
	crud, ts := true, true
	scope := &ScopeConfig{OwnerField: "owner_id"}
	pag := &PaginationConfig{CursorFields: []string{"title"}}
	rs := &ReadScopeConfig{Filter: []RowPredicate{{Field: "title", Value: "x"}}}
	rels := []Relation{{Name: "author", Type: RelManyToOne, Entity: "users", ForeignKey: []string{"author_id"}}}
	e := Define("notes", EntityConfig{
		Fields:     []schema.Field{{Name: "title", Type: schema.String, Required: true}, {Name: "author_id", Type: schema.String}},
		Scope:      scope,
		Pagination: pag,
		Exposure:   &ExposureConfig{CRUD: &crud, ReadScope: rs},
		Timestamps: &ts,
		Relations:  rels,
	})
	scope.OwnerField = "other"
	pag.CursorFields[0] = "other"
	crud, ts = false, false
	rs.Filter[0].Field = "other"
	rels[0].ForeignKey[0] = "other"
	c := e.Config
	if c.Scope.OwnerField != "owner_id" || c.Pagination.CursorFields[0] != "title" || !*c.Exposure.CRUD ||
		c.Exposure.ReadScope.Filter[0].Field != "title" || !*c.Timestamps || c.Relations[0].ForeignKey[0] != "author_id" {
		t.Fatalf("a caller edit after Define reached the entity: %+v", c)
	}
}

func TestSweepEntityUniqueKeyNames(t *testing.T) {
	if got := UniqueKeyName("app.users", "email"); got != "users_email_key" {
		t.Errorf("UniqueKeyName(app.users, email) = %q, want the schema left off", got)
	}
	if LegacyUniqueKeyName("users", "email") != UniqueKeyName("users", "email") {
		t.Error("legacy and current names differ for a short name")
	}
	long := strings.Repeat("t", 70)
	if LegacyUniqueKeyName(long, "email") == UniqueKeyName(long, "email") {
		t.Error("legacy name past 63 bytes is the hashed name, want the plain trimmed one")
	}
}

func TestSweepEntityAccessAndTenantDefaults(t *testing.T) {
	if !(AccessControl{Read: "posts:read"}).Declared() {
		t.Error("an AccessControl with only Read set is not Declared")
	}
	if got := (EntityConfig{}).TenantColumn(); got != "tenant_id" {
		t.Errorf("TenantColumn() = %q, want tenant_id", got)
	}
}

func TestSweepEntityOrderKeepsRawKeywordName(t *testing.T) {
	if col, dir := NewStringColumn("order").Asc().Term(); col != "order" || dir != "ASC" {
		t.Fatalf("Asc().Term() = %q %q, want the raw column; the ORDER BY slot quotes it", col, dir)
	}
}

type sweepReg map[string]*Entity

func (r sweepReg) All() map[string]*Entity { return r }
func (r sweepReg) AllSorted() []*Entity {
	out := make([]*Entity, 0, len(r))
	for _, e := range r {
		out = append(out, e)
	}
	return out
}
func (r sweepReg) Get(name string) (*Entity, error) {
	if e, ok := r[name]; ok {
		return e, nil
	}
	return nil, errors.New("not found: " + name)
}

func TestSweepEntityKeyColumnsIsACopy(t *testing.T) {
	e := Define("links", EntityConfig{PrimaryKey: []string{"a", "b"}, Fields: []schema.Field{{Name: "a", Type: schema.String}, {Name: "b", Type: schema.String}}})
	e.KeyColumns()[0] = "x"
	if e.KeyColumns()[0] != "a" {
		t.Fatal("editing KeyColumns' result changed the entity's key")
	}
}

func TestSweepEntityForeignKeyRepeatIgnoresCase(t *testing.T) {
	target := Define("orders", EntityConfig{PrimaryKey: []string{"store", "no"}, Fields: []schema.Field{{Name: "store", Type: schema.String}, {Name: "no", Type: schema.String}}})
	source := Define("lines", EntityConfig{Fields: []schema.Field{{Name: "a", Type: schema.String}, {Name: "A", Type: schema.String}}})
	if err := CheckRelationKeys(source, BelongsTo("order", "orders", "a", "A"), target); !errors.Is(err, ErrForeignKeyShape) {
		t.Fatalf("CheckRelationKeys = %v, want ErrForeignKeyShape for a column named twice in two cases", err)
	}
}

func TestSweepEntityValueFieldsFollowIntegerKeys(t *testing.T) {
	users := Define("users", EntityConfig{Fields: []schema.Field{{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement}}})
	profiles := Define("profiles", EntityConfig{PrimaryKey: []string{"user_id"}, Fields: []schema.Field{{Name: "user_id", Type: schema.Relation, To: "users", Required: true}}})
	notes := Define("notes", EntityConfig{Fields: []schema.Field{{Name: "profile_id", Type: schema.Relation, To: "profiles"}}})
	nodes := Define("nodes", EntityConfig{PrimaryKey: []string{"parent"}, Fields: []schema.Field{{Name: "parent", Type: schema.Relation, To: "nodes", Required: true}}})
	reg := sweepReg{"users": users, "profiles": profiles, "notes": notes, "nodes": nodes}
	for _, f := range ValueFields(reg, notes) {
		if f.Name == "profile_id" && f.Type != schema.Int {
			t.Fatalf("notes.profile_id reaches an integer key through profiles.user_id, typed %v", f.Type)
		}
	}
	if RelationHoldsInt(reg, nodes, "parent") {
		t.Fatal("a self-referencing key holds an integer")
	}
}

func TestSweepEntityDeclarationConfigOwnsItsSlices(t *testing.T) {
	d := EntityDeclaration{
		Name:       "links",
		PrimaryKey: []string{"a", "b"},
		Fields:     []FieldDeclaration{{Name: "a", Type: "string"}, {Name: "b", Type: "string"}},
		Pagination: &PaginationDeclaration{CursorFields: []string{"a"}},
		Exposure:   &ExposureDeclaration{ReadScope: &ReadScopeDeclaration{Filter: []RowPredicateDeclaration{{Field: "a", Op: "in", Values: []string{"x"}}}}},
	}
	cfg, err := d.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	d.PrimaryKey[0] = "z"
	d.Pagination.CursorFields[0] = "z"
	d.Exposure.ReadScope.Filter[0].Values[0] = "z"
	if cfg.PrimaryKey[0] != "a" || cfg.Pagination.CursorFields[0] != "a" || cfg.Exposure.ReadScope.Filter[0].Values[0] != "x" {
		t.Fatalf("an edit to the declaration reached the config: %+v", cfg)
	}
}
