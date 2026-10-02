package upgrade_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// midNote picks a shipped note from the v0.56.0–v0.85.0 slice of the
// registry so the tests exercise the shipped YAML, not a copy.
func midNote(t *testing.T, version string, index int, kinds ...string) *upgrade.Note {
	t.Helper()
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	n := scantest.Note(t, reg, version, index)
	if n.Find.Empty() {
		t.Fatalf("%s note %d has no find", version, index)
	}
	if len(kinds) == 0 {
		return n
	}
	return scantest.Only(n, kinds...)
}

// TestMidNotesOldSpellingHitsNewDoesNot drives every converted note in
// the slice whose find reads strings, config, go.mod or text files
// through scan.Run: the affected spelling is a hit, the migrated (or
// unaffected, for markers that legitimately survive migration — the
// note shows once while crossing its release) spelling is not.
func TestMidNotesOldSpellingHitsNewDoesNot(t *testing.T) {
	cases := []struct {
		name    string
		version string
		index   int
		kinds   []string
		old     map[string]string
		new     map[string]string
	}{
		{
			// v0.60.0/2: scraping the inline #gofastr-catalog block
			// (Go string and custom JS) breaks; the manifest globals do not.
			name: "v0.60.0/2", version: "v0.60.0", index: 2, kinds: []string{"strings", "text"},
			old: map[string]string{
				"main.go":       "package main\n\nvar catalogSel = \"#gofastr-catalog\"\n\nfunc main() { _ = catalogSel }\n",
				"assets/app.js": "var c = document.getElementById('gofastr-catalog');\n",
			},
			new: map[string]string{
				"main.go":       "package main\n\nfunc main() {}\n",
				"assets/app.js": "var c = window.__gofastr_catalog;\n",
			},
		},
		{
			// v0.60.0/4: history.pushState in custom JS leaves currentPath
			// stale; __gofastr._pushURL is the migrated call.
			name: "v0.60.0/4", version: "v0.60.0", index: 4, kinds: []string{"text"},
			old: map[string]string{"assets/nav.js": "history.pushState({}, '', href);\n"},
			new: map[string]string{"assets/nav.js": "__gofastr._pushURL(href);\n"},
		},
		{
			// v0.61.0/2: command extensions in gofastr.codegen.yml lose the
			// ambient environment. Marker note: the extensions block stays;
			// the control app has no such file.
			name: "v0.61.0/2", version: "v0.61.0", index: 2, kinds: []string{"text"},
			old: map[string]string{"gofastr.codegen.yml": "extensions:\n  - name: deps\n    command: [./bin/deps]\n"},
			new: map[string]string{"README.md": "# no codegen extensions\n"},
		},
		{
			// v0.61.0/3: an app.theme value that breaks the CSS declaration
			// is refused; a plain color is not.
			name: "v0.61.0/3", version: "v0.61.0", index: 3, kinds: []string{"config"},
			old: map[string]string{"gofastr.yml": "app:\n  name: Demo\n  theme:\n    --color-accent: \"#0f766e;}\"\n"},
			new: map[string]string{"gofastr.yml": "app:\n  name: Demo\n  theme:\n    --color-accent: \"#0f766e\"\n"},
		},
		{
			// v0.61.0/4: scope.multi_tenant as a YAML 1.2 string ("yes")
			// errors; a real boolean does not.
			name: "v0.61.0/4", version: "v0.61.0", index: 4, kinds: []string{"config"},
			old: map[string]string{"gofastr.yml": "entities:\n  - name: tasks\n    scope:\n      multi_tenant: yes\n"},
			new: map[string]string{"gofastr.yml": "entities:\n  - name: tasks\n    scope:\n      multi_tenant: true\n"},
		},
		{
			// v0.62.0/0: the scaffold's "Migration warning" log line is the
			// warn-and-boot spelling; the fail-closed scaffold drops it.
			name: "v0.62.0/0", version: "v0.62.0", index: 0, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nimport \"log\"\n\nfunc main() {\n\tif err := up(); err != nil {\n\t\tlog.Printf(\"Migration warning: %v\", err)\n\t}\n}\n\nfunc up() error { return nil }\n"},
			new: map[string]string{"main.go": "package main\n\nimport \"log\"\n\nfunc main() {\n\tif err := up(); err != nil {\n\t\tlog.Fatal(err)\n\t}\n}\n\nfunc up() error { return nil }\n"},
		},
		{
			// v0.63.0/1: the generated seed hook's skip log is the
			// count-error-is-not-seeded spelling; the fail-closed hook
			// returns the error instead.
			name: "v0.63.0/1", version: "v0.63.0", index: 1, kinds: []string{"strings"},
			old: map[string]string{"seeds.go": "package main\n\nimport \"log\"\n\nfunc seed() {\n\tif err := row(); err != nil {\n\t\tlog.Printf(\"seed %s: skipping row: %v\", \"tags\", err)\n\t}\n}\n\nfunc row() error { return nil }\n"},
			new: map[string]string{"seeds.go": "package main\n\nfunc seed() error { return row() }\n\nfunc row() error { return nil }\n"},
		},
		{
			// v0.64.0/0: a JS client concatenating servers[0].url onto each
			// path key double-counts the prefix now.
			name: "v0.64.0/0", version: "v0.64.0", index: 0, kinds: []string{"text"},
			old: map[string]string{"assets/client.js": "const base = spec.servers[0].url;\nfetch(base + '/posts');\n"},
			new: map[string]string{"assets/client.js": "const base = window.location.origin;\nfetch(base + '/api/posts');\n"},
		},
		{
			// v0.70.0/0: the module floor.
			name: "v0.70.0/0", version: "v0.70.0", index: 0, kinds: []string{"gomod"},
			old: map[string]string{"go.mod": "module example.com/app\n\ngo 1.26.4\n"},
			new: map[string]string{"go.mod": "module example.com/app\n\ngo 1.27.0\n"},
		},
		{
			// v0.74.0/0: shell scripts driving the removed `kiln acp`
			// surface; session/new is the migrated command.
			name: "v0.74.0/0", version: "v0.74.0", index: 0, kinds: []string{"text"},
			old: map[string]string{"scripts/replay.sh": "#!/bin/sh\nkiln acp --journal run\n"},
			new: map[string]string{"scripts/replay.sh": "#!/bin/sh\nkiln session run\n"},
		},
		{
			// v0.74.0/2: data-fui-confirm on a plain POST form now prompts;
			// the attribute key is the affected spelling.
			name: "v0.74.0/2", version: "v0.74.0", index: 2, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nvar attrs = map[string]string{\"data-fui-confirm\": \"Delete?\"}\n\nfunc main() { _ = attrs }\n"},
			new: map[string]string{"main.go": "package main\n\nvar attrs = map[string]string{\"data-fui-rpc\": \"delete\"}\n\nfunc main() { _ = attrs }\n"},
		},
		{
			// v0.77.0/3: blueprint keys the pack serializer used to drop;
			// re-packing emits them. Marker note: an app without them has
			// no diff to review.
			name: "v0.77.0/3", version: "v0.77.0", index: 3, kinds: []string{"config"},
			old: map[string]string{"gofastr.yml": "app:\n  name: Demo\n  description: A demo\nmiddleware:\n  - name: cors\n"},
			new: map[string]string{"gofastr.yml": "app:\n  name: Demo\n"},
		},
		{
			// v0.83.0/10: a shipped generated SDK is identified by its
			// README marker. Marker note: regeneration keeps the header;
			// the control app ships no SDK.
			name: "v0.83.0/10", version: "v0.83.0", index: 10, kinds: []string{"text"},
			old: map[string]string{"sdk/go/README.md": "# Demo Go SDK\n\n<!-- Code generated by gofastr v0.82.0 (sdk 1) for demo. DO NOT EDIT. -->\n"},
			new: map[string]string{"README.md": "# demo\n"},
		},
		{
			// v0.85.0/2: a tooling flow that fetches the spec anonymously.
			// Marker note: authenticating the fetch keeps the URL.
			name: "v0.85.0/2", version: "v0.85.0", index: 2, kinds: []string{"strings"},
			old: map[string]string{"main.go": "package main\n\nconst specURL = \"/openapi.json\"\n\nfunc main() { _ = specURL }\n"},
			new: map[string]string{"main.go": "package main\n\nfunc main() {}\n"},
		},
		{
			// v0.85.0/6: a kiln journal entry recorded without a plan.
			name: "v0.85.0/6", version: "v0.85.0", index: 6, kinds: []string{"strings", "text"},
			old: map[string]string{
				"main.go":            "package main\n\nconst replayOp = \"update_entity\"\n\nfunc main() { _ = replayOp }\n",
				"kiln/session.jsonl": "{\"op\": \"update_entity\", \"entity\": \"tasks\"}\n",
			},
			new: map[string]string{
				"main.go":            "package main\n\nfunc main() {}\n",
				"kiln/session.jsonl": "{\"op\": \"add_entity\", \"entity\": \"tasks\"}\n",
			},
		},
		{
			// v0.85.0/7: scripts passing --token on the command line.
			name: "v0.85.0/7", version: "v0.85.0", index: 7, kinds: []string{"text"},
			old: map[string]string{"scripts/ci.sh": "#!/bin/sh\n./dist/demo --token \"$TOKEN\" tasks list\n"},
			new: map[string]string{"scripts/ci.sh": "#!/bin/sh\nDEMO_TOKEN=\"$TOKEN\" ./dist/demo tasks list\n"},
		},
		{
			// v0.85.0/10: a whole-document MutationObserver waiting for
			// widget chrome; fui:widget-open is the migrated listener.
			name: "v0.85.0/10", version: "v0.85.0", index: 10, kinds: []string{"text"},
			old: map[string]string{"assets/watch.js": "const obs = new MutationObserver(rebind);\n"},
			new: map[string]string{"assets/watch.js": "document.addEventListener('fui:widget-open', rebind);\n"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := midNote(t, tc.version, tc.index, tc.kinds...)
			for label, files := range map[string]map[string]string{"old": tc.old, "new": tc.new} {
				root := scantest.App(t, files, scantest.Options{})
				res := scantest.Run(t, root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
				got := scantest.Hits(res, n)
				if label == "old" && len(got) == 0 {
					t.Fatalf("%s: affected spelling produced no hit", tc.name)
				}
				if label == "new" && len(got) != 0 {
					t.Fatalf("%s: migrated spelling still hits: %v", tc.name, got)
				}
			}
		})
	}
}

