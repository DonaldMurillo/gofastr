package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// Bulk jobs: an entityui.JobRunner on battery/queue, so a bulk action
// over more than entityui.InRequestCap records runs outside the request.
//
// The wiring order is NewBulkJobs → app.EntityUI(Extensions{Jobs: jobs})
// → admin.Config{UI, BulkJobs: jobs}. The runner must exist before
// EntityUI (the UI refuses a Jobs-less host's over-cap selection), and
// the UI must exist before the runner can run anything, so Bind closes
// the cycle once the admin has both: it registers the queue handler for
// the job type and the admin's Init follows it with ResumeBulkJobs, so
// a job whose Enqueue was lost to a crash is handed over again.

// bulkJobType is the queue job type a bound BulkJobs runs.
const bulkJobType = "entityui.bulk"

// bulkJobMaxAttempts is how many times the queue hands one queued bulk
// job to the handler before it dead-letters. A run resumes where it left
// off (settled rows never re-run), so a handful of retries covers a busy
// lease, a lost lease after a crash, and a transient database failure.
const bulkJobMaxAttempts = 5

// bulkResumeGrace is how old an unenqueued bulk job must be before the
// admin's Init hands it over again, the same window App.EntityUI uses at
// start: a younger job may belong to a confirm still between writing its
// snapshot and enqueuing it.
const bulkResumeGrace = time.Minute

// errBulkJobsUnbound answers an Enqueue (or a queued run) before Bind:
// the job stays queued in the snapshot store, so ResumeBulkJobs picks it
// up once the runner is bound.
var errBulkJobsUnbound = errors.New("admin: BulkJobs is not bound to the entity UI yet; pass it as admin.Config.BulkJobs so Init binds it")

// PrincipalFunc rebuilds one user's request context as of now: the user,
// their CURRENT roles and the tenant, read fresh, the way a request from
// them would carry them. RunBulkJob calls it before every chunk; an
// error stops the run, so a creator who is gone, or whose roles no
// longer admit the action, runs nothing. See AuthPrincipal for
// battery/auth.
type PrincipalFunc func(ctx context.Context, userID, tenant string) (context.Context, error)

// handlerQueue is the slice of battery/queue a BulkJobs runs on: a
// queue whose worker pool takes a handler for a job type. Both DBQueue
// and MemoryQueue implement it.
type handlerQueue interface {
	queue.Queue
	RegisterHandler(jobType string, h queue.Handler)
}

// BulkJobs is entityui.JobRunner backed by a battery/queue backend. The
// queue payload carries only the job id: the confirmed selection lives
// in the host's snapshot store, and no roles or record ids ride along.
type BulkJobs struct {
	q         handlerQueue
	principal PrincipalFunc

	mu sync.RWMutex
	ui *entityui.UI // set by Bind
	// admit is the admin's gate, run on every rebuilt context: set by
	// the admin's Init before Bind.
	admit func(context.Context) context.Context
}

// NewBulkJobs builds the runner. principal is required: it is how the
// run re-authenticates the confirming user for every chunk, and a runner
// that cannot rebuild a creator's context must refuse to start rather
// than run bulk writes under nobody's identity.
func NewBulkJobs(q handlerQueue, principal PrincipalFunc) (*BulkJobs, error) {
	if q == nil {
		return nil, errors.New("admin: NewBulkJobs needs a queue (queue.NewDBQueue or queue.NewMemoryQueue)")
	}
	if principal == nil {
		return nil, errors.New("admin: NewBulkJobs needs a principal that rebuilds the confirming user's context (admin.AuthPrincipal)")
	}
	return &BulkJobs{q: q, principal: principal}, nil
}

// Bind registers the queue handler that runs queued bulk jobs through
// u, closing the NewBulkJobs → EntityUI → Config cycle. Call it once;
// the admin's Init does (Config.BulkJobs). A nil u is a no-op: Enqueue
// keeps answering errBulkJobsUnbound and ResumeBulkJobs hands the jobs
// to a later bound runner.
func (j *BulkJobs) Bind(u *entityui.UI) {
	if u == nil {
		return
	}
	j.mu.Lock()
	j.ui = u
	j.mu.Unlock()
	j.q.RegisterHandler(bulkJobType, func(ctx context.Context, job queue.Job) error {
		return j.run(ctx, job)
	})
}

