package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/battery/queue"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// jobsEnv is the full bulk-jobs wiring over the memory queue: a
// permission-gated posts entity, a policy whose "editor" role holds the
// posts permissions, and a mutable user table both the request
// middleware and the runner's principal read roles from.
type jobsEnv struct {
	*env
	q      *queue.MemoryQueue
	policy *access.RolePolicy

	mu    sync.Mutex
	roles map[string][]string
}

// buildJobsEnv builds the app the documented way, up to but excluding
// the admin's Init: NewBulkJobs → EntityUI(Extensions{Jobs}) →
// Config{UI, BulkJobs}. posts are seeded with n rows, the bulk tables
// exist, so a test can seed jobs Init will resume. It builds its own
// app because the shared harness calls app.EntityUI without Extensions,
// and EntityUI runs once per app.
func buildJobsEnv(t *testing.T, posts int) *jobsEnv {
	t.Helper()
	return buildJobsEnvWith(t, posts, lockedPosts())
}

// buildJobsEnvWith is buildJobsEnv over the given posts declaration.
func buildJobsEnvWith(t *testing.T, posts int, postsCfg entity.EntityConfig) *jobsEnv {
	t.Helper()
	policy := access.NewRolePolicy()
	if err := policy.Grant("editor", "posts:read", "posts:write"); err != nil {
		t.Fatal(err)
	}
	j := &jobsEnv{q: queue.NewMemoryQueue(1), policy: policy, roles: map[string][]string{}}
	jobs, err := NewBulkJobs(j.q, j.principal)
	if err != nil {
		t.Fatal(err)
	}
	db := newDB(t)
	if err := framework.EnsureAuditTable(db, ""); err != nil {
		t.Fatalf("audit table: %v", err)
	}
	app := framework.NewUIHostApp(uihost.New(appui.NewApp("admin-test")),
		framework.WithDB(db), framework.WithoutDefaultMiddleware())
	app.Entity("posts", postsCfg)
	// notes is the app's own: the admin does not expose it, but its
	// public bulk route queues on the same runner.
	app.Entity("notes", notesConfig())
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	app.WithAuditLog(framework.AuditConfig{})
	ui := app.EntityUI(entityui.Extensions{Jobs: jobs})
	b := New(Config{Entities: []string{"posts"}, UI: ui, BulkJobs: jobs})
	j.env = &env{t: t, db: db, app: app, b: b, h: app.Router()}
	t.Cleanup(func() { _ = j.q.Close() })
	for i := range posts {
		j.env.insert("posts", map[string]any{"id": fmt.Sprintf("p%03d", i), "title": fmt.Sprintf("Post %d", i), "status": "draft"})
	}
	return j
}

// init runs the admin's Init; separate so a test can seed state first.
func (j *jobsEnv) init(t *testing.T) {
	t.Helper()
	if err := j.env.b.Init(j.env.app); err != nil {
		t.Fatalf("admin init: %v", err)
	}
}

// newJobsEnv is buildJobsEnv plus Init.
func newJobsEnv(t *testing.T, posts int) *jobsEnv {
	t.Helper()
	j := buildJobsEnv(t, posts)
	j.init(t)
	return j
}

func (j *jobsEnv) principal(ctx context.Context, userID, tenantID string) (context.Context, error) {
	roles, ok := j.principalRolesOK(userID)
	if !ok {
		return nil, fmt.Errorf("no user %q", userID)
	}
	c := handler.SetUser(ctx, roleUser{id: userID, roles: roles})
	c = access.WithPolicy(c, j.policy)
	c = access.WithRoles(c, roles)
	if tenantID != "" {
		c = tenant.SetTenantID(c, tenantID)
	}
	return c, nil
}

// asUser serves with user id's current roles, the app's middleware
// shape: user + policy + roles.
func (j *jobsEnv) asUser(id string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		roles, _ := j.principalRolesOK(id)
		ctx := r.Context()
		if roles != nil {
			ctx = handler.SetUser(ctx, roleUser{id: id, roles: roles})
		}
		ctx = access.WithPolicy(ctx, j.policy)
		ctx = access.WithRoles(ctx, roles)
		j.h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// setRoles points a user id at fresh roles.
func (j *jobsEnv) setRoles(id string, roles ...string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.roles[id] = roles
}

// forgetUser removes a user id, the shape of a deleted account.
func (j *jobsEnv) forgetUser(id string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.roles, id)
}

