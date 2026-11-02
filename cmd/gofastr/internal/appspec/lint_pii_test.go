package appspec

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
)

// CLAUDE.md hard rule #6: an entity holding per-user (PII-shaped) data must
// not be exposed via auto-CRUD/MCP without owner_field, access, or
// multi_tenant. App spec auth alone does NOT suppress the rule: enabling
// auth only mounts pass-through SessionMiddleware, so anonymous requests
// still reach auto-CRUD/MCP.

func piiEntity(name string, fields ...string) framework.EntityDeclaration {
	decl := framework.EntityDeclaration{
		Name:     name,
		Scope:    &framework.ScopeDeclaration{},
		Exposure: &framework.ExposureDeclaration{},
	}
	for _, f := range fields {
		decl.Fields = append(decl.Fields, framework.FieldDeclaration{Name: f, Type: "string"})
	}
	return decl
}

func TestPIIUnscopedFlagged(t *testing.T) {
	bp := AppSpec{Entities: []framework.EntityDeclaration{
		piiEntity("patients", "name", "email", "ssn"),
	}}
	got := LintUnscopedPII(bp)
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
	decl.Scope.OwnerField = "user_id"
	bp := AppSpec{Entities: []framework.EntityDeclaration{decl}}
	if got := LintUnscopedPII(bp); len(got) != 0 {
		t.Fatalf("owner_field set, want no findings: %+v", got)
	}
}

func TestPIIMultiTenantPasses(t *testing.T) {
	decl := piiEntity("patients", "email")
	decl.Scope.MultiTenant = true
	bp := AppSpec{Entities: []framework.EntityDeclaration{decl}}
	if got := LintUnscopedPII(bp); len(got) != 0 {
		t.Fatalf("multi_tenant set, want no findings: %+v", got)
	}
}

func TestPIIAccessPasses(t *testing.T) {
	decl := piiEntity("patients", "email")
	decl.Exposure.Access = &fwentity.AccessDeclaration{Read: "patients:read"}
	bp := AppSpec{Entities: []framework.EntityDeclaration{decl}}
	if got := LintUnscopedPII(bp); len(got) != 0 {
		t.Fatalf("access set, want no findings: %+v", got)
	}
}

// An access: map whose entries are all blank gates nothing; it must not
// count as a remedy.
func TestPIIEmptyAccessStillFlagged(t *testing.T) {
	decl := piiEntity("patients", "email")
	decl.Exposure.Access = &fwentity.AccessDeclaration{}
	bp := AppSpec{Entities: []framework.EntityDeclaration{decl}}
	if got := LintUnscopedPII(bp); len(got) != 1 {
		t.Fatalf("blank access map should not suppress: %+v", got)
	}
}

// anyone opens an operation to every caller, so an access: map holding
// only anyone (the contact-form shape: create: anyone, read blank) gates
// nothing and anonymous callers list every email.
func TestPIIAnyoneAccessStillFlagged(t *testing.T) {
	decl := piiEntity("subscribers", "email")
	decl.Exposure.Access = &fwentity.AccessDeclaration{Create: fwentity.Anyone}
	bp := AppSpec{Entities: []framework.EntityDeclaration{decl}}
	if got := LintUnscopedPII(bp); len(got) != 1 {
		t.Fatalf("an anyone-only access map should not suppress: %+v", got)
	}
}

// The examples/blog case: auth enabled, users entity with email, no
// scoping. Auth only mounts pass-through SessionMiddleware: anonymous
// requests still reach auto-CRUD/MCP, so it must NOT suppress the rule.
func TestPIIAuthAloneStillFlagged(t *testing.T) {
	bp := AppSpec{
		App:      AppSpecApp{Auth: AppSpecAuth{Enabled: true}},
		Entities: []framework.EntityDeclaration{piiEntity("users", "email")},
	}
	got := LintUnscopedPII(bp)
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
	decl.Exposure.CRUD = &off
	bp := AppSpec{Entities: []framework.EntityDeclaration{decl}}
	if got := LintUnscopedPII(bp); len(got) != 0 {
		t.Fatalf("crud off, want no findings: %+v", got)
	}
}

// crud: false but mcp: true still exposes the entity through MCP tools.
func TestPIIMcpOnlyFlagged(t *testing.T) {
	decl := piiEntity("patients", "email")
	off := false
	decl.Exposure.CRUD = &off
	decl.Exposure.MCP = true
	bp := AppSpec{Entities: []framework.EntityDeclaration{decl}}
	if got := LintUnscopedPII(bp); len(got) != 1 {
		t.Fatalf("mcp-only entity should be flagged: %+v", got)
	}
}

// Column naming preserves case, so camelCase PII names must match too.
func TestPIICamelCaseFieldFlagged(t *testing.T) {
	bp := AppSpec{Entities: []framework.EntityDeclaration{
		piiEntity("users", "userEmail", "creditCard"),
	}}
	got := LintUnscopedPII(bp)
	if len(got) != 1 || len(got[0].Fields) != 2 {
		t.Fatalf("want both camelCase fields flagged: %+v", got)
	}
}

// FK columns typed `relation` reference PII, they don't hold it; the
// target entity gets flagged instead.
func TestPIIRelationFieldNotFlagged(t *testing.T) {
	decl := framework.EntityDeclaration{Name: "orders", Fields: []framework.FieldDeclaration{
		{Name: "address_id", Type: "relation", To: "addresses"},
		{Name: "total", Type: "float"},
	}}
	bp := AppSpec{Entities: []framework.EntityDeclaration{decl}}
	if got := LintUnscopedPII(bp); len(got) != 0 {
		t.Fatalf("relation FK should not be flagged: %+v", got)
	}
}

func TestPIINoPIIFieldsPasses(t *testing.T) {
	bp := AppSpec{Entities: []framework.EntityDeclaration{
		piiEntity("posts", "title", "body"),
	}}
	if got := LintUnscopedPII(bp); len(got) != 0 {
		t.Fatalf("no PII fields, want no findings: %+v", got)
	}
}
