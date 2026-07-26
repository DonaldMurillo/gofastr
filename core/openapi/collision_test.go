package openapi

import "testing"

// TestAddSchemaPanicsOnDuplicateName pins that registering the same
// schema component name twice surfaces a panic instead of silently
// overwriting. Before the fix, a versioned entity whose computed
// component name matched another entity's name would silently clobber
// the first schema, leaving $ref pointing at the wrong type.
func TestAddSchemaPanicsOnDuplicateName(t *testing.T) {
	s := NewSpec("T", "1")
	s.AddSchema("Posts", map[string]any{"type": "object"})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("AddSchema with a duplicate name did not panic")
		}
	}()
	s.AddSchema("Posts", map[string]any{"type": "string"})
}

// TestAddPathPanicsOnDuplicateMethod pins that registering the same
// (method, path) pair twice surfaces a panic. Different methods on the
// same path are fine (they land in different keys of the path item).
func TestAddPathPanicsOnDuplicateMethod(t *testing.T) {
	s := NewSpec("T", "1")
	s.AddPath("GET", "/posts", *NewOperation())
	// A different method on the same path must NOT panic.
	s.AddPath("POST", "/posts", *NewOperation())
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("AddPath with a duplicate method+path did not panic")
		}
	}()
	s.AddPath("GET", "/posts", *NewOperation())
}
