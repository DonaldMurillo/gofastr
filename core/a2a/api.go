package a2a

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Skill is one thing the agent can do, invoked by name. The descriptive
// fields are exactly the AgentSkill fields of the agent card (spec §8),
// so the card and the server list one set of skills from one source.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`

	// Handler runs the skill for one message. Nil is a build error.
	Handler Handler `json:"-"`
}

// Handler does the work of a skill for one inbound message. It reports
// progress and results through the TaskContext; returning nil with the
// task still in progress completes it, returning an error fails it (a
// context cancellation cancels it instead). A returned *Error keeps its
// code and message; any other error is logged and the client sees the
// generic "skill handler failed", so a wrapped database or network error
// never reaches a caller. Fail(TextPart(...)) is the way to say why.
type Handler func(ctx context.Context, t TaskContext) error

// TaskContext is a handler's view of, and control over, its task. Every
// mutating call persists the task through the Store and fans the
// corresponding event out to streaming subscribers and push configs
// before returning, so a crash after the call cannot lose what the client
// was already told.
type TaskContext interface {
	// Task returns a snapshot of the task as currently persisted.
	Task() *Task
	// Message returns the message this run was started or resumed with.
	Message() *Message
	// Owner returns the principal that owns the task (Config.Owner's
	// value for the request that created it).
	Owner() string
	// Request returns the HTTP request that started or resumed this run,
	// for handlers that re-dispatch into the app with the caller's
	// credentials. Its Context is not the handler's context.
	Request() *http.Request

	// Working moves the task to TASK_STATE_WORKING with an optional agent
	// status message (nil parts: no message). Idempotent once working.
	Working(parts ...Part) error
	// Artifact records an artifact and emits an artifact-update event. An
	// artifact whose ArtifactID matches an existing one replaces it unless
	// append is set, in which case its parts are appended.
	Artifact(a Artifact, append bool) error
	// Complete ends the task in TASK_STATE_COMPLETED with an optional
	// final agent message.
	Complete(parts ...Part) error
	// Fail ends the task in TASK_STATE_FAILED.
	Fail(parts ...Part) error
	// Reject ends the task in TASK_STATE_REJECTED: the agent will not do
	// this work (unknown skill, bad input).
	Reject(parts ...Part) error
	// RequireInput pauses the task in TASK_STATE_INPUT_REQUIRED. The next
	// SendMessage addressed to the task resumes it: the handler runs again
	// with the new message and the task's history.
	RequireInput(parts ...Part) error
	// RequireAuth pauses the task in TASK_STATE_AUTH_REQUIRED, the same
	// way as RequireInput.
	RequireAuth(parts ...Part) error
}

// Router picks the skill for a message. The default reads
// msg.Metadata["skill"], then the first data part carrying a "skill"
// key, then the only skill when exactly one is registered, and otherwise
// rejects the task with a message naming the available skill ids.
type Router func(ctx context.Context, msg *Message, skills []Skill) (skillID string, err error)

// TaskRecord is a task as the Store holds it: the wire Task plus the
// bookkeeping the server needs and never sends.
type TaskRecord struct {
	Task    Task
	Owner   string
	SkillID string
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
	After  time.Time
	Limit  int
	Offset int
}

// Store persists tasks and push configs. Every read and write is scoped
// by owner: a task belongs to the principal that created it, and no call
// returns or touches another owner's rows. Implementations are safe for
// concurrent use and shared across replicas (the SQL store); the memory
// store is per process.
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

// Store errors. Implementations return these (or wrap them) so the server
// can map them to JSON-RPC codes without knowing the backend.
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

// requestKey carries the inbound request into a task run.
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
