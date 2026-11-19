package chat_test

import "testing"

// Over HTTP an empty body is world_get's zero args, while every other
// args-taking tool refuses it; undo and reset_session never read the body.
func TestDispatchEmptyBody(t *testing.T) {
	l, _ := setup(t)
	if res := toolPost(t, l, "world_get", ""); !res.OK {
		t.Errorf("world_get with an empty body: %+v", res)
	}
	if res := toolPost(t, l, "undo", "not json"); res.Error != "nothing to undo" {
		t.Errorf("undo read its body: %+v", res)
	}
}
