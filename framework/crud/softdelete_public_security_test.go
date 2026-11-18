package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestSoftDelete_PublicListDoesNotExposeDeletedRecords(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("announcements", "announcements", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}, func(c *entity.EntityConfig) {
		if c.Scope == nil {
			c.Scope = &entity.ScopeConfig{}
		}
		if c.Exposure == nil {
			c.Exposure = &entity.ExposureConfig{}
		}
		c.Scope.SoftDelete = true
		c.Exposure.Public = true
	}),
		`CREATE TABLE announcements (id TEXT PRIMARY KEY, title TEXT, deleted_at TEXT)`)

	seedRows(t, db, "announcements", []map[string]any{
		{"id": "ann-1", "title": "deleted announcement", "deleted_at": "2024-01-01T00:00:00Z"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/announcements?trashed=true",
	})
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertBodyNotContains(t, rec, "deleted announcement", "softdelete",
		"anonymous caller retrieved deleted rows from a public entity via ?trashed=true")
	resp := decodeListResponse(t, rec.Body.String())
	if resp.Total != 0 {
		t.Fatalf("SECURITY: [softdelete] trashed=true public list returned total=%d. Attack: deleted rows counted and exposed to anonymous callers.", resp.Total)
	}
}

func TestSoftDelete_PublicGetWithTrashedReturnsNotFound(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("announcements", "announcements", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}, func(c *entity.EntityConfig) {
		if c.Scope == nil {
			c.Scope = &entity.ScopeConfig{}
		}
		if c.Exposure == nil {
			c.Exposure = &entity.ExposureConfig{}
		}
		c.Scope.SoftDelete = true
		c.Exposure.Public = true
	}),
		`CREATE TABLE announcements (id TEXT PRIMARY KEY, title TEXT, deleted_at TEXT)`)

	seedRows(t, db, "announcements", []map[string]any{
		{"id": "ann-1", "title": "deleted announcement", "deleted_at": "2024-01-01T00:00:00Z"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/announcements/ann-1?trashed=true",
	})
	req.SetPathValue("id", "ann-1")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)

	assertStatus(t, rec, http.StatusNotFound, "softdelete",
		"anonymous caller fetched a soft-deleted public record directly via GET ?trashed=true")
	assertBodyNotContains(t, rec, "deleted announcement", "softdelete",
		"anonymous caller fetched a soft-deleted public record directly via GET ?trashed=true")
}

// setupSoftDeletePublicAnnouncements builds a public, soft-deleting
// announcements handler seeded with one trashed row.
func setupSoftDeletePublicAnnouncements(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("announcements", "announcements", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}, func(c *entity.EntityConfig) {
		if c.Scope == nil {
			c.Scope = &entity.ScopeConfig{}
		}
		if c.Exposure == nil {
			c.Exposure = &entity.ExposureConfig{}
		}
		c.Scope.SoftDelete = true
		c.Exposure.Public = true
	}),
		`CREATE TABLE announcements (id TEXT PRIMARY KEY, title TEXT, deleted_at TEXT)`)
	seedRows(t, db, "announcements", []map[string]any{
		{"id": "ann-1", "title": "deleted announcement", "deleted_at": "2024-01-01T00:00:00Z"},
	})
	return ch, db
}

// TestSoftDelete_PublicPagedListDoesNotExposeDeletedRecords pins both
// non-buffered list paths (?stream=true and ?cursor=): a public entity's
// trashed rows stay hidden even when the caller asks for ?trashed=true.
func TestSoftDelete_PublicPagedListDoesNotExposeDeletedRecords(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		attack string
	}{
		{"stream", "/announcements?trashed=true&stream=true&limit=100000",
			"stream=true bypassed soft-delete visibility controls."},
		{"cursor", "/announcements?trashed=true&cursor=",
			"anonymous caller paged through deleted rows via ?trashed=true."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch, _ := setupSoftDeletePublicAnnouncements(t)
			rec := httptest.NewRecorder()
			ch.List()(rec, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: tc.path}))
			if rec.Code != http.StatusOK {
				t.Fatalf("expected %s list status 200, got %d body=%s", tc.name, rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "deleted announcement") {
				t.Fatalf("SECURITY: [softdelete] %s list exposed deleted public record. Attack: %s", tc.name, tc.attack)
			}
		})
	}
}
