package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastr/gofastr/framework"
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
	goMod := "module example.com/cli\n\ngo " + goVersion + "\n\nrequire github.com/gofastr/gofastr v0.0.0\n\nreplace github.com/gofastr/gofastr => " + repoRoot + "\n"
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
