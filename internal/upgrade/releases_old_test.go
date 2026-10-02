package upgrade_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// oldNote picks a shipped note from the v0.3.0–v0.55.0 slice of the
// registry so the tests exercise the shipped YAML, not a copy.
func oldNote(t *testing.T, version string, index int, kinds ...string) *upgrade.Note {
	t.Helper()
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	n := scantest.Note(t, reg, version, index)
	if n.Nodetect != "" {
		t.Fatalf("%s note %d is nodetect; it has no matcher to drive", version, index)
	}
	if n.Find.Empty() {
		t.Fatalf("%s note %d has no find", version, index)
	}
	if len(kinds) == 0 {
		return n
	}
	return scantest.Only(n, kinds...)
}

// TestOldNotesOldSpellingHitsNewDoesNot drives every converted note in
// the slice whose find reads strings, CSS, gofastr.yml or text files
// through scan.Run: the affected spelling is a hit, the migrated (or
// unaffected, for markers that legitimately survive migration — the
// note shows once while crossing its release) spelling is not.
// wantWhy pins one hit's Why so a matcher whose only arm went missing
// fails the case even when another arm still hits.
func TestOldNotesOldSpellingHitsNewDoesNot(t *testing.T) {
	cases := []struct {
		name    string
		version string
		index   int
		kinds   []string
		old     map[string]string
		wantWhy []string // substrings that must each appear in some hit
		new     map[string]string
	}{
		{
			// v0.4.0/0: entities/*.json declarations are gone; Go
			// declarations are the replacement.
			name: "v0.4.0/0", version: "v0.4.0", index: 0, kinds: []string{"text"},
			old: map[string]string{"entities/users.json": "{\n  \"name\": \"users\",\n  \"table\": \"users\",\n  \"fields\": []\n}\n"},
			new: map[string]string{"main.go": "package main\n\nfunc main() {}\n"},
		},
		{
			// v0.13.0/2: the .fui-pos-center > .fui-slot selector moves
			// to .fui-panel. fui-pos-center itself is still emitted on
			// every centered widget, so the class alone is no hit.
			name: "v0.13.0/2", version: "v0.13.0", index: 2, kinds: []string{"strings", "css"},
			wantWhy: []string{"match", "css .fui-pos-center > .fui-slot"},
			old: map[string]string{
				"main.go":        "package main\n\nvar modalCSS = \".fui-pos-center>.fui-slot{padding:0}\"\n\nfunc main() { _ = modalCSS }\n",
				"styles/app.css": ".fui-pos-center > .fui-slot { outline: none; }\n",
			},
			new: map[string]string{
				"main.go":        "package main\n\nvar modalCSS = \".fui-pos-center>.fui-panel{padding:0}\"\nvar pos = \"fui-pos-center\"\n\nfunc main() { _, _ = modalCSS, pos }\n",
				"styles/app.css": ".fui-pos-center > .fui-panel { outline: none; }\n",
			},
		},
		{
			// v0.27.0/0: a relative file: sqlite URL resolves against
			// the launch cwd before this release.
			name: "v0.27.0/0a", version: "v0.27.0", index: 0, kinds: []string{"config"},
			old: map[string]string{"gofastr.yml": "app:\n  name: Demo\n  db:\n    url: file:blog.db\n"},
			new: map[string]string{"gofastr.yml": "app:\n  name: Demo\n  db:\n    url: file:/srv/app/blog.db\n"},
		},
		{
			// v0.27.0/0: a bare relative path has the same problem.
			name: "v0.27.0/0b", version: "v0.27.0", index: 0, kinds: []string{"config"},
			old: map[string]string{"gofastr.yml": "app:\n  name: Demo\n  db:\n    url: blog.db\n"},
			new: map[string]string{"gofastr.yml": "app:\n  name: Demo\n  db:\n    url: postgres://u:p@localhost/app\n"},
		},
		{
			// v0.38.0/1: client JS polling the removed signal endpoint.
			name: "v0.38.0/1", version: "v0.38.0", index: 1, kinds: []string{"text"},
			old: map[string]string{"assets/app.js": "fetch('/__gofastr/signal/abc').then(r => r.json());\n"},
			new: map[string]string{"assets/app.js": "fetch('/__gofastr/sse').then(r => r.json());\n"},
		},
		{
			// v0.43.0/0: the client-selected slow-mode header is now a
			// broker opt-in.
			name: "v0.43.0/0", version: "v0.43.0", index: 0, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nvar slowHeader = \"X-SSE-Slow\"\n\nfunc main() { _ = slowHeader }\n"},
			new: map[string]string{"main.go": "package main\n\nvar slowHeader = \"X-Accept-Buffering\"\n\nfunc main() { _ = slowHeader }\n"},
		},
		{
			// v0.43.0/6: a blueprint field name that is not a Go
			// identifier, and an enum value carrying a quote.
			name: "v0.43.0/6", version: "v0.43.0", index: 6, kinds: []string{"config"},
			wantWhy: []string{"config entities.0.fields.0.name", "config entities.0.fields.1.values.0"},
			old:     map[string]string{"gofastr.yml": "entities:\n  - name: cards\n    fields:\n      - name: \"9lives\"\n        type: int\n      - name: suit\n        type: enum\n        values: [\"He\\\"arts\"]\n"},
			new:     map[string]string{"gofastr.yml": "entities:\n  - name: cards\n    fields:\n      - name: lives\n        type: int\n      - name: suit\n        type: enum\n        values: [hearts, spades]\n"},
		},
		{
			// v0.45.0/2: _like is a literal substring now, so the
			// wildcard-sending URL builder is the review point.
			name: "v0.45.0/2", version: "v0.45.0", index: 2, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nvar q = \"/api/tasks?title_like=%25\"\n\nfunc main() { _ = q }\n"},
			new: map[string]string{"main.go": "package main\n\nvar q = \"/api/tasks?title=abc\"\n\nfunc main() { _ = q }\n"},
		},
		{
			// v0.45.0/6: clients that scripted the GET verify must POST.
			name: "v0.45.0/6", version: "v0.45.0", index: 6, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nvar verify = \"/auth/magic-link/verify?token=\"\n\nfunc main() { _ = verify }\n"},
			new: map[string]string{"main.go": "package main\n\nvar verify = \"/auth/login\"\n\nfunc main() { _ = verify }\n"},
		},
		{
			// v0.45.0/10: a discovered codegen config with a command
			// extension is refused.
			name: "v0.45.0/10", version: "v0.45.0", index: 10, kinds: []string{"text"},
			old: map[string]string{"gofastr.codegen.yml": "extensions:\n  - name: deps\n    command: [./bin/fetch-deps]\n"},
			new: map[string]string{"gofastr.codegen.yml": "extensions:\n  - name: deps\n    copy:\n      - from: assets\n        to: static\n"},
		},
		{
			// v0.46.0/7: a table name with a dash breaks generated
			// source and bare DDL identifiers.
			name: "v0.46.0/7", version: "v0.46.0", index: 7, kinds: []string{"config"},
			old: map[string]string{"gofastr.yml": "entities:\n  - name: licenses\n    table: license-keys\n"},
			new: map[string]string{"gofastr.yml": "entities:\n  - name: licenses\n    table: license_keys\n"},
		},
		{
			// v0.46.0/8: a pre-v0.46 session journal carrying deletes
			// fails replay.
			name: "v0.46.0/8", version: "v0.46.0", index: 8, kinds: []string{"text"},
			old: map[string]string{".kiln.session.jsonl": "{\"op\":\"add_entity\",\"name\":\"posts\"}\n{\"op\":\"delete_entity\",\"name\":\"temps\"}\n"},
			new: map[string]string{".kiln.session.jsonl": "{\"op\":\"add_entity\",\"name\":\"posts\"}\n{\"op\":\"update_entity\",\"name\":\"posts\"}\n"},
		},
		{
			// v0.47.0/1: hydration wired to the dead placeholder
			// attributes, in Go attrs and custom JS.
			name: "v0.47.0/1", version: "v0.47.0", index: 1, kinds: []string{"strings", "text"},
			wantWhy: []string{"attr data-blurhash", "text data-(placeholder|blurhash)"},
			old: map[string]string{
				"main.go":           "package main\n\nvar attrs = map[string]string{\"data-blurhash\": h}\n\nfunc main() { _ = attrs }\n",
				"assets/hydrate.js": "document.querySelectorAll('[data-blurhash]').forEach(paint);\n",
			},
			new: map[string]string{
				"main.go":           "package main\n\nvar attrs = map[string]string{\"data-src\": h}\n\nfunc main() { _ = attrs }\n",
				"assets/hydrate.js": "document.querySelectorAll('[data-src]').forEach(paint);\n",
			},
		},
		{
			// v0.48.0/6: nested ?rel.field= filters refuse unregistered
			// targets.
			name: "v0.48.0/6", version: "v0.48.0", index: 6, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nvar list = \"/api/tasks?author.name_like=A\"\n\nfunc main() { _ = list }\n"},
			new: map[string]string{"main.go": "package main\n\nvar list = \"/api/tasks?author=A\"\n\nfunc main() { _ = list }\n"},
		},
		{
			// v0.48.0/9: ?cursor= now validates ?sort=.
			name: "v0.48.0/9", version: "v0.48.0", index: 9, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nvar page = \"/api/tasks?cursor=abc&sort=-id\"\n\nfunc main() { _ = page }\n"},
			new: map[string]string{"main.go": "package main\n\nvar page = \"/api/tasks?p=2&sort=-id\"\n\nfunc main() { _ = page }\n"},
		},
		{
			// v0.50.0/1: G.serverAction inside an embedded screen's
			// ClientJS panics at boot.
			name: "v0.50.0/1", version: "v0.50.0", index: 1, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nconst clientJS = `onclick: () => G.serverAction('save')`\n\nfunc main() { _ = clientJS }\n"},
			new: map[string]string{"main.go": "package main\n\nconst clientJS = `onclick: () => __gofastr.rpc('save')`\n\nfunc main() { _ = clientJS }\n"},
		},
		{
			// v0.52.0/2: the embed CSRF exemption no longer covers a
			// host's own POST under the prefix.
			name: "v0.52.0/2", version: "v0.52.0", index: 2, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nvar hook = \"/__gofastr/embed/telemetry\"\n\nfunc main() { _ = hook }\n"},
			new: map[string]string{"main.go": "package main\n\nvar hook = \"/api/telemetry\"\n\nfunc main() { _ = hook }\n"},
		},
		{
			// v0.54.0/3: upload metadata keys moved to camelCase.
			name: "v0.54.0/3", version: "v0.54.0", index: 3, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nfunc name(b map[string]any) string { return b[\"original_name\"].(string) }\n\nfunc main() { _ = name }\n"},
			new: map[string]string{"main.go": "package main\n\nfunc name(b map[string]any) string { return b[\"originalName\"].(string) }\n\nfunc main() { _ = name }\n"},
		},
		{
			// v0.55.0/6: an over-length _in= list is a 400 now.
			name: "v0.55.0/6", version: "v0.55.0", index: 6, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nvar list = \"/api/tasks?ids_in=1,2,3\"\n\nfunc main() { _ = list }\n"},
			new: map[string]string{"main.go": "package main\n\nvar list = \"/api/tasks?ids=1&ids=2&ids=3\"\n\nfunc main() { _ = list }\n"},
		},
		{
			// v0.55.0/10: the island endpoint gains a screen segment;
			// the old single-segment URL is the affected spelling.
			name: "v0.55.0/10", version: "v0.55.0", index: 10, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nvar endpoint = \"/api/tables/tasks\"\n\nfunc main() { _ = endpoint }\n"},
			new: map[string]string{"main.go": "package main\n\nvar endpoint = \"/api/tables/inbox/tasks\"\n\nfunc main() { _ = endpoint }\n"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := oldNote(t, tc.version, tc.index, tc.kinds...)
			for label, files := range map[string]map[string]string{"old": tc.old, "new": tc.new} {
				root := scantest.App(t, files, scantest.Options{})
				res := scantest.Run(t, root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
				got := scantest.Hits(res, n)
				if label == "old" {
					if len(got) == 0 {
						t.Fatalf("%s: affected spelling produced no hit", tc.name)
					}
					for _, want := range tc.wantWhy {
						if !strings.Contains(strings.Join(got, "\n"), want) {
							t.Fatalf("%s: no hit why contains %q; hits = %v", tc.name, want, got)
						}
					}
				}
				if label == "new" && len(got) != 0 {
					t.Fatalf("%s: migrated spelling still hits: %v", tc.name, got)
				}
			}
		})
	}
}

