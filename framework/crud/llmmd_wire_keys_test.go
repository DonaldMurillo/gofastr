package crud

import (
	"encoding/json"
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

func TestLLMMDPostExampleEscapesWireKeys(t *testing.T) {
	for _, wire := range []string{`quoted"key`, `back\slash`, "line\nbreak", "tab\tkey", "control\x01key"} {
		t.Run(wire, func(t *testing.T) {
			ent := entity.Define("articles", entity.EntityConfig{
				Fields: []schema.Field{{Name: "body_text", WireName: wire, Type: schema.String}},
			}.WithTimestamps(false))
			document := EntityLLMMD(ent)
			_, example, found := strings.Cut(document, "**Request body:** JSON object with writable fields.\n```json\n")
			if !found {
				t.Fatal("POST request example missing")
			}
			example, _, found = strings.Cut(example, "\n```")
			if !found {
				t.Fatal("POST request example has no closing fence")
			}
			var body map[string]string
			if err := json.Unmarshal([]byte(example), &body); err != nil {
				t.Fatalf("invalid POST JSON %q: %v", example, err)
			}
			if len(body) != 1 || body[wire] != "<value>" {
				t.Fatalf("POST body = %#v; want original wire key %q", body, wire)
			}
		})
	}
}
