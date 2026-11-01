package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// The _events filter decided the read-scope lift on the context the last
// delivery or reauth tick refreshed. The first private row emitted after
// the lifting role was removed passed that stale lift, and delivery
// re-checked the permission but not the row scope, so the draft shipped
// (2026-10-27 bug bash R10). Delivery now re-applies the scope on the
// refreshed context.
func TestEventStreamRechecksReadScope(t *testing.T) {
	ddl := `CREATE TABLE notes (id TEXT PRIMARY KEY, status TEXT, body TEXT);`
	cfg := makeEntityConfig("notes", "notes", "", []schema.Field{
		{Name: "status", Type: schema.String},
		{Name: "body", Type: schema.String},
	})
	cfg.Exposure = &entity.ExposureConfig{
		Public: true,
		ReadScope: &entity.ReadScopeConfig{
			Filter:       []entity.RowPredicate{{Field: "status", Value: "published"}},
			Unrestricted: "notes:review",
		},
	}
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	ch.Events = event.NewEventBus()

	var demoted atomic.Bool
	req := grantRequest(httptest.NewRequest(http.MethodGet, "/notes/_events", nil), "u1", "notes:review")
	ctx := handler.WithPrincipalCheck(req.Context(), func(c context.Context) (context.Context, bool) {
		if demoted.Load() {
			return access.WithRoles(c, nil), true
		}
		return c, true
	})
	sctx, cancel := context.WithCancel(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { ch.EventStream()(rec, req.WithContext(sctx)); close(done) }()
	time.Sleep(50 * time.Millisecond)

	demoted.Store(true)
	ch.EmitEvent(context.Background(), event.EntityCreated,
		map[string]any{"id": "draft-1", "status": "draft", "body": "secret draft"})
	time.Sleep(150 * time.Millisecond)
	ch.EmitEvent(context.Background(), event.EntityCreated,
		map[string]any{"id": "pub-1", "status": "published", "body": "visible"})
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if strings.Contains(body, "draft-1") {
		t.Fatalf("SECURITY: a draft reached a subscriber whose lifting role was removed:\n%s", body)
	}
	if !strings.Contains(body, "pub-1") {
		t.Fatalf("the published record was withheld:\n%s", body)
	}
}
