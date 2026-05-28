package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

type errBrowsableQueue struct {
	err error
}

func (e errBrowsableQueue) ListJobs(ctx context.Context, status string, limit int) ([]queue.Job, error) {
	return nil, e.err
}

func (e errBrowsableQueue) Stats(ctx context.Context) (queue.JobStats, error) {
	return queue.JobStats{}, nil
}

func TestAdmin_IndexRequiresAuthentication(t *testing.T) {
	h := mountAdminBare(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [admin] unauthenticated /admin returned %d. Attack: admin overview exposed without auth.", rr.Code)
	}
}

func TestAdmin_QueuePageRequiresAuthentication(t *testing.T) {
	h := mountAdminBare(t, Config{Queue: errBrowsableQueue{}})
	req := httptest.NewRequest(http.MethodGet, "/admin/queue", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [admin] unauthenticated /admin/queue returned %d. Attack: queue dashboard exposed without auth.", rr.Code)
	}
}

func TestAdmin_AuditPageRequiresAuthentication(t *testing.T) {
	db := newDB(t)
	newAuditTable(t, db)
	h := mountAdminBare(t, Config{DB: db})
	req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [admin] unauthenticated /admin/audit returned %d. Attack: audit dashboard exposed without auth.", rr.Code)
	}
}

func TestAdmin_QueueErrorDoesNotLeakInternalText(t *testing.T) {
	b := New(Config{Queue: errBrowsableQueue{err: errors.New("dial tcp 10.0.0.5:5432 password=secret")}})
	req := httptest.NewRequest(http.MethodGet, "/admin/queue", nil)
	rr := httptest.NewRecorder()
	b.handleQueue(rr, req)

	body := rr.Body.String()
	if strings.Contains(body, "10.0.0.5") || strings.Contains(body, "password=secret") {
		t.Fatalf("SECURITY: [admin] queue page leaked internal error text: %q", body)
	}
}

func TestAdmin_AuditErrorDoesNotLeakInternalText(t *testing.T) {
	db := newDB(t)
	_ = db.Close()
	b := New(Config{DB: db})
	req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	rr := httptest.NewRecorder()
	b.handleAudit(rr, req)

	body := rr.Body.String()
	if strings.Contains(strings.ToLower(body), "database is closed") || strings.Contains(strings.ToLower(body), "sql:") {
		t.Fatalf("SECURITY: [admin] audit page leaked internal DB error text: %q", body)
	}
}

func TestAdmin_ResponseCarriesFrameDenyHeader(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("SECURITY: [admin] admin page missing X-Frame-Options DENY: %#v", rr.Header())
	}
}

func TestAdmin_ResponseCarriesContentSecurityPolicy(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("SECURITY: [admin] admin page missing Content-Security-Policy header: %#v", rr.Header())
	}
}

func TestAdmin_ResponseCarriesNoSniffHeader(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [admin] admin page missing X-Content-Type-Options nosniff: %#v", rr.Header())
	}
}

func TestAdmin_ResponseCarriesReferrerPolicy(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Referrer-Policy") == "" {
		t.Fatalf("SECURITY: [admin] admin page missing Referrer-Policy header: %#v", rr.Header())
	}
}

var _ queue.Browsable = errBrowsableQueue{}
var _ = sql.ErrNoRows
