package entityui

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// getExport fetches the invoices export with query as ctx's caller.
func getExport(t *testing.T, x *testUI, ctx context.Context, query string) (*httptest.ResponseRecorder, [][]string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/invoices/_export.csv?"+query, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	x.ui.ExportHandler("invoices").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return rec, nil
	}
	rows, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("export is not CSV: %v\n%s", err, rec.Body.String())
	}
	return rec, rows
}

// A cell a spreadsheet would run as a formula gets a leading quote; the
// masked (NoQuery) token never appears.
func TestExportDefusesFormulasAndDropsNoQuery(t *testing.T) {
	rows := invoiceRows()
	rows["invoices"] = append(rows["invoices"],
		map[string]any{"id": "inv-2", "number": "=HYPERLINK(\"http://x\")", "memo": "+1", "status": "draft"},
		map[string]any{"id": "inv-3", "number": "@SUM(A1)", "memo": "-2", "status": "draft"})
	ents := invoiceEntities()
	ents["invoices"].Display.Fields["memo"] = entity.FieldDisplay{Label: "=Memo"}
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	rec, got := getExport(t, x, bulkCtx("u1", nil), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("Content-Type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control %q", cc)
	}
	body := rec.Body.String()
	if strings.Contains(body, "tok-secret") {
		t.Fatalf("SECURITY: the NoQuery token reached the export:\n%s", body)
	}
	for _, row := range got {
		for _, c := range row {
			if c != "" && strings.ContainsRune("=+-@", rune(c[0])) {
				t.Errorf("cell %q starts with a formula character", c)
			}
		}
	}
	if !strings.Contains(body, `'=HYPERLINK`) || !strings.Contains(body, "'@SUM") || !strings.Contains(body, "'+1") {
		t.Fatalf("formula cells not defused:\n%s", body)
	}
	if len(got) != 4 {
		t.Fatalf("%d lines, want a header and 3 rows", len(got))
	}
}

// The export follows the list's narrowing and the caller's owner scope.
func TestExportFollowsQueryAndScope(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	_, got := getExport(t, x, bulkCtx("u1", nil), "")
	if len(got) != 3 {
		t.Fatalf("u1 export has %d lines, want a header and u1's 2 rows: %v", len(got), got)
	}
	for _, row := range got[1:] {
		if strings.HasPrefix(row[0], "b") {
			t.Fatalf("SECURITY: u2's row %v reached u1's export", row)
		}
	}
	_, got = getExport(t, x, bulkCtx("u1", nil), "filter=number+%3D+%22A-2%22")
	if len(got) != 2 || got[1][0] != "a2" {
		t.Fatalf("filtered export = %v, want only a2", got)
	}
}

// An anonymous caller on a default-posture entity gets a refusal, not
// rows.
func TestExportRefusesAnonymous(t *testing.T) {
	x := newTestUI(t, invoiceEntities(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	rec, _ := getExport(t, x, context.Background(), "")
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "INV-1") {
		t.Fatalf("anonymous export: %d %s", rec.Code, rec.Body.String())
	}
}

// A filter the list refuses is refused for the export too.
func TestExportBadFilterRefused(t *testing.T) {
	x := newTestUI(t, invoiceEntities(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	rec, _ := getExport(t, x, bulkCtx("u1", nil), "filter=token+%3D+%22tok-secret%22")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
}
