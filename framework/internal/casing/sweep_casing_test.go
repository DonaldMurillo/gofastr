package casing

import "testing"

func TestConversionsAndMappings(t *testing.T) {
	for in, want := range map[string]string{"": "", "id": "id", "created_at": "createdAt", "a__b": "aB", "x_1": "x1"} {
		if got := ToCamel(in); got != want {
			t.Errorf("ToCamel(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"": "", "ID": "i_d", "createdAt": "created_at", "Name": "name"} {
		if got := ToSnake(in); got != want {
			t.Errorf("ToSnake(%q) = %q, want %q", in, got, want)
		}
	}
	if got := MapToCamel(map[string]any{"author_id": 1}); got["authorId"] != 1 || len(got) != 1 {
		t.Errorf("MapToCamel = %v", got)
	}
	if got := MapToSnake(map[string]any{"authorId": 1}); got["author_id"] != 1 || len(got) != 1 {
		t.Errorf("MapToSnake = %v", got)
	}
	mapping := PrecomputeMapping([]string{"author_id"})
	got := ApplyMapping(map[string]any{"author_id": 1, "extra_col": 2}, mapping)
	if got["authorId"] != 1 || got["extra_col"] != 2 || len(got) != 2 {
		t.Errorf("ApplyMapping = %v, want mapped keys converted and others passed through", got)
	}
}
