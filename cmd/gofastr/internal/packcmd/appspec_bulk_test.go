package packcmd

import (
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// bulk: true on an entity_list emits .Bulk() on the list builder.
func TestAppSpecListBulkEmitsBulk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: posts
    route: /posts
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
        bulk: true
`)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	screens := allScreenContent(mustRenderAppSpecFiles(t, bp))
	// No record screen, so the rows draw without links (NoLinks); Bulk
	// still follows.
	clitest.AssertContains(t, screens, `appUI.List("posts").Columns("title").NoCreate().NoLinks().Bulk()`)
}

// A list placed off its entity's home (a dashboard declared before the
// entity's own screen) links its records to the home list, the one the
// detail screen sits under. The create screen and the drawer hang off the
// home list too, never the dashboard.
func TestAppSpecOffHomeListLinksHome(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: dashboard
    route: /app
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
  - name: posts
    route: /app/posts
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
        create: true
  - name: post_detail
    route: /app/posts/{id}
    body:
      - kind: entity_detail
        entity: posts
`)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	screens := allScreenContent(mustRenderAppSpecFiles(t, bp))
	clitest.AssertContains(t, screens, `appUI.List("posts").Columns("title").NoCreate().Base("/app/posts").RenderCtx(ctx)`)
	clitest.AssertContains(t, screens, `appUI.List("posts").Columns("title").RenderCtx(ctx)`)
	clitest.AssertContains(t, screens, `appUI.Create("posts").Base("/app/posts")`)
	clitest.AssertContains(t, screens, `app.Intercept{From: "/app/posts"`)
}

// The detail screen picks the home ahead of create: true, and with no
// detail screen the first create: true list is the home.
func TestAppSpecEntityHomeOrder(t *testing.T) {
	render := func(dashCreate, postsCreate, detail string) string {
		dir := t.TempDir()
		path := filepath.Join(dir, "gofastr.yml")
		clitest.WriteTestFile(t, path, `
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: dashboard
    route: /app
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
        create: `+dashCreate+`
  - name: posts
    route: /app/posts
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
        create: `+postsCreate+`
`+detail)
		bp, err := appspec.LoadAppSpec(path)
		if err != nil {
			t.Fatalf("loadAppSpec: %v", err)
		}
		return allScreenContent(mustRenderAppSpecFiles(t, bp))
	}
	detail := `
  - name: post_detail
    route: /app/posts/{id}
    body:
      - kind: entity_detail
        entity: posts
`
	byDetail := render("true", "false", detail)
	clitest.AssertContains(t, byDetail, `appUI.List("posts").Columns("title").Base("/app/posts").RenderCtx(ctx)`)
	clitest.AssertContains(t, byDetail, `site.Register("/app/posts/create"`)

	byCreate := render("false", "true", "")
	// With no record screen the dashboard's rows have nowhere to link, so
	// it draws them as text (NoLinks) and needs no Base; the create
	// screen hanging off /app/posts is what shows that list is the home.
	clitest.AssertContains(t, byCreate, `appUI.List("posts").Columns("title").NoCreate().NoLinks().RenderCtx(ctx)`)
	clitest.AssertContains(t, byCreate, `appUI.Create("posts").Base("/app/posts")`)
}
