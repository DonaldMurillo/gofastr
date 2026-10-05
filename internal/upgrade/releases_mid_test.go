package upgrade_test

import (
	"maps"
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
			// v0.62.0/1: the flat entity-level multi_tenant spelling and
			// app-level auth.enabled reject YAML 1.1 truthy strings, like
			// the scope.* twins the v0.61.0 note covers.
			name: "v0.62.0/1", version: "v0.62.0", index: 1, kinds: []string{"config"},
			old: map[string]string{"gofastr.yml": "app:\n  name: Demo\n  auth:\n    enabled: on\nentities:\n  - name: tasks\n    multi_tenant: yes\n"},
			new: map[string]string{"gofastr.yml": "app:\n  name: Demo\n  auth:\n    enabled: true\nentities:\n  - name: tasks\n    multi_tenant: true\n"},
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
			// v0.85.0/7: scripts passing --token on the command line; a
			// different tool's --tokens flag is not the CLI's token flag.
			name: "v0.85.0/7", version: "v0.85.0", index: 7, kinds: []string{"text"},
			old: map[string]string{"scripts/ci.sh": "#!/bin/sh\n./dist/demo --token \"$TOKEN\" tasks list\n"},
			new: map[string]string{"scripts/ci.sh": "#!/bin/sh\nDEMO_TOKEN=\"$TOKEN\" ./dist/demo tasks list\n./bin/import --tokens 4\n"},
		},
		{
			// v0.85.0/10: an observer attached to the document waits for
			// widget chrome; a component-local observer and the
			// fui:widget-open listener are the migrated spellings.
			name: "v0.85.0/10", version: "v0.85.0", index: 10, kinds: []string{"text"},
			old: map[string]string{"assets/watch.js": "const obs = new MutationObserver(rebind);\nobs.observe(document.body, {childList: true, subtree: true});\n"},
			new: map[string]string{"assets/watch.js": "const panel = document.querySelector('#panel');\nconst obs = new MutationObserver(rebind);\nobs.observe(panel, {childList: true, subtree: true});\nobs.observe(document.querySelector('#other'), {childList: true});\ndocument.addEventListener('fui:widget-open', rebind);\n"},
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

// midGoAPICase is one Go-API note of the slice driven through stub
// kits: the note's symbols declared at their real import paths with the
// signatures the previous release spelled (oldKit) and with the shapes
// the note's own release shipped (newKit), one app per kit, and the
// symbol names each old-spelling use must hit.
type midGoAPICase struct {
	name    string
	version string
	index   int
	oldKit  map[string]string
	newKit  map[string]string
	oldApp  map[string]string
	newApp  map[string]string
	wantHit []string
}

// whyHas reports whether the hits include one for the symbol named: a
// uses hit's why is the symbol alone, a shapes hit's why continues with
// the resolved type string. Line-final matching keeps crud.RegistryLLMMD
// from satisfying crud.RegistryLLMMDHandler's line.
func whyHas(hits []string, sym string) bool {
	for _, h := range hits {
		if strings.HasSuffix(h, sym) || strings.Contains(h, sym+" shape ") {
			return true
		}
	}
	return false
}

// TestMidGoAPINotesHitThroughStubKit drives the slice's Go-API notes
// end to end: a stub kit module declares the old symbols at their real
// import paths with their pre-release signatures, the app uses them,
// and the typed matcher hits, so a typo in any symbol spelling or
// shape regex fails here. Each note is scanned again over the migrated
// app against a kit spelling the new signatures and must stay silent,
// the property a uses matcher cannot give a symbol that survives its
// release with a new shape.
func TestMidGoAPINotesHitThroughStubKit(t *testing.T) {
	for _, tc := range midGoAPICases {
		t.Run(tc.version+" "+tc.name, func(t *testing.T) {
			n := midNote(t, tc.version, tc.index, "uses", "shapes")
			oldRes := scantest.Run(t, scantest.App(t, tc.oldApp, scantest.Options{Kit: tc.oldKit}), []*upgrade.Note{n}, upgrade.MarkerSinks{})
			if !oldRes.TypeChecked {
				t.Fatalf("old app did not type-check: broken=%v unexplained=%v", oldRes.Broken, oldRes.Unexplained)
			}
			got := scantest.Hits(oldRes, n)
			for _, want := range tc.wantHit {
				if !whyHas(got, want) {
					t.Errorf("old spelling: no hit naming %s, got %v", want, got)
				}
			}
			if len(got) != len(tc.wantHit) {
				t.Errorf("old spelling: %d hits (%v), want %d", len(got), got, len(tc.wantHit))
			}
			newRes := scantest.Run(t, scantest.App(t, tc.newApp, scantest.Options{Kit: tc.newKit}), []*upgrade.Note{n}, upgrade.MarkerSinks{})
			if !newRes.TypeChecked {
				t.Fatalf("migrated app did not type-check: broken=%v unexplained=%v", newRes.Broken, newRes.Unexplained)
			}
			if got := scantest.Hits(newRes, n); len(got) != 0 {
				t.Errorf("fires on the migrated spelling: %v", got)
			}
		})
	}

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

var midGoAPICases = []midGoAPICase{
	{
		name:    "idempotency Finish takes a fingerprint",
		version: "v0.64.0",
		index:   1,
		oldKit:  midMiddlewareKit,
		newKit:  midMiddlewareNewKit,
		oldApp:  midMiddlewareOldApp,
		newApp:  midMiddlewareNewApp,
		wantHit: []string{"middleware.IdempotencyStore.Finish"},
	},
	{
		name:    "queue Ack Nack take a Job",
		version: "v0.66.0",
		index:   0,
		oldKit:  midQueueKit,
		newKit:  midQueueNewKit,
		oldApp:  midQueueOldApp,
		newApp:  midQueueNewApp,
		wantHit: []string{
			"queue.Queue.Ack", "queue.Queue.Nack",
			"queue.DBQueue.Ack", "queue.DBQueue.Nack",
			"queue.RedisQueue.Ack", "queue.RedisQueue.Nack",
			"queue.MemoryQueue.Ack", "queue.MemoryQueue.Nack",
		},
	},
	{
		name:    "openapi and llm.md gain crudMounted",
		version: "v0.74.0",
		index:   4,
		oldKit:  mid74Kit,
		newKit:  mid74NewKit,
		oldApp:  mid74OldApp,
		newApp:  mid74NewApp,
		wantHit: []string{"openapi.EntityOpenAPI", "crud.RegistryLLMMD", "crud.RegistryLLMMDHandler"},
	},
	{
		name:    "llm.md generators change shape",
		version: "v0.78.0",
		index:   3,
		oldKit:  mid78Kit,
		newKit:  mid78NewKit,
		oldApp:  mid78OldApp,
		newApp:  mid78NewApp,
		wantHit: []string{
			"crud.EntityLLMMD", "crud.LLMMDHandler", "crud.LLMMDHandlerFor",
			"crud.RegistryLLMMD", "crud.RegistryLLMMDHandler",
		},
	},
	{
		name:    "two-fa store takes a config",
		version: "v0.82.0",
		index:   0,
		oldKit:  midAuthKit,
		newKit:  midAuthNewKit,
		oldApp:  midAuthOldApp,
		newApp:  midAuthNewApp,
		wantHit: []string{"auth.NewEntityTwoFAStore"},
	},
}

// midMiddlewareKit is the core/middleware idempotency surface as v0.63.0
// spelled it: Finish without the request fingerprint.
var midMiddlewareKit = map[string]string{
	"core/middleware/idempotency.go": `package middleware

import (
	"context"
	"net/http"
)

type IdempotentResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

type IdempotencyStore interface {
	Begin(ctx context.Context, key, fingerprint string) (replay *IdempotentResponse, ok bool, err error)
	Finish(ctx context.Context, key string, resp *IdempotentResponse) error
}
`,
}

// midMiddlewareNewKit spells Finish in its v0.64.0 shape.
var midMiddlewareNewKit = func() map[string]string {
	kit := maps.Clone(midMiddlewareKit)
	kit["core/middleware/idempotency.go"] = `package middleware

import (
	"context"
	"net/http"
)

type IdempotentResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

type IdempotencyStore interface {
	Begin(ctx context.Context, key, fingerprint string) (replay *IdempotentResponse, ok bool, err error)
	Finish(ctx context.Context, key, fingerprint string, resp *IdempotentResponse) error
}
`
	return kit
}()

var midMiddlewareOldApp = map[string]string{"main.go": `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

func finish(ctx context.Context, store middleware.IdempotencyStore, key string, resp *middleware.IdempotentResponse) error {
	return store.Finish(ctx, key, resp)
}

func main() { _ = finish }
`}

var midMiddlewareNewApp = map[string]string{"main.go": `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

func finish(ctx context.Context, store middleware.IdempotencyStore, key, fingerprint string, resp *middleware.IdempotentResponse) error {
	return store.Finish(ctx, key, fingerprint, resp)
}

func main() { _ = finish }
`}

// midQueueKit is battery/queue as v0.65.0 spelled it: Ack and Nack take
// the job ID, on the interface and every backend.
var midQueueKit = map[string]string{
	"battery/queue/queue.go": `package queue

import "context"

type Job struct{ ID string }

type Queue interface {
	Enqueue(ctx context.Context, job Job) error
	Dequeue(ctx context.Context, types ...string) (Job, error)
	Ack(ctx context.Context, jobID string) error
	Nack(ctx context.Context, jobID string) error
	Close() error
}

type DBQueue struct{}

func (q *DBQueue) Dequeue(ctx context.Context, types ...string) (Job, error) { return Job{}, nil }
func (q *DBQueue) Ack(ctx context.Context, jobID string) error               { return nil }
func (q *DBQueue) Nack(ctx context.Context, jobID string) error              { return nil }

type RedisQueue struct{}

func (q *RedisQueue) Dequeue(ctx context.Context, types ...string) (Job, error) { return Job{}, nil }
func (q *RedisQueue) Ack(ctx context.Context, jobID string) error              { return nil }
func (q *RedisQueue) Nack(ctx context.Context, jobID string) error             { return nil }

type MemoryQueue struct{}

func (q *MemoryQueue) Dequeue(ctx context.Context, types ...string) (Job, error) { return Job{}, nil }
func (q *MemoryQueue) Ack(_ context.Context, jobID string) error                { return nil }
func (q *MemoryQueue) Nack(_ context.Context, jobID string) error               { return nil }
`,
}

// midQueueNewKit spells the completion arms in their v0.66.0 shape: the
// Dequeue-returned Job, with RedisQueue.Nack's claimed param name.
var midQueueNewKit = func() map[string]string {
	kit := maps.Clone(midQueueKit)
	kit["battery/queue/queue.go"] = `package queue

import "context"

type Job struct{ ID, ClaimToken string }

type Queue interface {
	Enqueue(ctx context.Context, job Job) error
	Dequeue(ctx context.Context, types ...string) (Job, error)
	Ack(ctx context.Context, job Job) error
	Nack(ctx context.Context, job Job) error
	Close() error
}

type DBQueue struct{}

func (q *DBQueue) Dequeue(ctx context.Context, types ...string) (Job, error) { return Job{}, nil }
func (q *DBQueue) Ack(ctx context.Context, job Job) error                    { return nil }
func (q *DBQueue) Nack(ctx context.Context, job Job) error                   { return nil }

type RedisQueue struct{}

func (q *RedisQueue) Dequeue(ctx context.Context, types ...string) (Job, error) { return Job{}, nil }
func (q *RedisQueue) Ack(ctx context.Context, job Job) error                   { return nil }
func (q *RedisQueue) Nack(ctx context.Context, claimed Job) error              { return nil }

type MemoryQueue struct{}

func (q *MemoryQueue) Dequeue(ctx context.Context, types ...string) (Job, error) { return Job{}, nil }
func (q *MemoryQueue) Ack(_ context.Context, job Job) error                     { return nil }
func (q *MemoryQueue) Nack(_ context.Context, job Job) error                    { return nil }
`
	return kit
}()

var midQueueOldApp = map[string]string{"main.go": `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

func drain(ctx context.Context, q queue.Queue, dbq *queue.DBQueue, rq *queue.RedisQueue, mq *queue.MemoryQueue) error {
	j, _ := q.Dequeue(ctx)
	if err := q.Ack(ctx, j.ID); err != nil {
		return q.Nack(ctx, j.ID)
	}
	_ = dbq.Ack(ctx, j.ID)
	_ = dbq.Nack(ctx, j.ID)
	_ = rq.Ack(ctx, j.ID)
	_ = rq.Nack(ctx, j.ID)
	_ = mq.Ack(ctx, j.ID)
	return mq.Nack(ctx, j.ID)
}
func main() { _ = drain }
`}

var midQueueNewApp = map[string]string{"main.go": `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

func drain(ctx context.Context, q queue.Queue, dbq *queue.DBQueue, rq *queue.RedisQueue, mq *queue.MemoryQueue) error {
	j, _ := q.Dequeue(ctx)
	if err := q.Ack(ctx, j); err != nil {
		return q.Nack(ctx, j)
	}
	_ = dbq.Ack(ctx, j)
	_ = dbq.Nack(ctx, j)
	_ = rq.Ack(ctx, j)
	_ = rq.Nack(ctx, j)
	_ = mq.Ack(ctx, j)
	return mq.Nack(ctx, j)
}
func main() { _ = drain }
`}

// midEntityStub is the framework/entity surface the crud and openapi
// kits' signatures name.
var midEntityStub = map[string]string{
	"framework/entity/entity.go": `package entity

type Registry interface{}

type Entity struct{}
`,
}

// mid74Kit holds the openapi and llm.md registry surfaces as v0.73.0
// spelled them: no crudMounted predicate.
var mid74Kit = func() map[string]string {
	kit := maps.Clone(midEntityStub)
	kit["core/openapi/openapi.go"] = `package openapi

type Spec struct{}
`
	kit["framework/openapi/openapi.go"] = `package openapi

import (
	"github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func EntityOpenAPI(registry entity.Registry, title, version string, basePath ...string) *openapi.Spec {
	return nil
}
`
	kit["framework/crud/llmmd.go"] = `package crud

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func RegistryLLMMD(registry entity.Registry, appName string) string { return "" }

func RegistryLLMMDHandler(registry entity.Registry, appName string) http.Handler { return nil }
`
	return kit
}()

// mid74NewKit inserts the crudMounted predicate: before the variadic
// basePath on EntityOpenAPI, appended on the crud pair, as v0.74.0
// shipped them.
var mid74NewKit = func() map[string]string {
	kit := maps.Clone(mid74Kit)
	kit["framework/openapi/openapi.go"] = `package openapi

import (
	"github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func EntityOpenAPI(registry entity.Registry, title, version string, crudMounted func(*entity.Entity) bool, basePath ...string) *openapi.Spec {
	return nil
}
`
	kit["framework/crud/llmmd.go"] = `package crud

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func RegistryLLMMD(registry entity.Registry, appName string, crudMounted func(*entity.Entity) bool) string { return "" }

func RegistryLLMMDHandler(registry entity.Registry, appName string, crudMounted func(*entity.Entity) bool) http.Handler { return nil }
`
	return kit
}()

var mid74OldApp = map[string]string{"main.go": `package main

import (
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
)

func main() {
	_ = openapi.EntityOpenAPI(nil, "demo", "1.0.0")
	_ = crud.RegistryLLMMD(nil, "demo")
	_ = crud.RegistryLLMMDHandler(nil, "demo")
}
`}

var mid74NewApp = map[string]string{"main.go": `package main

import (
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
)

func main() {
	_ = openapi.EntityOpenAPI(nil, "demo", "1.0.0", nil)
	_ = crud.RegistryLLMMD(nil, "demo", nil)
	_ = crud.RegistryLLMMDHandler(nil, "demo", nil)
}
`}

// mid78Kit is the llm.md generator set as v0.77.0 spelled it: the
// registry pair takes a bool predicate, the rest no options.
var mid78Kit = func() map[string]string {
	kit := maps.Clone(midEntityStub)
	kit["framework/crud/llmmd.go"] = `package crud

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

type CrudHandler struct{}

func EntityLLMMD(ent *entity.Entity) string { return "" }

func LLMMDHandler(ent *entity.Entity) http.Handler { return nil }

func LLMMDHandlerFor(ch *CrudHandler) http.Handler { return nil }

func RegistryLLMMD(registry entity.Registry, appName string, crudMounted func(*entity.Entity) bool) string { return "" }

func RegistryLLMMDHandler(registry entity.Registry, appName string, crudMounted func(*entity.Entity) bool) http.Handler { return nil }
`
	return kit
}()

// mid78NewKit spells the generators in their v0.78.0 shape: the
// registry pair takes a MountInfo predicate, the rest an optional
// LLMMDOptions.
var mid78NewKit = func() map[string]string {
	kit := maps.Clone(mid78Kit)
	kit["framework/crud/llmmd.go"] = `package crud

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

type CrudHandler struct{}

type LLMMDOptions struct{ ReadOnly bool }

type MountInfo struct{}

func EntityLLMMD(ent *entity.Entity, opts ...LLMMDOptions) string { return "" }

func LLMMDHandler(ent *entity.Entity, opts ...LLMMDOptions) http.Handler { return nil }

func LLMMDHandlerFor(ch *CrudHandler, opts ...LLMMDOptions) http.Handler { return nil }

func RegistryLLMMD(registry entity.Registry, appName string, crudMount func(*entity.Entity) MountInfo) string { return "" }

func RegistryLLMMDHandler(registry entity.Registry, appName string, crudMount func(*entity.Entity) MountInfo) http.Handler { return nil }
`
	return kit
}()

var mid78OldApp = map[string]string{"main.go": `package main

import (
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func mounted(*entity.Entity) bool { return true }

func main() {
	_ = crud.EntityLLMMD(nil)
	_ = crud.LLMMDHandler(nil)
	_ = crud.LLMMDHandlerFor(nil)
	_ = crud.RegistryLLMMD(nil, "demo", mounted)
	_ = crud.RegistryLLMMDHandler(nil, "demo", mounted)
}
`}

var mid78NewApp = map[string]string{"main.go": `package main

import (
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func mountInfo(*entity.Entity) crud.MountInfo { return crud.MountInfo{} }

func main() {
	_ = crud.EntityLLMMD(nil, crud.LLMMDOptions{ReadOnly: true})
	_ = crud.LLMMDHandler(nil)
	_ = crud.LLMMDHandlerFor(nil, crud.LLMMDOptions{ReadOnly: true})
	_ = crud.RegistryLLMMD(nil, "demo", mountInfo)
	_ = crud.RegistryLLMMDHandler(nil, "demo", mountInfo)
}
`}

// midAuthKit is battery/auth's store constructor as v0.81.0 spelled it.
var midAuthKit = map[string]string{
	"battery/auth/twofa.go": `package auth

import "database/sql"

type EntityTwoFAStore struct{}

func NewEntityTwoFAStore(db *sql.DB, table string) *EntityTwoFAStore { return nil }
`,
}

// midAuthNewKit spells the constructor in its v0.82.0 shape: a config
// and an error.
var midAuthNewKit = func() map[string]string {
	kit := maps.Clone(midAuthKit)
	kit["battery/auth/twofa.go"] = `package auth

import "database/sql"

type EntityTwoFAStore struct{}

type EntityTwoFAStoreConfig struct{ EncryptionKey []byte }

func NewEntityTwoFAStore(db *sql.DB, table string, cfg EntityTwoFAStoreConfig) (*EntityTwoFAStore, error) {
	return nil, nil
}
`
	return kit
}()

var midAuthOldApp = map[string]string{"main.go": `package main

import (
	"database/sql"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

func main() {
	var db *sql.DB
	store := auth.NewEntityTwoFAStore(db, "user_twofa")
	_ = store
}
`}

var midAuthNewApp = map[string]string{"main.go": `package main

import (
	"database/sql"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

func main() {
	var db *sql.DB
	key := make([]byte, 32)
	store, err := auth.NewEntityTwoFAStore(db, "user_twofa", auth.EntityTwoFAStoreConfig{EncryptionKey: key})
	if err != nil {
		return
	}
	_ = store
}
`}

// TestMidTierDecisions pins the tier of notes whose hits may still be
// correct after their release: hits: edit would claim every hit must
// change, which is false where a spelling survives on another surface.
func TestMidTierDecisions(t *testing.T) {
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	// v0.60.0/2: static export, theme-variant pages, and embeds keep the
	// inline catalog blocks and the /__gofastr/actions.js concat endpoint,
	// so a hit on those spellings may be correct as written.
	n := scantest.Note(t, reg, "v0.60.0", 2)
	if !n.Review {
		t.Errorf("v0.60.0 note 2 must be hits: review: static export and embeds still serve the catalog blocks and the actions.js concat")
	}
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
		"v0.56.0": 1, "v0.57.0": 1, "v0.58.0": 1, "v0.59.0": 1,
		"v0.60.0": 0, "v0.61.0": 3, "v0.62.0": 0, "v0.63.0": 1,
		"v0.64.0": 3, "v0.65.0": 2, "v0.66.0": 3, "v0.67.0": 1,
		"v0.68.0": 2, "v0.69.0": 2, "v0.70.0": 2, "v0.71.0": 2,
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
