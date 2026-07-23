package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

const meridianDir = "../../examples/meridian"
const meridianYML = "../../examples/meridian/gofastr.yml"

// ensureMeridianEnv materializes the gitignored .env the generator emits
// alongside the meridian app. A fresh checkout doesn't have it — the seed
// password lives ONLY there, by design — so packing the committed tree
// cannot recover the blueprint's secrets without it. Leaves an existing
// .env (a developer's own generate output) untouched.
func ensureMeridianEnv(t *testing.T, bp Blueprint) {
	t.Helper()
	path := filepath.Join(meridianDir, ".env")
	if _, err := os.Stat(path); err == nil {
		return
	}
	env := renderBlueprintEnv(bp)
	if env == "" {
		return
	}
	if err := os.WriteFile(path, []byte(env), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
}

// TestPack_ReadEntities recovers the entity declarations from the generated
// meridian app's register.go and asserts they equal the parsed YAML's entities
// (order, fields, types, access, indices, relations, properties).
func TestPack_ReadEntities(t *testing.T) {
	a, err := decodeBlueprintFile(meridianYML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := packReadEntities(meridianDir)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	if !reflect.DeepEqual(a.Entities, got) {
		t.Errorf("entities mismatch:\n%s", firstBlueprintDiff(a.Entities, got))
	}
}

// firstBlueprintDiff walks two values and returns a description of the first
// path where they differ — far more useful than a giant DeepEqual dump.
func firstBlueprintDiff(a, b any) string {
	if d := diffValue(reflect.ValueOf(a), reflect.ValueOf(b), "blueprint"); d != "" {
		return d
	}
	return "(values are deep-equal)"
}

func diffValue(a, b reflect.Value, path string) string {
	if !a.IsValid() || !b.IsValid() {
		if a.IsValid() != b.IsValid() {
			return fmt.Sprintf("%s: validity differs", path)
		}
		return ""
	}
	if a.Type() != b.Type() {
		return fmt.Sprintf("%s: type %v != %v", path, a.Type(), b.Type())
	}
	switch a.Kind() {
	case reflect.Ptr, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return fmt.Sprintf("%s: nil-ness differs (%v vs %v)", path, a.IsNil(), b.IsNil())
			}
			return ""
		}
		return diffValue(a.Elem(), b.Elem(), path)
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if d := diffValue(a.Field(i), b.Field(i), path+"."+a.Type().Field(i).Name); d != "" {
				return d
			}
		}
	case reflect.Slice, reflect.Array:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: len %d != %d", path, a.Len(), b.Len())
		}
		for i := 0; i < a.Len(); i++ {
			if d := diffValue(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
	case reflect.Map:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: map len %d != %d (keys %v vs %v)", path, a.Len(), b.Len(), a.MapKeys(), b.MapKeys())
		}
		for _, k := range a.MapKeys() {
			bv := b.MapIndex(k)
			if !bv.IsValid() {
				return fmt.Sprintf("%s: key %v missing in second", path, k)
			}
			if d := diffValue(a.MapIndex(k), bv, fmt.Sprintf("%s[%v]", path, k)); d != "" {
				return d
			}
		}
	default:
		if !reflect.DeepEqual(a.Interface(), b.Interface()) {
			return fmt.Sprintf("%s: %#v != %#v", path, a.Interface(), b.Interface())
		}
	}
	return ""
}

// TestPack_SerializerRoundTrip is the core invariant: serializing a parsed
// blueprint and re-parsing it yields an identical Blueprint. It runs against the
// real Meridian flagship, so it exercises the full breadth of constructs (app +
// auth/admin/theme/dark, 5 entities with access/indices/relations, the marketing
// + app + auth screens with hero/section/card/stat_card+source/charts/entity
// blocks, nav, seed). When a new blueprint feature is added, the decoder AND
// encodeBlueprintYAML must both learn it or this fails.
func TestPack_SerializerRoundTrip(t *testing.T) {
	a, err := decodeBlueprintFile("../../examples/meridian/gofastr.yml")
	if err != nil {
		t.Fatalf("parse meridian.yml: %v", err)
	}
	yml := encodeBlueprintYAML(a)
	b, err := decodeBlueprintString(yml)
	if err != nil {
		t.Fatalf("re-parse serialized yaml: %v\n--- yaml ---\n%s", err, yml)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("round-trip mismatch.\n%s\n--- serialized yaml ---\n%s", firstBlueprintDiff(a, b), yml)
	}
}

