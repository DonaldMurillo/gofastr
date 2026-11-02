package generatecmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/core/settings"
)

// endpointsHandlers is a handler file for the posts entity of
// cliTempModule: one of each input location and output shape.
const endpointsHandlers = `package entities

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

type FeaturePostInput struct {
	ID   string  ` + "`path:\"id\" json:\"-\"`" + `
	Rank int     ` + "`json:\"rank\"`" + `
	Note *string ` + "`json:\"note\"`" + `
}

func FeaturePost(ctx context.Context, in FeaturePostInput) (Posts, error) {
	title := "featured " + in.ID + " at " + strconv.Itoa(in.Rank)
	if in.Note != nil {
		title += " (" + *in.Note + ")"
	}
	return Posts{ID: in.ID, Title: title}, nil
}

type SearchPostsInput struct {
	Q     *string ` + "`query:\"q\" json:\"-\"`" + `
	Limit int     ` + "`query:\"limit\" json:\"-\"`" + `
}

var SearchPosts handler.Handler[SearchPostsInput, []Posts] = func(ctx context.Context, in SearchPostsInput) ([]Posts, error) {
	q := "*"
	if in.Q != nil {
		q = *in.Q
	}
	out := make([]Posts, in.Limit)
	for i := range out {
		out[i] = Posts{ID: strconv.Itoa(i), Title: q}
	}
	return out, nil
}

type ClearCacheInput struct {
	ID string ` + "`path:\"id\" json:\"-\"`" + `
}

func ClearCache(ctx context.Context, in ClearCacheInput) (struct{}, error) {
	if in.ID == "" {
		return struct{}{}, handler.Errorf(400, "no id")
	}
	return struct{}{}, nil
}

func ArchivePost(ctx context.Context, in ClearCacheInput) (struct{}, error) {
	return struct{}{}, nil
}
`

