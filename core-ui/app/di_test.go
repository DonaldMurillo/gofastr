package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// =============================================================================
// DI Container behavioral tests
// =============================================================================
//
// What DI should do from a user's perspective:
//   1. Register a constructor → resolve returns its output (singleton)
//   2. Register a value → resolve returns that exact value
//   3. Resolve always returns the same instance (singleton guarantee)
//   4. Inject fills struct fields tagged `inject:""` from the container
//   5. Inject skips fields with no provider (zero value stays)
//   6. Resolve fails cleanly when no provider exists
//   7. Provide rejects bad inputs (nil, multi-return funcs)
//   8. Concurrent Provide/Resolve is safe
// =============================================================================

// --- Test service types ---

type Logger struct {
	Prefix string
}

type Database struct {
	DSN string
}

type UserService struct {
	Log *Logger   `inject:""`
	DB  *Database `inject:""`
}

type Config struct {
	Env string
}

// --- 1. Constructor registration ---

func TestDI_ConstructorReturnsSingleton(t *testing.T) {
	c := NewContainer()
	callCount := 0
	err := c.Provide(func() *Logger {
		callCount++
		return &Logger{Prefix: "test"}
	})
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}

	var l1, l2 *Logger
	if err := c.Resolve(&l1); err != nil {
		t.Fatalf("Resolve 1: %v", err)
	}
	if err := c.Resolve(&l2); err != nil {
		t.Fatalf("Resolve 2: %v", err)
	}

	if l1 != l2 {
		t.Error("Resolve should return the same singleton instance")
	}
	if callCount != 1 {
		t.Errorf("constructor should be called exactly once, called %d times", callCount)
	}
}

func TestDI_ConstructorOutputUsedAsValue(t *testing.T) {
	c := NewContainer()
	err := c.Provide(func() *Database {
		return &Database{DSN: "postgres://localhost:5432/testdb"}
	})
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}

	var db *Database
	if err := c.Resolve(&db); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if db.DSN != "postgres://localhost:5432/testdb" {
		t.Errorf("expected DSN from constructor, got %q", db.DSN)
	}
}

// --- 2. Direct value registration ---

func TestDI_DirectValueResolve(t *testing.T) {
	c := NewContainer()
	log := &Logger{Prefix: "direct"}
	err := c.Provide(log)
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}

	var resolved *Logger
	if err := c.Resolve(&resolved); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved != log {
		t.Error("resolved value should be the exact same pointer as provided")
	}
}

func TestDI_DirectValueIsSingleton(t *testing.T) {
	c := NewContainer()
	cfg := &Config{Env: "production"}
	c.Provide(cfg)

	var c1, c2 *Config
	c.Resolve(&c1)
	c.Resolve(&c2)

	if c1 != c2 {
		t.Error("direct value should resolve to same instance every time")
	}
}

// --- 3. Inject struct fields ---

func TestDI_InjectFillsTaggedFields(t *testing.T) {
	c := NewContainer()
	c.Provide(&Logger{Prefix: "svc"})
	c.Provide(&Database{DSN: "postgres://localhost/db"})

	svc := &UserService{}
	if err := c.Inject(svc); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	if svc.Log == nil {
		t.Error("Log field should be injected")
	}
	if svc.DB == nil {
		t.Error("DB field should be injected")
	}
	if svc.Log.Prefix != "svc" {
		t.Errorf("injected Log.Prefix = %q, want %q", svc.Log.Prefix, "svc")
	}
	if svc.DB.DSN != "postgres://localhost/db" {
		t.Errorf("injected DB.DSN = %q, want %q", svc.DB.DSN, "postgres://localhost/db")
	}
}

func TestDI_InjectSkipsUntaggedFields(t *testing.T) {
	type Mixed struct {
		Log  *Logger `inject:""`
		Name string  // NOT tagged — should stay empty
	}

	c := NewContainer()
	c.Provide(&Logger{Prefix: "mixed"})

	m := &Mixed{Name: "original"}
	c.Inject(m)

	if m.Log == nil || m.Log.Prefix != "mixed" {
		t.Error("tagged field should be injected")
	}
	if m.Name != "original" {
		t.Errorf("untagged field should not be touched, got %q", m.Name)
	}
}

