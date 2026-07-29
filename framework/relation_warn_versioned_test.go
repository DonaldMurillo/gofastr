package framework

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestRelationWarnQuietForVersions is the multi-version twin
// of TestWarnUnresolvableRelationsIsSelective (app_security_test.go:168),
// which only ever exercises unversioned App.Entity registrations.
//
// warnUnresolvableRelations (framework/app.go:2345) asks
// Registry.Get(rel.Entity). Registry.Get was changed in this release range to
// return an AMBIGUITY ERROR when a name has several versions and none is
// unversioned. The warn path reads any error as "not registered", so an app
// that mounts its target entity under /api/v1 and /api/v2 — a fully healthy
// graph — logs "relation target is not a registered entity; ?include= on it
// will be refused" for every relation pointing at it, on every boot.
//
// Same defect class as the eager-load fail-open that the review already fixed
// by routing through entity.ResolveTarget; this call site was missed. The harm
// is lower (a diagnostic, not a disclosure) but the fix is the same one line.
//
// Run:
//
//	go test ./framework/ -run TestRelationWarnQuietForVersions -v
func TestRelationWarnQuietForVersions(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	app := NewApp(WithLogger(logger))
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2")

	usersCfg := func() entity.EntityConfig {
		return entity.EntityConfig{
			Table:  "zzr2_users",
			Fields: []schema.Field{{Name: "name", Type: schema.String}},
		}.WithTimestamps(false)
	}
	postsCfg := func() entity.EntityConfig {
		return entity.EntityConfig{
			Table:  "zzr2_posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			Relations: []entity.Relation{
				{Name: "author", Type: entity.RelManyToOne, Entity: "zzr2_users", ForeignKey: "author_id"},
			},
		}.WithTimestamps(false)
	}

	app.GroupEntity(v1, "zzr2_users", usersCfg())
	app.GroupEntity(v2, "zzr2_users", usersCfg())
	app.GroupEntity(v1, "zzr2_posts", postsCfg())
	app.GroupEntity(v2, "zzr2_posts", postsCfg())

	app.warnUnresolvableRelations()

	if strings.Contains(buf.String(), "relation target") {
		t.Fatalf("a fully-registered multi-version graph warned about its own relation target:\n%s", buf.String())
	}
}
