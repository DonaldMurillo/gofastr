package crud

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// llm.md lists the state field, the initial values and every non-system
// move with its route, and says the field and stamps change only through
func TestLLMMDStatesSectionListsMoves(t *testing.T) {
	md := EntityLLMMD(invoicesEntity(false))

	if !strings.Contains(md, "## States\n") {
		t.Fatal("llm.md for a states entity has no States section")
	}
	for _, want := range []string{
		"`status` starts at one of: `draft`, `open`.",
		"| `issue` | `POST /invoices/{id}/transitions/issue` | `draft` → `open` | — |",
		"| `pay` | `POST /invoices/{id}/transitions/pay` | `open` → `paid` | `paid_on` |",
		"| `void` | `POST /invoices/{id}/transitions/void` | `draft`, `open` → `void` | — |",
		"`status` and every stamp change only through these moves",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("llm.md states section is missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "mark_overdue") {
		t.Error("llm.md mentions the System move; system moves appear nowhere")
	}
}

// Advisory drops the only-through-moves sentence (the field is freely
// writable) but keeps the moves: the routes exist and an agent can call
// them.
func TestLLMMDStatesAdvisoryOmitsRule(t *testing.T) {
	md := EntityLLMMD(invoicesEntity(true))
	if !strings.Contains(md, "POST /invoices/{id}/transitions/pay") {
		t.Error("advisory llm.md lost the pay move's route; the route is mounted")
	}
	if strings.Contains(md, "change only through these moves") {
		t.Error("advisory llm.md states the enforcement sentence; Advisory releases the field")
	}
}

// A read-only mount serves no transition route, so the section stays out
// with the other write sections.
func TestLLMMDStatesOmittedOnReadOnly(t *testing.T) {
	md := EntityLLMMD(invoicesEntity(false), LLMMDOptions{ReadOnly: true})
	if strings.Contains(md, "## States") {
		t.Error("read-only mount documents the states section; its routes are not mounted")
	}
}

// An entity without states keeps today's document: no empty section.
func TestLLMMDNoStatesSectionWithoutStates(t *testing.T) {
	md := EntityLLMMD(entityNoStates())
	if strings.Contains(md, "## States") {
		t.Error("states section emitted for an entity with no states block")
	}
}
func entityNoStates() *entity.Entity {
	return entity.Define("posts", entity.EntityConfig{
		Name:  "posts",
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	})
}
