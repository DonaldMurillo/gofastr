package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// camelEntityDecls is one entity whose name is not snake_case and that
// declares no table. The server mounts it at the snake_case default
// (entity.Define), so every generated caller must use that route too.
func camelEntityDecls() []framework.EntityDeclaration {
	return []framework.EntityDeclaration{{
		Name:   "BlogPost",
		Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		Endpoints: []framework.Endpoint{{
			Name: "publish", Method: "POST", Path: "{id}/publish", SDK: true,
		}},
	}}
}

// Every emitted request path takes the table the server mounts, not the
// raw entity name: the CLI, the typed Go client, the SDK model and the
// endpoint calls.
func TestCamelEntityUsesSnakeRoute(t *testing.T) {
	decls := camelEntityDecls()

	if got := buildEntityModel(decls[0], nil).Table; got != "blog_post" {
		t.Errorf("entity model table = %q, want blog_post (the SDKs route on it)", got)
	}
	if got := endpointRoute(decls[0]); got != "blog_post" {
		t.Errorf("endpoint route = %q, want blog_post", got)
	}

	client := renderClient(decls)
	if !strings.Contains(client, `"/blog_post"`) || strings.Contains(client, `"/BlogPost`) {
		t.Errorf("typed client does not call /blog_post:\n%s", client)
	}

	spec, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("buildCLISpec: %v", err)
	}
	var cli strings.Builder
	for _, f := range RenderCLIFiles(spec) {
		cli.WriteString(f.Content)
	}
	if !strings.Contains(cli.String(), `"/blog_post"`) || strings.Contains(cli.String(), `"/BlogPost"`) {
		t.Errorf("CLI does not call /blog_post:\n%s", cli.String())
	}
}
