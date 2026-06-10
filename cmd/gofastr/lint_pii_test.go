package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
)

// CLAUDE.md hard rule #6: an entity holding per-user (PII-shaped) data must
// not be exposed via auto-CRUD/MCP without owner_field, access, or
// multi_tenant. Blueprint auth alone does NOT suppress the rule: enabling
// auth only mounts pass-through SessionMiddleware, so anonymous requests
// still reach auto-CRUD/MCP.

func piiEntity(name string, fields ...string) framework.EntityDeclaration {
	decl := framework.EntityDeclaration{Name: name}
	for _, f := range fields {
		decl.Fields = append(decl.Fields, framework.FieldDeclaration{Name: f, Type: "string"})
	}
	return decl
}

func TestPIIUnscopedFlagged(t *testing.T) {
	bp := Blueprint{Entities: []framework.EntityDeclaration{
		piiEntity("patients", "name", "email", "ssn"),
	}}
	got := lintUnscopedPII(bp)
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(got), got)
	}
	msg := got[0].Message()
	for _, want := range []string{"patients", "email", "ssn", "owner_field", "access", "multi_tenant"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q should mention %q", msg, want)
		}
	}
	if strings.Contains(msg, "auth") {
		t.Fatalf("enabling auth is not a remedy, message must not suggest it: %q", msg)
	}
	if strings.Contains(msg, `"name"`) {
		t.Fatalf("non-PII field listed in %q", msg)
	}
}

func TestPIIOwnerFieldPasses(t *testing.T) {
	decl := piiEntity("patients", "email", "user_id")
	decl.OwnerField = "user_id"
	bp := Blueprint{Entities: []framework.EntityDeclaration{decl}}
	if got := lintUnscopedPII(bp); len(got) != 0 {
		t.Fatalf("owner_field set, want no findings: %+v", got)
	}
}

func TestPIIMultiTenantPasses(t *testing.T) {
	decl := piiEntity("patients", "email")
	decl.MultiTenant = true
	bp := Blueprint{Entities: []framework.EntityDeclaration{decl}}
	if got := lintUnscopedPII(bp); len(got) != 0 {
		t.Fatalf("multi_tenant set, want no findings: %+v", got)
	}
}

func TestPIIAccessPasses(t *testing.T) {
	decl := piiEntity("patients", "email")
	decl.Access = &fwentity.AccessDeclaration{Read: "patients:read"}
	bp := Blueprint{Entities: []framework.EntityDeclaration{decl}}
	if got := lintUnscopedPII(bp); len(got) != 0 {
		t.Fatalf("access set, want no findings: %+v", got)
	}
}

// An access: map whose entries are all blank gates nothing — it must not
// count as a remedy.
func TestPIIEmptyAccessStillFlagged(t *testing.T) {
	decl := piiEntity("patients", "email")
	decl.Access = &fwentity.AccessDeclaration{}
	bp := Blueprint{Entities: []framework.EntityDeclaration{decl}}
	if got := lintUnscopedPII(bp); len(got) != 1 {
		t.Fatalf("blank access map should not suppress: %+v", got)
	}
}

// The examples/blog case: auth enabled, users entity with email, no
// scoping. Auth only mounts pass-through SessionMiddleware — anonymous
// requests still reach auto-CRUD/MCP — so it must NOT suppress the rule.
func TestPIIAuthAloneStillFlagged(t *testing.T) {
	bp := Blueprint{
		App:      BlueprintApp{Auth: BlueprintAuth{Enabled: true}},
		Entities: []framework.EntityDeclaration{piiEntity("users", "email")},
	}
	got := lintUnscopedPII(bp)
	if len(got) != 1 {
		t.Fatalf("auth alone must not suppress, want 1 finding: %+v", got)
	}
	if msg := got[0].Message(); strings.Contains(msg, "auth") {
		t.Fatalf("enabling auth is not a remedy, message must not suggest it: %q", msg)
	}
}

// crud: false + mcp: false = not auto-exposed, nothing to flag.
func TestPIICrudOffPasses(t *testing.T) {
	decl := piiEntity("patients", "email")
	off := false
	decl.CRUD = &off
	bp := Blueprint{Entities: []framework.EntityDeclaration{decl}}
	if got := lintUnscopedPII(bp); len(got) != 0 {
		t.Fatalf("crud off, want no findings: %+v", got)
	}
}

// crud: false but mcp: true still exposes the entity through MCP tools.
func TestPIIMcpOnlyFlagged(t *testing.T) {
	decl := piiEntity("patients", "email")
	off := false
	decl.CRUD = &off
	decl.MCP = true
	bp := Blueprint{Entities: []framework.EntityDeclaration{decl}}
	if got := lintUnscopedPII(bp); len(got) != 1 {
		t.Fatalf("mcp-only entity should be flagged: %+v", got)
	}
}

