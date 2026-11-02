package live_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// A json_case: snake world answers its entity routes in snake_case
// (kiln/render sets AppConfig.JSONCase), so the spec Live serves at
// /openapi.json, which the in-studio API browser reads, must name the same
// keys and the same api_prefix'd paths. Live bypasses App.Start, and once
// built its spec with framework.EntityOpenAPI: camelCase keys and unprefixed
// paths whatever the world said.
func TestLiveOpenAPIFollowsSnakeCaseWorld(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-oacase")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	l, err := live.New(journal.NewMemory(), func() *framework.App {
		return framework.NewApp(framework.WithDB(d))
	})
	if err != nil {
		t.Fatal(err)
	}
	apply := func(id string, op journal.Op, payload any) {
		t.Helper()
		e, err := journal.NewEntry(id, time.Now(), journal.KindWorldEdit, op, payload)
		if err != nil {
			t.Fatal(err)
		}
		if err := l.Apply(e); err != nil {
			t.Fatalf("apply %s: %v", op, err)
		}
	}
	prev := l.Session().World.App
	next := prev
	next.JSONCase = "snake"
	apply("1", journal.OpSetAppConfig, journal.SetAppConfigPayload{Config: next, Prev: &prev})
	apply("2", journal.OpAddEntity, journal.AddEntityPayload{Entity: &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
			{Name: "author_name", Type: "string"},
		},
	}})

	do := func(method, path string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, req)
		return rec
	}
	created := do(http.MethodPost, "/api/posts", []byte(`{"title":"t","author_name":"ann"}`))
	if created.Code != http.StatusCreated && created.Code != http.StatusOK {
		t.Fatalf("POST /api/posts = %d %s", created.Code, created.Body)
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode row: %v (%s)", err, created.Body)
	}
	row := env.Data
	if row["author_name"] != "ann" {
		t.Fatalf("snake world's POST answered %s; want author_name (premise)", created.Body)
	}

	rec := do(http.MethodGet, "/openapi.json", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d", rec.Code)
	}
	var doc struct {
		Paths      map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Paths["/api/posts"]; !ok {
		paths := make([]string, 0, len(doc.Paths))
		for p := range doc.Paths {
			paths = append(paths, p)
		}
		slices.Sort(paths)
		t.Errorf("spec paths %v miss /api/posts, the route the world answers", paths)
	}
	var schemaKeys []string
	for name, s := range doc.Components.Schemas {
		if strings.EqualFold(name, "posts") || strings.EqualFold(name, "post") {
			for k := range s.Properties {
				schemaKeys = append(schemaKeys, k)
			}
		}
	}
	slices.Sort(schemaKeys)
	var rowKeys []string
	for k := range row {
		rowKeys = append(rowKeys, k)
	}
	slices.Sort(rowKeys)
	for _, k := range rowKeys {
		if !slices.Contains(schemaKeys, k) {
			t.Errorf("response key %q is not a property of the spec's posts schema %v", k, schemaKeys)
		}
	}
	for _, k := range schemaKeys {
		if !slices.Contains(rowKeys, k) {
			t.Errorf("spec property %q is not a key the route answers %v", k, rowKeys)
		}
	}
}