func TestPack_ReadSeedAndNav(t *testing.T) {
	a, err := decodeBlueprintFile(meridianYML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	seed, err := packReadSeed(meridianDir)
	if err != nil {
		t.Fatalf("packReadSeed: %v", err)
	}
	if !reflect.DeepEqual(a.Seed, seed) {
		t.Errorf("seed mismatch:\n%s", firstBlueprintDiff(a.Seed, seed))
	}
	nav, err := packReadNav(meridianDir)
	if err != nil {
		t.Fatalf("packReadNav: %v", err)
	}
	if !reflect.DeepEqual(a.Nav, nav) {
		t.Errorf("nav mismatch:\n%s", firstBlueprintDiff(a.Nav, nav))
	}
}

func TestPack_ReadApp(t *testing.T) {
	a, err := decodeBlueprintFile(meridianYML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ensureMeridianEnv(t, a)
	app, err := packReadApp(meridianDir)
	if err != nil {
		t.Fatalf("packReadApp: %v", err)
	}
	if !reflect.DeepEqual(a.App, app) {
		t.Errorf("app mismatch:\n%s", firstBlueprintDiff(a.App, app))
	}
}

func TestPack_ReadScreens(t *testing.T) {
	a, _ := decodeBlueprintFile(meridianYML)
	got, err := packReadScreens(meridianDir)
	if err != nil {
		t.Fatalf("packReadScreens: %v", err)
	}
	if !reflect.DeepEqual(a.Screens, got) {
		t.Errorf("screens mismatch:\n%s", firstBlueprintDiff(a.Screens, got))
	}
}

// TestPack_MeridianRoundTrip is the acceptance gate: packing the generated
// Meridian app reconstructs a Blueprint equal to the one parsed from the
// authored gofastr.yml. As features are added, this catches generator/pack
// divergence.
func TestPack_MeridianRoundTrip(t *testing.T) {
	a, err := decodeBlueprintFile(meridianYML)
	if err != nil {
		t.Fatalf("parse meridian.yml: %v", err)
	}
	ensureMeridianEnv(t, a)
	b, err := packBlueprint(meridianDir)
	if err != nil {
		t.Fatalf("packBlueprint: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("round-trip mismatch:\n%s", firstBlueprintDiff(a, b))
	}
}

// TestPackSelfReferentialHelperNoHang verifies the hop-depth bound in
// reverseEntityResource: a self-referential or mutually-recursive zero-arg
// helper must break the walk, not loop forever.
func TestPackSelfReferentialHelperNoHang(t *testing.T) {
	src := `package screens
func a() interface{} { return a() }
func b() interface{} { return c() }
func c() interface{} { return b() }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "screens.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	helpers := packHelperReturns(f)

	// Construct a().List(ctx) — the call shape reverseEntityResource walks.
	selfRef := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   &ast.CallExpr{Fun: &ast.Ident{Name: "a"}},
			Sel: &ast.Ident{Name: "List"},
		},
		Args: []ast.Expr{&ast.Ident{Name: "ctx"}},
	}
	// Mutually recursive: b() → c() → b() → …
	mutual := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   &ast.CallExpr{Fun: &ast.Ident{Name: "b"}},
			Sel: &ast.Ident{Name: "List"},
		},
		Args: []ast.Expr{&ast.Ident{Name: "ctx"}},
	}

	for name, call := range map[string]*ast.CallExpr{"self-ref": selfRef, "mutual": mutual} {
		done := make(chan struct{})
		go func(call *ast.CallExpr) {
			defer close(done)
			if _, ok := reverseBlock(call, helpers); ok {
				t.Errorf("%s: expected not-reversible", name)
			}
		}(call)
		select {
		case <-done:
			// returned — bound works
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: reverseBlock hung (hop-depth bound missing)", name)
		}
	}
}

// materializeBlueprint renders bp to a temp dir and returns the dir — the
// on-disk shape gofastr pack reads back.
func materializeBlueprint(t *testing.T, bp Blueprint) string {
	t.Helper()
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	dir := t.TempDir()
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestPack_TimestampsEntityRoundTrips guards that an entity declared with
// timestamps: true survives generate→pack. The generator emits the config as
// EntityConfig{...}.WithTimestamps(true), so pack must see through the method
// wrapper to recover the fields — otherwise the whole entity config is lost.
func TestPack_TimestampsEntityRoundTrips(t *testing.T) {
	ts := true
	bp := Blueprint{
		App: BlueprintApp{Name: "TS", Module: "example.com/ts", DBDriver: "sqlite", DBURL: "file:x.db"},
		Entities: []framework.EntityDeclaration{{
			Name:       "posts",
			Timestamps: &ts,
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string", Required: true},
			},
		}},
	}
	dir := materializeBlueprint(t, bp)
	got, err := packReadEntities(dir)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("recovered %d entities, want 1", len(got))
	}
	if got[0].Timestamps == nil || !*got[0].Timestamps {
		t.Errorf("timestamps lost: %+v", got[0].Timestamps)
	}
	if len(got[0].Fields) != 1 || got[0].Fields[0].Name != "title" {
		t.Fatalf("fields lost through the WithTimestamps wrapper: %+v", got[0].Fields)
	}
}

func TestPack_GroupedEntityConfigsRoundTrip(t *testing.T) {
	crud := false
	bp := Blueprint{
		App: BlueprintApp{Name: "Grouped", Module: "example.com/grouped", DBDriver: "sqlite", DBURL: "file:x.db"},
		Entities: []framework.EntityDeclaration{{
			Name: "notes", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
			Scope:      &framework.ScopeDeclaration{OwnerField: "user_id", SoftDelete: true},
			Pagination: &framework.PaginationDeclaration{CursorFields: []string{"created_at", "id"}, MaxListLimit: 50},
			Exposure:   &framework.ExposureDeclaration{CRUD: &crud, MCP: true, Access: &framework.AccessDeclaration{Read: "notes:read"}},
		}},
	}
	dir := materializeBlueprint(t, bp)
	got, err := packReadEntities(dir)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	if len(got) != 1 || got[0].Scope == nil || got[0].Pagination == nil || got[0].Exposure == nil {
		t.Fatalf("grouped configs lost: %#v", got)
	}
	if got[0].Scope.OwnerField != "user_id" || !got[0].Scope.SoftDelete || got[0].Pagination.MaxListLimit != 50 {
		t.Fatalf("grouped values changed: %#v", got[0])
	}
	if got[0].Exposure.CRUD == nil || *got[0].Exposure.CRUD || !got[0].Exposure.MCP || got[0].Exposure.Access.Read != "notes:read" {
		t.Fatalf("grouped exposure changed: %#v", got[0].Exposure)
	}
	m := entityToMap(got[0])
	if _, ok := m["scope"]; !ok {
		t.Fatalf("packed YAML map flattened scope: %#v", m)
	}
	if _, ok := m["pagination"]; !ok {
		t.Fatalf("packed YAML map flattened pagination: %#v", m)
	}
	if _, ok := m["exposure"]; !ok {
		t.Fatalf("packed YAML map flattened exposure: %#v", m)
	}
}

// TestPack_PublicEntityRoundTrips guards that an entity declared with
// public: true survives generate→pack. The generator emits EntityConfig
// {Public: true}; pack must read it back AND re-emit public: true into the
// YAML, or a generated public app silently flips to session-gated on
// round-trip (issue: Public declaration dropped by pack).
func TestPack_PublicEntityRoundTrips(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "Pub", Module: "example.com/pub", DBDriver: "sqlite", DBURL: "file:x.db"},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Public: true,
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}},
		}},
	}
	dir := materializeBlueprint(t, bp)
	got, err := packReadEntities(dir)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("recovered %d entities, want 1", len(got))
	}
	if !got[0].Public {
		t.Errorf("public flag lost on read-back: Public=%v", got[0].Public)
	}
	// The emit side: entityToMap must produce public: true so the serialized
	// YAML carries the declaration. putBool only writes when true, matching
	// how soft_delete/multi_tenant are emitted.
	m := entityToMap(got[0])
	if v, ok := m["public"]; !ok || v != true {
		t.Errorf("entityToMap did not emit public: true, got %#v", m["public"])
	}
}

// TestPack_ScreenlessAppRoundTrips guards that packing a valid generated app
// with entities but no screens does not error on a missing screens.go.
func TestPack_ScreenlessAppRoundTrips(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "NoScreens", Module: "example.com/noscreens", DBDriver: "sqlite", DBURL: "file:x.db"},
		Entities: []framework.EntityDeclaration{{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}},
		}},
	}
	dir := materializeBlueprint(t, bp)
	packed, err := packBlueprint(dir)
	if err != nil {
		t.Fatalf("packBlueprint on a screenless app errored: %v", err)
	}
	if len(packed.Screens) != 0 {
		t.Errorf("recovered %d screens, want 0", len(packed.Screens))
	}
	if len(packed.Entities) != 1 {
		t.Errorf("recovered %d entities, want 1", len(packed.Entities))
	}
}
