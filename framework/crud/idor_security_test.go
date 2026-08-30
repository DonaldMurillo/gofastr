package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestServeStreamingList_AnonymousOwnerScopedRequestReturnsNoRows(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice secret")
	seedRow(t, db, "log-b1", "bob", "bob secret")

	req := httptest.NewRequest(http.MethodGet, "/api/logs?stream=true", nil)
	rec := httptest.NewRecorder()
	ch.ServeStreamingList(context.Background(), rec, req, []string{"id", "user_id", "notes"}, nil, nil, nil, 1, 10, nil)

	if rec.Code != http.StatusUnauthorized {
		resp := decodeListResponse(t, rec.Body.String())
		if resp.Total != 0 || len(resp.Data) != 0 {
			t.Fatalf("SECURITY: [stream-owner] anonymous streaming list returned %d rows. Attack: direct ServeStreamingList call bypassed owner gate.", len(resp.Data))
		}
	}
}

func TestServeStreamingList_DBErrorsDoNotLeakDriverText(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice secret")

	req := httptest.NewRequest(http.MethodGet, "/api/logs?stream=true", nil)
	req = withTestUser(req, "alice")
	_ = db.Close()

	rec := httptest.NewRecorder()
	ch.ServeStreamingList(context.Background(), rec, req, []string{"id", "user_id", "notes"}, nil, nil, nil, 1, 10, nil)

	body := rec.Body.String()
	if strings.Contains(strings.ToLower(body), "database is closed") || strings.Contains(strings.ToLower(body), "sql:") {
		t.Fatalf("SECURITY: [stream-error] raw driver error leaked in response body: %s", body)
	}
}

func TestNestedFilter_ManyToOneRejectsUnsafeFieldName(t *testing.T) {
	sqlStr, _ := buildExistsSubquery("posts", "id", nestedFilter{
		Relation: entity.Relation{
			Type:       entity.RelManyToOne,
			Entity:     "users",
			ForeignKey: "author_id",
		},
		Field: "name OR 1=1 --",
		Op:    filter.OpEq,
		Value: "alice",
	})
	if strings.Contains(sqlStr, "OR 1=1") {
		t.Fatalf("SECURITY: [nested-filter] many-to-one query embeds attacker field name verbatim: %s", sqlStr)
	}
}

func TestNestedFilter_HasManyRejectsUnsafeFieldName(t *testing.T) {
	sqlStr, _ := buildExistsSubquery("posts", "id", nestedFilter{
		Relation: entity.Relation{
			Type:       entity.RelHasMany,
			Entity:     "comments",
			ForeignKey: "post_id",
		},
		Field: "body OR 1=1 --",
		Op:    filter.OpEq,
		Value: "x",
	})
	if strings.Contains(sqlStr, "OR 1=1") {
		t.Fatalf("SECURITY: [nested-filter] has-many query embeds attacker field name verbatim: %s", sqlStr)
	}
}

func TestNestedFilter_ManyToManyRejectsUnsafeFieldName(t *testing.T) {
	sqlStr, _ := buildExistsSubquery("posts", "id", nestedFilter{
		Relation: entity.Relation{
			Type:             entity.RelManyToMany,
			Entity:           "tags",
			ForeignKey:       "tag_id",
			Through:          "post_tags",
			LocalKey:         "post_id",
			ForeignKeyTarget: "id",
		},
		Field: "label OR 1=1 --",
		Op:    filter.OpEq,
		Value: "x",
	})
	if strings.Contains(sqlStr, "OR 1=1") {
		t.Fatalf("SECURITY: [nested-filter] many-to-many query embeds attacker field name verbatim: %s", sqlStr)
	}
}

// TestStreamDirectWithHooksRefuses pins the hook half of the direct-call
// contract on ServeStreamingList: when the entity registers AfterList
// hooks, the streaming path must refuse (400) rather than emit
// un-redacted rows. The scoping half for direct callers is pinned by
// TestServeStreamingList_AnonymousOwnerScopedRequestReturnsNoRows above;
// the List() route's own refusal is pinned by
// TestStreamWithAfterListRejected (stream_include_hook_test.go). The
// guard currently lives only in List() (crud.go), while the exported
// method re-enforces scoping for direct callers but not the hook
// precondition, so a direct call streams stored values past a
// registered redactor.
func TestStreamDirectWithHooksRefuses(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	// One pooled connection: sqlite :memory: gives each pool connection
	// its own private database, and the seeded row must be visible to the
	// streaming query for the leak assertion to be meaningful.
	db.SetMaxOpenConns(1)
	seedRow(t, db, "log-a1", "alice", "alice secret")

	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, _ := data.(*hook.ListPayload)
		for i := range p.Results {
			delete(p.Results[i], "notes")
		}
		return nil
	})

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/logs?stream=true", nil), "alice")
	rec := httptest.NewRecorder()
	ch.ServeStreamingList(req.Context(), rec, req, []string{"id", "user_id", "notes"}, nil, nil, nil, 1, 10, nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("SECURITY: [stream-hook] direct ServeStreamingList with AfterList hooks returned %d, want 400 (List() refuses the same request; the exported method must not bypass the redaction chain it cannot run). Body: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "alice secret") {
		t.Fatalf("SECURITY: [stream-hook] AfterList redaction bypassed by direct streaming: stored value %q reached the wire", "alice secret")
	}
}