// principalRolesOK reads a user's current roles under the lock.
func (j *jobsEnv) principalRolesOK(id string) ([]string, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	roles, ok := j.roles[id]
	return roles, ok
}

// confirmBulk posts a delete of the first n posts through the admin's
// bulk route and returns the answer's status and decoded body.
func (j *jobsEnv) confirmBulk(t *testing.T, id string, n int) (int, map[string]any) {
	t.Helper()
	ids := make([]string, n)
	for i := range n {
		ids[i] = fmt.Sprintf("p%03d", i)
	}
	body, _ := json.Marshal(map[string]any{"action": "delete", "scope": "selected", "ids": ids})
	req := httptest.NewRequest(http.MethodPost, "/admin/api/posts/_bulk", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := serve(j.asUser(id), req)
	out := map[string]any{}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	return rr.Code, out
}

func (j *jobsEnv) countPosts(t *testing.T) int {
	t.Helper()
	var n int
	if err := j.db.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// bulkJobStatus reads the snapshot store's row for a job id.
func (j *jobsEnv) bulkJobStatus(t *testing.T, id string) string {
	t.Helper()
	var status string
	if err := j.db.QueryRow(`SELECT status FROM gofastr_bulk_jobs WHERE id = ?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A selection over InRequestCap confirmed through the bulk route is
// queued, never run inline; draining the queue runs it to done and the
// writes land through the app's own CRUD handler (audit rows and all).
func TestBulkOverCapQueuedThenRunByDrain(t *testing.T) {
	j := newJobsEnv(t, 120)
	j.setRoles("u1", "admin", "editor")

	code, out := j.confirmBulk(t, "u1", 120)
	if code != http.StatusAccepted {
		t.Fatalf("confirm = %d %v, want 202", code, out)
	}
	jobID, _ := out["job"].(string)
	if jobID == "" || out["count"] != float64(120) {
		t.Fatalf("confirm answered %v, want a job over 120 rows", out)
	}
	// Not run inline: every row is still there, the job is queued.
	if n := j.countPosts(t); n != 120 {
		t.Fatalf("SECURITY: the request itself deleted %d of 120 rows; the run must be outside the request", 120-n)
	}
	if s := j.bulkJobStatus(t, jobID); s != entityui.BulkQueued {
		t.Fatalf("job status = %q, want queued before the drain", s)
	}

	j.q.Start()
	waitFor(t, "the drained job to finish", func() bool { return j.bulkJobStatus(t, jobID) == entityui.BulkDone })
	if n := j.countPosts(t); n != 0 {
		t.Fatalf("the drained run left %d rows", n)
	}
	deletes := 0
	for _, op := range j.auditOps("posts") {
		if op == "delete" {
			deletes++
		}
	}
	if deletes != 120 {
		t.Fatalf("audit wrote %d delete rows, want one per record", deletes)
	}
}

// A named entity with CRUD off is written only through the admin's own
// routes. A queued run over it runs through the same UI the bulk route
// served, so it finishes instead of stopping on "no write routes".
func TestBulkQueuedRunsOnCRUDOffEntity(t *testing.T) {
	cfg := lockedPosts()
	no := false
	cfg.Exposure.CRUD = &no
	j := buildJobsEnvWith(t, 120, cfg)
	j.init(t)
	j.setRoles("u1", "admin", "editor")

	code, out := j.confirmBulk(t, "u1", 120)
	if code != http.StatusAccepted {
		t.Fatalf("confirm = %d %v, want 202", code, out)
	}
	jobID, _ := out["job"].(string)
	j.q.Start()
	waitFor(t, "the drained job to settle", func() bool { return j.bulkJobStatus(t, jobID) != entityui.BulkQueued })
	if s := j.bulkJobStatus(t, jobID); s != entityui.BulkDone {
		t.Fatalf("job status = %q, want done: the runner refused a run the bulk route accepted", s)
	}
	if n := j.countPosts(t); n != 0 {
		t.Fatalf("the drained run left %d rows", n)
	}
}

// The app's own bulk route queues on the runner the admin binds: a run
// over an entity the admin does not expose still finishes under the
// app's REST routes.
func TestBulkQueuedRunsOnAppOnlyEntity(t *testing.T) {
	j := newJobsEnv(t, 0)
	j.setRoles("u1", "editor")
	ids := make([]string, 120)
	for i := range ids {
		ids[i] = fmt.Sprintf("n%03d", i)
		j.env.insert("notes", map[string]any{"id": ids[i], "text": "note"})
	}
	body, _ := json.Marshal(map[string]any{"action": "delete", "scope": "selected", "ids": ids})
	req := httptest.NewRequest(http.MethodPost, "/notes/_bulk", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := serve(j.asUser("u1"), req)
	out := map[string]any{}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("confirm = %d %v, want 202", rr.Code, out)
	}
	jobID, _ := out["job"].(string)
	j.q.Start()
	waitFor(t, "the drained job to settle", func() bool { return j.bulkJobStatus(t, jobID) != entityui.BulkQueued })
	if s := j.bulkJobStatus(t, jobID); s != entityui.BulkDone {
		t.Fatalf("job status = %q, want done", s)
	}
	var n int
	if err := j.db.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("the drained run left %d notes", n)
	}
}

// Enqueue before Bind answers an error and enqueues nothing;
// NewBulkJobs refuses a missing queue or principal.
func TestBulkJobsUnboundRefusals(t *testing.T) {
	if _, err := NewBulkJobs(nil, func(context.Context, string, string) (context.Context, error) { return nil, nil }); err == nil {
		t.Error("NewBulkJobs accepted a nil queue")
	}
	q := queue.NewMemoryQueue(1)
	t.Cleanup(func() { _ = q.Close() })
	if _, err := NewBulkJobs(q, nil); err == nil {
		t.Error("NewBulkJobs accepted a nil principal")
	}
	jobs, err := NewBulkJobs(q, func(context.Context, string, string) (context.Context, error) {
		return context.Background(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Enqueue(context.Background(), entityui.BulkJob{ID: "j1", Entity: "posts"}); !errors.Is(err, errBulkJobsUnbound) {
		t.Fatalf("Enqueue before Bind = %v, want the unbound error", err)
	}
	// The queue never saw it.
	if stats, _ := q.Stats(context.Background()); len(stats) != 0 {
		t.Errorf("an unbound Enqueue left %d statuses behind", len(stats))
	}
}

// The queue payload decodes strictly: an unknown field, an absent id or
// a non-object is an error, never a zero-value run.
func TestBulkJobPayloadDecodesStrictly(t *testing.T) {
	j := newJobsEnv(t, 1)
	j.setRoles("u1", "admin", "editor")
	for name, payload := range map[string]string{
		"unknown field": `{"id":"j1","ids":["p000"]}`,
		"no id":         `{}`,
		"not an object": `["j1"]`,
	} {
		err := j.env.b.cfg.BulkJobs.run(context.Background(), queue.Job{Type: bulkJobType, Payload: []byte(payload)})
		if err == nil {
			t.Errorf("%s: the handler accepted the payload", name)
		}
	}
}

// AuthPrincipal loads the user fresh: current roles, a deleted user an
// error, the tenant stamped, a manager without a user store an error.
func TestAuthPrincipalBuildsCurrentContext(t *testing.T) {
	store := &stubUserStore{users: map[string]*auth.BasicUser{
		"u1": {ID: "u1", Email: "u1@example.com", Roles: []string{"admin"}},
	}}
	am := auth.New(auth.AuthConfig{JWTSecret: "test-secret", UserStore: store})
	p := AuthPrincipal(am, AnyTenant)

	store.setRoles("u1", "editor")
	ctx, err := p(context.Background(), "u1", "t9")
	if err != nil {
		t.Fatal(err)
	}
	u, ok := handler.GetUser(ctx)
	if !ok {
		t.Fatal("no user on the rebuilt context")
	}
	if id := u.(interface{ GetID() string }).GetID(); id != "u1" {
		t.Fatalf("ctx user = %s", id)
	}
	if roles := access.GetRoles(ctx); len(roles) != 1 || roles[0] != "editor" {
		t.Fatalf("ctx roles = %v, want the CURRENT roles [editor]", roles)
	}
	if tenant.GetTenantID(ctx) != "t9" {
		t.Fatalf("ctx tenant = %q", tenant.GetTenantID(ctx))
	}

	store.remove("u1")
	if _, err := p(context.Background(), "u1", ""); err == nil {
		t.Fatal("a deleted user rebuilt a context; the run must stop")
	}
	if _, err := AuthPrincipal(nil, AnyTenant)(context.Background(), "u1", ""); err == nil {
		t.Fatal("a nil manager rebuilt a context")
	}
}

// A tenant run checks the creator still belongs to the tenant before
// every chunk: a removed member, a lookup error and a principal with no
// membership check all stop it. Roles were re-read per chunk while the
// tenant was stamped as given, so a bulk job kept writing tenant A's
// rows after its creator left A.
func TestAuthPrincipalChecksTenantMember(t *testing.T) {
	store := &stubUserStore{users: map[string]*auth.BasicUser{
		"u1": {ID: "u1", Email: "u1@example.com", Roles: []string{"admin"}},
	}}
	am := auth.New(auth.AuthConfig{JWTSecret: "test-secret", UserStore: store})
	var members sync.Map
	members.Store("u1/t9", true)
	p := AuthPrincipal(am, func(_ context.Context, userID, tenantID string) (bool, error) {
		if tenantID == "broken" {
			// true beside the error: the error alone must stop the run.
			return true, errors.New("membership store down")
		}
		_, ok := members.Load(userID + "/" + tenantID)
		return ok, nil
	})

	ctx, err := p(context.Background(), "u1", "t9")
	if err != nil {
		t.Fatalf("a member was refused: %v", err)
	}
	if tenant.GetTenantID(ctx) != "t9" {
		t.Fatalf("ctx tenant = %q", tenant.GetTenantID(ctx))
	}
	members.Delete("u1/t9")
	if _, err := p(context.Background(), "u1", "t9"); err == nil {
		t.Fatal("SECURITY: [tenant] a creator removed from the tenant kept running its bulk job")
	}
	if _, err := p(context.Background(), "u1", "broken"); err == nil {
		t.Fatal("a failed membership lookup admitted the run")
	}
	if _, err := p(context.Background(), "u1", ""); err != nil {
		t.Fatalf("a run with no tenant asked for membership: %v", err)
	}
	if _, err := AuthPrincipal(am, nil)(context.Background(), "u1", "t9"); err == nil {
		t.Fatal("SECURITY: [tenant] a principal with no membership check stamped tenant t9")
	}
	if _, err := AuthPrincipal(am, nil)(context.Background(), "u1", ""); err != nil {
		t.Fatalf("a run with no tenant needs no membership check: %v", err)
	}
}

// A job a crash left between its snapshot and its Enqueue is handed
// over by the admin's Init (Bind, then ResumeBulkJobs) and runs when
// the queue drains.
func TestBulkInitResumesOrphanJob(t *testing.T) {
	j := buildJobsEnv(t, 2)
	j.setRoles("u1", "admin", "editor")
	// An orphan: written to the snapshot store minutes ago, never
	// marked enqueued, its two ids unsettled.
	old := time.Now().UTC().Add(-2 * time.Minute)
	j.db.Exec(`INSERT INTO gofastr_bulk_jobs (id, entity, action, count, creator, tenant, filter_hash, run_key, status, enqueued, created_at)
		VALUES ('orphan1', 'posts', 'delete', 2, 'u1', '', 'h', 'k', 'queued', 0, ?)`, old)
	j.db.Exec(`INSERT INTO gofastr_bulk_items (job_id, record_id, seq) VALUES ('orphan1', 'p000', 0), ('orphan1', 'p001', 1)`)

	j.init(t)
	j.q.Start()
	waitFor(t, "the resumed orphan to finish", func() bool { return j.bulkJobStatus(t, "orphan1") == entityui.BulkDone })
	if n := j.countPosts(t); n != 0 {
		t.Fatalf("the resumed run left %d rows", n)
	}
}

// stubUserStore is the UserStore slice AuthPrincipal reads, with roles
// mutable between calls.
type stubUserStore struct {
	mu    sync.Mutex
	users map[string]*auth.BasicUser
}

func (s *stubUserStore) setRoles(id string, roles ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[id]; ok {
		u.Roles = roles
	}
}

func (s *stubUserStore) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, id)
}

func (s *stubUserStore) FindByEmail(_ context.Context, email string) (auth.User, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[email]; ok {
		return u, "", nil
	}
	return nil, "", auth.ErrUserNotFound
}

func (s *stubUserStore) FindByID(_ context.Context, id string) (auth.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[id]; ok {
		return u, nil
	}
	return nil, auth.ErrUserNotFound
}

func (s *stubUserStore) CreateUser(_ context.Context, email, _ string, _ []string) (auth.User, error) {
	return nil, auth.ErrEmailTaken
}

func (s *stubUserStore) UpdateRoles(_ context.Context, _ string, _ []string) error {
	return auth.ErrUserNotFound
}
