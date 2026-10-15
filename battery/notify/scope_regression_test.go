package notify

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type scopeChannel struct{ calls atomic.Int32 }

func (c *scopeChannel) Name() string { return "log" }
func (c *scopeChannel) Send(context.Context, Notification, Rendered) error {
	c.calls.Add(1)
	return nil
}
func TestRouterSelectionValidated(t *testing.T) {
	for _, names := range [][]string{{"missing"}, {"log", "log"}} {
		c := &scopeChannel{}
		n := New(WithChannel(c), WithRouter(func(string, Recipient, []string) []string { return names }))
		e := n.Send(context.Background(), Notification{Data: map[string]any{"_rendered": Rendered{}}})
		if names[0] == "missing" {
			if !errors.Is(e, ErrNoChannels) {
				t.Fatalf("unknown channel: %v", e)
			}
		} else if c.calls.Load() != 1 {
			t.Fatalf("duplicate sends=%d", c.calls.Load())
		}
	}
}
func TestTemplaterOwnsExtraMaps(t *testing.T) {
	m := NewMapTemplater()
	extra := map[string]any{"x": "old"}
	m.Set("a", "log", Template{Extra: extra})
	extra["x"] = "new"
	v, _ := m.Render(context.Background(), "a", "log", nil)
	if v.Extra["x"] != "old" {
		t.Fatal(v.Extra)
	}
	v.Extra["x"] = "bad"
	v, _ = m.Render(context.Background(), "a", "log", nil)
	if v.Extra["x"] != "old" {
		t.Fatal(v.Extra)
	}
}
