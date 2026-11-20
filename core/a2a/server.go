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
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/netguard"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// Defaults for Config's zero fields.
const (
	defaultMaxBodyBytes              = 1 << 20
	defaultTaskTimeout               = 5 * time.Minute
	defaultMaxHistory                = 200
	defaultDefaultPageSize           = 50
	defaultMaxPageSize               = 200
	defaultTerminalTaskRetention     = 64
	defaultPausedTaskRetention       = 64
	defaultMaxPushConfigsPerTask     = 8
	defaultMaxConcurrentRunsPerOwner = 16
	keepAliveEvery                   = 15 * time.Second
	pollEvery                        = 500 * time.Millisecond
	sseWriteDeadline                 = 10 * time.Second
)

// Config constructs a Server.
type Config struct {
	// Skills is what the agent can do; at least one, unique ids, and
	// every Handler non-nil.
	Skills []Skill
	// Store persists tasks and push configs. Nil means NewMemoryStore().
	Store Store
	// Router picks the skill for a message. Nil means DefaultRouter.
	Router Router
	// Owner resolves the caller for every request; ok=false answers 401.
	// Nil is a NewServer error.
	Owner func(r *http.Request) (owner string, ok bool)
	// Scopes reports the scopes the caller's credential is narrowed to, or
	// false for an unscoped caller. A scoped caller reaches only the tasks
	// made under the same scopes. Nil: every caller is unscoped.
	Scopes func(r *http.Request) (scopes []string, scoped bool)
	// Tenant reports the tenant the caller acts in, "" for none. A caller
	// reaches only the tasks made in its current tenant. Nil: no check.
	Tenant func(r *http.Request) string
	// ExtendedCard serves GetExtendedAgentCard; nil answers -32007.
	ExtendedCard func(ctx context.Context, owner string) (map[string]any, error)
	// Push tunes push-notification delivery; see PushOptions.
	Push PushOptions
	// Logger for handler failures, drops, and push errors. Nil means
	// slog.Default().
	Logger *slog.Logger
	// AllowedOrigins permits browser Origins that are not same-origin with
	// the request: tunnels (ngrok, Codespaces) and split-origin web clients.
	AllowedOrigins []string
	// MaxBodyBytes caps one request body. Default 1 MiB.
	MaxBodyBytes int64
	// TaskTimeout is the ceiling on one skill-handler run. Default 5 minutes.
	TaskTimeout time.Duration
	// MaxHistory caps stored history per task (oldest dropped). Default 200.
	MaxHistory int
	// DefaultPageSize and MaxPageSize bound ListTasks paging. Defaults 50
	// and 200.
	DefaultPageSize, MaxPageSize int
	// TerminalTaskRetention bounds the terminal task rows one owner keeps
	// per RetentionTier; the oldest past it are deleted. 0 means 64;
	// negative disables.
	TerminalTaskRetention int
	// PausedTaskRetention bounds the paused task rows one owner keeps per
	// RetentionTier; the oldest past it are canceled. 0 means 64; negative
	// disables.
	PausedTaskRetention int
	// MaxPushConfigsPerTask bounds push configs per task; the oldest past
	// it are deleted. 0 means 8; negative disables.
	MaxPushConfigsPerTask int
	// MaxStreamSeatsPerOwner bounds one owner's concurrent SSE streams.
	// 0 means 16; negative lifts the cap.
	MaxStreamSeatsPerOwner int
	// MaxConcurrentRunsPerOwner bounds one owner's concurrent skill-handler
	// runs. 0 means 16; negative lifts the cap.
	MaxConcurrentRunsPerOwner int
	// StreamSeatOverflow selects what an owner at MaxStreamSeatsPerOwner
	// does with its next stream: refuse with 429 (the default) or evict
	// its oldest stream.
	StreamSeatOverflow stream.SeatOverflowPolicy
}

// Server is the A2A task-exchange HTTP handler.
type Server struct {
	skills   []Skill
	byID     map[string]Skill
	store    Store
	router   Router
	owner    func(*http.Request) (string, bool)
	scopes   func(*http.Request) ([]string, bool)
	tenant   func(*http.Request) string
	extended func(context.Context, string) (map[string]any, error)
	push     *pusher
	log      *slog.Logger
	maxBody  int64
	timeout  time.Duration
	maxHist  int
	defPage  int
	maxPage  int

	maxTerminalTasks int
	maxPausedTasks   int
	maxPushConfigs   int
	keepAlive        time.Duration
	pollEvery        time.Duration

	maxStreamSeats            int
	streamSeatOverflow        stream.SeatOverflowPolicy
	seats                     *streamSeatRegistry
	maxConcurrentRunsPerOwner int

	allowedOrigins []string

	now   func() time.Time
	newID func() string

	mu          sync.Mutex
	runs        map[string]*run
	runsByOwner map[string]int
	closing     bool
	life        context.Context
	stopLife    context.CancelFunc
}

