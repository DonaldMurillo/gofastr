package crud

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestLLMMDUsesMountedWireKeys(t *testing.T) {
	for _, casing := range []JSONCase{CaseCamel, CaseSnake} {
		ent := entity.Define("articles", entity.EntityConfig{Table: "articles", Fields: []schema.Field{
			{Name: "author_name", Type: schema.String},
			{Name: "body_text", WireName: "content", Type: schema.Text},
		}}.WithTimestamps(false))
		crud := NewCrudHandler(ent, nil).WithJSONCase(casing)
		request := withTestUser(httptest.NewRequest("GET", "/articles/llm.md", nil), "user")
		recorder := httptest.NewRecorder()
		LLMMDHandlerFor(crud).ServeHTTP(recorder, request)
		if recorder.Code != 200 {
			t.Fatalf("llm.md = %d", recorder.Code)
		}
		for _, column := range []string{"author_name", "body_text"} {
			key := crud.WireKey(column)
			for _, want := range []string{"| `" + key + "` |", `"` + key + `": "<value>"`} {
				if !strings.Contains(recorder.Body.String(), want) {
					t.Errorf("%s llm.md lacks %q", casing, want)
				}
			}
		}
	}
}
