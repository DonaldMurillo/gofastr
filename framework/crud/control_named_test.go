package crud

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func controlNamedEntity(search []string) *entity.Entity {
	return entity.Define("transfers", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "direction", Type: schema.String},
			{Name: "q", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		SearchFields: search,
	})
}

func TestMountLogsControlNamedFields(t *testing.T) {
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) }()

	RegisterCrudRoutes(router.New(), NewCrudHandler(controlNamedEntity(nil), nil), "/transfers")
	got := buf.String()
	if !strings.Contains(got, "transfers.direction shares its name with the direction list control") || !strings.Contains(got, "?direction[eq]=") {
		t.Errorf("log = %q, want the direction line", got)
	}
	if strings.Contains(got, "transfers.q ") || strings.Contains(got, "title") {
		t.Errorf("log = %q: q without search and title filter by their bare keys", got)
	}

	buf.Reset()
	RegisterCrudRoutes(router.New(), NewCrudHandler(controlNamedEntity([]string{"title"}), nil), "/transfers")
	if !strings.Contains(buf.String(), "transfers.q shares its name with the q list control") {
		t.Errorf("log = %q, want the q line on a searching entity", buf.String())
	}
}

func TestLLMMDNotesControlNamedField(t *testing.T) {
	md := EntityLLMMD(controlNamedEntity(nil))
	if !strings.Contains(md, "filter with `direction[eq]=`: a bare `direction=` is the list control") {
		t.Errorf("llm.md has no note on direction:\n%s", md)
	}
	if strings.Contains(md, "`q[eq]=`: a bare") {
		t.Errorf("llm.md notes q on an entity that does not search")
	}
}

func TestListToolNotesControlNamedField(t *testing.T) {
	props := listToolSchema(controlNamedEntity(nil), nil)["properties"].(map[string]any)
	ps, _ := props["direction[eq]"].(map[string]any)
	if desc, _ := ps["description"].(string); !strings.Contains(desc, "a bare direction is the list control") {
		t.Errorf("direction[eq] = %v, want the reason for the brackets", ps)
	}
}