type run struct {
	id         string
	owner      string
	cancel     context.CancelFunc
	halted     atomic.Bool
	cancelDone chan struct{}
	bus        *taskBus
	done       chan struct{}
	released   chan struct{}
	pausedAt   atomic.Int64
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
	maxTerminal := cfg.TerminalTaskRetention
	if maxTerminal == 0 {
		maxTerminal = defaultTerminalTaskRetention
	}
	maxPaused := cfg.PausedTaskRetention
	if maxPaused == 0 {
		maxPaused = defaultPausedTaskRetention
	}
	maxPush := cfg.MaxPushConfigsPerTask
	if maxPush == 0 {
		maxPush = defaultMaxPushConfigsPerTask
	}
	if err := refuseUnwiredTrimmer(store, maxTerminal, maxPush); err != nil {
		return nil, err
	}
	maxConcurrentRuns := cfg.MaxConcurrentRunsPerOwner
	if maxConcurrentRuns == 0 {
		maxConcurrentRuns = defaultMaxConcurrentRunsPerOwner
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
	life, stopLife := context.WithCancel(context.Background())
	return &Server{
		life:                      life,
		stopLife:                  stopLife,
		skills:                    slices.Clone(cfg.Skills),
		byID:                      byID,
		store:                     store,
		router:                    router,
		owner:                     cfg.Owner,
		scopes:                    cfg.Scopes,
		tenant:                    cfg.Tenant,
		extended:                  cfg.ExtendedCard,
		push:                      newPusher(cfg.Push, log),
		log:                       log,
		maxBody:                   maxBody,
		timeout:                   timeout,
		maxHist:                   maxHist,
		defPage:                   defPage,
		maxPage:                   maxPage,
		maxTerminalTasks:          maxTerminal,
		maxPausedTasks:            maxPaused,
		maxPushConfigs:            maxPush,
		keepAlive:                 keepAliveEvery,
		pollEvery:                 pollEvery,
		maxStreamSeats:            cfg.MaxStreamSeatsPerOwner,
		streamSeatOverflow:        cfg.StreamSeatOverflow,
		seats:                     newStreamSeatRegistry(),
		maxConcurrentRunsPerOwner: maxConcurrentRuns,
		allowedOrigins:            slices.Clone(cfg.AllowedOrigins),
		now:                       time.Now,
		newID:                     newUUID,
		runs:                      map[string]*run{},
		runsByOwner:               map[string]int{},
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

// ServeHTTP answers one JSON-RPC request: POST only, same-origin or
// allow-listed Origin, a JSON content type, a capped body, and a caller
// Config.Owner resolves, before any method runs.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeTransport(w, http.StatusMethodNotAllowed,
			Errorf(CodeInvalidRequest, "POST required"))
		return
	}
	if !s.originOK(r) {
		s.writeTransport(w, http.StatusForbidden,
			Errorf(CodeUnauthorized, "cross-origin request refused"))
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		s.writeTransport(w, http.StatusUnsupportedMediaType,
			Errorf(CodeContentTypeNotSupported, "content type must be application/json or application/a2a+json"))
		return
	}
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
	if !utf8.Valid(raw) {
		s.writeTransport(w, http.StatusBadRequest, Errorf(CodeParseError, "request body is not valid UTF-8"))
		return
	}
	trimmed := strings.TrimLeft(string(raw), " \t\r\n")
	if strings.HasPrefix(trimmed, "[") {
		s.writeTransport(w, http.StatusBadRequest,
			Errorf(CodeInvalidRequest, "batch requests are not supported"))
		return
	}
	var req rpcRequest
	if err := handler.UnmarshalStrict(raw, &req); err != nil {
		s.writeTransport(w, http.StatusBadRequest, Errorf(CodeParseError, "parse request: %v", err))
		return
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		s.writeResult(w, nil, nil, Errorf(CodeInvalidRequest, "request id required"))
		return
	}
	if c := req.ID[0]; c != '"' && c != '-' && (c < '0' || c > '9') {
		s.writeResult(w, nil, nil, Errorf(CodeInvalidRequest, "request id must be a string or a number"))
		return
	}
	if req.JSONRPC != "2.0" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidRequest, `jsonrpc must be "2.0"`))
		return
	}
	owner, ok := s.owner(r)
	if !ok || !textsafe.Storable(owner) {
		s.writeTransport(w, http.StatusUnauthorized, ErrUnauthenticated())
		return
	}
	if s.tenant != nil {
		r = r.WithContext(context.WithValue(r.Context(), connectTenantKey{}, s.tenant(r)))
	}
	s.dispatch(w, r, &req, owner)
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string) {
	sc := s.callerScope(r)
	if sc.refused {
		s.writeTransport(w, http.StatusUnauthorized, ErrUnauthenticated())
		return
	}
	switch req.Method {
	case MethodSendMessage:
		s.handleSend(w, r, req, owner, false)
	case MethodSendStreamingMessage:
		s.handleSend(w, r, req, owner, true)
	case MethodGetTask:
		s.handleGetTask(w, r, req, owner, sc)
	case MethodListTasks:
		s.handleListTasks(w, r, req, owner, sc)
	case MethodCancelTask:
		s.handleCancelTask(w, r, req, owner, sc)
	case MethodSubscribeToTask:
		s.handleSubscribe(w, r, req, owner)
	case MethodCreateTaskPushNotificationConfig:
		s.handleCreatePushConfig(w, r, req, owner, sc)
	case MethodGetTaskPushNotificationConfig:
		s.handleGetPushConfig(w, r, req, owner, sc)
	case MethodListTaskPushNotificationConfigs:
		s.handleListPushConfigs(w, r, req, owner, sc)
	case MethodDeleteTaskPushNotificationConfig:
		s.handleDeletePushConfig(w, r, req, owner, sc)
	case MethodGetExtendedAgentCard:
		s.handleExtendedCard(w, r, req, owner)
	default:
		s.writeResult(w, req.ID, nil, Errorf(CodeMethodNotFound, "unknown method %q", req.Method))
	}
}

func (s *Server) writeResult(w http.ResponseWriter, id json.RawMessage, result any, aerr *Error) {
	s.writeStatus(w, http.StatusOK, id, result, aerr)
}

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
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		//gofastr:allow(rawtext) JSON-RPC protocol text for an agent, beside its numeric code
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func (s *Server) writeTransport(w http.ResponseWriter, status int, aerr *Error) {
	s.writeStatus(w, status, nil, nil, aerr)
}

