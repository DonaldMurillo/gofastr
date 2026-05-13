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
		"type PostsListResponse struct",
		"func (c *Client) ListPosts(",
		"func (c *Client) GetPosts(",
		"func (c *Client) CreatePosts(",
		"func (c *Client) UpdatePosts(",
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

// integrationTestSource is the Go test that gets dropped into the temp module
// to drive the generated client against a real httptest server. Kept as a
// raw constant (with %s placeholders intentionally avoided — the file is
// self-contained and references the generated package via the temp module's
// own import path "example.com/cli/.gofastr/entities/client").
const integrationTestSource = `package integration_test

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"

	gen "example.com/cli/.gofastr/entities/client"
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
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "views", Type: schema.Int},
		},
	}.WithTimestamps(false))
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	srv := httptest.NewServer(app.Router)
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
	if err := os.Mkdir(filepath.Join(dir, "entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "entities", "posts.json"), []byte(`{
		"name":"posts",
		"fields":[{"name":"title","type":"string","required":true},{"name":"views","type":"int"}],
		"crud":true
	}`), 0o644); err != nil {
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
	generateProject(nil)

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
	if err := os.Mkdir(filepath.Join(dir, "entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "entities", "posts.json"), []byte(`{
		"name":"posts",
		"fields":[{"name":"title","type":"string","required":true},{"name":"views","type":"int"}],
		"crud":true
	}`), 0o644); err != nil {
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
	generateProject(nil)

	clientPath := filepath.Join(dir, ".gofastr", "entities", "client", "client.go")
	if _, err := os.Stat(clientPath); err != nil {
		t.Fatalf("client file not written: %v", err)
	}

	// Client only uses the stdlib so no go mod tidy needed, but the parent
	// module needs a clean go.sum for the build resolver.
	cmd := exec.Command("go", "build", "./.gofastr/entities/client")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated client did not build: %v\n%s", err, out)
	}
}
