package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// renderClient emits a self-contained Go file for the "client" package.
// Verify the expected types and methods land in the output.
func TestRenderClient_StructsAndMethods(t *testing.T) {
	crud := true
	tsOff := false
	out := renderClient([]framework.EntityDeclaration{{
		Name:       "posts",
		Table:      "posts",
		CRUD:       &crud,
		Timestamps: &tsOff,
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string", Required: true},
			{Name: "views", Type: "int"},
			{Name: "published", Type: "bool"},
		},
	}})

	wants := []string{
		"package client",
		"type Client struct",
		"func NewClient(baseURL string, httpClient *http.Client)",
		"type Posts struct",
		"type PostsInput struct",
		"func (c *Client) doSingleJSON(",
		"type PostsListResponse struct",
		"func (c *Client) ListPosts(",
		"func (c *Client) GetPosts(",
		"func (c *Client) CreatePosts(",
		"func (c *Client) UpdatePosts(",
		"func (c *Client) PatchPosts(",
		"func (c *Client) DeletePosts(",
		`Title string `,
		`Views int `,
		`Published bool `,
		`/posts`, // route path
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("renderClient missing %q\n--- output:\n%s", w, out)
		}
	}
}

// Custom table name must be used as the route, not the entity name.
func TestRenderClient_HonoursCustomTable(t *testing.T) {
	tsOff := false
	out := renderClient([]framework.EntityDeclaration{{
		Name:       "user",
		Table:      "app_users", // intentionally != Name
		Timestamps: &tsOff,
		Fields:     []framework.FieldDeclaration{{Name: "email", Type: "string"}},
	}})
	if !strings.Contains(out, `"/app_users"`) {
		t.Fatalf("client should call /app_users, got:\n%s", out)
	}
	if strings.Contains(out, `"/user"`) {
		t.Fatalf("client should NOT call /user; got:\n%s", out)
	}
}

// PATCH must distinguish "field absent" from "field set to its zero value"
// (false, 0, ""). A value-typed Input with json:",omitempty" cannot: both
// cases marshal away. The generator must emit a dedicated <Entity>Patch
// struct whose fields are pointers — nil omits the field, a non-nil pointer
// sets it even when it points at a zero value — and Patch<Entity> takes it.
func TestRenderClient_PatchStructUsesPointers(t *testing.T) {
	crud := true
	tsOff := false
	out := renderClient([]framework.EntityDeclaration{{
		Name:       "posts",
		Table:      "posts",
		CRUD:       &crud,
		Timestamps: &tsOff,
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string", Required: true},
			{Name: "views", Type: "int"},
			{Name: "published", Type: "bool"},
		},
	}})
	wants := []string{
		"type PostsPatch struct",
		"Title *string ",
		"Views *int ",
		"Published *bool ",
		"func (c *Client) PatchPosts(ctx context.Context, id string, body PostsPatch)",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("renderClient missing %q\n--- output:\n%s", w, out)
		}
	}
	// The create/update Input stays value-typed and unchanged.
	if !strings.Contains(out, "Title string ") || !strings.Contains(out, "Views int ") {
		t.Errorf("PostsInput should keep value-typed fields:\n%s", out)
	}
}

// A CLI or script authenticates with a scoped API token (battery/auth PAT),
// so the generated client must carry an optional bearer token: a Token field
// on Client, sent as "Authorization: Bearer <token>" on every request when
// set. NewClient's signature stays unchanged (struct-literal callers keep
// compiling; token is opt-in via c.Token = ...).
func TestRenderClient_TokenBearerHeader(t *testing.T) {
	tsOff := false
	out := renderClient([]framework.EntityDeclaration{{
		Name:       "posts",
		Table:      "posts",
		Timestamps: &tsOff,
		Fields:     []framework.FieldDeclaration{{Name: "title", Type: "string"}},
	}})
	wants := []string{
		"Token   string", // gofmt-aligned struct field
		`req.Header.Set("Authorization", "Bearer "+c.Token)`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("renderClient missing %q\n--- output:\n%s", w, out)
		}
	}
	if !strings.Contains(out, "func NewClient(baseURL string, httpClient *http.Client)") {
		t.Errorf("NewClient signature must stay unchanged:\n%s", out)
	}
}