func (s *Server) originOK(r *http.Request) bool {
	s.mu.Lock()
	allowed := s.allowedOrigins
	s.mu.Unlock()
	return netguard.OriginAllowed(r, allowed)
}

// SetAllowedOrigins permits browser Origins that are not same-origin with the
// request, for servers constructed without going through
// Config.AllowedOrigins.
func (s *Server) SetAllowedOrigins(origins []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowedOrigins = slices.Clone(origins)
}

func (s *Server) internalErr(w http.ResponseWriter, id json.RawMessage, what string, err error) {
	s.log.Error("a2a: "+what, "err", err)
	s.writeResult(w, id, nil, Errorf(CodeInternalError, "internal error"))
}

func decodeParams(raw json.RawMessage, v any) *Error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	if err := handler.CheckObjectKeys(raw, handler.FoldJSONKey); err != nil {
		return Errorf(CodeInvalidParams, "invalid params: %v", err)
	}
	if err := jsonexact.Unmarshal(raw, v); err != nil {
		return Errorf(CodeInvalidParams, "invalid params: %v", err)
	}
	return nil
}

func validateStoreText(field, value string) *Error {
	if textsafe.Storable(value) {
		return nil
	}
	err := Errorf(CodeInvalidParams, "%s must be valid UTF-8 without NUL", field)
	err.Data = map[string]any{"fields": map[string]string{field: err.Message}}
	return err
}

func validatePushIDs(prefix string, cfg *PushNotificationConfig) *Error {
	if err := validateStoreText(prefix+"taskId", cfg.TaskID); err != nil {
		return err
	}
	return validateStoreText(prefix+"id", cfg.ID)
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

func (s *Server) clampPageSize(n *int) int {
	if n == nil || *n < 1 {
		return s.defPage
	}
	return min(*n, s.maxPage)
}

type runClaimResult uint8

const (
	runClaimed runClaimResult = iota
	runClaimTaskAlreadyActive
	runClaimOwnerAtLimit
	runClaimShuttingDown
)

func (s *Server) claimRun(taskID, owner string) (*run, runClaimResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return nil, runClaimShuttingDown
	}
	if _, exists := s.runs[taskID]; exists {
		return nil, runClaimTaskAlreadyActive
	}
	if s.maxConcurrentRunsPerOwner >= 0 && s.runsByOwner[owner] >= s.maxConcurrentRunsPerOwner {
		return nil, runClaimOwnerAtLimit
	}
	rn := &run{
		id:         taskID,
		owner:      owner,
		cancel:     func() {},
		cancelDone: make(chan struct{}),
		bus:        newTaskBus(s.log),
		done:       make(chan struct{}),
		released:   make(chan struct{}),
	}
	s.runs[taskID] = rn
	s.runsByOwner[owner]++
	return rn, runClaimed
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
		close(rn.released)
		if s.runsByOwner[rn.owner] <= 1 {
			delete(s.runsByOwner, rn.owner)
		} else {
			s.runsByOwner[rn.owner]--
		}
	}
}

func (s *Server) finishRun(rn *run) {
	if rn.halted.Load() {
		<-rn.cancelDone
	}
	s.releaseRun(rn)
	close(rn.done)
}

func (s *Server) setRunCancel(rn *run, cancel context.CancelFunc) (halted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rn.cancel = cancel
	return rn.halted.Load()
}

// Shutdown stops claiming runs, cancels every live run's context, and waits
// until each has been released (a started run releases after its final state
// is stored and published) or ctx ends. A send or resume after Shutdown is
// refused.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	released := make([]chan struct{}, 0, len(s.runs))
	for _, rn := range s.runs {
		released = append(released, rn.released)
	}
	s.mu.Unlock()
	s.stopLife()
	for _, r := range released {
		select {
		case <-r:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *Server) wakeRun(rn *run) {
	s.mu.Lock()
	cancel := rn.cancel
	s.mu.Unlock()
	cancel()
}

func (s *Server) startRun(r *http.Request, t *taskRun, rn *run, h Handler) {
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), s.timeout)
	stopLife := context.AfterFunc(s.life, cancel)
	runCtx = WithRequest(runCtx, r)
	haltedBeforeStart := s.setRunCancel(rn, cancel)
	if haltedBeforeStart {
		cancel()
	}
	t.attach(rn, runCtx)
	returned := make(chan struct{})
	if _, inProcess := s.store.(*MemoryStore); !inProcess && !haltedBeforeStart {
		go s.watchRemoteCancel(runCtx, returned, t, rn)
	}
	go func() {
		defer func() {
			stopLife()
			cancel()
			s.finishRun(rn)
		}()
		defer func() {
			if p := recover(); p != nil {
				s.log.Error("a2a: run finalize panicked",
					"taskId", rn.id, "panic", textsafe.Recovered(p))
			}
		}()
		var err error
		if !haltedBeforeStart {
			err = s.invoke(runCtx, t, h)
		}
		close(returned)
		s.finalize(runCtx, t, err)
	}()
}

