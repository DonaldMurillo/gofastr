package appgen

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// An entity_list the load accepts draws: whatever `fields:` and `filter:`
// the app spec loader lets through, the generated builder chain must be
// one entityui renders, not a list answering "Couldn't load this
// section" on every request.
//
// Each case loads a spec (decode + ValidateAppSpec), renders the app
// with the generator, reads the appUI.List(...) builder chain out of the
// generated screen file, replays its Columns and Where on a real
// entityui.UI over the spec's entities (in-process SQLite, one seeded
// row), and renders it.

const listDrawsEntities = `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: posts
    crud: true
    public: true
    display:
      fields:
        internal_note:
          omit: true
    fields:
      - name: title
        type: string
      - name: author_name
        type: string
      - name: is_featured
        type: bool
      - name: meta
        type: json
      - name: internal_note
        type: string
      - name: secret
        type: string
        hidden: true
screens:
  - name: home
    route: /
    body:
      - kind: entity_list
        entity: posts
`

// renderGeneratedList returns the load error, or the generated builder
// chain, the list's HTML and what entityui logged while drawing.
func renderGeneratedList(t *testing.T, listKeys string) (loadErr error, expr, html, logs string) {
	t.Helper()
	node, err := coreyaml.Parse(listDrawsEntities + listKeys)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bp, err := appspec.DecodeAppSpec(node)
	if err != nil {
		return err, "", "", ""
	}
	if err := appspec.ValidateAppSpec(bp); err != nil {
		return err, "", "", ""
	}
	files, err := RenderAppSpecCode(bp)
	if err != nil {
		return err, "", "", ""
	}
	for _, f := range files {
		if m := regexp.MustCompile(`appUI\.List\([^\n]*`).FindString(f.Content); m != "" {
			expr = m
		}
	}
	if expr == "" {
		t.Fatal("the generator emitted no appUI.List chain")
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	for _, decl := range bp.Entities {
		cfg, err := decl.Config()
		if err != nil {
			t.Fatal(err)
		}
		app.Entity(decl.Name, cfg)
	}
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	ui := app.EntityUI(entityui.Extensions{})
	h, err := app.CrudHandler("posts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.CreateOne(context.Background(), map[string]any{"title": "Hello row", "authorName": "Ann", "isFeatured": true, "meta": 5}); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	// Replay the generated chain's Columns and Where.
	b := ui.List("posts")
	if m := regexp.MustCompile(`\.Columns\(([^)]*)\)`).FindStringSubmatch(expr); m != nil {
		var cols []string
		for _, c := range strings.Split(m[1], ", ") {
			cols = append(cols, strings.Trim(c, `"`))
		}
		b = b.Columns(cols...)
	}
	if m := regexp.MustCompile(`\.Where\("([^"]*)", "([^"]*)"\)`).FindStringSubmatch(expr); m != nil {
		b = b.Where(m[1], m[2])
	}
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)
	ctx := appui.WithRequest(context.Background(), httptest.NewRequest("GET", "/", nil))
	return nil, expr, string(b.RenderCtx(ctx)), strings.TrimSpace(buf.String())
}

func listDrew(html string) bool {
	return strings.Contains(html, "Hello row") && !strings.Contains(html, "fui-callout--danger")
}

// The specs the load accepts draw. A field's camelCase JSON spelling is
// one of them, in `fields:` as in `filter:`: the generator emits the
// declared column, the name entityui resolves by. The bug: `fields:
// [authorName]` went into .Columns(...) as written, and the whole list
// failed with "not a visible field".
func TestGeneratedEntityListDraws(t *testing.T) {
	for name, tc := range map[string]struct{ keys, chain string }{
		"declared names":    {"        fields: [title, author_name]\n", `.Columns("title", "author_name")`},
		"camelCase field":   {"        fields: [title, authorName]\n", `.Columns("title", "author_name")`},
		"generated id":      {"        fields: [id, title]\n", `.Columns("id", "title")`},
		"pinned":            {"        fields: [title]\n        filter: is_featured=true\n", `.Where("is_featured", "true")`},
		"pinned, camelCase": {"        fields: [title]\n        filter: isFeatured=true\n", `.Where("is_featured", "true")`},
	} {
		t.Run(name, func(t *testing.T) {
			loadErr, expr, html, logs := renderGeneratedList(t, tc.keys)
			if loadErr != nil {
				t.Fatalf("the load refused the list: %v", loadErr)
			}
			if !strings.Contains(expr, tc.chain) {
				t.Errorf("generated chain %s does not hold %s", expr, tc.chain)
			}
			if !listDrew(html) {
				t.Fatalf("the load accepted the list, and the generated list never draws.\n  generated: %s\n  entityui:  %s", expr, logs)
			}
		})
	}
}

// The specs entityui could never draw are refused at load, naming the
// entry: a hidden field, a field the entity's display omits, one listed
// twice (under either spelling), and a pin on a json field. Each of these
// loaded before, and the generated list failed on every render.
func TestEntityListThatCannotDrawIsRefusedAtLoad(t *testing.T) {
	for name, tc := range map[string]struct{ keys, want string }{
		"hidden field":         {"        fields: [title, secret]\n", "hidden"},
		"omitted field":        {"        fields: [title, internal_note]\n", "omitted"},
		"repeated field":       {"        fields: [title, title]\n", "twice"},
		"repeated, camelCase":  {"        fields: [author_name, authorName]\n", "twice"},
		"pin on a json column": {"        fields: [title]\n        filter: meta=5\n", "json"},
	} {
		t.Run(name, func(t *testing.T) {
			loadErr, expr, html, logs := renderGeneratedList(t, tc.keys)
			if loadErr == nil {
				t.Fatalf("the load accepted the list (drew=%v).\n  generated: %s\n  entityui:  %s", listDrew(html), expr, logs)
			}
			if !strings.Contains(loadErr.Error(), tc.want) || !strings.Contains(loadErr.Error(), "entity_list") {
				t.Fatalf("the refusal does not say why (%q): %v", tc.want, loadErr)
			}
		})
	}
}
