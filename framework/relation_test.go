package framework

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// --- Relation tests ---

func TestRelationHasOne(t *testing.T) {
	r := HasOne("profile", "profiles", "user_id")
	if r.Type != RelHasOne {
		t.Errorf("expected RelHasOne, got %d", r.Type)
	}
	if r.Name != "profile" {
		t.Errorf("expected name profile, got %s", r.Name)
	}
	if r.Entity != "profiles" {
		t.Errorf("expected entity profiles, got %s", r.Entity)
	}
	if r.ForeignKey != "user_id" {
		t.Errorf("expected FK user_id, got %s", r.ForeignKey)
	}
}

func TestRelationBelongsTo(t *testing.T) {
	r := BelongsTo("author", "users", "user_id")
	if r.Type != RelManyToOne {
		t.Errorf("expected RelManyToOne, got %d", r.Type)
	}
	if r.Name != "author" {
		t.Errorf("expected name author, got %s", r.Name)
	}
	if r.Entity != "users" {
		t.Errorf("expected entity users, got %s", r.Entity)
	}
	if r.ForeignKey != "user_id" {
		t.Errorf("expected FK user_id, got %s", r.ForeignKey)
	}
}

func TestRelationHasMany(t *testing.T) {
	r := HasMany("comments", "comments", "post_id")
	if r.Type != RelHasMany {
		t.Errorf("expected RelHasMany, got %d", r.Type)
	}
	if r.Name != "comments" {
		t.Errorf("expected name comments, got %s", r.Name)
	}
	if r.Entity != "comments" {
		t.Errorf("expected entity comments, got %s", r.Entity)
	}
	if r.ForeignKey != "post_id" {
		t.Errorf("expected FK post_id, got %s", r.ForeignKey)
	}
}

func TestRelationManyToMany(t *testing.T) {
	r := ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id")
	if r.Type != RelManyToMany {
		t.Errorf("expected RelManyToMany, got %d", r.Type)
	}
	if r.Name != "tags" {
		t.Errorf("expected name tags, got %s", r.Name)
	}
	if r.Entity != "tags" {
		t.Errorf("expected entity tags, got %s", r.Entity)
	}
	if r.Through != "post_tags" {
		t.Errorf("expected through post_tags, got %s", r.Through)
	}
	if r.LocalKey != "post_id" {
		t.Errorf("expected localKey post_id, got %s", r.LocalKey)
	}
	if r.ForeignKeyTarget != "tag_id" {
		t.Errorf("expected foreignKeyTarget tag_id, got %s", r.ForeignKeyTarget)
	}
}

// --- Hook tests ---

func TestHookRegistrationAndExecution(t *testing.T) {
	hr := NewHookRegistry()
	var order []string

	hr.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		order = append(order, "first")
		return nil
	})
	hr.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		order = append(order, "second")
		return nil
	})
	hr.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		order = append(order, "third")
		return nil
	})

	err := hr.ExecuteHooks(context.Background(), BeforeCreate, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 hooks called, got %d", len(order))
	}
	if order[0] != "first" || order[1] != "second" || order[2] != "third" {
		t.Errorf("hooks not called in registration order: %v", order)
	}
}

func TestHookStopsOnFirstError(t *testing.T) {
	hr := NewHookRegistry()
	var order []string

	hr.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		order = append(order, "first")
		return nil
	})
	hr.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		order = append(order, "second")
		return errors.New("hook failed")
	})
	hr.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		order = append(order, "third")
		return nil
	})

	err := hr.ExecuteHooks(context.Background(), BeforeCreate, nil)
	if err == nil {
		t.Fatal("expected error from ExecuteHooks")
	}
	if err.Error() != "hook failed" {
		t.Errorf("unexpected error message: %s", err.Error())
	}

	if len(order) != 2 {
		t.Fatalf("expected 2 hooks called before stop, got %d", len(order))
	}
	if order[0] != "first" || order[1] != "second" {
		t.Errorf("hooks not stopped at correct point: %v", order)
	}
}

func TestHookCanModifyData(t *testing.T) {
	hr := NewHookRegistry()

	hr.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		if m, ok := data.(map[string]any); ok {
			m["modified"] = true
		}
		return nil
	})

	data := map[string]any{"name": "test"}
	err := hr.ExecuteHooks(context.Background(), BeforeCreate, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if modified, ok := data["modified"].(bool); !ok || !modified {
		t.Error("hook did not modify data")
	}
}