func (s *Server) watchRemoteCancel(ctx context.Context, returned <-chan struct{}, t *taskRun, rn *run) {
	tick := time.NewTicker(s.pollEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-returned:
			return
		case <-tick.C:
		}
		rctx, cancel := context.WithTimeout(ctx, storeOpTimeout)
		rec, err := s.pollGetTask(rctx, t.owner, t.taskID())
		cancel()
		if errors.Is(err, errStorePanicked) {
			return
		}
		gone := errors.Is(err, ErrNotFound)
		if !gone && (err != nil || !rec.Task.Status.State.Terminal()) {
			continue
		}
		t.mu.Lock()
		select {
		case <-returned:
			t.mu.Unlock()
			return
		default:
		}
		if gone {
			halt := !t.rec.Task.Status.State.Terminal() && rn.halted.CompareAndSwap(false, true)
			if halt {
				close(rn.cancelDone)
			}
			t.mu.Unlock()
			if halt {
				s.wakeRun(rn)
			}
			return
		}
		remote := rec.Version > t.rec.Version && !t.rec.Task.Status.State.Terminal() &&
			rn.halted.CompareAndSwap(false, true)
		if remote {
			close(rn.cancelDone)
			rn.bus.publish(busEvent{ev: StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
				TaskID:    rec.Task.ID,
				ContextID: rec.Task.ContextID,
				Status:    rec.Task.Status,
			}}, version: rec.Version})
		}
		t.mu.Unlock()
		if remote {
			s.wakeRun(rn)
		}
		return
	}
}

func (s *Server) publish(owner string, rec *TaskRecord, ev StreamResponse) {
	version := rec.Version
	if rn := s.runFor(rec.Task.ID); rn != nil {
		rn.bus.publish(busEvent{ev: ev, version: version})
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
	cfgs, err := s.store.ListPushConfigs(ctx, owner, rec.Task.ID)
	cancel()
	if err != nil {
		s.log.Error("a2a: list push configs", "taskId", rec.Task.ID, "err", err)
		return
	}
	s.push.deliver(cfgs, ev, version)
}

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
	if streaming {
		if _, ok := w.(http.Flusher); !ok {
			s.writeResult(w, req.ID, nil, ErrUnsupportedOperation("streaming requires a flushing ResponseWriter"))
			return
		}
		s.streamSend(w, r, req.ID, owner, &p)
		return
	}
	t, rn, h, aerr := s.prepareSend(r, owner, &p)
	if aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if p.Configuration != nil && p.Configuration.ReturnImmediately && h != nil {
		s.startRun(r, t, rn, h)
		task := t.snapshot()
		applyHistoryLength(task, historyLengthOf(&p))
		s.writeResult(w, req.ID, SendMessageResponse{Task: task}, nil)
		return
	}
	if h != nil {
		s.startRun(r, t, rn, h)
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

func (s *Server) validateInbound(p *SendMessageRequest) *Error {
	m := p.Message
	if m == nil {
		return Errorf(CodeInvalidParams, "message is required")
	}
	if err := validateStoreText("message.taskId", m.TaskID); err != nil {
		return err
	}
	if err := validateStoreText("message.contextId", m.ContextID); err != nil {
		return err
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
		if err := validatePushIDs("configuration.taskPushNotificationConfig.", cfg); err != nil {
			return err
		}
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
			rerr = fmt.Errorf("no skill named %q", skillID)
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
	sc := s.callerScope(r)
	if sc.refused {
		return nil, nil, nil, ErrUnauthenticated()
	}
	rec := &TaskRecord{
		Task:      task,
		Owner:     owner,
		SkillID:   skillID,
		Scope:     sc.key,
		Tenant:    sc.tenant,
		CreatedAt: s.now(),
		UpdatedAt: s.now(),
	}
	var rn *run
	if rerr == nil {
		var result runClaimResult
		rn, result = s.claimRun(taskID, owner)
		if result == runClaimOwnerAtLimit {
			return nil, nil, nil, ErrUnsupportedOperation("maximum concurrent task runs reached")
		}
		if result == runClaimShuttingDown {
			return nil, nil, nil, ErrUnsupportedOperation("server is shutting down")
		}
		if result != runClaimed {
			return nil, nil, nil, Errorf(CodeInternalError, "internal error")
		}
	}
	if cfg := pushConfigOf(p); cfg != nil {
		cfg.TaskID = taskID // the task id did not exist when the client built the request
		if aerr := s.storePushConfig(r.Context(), owner, cfg); aerr != nil {
			if rn != nil {
				s.releaseRun(rn)
			}
			return nil, nil, nil, aerr
		}
	}
	if err := s.store.CreateTask(r.Context(), rec); err != nil {
		s.log.Error("a2a: create task", "taskId", taskID, "err", err)
		s.dropSendPushConfig(r, owner, taskID, p)
		if rn != nil {
			s.releaseRun(rn)
		}
		return nil, nil, nil, Errorf(CodeInternalError, "internal error")
	}
	t := newTaskRun(s, rec, msg, r, owner)
	if rerr != nil {
		if err := t.Reject(TextPart(rerr.Error())); err != nil {
			s.log.Error("a2a: reject unrouted task", "taskId", taskID, "err", err)
			return nil, nil, nil, Errorf(CodeInternalError, "internal error")
		}
		return t, nil, nil, nil
	}
	return t, rn, h, nil
}

func (s *Server) prepareResume(r *http.Request, owner string, p *SendMessageRequest) (*taskRun, *run, Handler, *Error) {
	msg := p.Message
	if msg.MessageID == "" {
		msg.MessageID = s.newID()
	}
	taskID := msg.TaskID
	sc := s.callerScope(r)
	if sc.refused {
		return nil, nil, nil, ErrUnauthenticated()
	}
	rec, err := s.getTask(r.Context(), owner, sc, taskID)
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
		return nil, nil, nil, ErrUnsupportedOperation(fmt.Sprintf("task %s is running", taskID))
	}
	if cfg := pushConfigOf(p); cfg != nil && cfg.TaskID != "" && cfg.TaskID != taskID {
		return nil, nil, nil, Errorf(CodeInvalidParams, "taskPushNotificationConfig.taskId %q does not match addressed task %q", cfg.TaskID, taskID)
	}
	rn, claimResult := s.claimRun(taskID, owner)
	if claimResult == runClaimTaskAlreadyActive {
		return nil, nil, nil, ErrUnsupportedOperation(fmt.Sprintf("task %s is running", taskID))
	}
	if claimResult == runClaimOwnerAtLimit {
		return nil, nil, nil, ErrUnsupportedOperation("maximum concurrent task runs reached")
	}
	if claimResult == runClaimShuttingDown {
		return nil, nil, nil, ErrUnsupportedOperation("server is shutting down")
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
		s.dropSendPushConfig(r, owner, taskID, p)
		s.releaseRun(rn)
		if errors.Is(err, errConcurrentChange) {
			return nil, nil, nil, Errorf(CodeUnsupportedOperation, "task %s changed concurrently, retry", taskID)
		}
		s.log.Error("a2a: resume task", "taskId", taskID, "err", err)
		return nil, nil, nil, Errorf(CodeInternalError, "internal error")
	}
	sk, ok := s.byID[rec.SkillID]
	if !ok || sk.Handler == nil {
		if err := t.Reject(TextPart(fmt.Sprintf("no skill named %q", rec.SkillID))); err != nil {
			s.log.Error("a2a: reject resume without skill", "taskId", taskID, "err", err)
		}
		s.finishRun(rn)
		return t, nil, nil, nil
	}
	return t, rn, sk.Handler, nil
}

func (s *Server) dropSendPushConfig(r *http.Request, owner, taskID string, p *SendMessageRequest) {
	cfg := pushConfigOf(p)
	if cfg == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), storeOpTimeout)
	defer cancel()
	if err := s.store.DeletePushConfig(ctx, owner, taskID, cfg.ID); err != nil {
		s.log.Error("a2a: delete push config after refused send", "taskId", taskID, "err", err)
	}
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
	s.trimPushConfigs(ctx, owner, cfg.TaskID)
	return nil
}

