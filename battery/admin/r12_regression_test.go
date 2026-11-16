package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// A columned App.View is mounted read-only; the admin serves no write
// for it and its screens draw none.
func TestReadOnlyViewGetsNoAdminWrites(t *testing.T) {
	db := newDB(t)
	if err := framework.EnsureAuditTable(db, ""); err != nil {
		t.Fatal(err)
	}
	site := appui.NewApp("admin-test")
	app := framework.NewUIHostApp(uihost.New(site), framework.WithDB(db), framework.WithoutDefaultMiddleware())
	app.Entity("posts", postsConfig())
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE VIEW open_posts AS SELECT id, title FROM posts`,
		`INSERT INTO posts (id, title, status) VALUES ('p1', 'hello', 'draft')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	app.View(migrate.View{
		Name: "open_posts", Select: "SELECT id, title FROM posts", DependsOn: []string{"posts"},
		Columns: []migrate.Column{{Name: "id", Type: schema.String, PrimaryKey: true}, {Name: "title", Type: schema.String}},
	})
	app.WithAuditLog(framework.AuditConfig{})
	b := New(Config{AllEntities: true, UI: app.EntityUI(entityui.Extensions{})})
	if err := b.Init(app); err != nil {
		t.Fatal(err)
	}
	x := &env{t: t, db: db, app: app, site: site, h: app.Router(), b: b}

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/admin/api/open_posts"},
		{http.MethodPut, "/admin/api/open_posts/p1"},
		{http.MethodPatch, "/admin/api/open_posts/p1"},
		{http.MethodDelete, "/admin/api/open_posts/p1"},
		{http.MethodPost, "/admin/api/open_posts/_bulk"},
		{http.MethodPost, "/admin/api/open_posts/_pick"},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{"title":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		if rr := serve(x.as(theAdmin), req); rr.Code != http.StatusNotFound && rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want no such route", c.method, c.path, rr.Code)
		}
	}
	for _, p := range []string{"/admin", "/admin/entities/open_posts", "/admin/entities/open_posts/p1"} {
		rr := get(x.as(theAdmin), p)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s = %d", p, rr.Code)
		}
		for _, write := range []string{"/admin/entities/open_posts/create", "/admin/api/open_posts"} {
			if strings.Contains(rr.Body.String(), write) {
				t.Errorf("%s offers %s for a read-only view", p, write)
			}
		}
	}
	if !strings.Contains(get(x.as(theAdmin), "/admin/entities/open_posts").Body.String(), "hello") {
		t.Error("the view's list lost its rows")
	}
	// The writable entity beside it keeps its routes.
	if body := get(x.as(theAdmin), "/admin/entities/posts").Body.String(); !strings.Contains(body, "/admin/entities/posts/create") {
		t.Error("posts lost its New link")
	}
}

// Every New link asks one gate: the dashboard, its count fragment and
// the palette agree whichever of read or create the caller is refused.
func TestNewLinkNeedsReadAndCreateEverywhere(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": lockedPosts()}, Config{Entities: []string{"posts"}, DashboardNew: "posts"}, nil)
	const link = "/admin/entities/posts/create"
	for _, denied := range []string{"", "posts:read", "posts:write"} {
		with := func(req *http.Request) *http.Request {
			if denied == "" {
				return req
			}
			return req.WithContext(access.WithDecider(req.Context(), func(_ context.Context, _ []string, p access.Permission, _ access.Ref) access.Decision {
				if p == access.Permission(denied) {
					return access.DecisionDeny
				}
				return access.DecisionAbstain
			}))
		}
		for name, req := range map[string]*http.Request{
			"dashboard": httptest.NewRequest(http.MethodGet, "/admin", nil),
			"count":     httptest.NewRequest(http.MethodGet, "/admin/_count/posts", nil),
			"palette":   newPost("/admin/_palette", url.Values{"q": {""}}),
		} {
			rr := serve(x.as(theAdmin), with(req))
			if got := strings.Contains(rr.Body.String(), link); rr.Code != http.StatusOK || got != (denied == "") {
				t.Errorf("deny %q: %s = %d, offers New=%v", denied, name, rr.Code, got)
			}
		}
	}
}

