package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An explicit ?stream=true with an include the entity can serve is refused
// as stream_include, not streamed without the include.
func TestExplicitStreamWithResolvableIncludeRefused(t *testing.T) {
	ch, reg := streamPostsHandler(t)
	authors := entity.Define("authors", entity.EntityConfig{
		Name: "authors", Table: "authors",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	authors.SetDB(ch.DB.(*sql.DB))
	reg.ents["authors"] = authors
	req := withTestUser(httptest.NewRequest("GET", "/posts?stream=true&include=author", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "stream_include") {
		t.Fatalf("stream+include: %d %s, want 400 stream_include", rec.Code, rec.Body.String())
	}
}
