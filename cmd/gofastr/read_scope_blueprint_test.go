package main

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// readScopeYAML wraps an entity fragment into a loadable blueprint. The
// fragment is spliced under `entities:` so every test decodes through the
// real loadBlueprint path (file load, rejectUnknownKeys, field checks).
func readScopeYAML(entityBody string) string {
	return "app:\n  name: RS\n  module: example.com/rs\nentities:\n  - name: posts\n" + entityBody
}

func writeReadScopeBlueprint(t *testing.T, yml string) Blueprint {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, yml)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	return bp
}

func readScopeFields() string {
	return "    fields:\n      - name: title\n        type: string\n      - name: status\n        type: enum\n        values: [draft, published]\n        default: draft\n      - name: approved\n        type: bool\n        default: false\n"
}

func TestBlueprintDecodesReadScope(t *testing.T) {
	bp := writeReadScopeBlueprint(t, readScopeYAML(
		"    crud: true\n    read_scope:\n      filter:\n        - field: status\n          op: eq\n          value: published\n"+readScopeFields()))
	want := &entity.ReadScopeDeclaration{
		Filter: []entity.RowPredicateDeclaration{
			{Field: "status", Op: "eq", Value: "published"},
		},
	}
	got := bp.Entities[0].Exposure.ReadScope
	if got == nil || !reflect.DeepEqual(*got, *want) {
		t.Fatalf("ReadScope = %#v, want %#v", got, want)
	}
	// The declaration must convert into the config the CRUD layer reads.
	cfg, err := bp.Entities[0].Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	rs := cfg.Exposure.ReadScope
	if rs == nil || len(rs.Filter) != 1 ||
		rs.Filter[0].Field != "status" || rs.Filter[0].Op != "eq" || rs.Filter[0].Value != "published" {
		t.Fatalf("Config ReadScope = %#v", rs)
	}
}

// The grouped spelling must land in the same place as the flat one.
func TestBlueprintDecodesGroupedReadScope(t *testing.T) {
	bp := writeReadScopeBlueprint(t, readScopeYAML(
		"    exposure:\n      crud: true\n      read_scope:\n        unrestricted: posts:review\n        filter:\n          - field: status\n            value: published\n          - field: approved\n            op: eq\n            value: \"true\"\n"+readScopeFields()))
	want := &entity.ReadScopeDeclaration{
		Unrestricted: "posts:review",
		Filter: []entity.RowPredicateDeclaration{
			{Field: "status", Value: "published"}, // empty op = eq
			{Field: "approved", Op: "eq", Value: "true"},
		},
	}
	got := bp.Entities[0].Exposure.ReadScope
	if got == nil || !reflect.DeepEqual(*got, *want) {
		t.Fatalf("ReadScope = %#v, want %#v", got, want)
	}
}

// A declaration without read_scope must produce a nil ReadScope, exactly as
// before the key existed.
func TestBlueprintReadScopeDefaultNil(t *testing.T) {
	bp := writeReadScopeBlueprint(t, readScopeYAML("    crud: true\n"+readScopeFields()))
	if rs := bp.Entities[0].Exposure.ReadScope; rs != nil {
		t.Fatalf("ReadScope = %#v, want nil", rs)
	}
	cfg, err := bp.Entities[0].Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.Exposure.ReadScope != nil {
		t.Fatalf("Config ReadScope = %#v, want nil", cfg.Exposure.ReadScope)
	}
}

func TestBlueprintRejectsBadReadScopeKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, readScopeYAML(
		"    read_scope:\n      filters:\n        - field: status\n"+readScopeFields()))
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), `unknown key "filters"`) || !strings.Contains(err.Error(), "entities[0].read_scope") {
		t.Fatalf("err = %v, want unknown key naming entities[0].read_scope", err)
	}
}

func TestBlueprintRejectsBadReadScopePredicateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, readScopeYAML(
		"    read_scope:\n      filter:\n        - field: status\n          valeu: published\n"+readScopeFields()))
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), `unknown key "valeu"`) || !strings.Contains(err.Error(), "entities[0].read_scope.filter[0]") {
		t.Fatalf("err = %v, want unknown key naming entities[0].read_scope.filter[0]", err)
	}
}

func TestBlueprintRejectsUnknownReadScopeOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, readScopeYAML(
		"    read_scope:\n      filter:\n        - field: status\n          op: equals\n          value: published\n"+readScopeFields()))
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), "op is \"equals\"") {
		t.Fatalf("err = %v, want unknown op refused", err)
	}
}

