package main

import (
	"path/filepath"
	"testing"
)

// bulk: true on an entity_list emits .Bulk() on the list builder.
func TestBlueprintListBulkEmitsBulk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
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
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	screens := allScreenContent(mustRenderBlueprintFiles(t, bp))
	assertContains(t, screens, `appUI.List("posts").Columns("title").NoCreate().Bulk()`)
}

// A list placed off its entity's home (a dashboard declared before the
// entity's own screen) links its records to the home list, the one the
// detail screen sits under. The create screen and the drawer hang off the
// home list too, never the dashboard.
func TestBlueprintOffHomeListLinksHome(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
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
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	screens := allScreenContent(mustRenderBlueprintFiles(t, bp))
	assertContains(t, screens, `appUI.List("posts").Columns("title").NoCreate().Base("/app/posts").RenderCtx(ctx)`)
	assertContains(t, screens, `appUI.List("posts").Columns("title").RenderCtx(ctx)`)
	assertContains(t, screens, `appUI.Create("posts").Base("/app/posts")`)
	assertContains(t, screens, `app.Intercept{From: "/app/posts"`)
}

// The detail screen picks the home ahead of create: true, and with no
// detail screen the first create: true list is the home.
func TestBlueprintEntityHomeOrder(t *testing.T) {
	render := func(dashCreate, postsCreate, detail string) string {
		dir := t.TempDir()
		path := filepath.Join(dir, "gofastr.yml")
		writeTestFile(t, path, `
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
		bp, err := loadBlueprint(path)
		if err != nil {
			t.Fatalf("loadBlueprint: %v", err)
		}
		return allScreenContent(mustRenderBlueprintFiles(t, bp))
	}
	detail := `
  - name: post_detail
    route: /app/posts/{id}
    body:
      - kind: entity_detail
        entity: posts
`
	byDetail := render("true", "false", detail)
	assertContains(t, byDetail, `appUI.List("posts").Columns("title").Base("/app/posts").RenderCtx(ctx)`)
	assertContains(t, byDetail, `site.Register("/app/posts/create"`)

	byCreate := render("false", "true", "")
	assertContains(t, byCreate, `appUI.List("posts").Columns("title").NoCreate().Base("/app/posts").RenderCtx(ctx)`)
	assertContains(t, byCreate, `appUI.Create("posts").Base("/app/posts")`)
}
