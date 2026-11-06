package framework

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

func TestViewEventsUsesAppBus(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	app := NewApp(WithDB(database), WithoutDefaultMiddleware())
	app.View(activeUsersView())
	ctx, cancel := context.WithCancel(handler.SetUser(context.Background(), "u1"))
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/active_users/_events", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("view events = %d %q: %s", response.Code, response.Header().Get("Content-Type"), response.Body)
	}
}
