package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestListAllRejectsNoQueryNestedFilter(t *testing.T) {
	ch, _ := setupNoQueryRelated(t)
	_, err := ch.ListAll(context.Background(), ListOptions{
		NestedFilters: []NestedFilter{{
			Relation: "author",
			Field:    "secret",
			Op:       filter.OpLike,
			Value:    "SECRET-0",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be filtered") {
		t.Fatalf("ListAll NoQuery nested filter error = %v, want cannot be filtered", err)
	}
}

func TestEventStreamRunsResponseHook(t *testing.T) {
	ch, _ := setupRedactedHandler(t)
	ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		p := data.(*hook.GetPayload)
		if _, ok := p.Result["body"]; ok {
			p.Result["body"] = "REDACTED"
		}
		return nil
	})
	ch.Events = event.NewEventBus()

	ctx, cancel := context.WithCancel(withTestUserCtx("u1"))
	req := httptest.NewRequest(http.MethodGet, "/redacted_notes/_events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		ch.EventStream()(rec, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	ch.EmitEvent(withTestUserCtx("u1"), event.EntityCreated, map[string]any{
		"id": "n1", "owner": "alice", "body": "SECRET-042",
	})
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("event stream did not stop after cancellation")
	}
	body := rec.Body.String()
	if strings.Contains(body, "SECRET-042") || !strings.Contains(body, "REDACTED") {
		t.Fatalf("event stream body = %s, want REDACTED and no stored value", body)
	}
}

func TestMCPListToolDropsNoQueryArguments(t *testing.T) {
	parent, db := setupNoQueryRelated(t)
	authors, err := parent.Registry.Get("nq_authors")
	if err != nil {
		t.Fatal(err)
	}
	ch := NewCrudHandler(authors, db)

	var got url.Values
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"total":0}`))
	})
	_, err = ch.listTool(router)(context.Background(), map[string]any{
		"name":        "alice",
		"secret":      "SECRET-042",
		"secret_like": "SECRET-0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Has("secret") || got.Has("secret_like") {
		t.Fatalf("MCP forwarded NoQuery arguments: %v", got)
	}
	if got.Get("name") != "alice" {
		t.Fatalf("MCP dropped queryable argument: %v", got)
	}
}

func TestLLMMDFilterExampleSkipsLeadingNoQueryField(t *testing.T) {
	ent := entity.Define("llm_noquery_order", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, Hidden: true},
			{Name: "secret", Type: schema.String, NoQuery: true},
			{Name: "name", Type: schema.String},
		},
		CursorField: "name",
	}.WithTimestamps(false))

	doc := EntityLLMMD(ent)
	if strings.Contains(doc, "secret_like") || strings.Contains(doc, "`secret=active`") {
		t.Fatalf("llm.md uses a NoQuery field in its filter examples:\n%s", doc)
	}
	if !strings.Contains(doc, "name_like") {
		t.Fatalf("llm.md omitted the queryable field from its filter examples:\n%s", doc)
	}
}