func TestDI_InjectErrorsOnMissingProvider(t *testing.T) {
	type Partial struct {
		Log *Logger   `inject:""`
		DB  *Database `inject:""` // no provider for this
	}

	c := NewContainer()
	c.Provide(&Logger{Prefix: "partial"})

	p := &Partial{}
	err := c.Inject(p)
	if err == nil {
		t.Fatal("Inject should return error when a provider is missing")
	}
	if !strings.Contains(err.Error(), "no provider") {
		t.Errorf("error should mention 'no provider', got %q", err.Error())
	}

	// The first field should still be nil since injection stops on error
	// (or it may be set depending on implementation — both are acceptable)
}

// --- 4. Error handling ---

func TestDI_ResolveUnregisteredType(t *testing.T) {
	c := NewContainer()
	var db *Database
	err := c.Resolve(&db)
	if err == nil {
		t.Error("resolving unregistered type should return an error")
	}
	if !strings.Contains(err.Error(), "no provider") {
		t.Errorf("error should mention 'no provider', got %q", err.Error())
	}
}

func TestDI_ProvideNil(t *testing.T) {
	c := NewContainer()
	err := c.Provide(nil)
	if err == nil {
		t.Error("providing nil should return an error")
	}
}

func TestDI_ProvideMultiReturnFunc(t *testing.T) {
	c := NewContainer()
	err := c.Provide(func() (*Logger, error) { return &Logger{}, nil })
	if err == nil {
		t.Error("constructor returning 2 values should be rejected")
	}
	if !strings.Contains(err.Error(), "exactly one value") {
		t.Errorf("error should mention 'exactly one value', got %q", err.Error())
	}
}

func TestDI_ResolveNonPointer(t *testing.T) {
	c := NewContainer()
	err := c.Resolve("not a pointer")
	if err == nil {
		t.Error("resolving non-pointer should return an error")
	}
}

func TestDI_ResolveNilPointer(t *testing.T) {
	c := NewContainer()
	var db *Database // nil
	err := c.Resolve(db)
	if err == nil {
		t.Error("resolving nil pointer should return an error")
	}
}

func TestDI_InjectNonStruct(t *testing.T) {
	c := NewContainer()
	x := 42
	err := c.Inject(&x)
	if err == nil {
		t.Error("injecting into non-struct pointer should return an error")
	}
}

func TestDI_InjectNilPointer(t *testing.T) {
	c := NewContainer()
	var svc *UserService // nil
	err := c.Inject(svc)
	if err == nil {
		t.Error("injecting nil pointer should return an error")
	}
}

// --- 5. App-level convenience methods ---

func TestDI_AppProvideAndResolve(t *testing.T) {
	app := NewApp("test")
	err := app.Provide(func() *Config {
		return &Config{Env: "testing"}
	})
	if err != nil {
		t.Fatalf("App.Provide: %v", err)
	}

	var cfg *Config
	if err := app.Container.Resolve(&cfg); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Env != "testing" {
		t.Errorf("expected Env='testing', got %q", cfg.Env)
	}
}

func TestDI_AppInject(t *testing.T) {
	app := NewApp("test")
	app.Provide(&Logger{Prefix: "app"})
	app.Provide(&Database{DSN: "test://db"})

	svc := &UserService{}
	if err := app.Inject(svc); err != nil {
		t.Fatalf("App.Inject: %v", err)
	}
	if svc.Log == nil || svc.Log.Prefix != "app" {
		t.Error("App.Inject should fill tagged fields from App's container")
	}
	if svc.DB == nil || svc.DB.DSN != "test://db" {
		t.Error("App.Inject should fill DB field from App's container")
	}
}

// --- 6. Interface-based DI ---

type Store interface {
	Get(key string) string
}

type MemoryStore struct {
	data map[string]string
}

func (m *MemoryStore) Get(key string) string {
	return m.data[key]
}

func TestDI_InterfaceRegistration(t *testing.T) {
	c := NewContainer()
	store := &MemoryStore{data: map[string]string{"foo": "bar"}}
	c.Provide(func() Store { return store })

	var s Store
	if err := c.Resolve(&s); err != nil {
		t.Fatalf("Resolve interface: %v", err)
	}
	if s.Get("foo") != "bar" {
		t.Error("resolved interface should work through concrete implementation")
	}
}

