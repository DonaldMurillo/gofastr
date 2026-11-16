package framework

import (
	"context"
	"reflect"
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

type cyclicHolder struct {
	Data  map[string]any
	Items []any
	Done  chan struct{}
}

func TestSnapshotDeepCopyEndsMapAndSliceCycles(t *testing.T) {
	m := map[string]any{}
	m["self"] = m
	s := make([]any, 1)
	s[0] = s
	cp := snapshotDeepCopy(cyclicHolder{Data: m, Items: s})
	inner, _ := cp.Data["self"].(map[string]any)
	if inner == nil || reflect.ValueOf(inner).Pointer() != reflect.ValueOf(cp.Data).Pointer() || reflect.ValueOf(cp.Data).Pointer() == reflect.ValueOf(m).Pointer() {
		t.Errorf("map cycle not copied as a cycle of the new map")
	}
	innerS, _ := cp.Items[0].([]any)
	if len(innerS) != 1 || reflect.ValueOf(innerS).Pointer() != reflect.ValueOf(cp.Items).Pointer() || reflect.ValueOf(cp.Items).Pointer() == reflect.ValueOf(s).Pointer() {
		t.Errorf("slice cycle not copied as a cycle of the new slice")
	}
}

type pairAtOneAddress struct{ X int }

type sharedAddressHolder struct {
	P    *pairAtOneAddress
	X    *int
	Done chan struct{}
}

func TestSnapshotDeepCopyKeysPointersByType(t *testing.T) {
	p := &pairAtOneAddress{X: 7}
	cp := snapshotDeepCopy(sharedAddressHolder{P: p, X: &p.X})
	if cp.P == p || cp.X == &p.X || cp.P.X != 7 || *cp.X != 7 {
		t.Fatalf("pointers sharing an address were not copied each to its own type")
	}
}

type embeddedMeta struct {
	Meta map[string]string `json:"meta,omitempty"`
}

type embeddingPost struct {
	embeddedMeta
	Done chan struct{} `json:"done,omitempty"`
}

func TestTypedHookSnapshotCopiesUnexportedEmbeddedStructs(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeCreate[embeddingPost](app, "posts", func(_ context.Context, v *embeddingPost) error {
		v.Meta["role"] = "user"
		return nil
	})
	body := map[string]any{"id": "1", "meta": map[string]any{"role": "admin"}}
	if err := app.HookRegistry("posts").ExecuteHooks(context.Background(), hook.BeforeCreate, body); err != nil {
		t.Fatal(err)
	}
	if meta, _ := body["meta"].(map[string]any); meta["role"] != "user" {
		t.Errorf("in-place write under an unexported embedded struct was dropped: meta = %v", body["meta"])
	}
}
