package a2a

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

// Defaults for Config's zero fields.
const (
	defaultMaxBodyBytes    = 1 << 20
	defaultTaskTimeout     = 5 * time.Minute
	defaultMaxHistory      = 200
	defaultDefaultPageSize = 50
	defaultMaxPageSize     = 200
	// keepAliveEvery paces the SSE comment line while a stream waits
	// for the task's next event, so proxies do not idle the connection
	// out from under a long-running skill.
	keepAliveEvery = 15 * time.Second
	// pollEvery paces the multi-replica subscribe fallback.
	pollEvery = 500 * time.Millisecond
	// sseWriteDeadline bounds one SSE write; see sseStream.write.
	sseWriteDeadline = 10 * time.Second
)

// Config constructs a Server. Owner is required: multi-user scoping is
// not optional, so a nil Owner is a NewServer error rather than a
// silently single-tenant server.
type Config struct {
	// Skills is what the agent can do; at least one, unique ids, and
	// every Handler non-nil.
	Skills []Skill
	// Store persists tasks and push configs. Nil → NewMemoryStore().
	Store Store
	// Router picks the skill for a message. Nil → DefaultRouter.
	Router Router
	// Owner resolves the caller for every request. ok=false → HTTP 401
	// and CodeUnauthenticated. Nil is a NewServer error.
	Owner func(r *http.Request) (owner string, ok bool)
	// ExtendedCard, when set, serves GetExtendedAgentCard. Nil → the
	// method answers CodeExtendedAgentCardNotConfigured and
	// Capabilities().ExtendedAgentCard is false.
	ExtendedCard func(ctx context.Context, owner string) (map[string]any, error)
	// Push tunes push-notification delivery; see PushOptions.
	Push PushOptions
	// Logger for handler failures, drops, and push errors. Nil →
	// slog.Default().
	Logger *slog.Logger
	// MaxBodyBytes caps one request body. Default 1 MiB.
	MaxBodyBytes int64
	// TaskTimeout is the ceiling on one skill-handler run. Default
	// 5 minutes.
	TaskTimeout time.Duration
	// MaxHistory caps stored history per task (oldest dropped).
	// Default 200 messages.
	MaxHistory int
	// DefaultPageSize and MaxPageSize bound ListTasks paging.
	// Defaults 50 / 200.
	DefaultPageSize, MaxPageSize int
}

// Server is the A2A task-exchange HTTP handler. Construct with
// NewServer and mount behind the middleware that establishes the
// principal Config.Owner resolves.
type Server struct {
	skills    []Skill
	byID      map[string]Skill
	store     Store
	router    Router
	owner     func(*http.Request) (string, bool)
	extended  func(context.Context, string) (map[string]any, error)
	push      *pusher
	log       *slog.Logger
	maxBody   int64
	timeout   time.Duration
	maxHist   int
	defPage   int
	maxPage   int
	keepAlive time.Duration
	pollEvery time.Duration

	// now and newID are indirections for tests: a fixed clock and a
	// counter make golden frames byte-stable.
	now   func() time.Time
	newID func() string

	mu   sync.Mutex
	runs map[string]*run
}

// run tracks one in-process task run: the context CancelTask reaches,
// the event bus streaming subscribers attach to, and the done channel
// closed (after the final event is published) when the run settles.
type run struct {
	id     string
	cancel context.CancelFunc
	bus    *taskBus
	done   chan struct{}
}

// NewServer validates cfg and applies its defaults.
func NewServer(cfg Config) (*Server, error) {
	if len(cfg.Skills) == 0 {
		return nil, errors.New("a2a: Config.Skills must have at least one skill")
	}
	byID := map[string]Skill{}
	for _, sk := range cfg.Skills {
		if sk.ID == "" {
			return nil, errors.New("a2a: skill with empty id")
		}
		if sk.Handler == nil {
			return nil, fmt.Errorf("a2a: skill %q has no Handler", sk.ID)
		}
		if _, dup := byID[sk.ID]; dup {
			return nil, fmt.Errorf("a2a: duplicate skill id %q", sk.ID)
		}
		byID[sk.ID] = sk
	}
	if cfg.Owner == nil {
		return nil, errors.New("a2a: Config.Owner is required (resolve the caller or refuse it; there is no anonymous posture)")
	}
	store := cfg.Store
	if store == nil {
		store = NewMemoryStore()
	}
	router := cfg.Router
	if router == nil {
		router = DefaultRouter
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBytes
	}
	timeout := cfg.TaskTimeout
	if timeout <= 0 {
		timeout = defaultTaskTimeout
	}
	maxHist := cfg.MaxHistory
	if maxHist <= 0 {
		maxHist = defaultMaxHistory
	}
	maxPage := cfg.MaxPageSize
	if maxPage <= 0 {
		maxPage = defaultMaxPageSize
	}
	defPage := cfg.DefaultPageSize
	if defPage <= 0 {
		defPage = defaultDefaultPageSize
	}
	if defPage > maxPage {
		return nil, fmt.Errorf("a2a: Config.DefaultPageSize %d exceeds MaxPageSize %d", defPage, maxPage)
	}
	return &Server{
		skills:    slices.Clone(cfg.Skills),
		byID:      byID,
		store:     store,
		router:    router,
		owner:     cfg.Owner,
		extended:  cfg.ExtendedCard,
		push:      newPusher(cfg.Push, log),
		log:       log,
		maxBody:   maxBody,
		timeout:   timeout,
		maxHist:   maxHist,
		defPage:   defPage,
		maxPage:   maxPage,
		keepAlive: keepAliveEvery,
		pollEvery: pollEvery,
		now:       time.Now,
		newID:     newUUID,
		runs:      map[string]*run{},
	}, nil
}