// TestOldGoAPINotesHitThroughStubKit spot-checks three Go-API notes of
// the slice end to end: a stub kit module declares the old symbols at
// their real import paths, the app uses them, and the typed matcher
// hits — so a typo in any symbol spelling fails here.
func TestOldGoAPINotesHitThroughStubKit(t *testing.T) {
	t.Run("v0.29.0 App.Entity declarations", func(t *testing.T) {
		n := oldNote(t, "v0.29.0", 0, "uses")
		kit := map[string]string{"framework/app.go": `package framework

type EntityConfig struct{ Public bool }

type App struct{}

func (a *App) Entity(name string, cfg EntityConfig) *App { return a }
`}
		app := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework"

func main() {
	app := &framework.App{}
	app.Entity("tasks", framework.EntityConfig{})
}
`}, scantest.Options{Kit: kit})
		res := scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{})
		if !res.TypeChecked {
			t.Fatalf("app did not type-check: %v", res.Unexplained)
		}
		got := scantest.Hits(res, n)
		if len(got) != 1 || !strings.HasSuffix(got[0], "framework.App.Entity") {
			t.Fatalf("hits = %v, want framework.App.Entity", got)
		}
	})

	t.Run("v0.45.0 session minting paths", func(t *testing.T) {
		n := oldNote(t, "v0.45.0", 3, "uses")
		kit := map[string]string{"battery/auth/auth.go": `package auth

type Session struct{ Token string }

type SessionStore interface {
	Create(userID string) (*Session, error)
}

type AuthManager struct{ store SessionStore }

func (m *AuthManager) SessionStore() SessionStore { return m.store }
`}
		app := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/battery/auth"

func mint(mgr *auth.AuthManager) error {
	_, err := mgr.SessionStore().Create("u1")
	return err
}

func main() { _ = mint }
`}, scantest.Options{Kit: kit})
		res := scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{})
		if !res.TypeChecked {
			t.Fatalf("app did not type-check: %v", res.Unexplained)
		}
		got := scantest.Hits(res, n)
		joined := strings.Join(got, "\n")
		if !strings.Contains(joined, "auth.AuthManager.SessionStore") || !strings.Contains(joined, "auth.SessionStore.Create") {
			t.Fatalf("hits = %v, want SessionStore getter and Create", got)
		}
	})

	t.Run("v0.47.0 blurhash placeholder value", func(t *testing.T) {
		n := oldNote(t, "v0.47.0", 0, "fields")
		kit := map[string]string{"framework/ui/image.go": `package ui

type OptimizedImageConfig struct {
	Src         string
	Placeholder string
}
`}
		build := func(placeholder string) string {
			return `package main

import "github.com/DonaldMurillo/gofastr/framework/ui"

func main() {
	_ = ui.OptimizedImageConfig{Src: "/a.webp", Placeholder: "` + placeholder + `"}
}
`
		}
		for label, tc := range map[string]struct {
			src  string
			want bool
		}{
			"blurhash": {`LEHV6nWB2yk8pyo0adR*.7kCMdnj`, true},
			"filename": {`hero.jpg`, true},
			"data-uri": {`data:image/webp;base64,UklGR`, false},
		} {
			app := scantest.App(t, map[string]string{"main.go": build(tc.src)}, scantest.Options{Kit: kit})
			res := scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{})
			if !res.TypeChecked {
				t.Fatalf("%s: app did not type-check: %v", label, res.Unexplained)
			}
			got := scantest.Hits(res, n)
			if tc.want && len(got) == 0 {
				t.Fatalf("%s: %q produced no hit", label, tc.src)
			}
			if !tc.want && len(got) != 0 {
				t.Fatalf("%s: %q still hits: %v", label, tc.src, got)
			}
		}
	})
}

