package world_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gofastr/gofastr/kiln/world"
)

func TestNewIsCurrentSchema(t *testing.T) {
	w := world.New()
	if w.SchemaVersion != world.SchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", w.SchemaVersion, world.SchemaVersion)
	}
	if w.Entities == nil || w.Pages == nil {
		t.Fatalf("New must initialize Entities and Pages maps")
	}
}

func TestRoundTripJSON(t *testing.T) {
	timestamps := true
	crud := false
	maxLen := 200.0
	w := &world.World{
		SchemaVersion: world.SchemaVersion,
		App: world.AppConfig{
			Name:           "blog",
			JSONCase:       "snake",
			DebugEndpoints: true,
		},
		Entities: map[string]*world.Entity{
			"posts": {
				Name:        "posts",
				Table:       "posts",
				SoftDelete:  true,
				MultiTenant: true,
				Timestamps:  &timestamps,
				CRUD:        &crud,
				MCP:         true,
				Fields: []world.Field{
					{Name: "title", Type: "string", Required: true, Max: &maxLen},
					{Name: "status", Type: "enum", Values: []string{"draft", "published"}, Default: "draft"},
					{Name: "author_id", Type: "relation", To: "users"},
				},
				Relations: []world.Relation{
					{Name: "author", To: "users", Type: "belongs_to"},
				},
				Endpoints: []world.EntityEndpoint{
					{
						Method:      "POST",
						Path:        "/posts/{id}/publish",
						Name:        "publish_post",
						Description: "publish a draft",
						MCP:         true,
						Action: world.Action{
							Kind: world.ActionSetField,
							Params: map[string]any{
								"field": "status",
								"value": "published",
							},
						},
					},
				},
			},
		},
		Pages: map[string]*world.Page{
			"/": {
				Path:  "/",
				Title: "Home",
				Type:  "page",
				Tree: world.Node{
					Kind: "div",
					Props: map[string]any{"class": "container"},
					Children: []world.Node{
						{
							Kind:  "heading",
							Props: map[string]any{"level": float64(1), "text": "Posts"},
						},
						{
							Kind:    "button",
							Props:   map[string]any{"label": "New post"},
							Actions: map[string]world.Action{"click": {Kind: world.ActionRespondJSON, Params: map[string]any{"status": float64(204)}}},
						},
					},
					Bindings: map[string]string{"hidden": "user.role != 'admin'"},
				},
			},
		},
		Hooks: []*world.Hook{
			{
				ID:        "h1",
				Entity:    "posts",
				When:      "before_create",
				Condition: "ctx.user.role == 'author'",
				Action: world.Action{
					Kind: world.ActionValidate,
					Params: map[string]any{
						"expression": "len(entity.title) > 0",
						"message":    "title required",
					},
				},
			},
		},
		Routes: []*world.Route{
			{Method: "GET", Path: "/health", Action: world.Action{Kind: world.ActionRespondJSON, Params: map[string]any{"status": float64(200), "body": map[string]any{"ok": true}}}},
		},
		Seeds: []*world.Seed{
			{Entity: "posts", Rows: []map[string]any{{"title": "hello"}}},
		},
		Middleware: []*world.Middleware{
			{Name: "logger"},
			{Name: "cors", Cfg: map[string]any{"origins": []any{"*"}}},
		},
	}

	encoded, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded world.World
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(w, &decoded) {
		t.Fatalf("round-trip mismatch.\nwant: %#v\n got: %#v", w, &decoded)
	}
}

func TestEmptyWorldRoundTrip(t *testing.T) {
	w := world.New()
	encoded, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded world.World
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.SchemaVersion != world.SchemaVersion {
		t.Fatalf("SchemaVersion lost in round trip: %d", decoded.SchemaVersion)
	}
}