// Skills returns a copy of the registered skills in registration order.
func (s *Server) Skills() []Skill {
	out := make([]Skill, len(s.skills))
	for i, sk := range s.skills {
		cp := sk
		cp.Tags = slices.Clone(sk.Tags)
		cp.Examples = slices.Clone(sk.Examples)
		cp.InputModes = slices.Clone(sk.InputModes)
		cp.OutputModes = slices.Clone(sk.OutputModes)
		out[i] = cp
	}
	return out
}

// Capabilities is what the agent card copies: this server always
// streams, and push/extended card follow configuration.
func (s *Server) Capabilities() Capabilities {
	return Capabilities{
		Streaming:         true,
		PushNotifications: !s.push.disable,
		ExtendedAgentCard: s.extended != nil,
	}
}

// rpcRequest and rpcResponse are the JSON-RPC 2.0 envelope. The id is
// kept as raw JSON so whatever the client sent (number, string, null)
// is echoed byte-for-byte.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// ServeHTTP answers one JSON-RPC request. POST only; the body is
// capped, the content type must be JSON, and the caller must resolve to
// an owner before any method runs.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeTransport(w, http.StatusMethodNotAllowed,
			Errorf(CodeInvalidRequest, "POST required"))
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		s.writeTransport(w, http.StatusUnsupportedMediaType,
			Errorf(CodeContentTypeNotSupported, "content type must be application/json or application/a2a+json"))
		return
	}
	// MaxBytesReader caps the read AND makes the server close the
	// connection after an over-large body, so a client cannot keep
	// streaming.
	body := http.MaxBytesReader(w, r.Body, s.maxBody)
	raw, err := io.ReadAll(body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.writeTransport(w, http.StatusRequestEntityTooLarge,
				Errorf(CodeInvalidRequest, "request body exceeds %d bytes", s.maxBody))
			return
		}
		s.writeTransport(w, http.StatusBadRequest, Errorf(CodeParseError, "read body: %v", err))
		return
	}
	trimmed := strings.TrimLeft(string(raw), " \t\r\n")
	if strings.HasPrefix(trimmed, "[") {
		// A batch has no single id to answer; refuse at the transport
		// level. A2A v1.0 carries no batch semantics.
		s.writeTransport(w, http.StatusBadRequest,
			Errorf(CodeInvalidRequest, "batch requests are not supported"))
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil { //gofastr:allow(GOFASTR1407) A2A JSON-RPC envelope (jsonrpc/method/params/id): the whole object is the protocol unit, no field is an identity
		s.writeTransport(w, http.StatusBadRequest, Errorf(CodeParseError, "parse request: %v", err))
		return
	}
	if req.JSONRPC != "2.0" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidRequest, `jsonrpc must be "2.0"`))
		return
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		s.writeResult(w, nil, nil, Errorf(CodeInvalidRequest, "request id required"))
		return
	}
	// The owner check precedes dispatch for every method, so no method
	// — not even the card — leaks anything to an unidentified caller.
	owner, ok := s.owner(r)
	if !ok {
		s.writeTransport(w, http.StatusUnauthorized, ErrUnauthenticated())
		return
	}
	s.dispatch(w, r, &req, owner)
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string) {
	switch req.Method {
	case MethodSendMessage:
		s.handleSend(w, r, req, owner, false)
	case MethodSendStreamingMessage:
		s.handleSend(w, r, req, owner, true)
	case MethodGetTask:
		s.handleGetTask(w, req, owner)
	case MethodListTasks:
		s.handleListTasks(w, req, owner)
	case MethodCancelTask:
		s.handleCancelTask(w, req, owner)
	case MethodSubscribeToTask:
		s.handleSubscribe(w, r, req, owner)
	case MethodCreateTaskPushNotificationConfig:
		s.handleCreatePushConfig(w, req, owner)
	case MethodGetTaskPushNotificationConfig:
		s.handleGetPushConfig(w, req, owner)
	case MethodListTaskPushNotificationConfigs:
		s.handleListPushConfigs(w, req, owner)
	case MethodDeleteTaskPushNotificationConfig:
		s.handleDeletePushConfig(w, req, owner)
	case MethodGetExtendedAgentCard:
		s.handleExtendedCard(w, req, owner)
	default:
		s.writeResult(w, req.ID, nil, Errorf(CodeMethodNotFound, "unknown method %q", req.Method))
	}
}

// ---- response writing -------------------------------------------------

func (s *Server) writeResult(w http.ResponseWriter, id json.RawMessage, result any, aerr *Error) {
	s.writeStatus(w, http.StatusOK, id, result, aerr)
}

