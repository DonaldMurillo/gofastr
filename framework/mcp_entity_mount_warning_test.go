package framework

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An entity that opts into MCP tools (Exposure.MCP: true) registers its
// tools on app.MCP, but without WithMCP() (or a hand-mounted /mcp route)
// nothing serves them in production. Dev auto-mounts /mcp, so the
// misconfiguration is invisible until deploy day: boot must warn instead.
func TestStartWarnsWhenEntityMCPToolsUnreachable(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	db := sqliteDB(t)
	t.Cleanup(func() { db.Close() })
	createPostsTable(t, db)

	app := NewApp(WithDB(db), WithLogger(logger))
	app.Entity("posts", entity.EntityConfig{
		Table:    "posts",
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{MCP: true},
	}.WithTimestamps(false))

	_, cleanup := startApp(t, app)
	defer cleanup()

	got := buf.String()
	if !strings.Contains(got, "/mcp") || !strings.Contains(got, "WithMCP()") {
		t.Errorf("missing boot warning naming the mount and the fix; log=%q", got)
	}
}

// The warning is specific: WithMCP() mounting /mcp silences it.
func TestStartEntityMCPWarningSilencedByWithMCP(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	db := sqliteDB(t)
	t.Cleanup(func() { db.Close() })
	createPostsTable(t, db)

	app := NewApp(WithDB(db), WithLogger(logger), WithMCP())
	app.Entity("posts", entity.EntityConfig{
		Table:    "posts",
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{MCP: true},
	}.WithTimestamps(false))

	_, cleanup := startApp(t, app)
	defer cleanup()

	if s := buf.String(); strings.Contains(s, "WithMCP()") {
		t.Errorf("warning fired although /mcp is mounted; log=%q", s)
	}
}

// Entities that never opted into MCP tools must not trigger the warning.
func TestStartNoEntityMCPWarningWithoutOptIn(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	db := sqliteDB(t)
	t.Cleanup(func() { db.Close() })
	createPostsTable(t, db)

	app := NewApp(WithDB(db), WithLogger(logger))
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))

	_, cleanup := startApp(t, app)
	defer cleanup()

	if s := buf.String(); strings.Contains(s, "MCP") {
		t.Errorf("warning fired without any MCP-enabled entity; log=%q", s)
	}
}