func TestDI_InterfaceSingleton(t *testing.T) {
	c := NewContainer()
	callCount := 0
	c.Provide(func() Store {
		callCount++
		return &MemoryStore{data: map[string]string{"k": "v"}}
	})

	var s1, s2 Store
	c.Resolve(&s1)
	c.Resolve(&s2)

	if s1 != s2 {
		t.Error("interface resolution should be singleton")
	}
	if callCount != 1 {
		t.Errorf("factory called %d times, want 1", callCount)
	}
}

// --- 7. Constructor that returns interface ---

type Greeter interface {
	Greet(name string) string
}

type FormalGreeter struct{}

func (FormalGreeter) Greet(name string) string { return "Good day, " + name }

type ServiceWithGreeter struct {
	G Greeter `inject:""`
}

func TestDI_InjectInterfaceField(t *testing.T) {
	c := NewContainer()
	c.Provide(func() Greeter { return FormalGreeter{} })

	svc := &ServiceWithGreeter{}
	if err := c.Inject(svc); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if svc.G.Greet("World") != "Good day, World" {
		t.Error("injected interface field should dispatch to concrete implementation")
	}
}

// --- 8. Real-world pattern: services depending on each other ---

type CartRepository struct {
	Store Store `inject:""`
}

type CartService struct {
	Repo *CartRepository `inject:""`
	Log  *Logger         `inject:""`
}

func TestDI_NestedInjection(t *testing.T) {
	c := NewContainer()
	c.Provide(func() Store { return &MemoryStore{data: map[string]string{}} })
	c.Provide(&Logger{Prefix: "cart"})

	// Inject fills only the top-level struct's fields.
	// Nested injection requires explicit Resolve calls.
	repo := &CartRepository{}
	c.Inject(repo) // fills repo.Store

	if repo.Store == nil {
		t.Error("nested struct field should be injected via Inject")
	}

	svc := &CartService{}
	c.Inject(svc) // fills svc.Log but NOT svc.Repo (it's nil)

	// Repo needs to be explicitly provided or manually set
	c.Provide(repo)
	c.Inject(svc)

	if svc.Log == nil {
		t.Error("top-level inject field should be filled")
	}
}

// --- 9. Value types (not just pointers) ---

func TestDI_NonPointerValue(t *testing.T) {
	c := NewContainer()
	c.Provide(Config{Env: "staging"})

	var cfg Config
	if err := c.Resolve(&cfg); err != nil {
		t.Fatalf("Resolve value type: %v", err)
	}
	if cfg.Env != "staging" {
		t.Errorf("got Env=%q, want %q", cfg.Env, "staging")
	}
}

// --- 10. Constructor returning value type ---

func TestDI_ConstructorReturningValueType(t *testing.T) {
	c := NewContainer()
	c.Provide(func() Config {
		return Config{Env: "from-constructor"}
	})

	var cfg Config
	if err := c.Resolve(&cfg); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Env != "from-constructor" {
		t.Errorf("got %q, want %q", cfg.Env, "from-constructor")
	}
}

// --- 11. Error type in constructor ---

func TestDI_ConstructorReturningErrorType(t *testing.T) {
	c := NewContainer()
	c.Provide(func() error {
		return errors.New("test error")
	})

	var err error
	if resolveErr := c.Resolve(&err); resolveErr != nil {
		t.Fatalf("Resolve: %v", resolveErr)
	}
	if err.Error() != "test error" {
		t.Errorf("got %q, want %q", err.Error(), "test error")
	}
}

// --- 12. fmt.Stringer interface resolution ---

func TestDI_FmtStringer(t *testing.T) {
	c := NewContainer()
	c.Provide(func() fmt.Stringer {
		return stringer("hello")
	})

	var s fmt.Stringer
	if err := c.Resolve(&s); err != nil {
		t.Fatalf("Resolve fmt.Stringer: %v", err)
	}
	if s.String() != "hello" {
		t.Errorf("got %q, want %q", s.String(), "hello")
	}
}

type stringer string

func (s stringer) String() string { return string(s) }