// writeStatus writes one JSON-RPC response. A non-200 status is only
// for transport failures (401/405/413/415/400-parse); JSON-RPC-level
// errors ride a 200 per convention.
func (s *Server) writeStatus(w http.ResponseWriter, status int, id json.RawMessage, result any, aerr *Error) {
	resp := rpcResponse{JSONRPC: "2.0", ID: id}
	if aerr != nil {
		resp.Error = aerr
	} else {
		b, err := json.Marshal(result)
		if err != nil {
			s.log.Error("a2a: marshal result", "err", err)
			resp.Error = Errorf(CodeInternalError, "internal error")
		} else {
			resp.Result = b
		}
	}
	b, err := json.Marshal(resp)
	if err != nil {
		// One of the fixed error shapes; unreachable in practice.
		http.Error(w, `{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`, http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// writeTransport answers a transport-level failure (non-200): the
// request could not be addressed, so there is no id to echo.
func (s *Server) writeTransport(w http.ResponseWriter, status int, aerr *Error) {
	s.writeStatus(w, status, nil, nil, aerr)
}

// internalErr logs err and answers the generic JSON-RPC internal error:
// a Store or handler error's text (driver messages, paths, SQL) never
// reaches the client.
func (s *Server) internalErr(w http.ResponseWriter, id json.RawMessage, what string, err error) {
	s.log.Error("a2a: "+what, "err", err)
	s.writeResult(w, id, nil, Errorf(CodeInternalError, "internal error"))
}

func decodeParams(raw json.RawMessage, v any) *Error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return Errorf(CodeInvalidParams, "invalid params: %v", err)
	}
	return nil
}

func isJSONContentType(ct string) bool {
	if ct == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mediaType == "application/json" || mediaType == "application/a2a+json"
}

// applyHistoryLength keeps the last n history messages on a task copy;
// nil keeps everything. Only response copies are trimmed — the store
// always holds the full history.
func applyHistoryLength(t *Task, n *int) {
	if n == nil {
		return
	}
	k := *n
	if k < 0 {
		k = 0
	}
	if len(t.History) > k {
		t.History = t.History[len(t.History)-k:]
	}
}

// encodePageToken/decodePageToken make the page cursor opaque: base64
// of the decimal offset. Nothing up to MaxPageSize*2^31 rows depends on
// the encoding being clever.
func encodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodePageToken(tok string) (int, error) {
	b, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(string(b))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("bad token")
	}
	return n, nil
}

// clampPageSize applies the configured bounds. A supplied size outside
// [1, MaxPageSize] is clamped, not refused: paging is not a security
// posture, and silently shrinking is friendlier than erroring.
func (s *Server) clampPageSize(n *int) int {
	if n == nil || *n < 1 {
		return s.defPage
	}
	return min(*n, s.maxPage)
}

// ---- run registry -----------------------------------------------------

// claimRun registers a placeholder run for taskID and returns it, or
// nil when a run is already registered. Claiming BEFORE the WORKING
// transition is what makes two concurrent resuming messages on one task
// fail cleanly instead of interleaving writes.
func (s *Server) claimRun(taskID string) *run {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.runs[taskID]; exists {
		return nil
	}
	rn := &run{
		id:     taskID,
		cancel: func() {},
		bus:    newTaskBus(s.log),
		done:   make(chan struct{}),
	}
	s.runs[taskID] = rn
	return rn
}

func (s *Server) runFor(taskID string) *run {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[taskID]
}

func (s *Server) releaseRun(rn *run) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runs[rn.id] == rn {
		delete(s.runs, rn.id)
	}
}

// finishRun unregisters the run and closes done. Called after finalize,
// so the terminal event is already published when done closes and a
// draining subscriber still sees it.
func (s *Server) finishRun(rn *run) {
	s.releaseRun(rn)
	close(rn.done)
}

func (s *Server) setRunCancel(rn *run, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rn.cancel = cancel
}

// startRun launches the handler goroutine for a claimed run. The run
// context is cut from the request's CANCELLATION but not from the
// request itself: returnImmediately and stream disconnects must not
// abort work the client was told is running. TaskTimeout bounds it.
func (s *Server) startRun(r *http.Request, t *taskRun, rn *run, h Handler) {
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), s.timeout)
	runCtx = WithRequest(runCtx, r)
	s.setRunCancel(rn, cancel)
	go func() {
		defer func() {
			cancel()
			s.finishRun(rn)
		}()
		err := s.invoke(runCtx, t, h)
		s.finalize(runCtx, t, err)
	}()
}

// publish fans one event out to the task's live subscribers and its
// push configs. Called only AFTER the event's task state is persisted.
func (s *Server) publish(owner string, rec *TaskRecord, ev StreamResponse) {
	if rn := s.runFor(rec.Task.ID); rn != nil {
		rn.bus.publish(ev)
	}
	cfgs, err := s.store.ListPushConfigs(context.Background(), owner, rec.Task.ID)
	if err != nil {
		s.log.Error("a2a: list push configs", "taskId", rec.Task.ID, "err", err)
		return
	}
	s.push.deliver(cfgs, ev)
}