// The _batch endpoints are part of the CRUD surface, so the generated client
// must cover them: BatchCreate (value inputs), BatchUpdate (id + pointer
// fields, mirroring the PATCH presence semantics), BatchDelete (ids), all
// returning the shared {committed, results[]} envelope.
func TestRenderClient_BatchMethods(t *testing.T) {
	tsOff := false
	out := renderClient([]framework.EntityDeclaration{{
		Name:       "posts",
		Table:      "posts",
		Timestamps: &tsOff,
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
			{Name: "published", Type: "bool"},
		},
	}})
	wants := []string{
		"type BatchResult struct",
		"type BatchResponse struct",
		"type PostsBatchPatch struct",
		"ID string `json:\"id\"`",
		"func (c *Client) BatchCreatePosts(ctx context.Context, items []PostsInput) (BatchResponse, error)",
		"func (c *Client) BatchUpdatePosts(ctx context.Context, items []PostsBatchPatch) (BatchResponse, error)",
		"func (c *Client) BatchDeletePosts(ctx context.Context, ids []string) (BatchResponse, error)",
		`"/posts/_batch"`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("renderClient missing %q", w)
		}
	}
}

// Every entity mounts a GET {path}/_events SSE feed; the generated client
// exposes it as Watch<Entity>: a blocking loop that parses event:/data:
// frames and hands each to the callback until ctx cancels, the stream ends,
// or the callback errors.
func TestRenderClient_WatchSSE(t *testing.T) {
	tsOff := false
	out := renderClient([]framework.EntityDeclaration{{
		Name:       "posts",
		Table:      "posts",
		Timestamps: &tsOff,
		Fields:     []framework.FieldDeclaration{{Name: "title", Type: "string"}},
	}})
	wants := []string{
		"func (c *Client) WatchPosts(ctx context.Context, fn func(event string, data []byte) error) error",
		`"/posts/_events"`,
		"func (c *Client) watchSSE(",
		`"text/event-stream"`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("renderClient missing %q", w)
		}
	}
}

// integrationTestSource is the Go test that gets dropped into the temp module
// to drive the generated client against a real httptest server. Kept as a
// raw constant (with %s placeholders intentionally avoided — the file is
// self-contained and references the generated package via the temp module's
// own import path "example.com/cli/gen/entities/client").
const integrationTestSource = `package integration_test

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"

	gen "example.com/cli/gen/entities/client"
)

// TestGeneratedClient_RoundTrip stands up the same entity the generator was
// run against, points the generated client at it, and exercises every CRUD
// method. Anything that drifts between server contract and generated
// client shows up here.
func TestGeneratedClient_RoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithoutDefaultMiddleware(),
	)
	app.Entity("posts", framework.EntityConfig{
		Table:  "posts",
		Public: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "views", Type: schema.Int},
			{Name: "published", Type: schema.Bool},
		},
	}.WithTimestamps(false))
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	ctx := context.Background()
	c := gen.NewClient(srv.URL, nil)

	created, err := c.CreatePosts(ctx, gen.PostsInput{Title: "hello", Views: 1})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Title != "hello" || created.ID == "" {
		t.Fatalf("create round-trip lost data: %+v", created)
	}

	got, err := c.GetPosts(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("get returned wrong row: %+v", got)
	}

	updated, err := c.UpdatePosts(ctx, created.ID, gen.PostsInput{Title: "edited", Views: 99})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "edited" || updated.Views != 99 {
		t.Fatalf("update did not persist: %+v", updated)
	}

	patched, err := c.PatchPosts(ctx, created.ID, gen.PostsPatch{
		Views:     new(0),   // was 99 → set to zero value
		Published: new(false), // set bool to its zero value
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	// PATCH must set fields to their zero values (0, false). A value-typed
	// Input with json:",omitempty" cannot express this — both "absent" and
	// "set to zero" marshal away, so the server would see an empty body.
	// The pointer-based Patch keeps non-nil pointers even when they point at
	// a zero value, so the server applies the update. Title is left nil here
	// (untouched) and must stay "edited".
	if patched.Title != "edited" || patched.Views != 0 || patched.Published {
		t.Fatalf("patch did not set zero values / preserve untouched fields: %+v", patched)
	}

	list, err := c.ListPosts(ctx, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("expected total=1, got %d", list.Total)
	}
	if len(list.Data) != 1 || list.Data[0].Title != "edited" {
		t.Fatalf("list payload mismatch: %+v", list)
	}

	if err := c.DeletePosts(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	after, err := c.ListPosts(ctx, nil)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if after.Total != 0 {
		t.Fatalf("expected empty after delete, got %d", after.Total)
	}

	// _batch round-trip: create two atomically, patch one, verify a
	// validation failure rolls back (returned as Committed=false, not an
	// error), delete both.
	bc, err := c.BatchCreatePosts(ctx, []gen.PostsInput{{Title: "b1"}, {Title: "b2", Views: 5}})
	if err != nil {
		t.Fatalf("batch create: %v", err)
	}
	if !bc.Committed || len(bc.Results) != 2 {
		t.Fatalf("batch create: %+v", bc)
	}
	id1, _ := bc.Results[0].Data["id"].(string)
	id2, _ := bc.Results[1].Data["id"].(string)
	if id1 == "" || id2 == "" {
		t.Fatalf("batch create ids missing: %+v", bc.Results)
	}

	bu, err := c.BatchUpdatePosts(ctx, []gen.PostsBatchPatch{{ID: id1, Views: new(7)}})
	if err != nil {
		t.Fatalf("batch update: %v", err)
	}
	if !bu.Committed {
		t.Fatalf("batch update: %+v", bu)
	}

	rb, err := c.BatchCreatePosts(ctx, []gen.PostsInput{{Title: "ok"}, {}})
	if err != nil {
		t.Fatalf("batch rollback: %v", err)
	}
	if rb.Committed {
		t.Fatalf("expected rollback, got committed: %+v", rb)
	}

	bd, err := c.BatchDeletePosts(ctx, []string{id1, id2})
	if err != nil {
		t.Fatalf("batch delete: %v", err)
	}
	if !bd.Committed {
		t.Fatalf("batch delete: %+v", bd)
	}
	final, err := c.ListPosts(ctx, nil)
	if err != nil {
		t.Fatalf("final list: %v", err)
	}
	if final.Total != 0 {
		t.Fatalf("expected empty after batch delete, got %d", final.Total)
	}

	// Watch: subscribe to the SSE feed, then create until the event lands.
	// Creation retries paper over the subscription attach race without a
	// fixed sleep.
	wctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	events := make(chan string, 1)
	go func() {
		_ = c.WatchPosts(wctx, func(event string, _ []byte) error {
			select {
			case events <- event:
			default:
			}
			return context.Canceled // one event is enough
		})
	}()
	var gotEvent string
poll:
	for i := 0; i < 50; i++ {
		if _, err := c.CreatePosts(ctx, gen.PostsInput{Title: "sse"}); err != nil {
			t.Fatalf("create for sse: %v", err)
		}
		select {
		case gotEvent = <-events:
			break poll
		case <-time.After(200 * time.Millisecond):
		case <-wctx.Done():
			break poll
		}
	}
	if gotEvent != "entity.created" {
		t.Fatalf("expected entity.created via watch, got %q", gotEvent)
	}
}

`