// endpointsWireTest runs inside the generated module: it boots the app's
// entities on a bare framework.App and calls each endpoint through the
// generated client, so the request the client builds is the one the
// handler binds.
const endpointsWireTest = `package entities_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"example.com/myapp/entities"
	"example.com/myapp/entities/client"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/sdk"
)

func TestEndpointsOverTheWire(t *testing.T) {
	app := framework.NewApp()
	entities.RegisterAll(app)
	// srv stands in for an app with auth and a CSRF gate wired: every
	// request is signed in, and a write with no JSON body is refused the
	// way a CSRF gate refuses a form post that carries no token.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && handler.IsForgeableRequest(r) {
			http.Error(w, "csrf token required", http.StatusForbidden)
			return
		}
		app.Router().ServeHTTP(w, r.WithContext(handler.SetUser(r.Context(), "u1")))
	}))
	defer srv.Close()
	c := client.NewClient(srv.URL, srv.Client())
	ctx := context.Background()

	// Anonymous: only the endpoint the settings mark public answers.
	anon := httptest.NewServer(app.Router())
	defer anon.Close()
	ac := client.NewClient(anon.URL, anon.Client())
	if _, err := ac.SearchPosts(ctx, client.SearchPostsRequest{Limit: 1}); err != nil {
		t.Errorf("public endpoint refused an anonymous caller: %v", err)
	}
	var apiErr *client.APIError
	if _, err := ac.FeaturePost(ctx, "p1", client.FeaturePostRequest{Rank: 1}); !errors.As(err, &apiErr) || apiErr.Status != 401 {
		t.Errorf("anonymous FeaturePost = %v, want 401", err)
	}

	// The MCP twin binds the same input from tool arguments.
	resp := app.MCP.HandleRequest(handler.SetUser(ctx, "u1"), mcp.Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(` + "`" + `{"name":"feature_post","arguments":{"id":"p9","rank":2}}` + "`" + `),
	})
	if out, _ := json.Marshal(resp); resp.Error != nil || !strings.Contains(string(out), "featured p9 at 2") {
		t.Errorf("MCP feature_post = %s", out)
	}

	note := "staff pick"
	post, err := c.FeaturePost(ctx, "p 1", client.FeaturePostRequest{Rank: 3, Note: &note})
	if err != nil || post.ID != "p 1" || post.Title != "featured p 1 at 3 (staff pick)" {
		t.Fatalf("FeaturePost = %+v, %v", post, err)
	}
	q := "go"
	list, err := c.SearchPosts(ctx, client.SearchPostsRequest{Q: &q, Limit: 2})
	if err != nil || len(list) != 2 || list[1].Title != "go" {
		t.Fatalf("SearchPosts = %+v, %v", list, err)
	}
	if err := c.ClearCache(ctx, "p1"); err != nil {
		t.Fatalf("ClearCache: %v", err)
	}
	if err := c.ArchivePost(ctx, "p1"); err != nil {
		t.Fatalf("ArchivePost: %v", err)
	}
	live := sdk.SchemaHash(sdk.RegistryNamedConfigs(app.Registry, []string{"posts", "documents"}))
	if want := os.Getenv("WANT_SCHEMA_HASH"); live != want {
		t.Fatalf("live schema hash %s, generated %s: the SDK would report drift", live, want)
	}

	// The generated CLI: positional path values, flags in the body or the
	// query string, the required flag enforced before any request.
	bin := filepath.Join(t.TempDir(), "shop")
	if out, err := exec.Command("go", "build", "-mod=mod", "-o", bin, "../cmd/shop").CombinedOutput(); err != nil {
		t.Fatalf("build the CLI: %v\n%s", err, out)
	}
	shop := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "SHOP_URL="+srv.URL, "HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir())
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"posts", "feature-post", "p 1", "--rank", "3", "--note", "cli"}, "featured p 1 at 3 (cli)"},
		{[]string{"posts", "feature-post", "p1", "--json", ` + "`" + `{"rank": 4}` + "`" + `}, "featured p1 at 4"},
		{[]string{"posts", "search-posts", "--limit", "2", "--q", "cli"}, ` + "`" + `"title": "cli"` + "`" + `},
		{[]string{"posts", "clear-cache", "p1"}, "{}"},
		{[]string{"posts", "archive-post", "p1"}, "{}"},
	} {
		if out, err := shop(c.args...); err != nil || !strings.Contains(out, c.want) {
			t.Errorf("shop %v = %v\n%s", c.args, err, out)
		}
	}
	if out, err := shop("posts", "feature-post", "p1"); err == nil || !strings.Contains(out, "--rank is required") {
		t.Errorf("a missing required flag was not refused: %v\n%s", err, out)
	}

	// The generated JS SDK under node, when node is installed.
	node, err := exec.LookPath("node")
	if err != nil {
		return
	}
	script := ` + "`" + `
import { Client } from "../gen/js/client.js";
const api = new Client({ baseURL: process.env.BASE });
const post = await api.posts.featurePost("p 1", { rank: 5, note: "js" });
const list = await api.posts.searchPosts({ q: "js", limit: 2 });
const cleared = await api.posts.clearCache("p1");
await api.posts.archivePost("p1");
console.log(JSON.stringify({ title: post.title, n: list.length, q: list[1].title, cleared }));
` + "`" + `
	if err := os.WriteFile("endpoints_wire.mjs", []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove("endpoints_wire.mjs")
	cmd := exec.Command(node, "endpoints_wire.mjs")
	cmd.Env = append(os.Environ(), "BASE="+srv.URL)
	out, err := cmd.CombinedOutput()
	if want := ` + "`" + `{"title":"featured p 1 at 5 (js)","n":2,"q":"js"}` + "`" + `; err != nil || strings.TrimSpace(string(out)) != want {
		t.Fatalf("JS SDK = %v\n%s\nwant %s", err, out, want)
	}
}
`

