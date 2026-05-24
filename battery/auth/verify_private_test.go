package auth

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// mapRegistry is a tiny entity.Registry for tests.
type mapRegistry struct {
	entities map[string]*entity.Entity
}

func newMapRegistry() *mapRegistry {
	return &mapRegistry{entities: map[string]*entity.Entity{}}
}
func (m *mapRegistry) Register(e *entity.Entity) error {
	if _, ok := m.entities[e.Config.Name]; ok {
		return fmt.Errorf("duplicate %q", e.Config.Name)
	}
	m.entities[e.Config.Name] = e
	return nil
}
func (m *mapRegistry) All() map[string]*entity.Entity { return m.entities }
func (m *mapRegistry) Get(name string) (*entity.Entity, error) {
	e, ok := m.entities[name]
	if !ok {
		return nil, fmt.Errorf("not found: %q", name)
	}
	return e, nil
}

// TestVerifyAuthEntitiesPrivate_FlagsDangerousConfig pins the half-measure
// fix: when a host wires auth.NewEntityUserStore against an entity that
// was registered with CRUD enabled, VerifyAuthEntitiesPrivate emits a
// loud WARN. Hosts can call this once at startup to catch the footgun
// the auto-private helpers were designed to prevent.
func TestVerifyAuthEntitiesPrivate_FlagsDangerousConfig(t *testing.T) {
	reg := newMapRegistry()
	crudOn := true
	ent := entity.Define("users", entity.EntityConfig{
		Fields: UserEntityFields(),
		CRUD:   &crudOn, // dangerous-old-pattern
		MCP:    true,
	})
	_ = reg.Register(ent)

	var sink bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&sink, &slog.HandlerOptions{Level: slog.LevelDebug}))
	VerifyAuthEntitiesPrivate(reg, "users", "", logger)

	out := sink.String()
	if !strings.Contains(out, "users") {
		t.Errorf("expected warning to name the offending entity; got: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "warn") {
		t.Errorf("expected WARN level; got: %q", out)
	}
	if !strings.Contains(out, "UserEntityConfig") {
		t.Errorf("expected migration hint pointing at UserEntityConfig; got: %q", out)
	}
}

// TestVerifyAuthEntitiesPrivate_SilentOnSafeConfig confirms the helper is
// silent for the recommended pattern (UserEntityConfig/SessionEntityConfig).
func TestVerifyAuthEntitiesPrivate_SilentOnSafeConfig(t *testing.T) {
	reg := newMapRegistry()
	ent := entity.Define("users", UserEntityConfig())
	_ = reg.Register(ent)
	sess := entity.Define("sessions", SessionEntityConfig())
	_ = reg.Register(sess)

	var sink bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&sink, &slog.HandlerOptions{Level: slog.LevelDebug}))
	VerifyAuthEntitiesPrivate(reg, "users", "sessions", logger)

	if sink.Len() > 0 {
		t.Errorf("safe config produced log noise: %q", sink.String())
	}
}

// TestVerifyAuthEntitiesPrivate_WarnsOnMissingEntity pins the visibility
// fix: calling the helper before app.Entity() shouldn't produce false
// confidence (silent pass when the entity isn't even registered).
// The helper now emits a WARN naming the missing entity so the host
// notices the call-order bug.
func TestVerifyAuthEntitiesPrivate_WarnsOnMissingEntity(t *testing.T) {
	reg := newMapRegistry()
	var sink bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&sink, &slog.HandlerOptions{Level: slog.LevelDebug}))
	VerifyAuthEntitiesPrivate(reg, "users", "", logger)

	out := sink.String()
	if !strings.Contains(out, "not registered yet") {
		t.Errorf("expected WARN about missing entity; got: %q", out)
	}
	if !strings.Contains(out, "users") {
		t.Errorf("WARN didn't name the entity: %q", out)
	}
}

// Suppress unused import for go vet when this file is the only one
// referencing schema.
var _ = schema.String