// listRedis is the least RedisClient a RedisQueue's enqueue, claim, nack,
// replay and list need: lists and hashes in memory.
type listRedis struct {
	lists  map[string][]string
	hashes map[string]map[string]string
}

func (m *listRedis) LPush(_ context.Context, key string, values ...any) error {
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			s = string(v.([]byte))
		}
		m.lists[key] = append([]string{s}, m.lists[key]...)
	}
	return nil
}

func (m *listRedis) RPop(_ context.Context, key string) (string, error) {
	l := m.lists[key]
	if len(l) == 0 {
		return "", queue.ErrRedisEmpty
	}
	m.lists[key] = l[:len(l)-1]
	return l[len(l)-1], nil
}

func (m *listRedis) HGet(_ context.Context, key, field string) (string, error) {
	v, ok := m.hashes[key][field]
	if !ok {
		return "", queue.ErrRedisEmpty
	}
	return v, nil
}

func (m *listRedis) HSet(_ context.Context, key string, values ...any) error {
	if m.hashes[key] == nil {
		m.hashes[key] = map[string]string{}
	}
	for i := 0; i+1 < len(values); i += 2 {
		v, ok := values[i+1].(string)
		if !ok {
			v = string(values[i+1].([]byte))
		}
		m.hashes[key][values[i].(string)] = v
	}
	return nil
}

func (m *listRedis) HGetAll(_ context.Context, key string) (map[string]string, error) {
	return m.hashes[key], nil
}

func (m *listRedis) HDel(_ context.Context, key string, fields ...string) error {
	for _, f := range fields {
		delete(m.hashes[key], f)
	}
	return nil
}

func (m *listRedis) Del(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(m.lists, k)
		delete(m.hashes, k)
	}
	return nil
}

func (m *listRedis) LRange(_ context.Context, key string, start, stop int64) ([]string, error) {
	l := m.lists[key]
	n := int64(len(l))
	if start < 0 {
		start = max(start+n, 0)
	}
	if stop < 0 {
		stop += n
	}
	stop = min(stop, n-1)
	if start > stop {
		return nil, nil
	}
	return slices.Clone(l[start : stop+1]), nil
}

func (m *listRedis) LRem(_ context.Context, key string, _ int64, value any) (int64, error) {
	s, ok := value.(string)
	if !ok {
		s = string(value.([]byte))
	}
	if i := slices.Index(m.lists[key], s); i >= 0 {
		m.lists[key] = slices.Delete(m.lists[key], i, i+1)
		return 1, nil
	}
	return 0, nil
}

// replayBackend is what the replay tests ask of a queue.
type replayBackend interface {
	queue.Queue
	queue.Browsable
	queue.Replayable
}

func replayBackends(t *testing.T) map[string]replayBackend {
	mem := queue.NewMemoryQueue(0)
	red := queue.NewRedisQueue(&listRedis{lists: map[string][]string{}, hashes: map[string]map[string]string{}}, "jobs")
	// Close drops each queue's user-data eraser, which is process-wide.
	t.Cleanup(func() { _, _ = mem.Close(), red.Close() })
	return map[string]replayBackend{"db": newDBQueue(t, newDB(t)), "memory": mem, "redis": red}
}