// Column naming preserves case, so camelCase PII names must match too.
func TestPIICamelCaseFieldFlagged(t *testing.T) {
	bp := Blueprint{Entities: []framework.EntityDeclaration{
		piiEntity("users", "userEmail", "creditCard"),
	}}
	got := lintUnscopedPII(bp)
	if len(got) != 1 || len(got[0].Fields) != 2 {
		t.Fatalf("want both camelCase fields flagged: %+v", got)
	}
}

// FK columns typed `relation` reference PII, they don't hold it — the
// target entity gets flagged instead.
func TestPIIRelationFieldNotFlagged(t *testing.T) {
	decl := framework.EntityDeclaration{Name: "orders", Fields: []framework.FieldDeclaration{
		{Name: "address_id", Type: "relation", To: "addresses"},
		{Name: "total", Type: "float"},
	}}
	bp := Blueprint{Entities: []framework.EntityDeclaration{decl}}
	if got := lintUnscopedPII(bp); len(got) != 0 {
		t.Fatalf("relation FK should not be flagged: %+v", got)
	}
}

func TestPIINoPIIFieldsPasses(t *testing.T) {
	bp := Blueprint{Entities: []framework.EntityDeclaration{
		piiEntity("posts", "title", "body"),
	}}
	if got := lintUnscopedPII(bp); len(got) != 0 {
		t.Fatalf("no PII fields, want no findings: %+v", got)
	}
}

// --- gofastr validate: unscoped PII is an error (exit 1).

const piiTripYML = `
app:
  name: Demo
entities:
  - name: patients
    fields:
      - name: email
        type: string
      - name: ssn
        type: string
`

const piiAuthOnYML = `
app:
  name: Demo
  auth:
    enabled: true
entities:
  - name: users
    fields:
      - name: email
        type: string
`

const piiOwnerYML = `
app:
  name: Demo
entities:
  - name: patients
    owner_field: user_id
    fields:
      - name: email
        type: string
      - name: user_id
        type: string
`

func TestValidatePIIExits1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeValidateFile(t, path, piiTripYML)
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runValidate([]string{path}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	for _, want := range []string{"patients", "email", "ssn", "owner_field", "access", "multi_tenant"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output should contain %q:\n%s", want, out)
		}
	}
}

// The blog repro end to end: auth enabled in the blueprint, users entity
// with email, no scoping — validate must still exit 1.
func TestValidateAuthOnPIIExits1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeValidateFile(t, path, piiAuthOnYML)
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runValidate([]string{path}) })
	})
	if code != 1 {
		t.Fatalf("auth alone must not suppress unscoped PII, want exit 1, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "users") {
		t.Fatalf("output should name the entity:\n%s", out)
	}
}

func TestValidatePIIOwnerFieldOk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeValidateFile(t, path, piiOwnerYML)
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runValidate([]string{path}) })
	})
	if code != -1 {
		t.Fatalf("owner_field set, blueprint should validate, got exit %d\n%s", code, out)
	}
}

// --- gofastr generate: same condition is a prominent warning, not a block.

func TestGeneratePIIWarnsNotFatal(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	writeValidateFile(t, filepath.Join(dir, "bp.yml"), piiTripYML)
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() {
			generateFromBlueprint(generateOptions{from: filepath.Join(dir, "bp.yml"), outputDir: "gen", dryRun: true})
		})
	})
	if code != -1 {
		t.Fatalf("PII warning must not block generate, got exit %d\n%s", code, out)
	}
	for _, want := range []string{"patients", "owner_field", "Would generate"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output should contain %q:\n%s", want, out)
		}
	}
}

// --- gofastr audit lint: a root gofastr.yml is linted with the Go rules.

func TestAuditLintFlagsBlueprintPII(t *testing.T) {
	dir := t.TempDir()
	writeValidateFile(t, filepath.Join(dir, "gofastr.yml"), piiTripYML)
	got, err := auditLint(dir)
	if err != nil {
		t.Fatalf("auditLint: %v", err)
	}
	mustHaveRule(t, got, "unscoped-pii")
	for _, f := range got {
		if f.Rule == "unscoped-pii" && f.File != "gofastr.yml" {
			t.Fatalf("finding should name the blueprint file: %+v", f)
		}
	}
}

func TestAuditLintBlueprintOwnerFieldOk(t *testing.T) {
	dir := t.TempDir()
	writeValidateFile(t, filepath.Join(dir, "gofastr.yml"), piiOwnerYML)
	got, err := auditLint(dir)
	if err != nil {
		t.Fatalf("auditLint: %v", err)
	}
	mustNotHaveRule(t, got, "unscoped-pii")
}