// TestOldNodetectNotesJustified walks the notes converted to nodetect
// and holds the count to the ones this slice justified, so a silent
// extra nodetect cannot slip in.
func TestOldNodetectNotesJustified(t *testing.T) {
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	want := map[string]int{ // version -> count of nodetect notes
		"v0.3.0": 6, "v0.4.0": 0, "v0.5.0": 1, "v0.6.0": 2,
		"v0.7.0": 0, "v0.11.0": 0, "v0.12.0": 1, "v0.13.0": 2,
		"v0.16.0": 0, "v0.21.0": 1, "v0.23.0": 0, "v0.26.0": 1,
		"v0.27.0": 0, "v0.29.0": 1, "v0.30.0": 1, "v0.31.0": 2,
		"v0.32.0": 3, "v0.35.0": 1, "v0.36.0": 1, "v0.38.0": 0,
		"v0.40.0": 1, "v0.43.0": 4, "v0.45.0": 5, "v0.46.0": 1,
		"v0.47.0": 1, "v0.48.0": 5, "v0.49.0": 3, "v0.50.0": 0,
		"v0.52.0": 1, "v0.53.0": 0, "v0.54.0": 1, "v0.55.0": 5,
	}
	for _, rel := range reg.Releases {
		wantCount, ok := want[rel.Version]
		if !ok {
			continue // another worker's slice
		}
		var reasons []string
		for i, n := range rel.Notes {
			if n.Nodetect == "" {
				if n.Find.Empty() {
					t.Errorf("%s note %d: neither find nor nodetect", rel.Version, i)
				}
				continue
			}
			if !n.Find.Empty() {
				t.Errorf("%s note %d: find and nodetect together", rel.Version, i)
			}
			reasons = append(reasons, n.Nodetect)
		}
		if len(reasons) != wantCount {
			t.Errorf("%s: %d nodetect notes (%v), want %d", rel.Version, len(reasons), reasons, wantCount)
		}
	}
}