// Replay answers nil for a job that is not failed; on every backend the
// admin neither audits nor reports that as a replay, and a failed job
// replays once.
func TestReplayOfAJobThatIsNotFailedDoesNothing(t *testing.T) {
	for name, q := range replayBackends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if err := q.Enqueue(ctx, queue.Job{Type: "send.email", MaxAttempts: 1}); err != nil {
				t.Fatal(err)
			}
			job, err := q.Dequeue(ctx)
			if err != nil {
				t.Fatal(err)
			}
			x := queueEnv(t, q)
			nothing := func(id string) {
				t.Helper()
				rr := rpc(x.as(theAdmin), "/admin/queue/_replay/"+id, map[string]any{})
				if rr.Code != http.StatusConflict || strings.Contains(rr.Header().Get("X-Gofastr-Toast"), "success") {
					t.Errorf("replay %s = %d, toast %q", id, rr.Code, rr.Header().Get("X-Gofastr-Toast"))
				}
				if got := resultOf(t, post(x.as(theAdmin), "/admin/queue/_replay/"+id, nil)); got != "replay-nothing" {
					t.Errorf("replay %s result = %q", id, got)
				}
			}
			nothing("no-such-job")
			nothing(job.ID) // claimed, not failed
			if ops := x.auditOps("queue"); len(ops) != 0 {
				t.Fatalf("audit rows for replays that replayed nothing: %v", ops)
			}
			if err := q.Nack(ctx, job); err != nil {
				t.Fatal(err)
			}
			if got := resultOf(t, post(x.as(theAdmin), "/admin/queue/_replay/"+job.ID, nil)); got != "replayed" {
				t.Fatalf("replay of the failed job = %q", got)
			}
			nothing(job.ID) // already replayed
			if ops := x.auditOps("queue"); !slices.Equal(ops, []string{"replay"}) {
				t.Errorf("audit ops = %v, want the one replay", ops)
			}
			if body := get(x.as(theAdmin), "/admin/queue?result=replay-nothing").Body.String(); !strings.Contains(body, "no longer failed") {
				t.Error("the jobs page draws no notice for replay-nothing")
			}
		})
	}
}

// noListQueue fails the test when a single replay reads the job list.
type noListQueue struct {
	*fakeQueue
	t *testing.T
}

func (q noListQueue) ListJobs(context.Context, string, int, int) ([]queue.Job, error) {
	q.t.Error("a single replay read the job list")
	return nil, nil
}

// reportingQueue is noListQueue with queue.ReplayReporter: only the ids
// in failed replay.
type reportingQueue struct {
	noListQueue
	failed map[string]bool
}

func (q reportingQueue) ReplayJob(ctx context.Context, id string) (bool, error) {
	if !q.failed[id] {
		return false, nil
	}
	delete(q.failed, id)
	return true, q.Replay(ctx, id)
}

// One replay is one call to the queue however long the failed list is: a
// failed job older than the newest maxReplayAll still replays, and a
// queue that cannot report replays and audits as it always did.
func TestSingleReplayNeverScansTheFailedList(t *testing.T) {
	for name, withReporter := range map[string]bool{"reporter": true, "replay only": false} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeQueue{}
			var q queue.Browsable = noListQueue{fake, t}
			if withReporter {
				q = reportingQueue{noListQueue{fake, t}, map[string]bool{"job-older-than-the-cap": true}}
			}
			x := queueEnv(t, q)
			rr := rpc(x.as(theAdmin), "/admin/queue/_replay/job-older-than-the-cap", map[string]any{})
			if rr.Code != http.StatusNoContent || !slices.Equal(fake.replayed, []string{"job-older-than-the-cap"}) {
				t.Fatalf("replay = %d, replayed %v", rr.Code, fake.replayed)
			}
			if ops := x.auditOps("queue"); !slices.Equal(ops, []string{"replay"}) {
				t.Errorf("audit ops = %v", ops)
			}
		})
	}
}

