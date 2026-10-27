package framework

import (
	"context"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

type embedSecretFields struct {
	Secret string `json:"secret"`
}

type embedRow struct {
	embedSecretFields
	Title string `json:"title"`
}

type EmbedSecrets struct {
	Secret string `json:"secret"`
}

type embedOptRow struct {
	EmbedSecrets `json:",omitempty"`
	Title        string `json:"title"`
}

type embedPtrRow struct {
	*EmbedSecrets
	Title string `json:"title"`
}

// A tag with options and no name still promotes, as encoding/json does.
func TestTypedHookTagOptionsPromote(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeCreate[embedOptRow](app, "rows", func(_ context.Context, v *embedOptRow) error {
		v.Secret = ""
		return nil
	})
	body := map[string]any{"title": "t", "secret": "s3cr3t"}
	if err := app.HookRegistry("rows").ExecuteHooks(context.Background(), hook.BeforeCreate, body); err != nil {
		t.Fatal(err)
	}
	if body["secret"] != "" {
		t.Errorf("secret = %v, want the hook's empty value", body["secret"])
	}
	if _, ok := body["EmbedSecrets"]; ok {
		t.Errorf("merge added a key named after the embedded type: %v", body)
	}
}

type shadowRow struct {
	EmbedSecrets
	Secret string `json:"secret"`
}

type tieA struct {
	Note string
}

type tieB struct {
	Note string
}

type tieRow struct {
	tieA
	tieB
	Title string `json:"title"`
}

type taggedNote struct {
	Note string `json:"Note"`
}

type untaggedNote struct {
	Note string
}

type profileFields struct {
	Secret string `json:"secret"`
}

// An unexported embedded struct with a tag name is a named field in
// JSON, not skipped as unexported.
type profileRow struct {
	profileFields `json:"profile"`
}

type HookCount int

// JSON ignores a tag name it finds invalid and keeps the Go name.
type badTagRow struct {
	HookCount `json:"bad\\name"`
}

// A tagged unexported embed and an invalid tag name map to the key JSON
// uses.
func TestTypedHookJSONNamesMatch(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeCreate[profileRow](app, "profiles", func(_ context.Context, v *profileRow) error {
		v.profileFields.Secret = ""
		return nil
	})
	OnBeforeCreate[badTagRow](app, "counts", func(_ context.Context, v *badTagRow) error {
		v.HookCount = 3
		return nil
	})
	p := map[string]any{"profile": map[string]any{"secret": "leak"}}
	if err := app.HookRegistry("profiles").ExecuteHooks(context.Background(), hook.BeforeCreate, p); err != nil {
		t.Fatal(err)
	}
	if prof, _ := p["profile"].(map[string]any); prof == nil || prof["secret"] != "" {
		t.Errorf("profile = %v, want the hook's emptied secret", p["profile"])
	}
	c := map[string]any{}
	if err := app.HookRegistry("counts").ExecuteHooks(context.Background(), hook.BeforeCreate, c); err != nil {
		t.Fatal(err)
	}
	if n, ok := c["hook_count"].(int64); !ok || n != 3 || len(c) != 1 {
		t.Errorf("payload = %#v, want hook_count 3 under the Go name", c)
	}
}

// At one depth the field tagged with the name wins over an untagged one.
type tagWinsRow struct {
	untaggedNote
	taggedNote
}

// The merge writes only the fields JSON encodes. An outer field shadows
// the embedded one of the same name, and two embedded fields tied at one
// depth cancel out, so a hook writing the hidden field changes nothing
// the row stores.
func TestTypedHookShadowedFieldIgnored(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeCreate[shadowRow](app, "rows", func(_ context.Context, v *shadowRow) error {
		v.EmbedSecrets.Secret = "admin"
		return nil
	})
	OnBeforeCreate[shadowRow](app, "outer", func(_ context.Context, v *shadowRow) error {
		v.Secret = "set"
		return nil
	})
	outer := map[string]any{"secret": "user"}
	if err := app.HookRegistry("outer").ExecuteHooks(context.Background(), hook.BeforeCreate, outer); err != nil {
		t.Fatal(err)
	}
	if outer["secret"] != "set" {
		t.Errorf("outer secret = %v, want the hook's set", outer["secret"])
	}
	OnBeforeCreate[tieRow](app, "ties", func(_ context.Context, v *tieRow) error {
		v.tieA.Note = "hidden"
		return nil
	})
	body := map[string]any{"secret": "user"}
	if err := app.HookRegistry("rows").ExecuteHooks(context.Background(), hook.BeforeCreate, body); err != nil {
		t.Fatal(err)
	}
	if body["secret"] != "user" {
		t.Errorf("secret = %v, want the outer field's user", body["secret"])
	}
	ties := map[string]any{"title": "t"}
	if err := app.HookRegistry("ties").ExecuteHooks(context.Background(), hook.BeforeCreate, ties); err != nil {
		t.Fatal(err)
	}
	if _, ok := ties["note"]; ok {
		t.Errorf("a tied embedded field reached the payload: %v", ties)
	}
	OnBeforeCreate[tagWinsRow](app, "tagwins", func(_ context.Context, v *tagWinsRow) error {
		v.taggedNote.Note = "tagged"
		return nil
	})
	tw := map[string]any{"Note": "n"}
	if err := app.HookRegistry("tagwins").ExecuteHooks(context.Background(), hook.BeforeCreate, tw); err != nil {
		t.Fatal(err)
	}
	if tw["Note"] != "tagged" {
		t.Errorf("Note = %v, want the tagged field's write", tw["Note"])
	}
}

// Two fields tagged with one name at one depth cancel out, as in JSON.
// vet refuses the duplicate tag in source, so the type is built here.
func TestJSONFieldsTaggedTieDropped(t *testing.T) {
	str := reflect.TypeOf("")
	typ := reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: str, Tag: `json:"note"`},
		{Name: "B", Type: str, Tag: `json:"note"`},
		{Name: "Title", Type: str, Tag: `json:"title"`},
	})
	for _, f := range jsonFields(typ) {
		if f.name == "note" {
			t.Errorf("jsonFields kept tied field %v", f.index)
		}
	}
}

// A hook that drops an embedded pointer clears the fields it promoted.
func TestTypedHookNilEmbedClears(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeCreate[embedPtrRow](app, "rows", func(_ context.Context, v *embedPtrRow) error {
		v.EmbedSecrets = nil
		return nil
	})
	body := map[string]any{"title": "t", "secret": "s3cr3t"}
	if err := app.HookRegistry("rows").ExecuteHooks(context.Background(), hook.BeforeCreate, body); err != nil {
		t.Fatal(err)
	}
	if body["secret"] != "" {
		t.Errorf("secret = %v, want it cleared with the embedded pointer", body["secret"])
	}
}

// A hook's write to a field promoted from an embedded struct lands on
// that field's own key, as JSON promotes it, never on a key named
// after the embedded type.
func TestTypedHookEmbeddedFieldMerges(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeCreate[embedRow](app, "rows", func(_ context.Context, v *embedRow) error {
		v.Secret = ""
		return nil
	})
	body := map[string]any{"title": "t", "secret": "s3cr3t"}
	if err := app.HookRegistry("rows").ExecuteHooks(context.Background(), hook.BeforeCreate, body); err != nil {
		t.Fatal(err)
	}
	if body["secret"] != "" {
		t.Errorf("secret = %v, want the hook's empty value", body["secret"])
	}
	for k := range body {
		if k != "title" && k != "secret" {
			t.Errorf("merge added key %q", k)
		}
	}
}
