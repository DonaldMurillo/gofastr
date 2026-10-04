package localentity

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/localdb"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

func f64(v float64) *float64 { return &v }

func mustPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected a panic containing %q", want)
		}
		if msg, _ := r.(string); !strings.Contains(msg, want) {
			t.Fatalf("panic = %v, want it to contain %q", r, want)
		}
	}()
	fn()
}

var (
	testDB  = localdb.New("localentity-test")
	members = Define(testDB, "members", []schema.Field{
		{Name: "nickname", Type: schema.String, Required: true, Max: f64(20)},
		{Name: "level", Type: schema.Int, Min: f64(1), Max: f64(100)},
		{Name: "kind", Type: schema.Enum, Values: []string{"a", "b"}, Default: "a"},
		{Name: "shiny", Type: schema.Bool},
	}, Indexed("level"), MaxRecords(6), WithMessages(Messages{Full: "Team full"}))
)

func attr(t *testing.T, h render.HTML, name string) string {
	t.Helper()
	m := regexp.MustCompile(name + `="([^"]*)"`).FindStringSubmatch(string(h))
	if m == nil {
		t.Fatalf("no %s in %s", name, h)
	}
	return html.UnescapeString(m[1])
}

// Define declares the backing store itself: keyed by a minted id,
// created_at and updated_at indexed, plus every Indexed field.
func TestDefineDeclaresTheStore(t *testing.T) {
	var manifest map[string]struct {
		Stores map[string]struct {
			KeyPath string                     `json:"keyPath"`
			AutoKey bool                       `json:"autoKey"`
			Indexes map[string]json.RawMessage `json:"indexes"`
		} `json:"stores"`
	}
	if err := json.Unmarshal(localdb.ManifestJSON(), &manifest); err != nil {
		t.Fatal(err)
	}
	st := manifest["localentity-test"].Stores["members"]
	if st.KeyPath != "id" || !st.AutoKey {
		t.Fatalf("store = %+v, want keyed by an AutoKey id", st)
	}
	for _, ix := range []string{"by_created_at", "by_updated_at", "by_level"} {
		if _, ok := st.Indexes[ix]; !ok {
			t.Errorf("missing index %s in %v", ix, st.Indexes)
		}
	}
	if len(st.Indexes) != 3 {
		t.Errorf("indexes = %v, want exactly the built-ins and level", st.Indexes)
	}
}

// The form carrier hands the behaviour the declaration it validates
// against: types, bounds, choices, defaults, the cap and the words.
func TestFormCarriesTheSpec(t *testing.T) {
	h := members.Form("team-form", render.HTML("<form></form>"))
	if got := attr(t, h, "data-fui-local-form"); got != "localentity-test/members" {
		t.Fatalf("form target = %q", got)
	}
	if !strings.Contains(string(h), `id="team-form"`) || !strings.Contains(string(h), "<form></form>") {
		t.Fatalf("carrier lost its id or its form: %s", h)
	}
	var spec struct {
		Fields []struct {
			Name   string   `json:"name"`
			T      string   `json:"t"`
			Req    bool     `json:"req"`
			Min    *float64 `json:"min"`
			Max    *float64 `json:"max"`
			Values []string `json:"values"`
			Def    any      `json:"def"`
		} `json:"fields"`
		Max  int               `json:"max"`
		Msgs map[string]string `json:"msgs"`
	}
	if err := json.Unmarshal([]byte(attr(t, h, "data-fui-local-schema")), &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Fields) != 4 || spec.Max != 6 {
		t.Fatalf("spec = %+v", spec)
	}
	f := spec.Fields
	if f[0].Name != "nickname" || f[0].T != "string" || !f[0].Req || *f[0].Max != 20 {
		t.Errorf("nickname = %+v", f[0])
	}
	if f[1].T != "int" || *f[1].Min != 1 || *f[1].Max != 100 {
		t.Errorf("level = %+v", f[1])
	}
	if f[2].T != "enum" || strings.Join(f[2].Values, ",") != "a,b" || f[2].Def != "a" {
		t.Errorf("kind = %+v", f[2])
	}
	if f[3].T != "bool" {
		t.Errorf("shiny = %+v", f[3])
	}
	if spec.Msgs["full"] != "Team full" || spec.Msgs["required"] != DefaultMessages.Required {
		t.Errorf("messages = %v; want the override merged over the defaults", spec.Msgs)
	}
}