// ---- SendMessage / SendStreamingMessage -------------------------------

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, streaming bool) {
	var p SendMessageRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if aerr := s.validateInbound(&p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	t, rn, h, aerr := s.prepareSend(r, owner, &p)
	if aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if streaming {
		s.streamSend(w, r, req.ID, t, rn, h, historyLengthOf(&p))
		return
	}
	// Non-streaming: answer with the task. returnImmediately hands the
	// SUBMITTED/WORKING snapshot back at once and keeps running in the
	// background; otherwise the response IS the final task.
	if p.Configuration != nil && p.Configuration.ReturnImmediately && h != nil {
		s.startRun(r, t, rn, h)
		task := t.snapshot()
		applyHistoryLength(task, historyLengthOf(&p))
		s.writeResult(w, req.ID, SendMessageResponse{Task: task}, nil)
		return
	}
	if h != nil {
		s.startRun(r, t, rn, h)
		// Wait for the run, but not past the client: a caller that hangs
		// up must not pin this goroutine for the rest of TaskTimeout. The
		// run itself carries on regardless (its context is cut from the
		// request's cancellation), so the client was told nothing false.
		select {
		case <-rn.done:
		case <-r.Context().Done():
			return
		}
	}
	task := t.snapshot()
	applyHistoryLength(task, historyLengthOf(&p))
	s.writeResult(w, req.ID, SendMessageResponse{Task: task}, nil)
}

func historyLengthOf(p *SendMessageRequest) *int {
	if p.Configuration == nil {
		return nil
	}
	return p.Configuration.HistoryLength
}

func pushConfigOf(p *SendMessageRequest) *PushNotificationConfig {
	if p.Configuration == nil {
		return nil
	}
	return p.Configuration.TaskPushNotificationConfig
}

// validateInbound enforces the message shape and the push config (when
// one rides along in configuration) BEFORE anything is created, so a
// refused request leaves no half-built task behind.
func (s *Server) validateInbound(p *SendMessageRequest) *Error {
	m := p.Message
	if m == nil {
		return Errorf(CodeInvalidParams, "message is required")
	}
	if m.Role != RoleUser {
		return Errorf(CodeInvalidParams, "message.role must be ROLE_USER, got %q", m.Role)
	}
	if len(m.Parts) == 0 {
		return Errorf(CodeInvalidParams, "message.parts must not be empty")
	}
	for i := range m.Parts {
		if err := m.Parts[i].Validate(); err != nil {
			return Errorf(CodeInvalidParams, "message.parts[%d]: %v", i, err)
		}
	}
	if cfg := pushConfigOf(p); cfg != nil {
		if s.push.disable {
			return Errorf(CodePushNotificationNotSupported, "push notifications are disabled")
		}
		if cfg.URL == "" {
			return Errorf(CodeInvalidParams, "taskPushNotificationConfig.url is required")
		}
		if err := validatePushURL(cfg.URL, hasCredentials(*cfg), s.push.allowPrivate); err != nil {
			return Errorf(CodeInvalidParams, "%v", err)
		}
	}
	return nil
}

// prepareSend resolves a SendMessage to a runnable task. It returns the
// run context (h and rn non-nil exactly together), or a task that is
// already answered (h nil: rejected at routing), or a JSON-RPC error —
// in which case nothing is left running.
func (s *Server) prepareSend(r *http.Request, owner string, p *SendMessageRequest) (t *taskRun, rn *run, h Handler, aerr *Error) {
	if p.Message.TaskID == "" {
		return s.prepareNewTask(r, owner, p)
	}
	return s.prepareResume(r, owner, p)
}

func (s *Server) prepareNewTask(r *http.Request, owner string, p *SendMessageRequest) (*taskRun, *run, Handler, *Error) {
	msg := p.Message
	if msg.MessageID == "" {
		msg.MessageID = s.newID()
	}
	taskID := s.newID()
	if msg.ContextID == "" {
		msg.ContextID = s.newID()
	}
	msg.TaskID = taskID

	skillID, rerr := s.router(r.Context(), msg, s.skills)
	h := Handler(nil)
	if rerr == nil {
		if sk, ok := s.byID[skillID]; ok {
			h = sk.Handler
		} else {
			rerr = fmt.Errorf("no skill named %q; available: %s", skillID, strings.Join(s.skillIDs(), ", "))
		}
	}

	task := Task{
		ID:        taskID,
		ContextID: msg.ContextID,
		Status:    TaskStatus{State: TaskStateSubmitted, Timestamp: s.stamp()},
		History:   []Message{*msg},
	}
	if rerr == nil {
		task.Metadata = map[string]any{"gofastr.skill": skillID}
	}
	rec := &TaskRecord{
		Task:      task,
		Owner:     owner,
		SkillID:   skillID,
		CreatedAt: s.now(),
		UpdatedAt: s.now(),
	}
	// Store the push config BEFORE the task row, and compensate with a
	// delete if the task insert then fails. The opposite order left a
	// half-built send behind when the config insert failed: an
	// unreachable SUBMITTED task with no run that would never settle
	// (validateInbound's no-half-built posture, but for the post-create
	// failure path). A push config for a task id that never materialized
	// is inert by comparison, and DeletePushConfig removes even that.
	if cfg := pushConfigOf(p); cfg != nil {
		cfg.TaskID = taskID // the task id did not exist when the client built the request
		if aerr := s.storePushConfig(r.Context(), owner, cfg); aerr != nil {
			return nil, nil, nil, aerr
		}
	}
	if err := s.store.CreateTask(r.Context(), rec); err != nil {
		s.log.Error("a2a: create task", "taskId", taskID, "err", err)
		if cfg := pushConfigOf(p); cfg != nil {
			if derr := s.store.DeletePushConfig(r.Context(), owner, taskID, cfg.ID); derr != nil {
				// The config row is inert without its task, so this is
				// a log line, not a second error to report.
				s.log.Error("a2a: delete push config after failed task create", "taskId", taskID, "err", derr)
			}
		}
		return nil, nil, nil, Errorf(CodeInternalError, "internal error")
	}
	t := newTaskRun(s, rec, msg, r, owner)
	if rerr != nil {
		// A skill miss is the agent's decision, not a protocol error:
		// the task exists and is REJECTED with the router's message.
		if err := t.Reject(TextPart(rerr.Error())); err != nil {
			s.log.Error("a2a: reject unrouted task", "taskId", taskID, "err", err)
			return nil, nil, nil, Errorf(CodeInternalError, "internal error")
		}
		return t, nil, nil, nil
	}
	rn := s.claimRun(taskID)
	if rn == nil {
		// A freshly minted UUID cannot collide; treat as unrecoverable.
		return nil, nil, nil, Errorf(CodeInternalError, "internal error")
	}
	return t, rn, h, nil
}

func (s *Server) prepareResume(r *http.Request, owner string, p *SendMessageRequest) (*taskRun, *run, Handler, *Error) {
	msg := p.Message
	if msg.MessageID == "" {
		msg.MessageID = s.newID()
	}
	taskID := msg.TaskID
	rec, err := s.store.GetTask(r.Context(), owner, taskID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil, nil, ErrTaskNotFound(taskID)
	}
	if err != nil {
		s.log.Error("a2a: get task for resume", "taskId", taskID, "err", err)
		return nil, nil, nil, Errorf(CodeInternalError, "internal error")
	}
	state := rec.Task.Status.State
	if state.Terminal() {
		return nil, nil, nil, ErrUnsupportedOperation(fmt.Sprintf("task %s is %s", taskID, state))
	}
	if !state.Interrupted() {
		// SUBMITTED or WORKING: the task has a run (here or on another
		// replica). Conservative reading: refuse rather than queue.
		return nil, nil, nil, ErrUnsupportedOperation(fmt.Sprintf("task %s is running", taskID))
	}
	if cfg := pushConfigOf(p); cfg != nil && cfg.TaskID != "" && cfg.TaskID != taskID {
		return nil, nil, nil, Errorf(CodeInvalidParams, "taskPushNotificationConfig.taskId %q does not match addressed task %q", cfg.TaskID, taskID)
	}
	// Claim before the WORKING write: the claim is what a second
	// message to the same interrupted task hits.
	rn := s.claimRun(taskID)
	if rn == nil {
		return nil, nil, nil, ErrUnsupportedOperation(fmt.Sprintf("task %s is running", taskID))
	}
	if cfg := pushConfigOf(p); cfg != nil {
		cfg.TaskID = taskID
		if aerr := s.storePushConfig(r.Context(), owner, cfg); aerr != nil {
			s.releaseRun(rn)
			return nil, nil, nil, aerr
		}
	}
	t := newTaskRun(s, rec, msg, r, owner)
	if err := t.resumeWorking(msg); err != nil {
		s.releaseRun(rn)
		if errors.Is(err, errConcurrentChange) {
			return nil, nil, nil, Errorf(CodeUnsupportedOperation, "task %s changed concurrently, retry", taskID)
		}
		// Any other failure here is a backend error: report the generic
		// internal error (errors.go's contract), never the driver text.
		s.log.Error("a2a: resume task", "taskId", taskID, "err", err)
		return nil, nil, nil, Errorf(CodeInternalError, "internal error")
	}
	sk, ok := s.byID[rec.SkillID]
	if !ok || sk.Handler == nil {
		// The skill this task was routed to is no longer registered.
		if err := t.Reject(TextPart(fmt.Sprintf("no skill named %q; available: %s", rec.SkillID, strings.Join(s.skillIDs(), ", ")))); err != nil {
			s.log.Error("a2a: reject resume without skill", "taskId", taskID, "err", err)
		}
		return t, nil, nil, nil
	}
	return t, rn, sk.Handler, nil
}

func (s *Server) storePushConfig(ctx context.Context, owner string, cfg *PushNotificationConfig) *Error {
	if cfg.ID == "" {
		cfg.ID = s.newID()
	}
	rec := &PushConfigRecord{Config: *cfg, Owner: owner, CreatedAt: s.now()}
	if err := s.store.CreatePushConfig(ctx, rec); err != nil {
		if errors.Is(err, ErrConflict) {
			return Errorf(CodeInvalidParams, "push config %q already exists for task %q", cfg.ID, cfg.TaskID)
		}
		s.log.Error("a2a: create push config", "taskId", cfg.TaskID, "err", err)
		return Errorf(CodeInternalError, "internal error")
	}
	return nil
}

func (s *Server) skillIDs() []string {
	ids := make([]string, len(s.skills))
	for i, sk := range s.skills {
		ids[i] = sk.ID
	}
	return ids
}

// snapshot returns a deep copy of the run's current task.
func (t *taskRun) snapshot() *Task {
	return t.Task()
}

// ---- SSE ---------------------------------------------------------------

// sseStream writes one task-exchange SSE response. Events are framed by
// mcp.StreamSSE, which delivers a payload with embedded newlines as
// spec-correct multi-line data and so can never be split into two
// events by one; each write is flushed immediately and carries a
// per-write deadline so a client that stops reading cannot pin this
// goroutine.
type sseStream struct {
	w    *errWriter
	fl   http.Flusher
	id   json.RawMessage
	srv  *Server
	rc   *http.ResponseController
	sent bool
}

// errWriter records the first write error; an io.Writer API cannot
// return one, so every later write short-circuits instead of touching
// a dead connection.
type errWriter struct {
	w   http.ResponseWriter
	err error
}

func (ew *errWriter) Write(p []byte) (int, error) {
	if ew.err != nil {
		return 0, ew.err
	}
	n, err := ew.w.Write(p)
	if err != nil {
		ew.err = err
	}
	return n, err
}

func (s *Server) newSSEStream(w http.ResponseWriter, id json.RawMessage) *sseStream {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	return &sseStream{
		w:   &errWriter{w: w},
		fl:  w.(http.Flusher),
		id:  id,
		srv: s,
		rc:  http.NewResponseController(w),
	}
}

// send marshals one JSON-RPC response whose result is the
// StreamResponse and writes it as a single SSE event.
func (st *sseStream) send(ev StreamResponse) error {
	result, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: st.id, Result: result})
	if err != nil {
		return err
	}
	return st.write("data", payload)
}

