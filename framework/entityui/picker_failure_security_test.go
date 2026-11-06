package entityui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func TestPickerReportsReadFailures(t *testing.T) {
	for _, failure := range []string{"store", "hook"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newInvoiceUI(t)
			handler, err := fixture.host.Crud(mustEntity(t, fixture, "customers"))
			if err != nil {
				t.Fatal(err)
			}
			if failure == "store" {
				if err := fixture.db.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				handler.Hooks = hook.NewHookRegistry()
				handler.Hooks.RegisterHook(hook.BeforeList, func(context.Context, any) error {
					return errors.New("private database address")
				})
			}
			for _, query := range []string{"", "Acme", "another customer"} {
				request := httptest.NewRequest(http.MethodPost, "/api/invoices/_pick?field=customer_id", strings.NewReader(`{"q":"`+query+`"}`))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				fixture.ui.PickerHandler("invoices").ServeHTTP(response, request.WithContext(fixture.userCtx(request.URL.Path, "", "u1")))
				if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"error"`) {
					t.Errorf("query %q: status=%d body=%s, want structured 500", query, response.Code, response.Body)
				}
				if strings.Contains(response.Body.String(), "private database address") || response.Header().Get("Cache-Control") != "no-store" {
					t.Errorf("query %q: failure leaked details or was cacheable", query)
				}
			}
			if failure == "hook" {
				body := string(fixture.ui.Create("invoices").Base("/rec/invoices").RenderCtx(fixture.userCtx("/rec/invoices/create", "", "u1")))
				if !strings.Contains(body, render.Escape(i18nui.EntitySlotFailed.English())) {
					t.Error("initial relation picker silently rendered empty options after a read failure")
				}
			}
		})
	}
}
