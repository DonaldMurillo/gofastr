package crud

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestHTTPListsAnswerEncodeFailure(t *testing.T) {
	for _, query := range []string{"", "?cursor=&limit=2"} {
		t.Run(query, func(t *testing.T) {
			ch, db := setupHookableHandler(t)
			if _, err := db.Exec(`INSERT INTO notes (id, owner, body) VALUES ('n1', 'alice', 'one')`); err != nil {
				t.Fatal(err)
			}
			ch.Hooks.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
				data.(*hook.ListPayload).Results[0]["body"] = math.NaN()
				return nil
			})
			w := httptest.NewRecorder()
			ch.List()(w, withTestUser(httptest.NewRequest(http.MethodGet, "/notes"+query, nil), "alice"))
			if w.Code != http.StatusInternalServerError || !json.Valid(w.Body.Bytes()) {
				t.Fatalf("response = %d %q; want a JSON 500", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "NaN") {
				t.Fatalf("response leaks encode details: %s", w.Body.String())
			}
		})
	}
}