func (s *Server) trimTerminalTasks(owner string, tier RetentionTier) {
	if s.maxTerminalTasks < 0 {
		return
	}
	rt, ok := s.store.(RetentionTrimmer)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
	defer cancel()
	if err := rt.TrimTerminalTasks(ctx, owner, tier, s.maxTerminalTasks); err != nil {
		s.log.Error("a2a: trim terminal tasks", "owner", owner, "err", err)
	}
}

func refuseUnwiredTrimmer(store Store, maxTerminal, maxPush int) error {
	if _, ok := store.(RetentionTrimmer); ok || (maxTerminal < 0 && maxPush < 0) {
		return nil
	}
	t := reflect.TypeOf(store)
	var found []string
	for _, name := range []string{"TrimTerminalTasks", "TrimPushConfigs"} {
		if _, ok := t.MethodByName(name); ok {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return fmt.Errorf("a2a: Config.Store %T has %s but does not implement RetentionTrimmer, so the server would never trim it and its task rows would grow without bound; "+
		"RetentionTrimmer wants TrimTerminalTasks(ctx context.Context, owner string, tier RetentionTier, keep int) error "+
		"and TrimPushConfigs(ctx context.Context, owner, taskID string, keep int) error (the tier argument arrived in v0.96.0); "+
		"a store that keeps its own retention sets TerminalTaskRetention and MaxPushConfigsPerTask to -1",
		store, strings.Join(found, " and "))
}

const pausedTrimBatch = 16

func (s *Server) trimPausedTasks(owner string, tier RetentionTier) {
	if s.maxPausedTasks < 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeOpTimeout)
	defer cancel()
	_, scopeFilters := s.store.(ScopeFilter)
	_, tenantFilters := s.store.(TenantFilter)
	var paused []*TaskRecord
	for _, st := range []TaskState{TaskStateInputRequired, TaskStateAuthRequired} {
		q := ListQuery{Status: st, Scoped: true, Scope: tier.Scope, Tenanted: true, Tenant: tier.Tenant}
		if scopeFilters && tenantFilters {
			q.Limit = s.maxPausedTasks + pausedTrimBatch
		}
		recs, _, err := s.store.ListTasks(ctx, owner, q)
		if err != nil {
			s.log.Error("a2a: list paused tasks", "owner", owner, "err", err)
			return
		}
		paused = append(paused, recs...)
	}
	paused = slices.DeleteFunc(paused, func(rec *TaskRecord) bool { return !tier.holds(rec) })
	sortRecsNewestFirst(paused)
	seen := make(map[string]bool, len(paused))
	paused = slices.DeleteFunc(paused, func(rec *TaskRecord) bool {
		dup := seen[rec.Task.ID]
		seen[rec.Task.ID] = true
		return dup
	})
	if len(paused) <= s.maxPausedTasks {
		return
	}
	canceled := false
	for _, rec := range paused[s.maxPausedTasks:] {
		cand := rec.Clone()
		cand.Task.Status = TaskStatus{State: TaskStateCanceled, Timestamp: s.stamp()}
		if err := s.store.UpdateTask(ctx, cand); err != nil {
			if !errors.Is(err, ErrConflict) {
				s.log.Error("a2a: cancel paused task", "taskId", rec.Task.ID, "err", err)
			}
			continue
		}
		s.publish(owner, cand, StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
			TaskID:    cand.Task.ID,
			ContextID: cand.Task.ContextID,
			Status:    cand.Task.Status,
		}})
		canceled = true
	}
	if canceled {
		s.trimTerminalTasks(owner, tier)
	}
}

