package auth

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestAuditSeesEveryVersion proves the auth
// exposure audit goes silent on exactly the app it is meant to protect.
//
// VerifyAuthEntitiesPrivate (battery/auth/verify_private.go:35) resolves the
// users/sessions entity with reg.Get(name). Registry.Get was changed in this
// release range to return an AMBIGUITY ERROR when a name is registered under
// several versions and none is unversioned — the shape App.GroupEntity
// produces. The audit reads that error as "not registered yet", logs a
// misleading "call AFTER app.Entity(...)" hint, and SKIPS the check.
//
// So a host that mounts its users table at /api/v1 and /api/v2 with auto-CRUD
// left on gets no warning that its password_hash column is behind a public
// CRUD route — from the one function whose entire job is to say so. The
// operator is additionally sent to fix a registration-order problem that does
// not exist.
//
// Run:
//
//	go test ./battery/auth/ -run TestAuditSeesEveryVersion -v
func TestAuditSeesEveryVersion(t *testing.T) {
	crudOn := true
	mkUsers := func(version string) *entity.Entity {
		e := entity.Define("zzr2_users", entity.EntityConfig{Table: "zzr2_users",
			Fields: []schema.Field{
				{Name: "email", Type: schema.String},
				{Name: "password_hash", Type: schema.String},
			}, Exposure: &entity.ExposureConfig{
				// explicitly exposed — exactly what the audit must catch
				CRUD: &crudOn},
		}.WithTimestamps(false))
		e.Version = version
		return e
	}

	reg := framework.NewRegistry()
	if err := reg.Register(mkUsers("/api/v1")); err != nil {
		t.Fatalf("register v1: %v", err)
	}
	if err := reg.Register(mkUsers("/api/v2")); err != nil {
		t.Fatalf("register v2: %v", err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	VerifyAuthEntitiesPrivate(reg, "zzr2_users", "", logger)

	out := buf.String()
	if !strings.Contains(out, "exposed via auto-CRUD/MCP") {
		t.Fatalf("the auth exposure audit never fired for a CRUD-exposed users table mounted under two API versions.\nlogged instead:\n%s", out)
	}
}