func TestHookDifferentTypes(t *testing.T) {
	hr := NewHookRegistry()
	var called HookType

	hr.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		called = BeforeCreate
		return nil
	})
	hr.RegisterHook(AfterCreate, func(ctx context.Context, data any) error {
		called = AfterCreate
		return nil
	})

	_ = hr.ExecuteHooks(context.Background(), BeforeCreate, nil)
	if called != BeforeCreate {
		t.Errorf("expected BeforeCreate hook, got %d", called)
	}

	_ = hr.ExecuteHooks(context.Background(), AfterCreate, nil)
	if called != AfterCreate {
		t.Errorf("expected AfterCreate hook, got %d", called)
	}
}

// --- Validator tests ---

func TestValidatorCollectsAllErrors(t *testing.T) {
	vr := NewValidationRegistry()

	vr.RegisterValidator(func(ctx context.Context, data map[string]any) map[string]string {
		errs := make(map[string]string)
		if val, ok := data["name"].(string); !ok || val == "" {
			errs["name"] = "is required"
		}
		return errs
	})

	vr.RegisterValidator(func(ctx context.Context, data map[string]any) map[string]string {
		errs := make(map[string]string)
		if val, ok := data["email"].(string); !ok || val == "" {
			errs["email"] = "is required"
		}
		return errs
	})

	vr.RegisterValidator(func(ctx context.Context, data map[string]any) map[string]string {
		errs := make(map[string]string)
		if val, ok := data["age"].(int); !ok || val < 18 {
			errs["age"] = "must be at least 18"
		}
		return errs
	})

	errs := vr.Validate(context.Background(), map[string]any{})

	if len(errs) != 3 {
		t.Fatalf("expected 3 errors, got %d: %v", len(errs), errs)
	}
	if errs["name"] != "is required" {
		t.Errorf("expected name error, got %q", errs["name"])
	}
	if errs["email"] != "is required" {
		t.Errorf("expected email error, got %q", errs["email"])
	}
	if errs["age"] != "must be at least 18" {
		t.Errorf("expected age error, got %q", errs["age"])
	}
}

func TestValidatorNoErrorsWhenValid(t *testing.T) {
	vr := NewValidationRegistry()
	vr.RegisterValidator(Required("name", "email"))

	data := map[string]any{
		"name":  "Alice",
		"email": "alice@example.com",
	}

	errs := vr.Validate(context.Background(), data)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestRequiredValidatorDetectsMissingFields(t *testing.T) {
	vr := NewValidationRegistry()
	vr.RegisterValidator(Required("name", "email", "age"))

	// Missing all fields
	errs := vr.Validate(context.Background(), map[string]any{})
	if len(errs) != 3 {
		t.Fatalf("expected 3 errors, got %d", len(errs))
	}
	for _, field := range []string{"name", "email", "age"} {
		if errs[field] != "is required" {
			t.Errorf("expected %q to be required, got %q", field, errs[field])
		}
	}
}

func TestRequiredValidatorDetectsEmptyStrings(t *testing.T) {
	vr := NewValidationRegistry()
	vr.RegisterValidator(Required("name"))

	errs := vr.Validate(context.Background(), map[string]any{
		"name": "",
	})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if errs["name"] != "is required" {
		t.Errorf("expected 'is required', got %q", errs["name"])
	}
}

func TestRequiredValidatorDetectsNilValues(t *testing.T) {
	vr := NewValidationRegistry()
	vr.RegisterValidator(Required("field"))

	errs := vr.Validate(context.Background(), map[string]any{
		"field": nil,
	})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for nil value, got %d", len(errs))
	}
}

