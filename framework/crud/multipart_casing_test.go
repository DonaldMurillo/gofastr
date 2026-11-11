package crud

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// uploads.md said multipart field names are taken literally as column
// names, with no JSON-case translation. They resolve through the same
// wire-key fold as JSON: avatarUrl lands on avatar_url, and two names
// for one column in one body are refused.
func TestMultipartKeysFoldLikeJSON(t *testing.T) {
	database := setupDB(t, `CREATE TABLE mp_case (id TEXT PRIMARY KEY, avatar_url TEXT)`)
	def := entity.Define("mp_case", entity.EntityConfig{Name: "mp_case", Table: "mp_case",
		Fields: []schema.Field{{Name: "avatar_url", Type: schema.String}}}.WithTimestamps(false))
	ch := NewCrudHandler(def, database)
	read := func(fields ...string) (map[string]any, error) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for i := 0; i < len(fields); i += 2 {
			if err := mw.WriteField(fields[i], fields[i+1]); err != nil {
				t.Fatal(err)
			}
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/mp_case", &buf)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		body, _, err := ch.readRequestBody(r)
		return body, err
	}
	for _, name := range []string{"avatarUrl", "avatar_url"} {
		body, err := read(name, "x.png")
		if err != nil || body["avatar_url"] != "x.png" {
			t.Errorf("multipart %s = %v, %v; want it on avatar_url", name, body, err)
		}
	}
	if _, err := read("avatarUrl", "a", "avatar_url", "b"); err == nil || !strings.Contains(err.Error(), "both name the field") {
		t.Errorf("two names for one column = %v, want the keys-fold refusal", err)
	}
}
