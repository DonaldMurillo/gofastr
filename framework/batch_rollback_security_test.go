package framework

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// When a batch tx aborts at index N, earlier items' Data must NOT
// appear in the response. The constructed shape never landed in the DB;
// surfacing it tempts clients to read the (false) "succeeded" data
// without first checking Committed=false. Committed=false alone is the
// only authoritative signal of persistence.

func assertBatchScrubbed(t *testing.T, label string, resp crud.BatchResponse) {
	t.Helper()
	if resp.Committed {
		t.Fatalf("[%s] batch committed unexpectedly: %+v", label, resp)
	}
	for i, r := range resp.Results {
		if r.Data != nil {
			t.Fatalf("[%s] rolled-back item %d leaked success data: %+v", label, i, r)
		}
	}
}

func TestBatchCreate_RollbackScrubsData(t *testing.T) {
	runBatchTestWithApp(t, func(app *App) {
		app.HookRegistry("posts").RegisterHook(hook.BeforeCreate, func(_ context.Context, data any) error {
			if m, _ := data.(map[string]any); m["title"] == "reject" {
				return errors.New("policy reject")
			}
			return nil
		})
	}, func(t *testing.T, _ *sql.DB, ta *TestApp) {
		items := []map[string]any{
			{"title": "ok-0"},
			{"title": "ok-1"},
			{"title": "reject"},
			{"title": "ok-3"},
		}
		resp := ta.Post("/posts/_batch", map[string]any{"items": items})
		resp.AssertStatus(t, http.StatusBadRequest)
		assertBatchScrubbed(t, "create", decodeBatchResponse(t, resp.Body()))
	})
}

func TestBatchUpdate_RollbackScrubsData(t *testing.T) {
	runBatchTestWithApp(t, func(app *App) {
		app.HookRegistry("posts").RegisterHook(hook.BeforeUpdate, func(_ context.Context, data any) error {
			if m, _ := data.(map[string]any); m["title"] == "reject" {
				return errors.New("policy reject")
			}
			return nil
		})
	}, func(t *testing.T, db *sql.DB, ta *TestApp) {
		for i := 0; i < 4; i++ {
			if _, err := db.Exec("INSERT INTO posts(id, title) VALUES ($1, $2)", "p"+string(rune('1'+i)), "seed-"+string(rune('1'+i))); err != nil {
				t.Fatalf("seed row %d: %v", i, err)
			}
		}
		items := []map[string]any{
			{"id": "p1", "title": "updated-0"},
			{"id": "p2", "title": "updated-1"},
			{"id": "p3", "title": "reject"},
			{"id": "p4", "title": "updated-3"},
		}
		resp := ta.Request(http.MethodPatch, "/posts/_batch", nil).WithBody(map[string]any{"items": items}).Execute()
		resp.AssertStatus(t, http.StatusBadRequest)
		assertBatchScrubbed(t, "update", decodeBatchResponse(t, resp.Body()))
	})
}

func TestBatchDelete_RollbackScrubsData(t *testing.T) {
	runBatchTestWithApp(t, func(app *App) {
		app.HookRegistry("posts").RegisterHook(hook.BeforeDelete, func(_ context.Context, data any) error {
			if id, _ := data.(string); id == "p3" {
				return errors.New("protected")
			}
			return nil
		})
	}, func(t *testing.T, db *sql.DB, ta *TestApp) {
		for i := 0; i < 4; i++ {
			if _, err := db.Exec("INSERT INTO posts(id, title) VALUES ($1, $2)", "p"+string(rune('1'+i)), "seed-"+string(rune('1'+i))); err != nil {
				t.Fatalf("seed row %d: %v", i, err)
			}
		}
		resp := ta.Request(http.MethodDelete, "/posts/_batch", nil).WithBody(map[string]any{"ids": []string{"p1", "p2", "p3", "p4"}}).Execute()
		resp.AssertStatus(t, http.StatusBadRequest)
		assertBatchScrubbed(t, "delete", decodeBatchResponse(t, resp.Body()))
	})
}