func TestUniqueValidator(t *testing.T) {
	vr := NewValidationRegistry()
	vr.RegisterValidator(Unique("email", func(ctx context.Context, value any) bool {
		// Simulate: "taken@example.com" already exists
		return value != "taken@example.com"
	}))

	// Not taken — no errors
	errs := vr.Validate(context.Background(), map[string]any{
		"email": "new@example.com",
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}

	// Taken — error
	errs = vr.Validate(context.Background(), map[string]any{
		"email": "taken@example.com",
	})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if errs["email"] != "must be unique" {
		t.Errorf("expected 'must be unique', got %q", errs["email"])
	}
}

func TestCustomValidator(t *testing.T) {
	vr := NewValidationRegistry()
	vr.RegisterValidator(Custom("password_strength", func(ctx context.Context, data map[string]any) map[string]string {
		errs := make(map[string]string)
		pw, _ := data["password"].(string)
		if len(pw) < 8 {
			errs["password"] = "must be at least 8 characters"
		}
		return errs
	}))

	errs := vr.Validate(context.Background(), map[string]any{
		"password": "short",
	})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if errs["password"] != "must be at least 8 characters" {
		t.Errorf("unexpected error: %q", errs["password"])
	}
}

func TestFormatValidationErrors(t *testing.T) {
	errs := map[string]string{
		"name":  "is required",
		"email": "must be unique",
	}
	formatted := FormatValidationErrors(errs)
	if len(formatted) != 2 {
		t.Fatalf("expected 2 formatted errors, got %d", len(formatted))
	}
	for _, msg := range formatted {
		if !strings.Contains(msg, "is required") && !strings.Contains(msg, "must be unique") {
			t.Errorf("unexpected formatted error: %s", msg)
		}
	}
}

// --- EagerLoad edge case tests ---

func TestEagerLoadEmptyIDs(t *testing.T) {
	entity := Define("users", EntityConfig{Table: "users"})
	relations := []Relation{HasMany("posts", "posts", "user_id")}

	result, err := EagerLoad(context.Background(), nil, entity, relations, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestEagerLoadEmptyRelations(t *testing.T) {
	entity := Define("users", EntityConfig{Table: "users"})

	result, err := EagerLoad(context.Background(), nil, entity, nil, []string{"1", "2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With no relations, EagerLoad returns empty map (nothing to load)
	if len(result) != 0 {
		t.Errorf("expected 0 entries with no relations, got %d", len(result))
	}
}

func TestEntityTableName(t *testing.T) {
	e := Define("users", EntityConfig{})
	if e.GetTable() != "users" {
		t.Errorf("expected table 'users', got %s", e.GetTable())
	}
}

func TestHooksForEmpty(t *testing.T) {
	hr := NewHookRegistry()
	hooks := hr.HooksFor(BeforeCreate)
	if len(hooks) != 0 {
		t.Errorf("expected 0 hooks, got %d", len(hooks))
	}
}

func TestValidatorsCount(t *testing.T) {
	vr := NewValidationRegistry()
	if vr.Validators() != 0 {
		t.Errorf("expected 0 validators, got %d", vr.Validators())
	}
	vr.RegisterValidator(Required("name"))
	if vr.Validators() != 1 {
		t.Errorf("expected 1 validator, got %d", vr.Validators())
	}
}

func TestUniqueValidatorMissingField(t *testing.T) {
	vr := NewValidationRegistry()
	vr.RegisterValidator(Unique("email", func(ctx context.Context, value any) bool {
		return true
	}))

	// Field not present — should not error
	errs := vr.Validate(context.Background(), map[string]any{})
	if len(errs) != 0 {
		t.Errorf("expected no errors for missing field, got %v", errs)
	}
}

func TestFormatValidationErrorsEmpty(t *testing.T) {
	result := FormatValidationErrors(nil)
	if result != nil {
		t.Errorf("expected nil for empty errors, got %v", result)
	}
	result = FormatValidationErrors(map[string]string{})
	if result != nil {
		t.Errorf("expected nil for empty map, got %v", result)
	}
}

// --- Integration: Hooks + Validators together ---

func TestHooksAndValidatorsIntegration(t *testing.T) {
	vr := NewValidationRegistry()
	vr.RegisterValidator(Required("title"))

	hr := NewHookRegistry()
	hr.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		if m, ok := data.(map[string]any); ok {
			m["slug"] = fmt.Sprintf("%v-slug", m["title"])
		}
		return nil
	})

	data := map[string]any{
		"title": "My Post",
	}

	// Validate first
	errs := vr.Validate(context.Background(), data)
	if len(errs) != 0 {
		t.Fatalf("validation failed: %v", errs)
	}

	// Then run hooks
	err := hr.ExecuteHooks(context.Background(), BeforeCreate, data)
	if err != nil {
		t.Fatalf("hook failed: %v", err)
	}

	if data["slug"] != "My Post-slug" {
		t.Errorf("hook did not set slug, got %v", data["slug"])
	}
}
