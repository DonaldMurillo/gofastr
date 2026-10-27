package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// cancelOnFlush cancels the query context once the first row is on the
// wire: the rows loop then ends on rows.Err, the mid-stream failure.
type cancelOnFlush struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

// Flush cancels, then gives database/sql's watcher goroutine time to close
// the rows, so the next rows.Next reports context.Canceled.
func (c cancelOnFlush) Flush() {
	c.cancel()
	time.Sleep(50 * time.Millisecond)
}

// A stream that fails after its first row closed the envelope with the
// full total, so a truncated list parsed as a complete one.
func TestStreamErrorLeavesJSONOpen(t *testing.T) {
	cfg := makeEntityConfig("logs", "logs", "", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "msg", Type: schema.String},
	})
	cfg.Exposure = &entity.ExposureConfig{Public: true}
	ch, db := setupSecurityTestHandler(t, cfg, `CREATE TABLE logs (id TEXT PRIMARY KEY, msg TEXT)`)
	for _, id := range []string{"a", "b", "c", "d"} {
		seedRows(t, db, "logs", []map[string]any{{"id": id, "msg": "m"}})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := cancelOnFlush{httptest.NewRecorder(), cancel}
	r := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/logs?stream=true"})
	ch.ServeStreamingList(ctx, w, r, ch.visibleFields(), nil, nil, nil, 1, 10, nil)
	body := w.Body.Bytes()
	if json.Valid(body) {
		t.Fatalf("truncated stream is valid JSON: %s", body)
	}
}