// write emits one raw SSE line ("data: <payload>" or ": comment") plus
// the dispatch blank line, then flushes.
func (st *sseStream) write(field string, payload []byte) error {
	if st.w.err != nil {
		return st.w.err
	}
	_ = st.rc.SetWriteDeadline(time.Now().Add(sseWriteDeadline))
	mcp.StreamSSE(st.w, "", string(payload))
	st.fl.Flush()
	return st.w.err
}

func (st *sseStream) keepAlive() error {
	if st.w.err != nil {
		return st.w.err
	}
	_ = st.rc.SetWriteDeadline(time.Now().Add(sseWriteDeadline))
	_, err := fmt.Fprintf(st.w, ": keep-alive\n\n")
	if err != nil {
		st.w.err = err
		return err
	}
	st.fl.Flush()
	return st.w.err
}

// streamSend answers SendStreamingMessage: the initial task snapshot,
// then every event until the task settles.
func (s *Server) streamSend(w http.ResponseWriter, r *http.Request, id json.RawMessage, t *taskRun, rn *run, h Handler, history *int) {
	fl, ok := w.(http.Flusher)
	if !ok {
		// Without a flusher the events would buffer unboundedly; answer
		// non-streaming instead of pretending.
		s.writeResult(w, id, nil, ErrUnsupportedOperation("streaming requires a flushing ResponseWriter"))
		return
	}
	_ = fl
	st := s.newSSEStream(w, id)
	task := t.snapshot()
	applyHistoryLength(task, history)
	if err := st.send(StreamResponse{Task: task}); err != nil {
		return
	}
	if h == nil {
		return // routing reject: the snapshot is already terminal
	}
	s.startRun(r, t, rn, h)
	s.forwardEvents(st, rn, r.Context())
}

