package freeze_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestFreezeWritesBlueprintAndWorldSnapshot(t *testing.T) {
	w := world.New()
	w.App.Name = "blog"
	w.App.Module = "example.com/blog"
	w.Entities["posts"] = &world.Entity{
		Name: "posts", OwnerField: "user_id", SearchFields: []string{"title", "body"},
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
			{Name: "body", Type: "text"},
		},
	}
	w.Pages["/posts"] = &world.Page{
		Path: "/posts", Name: "posts", Title: "Posts",
		Tree: world.Node{Kind: "page_header", Props: map[string]any{"title": "Posts"}},
	}
	w.Nav = []world.NavItem{{Label: "Posts", Href: "/posts"}}

	dir := t.TempDir()
	if err := freeze.Freeze(w, dir); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	buf, err := os.ReadFile(filepath.Join(dir, "gofastr.yml"))
	if err != nil {
		t.Fatalf("read gofastr.yml: %v", err)
	}
	yml := string(buf)
	for _, want := range []string{
		"name: blog", "module: example.com/blog", "api_prefix: api",
		"owner_field: user_id", "search_fields:", "route: /posts", "nav:",
	} {
		if !strings.Contains(yml, want) {
			t.Errorf("gofastr.yml missing %q:\n%s", want, yml)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "entities")); !os.IsNotExist(err) {
		t.Errorf("legacy entities/ output should be gone; stat err=%v", err)
	}

	snapshot, err := os.ReadFile(filepath.Join(dir, "world.json"))
	if err != nil {
		t.Fatalf("read world.json: %v", err)
	}
	var got world.World
	if err := json.Unmarshal(snapshot, &got); err != nil {
		t.Fatalf("unmarshal world snapshot: %v", err)
	}
	if got.App.Name != "blog" || got.Entities["posts"].OwnerField != "user_id" {
		t.Fatalf("snapshot lost current world fields: %+v", got)
	}
}

func TestFreezeIdempotent(t *testing.T) {
	w := world.New()
	w.Entities["posts"] = &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	dir := t.TempDir()
	if err := freeze.Freeze(w, dir); err != nil {
		t.Fatalf("first: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "gofastr.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := freeze.Freeze(w, dir); err != nil {
		t.Fatalf("second: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, "gofastr.yml"))
	if string(first) != string(second) {
		t.Fatalf("freeze output changed across identical runs:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func TestFreezeRejectsNilWorld(t *testing.T) {
	if err := freeze.Freeze(nil, t.TempDir()); err == nil {
		t.Fatal("nil world should error")
	}
}

func TestFreezeRejectsBlueprintThatCannotGraduate(t *testing.T) {
	w := world.New()
	w.Entities["records"] = &world.Entity{Name: "records", MultiTenant: true}
	if err := freeze.Freeze(w, t.TempDir()); err == nil || !strings.Contains(err.Error(), "tenant resolver") {
		t.Fatalf("expected actionable multi-tenant graduation error, got %v", err)
	}
}

func TestBlueprintKeepsLegacyRelationTarget(t *testing.T) {
	w := world.New()
	w.Entities["posts"] = &world.Entity{
		Name:      "posts",
		Relations: []world.Relation{{Name: "author", To: "users", Type: "belongs_to", ForeignKey: "user_id"}},
	}
	buf, err := freeze.BlueprintYAML(w)
	if err != nil {
		t.Fatal(err)
	}
	yml := string(buf)
	if !strings.Contains(yml, "entity: users") || strings.Contains(yml, "to: users") {
		t.Fatalf("legacy relation target did not normalize to current blueprint shape:\n%s", yml)
	}
}
