package entity

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// --- column.go: typed query DSL ---

func sqlOf(c Condition) string { return c.SQL() }

func TestColumnConditions(t *testing.T) {
	s := NewStringColumn("name")
	i := NewIntColumn("age")
	f := NewFloatColumn("score")
	b := NewBoolColumn("active")
	ts := NewTimestampColumn("created_at")
	u := NewUUIDColumn("id")

	checks := map[string]Condition{
		"name = $1":          s.Eq("x"),
		"name != $1":         s.Neq("x"),
		"name LIKE $1":       s.Like("%x%"),
		"name NOT LIKE $1":   s.NotLike("%x%"),
		"name IN ($1, $2)":   s.In("a", "b"),
		"age = $1":           i.Eq(1),
		"age != $1":          i.Neq(1),
		"age > $1":           i.Gt(1),
		"age >= $1":          i.Gte(1),
		"age < $1":           i.Lt(1),
		"age <= $1":          i.Lte(1),
		"age IN ($1, $2)":    i.In(1, 2),
		"score = $1":         f.Eq(1),
		"score != $1":        f.Neq(1),
		"score > $1":         f.Gt(1),
		"score >= $1":        f.Gte(1),
		"score < $1":         f.Lt(1),
		"score <= $1":        f.Lte(1),
		"active = $1":        b.Eq(true),
		"created_at = $1":    ts.Eq(1),
		"created_at > $1":    ts.Gt(1),
		"created_at >= $1":   ts.Gte(1),
		"created_at < $1":    ts.Lt(1),
		"created_at <= $1":   ts.Lte(1),
		"id = $1":            u.Eq("x"),
		"id != $1":           u.Neq("x"),
		"id IN ($1, $2)":     u.In("a", "b"),
		"name IS NULL":       s.IsNull(),
		"name IS NOT NULL":   s.IsNotNull(),
	}
	for want, cond := range checks {
		if got := sqlOf(cond); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
	// Bool sugar.
	if b.IsTrue().SQL() != "active = $1" || b.IsTrue().Args()[0] != true {
		t.Error("IsTrue")
	}
	if b.IsFalse().Args()[0] != false {
		t.Error("IsFalse")
	}
	// Order: apply Asc/Desc to a builder and confirm direction renders.
	qb := query.Select("name").From("t")
	s.Asc().Apply(qb)
	NewIntColumn("age").Desc().Apply(qb)
	osql, _ := qb.Build()
	up := strings.ToUpper(osql)
	if !strings.Contains(up, "ASC") || !strings.Contains(up, "DESC") {
		t.Errorf("order directions missing: %s", osql)
	}
}

func TestConditionCombinators(t *testing.T) {
	a := NewIntColumn("a").Eq(1)
	b := NewIntColumn("b").Eq(2)

	if And().SQL() != "1 = 1" {
		t.Error("And() empty")
	}
	if And(a).SQL() != a.SQL() {
		t.Error("And(one) passthrough")
	}
	if got := And(a, b).SQL(); got != "(a = $1 AND b = $1)" {
		t.Errorf("And(two)=%q", got)
	}
	if Or().SQL() != "1 = 0" {
		t.Error("Or() empty")
	}
	if Or(a).SQL() != a.SQL() {
		t.Error("Or(one) passthrough")
	}
	if got := Or(a, b).SQL(); got != "(a = $1 OR b = $1)" {
		t.Errorf("Or(two)=%q", got)
	}
	if got := Not(a).SQL(); got != "NOT (a = $1)" {
		t.Errorf("Not=%q", got)
	}
	// In with no values -> tautologically false.
	if NewIntColumn("x").In().SQL() != "1 = 0" {
		t.Error("empty In")
	}
	if len(And(a, b).Args()) != 2 {
		t.Error("And args concatenated")
	}
}

func TestConditionApplyOnQueryBuilder(t *testing.T) {
	qb := query.Select("id").From("t")
	NewIntColumn("a").Eq(7).Apply(qb)
	NewStringColumn("a").Asc().Apply(qb)
	sql, args := qb.Build()
	if !strings.Contains(sql, "a = $1") || !strings.Contains(strings.ToUpper(sql), "ORDER BY") {
		t.Errorf("apply produced: %s", sql)
	}
	if len(args) != 1 || args[0] != 7 {
		t.Errorf("args: %v", args)
	}
}

// --- declaration.go ---

func writeJSON(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadEntityDeclaration(t *testing.T) {
	dir := t.TempDir()
	// Missing file.
	if _, err := LoadEntityDeclaration(filepath.Join(dir, "nope.json")); err == nil {
		t.Error("missing file should error")
	}
	// Bad JSON.
	bad := writeJSON(t, dir, "bad.json", "{not json")
	if _, err := LoadEntityDeclaration(bad); err == nil {
		t.Error("bad json should error")
	}
	// Name derived from filename when omitted.
	noName := writeJSON(t, dir, "widgets.json", `{"fields":[{"name":"title","type":"string"}]}`)
	d, err := LoadEntityDeclaration(noName)
	if err != nil || d.Name != "widgets" {
		t.Fatalf("name-from-file: %v name=%q", err, d.Name)
	}
	// MCP endpoint in JSON is rejected.
	mcpDecl := writeJSON(t, dir, "mcp.json", `{"name":"x","fields":[{"name":"t","type":"string"}],"endpoints":[{"method":"GET","path":"/x","mcp":true}]}`)
	if _, err := LoadEntityDeclaration(mcpDecl); err == nil {
		t.Error("mcp endpoint should be rejected")
	}
	// Invalid config (bad field type).
	badField := writeJSON(t, dir, "bf.json", `{"name":"x","fields":[{"name":"t","type":"bogus"}]}`)
	if _, err := LoadEntityDeclaration(badField); err == nil {
		t.Error("bad field type should error")
	}
}

func TestLoadEntityDeclarations(t *testing.T) {
	// Missing dir.
	if _, err := LoadEntityDeclarations(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("missing dir should error")
	}
	dir := t.TempDir()
	writeJSON(t, dir, "b.json", `{"name":"b","fields":[{"name":"t","type":"string"}]}`)
	writeJSON(t, dir, "a.json", `{"name":"a","fields":[{"name":"t","type":"string"}]}`)
	writeJSON(t, dir, "ignore.txt", "not json")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	decls, err := LoadEntityDeclarations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(decls) != 2 || decls[0].Name != "a" || decls[1].Name != "b" {
		t.Fatalf("expected sorted [a b], got %+v", decls)
	}
	// A bad file in the dir surfaces an error.
	writeJSON(t, dir, "c.json", "{broken")
	if _, err := LoadEntityDeclarations(dir); err == nil {
		t.Error("bad file in dir should error")
	}
}

func TestDeclarationConfigAndField(t *testing.T) {
	// Empty name.
	if _, err := (EntityDeclaration{}).Config(); err == nil {
		t.Error("empty name")
	}
	// Bad field bubbles up.
	if _, err := (EntityDeclaration{Name: "x", Fields: []FieldDeclaration{{Name: "f", Type: "bogus"}}}).Config(); err == nil {
		t.Error("bad field type in config")
	}
	// Timestamps pointer applied.
	tsTrue := true
	cfg, err := (EntityDeclaration{Name: "x", Timestamps: &tsTrue, Fields: []FieldDeclaration{{Name: "f", Type: "string"}}}).Config()
	if err != nil || !cfg.Timestamps {
		t.Fatalf("timestamps: %v %+v", err, cfg)
	}
	// Field: empty name.
	if _, err := (FieldDeclaration{}).Field(); err == nil {
		t.Error("field empty name")
	}
	// Field: bad auto_generate.
	if _, err := (FieldDeclaration{Name: "f", Type: "string", AutoGenerate: "bogus"}).Field(); err == nil {
		t.Error("bad auto_generate")
	}
	// Field: full ok.
	f, err := (FieldDeclaration{Name: "f", Type: "uuid", AutoGenerate: "uuid"}).Field()
	if err != nil || f.Type != schema.UUID || f.AutoGenerate != schema.AutoUUID {
		t.Fatalf("field ok: %v %+v", err, f)
	}
}

func TestParseFieldType(t *testing.T) {
	cases := map[string]schema.FieldType{
		"": schema.String, "string": schema.String, "text": schema.Text,
		"int": schema.Int, "integer": schema.Int, "float": schema.Float, "number": schema.Float,
		"decimal": schema.Decimal, "bool": schema.Bool, "boolean": schema.Bool, "enum": schema.Enum,
		"uuid": schema.UUID, "timestamp": schema.Timestamp, "datetime": schema.Timestamp,
		"date": schema.Date, "json": schema.JSON, "relation": schema.Relation,
		"image": schema.Image, "file": schema.File,
	}
	for in, want := range cases {
		got, err := parseFieldType(in)
		if err != nil || got != want {
			t.Errorf("parseFieldType(%q)=%v,%v want %v", in, got, err, want)
		}
	}
	if _, err := parseFieldType("bogus"); err == nil {
		t.Error("unknown type")
	}
}

func TestParseAutoGenerate(t *testing.T) {
	cases := map[string]schema.AutoGenerate{
		"": schema.AutoNone, "none": schema.AutoNone, "uuid": schema.AutoUUID,
		"timestamp": schema.AutoTimestamp, "increment": schema.AutoIncrement, "auto_increment": schema.AutoIncrement,
	}
	for in, want := range cases {
		got, err := parseAutoGenerate(in)
		if err != nil || got != want {
			t.Errorf("parseAutoGenerate(%q)=%v,%v want %v", in, got, err, want)
		}
	}
	if _, err := parseAutoGenerate("bogus"); err == nil {
		t.Error("unknown auto_generate")
	}
}

// --- entity.go: String / Validate / toSnake / Define injection skips ---

func TestEntityString(t *testing.T) {
	e := Define("posts", EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
	if got := e.String(); !strings.Contains(got, "posts") {
		t.Errorf("String()=%q", got)
	}
}

func TestEntityValidate(t *testing.T) {
	base := func() *Entity {
		return &Entity{Config: EntityConfig{Name: "x", Table: "x", Fields: []schema.Field{{Name: "a", Type: schema.String}}}}
	}
	if err := base().Validate(); err != nil {
		t.Errorf("valid: %v", err)
	}
	if err := (&Entity{}).Validate(); err == nil {
		t.Error("empty name")
	}
	if err := (&Entity{Config: EntityConfig{Name: "x"}}).Validate(); err == nil {
		t.Error("empty table")
	}
	if err := (&Entity{Config: EntityConfig{Name: "x", Table: "x"}}).Validate(); err == nil {
		t.Error("no fields")
	}
	if err := (&Entity{Config: EntityConfig{Name: "x", Table: "x", Fields: []schema.Field{{Name: ""}}}}).Validate(); err == nil {
		t.Error("empty field name")
	}
	dup := &Entity{Config: EntityConfig{Name: "x", Table: "x", Fields: []schema.Field{{Name: "a"}, {Name: "a"}}}}
	if err := dup.Validate(); err == nil {
		t.Error("duplicate field")
	}
	rel := &Entity{Config: EntityConfig{Name: "x", Table: "x", Fields: []schema.Field{{Name: "a", Type: schema.Relation}}}}
	if err := rel.Validate(); err == nil {
		t.Error("relation without To")
	}
}

func TestToSnake(t *testing.T) {
	cases := map[string]string{
		"already_snake": "already_snake",
		"CamelCase":     "camel_case",
		"kebab-case":    "kebab_case",
		"with space":    "with_space",
		"HTTPServer":    "h_t_t_p_server",
	}
	for in, want := range cases {
		if got := toSnake(in); got != want {
			t.Errorf("toSnake(%q)=%q want %q", in, got, want)
		}
	}
}

func TestDefine_SkipsPreDeclaredInjectedColumns(t *testing.T) {
	ts := true
	e := Define("things", EntityConfig{
		Timestamps:  ts,
		SoftDelete:  true,
		MultiTenant: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "created_at", Type: schema.Timestamp},
			{Name: "updated_at", Type: schema.Timestamp},
			{Name: "deleted_at", Type: schema.Timestamp},
			{Name: "tenant_id", Type: schema.String},
		},
	})
	// No duplicate columns were injected.
	counts := map[string]int{}
	for _, f := range e.Config.Fields {
		counts[f.Name]++
	}
	for _, col := range []string{"created_at", "updated_at", "deleted_at", "tenant_id"} {
		if counts[col] != 1 {
			t.Errorf("%s appears %d times (injection should skip pre-declared)", col, counts[col])
		}
	}
}

// --- validator.go: isZero via Required + FormatValidationErrors ---

func TestRequired_IsZeroAllKinds(t *testing.T) {
	req := Required("a", "b", "c", "d", "e", "f", "g")
	data := map[string]any{
		"a": "",          // string zero
		"b": 0,           // int zero
		"c": int64(0),    // int64 zero
		"d": 0.0,         // float zero
		"e": false,       // bool zero
		"f": nil,         // nil
		"g": []string{},  // default branch: non-zero (slice) -> not flagged
	}
	errs := req(context.Background(), data)
	for _, k := range []string{"a", "b", "c", "d", "e", "f"} {
		if errs[k] == "" {
			t.Errorf("expected %q flagged as required-zero", k)
		}
	}
	if errs["g"] != "" {
		t.Error("non-zero slice should not be flagged")
	}
	// Missing key also flagged.
	if Required("z")(context.Background(), map[string]any{})["z"] == "" {
		t.Error("missing key should be required")
	}
	// Present non-zero passes.
	if len(Required("a")(context.Background(), map[string]any{"a": "x"})) != 0 {
		t.Error("present value should pass")
	}
}

func TestFormatValidationErrors(t *testing.T) {
	if FormatValidationErrors(nil) != nil {
		t.Error("empty -> nil")
	}
	out := FormatValidationErrors(map[string]string{"title": "is required"})
	if len(out) != 1 || out[0] != "title is required" {
		t.Errorf("format: %v", out)
	}
}

func TestCustomValidator(t *testing.T) {
	v := Custom("c", func(ctx context.Context, data map[string]any) map[string]string {
		if data["x"] == nil {
			return map[string]string{"x": "missing"}
		}
		return nil
	})
	if len(v(context.Background(), map[string]any{})) != 1 {
		t.Error("custom should flag")
	}
}

// --- seed_ctx.go ---

func TestSeedDataFromContext(t *testing.T) {
	// No FS configured -> error.
	if _, err := SeedDataFromContext(context.Background()); err == nil {
		t.Error("missing seed FS should error")
	}
	// nil FS -> context unchanged -> still errors.
	if ctx := WithSeedDataContext(context.Background(), nil, "x"); ctx != context.Background() {
		t.Error("nil fs should return original ctx")
	}
	// Configured -> reads.
	fsys := fstest.MapFS{"data/seed.json": {Data: []byte(`[{"id":1}]`)}}
	ctx := WithSeedDataContext(context.Background(), fsys, "data/seed.json")
	got, err := SeedDataFromContext(ctx)
	if err != nil || string(got) != `[{"id":1}]` {
		t.Fatalf("seed read: %v %q", err, got)
	}
}