// TestMidGoAPINotesHitThroughStubKit spot-checks three Go-API notes of
// the slice end to end: a stub kit module declares the old symbols at
// their real import paths, the app uses them, and the typed matcher
// hits — so a typo in any symbol spelling fails here.
func TestMidGoAPINotesHitThroughStubKit(t *testing.T) {
	t.Run("v0.66.0 queue Ack Nack take Job", func(t *testing.T) {
		n := midNote(t, "v0.66.0", 0, "uses")
		kit := map[string]string{"battery/queue/queue.go": `package queue

type Job struct{ ID string }

type Queue interface {
	Dequeue() (*Job, error)
	Ack(jobID string) error
	Nack(jobID string) error
}
`}
		app := scantest.App(t, map[string]string{"main.go": `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

func drain(ctx context.Context, q queue.Queue) error {
	j, _ := q.Dequeue()
	if err := q.Ack(j.ID); err != nil {
		return q.Nack(j.ID)
	}
	return nil
}

func main() { _ = drain }
`}, scantest.Options{Kit: kit})
		res := scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{})
		if !res.TypeChecked {
			t.Fatalf("app did not type-check: %v", res.Unexplained)
		}
		got := scantest.Hits(res, n)
		if len(got) != 2 || !strings.HasSuffix(got[0], "queue.Queue.Ack") || !strings.HasSuffix(got[1], "queue.Queue.Nack") {
			t.Fatalf("hits = %v, want Ack and Nack on queue.Queue", got)
		}
	})

	t.Run("v0.78.0 llm.md generators", func(t *testing.T) {
		n := midNote(t, "v0.78.0", 3, "uses")
		kit := map[string]string{"framework/crud/llmmd.go": `package crud

func EntityLLMMD(e int) string                     { return "" }
func RegistryLLMMD(r int, n string, f func(int) bool) string { return "" }
func LLMMDHandler(e int) int                       { return 0 }
func LLMMDHandlerFor(c int) int                    { return 0 }
func RegistryLLMMDHandler(r int, n string, f func(int) bool) int { return 0 }
`}
		app := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework/crud"

func main() {
	_ = crud.EntityLLMMD(1)
	_ = crud.RegistryLLMMD(1, "app", nil)
}
`}, scantest.Options{Kit: kit})
		res := scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{})
		if !res.TypeChecked {
			t.Fatalf("app did not type-check: %v", res.Unexplained)
		}
		got := scantest.Hits(res, n)
		if len(got) != 2 || !strings.HasSuffix(got[0], "crud.EntityLLMMD") || !strings.HasSuffix(got[1], "crud.RegistryLLMMD") {
			t.Fatalf("hits = %v, want EntityLLMMD and RegistryLLMMD", got)
		}
	})

	t.Run("v0.67.0 in-house sqlite engine import", func(t *testing.T) {
		n := midNote(t, "v0.67.0", 1, "imports")
		kit := map[string]string{"sqlite/sqlite.go": "package sqlite\n\nfunc DriverName() string { return \"gofastr-sqlite\" }\n"}
		app := scantest.App(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/sqlite"

func main() { _ = sqlite.DriverName() }
`}, scantest.Options{Kit: kit})
		res := scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{})
		got := scantest.Hits(res, n)
		if len(got) != 1 || !strings.Contains(got[0], "import ") {
			t.Fatalf("hits = %v, want the sqlite import", got)
		}
	})
}

// TestMidNodetectNotesJustified walks the notes converted to nodetect
// and holds the count to the ones this slice justified, so a silent
// extra nodetect cannot slip in.
func TestMidNodetectNotesJustified(t *testing.T) {
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	want := map[string]int{ // version -> count of nodetect notes
		"v0.56.0": 1, "v0.57.0": 0, "v0.58.0": 1, "v0.59.0": 1,
		"v0.60.0": 0, "v0.61.0": 3, "v0.62.0": 0, "v0.63.0": 1,
		"v0.64.0": 3, "v0.65.0": 1, "v0.66.0": 3, "v0.67.0": 1,
		"v0.68.0": 1, "v0.69.0": 2, "v0.70.0": 2, "v0.71.0": 2,
		"v0.71.1": 1, "v0.71.2": 1, "v0.72.0": 3, "v0.73.0": 2,
		"v0.74.0": 1, "v0.75.0": 2, "v0.76.0": 2, "v0.77.0": 2,
		"v0.78.0": 2, "v0.79.0": 2, "v0.80.0": 2, "v0.81.0": 3,
		"v0.82.0": 6, "v0.83.0": 5, "v0.84.0": 1, "v0.84.1": 1,
		"v0.85.0": 3,
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