func TestBlueprintRejectsReadScopeValueWithValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, readScopeYAML(
		"    read_scope:\n      filter:\n        - field: status\n          op: in\n          value: published\n          values: [draft, published]\n"+readScopeFields()))
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v, want value+values refused", err)
	}
}

func TestBlueprintRejectsReadScopePredicateWithoutField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, readScopeYAML(
		"    read_scope:\n      filter:\n        - op: eq\n          value: published\n"+readScopeFields()))
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), "field is required") {
		t.Fatalf("err = %v, want missing field refused", err)
	}
}

func TestBlueprintRejectsReadScopeInWithoutValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, readScopeYAML(
		"    read_scope:\n      filter:\n        - field: status\n          op: in\n"+readScopeFields()))
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), "non-empty values") {
		t.Fatalf("err = %v, want in without values refused", err)
	}
}

func TestBlueprintRejectsReadScopeEqWithValueList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, readScopeYAML(
		"    read_scope:\n      filter:\n        - field: status\n          op: eq\n          values: [draft, published]\n"+readScopeFields()))
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), "must not set values") {
		t.Fatalf("err = %v, want eq with values refused", err)
	}
}

// An empty read_scope block filters nothing; accepting it would let a typo'd
// indentation pass validation and change no behaviour while looking guarded.
func TestBlueprintRejectsEmptyReadScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, readScopeYAML(
		"    read_scope:\n      unrestricted: \"\"\n"+readScopeFields()))
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), "filters nothing") {
		t.Fatalf("err = %v, want empty read_scope refused", err)
	}
}

// The framework panics at registration for these; the blueprint must fail at
// decode instead, naming the entity and column.
func TestBlueprintRejectsUndeclaredReadScopeField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, readScopeYAML(
		"    read_scope:\n      filter:\n        - field: statuz\n          value: published\n"+readScopeFields()))
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), `entity "posts" read_scope field "statuz" is not a declared field`) {
		t.Fatalf("err = %v, want undeclared field refused", err)
	}
}

func TestBlueprintRejectsHiddenReadScopeField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, readScopeYAML(
		"    read_scope:\n      filter:\n        - field: status\n          value: published\n"+
			"    fields:\n      - name: title\n        type: string\n      - name: status\n        type: enum\n        values: [draft, published]\n        hidden: true\n"))
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), `read_scope field "status" is Hidden`) {
		t.Fatalf("err = %v, want Hidden field refused", err)
	}
}

// Flat and grouped spellings with different values are a hard error for every
// other exposure key; read_scope must not be the silent-precedence exception.
func TestBlueprintRejectsConflictingReadScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, readScopeYAML(
		"    read_scope:\n      filter:\n        - field: approved\n          value: \"true\"\n"+
			"    exposure:\n      read_scope:\n        filter:\n          - field: status\n            value: published\n"+readScopeFields()))
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), "conflicting values for read_scope and exposure.read_scope") {
		t.Fatalf("err = %v, want flat/grouped conflict refused", err)
	}
}

// Identical flat and grouped spellings are accepted, like every other key.
func TestBlueprintAcceptsMatchingReadScope(t *testing.T) {
	bp := writeReadScopeBlueprint(t, readScopeYAML(
		"    read_scope:\n      filter:\n        - field: status\n          value: published\n"+
			"    exposure:\n      read_scope:\n        filter:\n          - field: status\n            value: published\n"+readScopeFields()))
	want := &entity.ReadScopeDeclaration{Filter: []entity.RowPredicateDeclaration{{Field: "status", Value: "published"}}}
	if got := bp.Entities[0].Exposure.ReadScope; got == nil || !reflect.DeepEqual(*got, *want) {
		t.Fatalf("ReadScope = %#v, want %#v", got, want)
	}
}

// readScopeRoundTripYAML exercises every emission shape: unrestricted set and
// unset, the four ops, single values, value lists, and bools.
const readScopeRoundTripYAML = `app:
  name: RSRT
  module: example.com/rsrt
entities:
  - name: posts
    crud: true
    read_scope:
      unrestricted: posts:review
      filter:
        - field: status
          op: eq
          value: published
        - field: category
          op: neq
          value: internal
        - field: visibility
          op: in
          values: [public, unlisted]
        - field: approved
          op: not_in
          values: ["false", "pending"]
    fields:
      - name: title
        type: string
      - name: status
        type: enum
        values: [draft, published]
      - name: category
        type: string
      - name: visibility
        type: string
      - name: approved
        type: bool
  - name: comments
    crud: true
    read_scope:
      filter:
        - field: approved
          value: "true"
    fields:
      - name: body
        type: text
      - name: approved
        type: bool
`

