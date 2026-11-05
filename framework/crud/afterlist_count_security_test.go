package crud

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

var listCountChanges = []string{"drop", "drop-all", "append", "append-empty"}

func resizingListHandler(t *testing.T, change string) *CrudHandler {
	t.Helper()
	handler, database := setupHookableHandler(t)
	if change != "append-empty" {
		if _, err := database.Exec(`INSERT INTO notes (id, owner, body) VALUES
			('n1', 'alice', 'one'), ('n2', 'alice', 'two'), ('n3', 'bob', 'three')`); err != nil {
			t.Fatal(err)
		}
	}
	var calls int
	handler.Hooks.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
		calls++
		payload := data.(*hook.ListPayload)
		switch change {
		case "drop":
			payload.Results = payload.Results[:len(payload.Results)-1]
		case "drop-all":
			payload.Results = nil
		default:
			payload.Results = append(payload.Results, map[string]any{"id": "extra", "owner": "alice", "body": "added"})
		}
		return nil
	})
	t.Cleanup(func() {
		if calls != 1 {
			t.Errorf("AfterList ran %d times, want 1", calls)
		}
	})
	return handler
}

func checkHTTPListCount(t *testing.T, query string) {
	t.Helper()
	for _, change := range listCountChanges {
		t.Run(change, func(t *testing.T) {
			handler := resizingListHandler(t, change)
			var logs bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previous) })
			response := httptest.NewRecorder()
			request := withTestUser(httptest.NewRequest(http.MethodGet, "/notes"+query, nil), "alice")
			handler.List()(response, request)
			if response.Code != http.StatusInternalServerError {
				t.Errorf("row-count change answered %d, want 500: %s", response.Code, response.Body.String())
			}
			if !strings.Contains(logs.String(), "after-list") || !strings.Contains(logs.String(), "row count") {
				t.Errorf("log does not name hook phase and row-count refusal: %s", logs.String())
			}
			if strings.Contains(response.Body.String(), `"data"`) || strings.Contains(response.Body.String(), "row count") {
				t.Errorf("refused hook returned rows or internal diagnostics: %s", response.Body.String())
			}
		})
	}
}

func TestHTTPListRejectsRowCountChange(t *testing.T) {
	checkHTTPListCount(t, "")
}

func TestCursorRejectsRowCountChange(t *testing.T) {
	checkHTTPListCount(t, "?cursor=&limit=2")
}

func TestListAllRejectsRowCountChange(t *testing.T) {
	for _, change := range listCountChanges {
		t.Run(change, func(t *testing.T) {
			handler := resizingListHandler(t, change)
			rows, err := handler.ListAll(WithReadHooks(context.Background()), ListOptions{})
			if err == nil || !strings.Contains(err.Error(), "after-list") || !strings.Contains(err.Error(), "row count") || rows != nil {
				t.Fatalf("ListAll returned rows=%v, error=%v; want row-count refusal", rows, err)
			}
		})
	}
}

func TestTypedFindRejectsRowCountChange(t *testing.T) {
	for _, change := range listCountChanges {
		t.Run(change, func(t *testing.T) {
			handler := resizingListHandler(t, change)
			rows, err := NewTypedQuery[struct{ ID string }](handler).Find(WithReadHooks(context.Background()))
			if err == nil || !strings.Contains(err.Error(), "after-list") || !strings.Contains(err.Error(), "row count") || rows != nil {
				t.Fatalf("Find returned rows=%v, error=%v; want row-count refusal", rows, err)
			}
		})
	}
}
