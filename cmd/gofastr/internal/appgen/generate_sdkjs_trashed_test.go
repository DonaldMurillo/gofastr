package appgen

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// client.d.ts typed ListParams.trashed as a string documented "only" |
// "with". The server reads ?trashed=true and nothing else, so either
// documented value listed the live rows alone, with no error to say the
// parameter was ignored. The type is the one value the server reads:
// encodeParams sends String(true), "true".
func TestDTSTrashedIsTheValueTheServerReads(t *testing.T) {
	decl := framework.EntityDeclaration{Name: "posts", Table: "posts",
		Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}
	spec := sdkSpec{Decls: []framework.EntityDeclaration{decl}, Entities: []cliEntity{buildEntityModel(decl, nil)}}
	var dts string
	for _, f := range renderSDKJSFiles(spec) {
		if f.Name == "client.d.ts" {
			dts = f.Content
		}
	}
	m := regexp.MustCompile(`(?s)/\*\*((?:[^*]|\*[^/])*)\*/\s*trashed\?: ([^;]+);`).FindStringSubmatch(dts)
	if m == nil {
		t.Fatal("client.d.ts declares no documented ListParams.trashed")
	}
	if m[2] != "boolean" {
		t.Errorf("trashed is typed %q; the server reads only trashed=true, so the type is boolean", m[2])
	}
	for _, ghost := range []string{`"only"`, `"with"`} {
		if strings.Contains(m[1], ghost) {
			t.Errorf("the trashed comment names %s, a value the server ignores", ghost)
		}
	}
}