// bound returns the UI Bind registered, or nil.
func (j *BulkJobs) bound() *entityui.UI {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.ui
}

// Enqueue implements entityui.JobRunner. It schedules the job and runs
// nothing inline; the queue's worker calls RunBulkJob, as often as it
// retries. Before Bind it answers an error and leaves the job
// unenqueued, so ResumeBulkJobs picks it up.
func (j *BulkJobs) Enqueue(ctx context.Context, job entityui.BulkJob) error {
	if j.bound() == nil {
		return errBulkJobsUnbound
	}
	payload, err := json.Marshal(bulkJobPayload{ID: job.ID})
	if err != nil {
		return fmt.Errorf("admin: bulk job payload: %w", err)
	}
	return j.q.Enqueue(ctx, queue.Job{
		Type:        bulkJobType,
		Payload:     payload,
		MaxAttempts: bulkJobMaxAttempts,
		// The payload holds only a job id, but the run it drives acts on
		// the creator's data: erasing the user drops their pending runs
		// with it instead of dead-lettering them later.
		UserID: job.Creator,
	})
}

// Principal implements entityui.JobRunner: it hands principal the job's
// creator and tenant, the only identity the queue job carries, then runs
// the admin's gate on the rebuilt context, as a request to the admin's
// bulk route would: a creator who passes it now runs elevated, the way
// the admin's own screens write; one who no longer does runs as a plain
// caller and passes only their own roles' gates.
func (j *BulkJobs) Principal(ctx context.Context, job entityui.BulkJob) (context.Context, error) {
	c, err := j.principal(ctx, job.Creator, job.Tenant)
	if err != nil {
		return nil, err
	}
	j.mu.RLock()
	admit := j.admit
	j.mu.RUnlock()
	if admit != nil {
		c = admit(c)
	}
	return c, nil
}

// setAdmit installs the admin's gate; the admin's Init calls it.
func (j *BulkJobs) setAdmit(admit func(context.Context) context.Context) {
	j.mu.Lock()
	j.admit = admit
	j.mu.Unlock()
}

// bulkJobPayload is all a queued job carries.
type bulkJobPayload struct {
	ID string `json:"id"`
}

// run is the queue handler for one queued bulk job. The payload decodes
// strictly: anything else is an error, never a zero-value id. A busy or
// lost lease is returned for the queue to retry; every other error runs
// the queue's own retry and dead-letter path.
func (j *BulkJobs) run(ctx context.Context, job queue.Job) error {
	u := j.bound()
	if u == nil {
		return errBulkJobsUnbound
	}
	var p bulkJobPayload
	if err := handler.DecodeStrict(bytes.NewReader(job.Payload), &p); err != nil {
		return fmt.Errorf("admin: bulk job payload: %w", err)
	}
	if p.ID == "" {
		return errors.New("admin: bulk job payload carries no id")
	}
	err := u.RunBulkJob(ctx, p.ID)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, entityui.ErrBulkJobBusy), errors.Is(err, entityui.ErrBulkLeaseLost):
		return err
	default:
		return fmt.Errorf("admin: bulk job %s: %w", p.ID, err)
	}
}

// AuthPrincipal builds the PrincipalFunc for battery/auth: it loads the
// user by id through the manager's UserStore — a user who is gone is an
// error, so the run stops — installs them as the context's user with
// their CURRENT roles (as the user and via access.WithRoles, so both the
// User-reading gates and the permission-reading gates see them), and
// stamps the tenant. The admin adds Config.Policy when the context has
// no policy of its own.
func AuthPrincipal(am *auth.AuthManager) PrincipalFunc {
	return func(ctx context.Context, userID, tenantID string) (context.Context, error) {
		if am == nil || am.UserStore() == nil {
			return nil, errors.New("admin: AuthPrincipal needs the auth manager's user store")
		}
		u, err := am.UserStore().FindByID(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("admin: load user %q for a bulk job: %w", userID, err)
		}
		c := handler.SetUser(ctx, u)
		c = access.WithRoles(c, u.GetRoles())
		if tenantID != "" {
			c = tenant.SetTenantID(c, tenantID)
		}
		return c, nil
	}
}
