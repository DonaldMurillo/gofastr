package appgen

import (
	"go/format"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// counterDecl keys on a declared integer id the database assigns.
func counterDecl() framework.EntityDeclaration {
	return framework.EntityDeclaration{
		Name:  "counters",
		Table: "counters",
		Fields: []framework.FieldDeclaration{
			{Name: "id", Type: "int", AutoGenerate: "increment"},
			{Name: "label", Type: "string", Required: true},
		},
	}
}

// The server answers a declared integer id as a JSON number: a client
// typing it string fails every decode after the row is saved.
func TestClientTypesIntID(t *testing.T) {
	src, err := format.Source([]byte(renderClient([]framework.EntityDeclaration{counterDecl()})))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ID    int    `json:\"id\"`",
		"func (c *Client) GetCounters(ctx context.Context, id int) (Counters, error)",
		"func (c *Client) DeleteCounters(ctx context.Context, id int) error",
		`"/counters/"+strconv.Itoa(id)`,
		"func (c *Client) BatchDeleteCounters(ctx context.Context, ids []int)",
		"\t\"strconv\"\n",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("client lacks %q", want)
		}
	}
	model := renderEntityModel(counterDecl())
	if !strings.Contains(model, "ID int `json:\"id\"`") {
		t.Errorf("entity model does not type the id int:\n%s", model)
	}

	opts := SdkOptions{name: "myapp", sdkVersion: "1.0.0"}
	spec, err := buildSDKSpec([]framework.EntityDeclaration{counterDecl()}, &opts)
	if err != nil {
		t.Fatal(err)
	}
	var dts string
	for _, f := range renderSDKJSFiles(spec) {
		if f.Name == "client.d.ts" {
			dts = f.Content
		}
	}
	for _, want := range []string{"  id: number | string;\n", "Resource<Counters, CountersInput, CountersPatch, number | string, { id: number | string }>"} {
		if !strings.Contains(dts, want) {
			t.Errorf("client.d.ts lacks %q", want)
		}
	}
}
