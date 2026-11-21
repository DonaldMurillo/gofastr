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

const bulkJobType = "entityui.bulk"

const bulkJobMaxAttempts = 5

var errBulkJobsUnbound = errors.New("admin: BulkJobs is not bound to the entity UI yet; pass it as admin.Config.BulkJobs so Init binds it")

// PrincipalFunc rebuilds userID's request context with their current roles in
// tenant, or errors to stop the run.
type PrincipalFunc func(ctx context.Context, userID, tenant string) (context.Context, error)

type handlerQueue interface {
	queue.Queue
	RegisterHandler(jobType string, h queue.Handler)
}

// BulkJobs is an entityui.JobRunner on a battery/queue backend.
type BulkJobs struct {
	q         handlerQueue
	principal PrincipalFunc

	mu    sync.RWMutex
	ui    *entityui.UI
	admit func(context.Context) context.Context
}

// NewBulkJobs builds the runner; it errors without a queue or a principal.
func NewBulkJobs(q handlerQueue, principal PrincipalFunc) (*BulkJobs, error) {
	if q == nil {
		return nil, errors.New("admin: NewBulkJobs needs a queue (queue.NewDBQueue or queue.NewMemoryQueue)")
	}
	if principal == nil {
		return nil, errors.New("admin: NewBulkJobs needs a principal that rebuilds the confirming user's context (admin.AuthPrincipal)")
	}
	return &BulkJobs{q: q, principal: principal}, nil
}

// Bind registers the queue handler that runs queued bulk jobs through u. A nil
// u is a no-op.
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

func (j *BulkJobs) bound() *entityui.UI {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.ui
}

// Enqueue implements entityui.JobRunner; it errors before Bind.
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
		UserID:      job.Creator,
	})
}

// Principal implements entityui.JobRunner: the creator's rebuilt context,
// passed through the admin's gate.
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

func (j *BulkJobs) setAdmit(admit func(context.Context) context.Context) {
	j.mu.Lock()
	j.admit = admit
	j.mu.Unlock()
}

type bulkJobPayload struct {
	ID string `json:"id"`
}

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
		if err := j.q.Enqueue(ctx, queue.Job{
			Type:        bulkJobType,
			Payload:     job.Payload,
			MaxAttempts: bulkJobMaxAttempts,
			ScheduledAt: time.Now().Add(entityui.BulkJobLease),
			UserID:      job.UserID,
		}); err != nil {
			return fmt.Errorf("admin: reschedule bulk job %s: %w", p.ID, err)
		}
		return nil
	default:
		return fmt.Errorf("admin: bulk job %s: %w", p.ID, err)
	}
}

// TenantMember reports whether userID still belongs to tenantID.
type TenantMember func(ctx context.Context, userID, tenantID string) (bool, error)

// AnyTenant is the TenantMember that admits every run in the tenant it was
// confirmed in.
func AnyTenant(context.Context, string, string) (bool, error) { return true, nil }

// AuthPrincipal builds the PrincipalFunc for battery/auth, checking tenant
// membership through member.
func AuthPrincipal(am *auth.AuthManager, member TenantMember) PrincipalFunc {
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
			if member == nil {
				return nil, fmt.Errorf("admin: AuthPrincipal has no TenantMember to check user %q in tenant %q", userID, tenantID)
			}
			ok, err := member(ctx, userID, tenantID)
			if err != nil {
				return nil, fmt.Errorf("admin: check user %q in tenant %q for a bulk job: %w", userID, tenantID, err)
			}
			if !ok {
				return nil, fmt.Errorf("admin: user %q no longer belongs to tenant %q", userID, tenantID)
			}
			c = tenant.SetTenantID(c, tenantID)
		}
		return c, nil
	}
}