// forwardEvents relays bus events to the stream until the task settles,
// the client goes away, or the run ends.
func (s *Server) forwardEvents(st *sseStream, rn *run, ctx context.Context) {
	ch := rn.bus.subscribe()
	defer rn.bus.unsubscribe(ch)
	keepAlive := time.NewTicker(s.keepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				// Dropped for being slow; the stream is done.
				return
			}
			if err := st.send(ev); err != nil {
				return
			}
			if endsStream(ev) {
				return
			}
		case <-ctx.Done():
			return
		case <-rn.done:
			// The run settled; drain anything buffered after its final
			// event, then close.
			for {
				select {
				case ev, ok := <-ch:
					if !ok {
						return
					}
					if err := st.send(ev); err != nil {
						return
					}
					if endsStream(ev) {
						return
					}
				default:
					return
				}
			}
		case <-keepAlive.C:
			if err := st.keepAlive(); err != nil {
				return
			}
		}
	}
}

// ---- GetTask / ListTasks / CancelTask / SubscribeToTask ---------------

func (s *Server) handleGetTask(w http.ResponseWriter, req *rpcRequest, owner string) {
	var p GetTaskRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if p.ID == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "id is required"))
		return
	}
	rec, err := s.store.GetTask(context.Background(), owner, p.ID)
	if errors.Is(err, ErrNotFound) {
		s.writeResult(w, req.ID, nil, ErrTaskNotFound(p.ID))
		return
	}
	if err != nil {
		s.internalErr(w, req.ID, "get task", err)
		return
	}
	task := cloneTask(rec.Task)
	applyHistoryLength(&task, p.HistoryLength)
	s.writeResult(w, req.ID, task, nil)
}