func (s *Server) trimPushConfigs(ctx context.Context, owner, taskID string) {
	if s.maxPushConfigs < 0 {
		return
	}
	rt, ok := s.store.(RetentionTrimmer)
	if !ok {
		return
	}
	c, cancel := context.WithTimeout(ctx, storeOpTimeout)
	defer cancel()
	if err := rt.TrimPushConfigs(c, owner, taskID, s.maxPushConfigs); err != nil {
		s.log.Error("a2a: trim push configs", "taskId", taskID, "err", err)
	}
}

func (t *taskRun) snapshot() *Task {
	return t.Task()
}

func (t *taskRun) snapshotRecord() *TaskRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rec.Clone()
}

type sseStream struct {
	w    *errWriter
	fl   http.Flusher
	id   json.RawMessage
	srv  *Server
	rc   *http.ResponseController
	sent bool
}

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

func (st *sseStream) write(field string, payload []byte) error {
	if st.w.err != nil {
		return st.w.err
	}
	_ = st.rc.SetWriteDeadline(time.Now().Add(sseWriteDeadline))
	mcp.StreamSSE(st.w, "", string(payload))
	st.fl.Flush()
	st.clearDeadline()
	return st.w.err
}

func (st *sseStream) clearDeadline() {
	_ = st.rc.SetWriteDeadline(time.Time{})
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
	st.clearDeadline()
	return st.w.err
}

func (s *Server) streamSend(w http.ResponseWriter, r *http.Request, id json.RawMessage, owner string, p *SendMessageRequest) {
	seat, ok := s.seats.admitSeat(owner, s.maxStreamSeats, s.streamSeatOverflow)
	if !ok {
		s.writeTransport(w, http.StatusTooManyRequests,
			Errorf(CodeInvalidRequest, "too many concurrent streams for this owner"))
		return
	}
	defer s.seats.releaseSeat(seat)
	t, rn, h, aerr := s.prepareSend(r, owner, p)
	if aerr != nil {
		s.writeResult(w, id, nil, aerr)
		return
	}
	st := s.newSSEStream(w, id)
	var ch chan busEvent
	if h != nil {
		ch = rn.bus.subscribe()
		defer rn.bus.unsubscribe(ch)
	}
	snap := t.snapshotRecord()
	task := cloneTask(snap.Task)
	applyHistoryLength(&task, historyLengthOf(p))
	if err := st.send(StreamResponse{Task: &task}); err != nil {
		if h != nil {
			s.startRun(r, t, rn, h)
		}
		return
	}
	if h == nil {
		return // routing reject: the snapshot is already terminal
	}
	s.startRun(r, t, rn, h)
	seen, unsettled := s.forwardEvents(st, rn, ch, r, owner, seat, 0)
	if !unsettled {
		return
	}
	snap.Version = max(seen, snap.Version)
	s.pollEvents(st, r, owner, snap, seat)
}

func (s *Server) forwardEvents(st *sseStream, rn *run, ch chan busEvent, r *http.Request, owner string, seat *streamSeat, floor int64) (seen int64, unsettled bool) {
	ctx := r.Context()
	keepAlive := time.NewTicker(s.keepAlive)
	defer keepAlive.Stop()
	seen = floor
	relay := func(be busEvent) bool {
		if be.version <= floor {
			return false
		}
		if !s.callerHeld(r, owner) {
			return true
		}
		if err := st.send(be.ev); err != nil {
			return true
		}
		seen = max(seen, be.version)
		return endsStream(be.ev)
	}
	for {
		select {
		case be, ok := <-ch:
			if !ok {
				return
			}
			if relay(be) {
				return
			}
		case <-seat.done:
			return
		case <-ctx.Done():
			return
		case <-rn.done:
			for {
				select {
				case be, ok := <-ch:
					if !ok || relay(be) {
						return
					}
				default:
					return seen, true
				}
			}
		case <-keepAlive.C:
			if !s.callerHeld(r, owner) {
				return
			}
			if err := st.keepAlive(); err != nil {
				return
			}
		}
	}
}

func (s *Server) callerHeld(r *http.Request, owner string) (held bool) {
	defer func() {
		if recover() != nil {
			held = false
		}
	}()
	fresh, ok := handler.RecheckPrincipal(r.Context())
	if !ok {
		return false
	}
	fr := r.WithContext(fresh)
	got, ok := s.owner(fr)
	if !ok || got != owner {
		return false
	}
	if s.tenant == nil {
		return true
	}
	at, ok := r.Context().Value(connectTenantKey{}).(string)
	return ok && s.tenant(fr) == at
}

type connectTenantKey struct{}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var p GetTaskRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if aerr := validateStoreText("id", p.ID); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if p.ID == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "id is required"))
		return
	}
	rec, err := s.getTask(r.Context(), owner, sc, p.ID)
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

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var p ListTasksRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if aerr := validateStoreText("contextId", p.ContextID); aerr != nil {
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
	q := ListQuery{ContextID: p.ContextID, Status: p.Status, After: after,
		Scoped: sc.scoped, Scope: sc.key, Tenanted: sc.tenanted, Tenant: sc.tenant,
		Limit: size, Offset: offset}
	var recs []*TaskRecord
	var total, advance int
	var err error
	_, scopeFilters := s.store.(ScopeFilter)
	_, tenantFilters := s.store.(TenantFilter)
	if (sc.scoped && !scopeFilters) || (sc.tenanted && !tenantFilters) {
		recs, total, err = s.walkScopedTasks(r.Context(), owner, q, sc)
		advance = len(recs)
	} else {
		recs, total, err = s.store.ListTasks(r.Context(), owner, q)
		advance = len(recs)
		recs = slices.DeleteFunc(recs, func(rec *TaskRecord) bool { return !sc.sees(rec) })
	}
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
	if offset+advance < total {
		resp.NextPageToken = encodePageToken(offset + advance)
	}
	s.writeResult(w, req.ID, resp, nil)
}

