package appspec

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// readScopeYAML wraps an entity fragment into a loadable app spec. The
// fragment is spliced under `entities:` so every test decodes through the
// real loadAppSpec path (file load, rejectUnknownKeys, field checks).
func readScopeYAML(entityBody string) string {
	return "app:\n  name: RS\n  module: example.com/rs\nentities:\n  - name: posts\n" + entityBody
}

func writeReadScopeAppSpec(t *testing.T, yml string) AppSpec {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	clitest.WriteTestFile(t, path, yml)
	bp, err := LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	return bp
}

// refuseReadScope loads an app spec whose entity body is body and proves
// LoadAppSpec fails with every substring in its error; want names the
// refusal in failure messages.
func refuseReadScope(t *testing.T, body, want string, substrings ...string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	clitest.WriteTestFile(t, path, readScopeYAML(body))
	_, err := LoadAppSpec(path)
	if err == nil {
		t.Fatalf("err = %v, want %s", err, want)
	}
	for _, sub := range substrings {
		if !strings.Contains(err.Error(), sub) {
			t.Fatalf("err = %v, want %s", err, want)
		}
	}
}

func readScopeFields() string {
	return "    fields:\n      - name: title\n        type: string\n      - name: status\n        type: enum\n        values: [draft, published]\n        default: draft\n      - name: approved\n        type: bool\n        default: false\n"
}

func TestAppSpecDecodesReadScope(t *testing.T) {
	bp := writeReadScopeAppSpec(t, readScopeYAML(
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
func TestAppSpecDecodesGroupedReadScope(t *testing.T) {
	bp := writeReadScopeAppSpec(t, readScopeYAML(
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
func TestAppSpecReadScopeDefaultNil(t *testing.T) {
	bp := writeReadScopeAppSpec(t, readScopeYAML("    crud: true\n"+readScopeFields()))
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

func TestAppSpecRejectsBadReadScopeKey(t *testing.T) {
	refuseReadScope(t,
		"    read_scope:\n      filters:\n        - field: status\n"+readScopeFields(),
		"unknown key naming entities[0].read_scope",
		`unknown key "filters"`, "entities[0].read_scope")
}

func TestAppSpecRejectsBadReadScopePredicateKey(t *testing.T) {
	refuseReadScope(t,
		"    read_scope:\n      filter:\n        - field: status\n          valeu: published\n"+readScopeFields(),
		"unknown key naming entities[0].read_scope.filter[0]",
		`unknown key "valeu"`, "entities[0].read_scope.filter[0]")
}

func TestAppSpecRejectsUnknownReadScopeOp(t *testing.T) {
	refuseReadScope(t,
		"    read_scope:\n      filter:\n        - field: status\n          op: equals\n          value: published\n"+readScopeFields(),
		"unknown op refused", "op is \"equals\"")
}

func TestAppSpecRejectsReadScopeValueWithValues(t *testing.T) {
	refuseReadScope(t,
		"    read_scope:\n      filter:\n        - field: status\n          op: in\n          value: published\n          values: [draft, published]\n"+readScopeFields(),
		"value+values refused", "mutually exclusive")
}

func TestAppSpecRejectsReadScopePredicateWithoutField(t *testing.T) {
	refuseReadScope(t,
		"    read_scope:\n      filter:\n        - op: eq\n          value: published\n"+readScopeFields(),
		"missing field refused", "field is required")
}

func TestAppSpecRejectsReadScopeInWithoutValues(t *testing.T) {
	refuseReadScope(t,
		"    read_scope:\n      filter:\n        - field: status\n          op: in\n"+readScopeFields(),
		"in without values refused", "non-empty values")
}

func TestAppSpecRejectsReadScopeEqWithValueList(t *testing.T) {
	refuseReadScope(t,
		"    read_scope:\n      filter:\n        - field: status\n          op: eq\n          values: [draft, published]\n"+readScopeFields(),
		"eq with values refused", "must not set values")
}

// An empty read_scope block filters nothing; accepting it would let a typo'd
// indentation pass validation and change no behaviour while looking guarded.
func TestAppSpecRejectsEmptyReadScope(t *testing.T) {
	refuseReadScope(t,
		"    read_scope:\n      unrestricted: \"\"\n"+readScopeFields(),
		"empty read_scope refused", "filters nothing")
}

// The framework panics at registration for these; the app spec must fail at
// decode instead, naming the entity and column.
func TestAppSpecRejectsUndeclaredReadScopeField(t *testing.T) {
	refuseReadScope(t,
		"    read_scope:\n      filter:\n        - field: statuz\n          value: published\n"+readScopeFields(),
		"undeclared field refused", `entity "posts" read_scope field "statuz" is not a declared field`)
}

func TestAppSpecRejectsHiddenReadScopeField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	clitest.WriteTestFile(t, path, readScopeYAML(
		"    read_scope:\n      filter:\n        - field: status\n          value: published\n"+
			"    fields:\n      - name: title\n        type: string\n      - name: status\n        type: enum\n        values: [draft, published]\n        hidden: true\n"))
	_, err := LoadAppSpec(path)
	if err == nil || !strings.Contains(err.Error(), `read_scope field "status" is Hidden`) {
		t.Fatalf("err = %v, want Hidden field refused", err)
	}
}

// Flat and grouped spellings with different values are a hard error for every
// other exposure key; read_scope must not be the silent-precedence exception.
func TestAppSpecRejectsConflictingReadScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	clitest.WriteTestFile(t, path, readScopeYAML(
		"    read_scope:\n      filter:\n        - field: approved\n          value: \"true\"\n"+
			"    exposure:\n      read_scope:\n        filter:\n          - field: status\n            value: published\n"+readScopeFields()))
	_, err := LoadAppSpec(path)
	if err == nil || !strings.Contains(err.Error(), "conflicting values for read_scope and exposure.read_scope") {
		t.Fatalf("err = %v, want flat/grouped conflict refused", err)
	}
}

// Identical flat and grouped spellings are accepted, like every other key.
func TestAppSpecAcceptsMatchingReadScope(t *testing.T) {
	bp := writeReadScopeAppSpec(t, readScopeYAML(
		"    read_scope:\n      filter:\n        - field: status\n          value: published\n"+
			"    exposure:\n      read_scope:\n        filter:\n          - field: status\n            value: published\n"+readScopeFields()))
	want := &entity.ReadScopeDeclaration{Filter: []entity.RowPredicateDeclaration{{Field: "status", Value: "published"}}}
	if got := bp.Entities[0].Exposure.ReadScope; got == nil || !reflect.DeepEqual(*got, *want) {
		t.Fatalf("ReadScope = %#v, want %#v", got, want)
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
			_, err := DecodeAppSpecFile(path)
			if err == nil {
				t.Fatal("a non-scalar node was accepted; it decodes to the empty string, which is the weakest posture")
			}
			if !strings.Contains(err.Error(), "single value") {
				t.Errorf("the error should say the key needs a single value, got: %v", err)
			}
		})
	}
}