func (s *Server) handleListTasks(w http.ResponseWriter, req *rpcRequest, owner string) {
	var p ListTasksRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if p.Status != "" && !p.Status.Valid() {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "unknown status %q", p.Status))
		return
	}
	offset := 0
	if p.PageToken != "" {
		n, err := decodePageToken(p.PageToken)
		if err != nil {
			s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "invalid pageToken"))
			return
		}
		offset = n
	}
	size := s.clampPageSize(p.PageSize)
	var after time.Time
	if p.StatusTimestampAfter != nil {
		after = p.StatusTimestampAfter.Time
	}
	q := ListQuery{ContextID: p.ContextID, Status: p.Status, After: after, Limit: size, Offset: offset}
	recs, total, err := s.store.ListTasks(context.Background(), owner, q)
	if err != nil {
		s.internalErr(w, req.ID, "list tasks", err)
		return
	}
	includeArtifacts := p.IncludeArtifacts != nil && *p.IncludeArtifacts
	tasks := make([]Task, 0, len(recs))
	for _, rec := range recs {
		task := cloneTask(rec.Task)
		if !includeArtifacts {
			task.Artifacts = nil
		}
		applyHistoryLength(&task, p.HistoryLength)
		tasks = append(tasks, task)
	}
	resp := ListTasksResponse{Tasks: tasks, PageSize: size, TotalSize: total}
	if offset+len(recs) < total {
		resp.NextPageToken = encodePageToken(offset + len(recs))
	}
	s.writeResult(w, req.ID, resp, nil)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, req *rpcRequest, owner string) {
	var p CancelTaskRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if p.ID == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "id is required"))
		return
	}
	// The read-modify-write loop is bounded; each attempt re-reads, so
	// a handler writing concurrently cannot wedge cancellation. The
	// run's own finalize also writes CANCELED after our cancel() lands
	// (a safety net for a canceled context nobody else persists), so a
	// re-read that finds CANCELED after WE canceled the run means the
	// cancellation happened — answer it, do not report -32002 for the
	// state we asked for.
	const attempts = 8
	canceledRun := false
	for range attempts {
		rec, err := s.store.GetTask(context.Background(), owner, p.ID)
		if errors.Is(err, ErrNotFound) {
			s.writeResult(w, req.ID, nil, ErrTaskNotFound(p.ID))
			return
		}
		if err != nil {
			s.internalErr(w, req.ID, "get task for cancel", err)
			return
		}
		state := rec.Task.Status.State
		if state == TaskStateCanceled && canceledRun {
			task := cloneTask(rec.Task)
			s.writeResult(w, req.ID, SendMessageResponse{Task: &task}, nil)
			return
		}
		if state.Terminal() {
			s.writeResult(w, req.ID, nil, ErrTaskNotCancelable(p.ID, state))
			return
		}
		// Owner verified by the scoped read above; now the local run
		// may be canceled too, so the handler wakes promptly.
		if rn := s.runFor(p.ID); rn != nil {
			rn.cancel()
			canceledRun = true
		}
		cand := rec.Clone()
		cand.Task.Status = TaskStatus{State: TaskStateCanceled, Timestamp: s.stamp()}
		if err := s.store.UpdateTask(context.Background(), cand); err != nil {
			if errors.Is(err, ErrConflict) {
				continue
			}
			s.internalErr(w, req.ID, "cancel task", err)
			return
		}
		ev := StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
			TaskID:    cand.Task.ID,
			ContextID: cand.Task.ContextID,
			Status:    cand.Task.Status,
		}}
		s.publish(owner, cand, ev)
		task := cloneTask(cand.Task)
		s.writeResult(w, req.ID, SendMessageResponse{Task: &task}, nil)
		return
	}
	s.writeResult(w, req.ID, nil, Errorf(CodeInternalError, "internal error"))
}

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string) {
	var p SubscribeToTaskRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if p.ID == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "id is required"))
		return
	}
	rec, err := s.store.GetTask(context.Background(), owner, p.ID)
	if errors.Is(err, ErrNotFound) {
		s.writeResult(w, req.ID, nil, ErrTaskNotFound(p.ID))
		return
	}
	if err != nil {
		s.internalErr(w, req.ID, "get task for subscribe", err)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		s.writeResult(w, req.ID, nil, ErrUnsupportedOperation("streaming requires a flushing ResponseWriter"))
		return
	}
	st := s.newSSEStream(w, req.ID)
	// The a2a-go SDK sends the current task as one event and closes
	// when the task is already settled; do the same.
	snap := cloneTask(rec.Task)
	if err := st.send(StreamResponse{Task: &snap}); err != nil {
		return
	}
	state := rec.Task.Status.State
	if state.Terminal() || state.Interrupted() {
		return
	}
	if rn := s.runFor(p.ID); rn != nil {
		s.forwardEvents(st, rn, r.Context())
		return
	}
	// Multi-replica fallback: the task is non-terminal and has no run
	// in this process, so it is running on (or stalled from) another
	// replica sharing the store. Poll the store and emit a snapshot
	// whenever the version changes, until the task settles. Events
	// between polls are coalesced into the next snapshot — a replica
	// boundary is exactly where per-event fidelity would require a
	// shared bus, which a SQL store cannot give.
	s.pollEvents(st, owner, rec, r.Context())
}

func (s *Server) pollEvents(st *sseStream, owner string, rec *TaskRecord, ctx context.Context) {
	last := rec.Version
	poll := time.NewTicker(s.pollEvery)
	defer poll.Stop()
	keepAlive := time.NewTicker(s.keepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			if err := st.keepAlive(); err != nil {
				return
			}
		case <-poll.C:
			cur, err := s.store.GetTask(context.Background(), owner, rec.Task.ID)
			if errors.Is(err, ErrNotFound) {
				return
			}
			if err != nil {
				s.log.Error("a2a: poll read", "taskId", rec.Task.ID, "err", err)
				continue
			}
			if cur.Version == last {
				continue
			}
			last = cur.Version
			snap := cloneTask(cur.Task)
			if err := st.send(StreamResponse{Task: &snap}); err != nil {
				return
			}
			if state := cur.Task.Status.State; state.Terminal() || state.Interrupted() {
				return
			}
		}
	}
}

// ---- push config methods ----------------------------------------------

func (s *Server) handleCreatePushConfig(w http.ResponseWriter, req *rpcRequest, owner string) {
	var cfg PushNotificationConfig
	if aerr := decodeParams(req.Params, &cfg); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if cfg.TaskID == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "taskId is required"))
		return
	}
	if cfg.URL == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "url is required"))
		return
	}
	if s.push.disable {
		s.writeResult(w, req.ID, nil, Errorf(CodePushNotificationNotSupported, "push notifications are disabled"))
		return
	}
	if _, err := s.store.GetTask(context.Background(), owner, cfg.TaskID); err != nil {
		if errors.Is(err, ErrNotFound) {
			s.writeResult(w, req.ID, nil, ErrTaskNotFound(cfg.TaskID))
			return
		}
		s.internalErr(w, req.ID, "get task for push config", err)
		return
	}
	if err := validatePushURL(cfg.URL, hasCredentials(cfg), s.push.allowPrivate); err != nil {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "%v", err))
		return
	}
	if aerr := s.storePushConfig(context.Background(), owner, &cfg); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	s.writeResult(w, req.ID, cfg, nil)
}

