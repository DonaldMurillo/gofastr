package auditcmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// --- gofastr audit lint: a root gofastr.yml is linted with the Go rules.

func TestAuditLintFlagsAppSpecPII(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "gofastr.yml"), clitest.PiiTripYML)
	got, err := auditLint(dir)
	if err != nil {
		t.Fatalf("auditLint: %v", err)
	}
	mustHaveRule(t, got, "unscoped-pii")
	for _, f := range got {
		if f.Rule == "unscoped-pii" && f.File != "gofastr.yml" {
			t.Fatalf("finding should name the app spec file: %+v", f)
		}
	}
}

func TestAuditLintAppSpecOwnerFieldOk(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "gofastr.yml"), clitest.PiiOwnerYML)
	got, err := auditLint(dir)
	if err != nil {
		t.Fatalf("auditLint: %v", err)
	}
	mustNotHaveRule(t, got, "unscoped-pii")
}

// --- gofastr audit lint: Go-declared entities are linted as well as
// the app spec. A Go `app.Entity("x", framework.EntityConfig{...})` with
// an unscoped PII-shaped field exposed via auto-CRUD must surface the
// SAME rule "unscoped-pii" as the app spec path; it's the same defect.

// goPiiTripGo is a hand-written Go entity declaration: "members" with an
// email field, auto-CRUD on by default (no Exposure.CRUD), no scoping.
const goPiiTripGo = `package entities

func registerMembers() {
	app.Entity("members", framework.EntityConfig{
		Fields: []schema.Field{{Name: "email", Type: schema.String}},
	})
}
`

// goPiiScopedGo is the same entity WITH a Scope OwnerField, the remedy
// the rule prescribes, so it must NOT be flagged.
const goPiiScopedGo = `package entities

func registerMembers() {
	app.Entity("members", framework.EntityConfig{
		Fields: []schema.Field{{Name: "email", Type: schema.String}},
		Scope:  &framework.ScopeConfig{OwnerField: "user_id"},
	})
}
`

func TestAuditLintFlagsGoSourcePII(t *testing.T) {
	// Positive: unscoped PII-shaped field exposed via auto-CRUD → flagged.
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "entities.go"), goPiiTripGo)
	got, err := auditLint(dir)
	if err != nil {
		t.Fatalf("auditLint: %v", err)
	}
	mustHaveRule(t, got, "unscoped-pii")
	for _, f := range got {
		if f.Rule == "unscoped-pii" {
			if f.File != "entities.go" {
				t.Fatalf("finding should name the Go file: %+v", f)
			}
			if f.Line <= 0 {
				t.Fatalf("finding should carry the declaration line: %+v", f)
			}
			if !strings.Contains(f.Message, "members") {
				t.Fatalf("finding should name the entity: %+v", f)
			}
		}
	}

	// Negative: same entity WITH owner_field scoping → NOT flagged.
	scoped := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(scoped, "entities.go"), goPiiScopedGo)
	gotScoped, err := auditLint(scoped)
	if err != nil {
		t.Fatalf("auditLint: %v", err)
	}
	mustNotHaveRule(t, gotScoped, "unscoped-pii")
}

func TestLintGoSourcePII_InvalidOnDeleteDoesNotSuppressPII(t *testing.T) {
	src := `package entities

import (
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/schema"
)

func registerCustomers(app *framework.App) {
	app.Entity("customers", entity.EntityConfig{
		Table: "customers",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "email", Type: schema.String},
		},
		Relations: []entity.Relation{
			{
				Type:     entity.RelManyToOne,
				Name:     "company",
				Entity:   "companies",
				OnDelete: entity.OnDeleteAction("UnsupportedAction"),
			},
		},
	})
}
`
	findings := lintGoSourcePII("entities/customers.go", []byte(src))
	found := false
	for _, f := range findings {
		if f.Rule == "unscoped-pii" {
			found = true
			if !strings.Contains(f.Message, "customers") || !strings.Contains(f.Message, "email") {
				t.Fatalf("unexpected finding message: %s", f.Message)
			}
		}
	}
	if !found {
		t.Fatal("expected unscoped-pii finding despite invalid OnDelete action, got none")
	}
}
