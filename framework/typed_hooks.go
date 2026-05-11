package framework

import (
	"context"
	"encoding/json"
)

// Typed hooks
//
// The framework's HookRegistry stores hooks as func(ctx, any) error to keep
// the declarative path (entity declarations from JSON) untyped. Generated
// typed repositories want hook callbacks that receive *T directly. The
// helpers below register an untyped wrapper that marshals the hook payload
// through *T and back, so existing untyped hooks and new typed ones can
// coexist on the same entity.
//
// Casing: hook payloads inside the framework are snake_cased map[string]any
// (because unconvertMapKeys runs before hooks fire). Generated structs use
// JSON tags in camelCase. The wrappers translate via mapToCamelCase /
// mapToSnakeCase so json.Marshal/Unmarshal round-trips correctly.

// OnBeforeCreate registers a typed BeforeCreate hook on the entity named
// `name`. Mutations the callback makes to *T are reflected back into the
// pending body so the subsequent INSERT picks them up.
func OnBeforeCreate[T any](app *App, name string, fn func(ctx context.Context, value *T) error) {
	app.HookRegistry(name).RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		var v T
		if err := unmarshalHookPayload(data, &v); err != nil {
			return err
		}
		if err := fn(ctx, &v); err != nil {
			return err
		}
		if m, ok := data.(map[string]any); ok {
			return mergeStructIntoMap(&v, m)
		}
		return nil
	})
}

// OnAfterCreate registers a typed AfterCreate hook. The callback receives
// the just-inserted row (already includes server-generated fields like id).
// Mutations are not reflected — Create has already committed the row's
// shape; modifying the struct is harmless but pointless.
func OnAfterCreate[T any](app *App, name string, fn func(ctx context.Context, value *T) error) {
	app.HookRegistry(name).RegisterHook(AfterCreate, func(ctx context.Context, data any) error {
		var v T
		if err := unmarshalHookPayload(data, &v); err != nil {
			return err
		}
		return fn(ctx, &v)
	})
}

// OnBeforeUpdate registers a typed BeforeUpdate hook. *T is sparse — it
// holds whatever the caller sent, not the full row.
func OnBeforeUpdate[T any](app *App, name string, fn func(ctx context.Context, value *T) error) {
	app.HookRegistry(name).RegisterHook(BeforeUpdate, func(ctx context.Context, data any) error {
		var v T
		if err := unmarshalHookPayload(data, &v); err != nil {
			return err
		}
		if err := fn(ctx, &v); err != nil {
			return err
		}
		if m, ok := data.(map[string]any); ok {
			return mergeStructIntoMap(&v, m)
		}
		return nil
	})
}

// OnAfterUpdate registers a typed AfterUpdate hook receiving the post-update
// row.
func OnAfterUpdate[T any](app *App, name string, fn func(ctx context.Context, value *T) error) {
	app.HookRegistry(name).RegisterHook(AfterUpdate, func(ctx context.Context, data any) error {
		var v T
		if err := unmarshalHookPayload(data, &v); err != nil {
			return err
		}
		return fn(ctx, &v)
	})
}

// OnBeforeDelete registers a typed BeforeDelete hook. The payload is the
// record id; no generic parameter needed.
func OnBeforeDelete(app *App, name string, fn func(ctx context.Context, id string) error) {
	app.HookRegistry(name).RegisterHook(BeforeDelete, func(ctx context.Context, data any) error {
		id, _ := data.(string)
		return fn(ctx, id)
	})
}

// OnAfterDelete registers a typed AfterDelete hook. Same shape as
// OnBeforeDelete.
func OnAfterDelete(app *App, name string, fn func(ctx context.Context, id string) error) {
	app.HookRegistry(name).RegisterHook(AfterDelete, func(ctx context.Context, data any) error {
		id, _ := data.(string)
		return fn(ctx, id)
	})
}

// unmarshalHookPayload converts the framework's hook payload (snake-cased
// map[string]any for create/update, string for delete) into a typed value.
// Maps are pre-converted to camelCase so json tags on generated structs line
// up.
func unmarshalHookPayload(data any, dest any) error {
	if m, ok := data.(map[string]any); ok {
		camel := mapToCamelCase(m)
		b, err := json.Marshal(camel)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, dest)
	}
	// Non-map payload (e.g. delete passes a string id) — round-trip through
	// JSON so callers can use *string or even custom typed wrappers.
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dest)
}

// mergeStructIntoMap reflects struct mutations from the typed Before-hook
// callback back into the snake-cased payload map. Only top-level fields are
// merged; nested maps are replaced wholesale.
func mergeStructIntoMap(src any, dest map[string]any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	var fresh map[string]any
	if err := json.Unmarshal(b, &fresh); err != nil {
		return err
	}
	snake := mapToSnakeCase(fresh)
	for k, v := range snake {
		dest[k] = v
	}
	return nil
}
