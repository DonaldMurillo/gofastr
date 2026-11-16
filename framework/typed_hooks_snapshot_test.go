package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// A chan field makes json.Marshal of the whole value fail.
type unmarshalablePost struct {
	Title string             `json:"title,omitempty"`
	Meta  map[string]string  `json:"meta,omitempty"`
	Tags  []string           `json:"tags,omitempty"`
	Self  *unmarshalablePost `json:"self,omitempty"`
	Done  chan struct{}      `json:"done,omitempty"`
}

func TestTypedHookSnapshotIsDeepWhenTheValueCannotBeMarshalled(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeCreate[unmarshalablePost](app, "posts", func(_ context.Context, v *unmarshalablePost) error {
		v.Meta["role"] = "user"
		v.Tags[0] = "redacted"
		return nil
	})
	body := map[string]any{
		"id":   "1",
		"meta": map[string]any{"role": "admin"},
		"tags": []any{"secret", "b"},
	}
	if err := app.HookRegistry("posts").ExecuteHooks(context.Background(), hook.BeforeCreate, body); err != nil {
		t.Fatal(err)
	}
	if meta, _ := body["meta"].(map[string]any); meta["role"] != "user" {
		t.Errorf("in-place map write was dropped: meta = %v", body["meta"])
	}
	if tags, _ := body["tags"].([]any); len(tags) == 0 || tags[0] != "redacted" {
		t.Errorf("in-place slice write was dropped: tags = %v", body["tags"])
	}
}

func TestSnapshotDeepCopyFollowsPointerCycles(t *testing.T) {
	p := &unmarshalablePost{Title: "a", Meta: map[string]string{"k": "v"}}
	p.Self = p
	cp := snapshotDeepCopy(*p)
	if cp.Self == p || cp.Self.Self != cp.Self {
		t.Fatalf("cycle not copied as a cycle of new values")
	}
	cp.Meta["k"] = "changed"
	if p.Meta["k"] != "v" {
		t.Errorf("copy's map aliases the original")
	}
}
