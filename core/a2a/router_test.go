package a2a

import (
	"context"
	"testing"
)

func routerSkills() []Skill {
	return []Skill{
		{ID: "echo", Handler: func(context.Context, TaskContext) error { return nil }},
		{ID: "other", Handler: func(context.Context, TaskContext) error { return nil }},
	}
}

// TestRouterMetadataSkill pins rule 1: metadata["skill"] wins.
func TestRouterMetadataSkill(t *testing.T) {
	msg := &Message{Metadata: map[string]any{"skill": "other"}}
	got, err := DefaultRouter(context.Background(), msg, routerSkills())
	if err != nil || got != "other" {
		t.Fatalf("got %q err %v, want other", got, err)
	}
}

// TestRouterMetadataUnknownSkill pins that an unknown id errors naming
// the known ids.
func TestRouterMetadataUnknownSkill(t *testing.T) {
	msg := &Message{Metadata: map[string]any{"skill": "zzz"}}
	_, err := DefaultRouter(context.Background(), msg, routerSkills())
	if err == nil {
		t.Fatal("unknown metadata skill must error")
	}
	want := `no skill named "zzz"; available: echo, other`
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}

// TestRouterDataPartSkill pins rule 2: the first data part carrying a
// string "skill" key routes, and non-string or non-object data does
// not.
func TestRouterDataPartSkill(t *testing.T) {
	v := any(map[string]any{"n": 1, "skill": "other"})
	msg := &Message{Parts: []Part{
		TextPart("hello"),
		{Data: &v},
	}}
	got, err := DefaultRouter(context.Background(), msg, routerSkills())
	if err != nil || got != "other" {
		t.Fatalf("got %q err %v, want other", got, err)
	}

	str := any("just a string")
	noRoute := &Message{Parts: []Part{{Data: &str}}}
	if _, err := DefaultRouter(context.Background(), noRoute, routerSkills()); err == nil {
		t.Fatal("a string data payload must not route")
	}
}

// TestRouterDataPartUnknownSkill errors like rule 1's unknown id.
func TestRouterDataPartUnknownSkill(t *testing.T) {
	v := any(map[string]any{"skill": "zzz"})
	msg := &Message{Parts: []Part{{Data: &v}}}
	_, err := DefaultRouter(context.Background(), msg, routerSkills())
	if err == nil {
		t.Fatal("unknown data-part skill must error")
	}
	if want := `no skill named "zzz"; available: echo, other`; err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}

// TestRouterSingleSkill pins rule 3: exactly one registered skill is
// the default.
func TestRouterSingleSkill(t *testing.T) {
	got, err := DefaultRouter(context.Background(), &Message{}, routerSkills()[:1])
	if err != nil || got != "echo" {
		t.Fatalf("got %q err %v, want echo", got, err)
	}
}

// TestRouterMissNamesSkills pins rule 4's exact message.
func TestRouterMissNamesSkills(t *testing.T) {
	_, err := DefaultRouter(context.Background(), &Message{Parts: []Part{TextPart("hi")}}, routerSkills())
	if err == nil {
		t.Fatal("ambiguous message must error")
	}
	want := "no skill named; available: echo, other"
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}
