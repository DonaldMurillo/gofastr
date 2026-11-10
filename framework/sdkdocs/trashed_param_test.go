package sdkdocs

import (
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/softdelete"
)

// The list notes of a soft-delete entity named ?trashed=only|with. The
// server reads one value, "true", and treats any other as the parameter
// not being there, so a reader who copied the documented spelling got the
// live rows only and no error. The spelling the notes print must be one
// the server's own parser accepts.
func TestListNotesNameATrashedValueTheServerReads(t *testing.T) {
	model := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Scope:  &entity.ScopeConfig{SoftDelete: true},
	})
	out := (&entityScreen{}).listParamsNotes(model.Config).String()
	m := regexp.MustCompile(`\?trashed=([^<\s]+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("the notes of a soft-delete entity do not name ?trashed=:\n%s", out)
	}
	req := httptest.NewRequest("GET", "/api/notes?trashed="+m[1], nil)
	if !softdelete.WithTrashed(req) {
		t.Fatalf("the notes document ?trashed=%s, which the server does not read as a request for trashed rows", m[1])
	}
}
