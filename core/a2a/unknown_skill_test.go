package a2a

import (
	"context"
	"testing"
	"time"
)

// The server's own unknown-skill refusals list no registered ids either
// (2026-10-27 bug bash P24): a custom Router that names a skill the
// server does not have, and a paused task resumed after its skill went
// away.

func otherSkill(c *Config) {
	c.Skills = append(c.Skills, Skill{ID: "other", Name: "Other", Handler: func(context.Context, TaskContext) error { return nil }})
}

func TestCustomRouterUnknownSkillRejected(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		otherSkill(c)
		c.Router = func(context.Context, *Message, []Skill) (string, error) { return "ghost", nil }
	})
	task := h.send("alice")
	if task.Status.State != TaskStateRejected {
		t.Fatalf("state = %s, want REJECTED", task.Status.State)
	}
	if msg := statusText(task); msg != `no skill named "ghost"` {
		t.Fatalf("message = %q", msg)
	}
}

func TestResumeWithoutSkillRejected(t *testing.T) {
	h := newHarness(t, otherSkill)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		return tc.RequireInput(TextPart("more?"))
	})
	id := h.send("alice").ID
	h.waitTask("alice", id, TaskStateInputRequired, time.Second)
	// The skill the task was routed to is gone, as after a redeploy that
	// dropped it with the store kept.
	delete(h.srv.byID, "echo")
	task := h.sendWithTask("alice", id, "here")
	if task.Status.State != TaskStateRejected {
		t.Fatalf("state = %s, want REJECTED", task.Status.State)
	}
	if msg := statusText(task); msg != `no skill named "echo"` {
		t.Fatalf("message = %q", msg)
	}
}