// TestGenerateClient_RoundTripAgainstLiveServer runs the full pipeline:
// generate the client against a fixture entity, drop a test file in the temp
// module that boots a framework server in-process and calls every CRUD
// method, then run `go test`. Anything that drifts between server contract
// and generated client surfaces here.
func TestGenerateClient_RoundTripAgainstLiveServer(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/cli\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(`app:
  name: cli
entities:
  - name: posts
    crud: true
    public: true
    fields:
      - name: title
        type: string
        required: true
      - name: views
        type: int
      - name: published
        type: bool
`), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	generateProject([]string{"--from=gofastr.yml", "--out=gen"})

	// Drop the integration test file into its own package directory.
	itDir := filepath.Join(dir, "integration")
	if err := os.MkdirAll(itDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(itDir, "client_integration_test.go"),
		[]byte(integrationTestSource), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "test", "-mod=mod", "-count=1", "./integration/...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated client integration failed: %v\n%s", err, out)
	}
}

// End-to-end: write a tiny entity, run generateProject, and confirm the
// generated client/ subpackage compiles against the framework stdlib.
func TestGenerateClient_E2EBuildsCleanly(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/cli\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(`app:
  name: cli
entities:
  - name: posts
    crud: true
    public: true
    fields:
      - name: title
        type: string
        required: true
      - name: views
        type: int
`), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	generateProject([]string{"--from=gofastr.yml", "--out=gen"})

	clientPath := filepath.Join(dir, "gen", "entities", "client", "client.go")
	if _, err := os.Stat(clientPath); err != nil {
		t.Fatalf("client file not written: %v", err)
	}

	// Client only uses the stdlib so no go mod tidy needed, but the parent
	// module needs a clean go.sum for the build resolver.
	cmd := exec.Command("go", "build", "./gen/entities/client")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated client did not build: %v\n%s", err, out)
	}
}