func (s *Server) walkScopedTasks(ctx context.Context, owner string, q ListQuery, sc taskScope) ([]*TaskRecord, int, error) {
	offset, size := q.Offset, q.Limit
	q.Limit = max(size, walkScopedBatch)
	var page []*TaskRecord
	seen, read := 0, 0
	for {
		q.Offset = read
		recs, total, err := s.store.ListTasks(ctx, owner, q)
		if err != nil {
			return nil, 0, err
		}
		for _, rec := range recs {
			if !sc.sees(rec) {
				continue
			}
			if seen >= offset && len(page) < size {
				page = append(page, rec)
			}
			seen++
		}
		read += len(recs)
		if len(recs) == 0 || read >= total {
			return page, seen, nil
		}
	}
}

const walkScopedBatch = 200

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var p CancelTaskRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if aerr := validateStoreText("id", p.ID); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if p.ID == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "id is required"))
		return
	}
	const attempts = 8
	var (
		halted     *run // the local run this call found or left halted
		haltsOwner bool // this call set the flag, so it closes cancelDone
	)
	defer func() {
		if halted != nil {
			if haltsOwner {
				close(halted.cancelDone)
			}
			s.wakeRun(halted)
		}
	}()
	base := context.WithoutCancel(r.Context())
	for range attempts {
		ctx, cancel := context.WithTimeout(base, storeOpTimeout)
		rec, err := s.getTask(ctx, owner, sc, p.ID)
		cancel()
		if errors.Is(err, ErrNotFound) {
			s.writeResult(w, req.ID, nil, ErrTaskNotFound(p.ID))
			return
		}
		if err != nil {
			s.internalErr(w, req.ID, "get task for cancel", err)
			return
		}
		state := rec.Task.Status.State
		if state == TaskStateCanceled && halted != nil {
			halted.bus.publish(busEvent{ev: StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{
				TaskID:    rec.Task.ID,
				ContextID: rec.Task.ContextID,
				Status:    rec.Task.Status,
			}}, version: rec.Version})
			task := cloneTask(rec.Task)
			s.writeResult(w, req.ID, SendMessageResponse{Task: &task}, nil)
			return
		}
		if state.Terminal() {
			s.writeResult(w, req.ID, nil, ErrTaskNotCancelable(p.ID, state))
			return
		}
		if rn := s.runFor(p.ID); rn != nil && halted == nil {
			haltsOwner = rn.halted.CompareAndSwap(false, true)
			halted = rn
		}
		cand := rec.Clone()
		cand.Task.Status = TaskStatus{State: TaskStateCanceled, Timestamp: s.stamp()}
		ctx, cancel = context.WithTimeout(base, storeOpTimeout)
		err = s.store.UpdateTask(ctx, cand)
		cancel()
		if err != nil {
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
		s.trimTerminalTasks(owner, TierOf(cand))
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
	if aerr := validateStoreText("id", p.ID); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if p.ID == "" {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "id is required"))
		return
	}
	sc := s.callerScope(r)
	if sc.refused {
		s.writeResult(w, req.ID, nil, ErrUnauthenticated())
		return
	}
	var (
		rn  *run
		ch  chan busEvent
		rec *TaskRecord
		err error
	)
	for try := 0; ; try++ {
		if rn = s.runFor(p.ID); rn != nil {
			ch = rn.bus.subscribe()
		}
		rec, err = s.getTask(r.Context(), owner, sc, p.ID)
		if err != nil || s.runFor(p.ID) == rn {
			break
		}
		if rn != nil {
			rn.bus.unsubscribe(ch)
			rn, ch = nil, nil
		}
		if try == 2 {
			break
		}
	}
	if rn != nil && err == nil {
		if at := rn.pausedAt.Load(); at != 0 && rec.Version > at {
			rn.bus.unsubscribe(ch)
			rn, ch = nil, nil
		}
	}
	if rn != nil {
		defer rn.bus.unsubscribe(ch)
	}
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
	seat, ok := s.seats.admitSeat(owner, s.maxStreamSeats, s.streamSeatOverflow)
	if !ok {
		s.writeTransport(w, http.StatusTooManyRequests,
			Errorf(CodeInvalidRequest, "too many concurrent streams for this owner"))
		return
	}
	defer s.seats.releaseSeat(seat)
	st := s.newSSEStream(w, req.ID)
	snap := cloneTask(rec.Task)
	if err := st.send(StreamResponse{Task: &snap}); err != nil {
		return
	}
	state := rec.Task.Status.State
	if state.Terminal() || state.Interrupted() {
		return
	}
	if rn != nil {
		seen, unsettled := s.forwardEvents(st, rn, ch, r, owner, seat, rec.Version)
		if !unsettled {
			return
		}
		from := *rec
		from.Version = seen
		rec = &from
	}
	s.pollEvents(st, r, owner, rec, seat)
}

