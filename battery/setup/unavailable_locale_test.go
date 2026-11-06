package setup

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func TestSetupUnavailableUsesLocale(t *testing.T) {
	done := false
	handler := buildTestRunner(t, true, &done).Handler(func() {}, nil, nil)
	for _, locale := range []string{"en", "fr", "ar"} {
		t.Run(locale, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/app", nil)
			ctx := request.Context()
			want := "Service is in first-run setup mode.\nComplete setup at /setup to enable this service.\n"
			if locale != "en" {
				catalog := i18n.NewMapCatalog()
				catalog.Set(locale, "setup.unavailable", i18n.Message{Text: "setup " + locale})
				catalog.Set(locale, "setup.unavailable_help", i18n.Message{Text: "visit /setup " + locale})
				ctx = i18n.WithTranslator(ctx, i18n.NewTranslator(catalog, "en"))
				ctx = i18n.WithContext(ctx, i18n.Locale{Tag: locale})
				want = "setup " + locale + "\nvisit /setup " + locale + "\n"
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request.WithContext(ctx))
			if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "60" {
				t.Fatalf("unavailable response = %d %v", response.Code, response.Header())
			}
			if got := response.Body.String(); got != want {
				t.Fatalf("locale %s body = %q, want %q", locale, got, want)
			}
		})
	}
}