// endpointsApp generates cliTempModule's app, links the handler file and
// declares its endpoints, then runs a plain generate.
func endpointsApp(t *testing.T) string {
	t.Helper()
	dir := cliTempModule(t)
	if err := os.WriteFile(filepath.Join(dir, "entities", "featured.go"), []byte(endpointsHandlers), 0o600); err != nil {
		t.Fatal(err)
	}
	clitest.WriteSettings(t, dir, clitest.EndpointsSettings)
	var code int
	out := clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, func() { appgen.GenerateProject(nil) }) })
	if code != -1 {
		t.Fatalf("generate exited %d:\n%s", code, out)
	}
	return dir
}

func TestEndpointsRegisterAndCallTyped(t *testing.T) {
	dir := endpointsApp(t)
	reg := strings.Join(strings.Fields(readFileT(t, filepath.Join(dir, appgen.AppEndpointsPath))), " ")
	for _, want := range []string{
		`app.EntityEndpoints("posts", framework.Endpoint{`,
		`Handler: handler.HandlerAdapter[FeaturePostInput, Posts](FeaturePost),`,
		`MCPHandler: handler.ToolAdapter[FeaturePostInput, Posts](FeaturePost),`,
		`{Name: "id", Type: schema.String, Required: true},`,
		`{Name: "limit", Type: schema.Int, Required: true},`,
		`Returns: "[]posts",`,
	} {
		if !strings.Contains(reg, want) {
			t.Errorf("%s lacks %s:\n%s", appgen.AppEndpointsPath, want, reg)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "entities", "endpoints_wire_test.go"), []byte(endpointsWireTest), 0o600); err != nil {
		t.Fatal(err)
	}
	decls, err := appgen.ReadAppDecls(dir)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := appgen.SdkSchemaHash(decls)
	if err != nil {
		t.Fatal(err)
	}
	withoutEndpoints, _ := appgen.SdkSchemaHash(appgen.DeclsWithEndpoints(decls, []appgen.AppEndpoint{{Endpoint: settings.Endpoint{Entity: "none"}}}))
	if hash == withoutEndpoints {
		t.Fatal("the schema hash ignores the settings endpoints")
	}
	clitest.VariantBuild(t)
	cmd := exec.Command("go", "test", "-mod=mod", "-count=1", "./entities/")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "WANT_SCHEMA_HASH="+hash)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated endpoints did not round-trip: %v\n%s", err, out)
	}
}

