package a2a

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Skill is one thing the agent can do, invoked by name.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`

	// Handler runs the skill for one message. Nil is a NewServer error.
	Handler Handler `json:"-"`
}

// Handler does the work of a skill for one inbound message. Returning nil
// with the task in progress completes it; an error fails it, and the client
// sees a returned *Error's message or else "skill handler failed".
type Handler func(ctx context.Context, t TaskContext) error

// TaskContext is a handler's view of, and control over, its task. Every
// mutating call persists the task, then notifies subscribers and push
// configs, before it returns.
type TaskContext interface {
	// Task returns a snapshot of the task as currently persisted.
	Task() *Task
	// Message returns the message this run was started or resumed with.
	Message() *Message
	// Owner returns the principal that owns the task (Config.Owner's
	// value for the request that created it).
	Owner() string
	// Request returns the HTTP request that started or resumed this run, for
	// handlers that re-dispatch into the app with the caller's credentials.
	// Its Context is not the handler's context.
	Request() *http.Request

	// Working moves the task to TASK_STATE_WORKING with an optional agent
	// status message (nil parts: no message). Idempotent once working.
	Working(parts ...Part) error
	// Artifact records an artifact and emits an artifact-update event. One
	// with an existing ArtifactID replaces it, or with append extends it.
	Artifact(a Artifact, append bool) error
	// Complete ends the task in TASK_STATE_COMPLETED with an optional
	// final agent message.
	Complete(parts ...Part) error
	// Fail ends the task in TASK_STATE_FAILED.
	Fail(parts ...Part) error
	// Reject ends the task in TASK_STATE_REJECTED: the agent will not do
	// this work (unknown skill, bad input).
	Reject(parts ...Part) error
	// RequireInput pauses the task in TASK_STATE_INPUT_REQUIRED; the next
	// SendMessage to the task runs the handler again.
	RequireInput(parts ...Part) error
	// RequireAuth pauses the task in TASK_STATE_AUTH_REQUIRED, the same
	// way as RequireInput.
	RequireAuth(parts ...Part) error
}

// Router picks the skill for a message. Nil in Config means DefaultRouter.
type Router func(ctx context.Context, msg *Message, skills []Skill) (skillID string, err error)

// TaskRecord is a task as the Store holds it: the wire Task plus the
// bookkeeping the server needs and never sends.
type TaskRecord struct {
	Task    Task
	Owner   string
	SkillID string
	// Scope is the scope set of the credential that created the task, in the
	// canonical form ScopeKey writes: "" for an unscoped caller (a session,
	// server code), a JSON array otherwise.
	Scope string
	// Tenant is the tenant the creating request acted in (Config.Tenant), ""
	// for none.
	Tenant string
	// Version is bumped on every UpdateTask; an update whose Version does
	// not match the stored row fails with ErrConflict.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PushConfigRecord is a push-notification config as the Store holds it.
type PushConfigRecord struct {
	Config    PushNotificationConfig
	Owner     string
	CreatedAt time.Time
}

// ListQuery is the store-level shape of ListTasks.
type ListQuery struct {
	ContextID string
	Status    TaskState
	// After filters to tasks whose status timestamp is after this instant
	// (zero: no filter).
	After time.Time
	// Scoped filters to tasks whose Scope equals Scope: the tasks a scoped
	// caller may see.
	Scoped bool
	Scope  string
	// Tenanted filters to tasks whose Tenant equals Tenant ("" included: a
	// tenant-less caller sees only tenant-less tasks).
	Tenanted bool
	Tenant   string
	Limit    int
	Offset   int
}

// Store persists tasks and push configs. Every call is scoped by owner and
// never returns or touches another owner's rows.
type Store interface {
	CreateTask(ctx context.Context, rec *TaskRecord) error
	// GetTask returns ErrNotFound for an unknown id OR another owner's task.
	GetTask(ctx context.Context, owner, id string) (*TaskRecord, error)
	// UpdateTask persists rec if rec.Version matches the stored row, then
	// increments rec.Version; otherwise ErrConflict.
	UpdateTask(ctx context.Context, rec *TaskRecord) error
	// ListTasks returns the owner's tasks matching q, newest status first,
	// plus the total match count before Limit/Offset.
	ListTasks(ctx context.Context, owner string, q ListQuery) (recs []*TaskRecord, total int, err error)

	CreatePushConfig(ctx context.Context, rec *PushConfigRecord) error
	GetPushConfig(ctx context.Context, owner, taskID, id string) (*PushConfigRecord, error)
	ListPushConfigs(ctx context.Context, owner, taskID string) ([]*PushConfigRecord, error)
	DeletePushConfig(ctx context.Context, owner, taskID, id string) error
}

// ScopeFilter is the optional Store capability that says ListTasks applies
// ListQuery.Scoped and Scope in its query, so its page and total count only a
// scoped caller's tasks.
type ScopeFilter interface {
	FiltersByScope()
}

// TenantFilter is the optional Store capability that says ListTasks applies
// ListQuery.Tenanted and Tenant in its query.
type TenantFilter interface {
	FiltersByTenant()
}

// RetentionTier is the set of one owner's tasks a retention bound counts
// within: the tasks made in one tenant under one scope set, i.e. whose
// TaskRecord.Tenant and TaskRecord.Scope equal these.
type RetentionTier struct {
	Tenant string
	Scope  string
}

// TierOf is the retention tier rec belongs to.
func TierOf(rec *TaskRecord) RetentionTier {
	return RetentionTier{Tenant: rec.Tenant, Scope: rec.Scope}
}

func (t RetentionTier) holds(rec *TaskRecord) bool {
	return rec.Tenant == t.Tenant && rec.Scope == t.Scope
}

// RetentionTrimmer is the optional Store capability that bounds the rows
// cheap authenticated writes mint.
type RetentionTrimmer interface {
	// TrimTerminalTasks deletes the OLDEST terminal task rows of owner in
	// tier (the rows whose Tenant and Scope equal tier's) until at most keep
	// remain in that tier. keep < 0 is a no-op.
	TrimTerminalTasks(ctx context.Context, owner string, tier RetentionTier, keep int) error
	// TrimPushConfigs deletes the OLDEST push-notification configs registered
	// for (owner, taskID) until at most keep remain. keep < 0 is a no-op.
	TrimPushConfigs(ctx context.Context, owner, taskID string, keep int) error
}

// Store errors, returned or wrapped by Store implementations.
var (
	ErrNotFound = errors.New("a2a: not found")
	ErrConflict = errors.New("a2a: version conflict")
)

// Capabilities is what the server advertises; the agent card copies it.
type Capabilities struct {
	Streaming         bool `json:"streaming"`
	PushNotifications bool `json:"pushNotifications"`
	ExtendedAgentCard bool `json:"extendedAgentCard,omitempty"`
}

type requestKey struct{}

// WithRequest returns a context carrying r, for TaskContext.Request.
func WithRequest(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, requestKey{}, r)
}

// RequestFromContext returns the request stashed by WithRequest.
func RequestFromContext(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(requestKey{}).(*http.Request)
	return r, ok && r != nil
}
