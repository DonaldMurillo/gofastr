package admin

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestAdminListRejectsRowCountChange(t *testing.T) {
	for _, change := range []string{"drop", "append"} {
		t.Run(change, func(t *testing.T) {
			fixture, handler := parityEnv(t)
			var calls int
			fixture.app.HookRegistry("cases").RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
				calls++
				payload := data.(*hook.ListPayload)
				if change == "append" {
					payload.Results = append(payload.Results, payload.Results[0])
				} else {
					payload.Results = nil
				}
				return nil
			})
			response := serve(handler, preq(http.MethodGet, "/admin/entities/cases", ""))
			if calls == 0 || !strings.Contains(response.Body.String(), "See server logs.") {
				t.Fatalf("admin accepted %s results instead of showing its load refusal (hook calls %d)", change, calls)
			}
		})
	}
}