func TestListRowAndCount(t *testing.T) {
	l := members.List(ListConfig{OrderBy: "level", Desc: true, Limit: 6})
	row := l.Row(func(r Row) render.HTML {
		return render.HTML("<div>") + r.Text("nickname") + r.Text("created_at") +
			r.Delete(render.HTML("<button>x</button>")) + r.Edit("team-form", render.HTML("<button>e</button>")) + render.HTML("</div>")
	})
	h := l.Render(row, l.Empty(render.HTML("<p>none</p>")))
	for name, want := range map[string]string{
		"data-fui-local-list":  "localentity-test/members",
		"data-fui-local-order": "by_level",
		"data-fui-local-dir":   "prev",
		"data-fui-local-limit": "6",
		"data-fui-local-fail":  DefaultMessages.Failed,
		"data-cui-comp":        "local-entity",
	} {
		if got := attr(t, h, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	for _, want := range []string{
		`<template data-fui-local-row="">`, `<template data-fui-local-empty="">`,
		`data-fui-local-text="nickname"`, `data-fui-local-text="created_at"`,
		`data-fui-local-delete=""`, `data-fui-local-edit="team-form"`,
	} {
		if !strings.Contains(string(h), want) {
			t.Errorf("list markup lacks %s:\n%s", want, h)
		}
	}
	if got := attr(t, members.Count(), "data-fui-local-count"); got != "localentity-test/members" {
		t.Errorf("count target = %q", got)
	}
	// The default order is creation, oldest first, everything.
	d := members.List(ListConfig{}).Render("", "")
	if attr(t, d, "data-fui-local-order") != "by_created_at" ||
		strings.Contains(string(d), "data-fui-local-dir") || strings.Contains(string(d), "data-fui-local-limit") {
		t.Errorf("default list = %s", d)
	}
}

// Every refusal is a startup panic naming what is wrong.
func TestRefusals(t *testing.T) {
	db := localdb.New("localentity-refusals")
	str := []schema.Field{{Name: "name", Type: schema.String}}
	mustPanic(t, "needs a localdb.DB", func() { Define(nil, "x", str) })
	mustPanic(t, "at least one field", func() { Define(db, "x", nil) })
	mustPanic(t, "snake_case", func() { Define(db, "x", []schema.Field{{Name: "Name", Type: schema.String}}) })
	mustPanic(t, "snake_case", func() { Define(db, "x", []schema.Field{{Name: "__proto__", Type: schema.String}}) })
	mustPanic(t, "snake_case", func() { Define(db, "x", []schema.Field{{Name: "_x", Type: schema.String}}) })
	mustPanic(t, "snake_case", func() { Define(db, "x", []schema.Field{{Name: strings.Repeat("a", 62), Type: schema.String}}) })
	mustPanic(t, "built in", func() { Define(db, "x", []schema.Field{{Name: "created_at", Type: schema.String}}) })
	mustPanic(t, "declared twice", func() { Define(db, "x", append(str, str...)) })
	mustPanic(t, "cannot store", func() { Define(db, "x", []schema.Field{{Name: "owner", Type: schema.Relation}}) })
	mustPanic(t, "cannot store", func() { Define(db, "x", []schema.Field{{Name: "blob", Type: schema.JSON}}) })
	mustPanic(t, "lists no Values", func() { Define(db, "x", []schema.Field{{Name: "kind", Type: schema.Enum}}) })
	mustPanic(t, "unknown field", func() { Define(db, "x", str, Indexed("nope")) })
	mustPanic(t, "cannot be indexed", func() { Define(db, "x", []schema.Field{{Name: "on", Type: schema.Bool}}, Indexed("on")) })
	mustPanic(t, "must not be negative", func() { Define(db, "x", str, MaxRecords(-1)) })
	mustPanic(t, "JSON values", func() { Define(db, "y", []schema.Field{{Name: "name", Type: schema.String, Default: func() {}}}) })

	mustPanic(t, "needs an index", func() { members.List(ListConfig{OrderBy: "nickname"}) })
	mustPanic(t, "needs an index", func() { members.List(ListConfig{OrderBy: "missing"}) })
	mustPanic(t, "must not be negative", func() { members.List(ListConfig{Limit: -1}) })
	l := members.List(ListConfig{})
	mustPanic(t, "unknown field", func() { l.Row(func(r Row) render.HTML { return r.Text("missing") }) })
	mustPanic(t, "form carrier's id", func() { l.Row(func(r Row) render.HTML { return r.Edit("", "") }) })
}