// requireOwnedTask answers ErrTaskNotFound when the task is absent or
// another owner's; the push-config handlers all need exactly that.
func (s *Server) requireOwnedTask(w http.ResponseWriter, req *rpcRequest, owner, taskID string) bool {
	_, err := s.store.GetTask(context.Background(), owner, taskID)
	if err == nil {
		return true
	}
	if errors.Is(err, ErrNotFound) {
		s.writeResult(w, req.ID, nil, ErrTaskNotFound(taskID))
		return false
	}
	s.internalErr(w, req.ID, "get task", err)
	return false
}

func (s *Server) handleGetPushConfig(w http.ResponseWriter, req *rpcRequest, owner string) {
	var p GetTaskPushNotificationConfigRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if !s.requireOwnedTask(w, req, owner, p.TaskID) {
		return
	}
	rec, err := s.store.GetPushConfig(context.Background(), owner, p.TaskID, p.ID)
	if errors.Is(err, ErrNotFound) {
		// The spec has no dedicated code for a missing config id; the
		// task was verified above, so the params are what is wrong.
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "push config not found"))
		return
	}
	if err != nil {
		s.internalErr(w, req.ID, "get push config", err)
		return
	}
	s.writeResult(w, req.ID, rec.Config, nil)
}

func (s *Server) handleListPushConfigs(w http.ResponseWriter, req *rpcRequest, owner string) {
	var p ListTaskPushNotificationConfigsRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if !s.requireOwnedTask(w, req, owner, p.TaskID) {
		return
	}
	offset := 0
	if p.PageToken != "" {
		n, err := decodePageToken(p.PageToken)
		if err != nil {
			s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "invalid pageToken"))
			return
		}
		offset = n
	}
	recs, err := s.store.ListPushConfigs(context.Background(), owner, p.TaskID)
	if err != nil {
		s.internalErr(w, req.ID, "list push configs", err)
		return
	}
	total := len(recs)
	if offset > total {
		offset = total
	}
	page := recs[offset:]
	size := s.clampPageSize(p.PageSize)
	if size < len(page) {
		page = page[:size]
	}
	configs := make([]PushNotificationConfig, 0, len(page))
	for _, rec := range page {
		configs = append(configs, rec.Config)
	}
	resp := ListTaskPushNotificationConfigsResponse{Configs: configs}
	if offset+len(page) < total {
		resp.NextPageToken = encodePageToken(offset + len(page))
	}
	s.writeResult(w, req.ID, resp, nil)
}

func (s *Server) handleDeletePushConfig(w http.ResponseWriter, req *rpcRequest, owner string) {
	var p DeleteTaskPushNotificationConfigRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if !s.requireOwnedTask(w, req, owner, p.TaskID) {
		return
	}
	err := s.store.DeletePushConfig(context.Background(), owner, p.TaskID, p.ID)
	if errors.Is(err, ErrNotFound) {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "push config not found"))
		return
	}
	if err != nil {
		s.internalErr(w, req.ID, "delete push config", err)
		return
	}
	s.writeResult(w, req.ID, struct{}{}, nil)
}

// ---- GetExtendedAgentCard ---------------------------------------------

func (s *Server) handleExtendedCard(w http.ResponseWriter, req *rpcRequest, owner string) {
	if s.extended == nil {
		s.writeResult(w, req.ID, nil, Errorf(CodeExtendedAgentCardNotConfigured, "extended agent card not configured"))
		return
	}
	var p GetExtendedAgentCardRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	card, err := s.extended(context.Background(), owner)
	if err != nil {
		var ae *Error
		if errors.As(err, &ae) {
			s.writeResult(w, req.ID, nil, ae)
			return
		}
		s.internalErr(w, req.ID, "extended card", err)
		return
	}
	s.writeResult(w, req.ID, card, nil)
}

// ---- DefaultRouter -----------------------------------------------------

// DefaultRouter picks the skill for a message:
//
//  1. msg.Metadata["skill"], when a non-empty string;
//  2. else the first data part whose Data is a JSON object with a
//     string "skill" key;
//  3. else the only registered skill, when exactly one is;
//  4. else an error naming the available ids.
//
// A skill id that is not registered is an error at step 1 and 2 alike —
// the router names what exists rather than guessing.
func DefaultRouter(_ context.Context, msg *Message, skills []Skill) (string, error) {
	available := make([]string, len(skills))
	for i, sk := range skills {
		available[i] = sk.ID
	}
	names := strings.Join(available, ", ")

	if msg != nil && msg.Metadata != nil {
		if v, ok := msg.Metadata["skill"]; ok {
			if name, isStr := v.(string); isStr && name != "" {
				if skillKnown(skills, name) {
					return name, nil
				}
				return "", fmt.Errorf("no skill named %q; available: %s", name, names)
			}
		}
	}
	if msg != nil {
		for i := range msg.Parts {
			data := msg.Parts[i].Data
			if data == nil {
				continue
			}
			obj, ok := (*data).(map[string]any)
			if !ok {
				continue
			}
			if name, ok := obj["skill"].(string); ok && name != "" {
				if skillKnown(skills, name) {
					return name, nil
				}
				return "", fmt.Errorf("no skill named %q; available: %s", name, names)
			}
		}
	}
	if len(skills) == 1 {
		return skills[0].ID, nil
	}
	return "", fmt.Errorf("no skill named; available: %s", names)
}

func skillKnown(skills []Skill, id string) bool {
	for _, sk := range skills {
		if sk.ID == id {
			return true
		}
	}
	return false
}