// TestPack_ReadScopeSerializerRoundTrip: a parsed blueprint carrying
// read_scope must survive encode -> decode unchanged, or a pack operation
// silently drops the security posture.
func TestPack_ReadScopeSerializerRoundTrip(t *testing.T) {
	a, err := decodeBlueprintString(readScopeRoundTripYAML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	yml, err := encodeBlueprintYAML(a)
	if err != nil {
		t.Fatalf("serialize blueprint: %v", err)
	}
	b, err := decodeBlueprintString(yml)
	if err != nil {
		t.Fatalf("re-parse serialized yaml: %v\n--- yaml ---\n%s", err, yml)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("round-trip mismatch.\n%s\n--- serialized yaml ---\n%s", firstBlueprintDiff(a, b), yml)
	}
}

// TestPack_ReadScopeRoundTripsThroughGo: the generate leg (renderEntityRegistration
// -> Go literal) and the pack leg (packEntityDeclFromCall -> declaration) must
// both carry read_scope, or `gofastr pack` drops the posture even though the
// serializer round-trip passes.
func TestPack_ReadScopeRoundTripsThroughGo(t *testing.T) {
	a, err := decodeBlueprintString(readScopeRoundTripYAML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dir := materializeBlueprint(t, a)
	got, err := packReadEntities(dir)
	if err != nil {
		t.Fatalf("packReadEntities: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("recovered %d entities, want 2", len(got))
	}
	posts := got[0].Exposure.ReadScope
	if posts == nil {
		t.Fatalf("posts ReadScope lost through generate+pack: %#v", got[0].Exposure)
	}
	if posts.Unrestricted != "posts:review" || len(posts.Filter) != 4 {
		t.Fatalf("posts ReadScope changed: %#v", posts)
	}
	if posts.Filter[2].Op != "in" || !reflect.DeepEqual(posts.Filter[2].Values, []string{"public", "unlisted"}) {
		t.Fatalf("in-predicate changed: %#v", posts.Filter[2])
	}
	if posts.Filter[3].Op != "not_in" || !reflect.DeepEqual(posts.Filter[3].Values, []string{"false", "pending"}) {
		t.Fatalf("not_in-predicate changed: %#v", posts.Filter[3])
	}
	comments := got[1].Exposure.ReadScope
	if comments == nil || len(comments.Filter) != 1 || comments.Filter[0].Value != "true" || comments.Filter[0].Op != "" {
		t.Fatalf("comments ReadScope changed: %#v", comments)
	}
}

// TestRegisterEmitsReadScopeLiteral pins the generated Go: the literal must
// reference framework.ReadScopeConfig so a re-export rename fails here, not
// in every generated app.
func TestRegisterEmitsReadScopeLiteral(t *testing.T) {
	bp, err := decodeBlueprintString(readScopeRoundTripYAML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	files, err := renderGeneratedProject(bp.Entities)
	if err != nil {
		t.Fatalf("renderGeneratedProject: %v", err)
	}
	var entityFile string
	// The registration literal lives in the entity's own file, not register.go.
	for _, f := range files {
		if f.name == "posts.go" {
			entityFile = f.content
		}
	}
	if entityFile == "" {
		t.Fatalf("no posts entity file among %d files", len(files))
	}
	want := `ReadScope: &framework.ReadScopeConfig{Unrestricted: "posts:review", Filter: []framework.RowPredicate{framework.RowPredicate{Field: "status", Op: "eq", Value: "published"}, framework.RowPredicate{Field: "category", Op: "neq", Value: "internal"}, framework.RowPredicate{Field: "visibility", Op: "in", Values: []string{"public", "unlisted"}}, framework.RowPredicate{Field: "approved", Op: "not_in", Values: []string{"false", "pending"}}}`
	if !strings.Contains(entityFile, want) {
		t.Fatalf("posts.go missing ReadScope literal:\n%s\n--- file ---\n%s", want, entityFile)
	}
}

// TestExampleReadScopeBoots is the end-to-end posture proof on a real shipped
// blueprint: an anonymous caller must NOT receive the draft post or the
// unapproved testimonial the portfolio seed writes, and a signed-in caller
// must. It also proves the bool predicate binds: approved eq "true" has to
// match rows stored as boolean true.
func TestExampleReadScopeBoots(t *testing.T) {
	if testing.Short() {
		t.Skip("generates, builds, and boots an app")
	}
	bin, appDir := generateAndCompileBlueprint(t, "../../examples/portfolio/gofastr.yml", "portfolio")
	baseURL, _ := bootGeneratedApp(t, "portfolio", bin, appDir)

	anon := func(table string) []map[string]any {
		t.Helper()
		resp, err := http.Get(baseURL + "/api/" + table)
		if err != nil {
			t.Fatalf("GET /api/%s: %v", table, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/%s = %d, want 200 (a blank read: serves anonymous callers)", table, resp.StatusCode)
		}
		var envelope struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode /api/%s payload: %v", table, err)
		}
		return envelope.Data
	}

	anonPosts := anon("posts")
	for _, row := range anonPosts {
		if row["status"] == "draft" {
			t.Errorf("anonymous /api/posts served the seeded draft post: %#v", row)
		}
	}
	if len(anonPosts) == 0 {
		t.Fatal("anonymous /api/posts served no rows at all: the filter matched nothing, not just unpublished rows")
	}
	anonTestimonials := anon("testimonials")
	for _, row := range anonTestimonials {
		if row["approved"] == false {
			t.Errorf("anonymous /api/testimonials served the unapproved testimonial: %#v", row)
		}
	}
	if len(anonTestimonials) == 0 {
		t.Fatal("anonymous /api/testimonials served no rows at all: the bool predicate matched nothing")
	}

	// Sign in (any session lifts the filter; Unrestricted is blank here).
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	creds := strings.NewReader(`{"email":"readscope@example.com","password":"str0ng-passphrase"}`)
	regResp, err := client.Post(baseURL+"/auth/register", "application/json", creds)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	regResp.Body.Close()
	loginResp, err := client.Post(baseURL+"/auth/login", "application/json",
		strings.NewReader(`{"email":"readscope@example.com","password":"str0ng-passphrase"}`))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d, want 200: the signed-in half of this proof cannot run", loginResp.StatusCode)
	}

	authed := func(table string) []map[string]any {
		t.Helper()
		resp, err := client.Get(baseURL + "/api/" + table)
		if err != nil {
			t.Fatalf("authed GET /api/%s: %v", table, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("authed GET /api/%s = %d, want 200", table, resp.StatusCode)
		}
		var envelope struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode authed /api/%s: %v", table, err)
		}
		return envelope.Data
	}

	var sawDraft bool
	for _, row := range authed("posts") {
		if row["status"] == "draft" {
			sawDraft = true
		}
	}
	if !sawDraft {
		t.Error("signed-in /api/posts did not serve the seeded draft post: an empty Unrestricted should lift the filter for any session")
	}
	var sawUnapproved bool
	for _, row := range authed("testimonials") {
		if row["approved"] == false {
			sawUnapproved = true
		}
	}
	if !sawUnapproved {
		t.Error("signed-in /api/testimonials did not serve the seeded unapproved testimonial")
	}
}

// A YAML shape that is not a scalar must be an error, never a default.
//
// stringValue answers "" for a list or a map, and an empty `unrestricted` is
// the WEAKEST setting there is: it lifts the read scope for every signed-in
// caller instead of only the permission holder. So `unrestricted:
// [posts:review]`, which reads as a perfectly ordinary typo, silently traded a
// permission gate for "anyone with an account". An empty `op` means eq, so the
// same shape on an operator quietly turns neq into eq.
//
// A posture the decoder silently widens is the failure this whole path exists
// to prevent, which is why these are decode errors and not registration ones.
//
// `field` as a nested list is not covered: the YAML parser rejects that
// indentation before the decoder is reached, which is fail-closed for a
// different reason. The decoder guards it anyway.
func TestReadScopeRefusesNonScalarNodes(t *testing.T) {
	cases := map[string]string{
		"unrestricted as a list": `
    read_scope:
      unrestricted:
        - content:review
      filter:
        - field: status
          value: published`,
		"unrestricted as a map": `
    read_scope:
      unrestricted:
        perm: content:review
      filter:
        - field: status
          value: published`,
		"op as a list": `
    read_scope:
      filter:
        - field: status
          op:
            - neq
          value: draft`,
		"value as a list": `
    read_scope:
      filter:
        - field: status
          value:
            - published`,
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			bp := "app:\n  name: t\n  module: example.com/t\nentities:\n  - name: posts\n    crud: true\n    fields:\n      - name: status\n        type: string" + block + "\n"
			dir := t.TempDir()
			path := filepath.Join(dir, "gofastr.yml")
			if err := os.WriteFile(path, []byte(bp), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := decodeBlueprintFile(path)
			if err == nil {
				t.Fatal("a non-scalar node was accepted; it decodes to the empty string, which is the weakest posture")
			}
			if !strings.Contains(err.Error(), "single value") {
				t.Errorf("the error should say the key needs a single value, got: %v", err)
			}
		})
	}
}