func readFileT(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// resolverPrelude opens every handler file the refusal cases write.
const resolverPrelude = `package entities

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

var _ handler.Handler[struct{}, struct{}]
var _ context.Context

`

func TestEndpointResolverRefusals(t *testing.T) {
	dir := cliTempModule(t)
	decls, err := appgen.PackReadEntities(dir)
	if err != nil {
		t.Fatal(err)
	}
	sig := "func H(ctx context.Context, in In) (Posts, error) { return Posts{}, nil }\n"
	for _, c := range []struct {
		name, src, entry, want string
	}{
		{"unknown entity", "type In struct{}\n" + sig, "entity: nope\n    method: POST\n    path: x", `entity "nope" is not declared`},
		{"missing file", "type In struct{}\n" + sig, "handler: entities/gone.go#H", "does not exist"},
		{"missing handler", "type In struct{}\n" + sig, "handler: entities/x.go#G", "declares no func or var G"},
		{"bad signature", "type In struct{}\nfunc H(in In) (Posts, error) { return Posts{}, nil }\n", "", "must have the handler signature"},
		{"untyped var", "type In struct{}\nvar H = handler.Handler[In, Posts](nil)\n", "", "must be declared with its type"},
		{"bad output", "type In struct{}\nfunc H(ctx context.Context, in In) (string, error) { return \"\", nil }\n", "", "output must be an entity type"},
		{"inline input", "func H(ctx context.Context, in struct{ X int }) (Posts, error) { return Posts{}, nil }\n", "", "declare the input as a named struct"},
		{"scalar input", "func H(ctx context.Context, in string) (Posts, error) { return Posts{}, nil }\n", "", "must be a struct type"},
		{"embedded", "type In struct{ Posts }\n" + sig, "", "embedded field"},
		{"header", "type In struct{ X string `header:\"x\" json:\"-\"` }\n" + sig, "", "header tags are not supported"},
		{"path and query", "type In struct{ X string `path:\"x\" query:\"x\" json:\"-\"` }\n" + sig, "path: \"{x}/y\"", "tagged both path and query"},
		{"path in body", "type In struct{ ID string `path:\"id\"` }\n" + sig, "path: \"{id}/y\"", "must also be tagged json"},
		{"path not routed", "type In struct{ ID string `path:\"slug\" json:\"-\"` }\n" + sig, "path: \"{id}/y\"", "route has no {slug}"},
		{"unbound path", "type In struct{}\n" + sig, "path: \"{id}/y\"", "the path names {id}"},
		{"query on post", "type In struct{ Q string `query:\"q\" json:\"-\"` }\n" + sig, "", "needs a GET or DELETE"},
		{"query option", "type In struct{ Q string `query:\"q,omitempty\" json:\"-\"` }\n" + sig, "method: GET", `query option "omitempty" is not supported`},
		{"path option", "type In struct{ ID string `path:\"id,empty\" json:\"-\"` }\n" + sig, "path: \"{id}/y\"", `path option "empty" is not supported`},
		{"query slice", "type In struct{ Q []string `query:\"q\" json:\"-\"` }\n" + sig, "method: GET", "must be a string, number or bool"},
		{"path pointer", "type In struct{ ID *string `path:\"id\" json:\"-\"` }\n" + sig, "path: \"{id}/y\"", "drop the pointer"},
		{"untagged body", "type In struct{ Rank int }\n" + sig, "", "give it a json tag"},
		{"body on get", "type In struct{ Rank int `json:\"rank\"` }\n" + sig, "method: GET", "takes no JSON body"},
		{"bad wire name", "type In struct{ Rank int `json:\"ra-nk\"` }\n" + sig, "", "must be a plain name"},
		{"shared wire name", "type In struct{ ID string `path:\"id\" json:\"-\"`\n\tID2 string `json:\"id\"` }\n" + sig, "path: \"{id}/y\"", "bound by an earlier field"},
		{"resource member", "type In struct{}\n" + sig, "name: list", "every resource already has"},
		{"object member", "type In struct{}\n" + sig, "name: to_string", "every resource already has"},
		{"cli verb", "type In struct{}\n" + sig, "name: help", "is a built-in verb"},
		{"dash key on path", "type In struct{ ID string `path:\"id\" json:\"-,omitempty\"` }\n" + sig, "path: \"{id}/y\"", "names the body key"},
		{"dash key in body", "type In struct{ X string `json:\"-,\"` }\n" + sig, "", "names the body key"},
		{"string option", "type In struct{ N int `json:\"n,string\"` }\n" + sig, "", `option "string" is not supported`},
		{"go arg clash", "type In struct{ A string `path:\"type\" json:\"-\"`\n\tB string `path:\"type_arg\" json:\"-\"` }\n" + sig, "path: \"{type}/{type_arg}/y\"", "both become the Go parameter typeArg"},
		{"go field clash", "type In struct{ A string `json:\"foo_bar\"`\n\tB string `json:\"fooBar\"` }\n" + sig, "", "both become the Go field FooBar"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, "entities", "x.go"), []byte(resolverPrelude+c.src), 0o600); err != nil {
				t.Fatal(err)
			}
			entry := map[string]string{"name": "call_it", "entity": "posts", "method": "POST", "path": "x", "handler": "entities/x.go#H"}
			for _, line := range strings.Split(c.entry, "\n") {
				if k, v, ok := strings.Cut(strings.TrimSpace(line), ": "); ok {
					entry[k] = v
				}
			}
			var yml strings.Builder
			yml.WriteString("endpoints:\n")
			for i, k := range []string{"name", "entity", "method", "path", "handler"} {
				lead := "    "
				if i == 0 {
					lead = "  - "
				}
				yml.WriteString(lead + k + ": " + entry[k] + "\n")
			}
			clitest.WriteSettings(t, dir, yml.String())
			_, err := appgen.ReadAppEndpoints(dir, decls)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
		})
	}
}

