package admin

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// capturingLogger builds a *slog.Logger that writes to buf at the given level
// so a test can assert that an operational warning (e.g. a failed audit write)
// actually reached a log sink instead of vanishing.
func capturingLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestRBAC_GrantAuditFailureLogged pins the audit-fire-and-forget fix: when
// the audit-row INSERT fails AFTER the grant already took effect, the failure
// must reach the log, a silent success here is a security gap (the mutation
// is durable but unrecorded). The grant itself must still succeed.
func TestRBAC_GrantAuditFailureLogged(t *testing.T) {
	b, h, policy, _, _, _ := rbacTestEnv(t)

	var buf bytes.Buffer
	// Point the audit sink at a table that does not exist so AppendAuditEvent
	// returns "no such table"; route the warning to a capturing logger.
	b.cfg.AuditTable = "definitely_not_a_table"
	b.cfg.Logger = capturingLogger(&buf)

	ctx := context.Background()
	c := access.WithRoles(access.WithPolicy(ctx, policy), []string{"editor"})
	if access.Can(c, "posts:write") {
		t.Fatal("expected Can=false before grant")
	}

	form := url.Values{"role": {"editor"}, "permission": {"posts:write"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/rbac/_grant", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("grant got %d, want 303; body=%s", rr.Code, rr.Body.String())
	}
	// The mutation MUST still take effect, failing the audit row does not roll
	// back the grant.
	if !access.Can(c, "posts:write") {
		t.Fatal("expected grant to apply even when audit write fails")
	}
	got := buf.String()
	if !strings.Contains(got, "grant") || !strings.Contains(got, "editor") {
		t.Fatalf("audit write failure was not logged; got=%q", got)
	}
}

// TestModules_EnableAuditFailureLogged applies the same policy to the
// process-module lifecycle handlers: a controller action that succeeded must
// not have its audit record vanish silently when the DB blips.
func TestModules_EnableAuditFailureLogged(t *testing.T) {
	fake := &fakeModuleController{
		list:    []framework.ProcessModuleInfo{moduleInfo("billing", framework.StateInstalledDisabled)},
		bumpGen: 1, revokeGen: 1,
	}
	b, h, _ := moduleTestEnv(t, fake)

	var buf bytes.Buffer
	b.cfg.AuditTable = "definitely_not_a_table"
	b.cfg.Logger = capturingLogger(&buf)

	form := url.Values{"module": {"billing"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/modules/_enable", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("enable got %d, want 303; body=%s", rr.Code, rr.Body.String())
	}
	if len(fake.enabled) != 1 || fake.enabled[0] != "billing" {
		t.Fatalf("controller Enable not called: %v", fake.enabled)
	}
	got := buf.String()
	if !strings.Contains(got, "module_enable") || !strings.Contains(got, "billing") {
		t.Fatalf("audit write failure was not logged; got=%q", got)
	}
}