func (s *Server) pollEvents(st *sseStream, r *http.Request, owner string, rec *TaskRecord, seat *streamSeat) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-seat.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	last := rec.Version
	poll := time.NewTicker(s.pollEvery)
	defer poll.Stop()
	keepAlive := time.NewTicker(s.keepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-seat.done:
			return
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			if !s.callerHeld(r, owner) {
				return
			}
			if err := st.keepAlive(); err != nil {
				return
			}
		case <-poll.C:
			cur, err := s.pollGetTask(ctx, owner, rec.Task.ID)
			if ctx.Err() != nil {
				return // the client left or the seat was evicted
			}
			if errors.Is(err, ErrNotFound) {
				return
			}
			if errors.Is(err, errStorePanicked) {
				s.log.Error("a2a: poll read panicked", "taskId", rec.Task.ID, "err", err)
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
			if !s.callerHeld(r, owner) {
				return
			}
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

var errStorePanicked = errors.New("a2a: store GetTask panicked")

type taskScope struct {
	scoped   bool
	key      string
	tenanted bool
	tenant   string
	refused  bool
}

func (sc taskScope) sees(rec *TaskRecord) bool {
	if sc.tenanted && rec.Tenant != sc.tenant {
		return false
	}
	return !sc.scoped || rec.Scope == sc.key
}

// ScopeKey is the canonical TaskRecord.Scope of a scoped credential: its
// scopes sorted and deduplicated, as a JSON array.
func ScopeKey(scopes []string) string {
	sorted := slices.Compact(slices.Sorted(slices.Values(scopes)))
	if sorted == nil {
		sorted = []string{}
	}
	b, err := json.Marshal(sorted)
	if err != nil {
		return "[\"\"]"
	}
	return string(b)
}

func (s *Server) callerScope(r *http.Request) taskScope {
	var sc taskScope
	if s.tenant != nil {
		t := s.tenant(r)
		if !textsafe.Storable(t) {
			sc.refused = true
			return sc
		}
		sc.tenanted, sc.tenant = true, t
	}
	if s.scopes == nil {
		return sc
	}
	scopes, scoped := s.scopes(r)
	if !scoped {
		return sc
	}
	sc.scoped, sc.key = true, ScopeKey(scopes)
	return sc
}

func (s *Server) getTask(ctx context.Context, owner string, sc taskScope, id string) (*TaskRecord, error) {
	rec, err := s.store.GetTask(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if !sc.sees(rec) {
		return nil, ErrNotFound
	}
	return rec, nil
}

func (s *Server) pollGetTask(ctx context.Context, owner, taskID string) (rec *TaskRecord, err error) {
	defer func() {
		if recover() != nil {
			rec = nil
			err = errStorePanicked
		}
	}()
	return s.store.GetTask(ctx, owner, taskID)
}

func (s *Server) handleCreatePushConfig(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var cfg PushNotificationConfig
	if aerr := decodeParams(req.Params, &cfg); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if aerr := validatePushIDs("", &cfg); aerr != nil {
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
	if _, err := s.getTask(r.Context(), owner, sc, cfg.TaskID); err != nil {
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
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), storeOpTimeout)
	defer cancel()
	if aerr := s.storePushConfig(ctx, owner, &cfg); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	s.writeResult(w, req.ID, cfg, nil)
}

func (s *Server) requireOwnedTask(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope, taskID string) bool {
	if aerr := validateStoreText("taskId", taskID); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return false
	}
	_, err := s.getTask(r.Context(), owner, sc, taskID)
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

func (s *Server) handleGetPushConfig(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var p GetTaskPushNotificationConfigRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if aerr := validateStoreText("id", p.ID); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if !s.requireOwnedTask(w, r, req, owner, sc, p.TaskID) {
		return
	}
	rec, err := s.store.GetPushConfig(r.Context(), owner, p.TaskID, p.ID)
	if errors.Is(err, ErrNotFound) {
		s.writeResult(w, req.ID, nil, Errorf(CodeInvalidParams, "push config not found"))
		return
	}
	if err != nil {
		s.internalErr(w, req.ID, "get push config", err)
		return
	}
	s.writeResult(w, req.ID, rec.Config, nil)
}

func (s *Server) handleListPushConfigs(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var p ListTaskPushNotificationConfigsRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if !s.requireOwnedTask(w, r, req, owner, sc, p.TaskID) {
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
	recs, err := s.store.ListPushConfigs(r.Context(), owner, p.TaskID)
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

func (s *Server) handleDeletePushConfig(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string, sc taskScope) {
	var p DeleteTaskPushNotificationConfigRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if aerr := validateStoreText("id", p.ID); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	if !s.requireOwnedTask(w, r, req, owner, sc, p.TaskID) {
		return
	}
	err := s.store.DeletePushConfig(r.Context(), owner, p.TaskID, p.ID)
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

func (s *Server) handleExtendedCard(w http.ResponseWriter, r *http.Request, req *rpcRequest, owner string) {
	if s.extended == nil {
		s.writeResult(w, req.ID, nil, Errorf(CodeExtendedAgentCardNotConfigured, "extended agent card not configured"))
		return
	}
	var p GetExtendedAgentCardRequest
	if aerr := decodeParams(req.Params, &p); aerr != nil {
		s.writeResult(w, req.ID, nil, aerr)
		return
	}
	card, err := s.extended(r.Context(), owner)
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

// DefaultRouter picks the skill for a message:
func DefaultRouter(_ context.Context, msg *Message, skills []Skill) (string, error) {
	if msg != nil && msg.Metadata != nil {
		if v, ok := msg.Metadata["skill"]; ok {
			if name, isStr := v.(string); isStr && name != "" {
				if skillKnown(skills, name) {
					return name, nil
				}
				return "", fmt.Errorf("no skill named %q", name)
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
				return "", fmt.Errorf("no skill named %q", name)
			}
		}
	}
	if len(skills) == 1 {
		return skills[0].ID, nil
	}
	return "", errors.New("the message names no skill")
}

func skillKnown(skills []Skill, id string) bool {
	for _, sk := range skills {
		if sk.ID == id {
			return true
		}
	}
	return false
}
