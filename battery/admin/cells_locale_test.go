package admin

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework"
)

// The generation pair is in the reader's digits.
func TestGenerationCellUsesLocaleDigits(t *testing.T) {
	ar := i18n.WithContext(context.Background(), i18n.Locale{Tag: "ar-EG"})
	got := string(moduleGenerationCell(ar, framework.ProcessModuleInfo{DesiredGeneration: 3, ObservedGeneration: 3}))
	if !strings.Contains(got, "٣ / ٣") {
		t.Errorf("ar-EG generation = %q, want ٣ / ٣", got)
	}
}

// The attention panel skipped an empty view only when its count read
// "0", so under ar-EG (٠) every empty watch drew an empty preview
// instead of the all-clear line (2026-10-27 A35).
func TestAttentionClearUnderArabicDigits(t *testing.T) {
	x := ordersEnv(t, Config{Entities: []string{"orders"}, Attention: []Watch{{Entity: "orders", View: "late", Columns: []string{"ref"}}}})
	if _, err := x.db.Exec(`UPDATE orders SET status = 'paid'`); err != nil {
		t.Fatal(err)
	}
	h := x.as(theAdmin)
	ar := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(i18n.WithContext(r.Context(), i18n.Locale{Tag: "ar-EG"})))
	})
	if dash := get(ar, "/admin").Body.String(); strings.Contains(dash, `href="/admin/entities/orders?view=late"`) {
		t.Errorf("an empty watch drew its preview under ar-EG:\n%s", dash)
	}
}

// The hover time is in the reader's date format; the datetime
// attribute stays RFC 3339.
func TestAgoCellTitleUsesLocale(t *testing.T) {
	fr := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	at := time.Date(2026, 3, 5, 14, 5, 0, 0, time.UTC)
	got := string(agoCell(fr, at.Add(2*time.Hour), at))
	if !strings.Contains(got, "5 mars 2026") {
		t.Errorf("fr title has no French date:\n%s", got)
	}
	if !strings.Contains(got, `datetime="2026-03-05T14:05:00Z"`) {
		t.Errorf("datetime attribute is not RFC 3339:\n%s", got)
	}
}