// Two snake names that fold to one generated call are refused, on any
// pair of entities: the Go client's methods share one namespace.
func TestEndpointNamesFoldingTogether(t *testing.T) {
	dir := cliTempModule(t)
	decls, err := appgen.PackReadEntities(dir)
	if err != nil {
		t.Fatal(err)
	}
	src := resolverPrelude + "type In struct{}\nfunc H(ctx context.Context, in In) (Posts, error) { return Posts{}, nil }\n"
	if err := os.WriteFile(filepath.Join(dir, "entities", "x.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	clitest.WriteSettings(t, dir, `endpoints:
  - name: cache_bust1
    entity: posts
    method: POST
    path: a
    handler: entities/x.go#H
  - name: cache_bust_1
    entity: posts
    method: POST
    path: b
    handler: entities/x.go#H
`)
	if _, err := appgen.ReadAppEndpoints(dir, decls); err == nil || !strings.Contains(err.Error(), "both become the call CacheBust1") {
		t.Fatalf("folded names: %v", err)
	}
}

// generate endpoints in an app with no entities yet fails on a declared
// endpoint instead of writing nothing in silence.
func TestEndpointsWithNoEntitiesFail(t *testing.T) {
	dir := cliTempModule(t)
	if err := os.RemoveAll(filepath.Join(dir, "entities")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "entities"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "entities", "register.go"), []byte(appgen.RenderRegisterSeam()), 0o600); err != nil {
		t.Fatal(err)
	}
	clitest.WriteSettings(t, dir, "endpoints:\n  - name: ping\n    entity: posts\n    method: POST\n    path: ping\n    handler: entities/register.go#H\n")
	if _, err := appgen.RefreshAppEndpoints(dir, true, true); err == nil || !strings.Contains(err.Error(), `entity "posts" is not declared`) {
		t.Fatalf("no entities: %v", err)
	}
}

// TestEndpointResolverAcceptsEmptyQueryOption pins that a query tag's
// ",empty" option (handler.Bind keeps an explicit ?q= for it) is accepted
// and the wire name stays the bare name.
func TestEndpointResolverAcceptsEmptyQueryOption(t *testing.T) {
	dir := cliTempModule(t)
	decls, err := appgen.PackReadEntities(dir)
	if err != nil {
		t.Fatal(err)
	}
	src := resolverPrelude + "type In struct{ Q *string `query:\"q,empty\" json:\"-\"` }\n" +
		"func H(ctx context.Context, in In) (Posts, error) { return Posts{}, nil }\n"
	if err := os.WriteFile(filepath.Join(dir, "entities", "x.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	clitest.WriteSettings(t, dir, "endpoints:\n  - name: call_it\n    entity: posts\n    method: GET\n    path: x\n    handler: entities/x.go#H\n")
	eps, err := appgen.ReadAppEndpoints(dir, decls)
	if err != nil {
		t.Fatalf("query:\"q,empty\" refused: %v", err)
	}
	var names []string
	for _, d := range appgen.DeclsWithEndpoints(decls, eps) {
		for _, e := range d.Endpoints {
			for _, f := range e.InputSchema {
				names = append(names, f.Name)
			}
		}
	}
	if !slices.Equal(names, []string{"q"}) {
		t.Fatalf("input names = %q, want [q]", names)
	}
}
