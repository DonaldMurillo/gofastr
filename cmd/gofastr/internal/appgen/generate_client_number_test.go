package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// clientNumberTest reads a json field holding 2^53+1 through every decode
// path of the generated client: a single record, a list, a relation
// entity's own UnmarshalJSON, and a batch result. Each re-marshals the
// value it read; a decode into any made it a float64 and sent back ...992.
const clientNumberTest = `package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const big = "9007199254740993"

func TestClientKeepsBigIntegers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		row := ` + "`" + `{"id":"p1","authorId":"a1","meta":{"n":` + "`" + ` + big + ` + "`" + `}}` + "`" + `
		switch {
		case strings.HasSuffix(r.URL.Path, "/_batch"):
			w.Write([]byte(` + "`" + `{"committed":true,"results":[{"index":0,"data":` + "`" + ` + row + ` + "`" + `}]}` + "`" + `))
		case strings.HasSuffix(r.URL.Path, "/p1"):
			w.Write([]byte(` + "`" + `{"data":` + "`" + ` + row + ` + "`" + `}` + "`" + `))
		default:
			w.Write([]byte(` + "`" + `{"data":[` + "`" + ` + row + ` + "`" + `],"total":1}` + "`" + `))
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL, nil)
	ctx := context.Background()
	check := func(what string, v any) {
		t.Helper()
		out, err := json.Marshal(v)
		if err != nil || !strings.Contains(string(out), big) {
			t.Errorf("%s re-marshals to %s, %v; want %s kept", what, out, err, big)
		}
	}
	got, err := c.GetPosts(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	check("GetPosts", got)
	list, err := c.ListPosts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	check("ListPosts", list)
	note, err := c.GetNotes(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	check("GetNotes", note)
	batch, err := c.BatchDeleteNotes(ctx, []string{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	check("BatchDeleteNotes", batch)
}
`

// A json field reads a number the server holds exactly (crud keeps
// 2^53+1, number_wire_test.go) as the same number: decoded into any it
// became a float64 and the client re-sent 9007199254740992.
func TestClientKeepsBigIntegers(t *testing.T) {
	decls := []framework.EntityDeclaration{
		{Name: "authors", Fields: []framework.FieldDeclaration{{Name: "email", Type: "string"}}},
		{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "meta", Type: "json"}}},
		{Name: "notes", Fields: []framework.FieldDeclaration{
			{Name: "meta", Type: "json"},
			{Name: "author_id", Type: "relation", To: "authors"},
		}},
	}
	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/client\n\ngo 1.21\n")
	clitest.WriteTestFile(t, filepath.Join(dir, "client.go"), renderClient(decls))
	clitest.WriteTestFile(t, filepath.Join(dir, "number_test.go"), clientNumberTest)
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated client: %v\n%s", err, out)
	}
}