// Replay all audits only the jobs it queued: one replayed elsewhere after
// the list was read is skipped without a row.
func TestReplayAllAuditsOnlyWhatItQueued(t *testing.T) {
	fake := &fakeQueue{jobs: failedJobs("j1", "j2", "j3")}
	q := struct {
		*fakeQueue
		queue.ReplayReporter
	}{fake, reporterFunc(func(ctx context.Context, id string) (bool, error) {
		if id == "j2" {
			return false, nil
		}
		return true, fake.Replay(ctx, id)
	})}
	x := queueEnv(t, q)
	if got := resultOf(t, post(x.as(theAdmin), "/admin/queue/_replay_all", nil)); got != "replayed-all" {
		t.Fatalf("result = %q", got)
	}
	if !slices.Equal(fake.replayed, []string{"j1", "j3"}) || len(x.auditOps("queue")) != 2 {
		t.Fatalf("replayed %v, audit %v", fake.replayed, x.auditOps("queue"))
	}
}

type reporterFunc func(context.Context, string) (bool, error)

func (f reporterFunc) ReplayJob(ctx context.Context, id string) (bool, error) { return f(ctx, id) }

// A ?status= that names no status lists All, and says so, as the audit
// page does for a bad ?op=.
func TestUnknownFilterValueWarns(t *testing.T) {
	const warning = "Filter not applied"
	qx := queueEnv(t, &fakeQueue{})
	for path, want := range map[string]bool{
		"/admin/queue?status=Failed": true,
		"/admin/queue?status=failed": false,
		"/admin/queue?status=":       false,
		"/admin/queue":               false,
	} {
		if got := strings.Contains(get(qx.as(theAdmin), path).Body.String(), warning); got != want {
			t.Errorf("%s warns=%v, want %v", path, got, want)
		}
	}
}

// Every op the admin's own pages write on success is one the audit
// filter takes, and a module lever is tinted and drawn by its verb.
func TestAuditOpsCoverWhatTheAdminWrites(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	for _, op := range []string{"set-name", "replay", "grant", "revoke", "assign-roles", opModuleEnable, opModuleDisable, opModuleBump, opModuleRevoke} {
		if _, warned := x.b.parseAuditFilter(url.Values{"op": {op}}); len(warned) != 0 {
			t.Errorf("?op=%s is refused", op)
		}
	}
	for op, want := range map[string]ui.StatusVariant{
		opModuleEnable: ui.StatusSuccess, opModuleDisable: ui.StatusDanger,
		opModuleRevoke: ui.StatusDanger, opModuleBump: ui.StatusInfo,
	} {
		if got := opVariant(op); got != want {
			t.Errorf("opVariant(%s) = %v, want %v", op, got, want)
		}
	}
	if opIcon(opModuleRevoke) != "shield" || opIcon(opModuleBump) != "repeat" {
		t.Errorf("module icons: revoke %q, bump %q", opIcon(opModuleRevoke), opIcon(opModuleBump))
	}
}

// A lever the supervisor refuses is not audited as done and answers the
// module-failed notice: a revoke of a capability the module does not hold
// is such a refusal (framework.TestRevokeGrantsOfAnUnheldGrantErrors).
func TestRefusedModuleLeverIsNotAuditedAsDone(t *testing.T) {
	for _, lever := range []string{"enable", "disable", "bump", "revoke"} {
		t.Run(lever, func(t *testing.T) {
			f := &fakeModules{err: errors.New(`revoke "users:read" covers no held grant`)}
			x := modulesEnv(t, f)
			fields := map[string]any{"module": "billing", "grant": "users:read"}
			rr := rpc(x.as(theAdmin), "/admin/modules/_"+lever, fields)
			if rr.Code != http.StatusConflict || strings.Contains(rr.Header().Get("X-Gofastr-Toast"), "success") || strings.Contains(rr.Body.String(), "users:read") {
				t.Fatalf("refused %s = %d %s, toast %q", lever, rr.Code, rr.Body.String(), rr.Header().Get("X-Gofastr-Toast"))
			}
			if !strings.Contains(rr.Body.String(), "did not accept that change") {
				t.Errorf("refused %s names no reason: %s", lever, rr.Body.String())
			}
			if len(f.calls) != 1 || len(x.auditOps("module")) != 0 {
				t.Errorf("refused %s: calls %v, audit %v", lever, f.calls, x.auditOps("module"))
			}
		})
	}
}
